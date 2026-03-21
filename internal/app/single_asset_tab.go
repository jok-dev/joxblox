package app

import (
	"fmt"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	fyneDialog "fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

func newSingleAssetTab(window fyne.Window) fyne.CanvasObject {
	assetInput := widget.NewEntry()
	assetInput.SetPlaceHolder("Paste Roblox asset ID (e.g. 138155379338302 or rbxassetid://138155379338302)")

	statusLabel := widget.NewLabel("Enter an asset ID and click Go.")
	assetDetailsView := newAssetView("No image loaded", false)
	loadingSpinner := widget.NewProgressBarInfinite()
	loadingSpinner.Hide()

	previewBox := container.NewMax(
		assetDetailsView.PreviewBox,
		container.NewVBox(loadingSpinner),
	)
	var explorerState *assetExplorerState
	var renderPreview func(selectedAssetID int64, previewResult *assetPreviewResult)
	renderPreview = func(selectedAssetID int64, previewResult *assetPreviewResult) {
		assetDetailsView.SetData(
			selectedAssetID,
			"",
			previewResult.Image,
			previewResult.Stats,
			previewResult.TotalBytesSize,
			previewResult.Source,
			previewResult.State,
			previewResult.WarningMessage,
			previewResult.AssetDeliveryJSON,
			previewResult.ThumbnailJSON,
			previewResult.EconomyJSON,
			previewResult.RustExtractorJSON,
			previewResult.ReferencedAssetIDs,
			previewResult.AssetTypeID,
			previewResult.AssetTypeName,
		)
		assetDetailsView.SetHierarchy(explorerState.getRows(), selectedAssetID, func(assetID int64) {
			if explorerState == nil {
				return
			}
			statusLabel.SetText(fmt.Sprintf("Loading asset %d...", assetID))
			loadingSpinner.Show()
			loadingSpinner.Start()
			go func() {
				selectedPreview, selectErr := explorerState.selectAsset(assetID)
				fyne.Do(func() {
					loadingSpinner.Stop()
					loadingSpinner.Hide()
					if selectErr != nil {
						statusLabel.SetText(selectErr.Error())
						return
					}
					renderPreview(assetID, selectedPreview)
					statusLabel.SetText(fmt.Sprintf("Showing asset %d.", assetID))
					previewBox.Refresh()
				})
			}()
		})
	}

	var goButton *widget.Button
	loadAsset := func() {
		if goButton != nil && goButton.Disabled() {
			return
		}

		assetID, err := parseAssetID(assetInput.Text)
		if err != nil {
			statusLabel.SetText(err.Error())
			return
		}
		logDebugf("Single asset load started for asset %d", assetID)

		statusLabel.SetText("Loading image...")
		goButton.Disable()
		loadingSpinner.Show()
		loadingSpinner.Start()
		assetDetailsView.PreviewPlaceholder.SetText("Loading image...")
		assetDetailsView.Clear()
		assetDetailsView.PreviewPlaceholder.SetText("Loading image...")
		assetDetailsView.PreviewPlaceholder.Show()
		previewBox.Refresh()

		go func(selectedAssetID int64) {
			previewResult, loadErr := loadAssetPreview(selectedAssetID)
			fyne.Do(func() {
				loadingSpinner.Stop()
				loadingSpinner.Hide()
				goButton.Enable()
				if loadErr != nil {
					statusLabel.SetText(loadErr.Error())
					logDebugf("Single asset load failed for %d: %s", selectedAssetID, loadErr.Error())
					fyneDialog.ShowError(loadErr, window)
					return
				}

				explorerState = newAssetExplorerState(selectedAssetID, previewResult)
				renderPreview(selectedAssetID, previewResult)
				previewBox.Refresh()
				if strings.EqualFold(previewResult.Source, sourceAssetDeliveryInGame) {
					statusLabel.SetText("Image loaded.")
					logDebugf("Single asset load complete for %d (AssetDelivery)", selectedAssetID)
				} else {
					statusLabel.SetText(fmt.Sprintf("Loaded fallback thumbnail (state: %s).", previewResult.State))
					logDebugf("Single asset load complete for %d (fallback thumbnail, state=%s)", selectedAssetID, previewResult.State)
				}
			})
		}(assetID)
	}

	goButton = widget.NewButton("Go", loadAsset)
	assetInput.OnSubmitted = func(_ string) {
		loadAsset()
	}

	inputRow := container.NewBorder(nil, nil, nil, goButton, assetInput)

	tabContent := container.NewVBox(
		widget.NewLabel("Roblox Asset Image Preview"),
		inputRow,
		statusLabel,
		assetDetailsView.HierarchySection,
		previewBox,
		assetDetailsView.MetadataForm,
		assetDetailsView.JSONAccordion,
		assetDetailsView.NoteLabel,
	)

	return container.NewVScroll(tabContent)
}
