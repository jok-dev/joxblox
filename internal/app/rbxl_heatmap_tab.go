package app

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg"
	"image/png"
	"math"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	nativeDialog "github.com/sqweek/dialog"
	xdraw "golang.org/x/image/draw"
)

const (
	rbxlHeatmapImageSize            = 4096
	rbxlHeatmapPadding              = 72
	rbxlHeatmapDefaultBgColor       = expandedBackgroundBlack
	rbxlHeatmapDefaultOpacity       = 0.72
	rbxlHeatmapDefaultSpread        = 1.0
	rbxlHeatmapMinSpread            = 0.1
	rbxlHeatmapMaxSpread            = 5.0
	rbxlHeatmapSpreadStep           = 0.05
	rbxlHeatmapGeneratedMapAlpha    = 0.88
	rbxlHeatmapManualUnderlayAlpha  = 0.84
	rbxlHeatmapShadowPixelScale     = 0.08
	rbxlHeatmapDefaultGridDivisions = 36
	rbxlHeatmapMinGridDivisions     = 4
	rbxlHeatmapMaxGridDivisions     = 256
	rbxlHeatmapGridDivisionStep     = 4
)

type rbxlHeatmapAssetStats struct {
	AssetID       int64
	AssetTypeID   int
	AssetTypeName string
	TotalBytes    int
	TextureBytes  int
	MeshBytes     int
	TriangleCount uint32
	PixelCount    int64
}

type heatmapAssetReference struct {
	AssetID    int64
	AssetInput string
}

type rbxlHeatmapPoint struct {
	AssetID      int64
	AssetInput   string
	InstanceType string
	InstanceName string
	InstancePath string
	PropertyName string
	Stats        rbxlHeatmapAssetStats
	X            float64
	Y            float64
	Z            float64
}

type rbxlHeatmapMapPart struct {
	InstanceType string
	InstanceName string
	InstancePath string
	MaterialKey  string
	CenterX      float64
	CenterY      float64
	CenterZ      float64
	SizeX        float64
	SizeY        float64
	SizeZ        float64
	YawDegrees   float64
	Color        color.NRGBA
	Transparency float64
}

type rbxlHeatmapScene struct {
	Points         []rbxlHeatmapPoint
	ComparePoints  []rbxlHeatmapPoint
	MapParts       []rbxlHeatmapMapPart
	Cells          []rbxlHeatmapCell
	CellIndexByKey map[string]int
	CellSizeWorld  float64
	ColumnCount    int
	RowCount       int
	GridDivisions  int
	MinimumX       float64
	MaximumX       float64
	MinimumZ       float64
	MaximumZ       float64
	MinimumY       float64
	MaximumY       float64
	MaxCellBytes   int64
	DiffMode       bool
	DiffLabel      string
}

type rbxlHeatmapBuildResult struct {
	Scene                       *rbxlHeatmapScene
	PointCount                  int
	MapPartCount                int
	UniqueAssetCount            int
	MissingPositionCount        int
	MissingSizeCount            int
	TotalWeightedBytes          int64
	ComparePointCount           int
	CompareMapPartCount         int
	CompareUniqueAssetCount     int
	CompareMissingPositionCount int
	CompareMissingSizeCount     int
	CompareTotalWeightedBytes   int64
	DiffMode                    bool
}

type heatmapPixelPoint struct {
	X float64
	Y float64
}

type rbxlHeatMetric string

type heatMetricMaximums struct {
	TotalBytes         float64
	TextureBytes       float64
	TexturePixels      float64
	MeshBytes          float64
	MeshTriangles      float64
	UniqueTextureCount float64
	UniqueMeshCount    float64
}

type heatmapGradientStop struct {
	position float64
	color    color.NRGBA
}

const (
	heatMetricTotalBytes         rbxlHeatMetric = "Total Byte Size"
	heatMetricTextureBytes       rbxlHeatMetric = "Texture Bytes"
	heatMetricTexturePixels      rbxlHeatMetric = "Texture Pixels"
	heatMetricMeshBytes          rbxlHeatMetric = "Mesh Bytes"
	heatMetricMeshTriangles      rbxlHeatMetric = "Mesh Triangles"
	heatMetricUniqueTextureCount rbxlHeatMetric = "Unique Texture Count"
	heatMetricUniqueMeshCount    rbxlHeatMetric = "Unique Mesh Count"
	heatMetricUniqueAssetCount   rbxlHeatMetric = "Unique Assets"
	heatMetricMeshPartCount      rbxlHeatMetric = "MeshParts"
	heatMetricPartCount          rbxlHeatMetric = "Parts"
)

type rbxlHeatmapCell struct {
	Row        int
	Column     int
	Stats      rbxlHeatmapTotals
	BaseStats  rbxlHeatmapTotals
	DeltaStats rbxlHeatmapTotals
	MinimumX   float64
	MaximumX   float64
	MinimumZ   float64
	MaximumZ   float64
}

type rbxlHeatmapTotals struct {
	ReferenceCount     int64
	UniqueAssetCount   int64
	UniqueTextureCount int64
	UniqueMeshCount    int64
	TextureBytes       int64
	MeshBytes          int64
	TotalBytes         int64
	TriangleCount      int64
	PixelCount         int64
	MeshPartCount      int64
	PartCount          int64
	DrawCallCount      int64
}

type heatmapSquareAssetRow struct {
	Side               string
	AssetID            int64
	UseCount           int
	AssetTypeID        int
	AssetTypeName      string
	AssetInput         string
	FilePath           string
	InstanceType       string
	InstanceName       string
	InstancePath       string
	PropertyName       string
	TotalBytes         int64
	TextureBytes       int64
	MeshBytes          int64
	Triangles          int64
	Pixels             int64
	WorldX             float64
	WorldY             float64
	WorldZ             float64
	SceneSurfaceArea   float64
	LargestSurfacePath string
}

var heatmapAssetStatsCache = struct {
	mutex        sync.RWMutex
	statsByAsset map[string]rbxlHeatmapAssetStats
}{
	statsByAsset: map[string]rbxlHeatmapAssetStats{},
}

func newRBXLHeatmapTab(window fyne.Window) fyne.CanvasObject {
	selectedFilePath := ""
	selectedCompareFilePath := ""
	selectedMapImagePath := ""
	mapImageBytes := []byte(nil)
	pathFilterEnabled := false
	pathFilterText := "Workspace.*"
	heatOpacity := rbxlHeatmapDefaultOpacity
	heatSpread := rbxlHeatmapDefaultSpread
	gridDivisions := rbxlHeatmapDefaultGridDivisions
	showGrid := true
	heatMetric := heatMetricTotalBytes
	diffMode := false
	currentOption := blankHeatmapPreviewOption()
	heatmapReady := false
	var currentScene *rbxlHeatmapScene
	var renderToken atomic.Uint64
	var loading atomic.Bool

	filePathLabel := widget.NewLabel("No .rbxl/.rbxm file selected.")
	filePathLabel.Wrapping = fyne.TextTruncate
	compareFilePathLabel := widget.NewLabel("No comparison .rbxl/.rbxm file selected.")
	compareFilePathLabel.Wrapping = fyne.TextTruncate
	warningBanner := newMaterialVariantWarningBanner(window)
	statusLabel := widget.NewLabel("Select an .rbxl or .rbxm file and build a heatmap.")
	statusLabel.Wrapping = fyne.TextWrapWord
	summaryLabel := widget.NewLabel("No heatmap built.")
	summaryLabel.Wrapping = fyne.TextWrapWord
	legendLabel := widget.NewLabel(heatmapLegendText(false))
	legendLabel.Wrapping = fyne.TextWrapWord
	setWarning := func(warningData materialVariantWarningData) {
		warningBanner.SetWarning(warningData)
	}
	mapImageLabel := widget.NewLabel("Using auto-generated map underlay.")
	mapImageLabel.Wrapping = fyne.TextTruncate
	opacityValueLabel := widget.NewLabel("72%")
	spreadValueLabel := widget.NewLabel(fmt.Sprintf("%.2fx", heatSpread))
	gridDensityValueLabel := widget.NewLabel(fmt.Sprintf("%d", gridDivisions))
	metricSelect := widget.NewSelect([]string{
		string(heatMetricTotalBytes),
		string(heatMetricTextureBytes),
		string(heatMetricTexturePixels),
		string(heatMetricMeshBytes),
		string(heatMetricMeshTriangles),
		string(heatMetricUniqueTextureCount),
		string(heatMetricUniqueMeshCount),
		string(heatMetricUniqueAssetCount),
		string(heatMetricMeshPartCount),
		string(heatMetricPartCount),
	}, nil)
	metricSelect.SetSelected(string(heatMetric))
	opacitySlider := widget.NewSlider(0.1, 1.0)
	opacitySlider.Step = 0.05
	opacitySlider.SetValue(heatOpacity)
	spreadSlider := widget.NewSlider(rbxlHeatmapMinSpread, rbxlHeatmapMaxSpread)
	spreadSlider.Step = rbxlHeatmapSpreadStep
	spreadSlider.SetValue(heatSpread)
	gridDensitySlider := widget.NewSlider(rbxlHeatmapMinGridDivisions, rbxlHeatmapMaxGridDivisions)
	gridDensitySlider.Step = rbxlHeatmapGridDivisionStep
	gridDensitySlider.SetValue(float64(gridDivisions))
	showGridCheck := widget.NewCheck("Show Grid", nil)
	showGridCheck.SetChecked(showGrid)

	pathFilterEntry := widget.NewMultiLineEntry()
	pathFilterEntry.SetText(pathFilterText)
	pathFilterEntry.SetPlaceHolder("One path filter per line, for example Workspace.*")
	pathFilterEntry.SetMinRowsVisible(3)
	pathFilterEntry.Wrapping = fyne.TextWrapOff
	pathFilterEntry.Disable()
	pathFilterEntry.OnChanged = func(text string) {
		pathFilterText = text
	}
	pathFilterCheck := widget.NewCheck("Path Filter", func(checked bool) {
		pathFilterEnabled = checked
		if checked {
			pathFilterEntry.Enable()
			return
		}
		pathFilterEntry.Disable()
	})
	var diffCompareSection *fyne.Container
	diffModeCheck := widget.NewCheck("Diff Against Another RBXL/RBXM", func(checked bool) {
		diffMode = checked
		if diffCompareSection != nil && checked {
			diffCompareSection.Show()
		} else if diffCompareSection != nil {
			diffCompareSection.Hide()
		}
		legendLabel.SetText(heatmapLegendText(checked))
	})

	progressBar := widget.NewProgressBarInfinite()
	progressBar.Hide()

	heatmapViewer := newZoomPanImage(currentOption)
	heatmapViewer.SetBackground(rbxlHeatmapDefaultBgColor)
	tooltipLabel := widget.NewLabel("")
	tooltipLabel.Wrapping = fyne.TextWrapOff
	tooltipLabel.Hide()
	tooltipBackground := canvas.NewRectangle(color.NRGBA{R: 22, G: 24, B: 30, A: 245})
	tooltipBackground.Hide()
	tooltipLayer := container.NewWithoutLayout(tooltipBackground, tooltipLabel)
	hideTooltip := func() {
		tooltipBackground.Hide()
		tooltipLabel.Hide()
		tooltipLayer.Refresh()
	}
	showTooltip := func(text string, pointer fyne.Position) {
		tooltipLabel.SetText(text)
		labelSize := tooltipLabel.MinSize()
		paddingX := float32(10)
		paddingY := float32(8)
		backgroundSize := fyne.NewSize(labelSize.Width+paddingX*2, labelSize.Height+paddingY*2)
		offsetX := float32(16)
		offsetY := float32(18)
		positionX := pointer.X + offsetX
		positionY := pointer.Y + offsetY
		viewerSize := heatmapViewer.Size()
		if viewerSize.Width > 0 && positionX+backgroundSize.Width > viewerSize.Width {
			positionX = pointer.X - backgroundSize.Width - 12
		}
		if viewerSize.Height > 0 && positionY+backgroundSize.Height > viewerSize.Height {
			positionY = pointer.Y - backgroundSize.Height - 12
		}
		if positionX < 0 {
			positionX = 0
		}
		if positionY < 0 {
			positionY = 0
		}
		tooltipBackground.Move(fyne.NewPos(positionX, positionY))
		tooltipBackground.Resize(backgroundSize)
		tooltipLabel.Move(fyne.NewPos(positionX+paddingX, positionY+paddingY))
		tooltipLabel.Resize(labelSize)
		tooltipBackground.Show()
		tooltipLabel.Show()
		tooltipLayer.Refresh()
	}
	heatmapViewer.SetHoverCallback(func(imageX float64, imageY float64, pointer fyne.Position, inside bool) {
		if !inside || currentScene == nil {
			hideTooltip()
			return
		}
		cell, found := heatmapCellAtImagePoint(currentScene, imageX, imageY, currentOption.width, currentOption.height)
		if !found {
			hideTooltip()
			return
		}
		showTooltip(formatHeatmapCellStats(cell, heatMetric), pointer)
	})
	heatmapViewer.SetTapCallback(func(imageX float64, imageY float64, pointer fyne.Position, inside bool) {
		if !inside || currentScene == nil || loading.Load() {
			return
		}
		cell, found := heatmapCellAtImagePoint(currentScene, imageX, imageY, currentOption.width, currentOption.height)
		if !found {
			return
		}
		rows := heatmapSquareRowsForCell(currentScene, cell, selectedFilePath, selectedCompareFilePath)
		if len(rows) == 0 {
			statusLabel.SetText("No asset references were found for that square.")
			return
		}
		openHeatmapSquareAssetsWindow(window, currentScene, cell, rows, heatMetric)
	})
	placeholderLabel := widget.NewLabelWithStyle("Load an RBXL/RBXM file to begin.", fyne.TextAlignCenter, fyne.TextStyle{})
	placeholderLabel.Wrapping = fyne.TextWrapOff
	viewerStack := container.NewMax(
		container.NewPadded(heatmapViewer),
		container.NewCenter(placeholderLabel),
		tooltipLayer,
	)

	saveButton := widget.NewButtonWithIcon("Save Heatmap", theme.DownloadIcon(), func() {
		if len(currentOption.bytes) == 0 {
			return
		}
		saved, saveErr := saveBytesWithNativeDialog("Save Heatmap", currentOption.fileName, currentOption.bytes)
		if saveErr != nil {
			fyne.Do(func() {
				statusLabel.SetText(fmt.Sprintf("Save failed: %s", saveErr.Error()))
			})
			return
		}
		if saved {
			statusLabel.SetText(fmt.Sprintf("Saved %s", currentOption.fileName))
		}
	})
	saveButton.Disable()
	var setBusy func(bool)
	var showHeatmapFailure func(message string)

	rerenderCurrentScene := func(statusText string) {
		if currentScene == nil {
			return
		}
		sceneSnapshot := cloneHeatmapSceneForRerender(currentScene, gridDivisions)
		underlayBytes := append([]byte(nil), mapImageBytes...)
		opacity := heatOpacity
		spread := heatSpread
		metric := heatMetric
		gridVisible := showGrid
		token := renderToken.Add(1)
		if strings.TrimSpace(statusText) != "" {
			statusLabel.SetText(statusText)
		}
		go func(renderID uint64, scene *rbxlHeatmapScene, manualUnderlay []byte, overlayOpacity float64, overlaySpread float64, renderMetric rbxlHeatMetric, renderGrid bool) {
			option, renderErr := renderRBXLHeatmapScene(scene, manualUnderlay, overlayOpacity, overlaySpread, renderMetric, renderGrid)
			fyne.Do(func() {
				if renderToken.Load() != renderID {
					return
				}
				if renderErr != nil {
					statusLabel.SetText(fmt.Sprintf("Heatmap render failed: %s", renderErr.Error()))
					return
				}
				currentScene = scene
				currentOption = option
				heatmapReady = true
				heatmapViewer.SetOption(currentOption)
				heatmapViewer.SetBackground(rbxlHeatmapDefaultBgColor)
				placeholderLabel.Hide()
				saveButton.Enable()
				statusLabel.SetText(heatmapReadyStatus(scene.DiffMode))
			})
		}(token, sceneSnapshot, underlayBytes, opacity, spread, metric, gridVisible)
	}

	selectButton := widget.NewButton("Select .rbxl/.rbxm File", func() {
		pickRBXLSource(window, func(filePath string) {
			selectedFilePath = strings.TrimSpace(filePath)
			filePathLabel.SetText(selectedFilePath)
			setWarning(materialVariantWarningData{})
			statusLabel.SetText("Ready to build heatmap.")
		}, func(err error) {
			statusLabel.SetText(fmt.Sprintf("File selection failed: %s", err.Error()))
		})
	})
	selectCompareButton := widget.NewButton("Select Compare .rbxl/.rbxm", func() {
		pickRBXLSource(window, func(filePath string) {
			selectedCompareFilePath = strings.TrimSpace(filePath)
			compareFilePathLabel.SetText(selectedCompareFilePath)
			setWarning(materialVariantWarningData{})
			statusLabel.SetText("Ready to build diff heatmap.")
		}, func(err error) {
			statusLabel.SetText(fmt.Sprintf("Comparison file selection failed: %s", err.Error()))
		})
	})
	diffCompareRow := container.NewBorder(nil, nil, widget.NewLabel("Compare File:"), nil, compareFilePathLabel)
	diffCompareActions := container.NewHBox(layout.NewSpacer(), selectCompareButton)
	diffCompareSection = container.NewVBox(diffCompareRow, diffCompareActions)
	diffCompareSection.Hide()
	selectMapButton := widget.NewButton("Select Map Image", func() {
		selectedPath, pickerErr := nativeDialog.File().
			Filter("Image files", "png", "jpg", "jpeg").
			Title("Select a map image to place under the heatmap").
			Load()
		if pickerErr != nil {
			if errors.Is(pickerErr, nativeDialog.Cancelled) {
				return
			}
			statusLabel.SetText(fmt.Sprintf("Map image picker failed: %s", pickerErr.Error()))
			return
		}
		imageBytes, readErr := os.ReadFile(selectedPath)
		if readErr != nil {
			statusLabel.SetText(fmt.Sprintf("Failed to read map image: %s", readErr.Error()))
			return
		}
		if _, _, decodeErr := image.Decode(bytes.NewReader(imageBytes)); decodeErr != nil {
			statusLabel.SetText(fmt.Sprintf("Failed to decode map image: %s", decodeErr.Error()))
			return
		}
		selectedMapImagePath = strings.TrimSpace(selectedPath)
		mapImageBytes = append([]byte(nil), imageBytes...)
		mapImageLabel.SetText(selectedMapImagePath)
		if currentScene != nil && heatmapReady && !loading.Load() {
			rerenderCurrentScene("Re-rendering heatmap with selected map image...")
			return
		}
		statusLabel.SetText("Map underlay loaded. Build the heatmap to apply it.")
	})
	clearMapButton := widget.NewButton("Clear Map Image", func() {
		selectedMapImagePath = ""
		mapImageBytes = nil
		mapImageLabel.SetText("Using auto-generated map underlay.")
		if currentScene != nil && heatmapReady && !loading.Load() {
			rerenderCurrentScene("Re-rendering heatmap with auto-generated map underlay...")
			return
		}
		statusLabel.SetText("Map underlay cleared. Build the heatmap to apply the generated map.")
	})
	opacitySlider.OnChanged = func(value float64) {
		heatOpacity = value
		opacityValueLabel.SetText(fmt.Sprintf("%d%%", int(math.Round(value*100))))
		if currentScene != nil && heatmapReady && !loading.Load() {
			rerenderCurrentScene("Updating heatmap opacity...")
		}
	}
	spreadSlider.OnChanged = func(value float64) {
		heatSpread = clampHeatmapFloat64(value, rbxlHeatmapMinSpread, rbxlHeatmapMaxSpread)
		spreadValueLabel.SetText(fmt.Sprintf("%.2fx", heatSpread))
		if currentScene != nil && heatmapReady && !loading.Load() {
			rerenderCurrentScene("Updating heat spread...")
		}
	}
	gridDensitySlider.OnChanged = func(value float64) {
		gridDivisions = clampHeatmapInt(
			int(math.Round(value/float64(rbxlHeatmapGridDivisionStep)))*rbxlHeatmapGridDivisionStep,
			rbxlHeatmapMinGridDivisions,
			rbxlHeatmapMaxGridDivisions,
		)
		gridDensityValueLabel.SetText(fmt.Sprintf("%d", gridDivisions))
		if currentScene != nil && heatmapReady && !loading.Load() {
			rerenderCurrentScene("Updating heatmap grid density...")
		}
	}
	showGridCheck.OnChanged = func(checked bool) {
		showGrid = checked
		if currentScene != nil && heatmapReady && !loading.Load() {
			rerenderCurrentScene("Updating heatmap grid...")
		}
	}
	metricSelect.OnChanged = func(selected string) {
		heatMetric = rbxlHeatMetric(strings.TrimSpace(selected))
		if currentScene != nil && heatmapReady && !loading.Load() {
			rerenderCurrentScene("Updating heatmap metric...")
		}
	}

	setBusy = func(busy bool) {
		loading.Store(busy)
		if busy {
			progressBar.Show()
			progressBar.Start()
			selectButton.Disable()
			selectCompareButton.Disable()
			selectMapButton.Disable()
			clearMapButton.Disable()
			diffModeCheck.Disable()
			pathFilterCheck.Disable()
			pathFilterEntry.Disable()
			saveButton.Disable()
			return
		}
		progressBar.Stop()
		progressBar.Hide()
		selectButton.Enable()
		selectCompareButton.Enable()
		selectMapButton.Enable()
		clearMapButton.Enable()
		diffModeCheck.Enable()
		pathFilterCheck.Enable()
		if pathFilterEnabled {
			pathFilterEntry.Enable()
		}
		if heatmapReady {
			saveButton.Enable()
		}
	}
	showHeatmapFailure = func(message string) {
		setBusy(false)
		statusLabel.SetText(message)
		setWarning(materialVariantWarningData{})
		currentOption = blankHeatmapPreviewOption()
		currentScene = nil
		heatmapReady = false
		heatmapViewer.SetOption(currentOption)
		hideTooltip()
		placeholderLabel.SetText(heatmapUnavailablePlaceholder())
		placeholderLabel.Show()
		summaryLabel.SetText("No heatmap built.")
		saveButton.Disable()
	}

	buildButton := widget.NewButton("Build Heatmap", nil)
	buildButton.OnTapped = func() {
		if loading.Load() {
			return
		}
		if strings.TrimSpace(selectedFilePath) == "" {
			statusLabel.SetText("Select an .rbxl or .rbxm file first.")
			return
		}
		if diffMode && strings.TrimSpace(selectedCompareFilePath) == "" {
			statusLabel.SetText("Select a comparison .rbxl or .rbxm file for diff mode.")
			return
		}

		pathPrefixes := []string{}
		if pathFilterEnabled {
			pathPrefixes = whitelistPatternsToPathPrefixes(pathFilterText)
		}

		if diffMode {
			placeholderLabel.SetText(heatmapBuildingPlaceholder(true))
			summaryLabel.SetText("Building diff heatmap...")
		} else {
			placeholderLabel.SetText(heatmapBuildingPlaceholder(false))
			summaryLabel.SetText("Building heatmap...")
		}
		placeholderLabel.Show()
		legendLabel.SetText(heatmapLegendText(diffMode))
		heatmapReady = false
		setWarning(materialVariantWarningData{})
		setBusy(true)

		backgroundBytes := append([]byte(nil), mapImageBytes...)
		go func(filePath string, comparePath string, prefixes []string, underlayBytes []byte, diffEnabled bool) {
			refs, extractErr := extractPositionedRefsWithRustyAssetTool(filePath, prefixes, nil)
			if extractErr != nil {
				fyne.Do(func() {
					showHeatmapFailure(fmt.Sprintf("Heatmap extraction failed: %s", extractErr.Error()))
				})
				return
			}
			mapParts, mapErr := extractMapRenderPartsWithRustyAssetTool(filePath, prefixes, nil)
			if mapErr != nil {
				fyne.Do(func() {
					showHeatmapFailure(fmt.Sprintf("Map extraction failed: %s", mapErr.Error()))
				})
				return
			}
			baseWarningText, warningErr := buildRBXLMissingMaterialVariantWarning(filePath, prefixes, nil)
			if warningErr != nil {
				logDebugf("Heatmap material warning extraction failed for %s: %s", filePath, warningErr.Error())
			}

			compareRefs := []positionedRustyAssetToolResult(nil)
			compareMapParts := []mapRenderPartRustyAssetToolResult(nil)
			compareWarningText := materialVariantWarningData{}
			if diffEnabled {
				fyne.Do(func() {
					statusLabel.SetText("Extracting comparison RBXL/RBXM...")
				})
				compareRefs, extractErr = extractPositionedRefsWithRustyAssetTool(comparePath, prefixes, nil)
				if extractErr != nil {
					fyne.Do(func() {
						showHeatmapFailure(fmt.Sprintf("Comparison heatmap extraction failed: %s", extractErr.Error()))
					})
					return
				}
				compareMapParts, mapErr = extractMapRenderPartsWithRustyAssetTool(comparePath, prefixes, nil)
				if mapErr != nil {
					fyne.Do(func() {
						showHeatmapFailure(fmt.Sprintf("Comparison map extraction failed: %s", mapErr.Error()))
					})
					return
				}
				compareWarningText, warningErr = buildRBXLMissingMaterialVariantWarning(comparePath, prefixes, nil)
				if warningErr != nil {
					logDebugf("Heatmap material warning extraction failed for %s: %s", comparePath, warningErr.Error())
				}
			}

			fyne.Do(func() {
				statusLabel.SetText(fmt.Sprintf("Resolving asset sizes for %d references...", len(refs)))
			})

			buildResult, buildErr := buildRBXLHeatmapScene(refs, mapParts, func(done int, total int, memoryCacheHits int, diskCacheHits int, networkFetches int) {
				fyne.Do(func() {
					statusLabel.SetText(fmt.Sprintf(
						"Resolving base asset sizes %d/%d... fetched from: mem %d, disk %d, net %d",
						done,
						total,
						memoryCacheHits,
						diskCacheHits,
						networkFetches,
					))
				})
			})
			if buildErr != nil {
				fyne.Do(func() {
					showHeatmapFailure(fmt.Sprintf("Heatmap build failed: %s", buildErr.Error()))
				})
				return
			}

			if diffEnabled {
				fyne.Do(func() {
					statusLabel.SetText(fmt.Sprintf("Resolving comparison asset sizes for %d references...", len(compareRefs)))
				})
				compareBuildResult, compareBuildErr := buildRBXLHeatmapScene(compareRefs, compareMapParts, func(done int, total int, memoryCacheHits int, diskCacheHits int, networkFetches int) {
					fyne.Do(func() {
						statusLabel.SetText(fmt.Sprintf(
							"Resolving comparison asset sizes %d/%d... fetched from: mem %d, disk %d, net %d",
							done,
							total,
							memoryCacheHits,
							diskCacheHits,
							networkFetches,
						))
					})
				})
				if compareBuildErr != nil {
					fyne.Do(func() {
						showHeatmapFailure(fmt.Sprintf("Diff heatmap build failed: %s", compareBuildErr.Error()))
					})
					return
				}
				buildResult = buildRBXLDiffHeatmapResult(buildResult, compareBuildResult)
			}

			option, renderErr := renderRBXLHeatmapScene(buildResult.Scene, underlayBytes, heatOpacity, heatSpread, heatMetric, showGrid)
			if renderErr != nil {
				fyne.Do(func() {
					showHeatmapFailure(fmt.Sprintf("Heatmap render failed: %s", renderErr.Error()))
				})
				return
			}

			fyne.Do(func() {
				currentScene = buildResult.Scene
				currentOption = option
				heatmapReady = true
				heatmapViewer.SetOption(currentOption)
				heatmapViewer.SetBackground(rbxlHeatmapDefaultBgColor)
				placeholderLabel.Hide()
				saveButton.Enable()
				hideTooltip()
				setWarning(combineMaterialVariantWarnings(baseWarningText, compareWarningText))
				summaryLabel.SetText(formatHeatmapBuildSummary(buildResult))
				statusLabel.SetText(heatmapReadyStatus(buildResult.DiffMode))
				setBusy(false)
			})
		}(selectedFilePath, selectedCompareFilePath, pathPrefixes, backgroundBytes, diffMode)
	}

	diffDescriptionLabel := widget.NewLabel("Optional compare mode. Heat is calculated as compare minus base on the same world grid.")
	diffDescriptionLabel.Wrapping = fyne.TextWrapWord
	mapDescriptionLabel := widget.NewLabel("Auto-generated from RBXL/RBXM part footprints. You can optionally override it with your own image.")
	mapDescriptionLabel.Wrapping = fyne.TextWrapWord
	filterDescriptionLabel := widget.NewLabel("These run before heatmap extraction, matching the RBXL/RBXM scan path-filter behavior.")
	filterDescriptionLabel.Wrapping = fyne.TextWrapWord
	advancedSections := widget.NewAccordion(
		widget.NewAccordionItem(
			"Diff",
			container.NewVBox(
				diffDescriptionLabel,
				diffModeCheck,
				diffCompareSection,
			),
		),
		widget.NewAccordionItem(
			"Map Underlay",
			container.NewVBox(
				mapDescriptionLabel,
				container.NewBorder(nil, nil, nil, container.NewHBox(selectMapButton, clearMapButton), mapImageLabel),
			),
		),
		widget.NewAccordionItem(
			"Filters",
			container.NewVBox(
				filterDescriptionLabel,
				pathFilterCheck,
				pathFilterEntry,
			),
		),
	)
	advancedSections.MultiOpen = true

	controls := container.NewVBox(
		container.NewBorder(nil, nil, widget.NewLabel("RBXL/RBXM File:"), selectButton, filePathLabel),
		advancedSections,
		container.NewBorder(
			nil,
			nil,
			widget.NewLabel("Heat Opacity:"),
			opacityValueLabel,
			opacitySlider,
		),
		container.NewBorder(
			nil,
			nil,
			widget.NewLabel("Heat Spread:"),
			spreadValueLabel,
			spreadSlider,
		),
		container.NewBorder(
			nil,
			nil,
			widget.NewLabel("Grid Density:"),
			gridDensityValueLabel,
			gridDensitySlider,
		),
		showGridCheck,
		container.NewBorder(
			nil,
			nil,
			widget.NewLabel("Heat Metric:"),
			nil,
			metricSelect,
		),
		container.NewHBox(buildButton, saveButton, layout.NewSpacer(), progressBar),
		warningBanner.root,
		summaryLabel,
		legendLabel,
	)

	return container.NewBorder(
		controls,
		statusLabel,
		nil,
		nil,
		viewerStack,
	)
}

func buildRBXLHeatmapScene(
	references []positionedRustyAssetToolResult,
	mapPartsRaw []mapRenderPartRustyAssetToolResult,
	onSizeProgress func(done int, total int, memoryCacheHits int, diskCacheHits int, networkFetches int),
) (rbxlHeatmapBuildResult, error) {
	type pendingPoint struct {
		AssetID      int64
		AssetInput   string
		InstanceType string
		InstanceName string
		InstancePath string
		PropertyName string
		X            float64
		Y            float64
		Z            float64
	}

	pendingPoints := make([]pendingPoint, 0, len(references))
	uniqueReferencesByKey := map[string]heatmapAssetReference{}
	missingPositionCount := 0
	minY := 0.0
	maxY := 0.0
	hasYBounds := false

	for _, reference := range references {
		if reference.ID <= 0 {
			continue
		}
		if reference.WorldX == nil || reference.WorldY == nil || reference.WorldZ == nil {
			missingPositionCount++
			continue
		}
		pendingPoints = append(pendingPoints, pendingPoint{
			AssetID:      reference.ID,
			AssetInput:   strings.TrimSpace(reference.RawContent),
			InstanceType: strings.TrimSpace(reference.InstanceType),
			InstanceName: strings.TrimSpace(reference.InstanceName),
			InstancePath: strings.TrimSpace(reference.InstancePath),
			PropertyName: strings.TrimSpace(reference.PropertyName),
			X:            *reference.WorldX,
			Y:            *reference.WorldY,
			Z:            *reference.WorldZ,
		})
		referenceKey := scanAssetReferenceKey(reference.ID, reference.RawContent)
		uniqueReferencesByKey[referenceKey] = heatmapAssetReference{
			AssetID:    reference.ID,
			AssetInput: strings.TrimSpace(reference.RawContent),
		}
		if !hasYBounds {
			minY = *reference.WorldY
			maxY = *reference.WorldY
			hasYBounds = true
		} else {
			minY = math.Min(minY, *reference.WorldY)
			maxY = math.Max(maxY, *reference.WorldY)
		}
	}

	if len(pendingPoints) == 0 {
		return rbxlHeatmapBuildResult{}, fmt.Errorf("no world-positioned asset references were found")
	}

	referenceKeys := make([]string, 0, len(uniqueReferencesByKey))
	for referenceKey := range uniqueReferencesByKey {
		referenceKeys = append(referenceKeys, referenceKey)
	}
	sort.Strings(referenceKeys)

	referencesToResolve := make([]heatmapAssetReference, 0, len(referenceKeys))
	for _, referenceKey := range referenceKeys {
		referencesToResolve = append(referencesToResolve, uniqueReferencesByKey[referenceKey])
	}

	statsByReferenceKey := resolveHeatmapAssetStats(referencesToResolve, onSizeProgress)
	points := make([]rbxlHeatmapPoint, 0, len(pendingPoints))
	missingSizeCount := 0
	totalWeightedBytes := int64(0)
	resolvedUniqueAssetIDs := map[int64]struct{}{}

	for _, point := range pendingPoints {
		assetStats := statsByReferenceKey[scanAssetReferenceKey(point.AssetID, point.AssetInput)]
		if assetStats.TotalBytes <= 0 {
			missingSizeCount++
			continue
		}
		points = append(points, rbxlHeatmapPoint{
			AssetID:      point.AssetID,
			AssetInput:   point.AssetInput,
			InstanceType: point.InstanceType,
			InstanceName: point.InstanceName,
			InstancePath: point.InstancePath,
			PropertyName: point.PropertyName,
			Stats:        assetStats,
			X:            point.X,
			Y:            point.Y,
			Z:            point.Z,
		})
		totalWeightedBytes += int64(assetStats.TotalBytes)
		resolvedUniqueAssetIDs[point.AssetID] = struct{}{}
	}

	if len(points) == 0 {
		return rbxlHeatmapBuildResult{}, fmt.Errorf("no asset sizes could be resolved for positioned references")
	}
	mapParts := convertRustMapParts(mapPartsRaw)
	minX, maxX, minZ, maxZ := heatmapSceneBounds(points, mapParts)
	if len(mapParts) == 0 && len(points) > 0 {
		minX, maxX, minZ, maxZ = heatmapBounds(points)
	}

	scene := &rbxlHeatmapScene{
		Points:        points,
		MapParts:      mapParts,
		GridDivisions: rbxlHeatmapDefaultGridDivisions,
		MinimumX:      minX,
		MaximumX:      maxX,
		MinimumZ:      minZ,
		MaximumZ:      maxZ,
		MinimumY:      minY,
		MaximumY:      maxY,
	}
	scene.Cells, scene.CellSizeWorld, scene.ColumnCount, scene.RowCount, scene.MaxCellBytes = buildHeatmapCells(scene, scene.GridDivisions)
	return rbxlHeatmapBuildResult{
		Scene:                scene,
		PointCount:           len(points),
		MapPartCount:         len(mapParts),
		UniqueAssetCount:     len(resolvedUniqueAssetIDs),
		MissingPositionCount: missingPositionCount,
		MissingSizeCount:     missingSizeCount,
		TotalWeightedBytes:   totalWeightedBytes,
	}, nil
}

func buildRBXLDiffHeatmapResult(baseResult rbxlHeatmapBuildResult, compareResult rbxlHeatmapBuildResult) rbxlHeatmapBuildResult {
	basePoints := append([]rbxlHeatmapPoint(nil), baseResult.Scene.Points...)
	comparePoints := append([]rbxlHeatmapPoint(nil), compareResult.Scene.Points...)
	mapParts := append([]rbxlHeatmapMapPart(nil), compareResult.Scene.MapParts...)
	if len(mapParts) == 0 {
		mapParts = append([]rbxlHeatmapMapPart(nil), baseResult.Scene.MapParts...)
	}
	allPoints := append(append([]rbxlHeatmapPoint(nil), basePoints...), comparePoints...)
	minX, maxX, minZ, maxZ := heatmapSceneBounds(allPoints, mapParts)
	if len(mapParts) == 0 && len(allPoints) > 0 {
		minX, maxX, minZ, maxZ = heatmapBounds(allPoints)
	}
	scene := &rbxlHeatmapScene{
		Points:        basePoints,
		ComparePoints: comparePoints,
		MapParts:      mapParts,
		GridDivisions: rbxlHeatmapDefaultGridDivisions,
		MinimumX:      minX,
		MaximumX:      maxX,
		MinimumZ:      minZ,
		MaximumZ:      maxZ,
		MinimumY:      math.Min(baseResult.Scene.MinimumY, compareResult.Scene.MinimumY),
		MaximumY:      math.Max(baseResult.Scene.MaximumY, compareResult.Scene.MaximumY),
		DiffMode:      true,
		DiffLabel:     "compare - base",
	}
	scene.Cells, scene.CellSizeWorld, scene.ColumnCount, scene.RowCount, scene.MaxCellBytes = buildHeatmapCells(scene, scene.GridDivisions)
	return rbxlHeatmapBuildResult{
		Scene:                       scene,
		PointCount:                  baseResult.PointCount,
		MapPartCount:                baseResult.MapPartCount,
		UniqueAssetCount:            baseResult.UniqueAssetCount,
		MissingPositionCount:        baseResult.MissingPositionCount,
		MissingSizeCount:            baseResult.MissingSizeCount,
		TotalWeightedBytes:          baseResult.TotalWeightedBytes,
		ComparePointCount:           compareResult.PointCount,
		CompareMapPartCount:         compareResult.MapPartCount,
		CompareUniqueAssetCount:     compareResult.UniqueAssetCount,
		CompareMissingPositionCount: compareResult.MissingPositionCount,
		CompareMissingSizeCount:     compareResult.MissingSizeCount,
		CompareTotalWeightedBytes:   compareResult.TotalWeightedBytes,
		DiffMode:                    true,
	}
}

func resolveHeatmapAssetStats(references []heatmapAssetReference, onProgress func(done int, total int, memoryRequestCount int, diskRequestCount int, networkRequestCount int)) map[string]rbxlHeatmapAssetStats {
	if len(references) == 0 {
		return map[string]rbxlHeatmapAssetStats{}
	}

	jobs := make(chan heatmapAssetReference, len(references))
	for _, reference := range references {
		jobs <- reference
	}
	close(jobs)

	statsByReferenceKey := make(map[string]rbxlHeatmapAssetStats, len(references))
	var statsMutex sync.Mutex
	var completed atomic.Int64
	var memoryRequestCount atomic.Int64
	var diskRequestCount atomic.Int64
	var networkRequestCount atomic.Int64
	workerCount := min(runtime.NumCPU()*2, len(references))
	if workerCount <= 0 {
		workerCount = 1
	}

	var waitGroup sync.WaitGroup
	for workerIndex := 0; workerIndex < workerCount; workerIndex++ {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			for reference := range jobs {
				referenceKey := scanAssetReferenceKey(reference.AssetID, reference.AssetInput)
				stats, requestSource := getHeatmapAssetStats(reference.AssetID, reference.AssetInput)
				statsMutex.Lock()
				statsByReferenceKey[referenceKey] = stats
				statsMutex.Unlock()
				if onProgress != nil {
					switch requestSource {
					case heatmapAssetRequestSourceNetwork:
						networkRequestCount.Add(1)
					case heatmapAssetRequestSourceDisk:
						diskRequestCount.Add(1)
					default:
						memoryRequestCount.Add(1)
					}
					onSizeProgressCount := int(completed.Add(1))
					onProgress(
						onSizeProgressCount,
						len(references),
						int(memoryRequestCount.Load()),
						int(diskRequestCount.Load()),
						int(networkRequestCount.Load()),
					)
				}
			}
		}()
	}
	waitGroup.Wait()
	return statsByReferenceKey
}

func renderRBXLHeatmapScene(scene *rbxlHeatmapScene, backgroundImageBytes []byte, heatOpacity float64, heatSpread float64, heatMetric rbxlHeatMetric, showGrid bool) (previewDownloadOption, error) {
	imageBytes, renderErr := renderRBXLHeatmapImage(scene, backgroundImageBytes, heatOpacity, heatSpread, heatMetric, showGrid)
	if renderErr != nil {
		return previewDownloadOption{}, renderErr
	}
	fileName := "rbxl_asset_size_heatmap.png"
	labelText := "Original"
	if scene != nil && scene.DiffMode {
		fileName = "rbxl_asset_size_heatmap_diff.png"
		labelText = "Diff"
	}
	return previewDownloadOption{
		labelText: labelText,
		fileName:  fileName,
		bytes:     imageBytes,
		width:     rbxlHeatmapImageSize,
		height:    rbxlHeatmapImageSize,
	}, nil
}

func renderRBXLHeatmapImage(scene *rbxlHeatmapScene, backgroundImageBytes []byte, heatOpacity float64, heatSpread float64, heatMetric rbxlHeatMetric, showGrid bool) ([]byte, error) {
	if scene == nil || len(scene.Cells) == 0 {
		return nil, fmt.Errorf("no points to render")
	}

	width := rbxlHeatmapImageSize
	height := rbxlHeatmapImageSize
	outputImage, backgroundErr := buildHeatmapBaseImage(width, height, scene, backgroundImageBytes)
	if backgroundErr != nil {
		return nil, backgroundErr
	}
	renderHeatmapCells(outputImage, scene, heatOpacity, heatSpread, heatMetric)
	if showGrid {
		drawHeatmapGrid(outputImage, scene)
	}

	var buffer bytes.Buffer
	encoder := png.Encoder{CompressionLevel: png.BestSpeed}
	if encodeErr := encoder.Encode(&buffer, outputImage); encodeErr != nil {
		return nil, encodeErr
	}

	return buffer.Bytes(), nil
}

func renderHeatmapCells(outputImage *image.NRGBA, scene *rbxlHeatmapScene, heatOpacity float64, heatSpread float64, heatMetric rbxlHeatMetric) {
	if outputImage == nil || scene == nil || len(scene.Cells) == 0 {
		return
	}
	baseAlpha := clampHeatmapFloat64(heatOpacity, 0.08, 1.0)
	spread := clampHeatmapFloat64(heatSpread, rbxlHeatmapMinSpread, rbxlHeatmapMaxSpread)
	maximums := buildHeatMetricMaximums(scene.Cells)
	maxMetricValue := maxHeatMetricValue(scene.Cells, heatMetric, maximums)
	if maxMetricValue <= 0 {
		return
	}
	for _, cell := range scene.Cells {
		metricValue := heatMetricValue(cell, heatMetric, maximums)
		if metricValue <= 0 {
			if !scene.DiffMode {
				continue
			}
			if metricValue == 0 {
				continue
			}
		}
		startX, endX, startY, endY := heatmapCellPixelBounds(scene, cell, outputImage.Bounds().Dx(), outputImage.Bounds().Dy())
		normalized := math.Log1p(math.Abs(metricValue)) / math.Log1p(maxMetricValue)
		normalized = applyHeatSpread(normalized, spread)
		fillColor := heatmapGradientColor(normalized)
		if scene.DiffMode {
			fillColor = heatmapDiffGradientColor(metricValue, normalized)
		}
		alpha := baseAlpha * (0.25 + normalized*0.75)
		for y := startY; y <= endY; y++ {
			for x := startX; x <= endX; x++ {
				base := outputImage.NRGBAAt(x, y)
				outputImage.SetNRGBA(x, y, blendNRGBA(base, fillColor, alpha))
			}
		}
	}
}

func buildHeatmapBaseImage(width int, height int, scene *rbxlHeatmapScene, backgroundImageBytes []byte) (*image.NRGBA, error) {
	outputImage := image.NewNRGBA(image.Rect(0, 0, width, height))
	backgroundColor := color.NRGBA{R: 28, G: 34, B: 40, A: 255}
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			outputImage.SetNRGBA(x, y, backgroundColor)
		}
	}
	if len(backgroundImageBytes) == 0 {
		renderGeneratedMapUnderlay(outputImage, scene)
		return outputImage, nil
	}

	decodedBackground, _, decodeErr := image.Decode(bytes.NewReader(backgroundImageBytes))
	if decodeErr != nil {
		return nil, fmt.Errorf("failed to decode map underlay: %w", decodeErr)
	}

	scaledBackground := image.NewNRGBA(image.Rect(0, 0, width, height))
	xdraw.CatmullRom.Scale(scaledBackground, scaledBackground.Bounds(), decodedBackground, decodedBackground.Bounds(), xdraw.Over, nil)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			base := outputImage.NRGBAAt(x, y)
			overlay := scaledBackground.NRGBAAt(x, y)
			outputImage.SetNRGBA(x, y, blendNRGBA(base, overlay, rbxlHeatmapManualUnderlayAlpha))
		}
	}
	return outputImage, nil
}

func getHeatmapAssetStats(assetID int64, assetInput string) (rbxlHeatmapAssetStats, heatmapAssetRequestSource) {
	cacheKey := scanAssetReferenceKey(assetID, assetInput)
	heatmapAssetStatsCache.mutex.RLock()
	cachedStats, found := heatmapAssetStatsCache.statsByAsset[cacheKey]
	heatmapAssetStatsCache.mutex.RUnlock()
	if found {
		return cachedStats, heatmapAssetRequestSourceMemory
	}

	stats := rbxlHeatmapAssetStats{AssetID: assetID}
	trace := &assetRequestTrace{}
	previewResult, previewErr := loadAssetStatsPreviewForReferenceWithTrace(assetID, assetInput, trace)
	if previewErr != nil || previewResult == nil {
		heatmapAssetStatsCache.mutex.Lock()
		heatmapAssetStatsCache.statsByAsset[cacheKey] = stats
		heatmapAssetStatsCache.mutex.Unlock()
		return stats, trace.classifyRequestSource()
	}

	stats.AssetTypeID = previewResult.AssetTypeID
	stats.AssetTypeName = strings.TrimSpace(previewResult.AssetTypeName)
	statsInfo := previewResult.Stats
	if statsInfo == nil {
		statsInfo = previewResult.Image
	}
	if statsInfo != nil {
		stats.TotalBytes = statsInfo.BytesSize
	}
	if previewResult.Image != nil && previewResult.Image.Width > 0 && previewResult.Image.Height > 0 {
		stats.TextureBytes = previewResult.Image.BytesSize
		stats.PixelCount = int64(previewResult.Image.Width * previewResult.Image.Height)
	}
	if isMeshAssetType(previewResult.AssetTypeID) && len(previewResult.DownloadBytes) > 0 {
		stats.MeshBytes = stats.TotalBytes
		if meshInfo, meshErr := parseMeshHeader(previewResult.DownloadBytes); meshErr == nil {
			stats.TriangleCount = meshInfo.NumFaces
		}
	}

	heatmapAssetStatsCache.mutex.Lock()
	heatmapAssetStatsCache.statsByAsset[cacheKey] = stats
	heatmapAssetStatsCache.mutex.Unlock()
	return stats, trace.classifyRequestSource()
}

func buildHeatmapCells(scene *rbxlHeatmapScene, gridDivisions int) ([]rbxlHeatmapCell, float64, int, int, int64) {
	if scene == nil || (len(scene.Points) == 0 && len(scene.ComparePoints) == 0) {
		return nil, 0, 0, 0, 0
	}
	if gridDivisions <= 0 {
		gridDivisions = rbxlHeatmapDefaultGridDivisions
	}
	rangeX := scene.MaximumX - scene.MinimumX
	rangeZ := scene.MaximumZ - scene.MinimumZ
	maxRange := math.Max(rangeX, rangeZ)
	if maxRange <= 0 {
		maxRange = 1
	}
	cellSizeWorld := maxRange / float64(gridDivisions)
	if cellSizeWorld <= 0 {
		cellSizeWorld = 1
	}
	columnCount := maxInt(1, int(math.Ceil(rangeX/cellSizeWorld)))
	rowCount := maxInt(1, int(math.Ceil(rangeZ/cellSizeWorld)))
	type heatmapCellAccumulator struct {
		cell                 *rbxlHeatmapCell
		baseSeenAssets       map[int64]struct{}
		compareSeenAssets    map[int64]struct{}
		baseSeenInstances    map[string]struct{}
		compareSeenInstances map[string]struct{}
	}
	cellByKey := map[string]*heatmapCellAccumulator{}
	maxCellBytes := int64(0)
	accumulatePoints := func(points []rbxlHeatmapPoint, apply func(accumulator *heatmapCellAccumulator, point rbxlHeatmapPoint)) {
		for _, point := range points {
			column := clampHeatmapInt(int(math.Floor((point.X-scene.MinimumX)/cellSizeWorld)), 0, columnCount-1)
			row := clampHeatmapInt(int(math.Floor((point.Z-scene.MinimumZ)/cellSizeWorld)), 0, rowCount-1)
			cellKey := fmt.Sprintf("%d:%d", row, column)
			accumulator, found := cellByKey[cellKey]
			if !found {
				accumulator = &heatmapCellAccumulator{
					cell: &rbxlHeatmapCell{
						Row:      row,
						Column:   column,
						MinimumX: scene.MinimumX + float64(column)*cellSizeWorld,
						MaximumX: scene.MinimumX + float64(column+1)*cellSizeWorld,
						MinimumZ: scene.MinimumZ + float64(row)*cellSizeWorld,
						MaximumZ: scene.MinimumZ + float64(row+1)*cellSizeWorld,
					},
				baseSeenAssets:       map[int64]struct{}{},
				compareSeenAssets:    map[int64]struct{}{},
				baseSeenInstances:    map[string]struct{}{},
				compareSeenInstances: map[string]struct{}{},
				}
				cellByKey[cellKey] = accumulator
			}
			apply(accumulator, point)
		}
	}
	accumulateTotals := func(totals *rbxlHeatmapTotals, point rbxlHeatmapPoint, delta int64, firstAssetInSquare bool, firstInstanceInSquare bool) {
		totals.ReferenceCount += delta
		if firstInstanceInSquare {
			switch point.InstanceType {
			case "MeshPart":
				totals.MeshPartCount += delta
			case "Part":
				totals.PartCount += delta
			}
		}
		totals.TriangleCount += int64(point.Stats.TriangleCount) * delta
		if !firstAssetInSquare {
			return
		}
		totals.UniqueAssetCount += delta
		if heatmapPointHasTextureContent(point) {
			totals.UniqueTextureCount += delta
		}
		if heatmapPointHasMeshContent(point) {
			totals.UniqueMeshCount += delta
		}
		totals.TextureBytes += int64(point.Stats.TextureBytes) * delta
		totals.MeshBytes += int64(point.Stats.MeshBytes) * delta
		totals.TotalBytes += int64(point.Stats.TotalBytes) * delta
		totals.PixelCount += point.Stats.PixelCount * delta
	}
	accumulatePoints(scene.Points, func(accumulator *heatmapCellAccumulator, point rbxlHeatmapPoint) {
		_, seenAssetInBase := accumulator.baseSeenAssets[point.AssetID]
		if !seenAssetInBase {
			accumulator.baseSeenAssets[point.AssetID] = struct{}{}
		}
		instancePath := point.InstancePath
		_, seenInstanceInBase := accumulator.baseSeenInstances[instancePath]
		if !seenInstanceInBase && instancePath != "" {
			accumulator.baseSeenInstances[instancePath] = struct{}{}
		}
		firstInst := !seenInstanceInBase && instancePath != ""
		if scene.DiffMode {
			accumulateTotals(&accumulator.cell.BaseStats, point, 1, !seenAssetInBase, firstInst)
			accumulateTotals(&accumulator.cell.DeltaStats, point, -1, !seenAssetInBase, firstInst)
			accumulator.cell.Stats = accumulator.cell.DeltaStats
		} else {
			accumulateTotals(&accumulator.cell.Stats, point, 1, !seenAssetInBase, firstInst)
		}
	})
	accumulatePoints(scene.ComparePoints, func(accumulator *heatmapCellAccumulator, point rbxlHeatmapPoint) {
		_, seenAssetInCompare := accumulator.compareSeenAssets[point.AssetID]
		if !seenAssetInCompare {
			accumulator.compareSeenAssets[point.AssetID] = struct{}{}
		}
		instancePath := point.InstancePath
		_, seenInstanceInCompare := accumulator.compareSeenInstances[instancePath]
		if !seenInstanceInCompare && instancePath != "" {
			accumulator.compareSeenInstances[instancePath] = struct{}{}
		}
		firstInst := !seenInstanceInCompare && instancePath != ""
		accumulateTotals(&accumulator.cell.Stats, point, 1, !seenAssetInCompare, firstInst)
		accumulateTotals(&accumulator.cell.DeltaStats, point, 1, !seenAssetInCompare, firstInst)
	})

	cells := make([]rbxlHeatmapCell, 0, len(cellByKey))
	for _, accumulator := range cellByKey {
		cell := accumulator.cell
		if scene.DiffMode {
			cell.Stats = cell.DeltaStats
		}
		if absInt64(cell.Stats.TotalBytes) > maxCellBytes {
			maxCellBytes = absInt64(cell.Stats.TotalBytes)
		}
		cells = append(cells, *cell)
	}
	sort.Slice(cells, func(left int, right int) bool {
		if cells[left].Row == cells[right].Row {
			return cells[left].Column < cells[right].Column
		}
		return cells[left].Row < cells[right].Row
	})
	scene.CellIndexByKey = make(map[string]int, len(cells))
	for index, cell := range cells {
		scene.CellIndexByKey[heatmapCellKey(cell.Row, cell.Column)] = index
	}
	return cells, cellSizeWorld, columnCount, rowCount, maxCellBytes
}

func heatmapPointHasTextureContent(point rbxlHeatmapPoint) bool {
	return point.Stats.TextureBytes > 0 || point.Stats.PixelCount > 0
}

func heatmapPointHasMeshContent(point rbxlHeatmapPoint) bool {
	return point.Stats.MeshBytes > 0 || point.Stats.TriangleCount > 0
}

func heatmapCellPixelBounds(scene *rbxlHeatmapScene, cell rbxlHeatmapCell, width int, height int) (int, int, int, int) {
	minPixelX, maxPixelY := mapHeatmapWorldPoint(cell.MinimumX, cell.MinimumZ, scene.MinimumX, scene.MaximumX, scene.MinimumZ, scene.MaximumZ, width, height)
	maxPixelX, minPixelY := mapHeatmapWorldPoint(cell.MaximumX, cell.MaximumZ, scene.MinimumX, scene.MaximumX, scene.MinimumZ, scene.MaximumZ, width, height)
	startX := clampHeatmapInt(int(math.Floor(math.Min(minPixelX, maxPixelX))), 0, width-1)
	endX := clampHeatmapInt(int(math.Ceil(math.Max(minPixelX, maxPixelX))), 0, width-1)
	startY := clampHeatmapInt(int(math.Floor(math.Min(minPixelY, maxPixelY))), 0, height-1)
	endY := clampHeatmapInt(int(math.Ceil(math.Max(minPixelY, maxPixelY))), 0, height-1)
	return startX, endX, startY, endY
}

func heatmapCellAtImagePoint(scene *rbxlHeatmapScene, imageX float64, imageY float64, width int, height int) (rbxlHeatmapCell, bool) {
	if scene == nil || len(scene.Cells) == 0 || width <= 0 || height <= 0 {
		return rbxlHeatmapCell{}, false
	}
	worldX, worldZ, inside := imagePointToHeatmapWorld(imageX, imageY, width, height, scene)
	if !inside {
		return rbxlHeatmapCell{}, false
	}
	column := clampHeatmapInt(int(math.Floor((worldX-scene.MinimumX)/scene.CellSizeWorld)), 0, scene.ColumnCount-1)
	row := clampHeatmapInt(int(math.Floor((worldZ-scene.MinimumZ)/scene.CellSizeWorld)), 0, scene.RowCount-1)

	pixelX := clampHeatmapInt(int(math.Round(imageX)), 0, width-1)
	pixelY := clampHeatmapInt(int(math.Round(imageY)), 0, height-1)
	bestIndex := -1
	bestCell := rbxlHeatmapCell{}
	for rowOffset := -1; rowOffset <= 1; rowOffset++ {
		candidateRow := row + rowOffset
		if candidateRow < 0 || candidateRow >= scene.RowCount {
			continue
		}
		for columnOffset := -1; columnOffset <= 1; columnOffset++ {
			candidateColumn := column + columnOffset
			if candidateColumn < 0 || candidateColumn >= scene.ColumnCount {
				continue
			}
			index, found := scene.CellIndexByKey[heatmapCellKey(candidateRow, candidateColumn)]
			if !found || index < 0 || index >= len(scene.Cells) {
				continue
			}
			cell := scene.Cells[index]
			startX, endX, startY, endY := heatmapCellPixelBounds(scene, cell, width, height)
			if pixelX < startX || pixelX > endX || pixelY < startY || pixelY > endY {
				continue
			}
			if index >= bestIndex {
				bestIndex = index
				bestCell = cell
			}
		}
	}
	if bestIndex >= 0 {
		return bestCell, true
	}
	if index, found := scene.CellIndexByKey[heatmapCellKey(row, column)]; found && index >= 0 && index < len(scene.Cells) {
		return scene.Cells[index], true
	}
	return rbxlHeatmapCell{}, false
}

func heatmapPointCellMatches(scene *rbxlHeatmapScene, point rbxlHeatmapPoint, row int, column int) bool {
	if scene == nil || scene.CellSizeWorld <= 0 || scene.ColumnCount <= 0 || scene.RowCount <= 0 {
		return false
	}
	pointColumn := clampHeatmapInt(int(math.Floor((point.X-scene.MinimumX)/scene.CellSizeWorld)), 0, scene.ColumnCount-1)
	pointRow := clampHeatmapInt(int(math.Floor((point.Z-scene.MinimumZ)/scene.CellSizeWorld)), 0, scene.RowCount-1)
	return pointRow == row && pointColumn == column
}

func heatmapSquareRowsForCell(scene *rbxlHeatmapScene, cell rbxlHeatmapCell, baseFilePath string, compareFilePath string) []heatmapSquareAssetRow {
	if scene == nil {
		return nil
	}
	rows := make([]heatmapSquareAssetRow, 0)
	rowIndexByKey := map[string]int{}
	sceneSurfaceAreasByPath := buildSceneSurfaceAreaIndexFromHeatmapParts(scene.MapParts)
	appendRows := func(points []rbxlHeatmapPoint, side string, filePath string) {
		for _, point := range points {
			if !heatmapPointCellMatches(scene, point, cell.Row, cell.Column) {
				continue
			}
			rowKey := fmt.Sprintf("%s:%d", side, point.AssetID)
			if existingIndex, found := rowIndexByKey[rowKey]; found {
				rows[existingIndex].UseCount++
				nextArea, nextPath := estimateSceneSurfaceAreaAndPathForPaths(point.InstancePath, nil, sceneSurfaceAreasByPath)
				if nextArea > rows[existingIndex].SceneSurfaceArea {
					rows[existingIndex].SceneSurfaceArea = nextArea
					rows[existingIndex].LargestSurfacePath = strings.TrimSpace(nextPath)
				} else if rows[existingIndex].SceneSurfaceArea <= 0 && strings.TrimSpace(rows[existingIndex].LargestSurfacePath) == "" {
					rows[existingIndex].LargestSurfacePath = strings.TrimSpace(nextPath)
				}
				continue
			}
			sceneSurfaceArea, largestSurfacePath := estimateSceneSurfaceAreaAndPathForPaths(point.InstancePath, nil, sceneSurfaceAreasByPath)
			rowIndexByKey[rowKey] = len(rows)
			rows = append(rows, heatmapSquareAssetRow{
				Side:               side,
				AssetID:            point.AssetID,
				UseCount:           1,
				AssetTypeID:        point.Stats.AssetTypeID,
				AssetTypeName:      point.Stats.AssetTypeName,
				AssetInput:         point.AssetInput,
				FilePath:           filePath,
				InstanceType:       point.InstanceType,
				InstanceName:       point.InstanceName,
				InstancePath:       point.InstancePath,
				PropertyName:       point.PropertyName,
				TotalBytes:         int64(point.Stats.TotalBytes),
				TextureBytes:       int64(point.Stats.TextureBytes),
				MeshBytes:          int64(point.Stats.MeshBytes),
				Triangles:          int64(point.Stats.TriangleCount),
				Pixels:             point.Stats.PixelCount,
				WorldX:             point.X,
				WorldY:             point.Y,
				WorldZ:             point.Z,
				SceneSurfaceArea:   sceneSurfaceArea,
				LargestSurfacePath: strings.TrimSpace(largestSurfacePath),
			})
		}
	}
	if scene.DiffMode {
		appendRows(scene.Points, "Base", baseFilePath)
		appendRows(scene.ComparePoints, "Compare", compareFilePath)
	} else {
		appendRows(scene.Points, "", baseFilePath)
	}
	sort.SliceStable(rows, func(left int, right int) bool {
		if rows[left].TotalBytes == rows[right].TotalBytes {
			if rows[left].AssetID == rows[right].AssetID {
				return rows[left].Side < rows[right].Side
			}
			return rows[left].AssetID < rows[right].AssetID
		}
		return rows[left].TotalBytes > rows[right].TotalBytes
	})
	return rows
}

func heatmapSquareRowsToScanResults(rows []heatmapSquareAssetRow) []scanResult {
	results := make([]scanResult, 0, len(rows))
	for _, row := range rows {
		meshTriangles := uint32(0)
		if row.Triangles > 0 {
			meshTriangles = uint32(row.Triangles)
		}
		assetTypeName := strings.TrimSpace(row.AssetTypeName)
		if assetTypeName == "" {
			assetTypeName = "Unknown"
		}
		results = append(results, refreshLargeTextureMetrics(scanResult{
			AssetID:            row.AssetID,
			AssetInput:         row.AssetInput,
			Side:               row.Side,
			UseCount:           row.UseCount,
			FilePath:           row.FilePath,
			InstanceType:       row.InstanceType,
			InstanceName:       row.InstanceName,
			InstancePath:       row.InstancePath,
			PropertyName:       row.PropertyName,
			Source:             sourceNoThumbnail,
			State:              "Heatmap",
			BytesSize:          int(row.TotalBytes),
			TotalBytesSize:     int(row.TotalBytes),
			MeshNumFaces:       meshTriangles,
			AssetTypeID:        row.AssetTypeID,
			AssetTypeName:      assetTypeName,
			WorldX:             row.WorldX,
			WorldY:             row.WorldY,
			WorldZ:             row.WorldZ,
			TextureBytes:       int(row.TextureBytes),
			MeshBytes:          int(row.MeshBytes),
			PixelCount:         row.Pixels,
			SceneSurfaceArea:   row.SceneSurfaceArea,
			LargestSurfacePath: row.LargestSurfacePath,
		}))
	}
	return results
}

func imagePointToHeatmapWorld(imageX float64, imageY float64, width int, height int, scene *rbxlHeatmapScene) (float64, float64, bool) {
	contentWidth := float64(width - rbxlHeatmapPadding*2)
	contentHeight := float64(height - rbxlHeatmapPadding*2)
	if contentWidth <= 0 || contentHeight <= 0 {
		return 0, 0, false
	}
	if imageX < rbxlHeatmapPadding || imageX > float64(width-rbxlHeatmapPadding) {
		return 0, 0, false
	}
	if imageY < rbxlHeatmapPadding || imageY > float64(height-rbxlHeatmapPadding) {
		return 0, 0, false
	}
	normalizedX := (imageX - float64(rbxlHeatmapPadding)) / contentWidth
	normalizedZ := (float64(height-rbxlHeatmapPadding) - imageY) / contentHeight
	worldX := scene.MinimumX + normalizedX*(scene.MaximumX-scene.MinimumX)
	worldZ := scene.MinimumZ + normalizedZ*(scene.MaximumZ-scene.MinimumZ)
	return worldX, worldZ, true
}

func formatHeatmapCellStats(cell rbxlHeatmapCell, heatMetric rbxlHeatMetric) string {
	centerX := (cell.MinimumX + cell.MaximumX) / 2
	centerZ := (cell.MinimumZ + cell.MaximumZ) / 2
	if cell.BaseStats.ReferenceCount > 0 || cell.DeltaStats.ReferenceCount != 0 {
		return fmt.Sprintf(
			"%s\nSquare %d,%d\nCenter: X %.1f, Z %.1f\nDelta: %s\nBase: refs %d, unique assets %d, unique textures %d, unique meshes %d, total %.2f MB\nCompare: refs %d, unique assets %d, unique textures %d, unique meshes %d, total %.2f MB\nTexture Delta: %s\nMesh Delta: %s\nTriangle Delta: %s\nPixel Delta: %s",
			formatHeatmapMetricSummary(cell, heatMetric),
			cell.Column+1,
			cell.Row+1,
			centerX,
			centerZ,
			formatHeatmapDeltaSummary(cell, heatMetric),
			cell.BaseStats.ReferenceCount,
			cell.BaseStats.UniqueAssetCount,
			cell.BaseStats.UniqueTextureCount,
			cell.BaseStats.UniqueMeshCount,
			float64(cell.BaseStats.TotalBytes)/megabyte,
			cell.BaseStats.ReferenceCount+cell.DeltaStats.ReferenceCount,
			cell.BaseStats.UniqueAssetCount+cell.DeltaStats.UniqueAssetCount,
			cell.BaseStats.UniqueTextureCount+cell.DeltaStats.UniqueTextureCount,
			cell.BaseStats.UniqueMeshCount+cell.DeltaStats.UniqueMeshCount,
			float64(cell.BaseStats.TotalBytes+cell.DeltaStats.TotalBytes)/megabyte,
			formatSignedSizeAuto(cell.Stats.TextureBytes),
			formatSignedSizeAuto(cell.Stats.MeshBytes),
			formatSignedIntCommas(cell.Stats.TriangleCount),
			formatSignedIntCommas(cell.Stats.PixelCount),
		)
	}
	return fmt.Sprintf(
		"%s\nSquare %d,%d · refs %d · unique assets %d · unique textures %d · unique meshes %d\nCenter: X %.1f, Z %.1f\nTextures: %s\nMeshes: %s\nTotal: %.2f MB\nTriangles: %s\nPixels: %s",
		formatHeatmapMetricSummary(cell, heatMetric),
		cell.Column+1,
		cell.Row+1,
		cell.Stats.ReferenceCount,
		cell.Stats.UniqueAssetCount,
		cell.Stats.UniqueTextureCount,
		cell.Stats.UniqueMeshCount,
		centerX,
		centerZ,
		formatSizeAuto(int(cell.Stats.TextureBytes)),
		formatSizeAuto(int(cell.Stats.MeshBytes)),
		float64(cell.Stats.TotalBytes)/megabyte,
		formatIntCommas(cell.Stats.TriangleCount),
		formatIntCommas(cell.Stats.PixelCount),
	)
}

func formatHeatmapMetricSummary(cell rbxlHeatmapCell, heatMetric rbxlHeatMetric) string {
	isDiff := cell.BaseStats.ReferenceCount > 0 || cell.DeltaStats.ReferenceCount != 0
	switch heatMetric {
	case heatMetricTotalBytes:
		if isDiff {
			return fmt.Sprintf("Heat Delta: Total Byte Size = %s", formatSignedSizeAuto(cell.Stats.TotalBytes))
		}
		return fmt.Sprintf("Heat: Total Byte Size = %s", formatSizeAuto(int(cell.Stats.TotalBytes)))
	case heatMetricTextureBytes:
		if isDiff {
			return fmt.Sprintf("Heat Delta: Texture Bytes = %s", formatSignedSizeAuto(cell.Stats.TextureBytes))
		}
		return fmt.Sprintf("Heat: Texture Bytes = %s", formatSizeAuto(int(cell.Stats.TextureBytes)))
	case heatMetricTexturePixels:
		if isDiff {
			return fmt.Sprintf("Heat Delta: Texture Pixels = %s", formatSignedIntCommas(cell.Stats.PixelCount))
		}
		return fmt.Sprintf("Heat: Texture Pixels = %s", formatIntCommas(cell.Stats.PixelCount))
	case heatMetricMeshBytes:
		if isDiff {
			return fmt.Sprintf("Heat Delta: Mesh Bytes = %s", formatSignedSizeAuto(cell.Stats.MeshBytes))
		}
		return fmt.Sprintf("Heat: Mesh Bytes = %s", formatSizeAuto(int(cell.Stats.MeshBytes)))
	case heatMetricMeshTriangles:
		if isDiff {
			return fmt.Sprintf("Heat Delta: Mesh Triangles = %s", formatSignedIntCommas(cell.Stats.TriangleCount))
		}
		return fmt.Sprintf("Heat: Mesh Triangles = %s", formatIntCommas(cell.Stats.TriangleCount))
	case heatMetricUniqueTextureCount:
		if isDiff {
			return fmt.Sprintf("Heat Delta: Unique Texture Count = %s", formatSignedIntCommas(cell.Stats.UniqueTextureCount))
		}
		return fmt.Sprintf("Heat: Unique Texture Count = %s", formatIntCommas(cell.Stats.UniqueTextureCount))
	case heatMetricUniqueMeshCount:
		if isDiff {
			return fmt.Sprintf("Heat Delta: Unique Mesh Count = %s", formatSignedIntCommas(cell.Stats.UniqueMeshCount))
		}
		return fmt.Sprintf("Heat: Unique Mesh Count = %s", formatIntCommas(cell.Stats.UniqueMeshCount))
	case heatMetricUniqueAssetCount:
		if isDiff {
			return fmt.Sprintf("Heat Delta: Unique Assets = %s", formatSignedIntCommas(cell.Stats.UniqueAssetCount))
		}
		return fmt.Sprintf("Heat: Unique Assets = %s", formatIntCommas(cell.Stats.UniqueAssetCount))
	case heatMetricMeshPartCount:
		if isDiff {
			return fmt.Sprintf("Heat Delta: MeshParts = %s", formatSignedIntCommas(cell.Stats.MeshPartCount))
		}
		return fmt.Sprintf("Heat: MeshParts = %s", formatIntCommas(cell.Stats.MeshPartCount))
	case heatMetricPartCount:
		if isDiff {
			return fmt.Sprintf("Heat Delta: Parts = %s", formatSignedIntCommas(cell.Stats.PartCount))
		}
		return fmt.Sprintf("Heat: Parts = %s", formatIntCommas(cell.Stats.PartCount))
	default:
		if isDiff {
			return fmt.Sprintf("Heat Delta: Total Byte Size = %s", formatSignedSizeAuto(cell.Stats.TotalBytes))
		}
		return fmt.Sprintf("Heat: Total Byte Size = %s", formatSizeAuto(int(cell.Stats.TotalBytes)))
	}
}

func heatMetricValue(cell rbxlHeatmapCell, heatMetric rbxlHeatMetric, maximums heatMetricMaximums) float64 {
	switch heatMetric {
	case heatMetricTotalBytes:
		return float64(cell.Stats.TotalBytes)
	case heatMetricTextureBytes:
		return float64(cell.Stats.TextureBytes)
	case heatMetricTexturePixels:
		return float64(cell.Stats.PixelCount)
	case heatMetricMeshBytes:
		return float64(cell.Stats.MeshBytes)
	case heatMetricMeshTriangles:
		return float64(cell.Stats.TriangleCount)
	case heatMetricUniqueTextureCount:
		return float64(cell.Stats.UniqueTextureCount)
	case heatMetricUniqueMeshCount:
		return float64(cell.Stats.UniqueMeshCount)
	case heatMetricUniqueAssetCount:
		return float64(cell.Stats.UniqueAssetCount)
	case heatMetricMeshPartCount:
		return float64(cell.Stats.MeshPartCount)
	case heatMetricPartCount:
		return float64(cell.Stats.PartCount)
	default:
		return float64(cell.Stats.TotalBytes)
	}
}

func maxHeatMetricValue(cells []rbxlHeatmapCell, heatMetric rbxlHeatMetric, maximums heatMetricMaximums) float64 {
	maximum := 0.0
	for _, cell := range cells {
		value := math.Abs(heatMetricValue(cell, heatMetric, maximums))
		if value > maximum {
			maximum = value
		}
	}
	return maximum
}

func applyHeatSpread(normalized float64, spread float64) float64 {
	clamped := clampHeatmapFloat64(normalized, 0, 1)
	if clamped <= 0 {
		return 0
	}
	if clamped >= 1 {
		return 1
	}
	if spread <= 0 {
		return clamped
	}
	return math.Pow(clamped, 1.0/spread)
}

func buildHeatMetricMaximums(cells []rbxlHeatmapCell) heatMetricMaximums {
	maximums := heatMetricMaximums{}
	for _, cell := range cells {
		maximums.TotalBytes = math.Max(maximums.TotalBytes, math.Abs(float64(cell.Stats.TotalBytes)))
		maximums.TextureBytes = math.Max(maximums.TextureBytes, math.Abs(float64(cell.Stats.TextureBytes)))
		maximums.TexturePixels = math.Max(maximums.TexturePixels, math.Abs(float64(cell.Stats.PixelCount)))
		maximums.MeshBytes = math.Max(maximums.MeshBytes, math.Abs(float64(cell.Stats.MeshBytes)))
		maximums.MeshTriangles = math.Max(maximums.MeshTriangles, math.Abs(float64(cell.Stats.TriangleCount)))
		maximums.UniqueTextureCount = math.Max(maximums.UniqueTextureCount, math.Abs(float64(cell.Stats.UniqueTextureCount)))
		maximums.UniqueMeshCount = math.Max(maximums.UniqueMeshCount, math.Abs(float64(cell.Stats.UniqueMeshCount)))
	}
	return maximums
}

func normalizedMetricComponent(value float64, maximum float64) float64 {
	if maximum <= 0 {
		return 0
	}
	return value / maximum
}

func formatHeatmapDeltaSummary(cell rbxlHeatmapCell, heatMetric rbxlHeatMetric) string {
	switch heatMetric {
	case heatMetricTotalBytes:
		return formatSignedSizeAuto(cell.Stats.TotalBytes)
	case heatMetricTextureBytes:
		return formatSignedSizeAuto(cell.Stats.TextureBytes)
	case heatMetricTexturePixels:
		return formatSignedIntCommas(cell.Stats.PixelCount)
	case heatMetricMeshBytes:
		return formatSignedSizeAuto(cell.Stats.MeshBytes)
	case heatMetricMeshTriangles:
		return formatSignedIntCommas(cell.Stats.TriangleCount)
	case heatMetricUniqueTextureCount:
		return formatSignedIntCommas(cell.Stats.UniqueTextureCount)
	case heatMetricUniqueMeshCount:
		return formatSignedIntCommas(cell.Stats.UniqueMeshCount)
	case heatMetricUniqueAssetCount:
		return formatSignedIntCommas(cell.Stats.UniqueAssetCount)
	case heatMetricMeshPartCount:
		return formatSignedIntCommas(cell.Stats.MeshPartCount)
	case heatMetricPartCount:
		return formatSignedIntCommas(cell.Stats.PartCount)
	default:
		return formatSignedSizeAuto(cell.Stats.TotalBytes)
	}
}

func formatSignedSizeAuto(value int64) string {
	if value == 0 {
		return "0 B"
	}
	if value < 0 {
		return "-" + formatSizeAuto(int(absInt64(value)))
	}
	return "+" + formatSizeAuto(int(value))
}

func formatSignedIntCommas(value int64) string {
	if value == 0 {
		return "0"
	}
	if value < 0 {
		return "-" + formatIntCommas(absInt64(value))
	}
	return "+" + formatIntCommas(value)
}

func formatHeatmapBuildSummary(result rbxlHeatmapBuildResult) string {
	if result.DiffMode {
		deltaBytes := result.CompareTotalWeightedBytes - result.TotalWeightedBytes
		return fmt.Sprintf(
			"Squares: %d  Base Points: %d  Compare Points: %d  Base Assets: %d  Compare Assets: %d  Referenced Delta: %s  Base Missing: %d/%d  Compare Missing: %d/%d",
			len(result.Scene.Cells),
			result.PointCount,
			result.ComparePointCount,
			result.UniqueAssetCount,
			result.CompareUniqueAssetCount,
			formatSignedSizeAuto(deltaBytes),
			result.MissingPositionCount,
			result.MissingSizeCount,
			result.CompareMissingPositionCount,
			result.CompareMissingSizeCount,
		)
	}
	return fmt.Sprintf(
		"Squares: %d  Points: %d  Assets: %d  Map Parts: %d  Referenced Total: %s  Missing Positions: %d  Missing Sizes: %d",
		len(result.Scene.Cells),
		result.PointCount,
		result.UniqueAssetCount,
		result.MapPartCount,
		formatSizeAuto(int(result.TotalWeightedBytes)),
		result.MissingPositionCount,
		result.MissingSizeCount,
	)
}

func heatmapLegendText(diffMode bool) string {
	if diffMode {
		return "Diff mode: cool colors decreased, warm colors increased, and transparent means little or no change."
	}
	return "Low intensity is blue, medium is yellow, and highest intensity is red."
}

func heatmapReadyStatus(diffMode bool) string {
	if diffMode {
		return "Diff heatmap ready. Warm colors increased and cool colors decreased."
	}
	return "Heatmap ready. Scroll to zoom and drag to pan."
}

func heatmapBuildingPlaceholder(diffMode bool) string {
	if diffMode {
		return "Generating diff heatmap..."
	}
	return "Generating heatmap..."
}

func heatmapUnavailablePlaceholder() string {
	return "No heatmap available."
}

func heatmapCellKey(row int, column int) string {
	return fmt.Sprintf("%d:%d", row, column)
}

func cloneHeatmapSceneForRerender(scene *rbxlHeatmapScene, gridDivisions int) *rbxlHeatmapScene {
	if scene == nil {
		return nil
	}
	snapshot := *scene
	snapshot.GridDivisions = gridDivisions
	snapshot.Cells = nil
	snapshot.CellIndexByKey = nil
	snapshot.Cells, snapshot.CellSizeWorld, snapshot.ColumnCount, snapshot.RowCount, snapshot.MaxCellBytes = buildHeatmapCells(&snapshot, snapshot.GridDivisions)
	return &snapshot
}

func openHeatmapSquareAssetsWindow(parentWindow fyne.Window, scene *rbxlHeatmapScene, cell rbxlHeatmapCell, rows []heatmapSquareAssetRow, heatMetric rbxlHeatMetric) {
	if len(rows) == 0 {
		return
	}
	guiApp := fyne.CurrentApp()
	if guiApp == nil {
		return
	}
	windowTitle := fmt.Sprintf("Heatmap Square %d,%d Assets", cell.Column+1, cell.Row+1)
	detailWindow := guiApp.NewWindow(windowTitle)
	summaryLabel := widget.NewLabel(formatHeatmapSquareWindowSummary(scene, cell, rows))
	summaryLabel.Wrapping = fyne.TextWrapWord
	explorer := newScanResultsExplorer(detailWindow, scanResultsExplorerOptions{
		Variant:            scanResultsExplorerVariantHeatmap,
		PreviewPlaceholder: "Select a heatmap asset row to preview",
		IncludeFileRow:     true,
		InitialStatusText:  fmt.Sprintf("Showing %d asset references from this square.", len(rows)),
		SearchPlaceholder:  "Search asset ID, instance path, property, or type",
		HeaderContent:      summaryLabel,
		ShowDuplicateUI:    false,
		ShowLargeTextureUI: true,
	})
	explorer.SetResults(heatmapSquareRowsToScanResults(rows))
	explorer.SetSort(heatmapSquareMetricColumnName(heatMetric), true)
	detailWindow.SetContent(explorer.Content())
	detailWindow.Resize(fyne.NewSize(1380, 760))
	detailWindow.Show()
}

func heatmapSquareMetricColumnName(heatMetric rbxlHeatMetric) string {
	switch heatMetric {
	case heatMetricTextureBytes:
		return "Texture Bytes"
	case heatMetricTexturePixels:
		return "Texture Pixels"
	case heatMetricMeshBytes:
		return "Mesh Bytes"
	case heatMetricMeshTriangles:
		return "Mesh Triangles"
	case heatMetricUniqueTextureCount:
		return "Texture Bytes"
	case heatMetricUniqueMeshCount:
		return "Mesh Bytes"
	case heatMetricUniqueAssetCount:
		return "Total Byte Size"
	case heatMetricMeshPartCount:
		return "Total Byte Size"
	case heatMetricPartCount:
		return "Total Byte Size"
	default:
		return "Total Byte Size"
	}
}

func heatmapSquareRowMatchesQuery(row heatmapSquareAssetRow, normalizedQuery string) bool {
	searchFields := []string{
		row.Side,
		fmt.Sprintf("%d", row.AssetID),
		row.AssetInput,
		row.InstanceType,
		row.InstanceName,
		row.InstancePath,
		row.PropertyName,
		fmt.Sprintf("%.1f %.1f %.1f", row.WorldX, row.WorldY, row.WorldZ),
	}
	for _, field := range searchFields {
		if strings.Contains(strings.ToLower(strings.TrimSpace(field)), normalizedQuery) {
			return true
		}
	}
	return false
}

func heatmapSquareRowColumnText(row heatmapSquareAssetRow, columnName string) string {
	switch columnName {
	case "Side":
		if strings.TrimSpace(row.Side) == "" {
			return "-"
		}
		return row.Side
	case "Asset ID":
		return fmt.Sprintf("%d", row.AssetID)
	case "Total Byte Size":
		return formatSizeAuto(int(row.TotalBytes))
	case "Texture Bytes":
		return formatSizeAuto(int(row.TextureBytes))
	case "Texture Pixels":
		return formatIntCommas(row.Pixels)
	case "Mesh Bytes":
		return formatSizeAuto(int(row.MeshBytes))
	case "Mesh Triangles":
		return formatIntCommas(row.Triangles)
	case "Instance Type":
		if strings.TrimSpace(row.InstanceType) == "" {
			return "-"
		}
		return row.InstanceType
	case "Property":
		if strings.TrimSpace(row.PropertyName) == "" {
			return "-"
		}
		return row.PropertyName
	case "Instance Path":
		if strings.TrimSpace(row.InstancePath) == "" {
			return "-"
		}
		return row.InstancePath
	case "World Position":
		return fmt.Sprintf("X %.1f, Y %.1f, Z %.1f", row.WorldX, row.WorldY, row.WorldZ)
	default:
		return ""
	}
}

func compareHeatmapSquareRows(left heatmapSquareAssetRow, right heatmapSquareAssetRow, sortField string) int {
	switch sortField {
	case "Side":
		return strings.Compare(left.Side, right.Side)
	case "Asset ID":
		return compareInt64(left.AssetID, right.AssetID)
	case "Texture Bytes":
		return compareInt64(left.TextureBytes, right.TextureBytes)
	case "Texture Pixels":
		return compareInt64(left.Pixels, right.Pixels)
	case "Mesh Bytes":
		return compareInt64(left.MeshBytes, right.MeshBytes)
	case "Mesh Triangles":
		return compareInt64(left.Triangles, right.Triangles)
	case "Instance Type":
		return strings.Compare(left.InstanceType, right.InstanceType)
	case "Property":
		return strings.Compare(left.PropertyName, right.PropertyName)
	case "Instance Path":
		return strings.Compare(left.InstancePath, right.InstancePath)
	case "World Position":
		leftPosition := fmt.Sprintf("%.4f:%.4f:%.4f", left.WorldX, left.WorldY, left.WorldZ)
		rightPosition := fmt.Sprintf("%.4f:%.4f:%.4f", right.WorldX, right.WorldY, right.WorldZ)
		return strings.Compare(leftPosition, rightPosition)
	default:
		return compareInt64(left.TotalBytes, right.TotalBytes)
	}
}

func formatHeatmapSquareWindowSummary(scene *rbxlHeatmapScene, cell rbxlHeatmapCell, rows []heatmapSquareAssetRow) string {
	centerX := (cell.MinimumX + cell.MaximumX) / 2
	centerZ := (cell.MinimumZ + cell.MaximumZ) / 2
	uniqueAssetIDs := map[int64]struct{}{}
	uniqueTextureIDs := map[int64]struct{}{}
	uniqueMeshIDs := map[int64]struct{}{}
	for _, row := range rows {
		uniqueAssetIDs[row.AssetID] = struct{}{}
		if row.TextureBytes > 0 || row.Pixels > 0 {
			uniqueTextureIDs[row.AssetID] = struct{}{}
		}
		if row.MeshBytes > 0 || row.Triangles > 0 {
			uniqueMeshIDs[row.AssetID] = struct{}{}
		}
	}
	sideSummary := ""
	if scene != nil && scene.DiffMode {
		baseCount := 0
		compareCount := 0
		for _, row := range rows {
			if strings.EqualFold(row.Side, "Compare") {
				compareCount++
			} else {
				baseCount++
			}
		}
		sideSummary = fmt.Sprintf("  Base Rows: %d  Compare Rows: %d", baseCount, compareCount)
	}
	return fmt.Sprintf(
		"Square %d,%d  Center: X %.1f, Z %.1f  Rows: %d  Unique Assets: %d  Unique Textures: %d  Unique Meshes: %d%s",
		cell.Column+1,
		cell.Row+1,
		centerX,
		centerZ,
		len(rows),
		len(uniqueAssetIDs),
		len(uniqueTextureIDs),
		len(uniqueMeshIDs),
		sideSummary,
	)
}

func absInt64(value int64) int64 {
	if value < 0 {
		return -value
	}
	return value
}

func convertRustMapParts(parts []mapRenderPartRustyAssetToolResult) []rbxlHeatmapMapPart {
	converted := make([]rbxlHeatmapMapPart, 0, len(parts))
	for _, part := range parts {
		if part.CenterX == nil || part.CenterY == nil || part.CenterZ == nil {
			continue
		}
		if part.SizeX == nil || part.SizeY == nil || part.SizeZ == nil {
			continue
		}
		sizeX := *part.SizeX
		sizeY := *part.SizeY
		sizeZ := *part.SizeZ
		if sizeX <= 0 || sizeZ <= 0 {
			continue
		}
		yawDegrees := 0.0
		if part.YawDegrees != nil {
			yawDegrees = *part.YawDegrees
		}
		transparency := 0.0
		if part.Transparency != nil {
			transparency = clampHeatmapFloat64(*part.Transparency, 0, 1)
		}
		red := 163
		green := 162
		blue := 165
		if part.ColorR != nil {
			red = clampHeatmapInt(*part.ColorR, 0, 255)
		}
		if part.ColorG != nil {
			green = clampHeatmapInt(*part.ColorG, 0, 255)
		}
		if part.ColorB != nil {
			blue = clampHeatmapInt(*part.ColorB, 0, 255)
		}
		converted = append(converted, rbxlHeatmapMapPart{
			InstanceType: strings.TrimSpace(part.InstanceType),
			InstanceName: strings.TrimSpace(part.InstanceName),
			InstancePath: strings.TrimSpace(part.InstancePath),
			MaterialKey:  strings.TrimSpace(part.MaterialKey),
			CenterX:      *part.CenterX,
			CenterY:      *part.CenterY,
			CenterZ:      *part.CenterZ,
			SizeX:        sizeX,
			SizeY:        sizeY,
			SizeZ:        sizeZ,
			YawDegrees:   yawDegrees,
			Color: color.NRGBA{
				R: uint8(red),
				G: uint8(green),
				B: uint8(blue),
				A: 255,
			},
			Transparency: transparency,
		})
	}
	return converted
}

func renderGeneratedMapUnderlay(outputImage *image.NRGBA, scene *rbxlHeatmapScene) {
	if outputImage == nil || scene == nil || len(scene.MapParts) == 0 {
		return
	}
	sortedParts := append([]rbxlHeatmapMapPart(nil), scene.MapParts...)
	sort.SliceStable(sortedParts, func(left int, right int) bool {
		return sortedParts[left].CenterY < sortedParts[right].CenterY
	})
	minY, maxY := mapPartHeightBounds(sortedParts)
	drawGeneratedMapGround(outputImage)
	for _, part := range sortedParts {
		drawGeneratedMapShadow(outputImage, scene, part, minY, maxY)
	}
	for _, part := range sortedParts {
		drawGeneratedMapPart(outputImage, scene, part, minY, maxY)
	}
}

func drawGeneratedMapPart(outputImage *image.NRGBA, scene *rbxlHeatmapScene, part rbxlHeatmapMapPart, minY float64, maxY float64) {
	pixelCorners, startX, endX, startY, endY := mapPartPixelPolygon(outputImage, scene, part)
	if len(pixelCorners) < 3 {
		return
	}
	heightScale := 0.0
	if maxY > minY {
		heightScale = (part.CenterY - minY) / (maxY - minY)
	}
	fillColor := mapPartVisualColor(part, heightScale)
	alpha := rbxlHeatmapGeneratedMapAlpha * (1 - part.Transparency*0.85)
	for y := startY; y <= endY; y++ {
		for x := startX; x <= endX; x++ {
			if !pointInConvexPolygon(float64(x)+0.5, float64(y)+0.5, pixelCorners) {
				continue
			}
			base := outputImage.NRGBAAt(x, y)
			outputImage.SetNRGBA(x, y, blendNRGBA(base, fillColor, alpha))
		}
	}
	drawGeneratedMapPattern(outputImage, pixelCorners, part, heightScale)
	outlineColor := shadeHeatmapColor(fillColor, 0.52)
	drawPolygonOutline(outputImage, pixelCorners, outlineColor, 0.9)
}

func drawGeneratedMapGround(outputImage *image.NRGBA) {
	if outputImage == nil {
		return
	}
	bounds := outputImage.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		vertical := float64(y-bounds.Min.Y) / float64(maxInt(1, bounds.Dy()-1))
		baseColor := color.NRGBA{
			R: uint8(math.Round(38 + 10*vertical)),
			G: uint8(math.Round(48 + 18*vertical)),
			B: uint8(math.Round(42 + 8*vertical)),
			A: 255,
		}
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			outputImage.SetNRGBA(x, y, baseColor)
		}
	}
}

func drawGeneratedMapShadow(outputImage *image.NRGBA, scene *rbxlHeatmapScene, part rbxlHeatmapMapPart, minY float64, maxY float64) {
	pixelCorners, startX, endX, startY, endY := mapPartPixelPolygon(outputImage, scene, part)
	if len(pixelCorners) < 3 {
		return
	}
	heightScale := 0.0
	if maxY > minY {
		heightScale = (part.CenterY - minY) / (maxY - minY)
	}
	shadowDistance := (part.SizeY*rbxlHeatmapShadowPixelScale + heightScale*14.0 + 2.0) * (1 - part.Transparency*0.65)
	shadowOffset := heatmapPixelPoint{X: shadowDistance * 0.85, Y: shadowDistance * 0.55}
	shadowPolygon := offsetPolygon(pixelCorners, shadowOffset)
	shadowStartX := clampHeatmapInt(startX+int(math.Floor(shadowOffset.X))-2, 0, outputImage.Bounds().Dx()-1)
	shadowEndX := clampHeatmapInt(endX+int(math.Ceil(shadowOffset.X))+2, 0, outputImage.Bounds().Dx()-1)
	shadowStartY := clampHeatmapInt(startY+int(math.Floor(shadowOffset.Y))-2, 0, outputImage.Bounds().Dy()-1)
	shadowEndY := clampHeatmapInt(endY+int(math.Ceil(shadowOffset.Y))+2, 0, outputImage.Bounds().Dy()-1)
	shadowAlpha := 0.18 + heightScale*0.22
	shadowColor := color.NRGBA{R: 5, G: 7, B: 10, A: 255}
	for y := shadowStartY; y <= shadowEndY; y++ {
		for x := shadowStartX; x <= shadowEndX; x++ {
			if !pointInConvexPolygon(float64(x)+0.5, float64(y)+0.5, shadowPolygon) {
				continue
			}
			base := outputImage.NRGBAAt(x, y)
			outputImage.SetNRGBA(x, y, blendNRGBA(base, shadowColor, shadowAlpha))
		}
	}
}

func heatmapSceneBounds(points []rbxlHeatmapPoint, mapParts []rbxlHeatmapMapPart) (float64, float64, float64, float64) {
	hasBounds := false
	minX := 0.0
	maxX := 0.0
	minZ := 0.0
	maxZ := 0.0
	updateBounds := func(x float64, z float64) {
		if !hasBounds {
			minX, maxX = x, x
			minZ, maxZ = z, z
			hasBounds = true
			return
		}
		minX = math.Min(minX, x)
		maxX = math.Max(maxX, x)
		minZ = math.Min(minZ, z)
		maxZ = math.Max(maxZ, z)
	}
	for _, point := range points {
		updateBounds(point.X, point.Z)
	}
	for _, part := range mapParts {
		for _, corner := range mapPartWorldCorners(part) {
			updateBounds(corner.X, corner.Y)
		}
	}
	if !hasBounds {
		return -1, 1, -1, 1
	}
	if minX == maxX {
		minX -= 1
		maxX += 1
	}
	if minZ == maxZ {
		minZ -= 1
		maxZ += 1
	}
	return minX, maxX, minZ, maxZ
}

func heatmapBounds(points []rbxlHeatmapPoint) (float64, float64, float64, float64) {
	minX := points[0].X
	maxX := points[0].X
	minZ := points[0].Z
	maxZ := points[0].Z
	for _, point := range points[1:] {
		minX = math.Min(minX, point.X)
		maxX = math.Max(maxX, point.X)
		minZ = math.Min(minZ, point.Z)
		maxZ = math.Max(maxZ, point.Z)
	}
	if minX == maxX {
		minX -= 1
		maxX += 1
	}
	if minZ == maxZ {
		minZ -= 1
		maxZ += 1
	}
	return minX, maxX, minZ, maxZ
}

func mapHeatmapWorldPoint(
	x float64,
	z float64,
	minX float64,
	maxX float64,
	minZ float64,
	maxZ float64,
	width int,
	height int,
) (float64, float64) {
	contentWidth := float64(width - rbxlHeatmapPadding*2)
	contentHeight := float64(height - rbxlHeatmapPadding*2)
	normalizedX := clampHeatmapFloat64((x-minX)/(maxX-minX), 0, 1)
	normalizedZ := clampHeatmapFloat64((z-minZ)/(maxZ-minZ), 0, 1)
	pixelX := float64(rbxlHeatmapPadding) + normalizedX*contentWidth
	pixelY := float64(height-rbxlHeatmapPadding) - normalizedZ*contentHeight
	return pixelX, pixelY
}

func mapHeatmapPoint(
	x float64,
	z float64,
	minX float64,
	maxX float64,
	minZ float64,
	maxZ float64,
	width int,
	height int,
) (int, int) {
	contentWidth := float64(width - rbxlHeatmapPadding*2)
	contentHeight := float64(height - rbxlHeatmapPadding*2)
	normalizedX := clampHeatmapFloat64((x-minX)/(maxX-minX), 0, 1)
	normalizedZ := clampHeatmapFloat64((z-minZ)/(maxZ-minZ), 0, 1)
	pixelX := int(math.Round(float64(rbxlHeatmapPadding) + normalizedX*contentWidth))
	pixelY := int(math.Round(float64(height-rbxlHeatmapPadding) - normalizedZ*contentHeight))
	if pixelX < 0 {
		pixelX = 0
	}
	if pixelX >= width {
		pixelX = width - 1
	}
	if pixelY < 0 {
		pixelY = 0
	}
	if pixelY >= height {
		pixelY = height - 1
	}
	return pixelX, pixelY
}

func mapPartWorldCorners(part rbxlHeatmapMapPart) []heatmapPixelPoint {
	halfX := part.SizeX / 2
	halfZ := part.SizeZ / 2
	angle := part.YawDegrees * (math.Pi / 180.0)
	cosAngle := math.Cos(angle)
	sinAngle := math.Sin(angle)
	localCorners := []heatmapPixelPoint{
		{X: -halfX, Y: -halfZ},
		{X: halfX, Y: -halfZ},
		{X: halfX, Y: halfZ},
		{X: -halfX, Y: halfZ},
	}
	worldCorners := make([]heatmapPixelPoint, 0, len(localCorners))
	for _, corner := range localCorners {
		worldCorners = append(worldCorners, heatmapPixelPoint{
			X: part.CenterX + corner.X*cosAngle - corner.Y*sinAngle,
			Y: part.CenterZ + corner.X*sinAngle + corner.Y*cosAngle,
		})
	}
	return worldCorners
}

func pointInConvexPolygon(x float64, y float64, polygon []heatmapPixelPoint) bool {
	if len(polygon) < 3 {
		return false
	}
	sign := 0.0
	for index := 0; index < len(polygon); index++ {
		current := polygon[index]
		next := polygon[(index+1)%len(polygon)]
		cross := (next.X-current.X)*(y-current.Y) - (next.Y-current.Y)*(x-current.X)
		if math.Abs(cross) < 0.001 {
			continue
		}
		if sign == 0 {
			sign = math.Copysign(1, cross)
			continue
		}
		if math.Copysign(1, cross) != sign {
			return false
		}
	}
	return true
}

func mapPartPixelPolygon(outputImage *image.NRGBA, scene *rbxlHeatmapScene, part rbxlHeatmapMapPart) ([]heatmapPixelPoint, int, int, int, int) {
	corners := mapPartWorldCorners(part)
	pixelCorners := make([]heatmapPixelPoint, 0, len(corners))
	minPixelX := float64(outputImage.Bounds().Dx())
	maxPixelX := 0.0
	minPixelY := float64(outputImage.Bounds().Dy())
	maxPixelY := 0.0
	for _, corner := range corners {
		pixelX, pixelY := mapHeatmapWorldPoint(
			corner.X,
			corner.Y,
			scene.MinimumX,
			scene.MaximumX,
			scene.MinimumZ,
			scene.MaximumZ,
			outputImage.Bounds().Dx(),
			outputImage.Bounds().Dy(),
		)
		pixelCorners = append(pixelCorners, heatmapPixelPoint{X: pixelX, Y: pixelY})
		minPixelX = math.Min(minPixelX, pixelX)
		maxPixelX = math.Max(maxPixelX, pixelX)
		minPixelY = math.Min(minPixelY, pixelY)
		maxPixelY = math.Max(maxPixelY, pixelY)
	}
	startX := clampHeatmapInt(int(math.Floor(minPixelX)), 0, outputImage.Bounds().Dx()-1)
	endX := clampHeatmapInt(int(math.Ceil(maxPixelX)), 0, outputImage.Bounds().Dx()-1)
	startY := clampHeatmapInt(int(math.Floor(minPixelY)), 0, outputImage.Bounds().Dy()-1)
	endY := clampHeatmapInt(int(math.Ceil(maxPixelY)), 0, outputImage.Bounds().Dy()-1)
	return pixelCorners, startX, endX, startY, endY
}

func offsetPolygon(points []heatmapPixelPoint, offset heatmapPixelPoint) []heatmapPixelPoint {
	shifted := make([]heatmapPixelPoint, 0, len(points))
	for _, point := range points {
		shifted = append(shifted, heatmapPixelPoint{
			X: point.X + offset.X,
			Y: point.Y + offset.Y,
		})
	}
	return shifted
}

func buildHeatmapKernel(radius int) [][]float64 {
	size := radius*2 + 1
	kernel := make([][]float64, size)
	sigma := float64(radius) / 2.4
	if sigma <= 0 {
		sigma = 1
	}
	for y := -radius; y <= radius; y++ {
		row := make([]float64, size)
		for x := -radius; x <= radius; x++ {
			distance := math.Sqrt(float64(x*x + y*y))
			if distance > float64(radius) {
				row[x+radius] = 0
				continue
			}
			gaussian := math.Exp(-(distance * distance) / (2 * sigma * sigma))
			row[x+radius] = gaussian
		}
		kernel[y+radius] = row
	}
	return kernel
}

func heatmapGradientColor(normalized float64) color.NRGBA {
	clamped := clampHeatmapFloat64(normalized, 0, 1)
	stops := []struct {
		position float64
		color    color.NRGBA
	}{
		{position: 0.00, color: color.NRGBA{R: 16, G: 42, B: 110, A: 255}},
		{position: 0.35, color: color.NRGBA{R: 23, G: 164, B: 255, A: 255}},
		{position: 0.62, color: color.NRGBA{R: 255, G: 217, B: 61, A: 255}},
		{position: 0.82, color: color.NRGBA{R: 255, G: 120, B: 32, A: 255}},
		{position: 1.00, color: color.NRGBA{R: 255, G: 48, B: 48, A: 255}},
	}
	for index := 0; index < len(stops)-1; index++ {
		left := stops[index]
		right := stops[index+1]
		if clamped > right.position {
			continue
		}
		local := (clamped - left.position) / (right.position - left.position)
		return color.NRGBA{
			R: uint8(math.Round(float64(left.color.R) + (float64(right.color.R)-float64(left.color.R))*local)),
			G: uint8(math.Round(float64(left.color.G) + (float64(right.color.G)-float64(left.color.G))*local)),
			B: uint8(math.Round(float64(left.color.B) + (float64(right.color.B)-float64(left.color.B))*local)),
			A: 255,
		}
	}
	return stops[len(stops)-1].color
}

func heatmapDiffGradientColor(metricValue float64, normalized float64) color.NRGBA {
	clamped := clampHeatmapFloat64(normalized, 0, 1)
	if metricValue < 0 {
		return interpolateHeatmapColor(clamped, []heatmapGradientStop{
			{position: 0.00, color: color.NRGBA{R: 72, G: 93, B: 140, A: 255}},
			{position: 0.45, color: color.NRGBA{R: 72, G: 170, B: 224, A: 255}},
			{position: 1.00, color: color.NRGBA{R: 120, G: 238, B: 255, A: 255}},
		})
	}
	return interpolateHeatmapColor(clamped, []heatmapGradientStop{
		{position: 0.00, color: color.NRGBA{R: 255, G: 217, B: 61, A: 255}},
		{position: 0.58, color: color.NRGBA{R: 255, G: 120, B: 32, A: 255}},
		{position: 1.00, color: color.NRGBA{R: 255, G: 48, B: 48, A: 255}},
	})
}

func interpolateHeatmapColor(normalized float64, stops []heatmapGradientStop) color.NRGBA {
	clamped := clampHeatmapFloat64(normalized, 0, 1)
	if len(stops) == 0 {
		return color.NRGBA{}
	}
	for index := 0; index < len(stops)-1; index++ {
		left := stops[index]
		right := stops[index+1]
		if clamped > right.position {
			continue
		}
		local := (clamped - left.position) / (right.position - left.position)
		return color.NRGBA{
			R: uint8(math.Round(float64(left.color.R) + (float64(right.color.R)-float64(left.color.R))*local)),
			G: uint8(math.Round(float64(left.color.G) + (float64(right.color.G)-float64(left.color.G))*local)),
			B: uint8(math.Round(float64(left.color.B) + (float64(right.color.B)-float64(left.color.B))*local)),
			A: 255,
		}
	}
	return stops[len(stops)-1].color
}

func blendNRGBA(base color.NRGBA, overlay color.NRGBA, alpha float64) color.NRGBA {
	clampedAlpha := clampHeatmapFloat64(alpha, 0, 1)
	inverseAlpha := 1 - clampedAlpha
	return color.NRGBA{
		R: uint8(math.Round(float64(base.R)*inverseAlpha + float64(overlay.R)*clampedAlpha)),
		G: uint8(math.Round(float64(base.G)*inverseAlpha + float64(overlay.G)*clampedAlpha)),
		B: uint8(math.Round(float64(base.B)*inverseAlpha + float64(overlay.B)*clampedAlpha)),
		A: 255,
	}
}

func drawHeatmapGrid(imageData *image.NRGBA, scene *rbxlHeatmapScene) {
	if imageData == nil || scene == nil {
		return
	}
	lineColor := color.NRGBA{R: 255, G: 255, B: 255, A: 34}
	strongLineColor := color.NRGBA{R: 255, G: 255, B: 255, A: 56}
	for column := 0; column <= scene.ColumnCount; column++ {
		worldX := scene.MinimumX + float64(column)*scene.CellSizeWorld
		pixelX, _ := mapHeatmapWorldPoint(worldX, scene.MinimumZ, scene.MinimumX, scene.MaximumX, scene.MinimumZ, scene.MaximumZ, imageData.Bounds().Dx(), imageData.Bounds().Dy())
		colorForLine := lineColor
		if column == 0 || column == scene.ColumnCount {
			colorForLine = strongLineColor
		}
		drawVerticalLine(imageData, int(math.Round(pixelX)), rbxlHeatmapPadding, imageData.Bounds().Dy()-rbxlHeatmapPadding, colorForLine)
	}
	for row := 0; row <= scene.RowCount; row++ {
		worldZ := scene.MinimumZ + float64(row)*scene.CellSizeWorld
		_, pixelY := mapHeatmapWorldPoint(scene.MinimumX, worldZ, scene.MinimumX, scene.MaximumX, scene.MinimumZ, scene.MaximumZ, imageData.Bounds().Dx(), imageData.Bounds().Dy())
		colorForLine := lineColor
		if row == 0 || row == scene.RowCount {
			colorForLine = strongLineColor
		}
		drawHorizontalLine(imageData, int(math.Round(pixelY)), rbxlHeatmapPadding, imageData.Bounds().Dx()-rbxlHeatmapPadding, colorForLine)
	}
}

func drawPolygonOutline(imageData *image.NRGBA, polygon []heatmapPixelPoint, lineColor color.NRGBA, alpha float64) {
	for index := 0; index < len(polygon); index++ {
		current := polygon[index]
		next := polygon[(index+1)%len(polygon)]
		drawLineSegment(imageData, current, next, lineColor, alpha)
	}
}

func drawLineSegment(imageData *image.NRGBA, start heatmapPixelPoint, end heatmapPixelPoint, lineColor color.NRGBA, alpha float64) {
	steps := int(math.Ceil(math.Max(math.Abs(end.X-start.X), math.Abs(end.Y-start.Y))))
	if steps <= 0 {
		steps = 1
	}
	for step := 0; step <= steps; step++ {
		progress := float64(step) / float64(steps)
		x := int(math.Round(start.X + (end.X-start.X)*progress))
		y := int(math.Round(start.Y + (end.Y-start.Y)*progress))
		if x < 0 || x >= imageData.Bounds().Dx() || y < 0 || y >= imageData.Bounds().Dy() {
			continue
		}
		base := imageData.NRGBAAt(x, y)
		imageData.SetNRGBA(x, y, blendNRGBA(base, lineColor, alpha))
	}
}

func drawGeneratedMapPattern(imageData *image.NRGBA, polygon []heatmapPixelPoint, part rbxlHeatmapMapPart, heightScale float64) {
	if imageData == nil || len(polygon) < 3 {
		return
	}
	partType := strings.ToLower(strings.TrimSpace(part.InstanceType))
	patternColor := shadeHeatmapColor(part.Color, 1.18+heightScale*0.12)
	switch partType {
	case "meshpart", "unionoperation":
		drawPolygonCross(imageData, polygon, patternColor, 0.18)
	case "trusspart":
		drawPolygonHatch(imageData, polygon, patternColor, 0.28, 8)
	case "spawnlocation":
		drawPolygonCross(imageData, polygon, color.NRGBA{R: 160, G: 255, B: 160, A: 255}, 0.35)
	case "seat", "vehicleseat":
		drawPolygonStripe(imageData, polygon, color.NRGBA{R: 180, G: 210, B: 255, A: 255}, 0.28)
	case "wedgepart", "cornerwedgepart":
		drawLineSegment(imageData, polygon[0], polygon[2], patternColor, 0.24)
	}
}

func drawPolygonCross(imageData *image.NRGBA, polygon []heatmapPixelPoint, lineColor color.NRGBA, alpha float64) {
	if len(polygon) < 4 {
		return
	}
	drawLineSegment(imageData, polygon[0], polygon[2], lineColor, alpha)
	drawLineSegment(imageData, polygon[1], polygon[3], lineColor, alpha)
}

func drawPolygonStripe(imageData *image.NRGBA, polygon []heatmapPixelPoint, lineColor color.NRGBA, alpha float64) {
	if len(polygon) < 4 {
		return
	}
	midLeft := midpoint(polygon[0], polygon[3])
	midRight := midpoint(polygon[1], polygon[2])
	drawLineSegment(imageData, midLeft, midRight, lineColor, alpha)
}

func drawPolygonHatch(imageData *image.NRGBA, polygon []heatmapPixelPoint, lineColor color.NRGBA, alpha float64, spacing int) {
	if spacing <= 1 || len(polygon) < 3 {
		return
	}
	minX, maxX, minY, maxY := polygonBounds(polygon)
	for offset := -int(maxY - minY); offset <= int(maxX-minX); offset += spacing {
		for step := 0; step < 2; step++ {
			start := heatmapPixelPoint{X: minX + float64(offset), Y: minY}
			end := heatmapPixelPoint{X: minX + float64(offset) + (maxY - minY), Y: maxY}
			if step == 1 {
				start = heatmapPixelPoint{X: minX + float64(offset), Y: maxY}
				end = heatmapPixelPoint{X: minX + float64(offset) + (maxY - minY), Y: minY}
			}
			drawClippedPatternLine(imageData, polygon, start, end, lineColor, alpha)
		}
	}
}

func drawClippedPatternLine(imageData *image.NRGBA, polygon []heatmapPixelPoint, start heatmapPixelPoint, end heatmapPixelPoint, lineColor color.NRGBA, alpha float64) {
	steps := int(math.Ceil(math.Max(math.Abs(end.X-start.X), math.Abs(end.Y-start.Y))))
	if steps <= 0 {
		steps = 1
	}
	for step := 0; step <= steps; step++ {
		progress := float64(step) / float64(steps)
		x := start.X + (end.X-start.X)*progress
		y := start.Y + (end.Y-start.Y)*progress
		if !pointInConvexPolygon(x, y, polygon) {
			continue
		}
		pixelX := int(math.Round(x))
		pixelY := int(math.Round(y))
		if pixelX < 0 || pixelX >= imageData.Bounds().Dx() || pixelY < 0 || pixelY >= imageData.Bounds().Dy() {
			continue
		}
		base := imageData.NRGBAAt(pixelX, pixelY)
		imageData.SetNRGBA(pixelX, pixelY, blendNRGBA(base, lineColor, alpha))
	}
}

func midpoint(left heatmapPixelPoint, right heatmapPixelPoint) heatmapPixelPoint {
	return heatmapPixelPoint{
		X: (left.X + right.X) / 2,
		Y: (left.Y + right.Y) / 2,
	}
}

func polygonBounds(polygon []heatmapPixelPoint) (float64, float64, float64, float64) {
	minX := polygon[0].X
	maxX := polygon[0].X
	minY := polygon[0].Y
	maxY := polygon[0].Y
	for _, point := range polygon[1:] {
		minX = math.Min(minX, point.X)
		maxX = math.Max(maxX, point.X)
		minY = math.Min(minY, point.Y)
		maxY = math.Max(maxY, point.Y)
	}
	return minX, maxX, minY, maxY
}

func drawVerticalLine(imageData *image.NRGBA, x int, startY int, endY int, lineColor color.NRGBA) {
	bounds := imageData.Bounds()
	if x < bounds.Min.X || x >= bounds.Max.X {
		return
	}
	for y := startY; y < endY; y++ {
		if y < bounds.Min.Y || y >= bounds.Max.Y {
			continue
		}
		base := imageData.NRGBAAt(x, y)
		imageData.SetNRGBA(x, y, blendNRGBA(base, lineColor, float64(lineColor.A)/255.0))
	}
}

func drawHorizontalLine(imageData *image.NRGBA, y int, startX int, endX int, lineColor color.NRGBA) {
	bounds := imageData.Bounds()
	if y < bounds.Min.Y || y >= bounds.Max.Y {
		return
	}
	for x := startX; x < endX; x++ {
		if x < bounds.Min.X || x >= bounds.Max.X {
			continue
		}
		base := imageData.NRGBAAt(x, y)
		imageData.SetNRGBA(x, y, blendNRGBA(base, lineColor, float64(lineColor.A)/255.0))
	}
}

func blankHeatmapPreviewOption() previewDownloadOption {
	blankImage := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	for y := 0; y < 2; y++ {
		for x := 0; x < 2; x++ {
			blankImage.SetNRGBA(x, y, color.NRGBA{R: 10, G: 12, B: 20, A: 255})
		}
	}
	var buffer bytes.Buffer
	_ = png.Encode(&buffer, blankImage)
	return previewDownloadOption{
		labelText: "Original",
		fileName:  "heatmap_placeholder.png",
		bytes:     buffer.Bytes(),
		width:     2,
		height:    2,
	}
}

func clampHeatmapFloat64(value float64, minimum float64, maximum float64) float64 {
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}
	return value
}

func clampHeatmapInt(value int, minimum int, maximum int) int {
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}
	return value
}

func mapPartHeightBounds(parts []rbxlHeatmapMapPart) (float64, float64) {
	if len(parts) == 0 {
		return 0, 0
	}
	minY := parts[0].CenterY
	maxY := parts[0].CenterY
	for _, part := range parts[1:] {
		minY = math.Min(minY, part.CenterY)
		maxY = math.Max(maxY, part.CenterY)
	}
	return minY, maxY
}

func shadeHeatmapColor(base color.NRGBA, factor float64) color.NRGBA {
	clamped := clampHeatmapFloat64(factor, 0, 1.4)
	return color.NRGBA{
		R: uint8(math.Round(clampHeatmapFloat64(float64(base.R)*clamped, 0, 255))),
		G: uint8(math.Round(clampHeatmapFloat64(float64(base.G)*clamped, 0, 255))),
		B: uint8(math.Round(clampHeatmapFloat64(float64(base.B)*clamped, 0, 255))),
		A: 255,
	}
}

func mapPartVisualColor(part rbxlHeatmapMapPart, heightScale float64) color.NRGBA {
	base := shadeHeatmapColor(part.Color, 0.82+heightScale*0.26)
	switch strings.ToLower(strings.TrimSpace(part.InstanceType)) {
	case "meshpart":
		return blendNRGBA(base, color.NRGBA{R: 205, G: 215, B: 222, A: 255}, 0.16)
	case "unionoperation":
		return shadeHeatmapColor(base, 0.94)
	case "trusspart":
		return blendNRGBA(base, color.NRGBA{R: 155, G: 170, B: 180, A: 255}, 0.22)
	case "spawnlocation":
		return blendNRGBA(base, color.NRGBA{R: 110, G: 200, B: 120, A: 255}, 0.34)
	case "seat", "vehicleseat":
		return blendNRGBA(base, color.NRGBA{R: 110, G: 150, B: 210, A: 255}, 0.28)
	default:
		return base
	}
}
