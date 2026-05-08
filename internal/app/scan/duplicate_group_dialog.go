package scan

import (
	"fmt"
	"math/bits"
	"sort"
	"strconv"
	"strings"
	"time"

	"joxblox/internal/app/loader"
	"joxblox/internal/debug"
	"joxblox/internal/format"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

// duplicatePickerDoubleClickWindow is how long after a click on a row
// another click on the same row still counts as a double-click rather
// than a fresh single-click. Single-click previews the asset; double-
// click toggles its selection in the group.
const duplicatePickerDoubleClickWindow = 350 * time.Millisecond

// duplicateGroupDialog lets the user pick the assets that belong in the
// same duplicate group as the row they right-clicked. The primary
// (right-clicked) asset is always preselected and floats to the top so
// the user can scan downward for matches; other current group members
// (if the row was already grouped) are also preselected so editing an
// existing group is an additive workflow rather than a redo.
type duplicateGroupDialog struct {
	window         fyne.Window
	rows           []loader.ScanResult
	primaryAssetID int64
	selected       map[int64]struct{}
	filterEntry    *widget.Entry
	filteredRows   []loader.ScanResult
	statusLabel    *widget.Label
	table          *widget.Table
	onConfirm      func(selectedIDs []int64)

	previewImage       *canvas.Image
	previewPlaceholder *widget.Label
	previewMetaLabel   *widget.Label
	previewHintLabel   *widget.Label
	lastClickedRow     int
	lastClickedAt      time.Time

	// distancesByID maps an asset id to the dHash Hamming distance from
	// the primary (or 0 when the SHA256 matches, since byte-identical
	// files trivially have distance 0 and we want to rank them even
	// without the primary's preview being decodable). Always non-nil and
	// always contains the primary (at distance 0); missing entries mean
	// "couldn't rank" and render "-" in the Similarity column.
	distancesByID map[int64]int
	// distanceMaxBits is the bit space of the active hash so the
	// Similarity percentage can scale correctly. 56 for the standard
	// luminance dHash, 112 for the dual R+G normal-map variant (twice
	// the single-channel range because we sum two independent hashes).
	distanceMaxBits int
}

var duplicatePickerHeaders = []string{"Pick", "Similarity", "Asset ID", "Type", "Self Size", "GPU Memory", "Dimensions", "Asset SHA256"}

var duplicatePickerColumnWidths = map[string]float32{
	"Pick":         60,
	"Similarity":   110,
	"Asset ID":     130,
	"Type":         170,
	"Self Size":    100,
	"GPU Memory":   140,
	"Dimensions":   120,
	"Asset SHA256": 460,
}

// showDuplicateGroupDialog opens the picker centred on the supplied
// primary asset id. preselected is the union of {primaryAssetID} and any
// existing group members so the dialog is "edit current group" when a
// group already exists. onConfirm receives the final ordered slice on OK.
//
// allRows is the full scan result list — the dialog filters to assets
// that share the primary's content type (texture vs mesh vs etc.) so the
// list isn't thousands of unrelated rows. Within that, the primary is
// always pinned to row 0; the rest are sorted by SHA256 then asset ID so
// near-identical files cluster together visually.
func showDuplicateGroupDialog(
	window fyne.Window,
	allRows []loader.ScanResult,
	primaryAssetID int64,
	preselected []int64,
	onConfirm func(selectedIDs []int64),
) {
	if window == nil || primaryAssetID <= 0 || onConfirm == nil {
		return
	}
	d := &duplicateGroupDialog{
		window:         window,
		primaryAssetID: primaryAssetID,
		selected:       map[int64]struct{}{},
		onConfirm:      onConfirm,
		lastClickedRow: -1,
	}
	d.selected[primaryAssetID] = struct{}{}
	for _, id := range preselected {
		if id <= 0 {
			continue
		}
		d.selected[id] = struct{}{}
	}
	debug.Logf("DupDialog open: primary=%d preselected=%d totalRows=%d", primaryAssetID, len(preselected), len(allRows))
	// Make sure the primary has decodable bytes / a hash before sorting.
	// Streaming scan ships rows with stats only (FileSHA256 + dimensions
	// but no Resource / DownloadBytes) when the AssetDelivery payload
	// isn't an image; lazy-loading the primary's full preview here means
	// we can dHash it for the perceptual sort against candidates that
	// also got their bytes during streaming.
	primaryRow := lookupOrLoadPrimaryWithBytes(allRows, primaryAssetID)
	if primaryRow.AssetID == primaryAssetID {
		// Splice the freshly-loaded row back into allRows so the
		// dialog's table renders the same Resource / SHA we just fetched.
		patched := make([]loader.ScanResult, len(allRows))
		copy(patched, allRows)
		for index := range patched {
			if patched[index].AssetID == primaryAssetID {
				patched[index] = primaryRow
				break
			}
		}
		allRows = patched
	}
	d.rows, d.distancesByID, d.distanceMaxBits = filterCandidateDuplicates(allRows, primaryAssetID)
	d.filteredRows = d.rows
	d.buildAndShow()
}

// lookupOrLoadPrimaryWithBytes finds the primary row in allRows and, if it
// has no decodable image bytes, tries a synchronous full-preview fetch via
// the loader. Returns the row with whatever bytes we could populate.
// Falls through to the original row on any error so the dialog still
// opens — the SHA fast-path will at least rank byte-identical candidates.
func lookupOrLoadPrimaryWithBytes(allRows []loader.ScanResult, primaryAssetID int64) loader.ScanResult {
	var primary loader.ScanResult
	for _, row := range allRows {
		if row.AssetID == primaryAssetID {
			primary = row
			break
		}
	}
	if primary.AssetID == 0 {
		debug.Logf("DupDialog primary lookup: id=%d NOT in allRows", primaryAssetID)
		return primary
	}
	debug.Logf("DupDialog primary lookup: id=%d found typeID=%d typeName=%q sha=%q downloadBytes=%d hasResource=%t resourceBytes=%d source=%q state=%q",
		primary.AssetID, primary.AssetTypeID, primary.AssetTypeName, primary.FileSHA256,
		len(primary.DownloadBytes), primary.Resource != nil, resourceContentLen(primary.Resource),
		primary.Source, primary.State)
	if len(scanResultDecodableImageBytes(primary)) > 0 {
		return primary
	}
	debug.Logf("DupDialog primary has no decodable bytes; attempting synchronous lazy-load")
	request, err := loader.BuildSingleAssetLoadRequest(primary.AssetID, primary.AssetInput)
	if err != nil {
		debug.Logf("DupDialog BuildSingleAssetLoadRequest failed: %s", err.Error())
		return primary
	}
	preview, err := loader.LoadSingleAssetPreviewWithTrace(request, &loader.AssetRequestTrace{})
	if err != nil || preview == nil {
		errStr := "<nil preview>"
		if err != nil {
			errStr = err.Error()
		}
		debug.Logf("DupDialog LoadSingleAssetPreviewWithTrace failed: %s", errStr)
		return primary
	}
	loaded := loader.ApplyPreviewToScanResult(primary, preview)
	debug.Logf("DupDialog primary lazy-load OK: sha=%q downloadBytes=%d hasResource=%t resourceBytes=%d",
		loaded.FileSHA256, len(loaded.DownloadBytes), loaded.Resource != nil, resourceContentLen(loaded.Resource))
	return loaded
}

// resourceContentLen distinguishes "nil Resource" from "non-nil Resource
// with empty StaticContent" in diagnostic logs — both are common.
func resourceContentLen(res *fyne.StaticResource) int {
	if res == nil {
		return 0
	}
	return len(res.Content())
}

// filterCandidateDuplicates returns the row list to render (primary first,
// then content-type-matching candidates), the per-row Hamming distance
// from the primary keyed by AssetID, and the bit space those distances
// live in (56 for luminance hashes, 112 for the dual R+G normal-map
// variant).
//
// Distance is computed in two passes so a missing or undecodable preview
// for the primary doesn't blank out the whole Similarity column:
//
//   - Fast SHA path: any candidate whose FileSHA256 matches the primary's
//     non-empty SHA gets distance 0 (byte-identical = 100% similarity).
//     This works even when the primary has no preview Resource yet,
//     which is the common cause of every row rendering "-".
//   - dHash path: when the primary's image bytes ARE decodable, every
//     candidate that decodes too gets a Hamming distance.
//
// Candidates that match neither path stay out of the distance map and
// render "-" in the column. The map is always non-nil (it always
// contains the primary at distance 0) so the cell renderer never falls
// into a "no map → all blank" branch.
//
// Sort order: ranked candidates first, ascending distance; unranked
// candidates last, sorted by SHA256 then AssetID so near-identical files
// still cluster.
func filterCandidateDuplicates(rows []loader.ScanResult, primaryAssetID int64) ([]loader.ScanResult, map[int64]int, int) {
	var primary loader.ScanResult
	primaryFound := false
	for _, row := range rows {
		if row.AssetID == primaryAssetID {
			primary = row
			primaryFound = true
			break
		}
	}
	candidates := make([]loader.ScanResult, 0, len(rows))
	for _, row := range rows {
		if row.AssetID <= 0 || row.AssetID == primaryAssetID {
			continue
		}
		if primaryFound && !sameContentType(primary, row) {
			continue
		}
		candidates = append(candidates, row)
	}

	primarySHA := strings.TrimSpace(primary.FileSHA256)
	// Detect normal-map mode from the primary's slot. Normal-map dHash
	// runs over R and G channels independently and sums their distances,
	// because a normal map's Z (blue) channel dominates brightness — the
	// standard luminance hash collapses two visibly different normal
	// maps into nearly the same bits. Apply the same mode to every
	// candidate so distances are comparable.
	normalMapMode := loader.IsNormalMapProperty(primary.PropertyName)
	maxBits := dHashMaxBitsLuminance
	if normalMapMode {
		maxBits = dHashMaxBitsNormalMap
	}
	primaryLumHash, primaryNormalHashes, primaryHashOK := primaryDHashEither(primary, normalMapMode)
	debug.Logf("DupDialog filter: candidatesAfterTypeFilter=%d primarySHA=%q primaryHashOK=%t normalMapMode=%t maxBits=%d",
		len(candidates), primarySHA, primaryHashOK, normalMapMode, maxBits)

	distancesByID := map[int64]int{primaryAssetID: 0}
	rankedSHA, rankedDHash, skippedNoBytes, skippedDecodeFail := 0, 0, 0, 0
	sampleSkipped := []int64{}
	for _, row := range candidates {
		candidateSHA := strings.TrimSpace(row.FileSHA256)
		if primarySHA != "" && primarySHA == candidateSHA {
			// Byte-identical files: trust the SHA without re-decoding.
			distancesByID[row.AssetID] = 0
			rankedSHA++
			continue
		}
		if !primaryHashOK {
			if len(sampleSkipped) < 5 {
				sampleSkipped = append(sampleSkipped, row.AssetID)
			}
			continue
		}
		distance, ok, bytesLen := candidateDistanceFor(row, normalMapMode, primaryLumHash, primaryNormalHashes)
		if !ok {
			if bytesLen == 0 {
				skippedNoBytes++
			} else {
				skippedDecodeFail++
			}
			if len(sampleSkipped) < 5 {
				sampleSkipped = append(sampleSkipped, row.AssetID)
			}
			continue
		}
		distancesByID[row.AssetID] = distance
		rankedDHash++
	}
	debug.Logf("DupDialog filter result: rankedSHA=%d rankedDHash=%d skippedNoBytes=%d skippedDecodeFail=%d skippedNoPrimaryHashAndNoSHA=%d sampleSkipped=%v",
		rankedSHA, rankedDHash, skippedNoBytes, skippedDecodeFail,
		len(candidates)-rankedSHA-rankedDHash-skippedNoBytes-skippedDecodeFail, sampleSkipped)

	sort.SliceStable(candidates, func(i, j int) bool {
		distA, hasA := distancesByID[candidates[i].AssetID]
		distB, hasB := distancesByID[candidates[j].AssetID]
		switch {
		case hasA && hasB:
			if distA != distB {
				return distA < distB
			}
		case hasA && !hasB:
			return true
		case !hasA && hasB:
			return false
		}
		left := strings.TrimSpace(candidates[i].FileSHA256)
		right := strings.TrimSpace(candidates[j].FileSHA256)
		if left != right {
			if left == "" {
				return false
			}
			if right == "" {
				return true
			}
			return left < right
		}
		return candidates[i].AssetID < candidates[j].AssetID
	})

	out := make([]loader.ScanResult, 0, len(candidates)+1)
	if primaryFound {
		out = append(out, primary)
	} else {
		out = append(out, loader.ScanResult{AssetID: primaryAssetID})
	}
	out = append(out, candidates...)
	return out, distancesByID, maxBits
}

// dHashMaxBitsLuminance / dHashMaxBitsNormalMap are the bit spaces of
// the two hash variants (luminance: 8 rows × 7 horizontal compares = 56;
// normal-map: that doubled because R and G are hashed independently and
// their distances summed).
const (
	dHashMaxBitsLuminance = 56
	dHashMaxBitsNormalMap = 112
)

// primaryDHashEither computes the appropriate hash variant for the
// primary asset based on normalMapMode and returns whichever variant is
// active (only one is meaningful at a time — the other return value is
// the zero value). Best-effort: when decoding fails, we still rank
// SHA-matched candidates so the dialog isn't blank.
func primaryDHashEither(primary loader.ScanResult, normalMapMode bool) (uint64, loader.NormalMapDHashes, bool) {
	if normalMapMode {
		hashes, ok, src, decodeErr, triedBytes := tryComputeRowNormalMapDHashes(primary)
		if !ok {
			debug.Logf("DupDialog primaryDHash failed: tried=%s bytesLen=%d normalMapMode=true err=%q", src, triedBytes, decodeErr)
		} else {
			debug.Logf("DupDialog primaryDHash ok: src=%s bytesLen=%d normalMapMode=true", src, triedBytes)
		}
		return 0, hashes, ok
	}
	hash, ok, src, decodeErr, triedBytes := tryComputeRowLuminanceDHash(primary)
	if !ok {
		debug.Logf("DupDialog primaryDHash failed: tried=%s bytesLen=%d normalMapMode=false err=%q", src, triedBytes, decodeErr)
	} else {
		debug.Logf("DupDialog primaryDHash ok: src=%s bytesLen=%d normalMapMode=false", src, triedBytes)
	}
	return hash, loader.NormalMapDHashes{}, ok
}

// candidateDistanceFor computes a candidate's similarity distance from
// the primary using the active hash variant. Returns (distance, ok,
// bytesLen) so the filter loop can both record the distance and break
// down whether a skip was caused by missing bytes vs failed decode.
func candidateDistanceFor(row loader.ScanResult, normalMapMode bool, primaryLumHash uint64, primaryNormalHashes loader.NormalMapDHashes) (int, bool, int) {
	if normalMapMode {
		hashes, ok, _, _, bytesLen := tryComputeRowNormalMapDHashes(row)
		if !ok {
			return 0, false, bytesLen
		}
		return loader.NormalMapHammingDistance(primaryNormalHashes, hashes), true, bytesLen
	}
	hash, ok, _, _, bytesLen := tryComputeRowLuminanceDHash(row)
	if !ok {
		return 0, false, bytesLen
	}
	return bits.OnesCount64(primaryLumHash ^ hash), true, bytesLen
}

// tryComputeRowLuminanceDHash hashes a row's image with the standard
// luminance-based dHash (used for non-normal-map slots). Tries
// DownloadBytes first (the raw asset payload), falls back to
// Resource.Content() (the UI-rendered PNG/JPEG preview) on decode
// failure. Roblox sometimes ships textures in formats Go's stdlib
// image package can't read directly (e.g. KTX2); the rendered preview
// is the reliable fallback.
//
// Returns (hash, ok, source-tried, lastErr, bytesLen) so the caller
// can log diagnostics about which path won / what failed.
func tryComputeRowLuminanceDHash(row loader.ScanResult) (uint64, bool, string, string, int) {
	return tryHashRow(row, loader.ComputeImageDHash)
}

// tryComputeRowNormalMapDHashes is the normal-map twin of
// tryComputeRowLuminanceDHash — same byte-source fallback strategy but
// runs the dual R+G hash variant. See ComputeImageDHashesForNormalMap
// for why two channels are needed instead of luminance.
func tryComputeRowNormalMapDHashes(row loader.ScanResult) (loader.NormalMapDHashes, bool, string, string, int) {
	return tryHashRow(row, loader.ComputeImageDHashesForNormalMap)
}

// tryHashRow runs hashFn against DownloadBytes first, falling back to
// Resource.Content() if the raw download fails to decode (Roblox can
// ship texture payloads in formats Go's stdlib image package doesn't
// read; the rendered preview is the reliable fallback). Returns the
// hash, a bool for "we got a hash", a source label for diagnostics,
// the last decode error if any, and the bytesLen of the source we
// tried last.
func tryHashRow[T any](row loader.ScanResult, hashFn func([]byte) (T, error)) (T, bool, string, string, int) {
	var zero T
	if len(row.DownloadBytes) > 0 {
		hash, err := hashFn(row.DownloadBytes)
		if err == nil {
			return hash, true, "download", "", len(row.DownloadBytes)
		}
		if row.Resource != nil {
			content := row.Resource.Content()
			if len(content) > 0 {
				if rh, rerr := hashFn(content); rerr == nil {
					return rh, true, "resource(after-download-fail)", "", len(content)
				} else {
					return zero, false, "both", fmt.Sprintf("download:%v resource:%v", err, rerr), len(row.DownloadBytes)
				}
			}
		}
		return zero, false, "download", err.Error(), len(row.DownloadBytes)
	}
	if row.Resource != nil {
		content := row.Resource.Content()
		if len(content) > 0 {
			hash, err := hashFn(content)
			if err == nil {
				return hash, true, "resource", "", len(content)
			}
			return zero, false, "resource", err.Error(), len(content)
		}
	}
	return zero, false, "none", "no bytes available", 0
}

// scanResultDecodableImageBytes mirrors the lookup the loader package
// uses internally — DownloadBytes first, then the embedded preview
// Resource — so the dialog can compute dHashes on whichever payload the
// scan tab already populated.
func scanResultDecodableImageBytes(row loader.ScanResult) []byte {
	if len(row.DownloadBytes) > 0 {
		return row.DownloadBytes
	}
	if row.Resource != nil {
		if content := row.Resource.Content(); len(content) > 0 {
			return content
		}
	}
	return nil
}

// sameContentType collapses near-identical taxonomy rows (textures,
// meshes, models) so the picker scope is "things plausibly the same kind
// as the primary". Falls back to AssetTypeName equality when AssetTypeID
// isn't populated.
func sameContentType(a, b loader.ScanResult) bool {
	if a.AssetTypeID > 0 && b.AssetTypeID > 0 {
		return a.AssetTypeID == b.AssetTypeID
	}
	if strings.TrimSpace(a.AssetTypeName) != "" && strings.TrimSpace(b.AssetTypeName) != "" {
		return strings.EqualFold(strings.TrimSpace(a.AssetTypeName), strings.TrimSpace(b.AssetTypeName))
	}
	return strings.EqualFold(strings.TrimSpace(a.ContentType), strings.TrimSpace(b.ContentType))
}

func (d *duplicateGroupDialog) buildAndShow() {
	d.filterEntry = widget.NewEntry()
	d.filterEntry.SetPlaceHolder("Filter rows by asset id, sha256, type, name…")
	d.filterEntry.OnChanged = func(query string) {
		d.applyFilter(query)
	}
	d.statusLabel = widget.NewLabel("")
	d.refreshStatus()
	d.buildTable()
	d.buildPreviewPane()
	tableScroll := container.NewScroll(d.table)
	tableScroll.SetMinSize(fyne.NewSize(640, 460))
	previewBlock := d.previewPaneContainer()
	split := container.NewHSplit(tableScroll, previewBlock)
	split.Offset = 0.62
	content := container.NewBorder(
		container.NewVBox(d.filterEntry, d.statusLabel),
		nil,
		nil,
		nil,
		split,
	)
	// Open the dialog with the primary's preview already showing so the
	// user has an anchor to compare candidate rows against without an
	// initial click.
	if len(d.rows) > 0 {
		d.showPreview(d.rows[0])
	}
	confirm := dialog.NewCustomConfirm(
		fmt.Sprintf("Group duplicates of asset %d", d.primaryAssetID),
		"Tag as duplicates",
		"Cancel",
		content,
		func(confirmed bool) {
			if !confirmed {
				return
			}
			d.onConfirm(d.orderedSelection())
		},
		d.window,
	)
	confirm.Resize(fyne.NewSize(1100, 600))
	confirm.Show()
}

func (d *duplicateGroupDialog) buildPreviewPane() {
	d.previewPlaceholder = widget.NewLabel("Single-click a row to preview, double-click to add or remove it from the group.")
	d.previewPlaceholder.Wrapping = fyne.TextWrapWord
	d.previewImage = canvas.NewImageFromImage(nil)
	d.previewImage.FillMode = canvas.ImageFillContain
	d.previewImage.ScaleMode = canvas.ImageScaleSmooth
	d.previewImage.SetMinSize(fyne.NewSize(320, 320))
	d.previewMetaLabel = widget.NewLabel("")
	d.previewMetaLabel.Wrapping = fyne.TextWrapWord
	d.previewHintLabel = widget.NewLabel("")
	d.previewHintLabel.Wrapping = fyne.TextWrapWord
	d.previewHintLabel.TextStyle = fyne.TextStyle{Italic: true}
}

func (d *duplicateGroupDialog) previewPaneContainer() fyne.CanvasObject {
	imageHolder := container.NewMax(
		container.NewCenter(d.previewPlaceholder),
		container.NewCenter(d.previewImage),
	)
	infoBlock := container.NewVBox(d.previewMetaLabel, d.previewHintLabel)
	pane := container.NewBorder(nil, infoBlock, nil, nil, imageHolder)
	return pane
}

func (d *duplicateGroupDialog) buildTable() {
	d.table = widget.NewTableWithHeaders(
		func() (int, int) {
			return len(d.filteredRows), len(duplicatePickerHeaders)
		},
		func() fyne.CanvasObject {
			return widget.NewLabel("")
		},
		func(id widget.TableCellID, object fyne.CanvasObject) {
			label, ok := object.(*widget.Label)
			if !ok {
				return
			}
			if id.Row < 0 || id.Row >= len(d.filteredRows) || id.Col < 0 || id.Col >= len(duplicatePickerHeaders) {
				label.SetText("")
				return
			}
			row := d.filteredRows[id.Row]
			label.SetText(d.cellValue(row, duplicatePickerHeaders[id.Col]))
		},
	)
	d.table.CreateHeader = func() fyne.CanvasObject {
		return widget.NewLabel("")
	}
	d.table.UpdateHeader = func(id widget.TableCellID, object fyne.CanvasObject) {
		label, ok := object.(*widget.Label)
		if !ok {
			return
		}
		if id.Row == -1 && id.Col >= 0 && id.Col < len(duplicatePickerHeaders) {
			label.SetText(duplicatePickerHeaders[id.Col])
			return
		}
		if id.Col == -1 && id.Row >= 0 {
			label.SetText(strconv.Itoa(id.Row + 1))
		} else {
			label.SetText("")
		}
	}
	for index, header := range duplicatePickerHeaders {
		if width, found := duplicatePickerColumnWidths[header]; found {
			d.table.SetColumnWidth(index, width)
		}
	}
	d.table.OnSelected = func(id widget.TableCellID) {
		if id.Row < 0 || id.Row >= len(d.filteredRows) {
			d.table.Unselect(id)
			return
		}
		row := d.filteredRows[id.Row]
		now := time.Now()
		isDoubleClick := id.Row == d.lastClickedRow && now.Sub(d.lastClickedAt) < duplicatePickerDoubleClickWindow
		d.lastClickedRow = id.Row
		d.lastClickedAt = now
		if isDoubleClick && row.AssetID > 0 {
			d.toggleSelection(row.AssetID)
		} else {
			// Single click: show the preview but don't change the
			// selection. The user double-clicks to commit a row to the
			// group, mirroring the Pick column's ✓ flip.
			d.showPreview(row)
		}
		// Always unselect so the same row's *next* click also fires
		// OnSelected — Fyne treats clicks on the currently-selected cell
		// as no-ops otherwise, which would prevent double-click detection
		// from working on rows the user just previewed.
		d.table.Unselect(id)
	}
}

func (d *duplicateGroupDialog) toggleSelection(assetID int64) {
	if _, ok := d.selected[assetID]; ok {
		delete(d.selected, assetID)
	} else {
		d.selected[assetID] = struct{}{}
	}
	d.refreshStatus()
	if d.table != nil {
		d.table.Refresh()
	}
}

// showPreview swaps the right-hand preview pane to the supplied row.
// Falls back to a placeholder label when the row has no preview Resource
// (e.g. failed loads, non-image asset types) — we don't try to fetch
// previews on the fly here; if the asset hasn't been previewed in the
// scan yet, it won't render in the dialog either.
func (d *duplicateGroupDialog) showPreview(row loader.ScanResult) {
	hint := "Single-click any row to preview it. Double-click to add or remove it from the group."
	if row.AssetID == d.primaryAssetID {
		hint = "Primary asset (will always be in the group). Double-click another row to add it as a duplicate."
	}
	d.previewHintLabel.SetText(hint)
	if row.Resource != nil && len(row.Resource.StaticContent) > 0 {
		d.previewImage.Resource = row.Resource
		d.previewImage.Refresh()
		d.previewImage.Show()
		d.previewPlaceholder.Hide()
	} else {
		d.previewImage.Hide()
		d.previewPlaceholder.SetText(fmt.Sprintf("No preview available for asset %d.", row.AssetID))
		d.previewPlaceholder.Show()
	}
	d.previewMetaLabel.SetText(formatPreviewMetaLine(row))
}

func formatPreviewMetaLine(row loader.ScanResult) string {
	parts := []string{fmt.Sprintf("ID %d", row.AssetID)}
	if typeLabel := strings.TrimSpace(loader.ScanResultTypeLabel(row)); typeLabel != "" && typeLabel != "-" {
		parts = append(parts, typeLabel)
	}
	if row.Width > 0 && row.Height > 0 {
		parts = append(parts, format.FormatDimensions(row.Width, row.Height))
	}
	if gpuLine := loader.FormatScanResultGPUMemory(row); gpuLine != "" && gpuLine != "-" {
		parts = append(parts, gpuLine)
	}
	if sha := strings.TrimSpace(row.FileSHA256); sha != "" {
		// SHA256 is long; truncate so the line stays readable.
		if len(sha) > 16 {
			sha = sha[:16] + "…"
		}
		parts = append(parts, "sha "+sha)
	}
	return strings.Join(parts, "   ·   ")
}

func (d *duplicateGroupDialog) cellValue(row loader.ScanResult, columnName string) string {
	switch columnName {
	case "Pick":
		if _, picked := d.selected[row.AssetID]; picked {
			return "✓"
		}
		return ""
	case "Similarity":
		if row.AssetID == d.primaryAssetID {
			return "primary"
		}
		dist, ok := d.distancesByID[row.AssetID]
		if !ok {
			return "-"
		}
		// Map distance → percent so the column reads "100% (0)" for
		// identical, decreasing as distance grows. distanceMaxBits picks
		// the right denominator for the active hash variant (56 for
		// luminance, 112 for the dual R+G normal-map sum). Distance 0
		// also covers byte-identical SHA matches, where we never had to
		// decode the image at all.
		maxBits := d.distanceMaxBits
		if maxBits <= 0 {
			maxBits = dHashMaxBitsLuminance
		}
		pct := 100 - dist*100/maxBits
		return fmt.Sprintf("%d%% (%d)", pct, dist)
	case "Asset ID":
		return strconv.FormatInt(row.AssetID, 10)
	case "Type":
		return loader.ScanResultTypeLabel(row)
	case "Self Size":
		return format.FormatSizeAuto(row.BytesSize)
	case "GPU Memory":
		return loader.FormatScanResultGPUMemory(row)
	case "Dimensions":
		if row.Width > 0 && row.Height > 0 {
			return format.FormatDimensions(row.Width, row.Height)
		}
		return "-"
	case "Asset SHA256":
		if strings.TrimSpace(row.FileSHA256) == "" {
			return "-"
		}
		return row.FileSHA256
	}
	return ""
}

func (d *duplicateGroupDialog) applyFilter(rawQuery string) {
	query := strings.TrimSpace(strings.ToLower(rawQuery))
	if query == "" {
		d.filteredRows = d.rows
	} else {
		out := make([]loader.ScanResult, 0, len(d.rows))
		for _, row := range d.rows {
			if row.AssetID == d.primaryAssetID {
				// Always keep the primary at the top so the user never
				// loses it by typing.
				out = append(out, row)
				continue
			}
			if duplicatePickerRowMatches(row, query) {
				out = append(out, row)
			}
		}
		d.filteredRows = out
	}
	d.refreshStatus()
	if d.table != nil {
		d.table.Refresh()
	}
}

func duplicatePickerRowMatches(row loader.ScanResult, lowerQuery string) bool {
	if strings.Contains(strconv.FormatInt(row.AssetID, 10), lowerQuery) {
		return true
	}
	if strings.Contains(strings.ToLower(strings.TrimSpace(row.FileSHA256)), lowerQuery) {
		return true
	}
	if strings.Contains(strings.ToLower(loader.ScanResultTypeLabel(row)), lowerQuery) {
		return true
	}
	if strings.Contains(strings.ToLower(strings.TrimSpace(row.InstanceName)), lowerQuery) {
		return true
	}
	if strings.Contains(strings.ToLower(strings.TrimSpace(row.AssetInput)), lowerQuery) {
		return true
	}
	return false
}

func (d *duplicateGroupDialog) refreshStatus() {
	// Count how many of the filtered candidates carry a similarity
	// distance — if this is 0 (or 1, meaning only the primary itself is
	// in the map) the column will look empty, which usually points at a
	// missing primary preview / SHA. Surfacing the count up-front gives
	// a faster diagnosis than staring at "-" cells.
	rankedCandidates := 0
	for _, row := range d.filteredRows {
		if row.AssetID == d.primaryAssetID {
			continue
		}
		if _, ok := d.distancesByID[row.AssetID]; ok {
			rankedCandidates++
		}
	}
	candidateCount := 0
	for _, row := range d.filteredRows {
		if row.AssetID != d.primaryAssetID {
			candidateCount++
		}
	}
	d.statusLabel.SetText(fmt.Sprintf(
		"Selected %d   |   Showing %d of %d candidates   |   Ranked %d/%d   |   Click row to preview, double-click to add/remove",
		len(d.selected),
		len(d.filteredRows),
		len(d.rows),
		rankedCandidates,
		candidateCount,
	))
}

// orderedSelection returns the selected ids with the primary first, then
// the remaining selected ids in the order they appear in d.rows so the
// HTML report can render the user's primary at the top of the group and
// the other duplicates beneath it in a stable order.
func (d *duplicateGroupDialog) orderedSelection() []int64 {
	out := make([]int64, 0, len(d.selected))
	if _, picked := d.selected[d.primaryAssetID]; picked {
		out = append(out, d.primaryAssetID)
	}
	for _, row := range d.rows {
		if row.AssetID == d.primaryAssetID {
			continue
		}
		if _, picked := d.selected[row.AssetID]; picked {
			out = append(out, row.AssetID)
		}
	}
	return out
}
