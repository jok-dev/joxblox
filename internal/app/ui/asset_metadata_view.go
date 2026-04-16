package ui

import (
	"joxblox/internal/app/loader"
	"joxblox/internal/roblox"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// metadataRow bundles the label + value widgets for one schema entry so the
// view layer can show/hide rows without reflowing the form.
type metadataRow struct {
	label     *widget.Label
	value     *widget.Label
	container fyne.CanvasObject
	spec      loader.MetadataSpec
}

// buildMetadataForm renders the metadata schema as a vertical form grouped by
// MetadataGroup with bold headers. Returns the form canvas and a map of rows
// keyed by spec.Key for fast updates.
//
// When includeFileRows is false, rows marked FileScoped are omitted entirely.
func buildMetadataForm(schema []loader.MetadataSpec, includeFileRows bool) (fyne.CanvasObject, map[string]*metadataRow) {
	rows := make(map[string]*metadataRow, len(schema))

	specsByGroup := make(map[loader.MetadataGroup][]loader.MetadataSpec)
	for _, spec := range schema {
		if spec.FileScoped && !includeFileRows {
			continue
		}
		specsByGroup[spec.Group] = append(specsByGroup[spec.Group], spec)
	}

	var formItems []fyne.CanvasObject
	firstGroup := true
	for _, group := range loader.MetadataGroupsInOrder() {
		specs := specsByGroup[group]
		if len(specs) == 0 {
			continue
		}
		if !firstGroup {
			formItems = append(formItems, widget.NewSeparator())
		}
		firstGroup = false
		header := widget.NewLabelWithStyle(group.Label(), fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
		formItems = append(formItems, header)
		for _, spec := range specs {
			row := newSchemaMetadataRow(spec)
			rows[spec.Key] = row
			formItems = append(formItems, row.container)
		}
	}
	return container.NewVBox(formItems...), rows
}

// newSchemaMetadataRow creates a row for one MetadataSpec. The row's container
// is hidden by default; updateMetadataRows will show it when there is a value.
func newSchemaMetadataRow(spec loader.MetadataSpec) *metadataRow {
	labelWidget := widget.NewLabel(spec.Label + ":")
	valueWidget := newMetadataValueLabel()
	labelSlot := container.NewGridWrap(
		fyne.NewSize(metadataLabelColumnWidth, metadataLabelRowHeight),
		labelWidget,
	)
	rowContainer := container.NewBorder(nil, nil, labelSlot, nil, valueWidget)
	rowContainer.Hide()
	return &metadataRow{
		label:     labelWidget,
		value:     valueWidget,
		container: rowContainer,
		spec:      spec,
	}
}

// updateMetadataRows applies the current AssetViewData to each row. Empty
// extract values hide the row; non-empty values set the text and show it.
// Widget identity is preserved across calls — no flicker.
func updateMetadataRows(rows map[string]*metadataRow, data loader.AssetViewData) {
	for _, row := range rows {
		if row == nil || row.spec.ViewExtract == nil {
			continue
		}
		text := row.spec.ViewExtract(data)
		labelText := row.spec.Label + ":"
		if row.spec.Key == "dimensions" && data.DimensionsLabel != "" {
			labelText = data.DimensionsLabel + ":"
		}
		row.label.SetText(labelText)
		if text == "" {
			row.container.Hide()
			continue
		}
		row.value.SetText(text)
		row.container.Show()
	}
}

// applySourceRowImportance highlights the "Image Source" row in red when the
// source description indicates a thumbnail fallback.
func applySourceRowImportance(rows map[string]*metadataRow, sourceDescription string) {
	row := rows["imagesource"]
	if row == nil || row.value == nil {
		return
	}
	if roblox.IsThumbnailFallback(sourceDescription) {
		row.value.Importance = widget.DangerImportance
	} else {
		row.value.Importance = widget.MediumImportance
	}
	row.value.Refresh()
}

// MetadataValueText returns the current display text for the metadata row
// identified by key (e.g. "ingamesize"). Returns "" if unknown or hidden.
func (view *AssetView) MetadataValueText(key string) string {
	row := view.metadataRows[key]
	if row == nil || row.value == nil {
		return ""
	}
	return row.value.Text
}
