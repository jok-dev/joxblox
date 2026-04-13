package app

import (
	"fmt"
	"image/color"

	"joxblox/internal/format"
	"joxblox/internal/roblox"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

func (view *assetView) SetHierarchy(rows []assetExplorerRow, selectedAssetID int64, selectAsset func(int64)) {
	view.hierarchyRows = rows
	view.hierarchySelectAsset = selectAsset
	view.selectedHierarchyAssetID = selectedAssetID

	hierarchyItems := make([]fyne.CanvasObject, 0, len(rows))
	for _, row := range rows {
		rowCopy := row
		sizeText := "size unavailable"
		if row.SelfBytesSize > 0 {
			sizeText = format.FormatSizeAuto(row.SelfBytesSize)
		}
		nodeIcon := roblox.GetAssetTypeEmoji(row.AssetTypeID)
		rowText := fmt.Sprintf("%s %d (%s)", nodeIcon, row.AssetID, sizeText)
		rowButton := widget.NewButton(rowText, func() {
			if view.hierarchySelectAsset != nil {
				view.hierarchySelectAsset(rowCopy.AssetID)
			}
		})
		rowButton.Importance = widget.LowImportance
		if row.AssetID == selectedAssetID {
			rowButton.Importance = widget.HighImportance
		}
		indentSpacer := canvas.NewRectangle(color.Transparent)
		indentSpacer.SetMinSize(fyne.NewSize(float32(row.Depth*24), 1))
		hierarchyItems = append(hierarchyItems, container.NewHBox(indentSpacer, rowButton))
	}

	view.hierarchyList.Objects = hierarchyItems
	view.hierarchyList.Refresh()
}
