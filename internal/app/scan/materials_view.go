package scan

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"sort"
	"strconv"
	"strings"

	"joxblox/internal/app/loader"
	"joxblox/internal/app/report"
	"joxblox/internal/app/ui/materialscommon"
	"joxblox/internal/format"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// Preview slot indices for the 4-up Color/Normal/Metalness/Roughness pane.
const (
	scanPreviewSlotColor     = 0
	scanPreviewSlotNormal    = 1
	scanPreviewSlotMetalness = 2
	scanPreviewSlotRoughness = 3
)

// materialsView renders one row per unique (color, normal, M, R) PBR
// material combo found across the scan's SurfaceAppearance rows, with the
// engine-allocation-correct effective sizes + GPU bytes per slot. Lives
// next to the asset-level scan table as a sibling sub-tab so users can
// drill into "what does the engine actually upload" without losing the
// "what's the literal authored size of each asset" view.
//
// Visual shell mirrors the RenderDoc tab's `Materials` sub-tab: filter
// entry above a sortable table on the left, 4-up Color/Normal/Metal/Rough
// preview pane on the right, click-to-preview semantics. Shared shell
// pieces live in [materialscommon].
type materialsView struct {
	content        fyne.CanvasObject
	statsLabel     *widget.Label
	countLabel     *widget.Label
	filterEntry    *widget.Entry
	table          *widget.Table
	preview        *materialscommon.PreviewPane
	headers        []string
	entries        []report.ScanMaterialEntry
	displayEntries []report.ScanMaterialEntry
	filterText     string
	sortField      string
	sortDescending bool
	totalBytes     int64
	thumbnails     *materialscommon.ThumbnailCache
	// resourceByAssetID indexes the latest scan rows by AssetID so the
	// thumbnail decode path can find the PNG/JPEG bytes for any slot
	// without re-walking the row list. Rebuilt every Refresh.
	resourceByAssetID map[int64]*fyne.StaticResource
	selectedRow       int
	// previewGeneration increments on each row click. Background decodes
	// capture the value at start and discard their result if a newer
	// click has happened, mirroring the renderdoc tab's staleness guard.
	previewGeneration int
}

var materialsColumnHeaders = []string{
	"Color",
	"Normal",
	"Metalness",
	"Roughness",
	"Effective Normal",
	"MR Pack",
	"GPU Memory",
	"Instances",
	"PBR",
	"Instance Path",
}

// thumbnailColumns is the set of headers that render as image cells
// rather than text. Anything else falls through to plain-label rendering.
var thumbnailColumns = map[string]bool{
	"Color":     true,
	"Normal":    true,
	"Metalness": true,
	"Roughness": true,
}

var materialsColumnWidths = map[string]float32{
	"Color":            48,
	"Normal":           48,
	"Metalness":        48,
	"Roughness":        48,
	"Effective Normal": 130,
	"MR Pack":          110,
	"GPU Memory":       110,
	"Instances":        80,
	"PBR":              80,
	"Instance Path":    420,
}

func newMaterialsView() *materialsView {
	view := &materialsView{
		statsLabel:        widget.NewLabel("Engine GPU memory: 0 B (0 materials)"),
		countLabel:        widget.NewLabel(""),
		headers:           materialsColumnHeaders,
		sortField:         "GPU Memory",
		sortDescending:    true,
		thumbnails:        materialscommon.NewThumbnailCache(),
		resourceByAssetID: map[int64]*fyne.StaticResource{},
		selectedRow:       -1,
	}

	view.filterEntry = widget.NewEntry()
	view.filterEntry.SetPlaceHolder("Filter by asset ID or instance path")
	view.filterEntry.OnChanged = func(text string) {
		view.filterText = strings.TrimSpace(text)
		view.applyFilterAndSort()
		if view.table != nil {
			view.table.Refresh()
		}
		view.updateCountLabel()
	}

	view.preview = materialscommon.NewPreviewPane([]materialscommon.PreviewSlot{
		{Title: "Color"},
		{Title: "Normal"},
		{Title: "Metalness"},
		{Title: "Roughness"},
	}, "Select a material to preview.")

	view.buildTable()

	header := container.NewVBox(view.statsLabel, view.filterEntry)
	split := container.NewHSplit(view.table, view.preview.Container())
	split.Offset = 0.65
	view.content = container.NewBorder(header, view.countLabel, nil, nil, split)
	return view
}

func (view *materialsView) Content() fyne.CanvasObject {
	return view.content
}

// Refresh recomputes the materials table from the latest scan rows.
// Single pass over rows + materials via CollectScanMaterialReport — both
// the per-entry list and the headline total are derived from the same
// data structures, so there's no double-walk during streaming scans.
func (view *materialsView) Refresh(rows []loader.ScanResult) {
	if view == nil {
		return
	}
	entries, totalBytes := report.CollectScanMaterialAndImageReport(rows)
	view.entries = entries
	view.totalBytes = totalBytes
	view.resourceByAssetID = buildResourceIndex(rows)
	// New row set → previously cached previews may belong to assets that
	// are no longer in the table. Drop the cache so stale thumbnails
	// don't leak across loads, and reset the selected-row pointer.
	view.thumbnails = materialscommon.NewThumbnailCache()
	view.selectedRow = -1
	view.previewGeneration++
	view.preview.Reset("Select a material to preview.")
	view.applyFilterAndSort()
	pbrCount, looseCount, mismatchedCount := countMaterialKinds(view.entries)
	view.statsLabel.SetText(fmt.Sprintf(
		"Engine GPU memory: %s   |   PBR: %d (%d mismatched)   |   Images: %d",
		format.FormatSizeAuto64(view.totalBytes),
		pbrCount,
		mismatchedCount,
		looseCount,
	))
	view.updateCountLabel()
	if view.table != nil {
		view.table.Refresh()
	}
}

func (view *materialsView) updateCountLabel() {
	if view.countLabel == nil {
		return
	}
	view.countLabel.SetText(fmt.Sprintf("Showing %d of %d materials", len(view.displayEntries), len(view.entries)))
}

// buildResourceIndex maps AssetID → the latest StaticResource available
// for that asset across all rows. Multiple rows can reference the same
// asset (one per SurfaceAppearance instance); any row's Resource is
// equivalent for thumbnail purposes, so last-write-wins is fine.
func buildResourceIndex(rows []loader.ScanResult) map[int64]*fyne.StaticResource {
	out := map[int64]*fyne.StaticResource{}
	for i := range rows {
		row := rows[i]
		if row.Resource == nil || row.AssetID <= 0 {
			continue
		}
		out[row.AssetID] = row.Resource
	}
	return out
}

func countMaterialKinds(entries []report.ScanMaterialEntry) (pbr, loose, mismatched int) {
	for _, entry := range entries {
		if entry.LooseImage {
			loose++
			continue
		}
		pbr++
		if entry.Mismatched {
			mismatched++
		}
	}
	return pbr, loose, mismatched
}

func (view *materialsView) buildTable() {
	view.table = widget.NewTableWithHeaders(
		func() (int, int) {
			return len(view.displayEntries), len(view.headers)
		},
		func() fyne.CanvasObject { return materialscommon.NewThumbnailCell() },
		func(id widget.TableCellID, object fyne.CanvasObject) {
			label := materialscommon.ThumbnailCellLabel(object)
			img := materialscommon.ThumbnailCellImage(object)
			if label == nil || img == nil {
				return
			}
			if id.Row < 0 || id.Row >= len(view.displayEntries) || id.Col < 0 || id.Col >= len(view.headers) {
				label.SetText("")
				label.Show()
				img.Hide()
				return
			}
			view.renderCell(view.displayEntries[id.Row], view.headers[id.Col], label, img)
		},
	)
	view.table.CreateHeader = func() fyne.CanvasObject {
		return widget.NewButton("", nil)
	}
	view.table.UpdateHeader = func(id widget.TableCellID, object fyne.CanvasObject) {
		button, ok := object.(*widget.Button)
		if !ok {
			return
		}
		if id.Row == -1 && id.Col >= 0 && id.Col < len(view.headers) {
			columnName := view.headers[id.Col]
			text := columnName
			if view.sortField == columnName {
				if view.sortDescending {
					text = columnName + " ▼"
				} else {
					text = columnName + " ▲"
				}
			}
			button.SetText(text)
			button.OnTapped = func() {
				if view.sortField == columnName {
					view.sortDescending = !view.sortDescending
				} else {
					view.sortField = columnName
					view.sortDescending = true
				}
				view.applyFilterAndSort()
				if view.table != nil {
					view.table.Refresh()
				}
			}
			return
		}
		if id.Col == -1 && id.Row >= 0 {
			button.SetText(strconv.Itoa(id.Row + 1))
		} else {
			button.SetText("")
		}
		button.OnTapped = nil
	}
	view.table.OnSelected = func(id widget.TableCellID) {
		if id.Row < 0 || id.Row >= len(view.displayEntries) {
			return
		}
		view.selectedRow = id.Row
		view.previewGeneration++
		view.updatePreview(view.displayEntries[id.Row])
	}
	for index, header := range view.headers {
		if width, found := materialsColumnWidths[header]; found {
			view.table.SetColumnWidth(index, width)
		}
	}
}

func (view *materialsView) renderCell(entry report.ScanMaterialEntry, columnName string, label *widget.Label, img *canvas.Image) {
	label.Hide()
	img.Hide()
	if thumbnailColumns[columnName] {
		view.renderThumbnailCell(entry, columnName, label, img)
		return
	}
	label.SetText(view.cellText(entry, columnName))
	label.Show()
}

func (view *materialsView) renderThumbnailCell(entry report.ScanMaterialEntry, columnName string, label *widget.Label, img *canvas.Image) {
	assetID := slotAssetID(entry, columnName)
	if assetID <= 0 {
		label.SetText("—")
		label.Show()
		return
	}
	key := strconv.FormatInt(assetID, 10)
	if cached, ok := view.thumbnails.Get(key); ok && cached != nil {
		img.Image = cached
		img.Refresh()
		img.Show()
		return
	}
	resource, ok := view.resourceByAssetID[assetID]
	if !ok || resource == nil {
		// No preview bytes loaded for this asset — show the asset ID so
		// the cell still identifies what's there.
		label.SetText(key)
		label.Show()
		return
	}
	label.SetText("…")
	label.Show()
	view.thumbnails.RequestDecode(key, func() (image.Image, error) {
		return decodeImageBytes(resource.Content())
	}, func() {
		if view.table != nil {
			view.table.Refresh()
		}
	})
}

func slotAssetID(entry report.ScanMaterialEntry, columnName string) int64 {
	switch columnName {
	case "Color":
		return entry.ColorAssetID
	case "Normal":
		return entry.NormalAssetID
	case "Metalness":
		return entry.MetalnessAssetID
	case "Roughness":
		return entry.RoughnessAssetID
	}
	return 0
}

func decodeImageBytes(data []byte) (image.Image, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("empty image bytes")
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	return img, err
}

func (view *materialsView) cellText(entry report.ScanMaterialEntry, columnName string) string {
	switch columnName {
	case "Effective Normal":
		if entry.EffectiveNormalWidth <= 0 || entry.EffectiveNormalHeight <= 0 {
			return "-"
		}
		if entry.EffectiveNormalWidth == entry.NormalWidth && entry.EffectiveNormalHeight == entry.NormalHeight {
			return format.FormatDimensions(entry.EffectiveNormalWidth, entry.EffectiveNormalHeight)
		}
		return fmt.Sprintf("%s ↑", format.FormatDimensions(entry.EffectiveNormalWidth, entry.EffectiveNormalHeight))
	case "MR Pack":
		if entry.MRPackWidth <= 0 || entry.MRPackHeight <= 0 {
			return "-"
		}
		return format.FormatDimensions(entry.MRPackWidth, entry.MRPackHeight)
	case "GPU Memory":
		total := entry.ColorBytes + entry.NormalBytes + entry.MRPackBytes
		if total <= 0 {
			return "-"
		}
		return format.FormatSizeAuto64(total)
	case "Instances":
		return strconv.Itoa(entry.InstanceCount)
	case "PBR":
		if entry.LooseImage {
			return "image"
		}
		if entry.Mismatched {
			return "mismatched"
		}
		return "matched"
	case "Instance Path":
		if strings.TrimSpace(entry.InstancePath) == "" {
			return "-"
		}
		return entry.InstancePath
	}
	return ""
}

// formatMaterialSlot is kept for the preview-pane info text — describes
// a slot with its asset ID and authored dimensions in the form
// "1234 (256×256)".
func formatMaterialSlot(assetID int64, width, height int) string {
	if assetID <= 0 && width <= 0 {
		return "—"
	}
	if assetID <= 0 {
		return format.FormatDimensions(width, height)
	}
	if width <= 0 || height <= 0 {
		return strconv.FormatInt(assetID, 10)
	}
	return fmt.Sprintf("%d (%s)", assetID, format.FormatDimensions(width, height))
}

func (view *materialsView) updatePreview(entry report.ScanMaterialEntry) {
	gen := view.previewGeneration
	view.setPreviewSlot(scanPreviewSlotColor, entry.ColorAssetID, gen)
	view.setPreviewSlot(scanPreviewSlotNormal, entry.NormalAssetID, gen)
	view.setPreviewSlot(scanPreviewSlotMetalness, entry.MetalnessAssetID, gen)
	view.setPreviewSlot(scanPreviewSlotRoughness, entry.RoughnessAssetID, gen)

	var b strings.Builder
	totalGPU := entry.ColorBytes + entry.NormalBytes + entry.MRPackBytes
	if entry.LooseImage {
		fmt.Fprintf(&b, "Image (Decal / ImageLabel / Texture / etc.)   References: %d   GPU Memory: %s\n",
			entry.InstanceCount, format.FormatSizeAuto64(totalGPU))
		fmt.Fprintf(&b, "Asset:     %s   (%s)\n",
			formatMaterialSlot(entry.ColorAssetID, entry.ColorWidth, entry.ColorHeight),
			format.FormatSizeAuto64(entry.ColorBytes))
	} else {
		fmt.Fprintf(&b, "Instances: %d   GPU Memory: %s\n",
			entry.InstanceCount, format.FormatSizeAuto64(totalGPU))
		fmt.Fprintf(&b, "Color:     %s   (%s)\n",
			formatMaterialSlot(entry.ColorAssetID, entry.ColorWidth, entry.ColorHeight),
			format.FormatSizeAuto64(entry.ColorBytes))
		fmt.Fprintf(&b, "Normal:    %s\n",
			formatMaterialSlot(entry.NormalAssetID, entry.NormalWidth, entry.NormalHeight))
		if entry.EffectiveNormalWidth > 0 && entry.EffectiveNormalHeight > 0 {
			if entry.EffectiveNormalWidth != entry.NormalWidth || entry.EffectiveNormalHeight != entry.NormalHeight {
				fmt.Fprintf(&b, "  effective: %s ↑   (%s)\n",
					format.FormatDimensions(entry.EffectiveNormalWidth, entry.EffectiveNormalHeight),
					format.FormatSizeAuto64(entry.NormalBytes))
			} else {
				fmt.Fprintf(&b, "  effective: %s   (%s)\n",
					format.FormatDimensions(entry.EffectiveNormalWidth, entry.EffectiveNormalHeight),
					format.FormatSizeAuto64(entry.NormalBytes))
			}
		}
		fmt.Fprintf(&b, "Metalness: %s\n",
			formatMaterialSlot(entry.MetalnessAssetID, entry.MetalnessWidth, entry.MetalnessHeight))
		fmt.Fprintf(&b, "Roughness: %s\n",
			formatMaterialSlot(entry.RoughnessAssetID, entry.RoughnessWidth, entry.RoughnessHeight))
		if entry.MRPackWidth > 0 && entry.MRPackHeight > 0 {
			fmt.Fprintf(&b, "MR pack:   %s   (%s)\n",
				format.FormatDimensions(entry.MRPackWidth, entry.MRPackHeight),
				format.FormatSizeAuto64(entry.MRPackBytes))
		}
		if entry.Mismatched {
			b.WriteString("\nMismatched PBR: authored slots are not at the same resolution\n")
		}
	}
	if path := strings.TrimSpace(entry.InstancePath); path != "" {
		fmt.Fprintf(&b, "\nInstance: %s\n", path)
	}
	view.preview.SetInfo(b.String())
}

func (view *materialsView) setPreviewSlot(slotIndex int, assetID int64, gen int) {
	if assetID <= 0 {
		view.preview.ClearSlot(slotIndex)
		return
	}
	caption := strconv.FormatInt(assetID, 10)
	key := caption
	if cached, ok := view.thumbnails.Get(key); ok && cached != nil {
		view.preview.SetImage(slotIndex, caption, cached)
		return
	}
	view.preview.SetImage(slotIndex, caption, nil)
	resource, ok := view.resourceByAssetID[assetID]
	if !ok || resource == nil {
		return
	}
	view.thumbnails.RequestDecode(key, func() (image.Image, error) {
		return decodeImageBytes(resource.Content())
	}, func() {
		if view.previewGeneration != gen {
			if view.table != nil {
				view.table.Refresh()
			}
			return
		}
		if decoded, ok := view.thumbnails.Get(key); ok {
			view.preview.SetImage(slotIndex, caption, decoded)
		}
		if view.table != nil {
			view.table.Refresh()
		}
	})
}

func (view *materialsView) applyFilterAndSort() {
	filter := strings.ToLower(view.filterText)
	display := view.entries[:0:0]
	for _, entry := range view.entries {
		if filter != "" && !materialEntryMatchesFilter(entry, filter) {
			continue
		}
		display = append(display, entry)
	}
	sort.SliceStable(display, func(i, j int) bool {
		left := display[i]
		right := display[j]
		cmp := compareMaterialEntries(left, right, view.sortField)
		if cmp == 0 {
			return left.InstancePath < right.InstancePath
		}
		if view.sortDescending {
			return cmp > 0
		}
		return cmp < 0
	})
	view.displayEntries = display
}

func materialEntryMatchesFilter(entry report.ScanMaterialEntry, filter string) bool {
	candidates := []string{
		strconv.FormatInt(entry.ColorAssetID, 10),
		strconv.FormatInt(entry.NormalAssetID, 10),
		strconv.FormatInt(entry.MetalnessAssetID, 10),
		strconv.FormatInt(entry.RoughnessAssetID, 10),
		entry.InstancePath,
	}
	for _, c := range candidates {
		if strings.Contains(strings.ToLower(c), filter) {
			return true
		}
	}
	return false
}

func compareMaterialEntries(left, right report.ScanMaterialEntry, field string) int {
	switch field {
	case "Color":
		return loader.CompareInt64(left.ColorAssetID, right.ColorAssetID)
	case "Normal":
		return loader.CompareInt64(left.NormalAssetID, right.NormalAssetID)
	case "Metalness":
		return loader.CompareInt64(left.MetalnessAssetID, right.MetalnessAssetID)
	case "Roughness":
		return loader.CompareInt64(left.RoughnessAssetID, right.RoughnessAssetID)
	case "Effective Normal":
		return loader.CompareInt(left.EffectiveNormalWidth*left.EffectiveNormalHeight,
			right.EffectiveNormalWidth*right.EffectiveNormalHeight)
	case "MR Pack":
		return loader.CompareInt(left.MRPackWidth*left.MRPackHeight, right.MRPackWidth*right.MRPackHeight)
	case "GPU Memory":
		return loader.CompareInt64(
			left.ColorBytes+left.NormalBytes+left.MRPackBytes,
			right.ColorBytes+right.NormalBytes+right.MRPackBytes,
		)
	case "Instances":
		return loader.CompareInt(left.InstanceCount, right.InstanceCount)
	case "PBR":
		l, r := 0, 0
		if left.Mismatched {
			l = 1
		}
		if right.Mismatched {
			r = 1
		}
		return loader.CompareInt(l, r)
	case "Instance Path":
		return strings.Compare(left.InstancePath, right.InstancePath)
	}
	return 0
}
