// Package lodviewer provides a tab for loading a Roblox mesh asset by ID and
// exploring each of its LODs (triangle counts + 3D preview).
package lodviewer

import (
	"fmt"
	"strings"

	"joxblox/internal/app/loader"
	"joxblox/internal/app/ui"
	"joxblox/internal/debug"
	"joxblox/internal/extractor"
	"joxblox/internal/format"
	"joxblox/internal/roblox"
	"joxblox/internal/roblox/mesh"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	fyneDialog "fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

// NewLodViewerTab builds the LOD Viewer tab: paste an asset ID (including
// rbxassetid:// and rbxthumb:// URIs), load the mesh, and inspect each LOD
// with a live 3D preview and triangle counts.
func NewLodViewerTab(window fyne.Window) fyne.CanvasObject {
	assetInput := widget.NewEntry()
	assetInput.SetPlaceHolder("Paste a mesh asset ID, rbxassetid URL, or rbxthumb URL")

	statusLabel := widget.NewLabel("Enter a mesh asset ID and click Load.")
	summaryLabel := widget.NewLabel("")
	summaryLabel.Wrapping = fyne.TextWrapWord

	controlsLabel := widget.NewLabel(ui.MeshPreviewControlsText())
	controlsLabel.Wrapping = fyne.TextWrapWord

	previewContainer, previewWidget := ui.NewMeshPreviewWithToolbar()
	previewWidget.SetFocusCanvas(window.Canvas())

	loadingSpinner := widget.NewProgressBarInfinite()
	loadingSpinner.Hide()

	// LOD selector is a radio group rebuilt on each successful load.
	lodSelector := widget.NewRadioGroup(nil, nil)
	lodSelector.Horizontal = false
	lodSelector.Hide()

	// Track the most recent preview payload so LOD switches don't have to
	// re-download or re-decode the mesh.
	var currentPreview *extractor.MeshPreviewRawResult

	applyLodByIndex := func(lodIndex int) {
		if currentPreview == nil {
			return
		}
		lods := currentPreview.Lods
		if lodIndex < 0 || lodIndex >= len(lods) {
			return
		}
		data, buildErr := ui.BuildMeshPreviewDataForLodRange(
			currentPreview.Positions,
			currentPreview.Indices,
			lods[lodIndex].TriangleStart,
			lods[lodIndex].TriangleEnd,
		)
		if buildErr != nil {
			statusLabel.SetText(fmt.Sprintf("LOD render failed: %s", buildErr.Error()))
			previewWidget.Clear()
			return
		}
		previewWidget.SetData(data)
	}

	lodSelector.OnChanged = func(selected string) {
		idx := indexOfLodOptionLabel(lodSelector.Options, selected)
		if idx < 0 {
			return
		}
		applyLodByIndex(idx)
	}

	resetDisplay := func() {
		currentPreview = nil
		summaryLabel.SetText("")
		lodSelector.Options = nil
		lodSelector.Selected = ""
		lodSelector.Hide()
		lodSelector.Refresh()
		previewWidget.Clear()
	}

	var loadButton *widget.Button
	loadAsset := func() {
		if loadButton != nil && loadButton.Disabled() {
			return
		}

		loadRequest, parseErr := loader.ParseSingleAssetLoadRequest(assetInput.Text)
		if parseErr != nil {
			statusLabel.SetText(parseErr.Error())
			return
		}
		selectedTargetID := loadRequest.TargetID
		debug.Logf("LOD viewer load started for asset %d (%s)", selectedTargetID, loadRequest.LogDescription())

		statusLabel.SetText(fmt.Sprintf("Loading mesh %d...", selectedTargetID))
		resetDisplay()
		loadButton.Disable()
		loadingSpinner.Show()
		loadingSpinner.Start()

		go func(targetID int64) {
			if loadRequest.RequiresAuth() {
				if authErr := roblox.ValidateCurrentAuthCookie(); authErr != nil {
					fyne.Do(func() {
						loadingSpinner.Stop()
						loadingSpinner.Hide()
						loadButton.Enable()
						statusLabel.SetText("Auth cookie expired or invalid")
						fyneDialog.ShowError(authErr, window)
					})
					return
				}
			}

			// Always fetch the mesh asset bytes via AssetDelivery, even for
			// rbxthumb inputs (we only use rbxthumb for its target id — the
			// thumbnail image itself isn't useful for LOD inspection).
			trace := &loader.AssetRequestTrace{}
			previewResult, loadErr := loader.LoadBestImageInfoWithOptionsAndTrace(targetID, false, trace)
			if loadErr != nil {
				fyne.Do(func() {
					loadingSpinner.Stop()
					loadingSpinner.Hide()
					loadButton.Enable()
					statusLabel.SetText(loadErr.Error())
					debug.Logf("LOD viewer load failed for %d: %s", targetID, loadErr.Error())
					fyneDialog.ShowError(loadErr, window)
				})
				return
			}
			if previewResult == nil || len(previewResult.DownloadBytes) == 0 {
				fyne.Do(func() {
					loadingSpinner.Stop()
					loadingSpinner.Hide()
					loadButton.Enable()
					statusLabel.SetText(fmt.Sprintf("Asset %d has no downloadable bytes.", targetID))
				})
				return
			}
			// If AssetDelivery classifies the asset as something non-mesh,
			// warn up front. Still attempt preview extraction — the Rust
			// parser will return a clear error for non-mesh payloads like
			// .rbxm models, audio or textures.
			isNonMeshKnownType := previewResult.AssetTypeID > 0 && !mesh.IsMeshAssetType(previewResult.AssetTypeID)

			rawPreview, extractErr := extractor.ExtractMeshPreviewRawFromBytesFull(previewResult.DownloadBytes)
			if extractErr != nil {
				fyne.Do(func() {
					loadingSpinner.Stop()
					loadingSpinner.Hide()
					loadButton.Enable()
					typeName := strings.TrimSpace(previewResult.AssetTypeName)
					if typeName == "" {
						typeName = "Unknown"
					}
					if isNonMeshKnownType {
						statusLabel.SetText(fmt.Sprintf("Asset %d is %s, not a Mesh. LOD Viewer only supports mesh assets.", targetID, typeName))
					} else {
						statusLabel.SetText(fmt.Sprintf("Mesh preview failed: %s", extractErr.Error()))
					}
					debug.Logf("LOD viewer preview failed for %d: %s", targetID, extractErr.Error())
				})
				return
			}

			lods := rawPreview.Lods
			if len(lods) == 0 {
				lods = []extractor.MeshLodRangeInfo{{TriangleStart: 0, TriangleEnd: rawPreview.TriangleCount}}
			}
			rawPreview.Lods = lods

			lodOptions := buildLodOptionLabels(lods, rawPreview.TriangleCount)

			fyne.Do(func() {
				loadingSpinner.Stop()
				loadingSpinner.Hide()
				loadButton.Enable()
				currentPreview = &rawPreview
				lodSelector.Options = lodOptions
				lodSelector.Refresh()
				lodSelector.Show()
				summaryLabel.SetText(buildMeshSummary(targetID, previewResult.AssetTypeName, rawPreview))
				statusLabel.SetText(fmt.Sprintf("Loaded mesh %d (%d LODs).", targetID, len(lods)))
				// Select LOD 0 by default to show the highest-detail mesh.
				lodSelector.SetSelected(lodOptions[0])
			})
		}(selectedTargetID)
	}

	loadButton = widget.NewButton("Load", loadAsset)
	assetInput.OnSubmitted = func(_ string) {
		loadAsset()
	}

	inputRow := container.NewBorder(nil, nil, nil, loadButton, assetInput)

	header := container.NewVBox(
		inputRow,
		loadingSpinner,
		statusLabel,
		summaryLabel,
	)
	footer := controlsLabel

	// The LOD selector sits to the right of the preview so the 3D widget
	// keeps as much horizontal room as possible. Wrap the selector in a
	// fixed-width grid so it doesn't collapse to the minimum radio width or
	// steal space from the preview as LOD counts change.
	lodSelectorPanel := container.NewVBox(
		widget.NewLabelWithStyle("LODs", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		lodSelector,
	)
	lodSelectorColumn := container.NewGridWrap(fyne.NewSize(240, ui.PreviewHeight), lodSelectorPanel)

	previewRow := container.NewBorder(
		nil,               // top
		nil,               // bottom
		nil,               // left
		lodSelectorColumn, // right
		container.NewMax(previewContainer),
	)

	// VScroll so the mesh preview's 440x300 MinSize + footer controls don't
	// pin the main window's minimum height.
	return container.NewVScroll(container.NewBorder(
		header,
		footer,
		nil,
		nil,
		previewRow,
	))
}

// buildLodOptionLabels produces one radio option per LOD entry. Each label
// encodes both the LOD index (for picking) and a human-readable triangle
// count. The triangle count in the summary always reflects the mesh file's
// reported per-LOD ranges (not the preview cap) because the LOD viewer
// always requests an unlimited preview.
func buildLodOptionLabels(lods []extractor.MeshLodRangeInfo, totalTriangles uint32) []string {
	if len(lods) == 0 {
		return nil
	}
	highestDetail := uint32(0)
	for _, lod := range lods {
		if lod.TriangleEnd > lod.TriangleStart {
			count := lod.TriangleEnd - lod.TriangleStart
			if count > highestDetail {
				highestDetail = count
			}
		}
	}
	labels := make([]string, len(lods))
	for i, lod := range lods {
		triangleCount := uint32(0)
		if lod.TriangleEnd > lod.TriangleStart {
			triangleCount = lod.TriangleEnd - lod.TriangleStart
		}
		percent := ""
		if highestDetail > 0 {
			percent = fmt.Sprintf(" · %d%% of LOD 0", int(100*uint64(triangleCount)/uint64(highestDetail)))
		}
		labels[i] = fmt.Sprintf("LOD %d · %s tris%s", i, format.FormatIntCommas(int64(triangleCount)), percent)
	}
	return labels
}

func indexOfLodOptionLabel(labels []string, target string) int {
	for i, label := range labels {
		if label == target {
			return i
		}
	}
	return -1
}

// buildMeshSummary produces a human-readable header that summarises the
// overall mesh before the per-LOD breakdown is shown in the selector.
func buildMeshSummary(assetID int64, assetTypeName string, preview extractor.MeshPreviewRawResult) string {
	parts := []string{
		fmt.Sprintf("Asset %d", assetID),
	}
	if strings.TrimSpace(assetTypeName) != "" {
		parts = append(parts, assetTypeName)
	}
	parts = append(parts, fmt.Sprintf("Format v%s", strings.TrimSpace(preview.FormatVersion)))
	parts = append(parts, fmt.Sprintf("%s tris total", format.FormatIntCommas(int64(preview.TriangleCount))))
	parts = append(parts, fmt.Sprintf("%s verts", format.FormatIntCommas(int64(preview.VertexCount))))

	lodSum := uint32(0)
	for _, lod := range preview.Lods {
		if lod.TriangleEnd > lod.TriangleStart {
			lodSum += lod.TriangleEnd - lod.TriangleStart
		}
	}
	note := ""
	if len(preview.Lods) > 0 && lodSum != preview.TriangleCount {
		diff := int64(preview.TriangleCount) - int64(lodSum)
		note = fmt.Sprintf("  (LOD ranges cover %s tris; %s tris outside any LOD bucket — likely collision/shadow data.)",
			format.FormatIntCommas(int64(lodSum)),
			format.FormatIntCommas(diff),
		)
	}
	return strings.Join(parts, " · ") + note
}
