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
	assetInput.SetPlaceHolder("Paste an asset ID, rbxassetid URL, or rbxthumb URL")

	statusLabel := widget.NewLabel("Enter an asset ID or rbxthumb URL and click Go.")
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
		context := buildExplorerSelectionReferenceContext(explorerState, selectedAssetID)
		assetDetailsView.SetData(buildAssetViewDataFromPreview(selectedAssetID, previewResult, context))
		assetDetailsView.SetHierarchy(explorerState.getRows(), selectedAssetID, func(assetID int64) {
			if explorerState == nil {
				return
			}
			statusLabel.SetText(fmt.Sprintf("Loading asset %d...", assetID))
			loadingSpinner.Show()
			loadingSpinner.Start()
			go func() {
				selectedPreview, selectErr, requestSource := explorerState.selectAssetWithRequestSource(assetID)
				fyne.Do(func() {
					loadingSpinner.Stop()
					loadingSpinner.Hide()
					if selectErr != nil {
						statusLabel.SetText(selectErr.Error())
						return
					}
					renderPreview(assetID, selectedPreview)
					statusLabel.SetText(fmt.Sprintf(
						"Showing asset %d. %s",
						assetID,
						formatSingleRequestSourceBreakdown(requestSource),
					))
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

		loadRequest, err := parseSingleAssetLoadRequest(assetInput.Text)
		if err != nil {
			statusLabel.SetText(err.Error())
			return
		}
		selectedTargetID := loadRequest.TargetID
		logDebugf("Single asset load started for %s", loadRequest.logDescription())

		statusLabel.SetText("Loading image...")
		goButton.Disable()
		loadingSpinner.Show()
		loadingSpinner.Start()
		assetDetailsView.PreviewPlaceholder.SetText("Loading image...")
		assetDetailsView.Clear()
		assetDetailsView.PreviewPlaceholder.SetText("Loading image...")
		assetDetailsView.PreviewPlaceholder.Show()
		previewBox.Refresh()

		go func(selectedAssetID int64, request singleAssetLoadRequest) {
			if request.requiresAuth() {
				authErr := validateCurrentAuthCookie()
				if authErr != nil {
					fyne.Do(func() {
						loadingSpinner.Stop()
						loadingSpinner.Hide()
						goButton.Enable()
						statusLabel.SetText("Auth cookie expired or invalid")
						fyneDialog.ShowError(authErr, window)
					})
					return
				}
			}

			trace := &assetRequestTrace{}
			previewResult, loadErr := loadSingleAssetPreviewWithTrace(request, trace)
			fyne.Do(func() {
				loadingSpinner.Stop()
				loadingSpinner.Hide()
				goButton.Enable()
				if loadErr != nil {
					statusLabel.SetText(loadErr.Error())
					logDebugf("Single asset load failed for %s: %s", request.logDescription(), loadErr.Error())
					fyneDialog.ShowError(loadErr, window)
					return
				}

				explorerState = newAssetExplorerState(selectedAssetID, previewResult)
				renderPreview(selectedAssetID, previewResult)
				previewBox.Refresh()
				requestSourceBreakdown := formatSingleRequestSourceBreakdown(trace.classifyRequestSource())
				if isMeshAssetType(previewResult.AssetTypeID) && len(previewResult.DownloadBytes) > 0 {
					if strings.EqualFold(previewResult.Source, sourceAssetDeliveryInGame) {
						statusLabel.SetText(fmt.Sprintf("Mesh loaded. %s", requestSourceBreakdown))
						logDebugf("Single asset load complete for %d (mesh via AssetDelivery)", selectedAssetID)
					} else {
						statusLabel.SetText(fmt.Sprintf("Mesh loaded with thumbnail metadata fallback (state: %s). %s", previewResult.State, requestSourceBreakdown))
						logDebugf("Single asset load complete for %d (mesh with thumbnail fallback, state=%s)", selectedAssetID, previewResult.State)
					}
				} else if strings.EqualFold(previewResult.Source, sourceAssetDeliveryInGame) {
					statusLabel.SetText(fmt.Sprintf("Image loaded. %s", requestSourceBreakdown))
					logDebugf("Single asset load complete for %d (AssetDelivery)", selectedAssetID)
				} else if strings.EqualFold(previewResult.Source, sourceThumbnailsDirect) {
					statusLabel.SetText(fmt.Sprintf("Thumbnail loaded (state: %s). %s", previewResult.State, requestSourceBreakdown))
					logDebugf("Single asset load complete for %d (direct thumbnail, state=%s)", selectedAssetID, previewResult.State)
				} else {
					statusLabel.SetText(fmt.Sprintf("Loaded fallback thumbnail (state: %s). %s", previewResult.State, requestSourceBreakdown))
					logDebugf("Single asset load complete for %d (fallback thumbnail, state=%s)", selectedAssetID, previewResult.State)
				}
			})
		}(selectedTargetID, loadRequest)
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
