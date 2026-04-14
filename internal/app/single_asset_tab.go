package app

import (
	"fmt"
	"strings"

	"joxblox/internal/app/loader"
	"joxblox/internal/app/ui"
	"joxblox/internal/debug"
	"joxblox/internal/heatmap"
	"joxblox/internal/roblox"
	"joxblox/internal/roblox/mesh"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	fyneDialog "fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

func newSingleAssetTab(window fyne.Window) fyne.CanvasObject {
	assetInput := widget.NewEntry()
	assetInput.SetPlaceHolder("Paste an asset ID, rbxassetid URL, or rbxthumb URL")

	statusLabel := widget.NewLabel("Enter an asset ID or rbxthumb URL and click Go.")
	assetDetailsView := ui.NewAssetView("No image loaded", false)
	loadingSpinner := widget.NewProgressBarInfinite()
	loadingSpinner.Hide()

	previewBox := container.NewMax(
		assetDetailsView.PreviewBox,
		container.NewVBox(loadingSpinner),
	)
	var explorerState *ui.AssetExplorerState
	var renderPreview func(selectedAssetID int64, previewResult *loader.AssetPreviewResult)
	renderPreview = func(selectedAssetID int64, previewResult *loader.AssetPreviewResult) {
		context := ui.BuildExplorerSelectionReferenceContext(explorerState, selectedAssetID)
		assetDetailsView.SetData(loader.BuildAssetViewDataFromPreview(selectedAssetID, previewResult, context))
		assetDetailsView.SetHierarchy(explorerState.GetRows(), selectedAssetID, func(assetID int64) {
			if explorerState == nil {
				return
			}
			statusLabel.SetText(fmt.Sprintf("Loading asset %d...", assetID))
			loadingSpinner.Show()
			loadingSpinner.Start()
			go func() {
				selectedPreview, selectErr, requestSource := explorerState.SelectAssetWithRequestSource(assetID)
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
						heatmap.FormatSingleRequestSourceBreakdown(requestSource),
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

		loadRequest, err := loader.ParseSingleAssetLoadRequest(assetInput.Text)
		if err != nil {
			statusLabel.SetText(err.Error())
			return
		}
		selectedTargetID := loadRequest.TargetID
		debug.Logf("Single asset load started for %s", loadRequest.LogDescription())

		statusLabel.SetText("Loading image...")
		goButton.Disable()
		loadingSpinner.Show()
		loadingSpinner.Start()
		assetDetailsView.PreviewPlaceholder.SetText("Loading image...")
		assetDetailsView.Clear()
		assetDetailsView.PreviewPlaceholder.SetText("Loading image...")
		assetDetailsView.PreviewPlaceholder.Show()
		previewBox.Refresh()

		go func(selectedAssetID int64, request loader.SingleAssetLoadRequest) {
			if request.RequiresAuth() {
				authErr := roblox.ValidateCurrentAuthCookie()
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

			trace := &loader.AssetRequestTrace{}
			previewResult, loadErr := loader.LoadSingleAssetPreviewWithTrace(request, trace)
			fyne.Do(func() {
				loadingSpinner.Stop()
				loadingSpinner.Hide()
				goButton.Enable()
				if loadErr != nil {
					statusLabel.SetText(loadErr.Error())
					debug.Logf("Single asset load failed for %s: %s", request.LogDescription(), loadErr.Error())
					fyneDialog.ShowError(loadErr, window)
					return
				}

				explorerState = ui.NewAssetExplorerState(selectedAssetID, previewResult)
				renderPreview(selectedAssetID, previewResult)
				previewBox.Refresh()
				requestSourceBreakdown := heatmap.FormatSingleRequestSourceBreakdown(trace.ClassifyRequestSource())
				if mesh.IsMeshAssetType(previewResult.AssetTypeID) && len(previewResult.DownloadBytes) > 0 {
					if strings.EqualFold(previewResult.Source, roblox.SourceAssetDeliveryInGame) {
						statusLabel.SetText(fmt.Sprintf("Mesh loaded. %s", requestSourceBreakdown))
						debug.Logf("Single asset load complete for %d (mesh via AssetDelivery)", selectedAssetID)
					} else {
						statusLabel.SetText(fmt.Sprintf("Mesh loaded with thumbnail metadata fallback (state: %s). %s", previewResult.State, requestSourceBreakdown))
						debug.Logf("Single asset load complete for %d (mesh with thumbnail fallback, state=%s)", selectedAssetID, previewResult.State)
					}
				} else if strings.EqualFold(previewResult.Source, roblox.SourceAssetDeliveryInGame) {
					statusLabel.SetText(fmt.Sprintf("Image loaded. %s", requestSourceBreakdown))
					debug.Logf("Single asset load complete for %d (AssetDelivery)", selectedAssetID)
				} else if strings.EqualFold(previewResult.Source, roblox.SourceThumbnailsDirect) {
					statusLabel.SetText(fmt.Sprintf("Thumbnail loaded (state: %s). %s", previewResult.State, requestSourceBreakdown))
					debug.Logf("Single asset load complete for %d (direct thumbnail, state=%s)", selectedAssetID, previewResult.State)
				} else {
					statusLabel.SetText(fmt.Sprintf("Loaded fallback thumbnail (state: %s). %s", previewResult.State, requestSourceBreakdown))
					debug.Logf("Single asset load complete for %d (fallback thumbnail, state=%s)", selectedAssetID, previewResult.State)
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
