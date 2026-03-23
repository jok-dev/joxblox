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
		referenceInstanceType := ""
		referencePropertyName := ""
		referenceInstancePath := ""
		if explorerState != nil {
			if selectedRow, found := explorerState.getRow(selectedAssetID); found {
				referenceInstanceType = selectedRow.InstanceType
				referencePropertyName = selectedRow.PropertyName
				referenceInstancePath = explorerState.getInstancePath(selectedAssetID)
				if referenceInstancePath == "" {
					referenceInstancePath = selectedRow.InstancePath
				}
				if referenceInstancePath == "" {
					referenceInstancePath = selectedRow.InstanceName
				}
			}
		}
		downloadedSHA256 := ""
		if previewResult.Stats != nil && strings.TrimSpace(previewResult.Stats.SHA256) != "" {
			downloadedSHA256 = previewResult.Stats.SHA256
		} else if previewResult.Image != nil {
			downloadedSHA256 = previewResult.Image.SHA256
		}
		assetDetailsView.SetData(
			selectedAssetID,
			"",
			downloadedSHA256,
			0,
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
			referenceInstanceType,
			referencePropertyName,
			referenceInstancePath,
			previewResult.AssetTypeID,
			previewResult.AssetTypeName,
			previewResult.DownloadBytes,
			previewResult.DownloadFileName,
			previewResult.DownloadIsOriginal,
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
		inputRow,
		statusLabel,
		previewBox,
		assetDetailsView.HierarchySection,
		assetDetailsView.MetadataForm,
		assetDetailsView.JSONAccordion,
		assetDetailsView.NoteLabel,
	)

	return container.NewVScroll(tabContent)
}
