package scan

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"joxblox/internal/app/loader"
	"joxblox/internal/app/report"
	"joxblox/internal/format"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// materialsView renders one row per unique (color, normal, M, R) PBR
// material combo found across the scan's SurfaceAppearance rows, with the
// engine-allocation-correct effective sizes + GPU bytes per slot. Lives
// next to the asset-level scan table as a sibling sub-tab so users can
// drill into "what does the engine actually upload" without losing the
// "what's the literal authored size of each asset" view.
type materialsView struct {
	content        fyne.CanvasObject
	statsLabel     *widget.Label
	table          *widget.Table
	headers        []string
	entries        []report.ScanMaterialEntry
	sortField      string
	sortDescending bool
}

var materialsColumnHeaders = []string{
	"Color",
	"Normal",
	"Effective Normal",
	"Metalness",
	"Roughness",
	"MR Pack",
	"GPU Memory",
	"Instances",
	"PBR",
	"Instance Path",
}

var materialsColumnWidths = map[string]float32{
	"Color":            190,
	"Normal":           190,
	"Effective Normal": 150,
	"Metalness":        190,
	"Roughness":        190,
	"MR Pack":          120,
	"GPU Memory":       120,
	"Instances":        90,
	"PBR":              80,
	"Instance Path":    520,
}

func newMaterialsView() *materialsView {
	view := &materialsView{
		statsLabel:     widget.NewLabel("Engine GPU memory: 0 B (0 materials)"),
		headers:        materialsColumnHeaders,
		sortField:      "GPU Memory",
		sortDescending: true,
	}
	view.buildTable()
	tableScroll := container.NewBorder(view.statsLabel, nil, nil, nil, view.table)
	view.content = tableScroll
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
	entries, totalBytes := report.CollectScanMaterialReport(rows)
	view.entries = entries
	view.applySort()
	view.statsLabel.SetText(fmt.Sprintf(
		"Engine GPU memory: %s   |   Materials: %d   |   Mismatched: %d",
		format.FormatSizeAuto64(totalBytes),
		len(view.entries),
		countMismatched(view.entries),
	))
	if view.table != nil {
		view.table.Refresh()
	}
}

func countMismatched(entries []report.ScanMaterialEntry) int {
	count := 0
	for _, entry := range entries {
		if entry.Mismatched {
			count++
		}
	}
	return count
}

func (view *materialsView) buildTable() {
	view.table = widget.NewTableWithHeaders(
		func() (int, int) {
			return len(view.entries), len(view.headers)
		},
		func() fyne.CanvasObject {
			return widget.NewLabel("")
		},
		func(id widget.TableCellID, object fyne.CanvasObject) {
			label, ok := object.(*widget.Label)
			if !ok {
				return
			}
			if id.Row < 0 || id.Row >= len(view.entries) || id.Col < 0 || id.Col >= len(view.headers) {
				label.SetText("")
				return
			}
			label.SetText(view.cellValue(view.entries[id.Row], view.headers[id.Col]))
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
				view.applySort()
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
	for index, header := range view.headers {
		if width, found := materialsColumnWidths[header]; found {
			view.table.SetColumnWidth(index, width)
		}
	}
}

func (view *materialsView) cellValue(entry report.ScanMaterialEntry, columnName string) string {
	switch columnName {
	case "Color":
		return formatMaterialSlot(entry.ColorAssetID, entry.ColorWidth, entry.ColorHeight)
	case "Normal":
		return formatMaterialSlot(entry.NormalAssetID, entry.NormalWidth, entry.NormalHeight)
	case "Effective Normal":
		if entry.EffectiveNormalWidth <= 0 || entry.EffectiveNormalHeight <= 0 {
			return "-"
		}
		if entry.EffectiveNormalWidth == entry.NormalWidth && entry.EffectiveNormalHeight == entry.NormalHeight {
			return format.FormatDimensions(entry.EffectiveNormalWidth, entry.EffectiveNormalHeight)
		}
		return fmt.Sprintf("%s ↑", format.FormatDimensions(entry.EffectiveNormalWidth, entry.EffectiveNormalHeight))
	case "Metalness":
		return formatMaterialSlot(entry.MetalnessAssetID, entry.MetalnessWidth, entry.MetalnessHeight)
	case "Roughness":
		return formatMaterialSlot(entry.RoughnessAssetID, entry.RoughnessWidth, entry.RoughnessHeight)
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

func formatMaterialSlot(assetID int64, width, height int) string {
	if assetID <= 0 && width <= 0 {
		return "-"
	}
	if assetID <= 0 {
		return format.FormatDimensions(width, height)
	}
	if width <= 0 || height <= 0 {
		return strconv.FormatInt(assetID, 10)
	}
	return fmt.Sprintf("%d (%s)", assetID, format.FormatDimensions(width, height))
}

func (view *materialsView) applySort() {
	sort.SliceStable(view.entries, func(i, j int) bool {
		left := view.entries[i]
		right := view.entries[j]
		cmp := compareMaterialEntries(left, right, view.sortField)
		if cmp == 0 {
			return left.InstancePath < right.InstancePath
		}
		if view.sortDescending {
			return cmp > 0
		}
		return cmp < 0
	})
}

func compareMaterialEntries(left, right report.ScanMaterialEntry, field string) int {
	switch field {
	case "Color":
		return loader.CompareInt64(left.ColorAssetID, right.ColorAssetID)
	case "Normal":
		return loader.CompareInt64(left.NormalAssetID, right.NormalAssetID)
	case "Effective Normal":
		return loader.CompareInt(left.EffectiveNormalWidth*left.EffectiveNormalHeight,
			right.EffectiveNormalWidth*right.EffectiveNormalHeight)
	case "Metalness":
		return loader.CompareInt64(left.MetalnessAssetID, right.MetalnessAssetID)
	case "Roughness":
		return loader.CompareInt64(left.RoughnessAssetID, right.RoughnessAssetID)
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
