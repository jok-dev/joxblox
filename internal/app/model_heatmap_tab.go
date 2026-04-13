package app

import (
	"fmt"
	"image/color"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"

	"joxblox/internal/debug"
	"joxblox/internal/format"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

const (
	modelHeatmapMaxTrianglesPerMesh = maxMeshPreviewTriangles
)

type modelHeatmapMode string

const (
	modelHeatmapModeTriangles           modelHeatmapMode = "Triangle Heat"
	modelHeatmapModeTexture             modelHeatmapMode = "Texture Heat"
	modelHeatmapModeSizeScaledTriangles modelHeatmapMode = "Size-Scaled Triangle Heat"
	modelHeatmapModeSizeScaledTexture   modelHeatmapMode = "Size-Scaled Texture Heat"
)

type modelHeatmapMeshInstance struct {
	InstancePath string
	MeshRef      heatmapAssetReference
	TextureRefs  []heatmapAssetReference
	CenterX      float64
	CenterY      float64
	CenterZ      float64
	SizeX        float64
	SizeY        float64
	SizeZ        float64
	BasisSizeX   float64
	BasisSizeY   float64
	BasisSizeZ   float64
	YawDegrees   float64
	Rotation     [9]float64
}

type modelHeatmapMeshBounds struct {
	CenterX float64
	CenterY float64
	CenterZ float64
	SizeX   float64
	SizeY   float64
	SizeZ   float64
}

type modelHeatmapResolvedMesh struct {
	Reference     heatmapAssetReference
	Preview       meshPreviewData
	Bounds        modelHeatmapMeshBounds
	TriangleCount uint32
}

type modelHeatmapResolvedTexture struct {
	Reference heatmapAssetReference
	Width     int
	Height    int
	BytesSize int64
}

type modelHeatmapSceneSummary struct {
	MeshPartCount         int
	RenderedMeshPartCount int
	UniqueMeshCount       int
	UniqueTextureCount    int
	MissingMeshRefCount   int
	FailedMeshCount       int
	TriangleCount         uint32
	PreviewTriangleCount  uint32
	TextureBytes          int64
	MaxDensity            float64
	MaxTextureDensity     float64
	MaxHeatValue          float64
	HeatMode              modelHeatmapMode
}

type modelHeatmapBatchInfo struct {
	InstancePath   string
	MeshAssetID    int64
	MeshAssetInput string
	CenterX        float64
	CenterY        float64
	CenterZ        float64
	SizeX          float64
	SizeY          float64
	SizeZ          float64
	BasisSizeX     float64
	BasisSizeY     float64
	BasisSizeZ     float64
	TriangleCount  uint32
	Density        float64
	TextureCount   int
	TextureBytes   int64
	TextureDensity float64
}

type modelHeatmapRenderState struct {
	Preview    meshPreviewData
	BatchInfos []modelHeatmapBatchInfo
	Summary    modelHeatmapSceneSummary
}

func newModelHeatmapTab(window fyne.Window) fyne.CanvasObject {
	selectedFilePath := ""
	var loadToken atomic.Uint64
	var loading atomic.Bool
	var batchInfos []modelHeatmapBatchInfo
	currentHeatSpread := rbxlHeatmapDefaultSpread
	currentHeatMode := modelHeatmapModeTriangles
	var currentRenderState *modelHeatmapRenderState
	viewer := newMeshPreviewWidget()
	viewer.SetFocusCanvas(window.Canvas())
	viewer.Hide()

	filePathLabel := widget.NewLabel("No .rbxl/.rbxm file selected.")
	filePathLabel.Wrapping = fyne.TextTruncate
	statusLabel := widget.NewLabel("Select an .rbxl or .rbxm file to render the model heatmap.")
	statusLabel.Wrapping = fyne.TextWrapWord
	summaryLabel := widget.NewLabel("No model heatmap built.")
	summaryLabel.Wrapping = fyne.TextWrapWord
	legendLabel := widget.NewLabel(modelHeatmapLegendText(currentHeatMode))
	legendLabel.Wrapping = fyne.TextWrapWord
	partInfoLabel := widget.NewLabel("Click a part to view its info.")
	partInfoLabel.Wrapping = fyne.TextWrapWord
	placeholderLabel := widget.NewLabelWithStyle("Select an .rbxl or .rbxm file to preview MeshPart heat in 3D.", fyne.TextAlignCenter, fyne.TextStyle{})
	placeholderLabel.Wrapping = fyne.TextWrapOff
	progressBar := widget.NewProgressBarInfinite()
	progressBar.Hide()
	opacityValueLabel := widget.NewLabel("100%")
	spreadValueLabel := widget.NewLabel(fmt.Sprintf("%.2fx", currentHeatSpread))
	opacitySlider := widget.NewSlider(0.1, 1.0)
	opacitySlider.Step = 0.05
	opacitySlider.SetValue(1.0)
	spreadSlider := widget.NewSlider(rbxlHeatmapMinSpread, rbxlHeatmapMaxSpread)
	spreadSlider.Step = rbxlHeatmapSpreadStep
	spreadSlider.SetValue(currentHeatSpread)
	heatModeSelect := widget.NewSelect([]string{
		string(modelHeatmapModeTriangles),
		string(modelHeatmapModeTexture),
		string(modelHeatmapModeSizeScaledTriangles),
		string(modelHeatmapModeSizeScaledTexture),
	}, nil)
	heatModeSelect.SetSelected(string(currentHeatMode))
	backgroundSelect := widget.NewSelect([]string{expandedBackgroundBlack, expandedBackgroundWhite}, nil)
	backgroundSelect.SetSelected(expandedBackgroundBlack)
	backgroundSelect.OnChanged = func(mode string) {
		viewer.SetBackground(zoomPanBackgroundColor(mode))
	}
	viewer.SetBackground(zoomPanBackgroundColor(backgroundSelect.Selected))
	viewer.OnBatchTapped = func(batchIndex int) {
		if batchIndex < 0 || batchIndex >= len(batchInfos) {
			return
		}
		partInfoLabel.SetText(formatModelHeatmapPartInfo(batchInfos[batchIndex], currentHeatMode))
	}

	setPreviewReady := func(previewData meshPreviewData, infos []modelHeatmapBatchInfo, summary modelHeatmapSceneSummary) {
		batchInfos = infos
		viewer.SetData(previewData)
		viewer.Show()
		placeholderLabel.Hide()
		partInfoLabel.SetText("Click a part to view its info.")
		summaryLabel.SetText(formatModelHeatmapSummary(summary))
	}
	setPreviewEmpty := func(message string) {
		currentRenderState = nil
		viewer.Clear()
		viewer.Hide()
		placeholderLabel.SetText(message)
		placeholderLabel.Show()
	}
	rerenderPreview := func(statusText string) {
		if currentRenderState == nil {
			return
		}
		if strings.TrimSpace(statusText) != "" {
			statusLabel.SetText(statusText)
		}
		previewData, infos, summary, buildErr := buildModelHeatmapPreviewDataFromState(currentRenderState, currentHeatSpread, currentHeatMode)
		if buildErr != nil {
			setPreviewEmpty("No model heatmap preview could be built.")
			statusLabel.SetText(fmt.Sprintf("Build failed: %s", buildErr.Error()))
			return
		}
		batchInfos = infos
		viewer.UpdateSceneColors(previewData)
		viewer.Show()
		placeholderLabel.Hide()
		if selectedBatch := viewer.SelectedBatch(); selectedBatch >= 0 && selectedBatch < len(batchInfos) {
			partInfoLabel.SetText(formatModelHeatmapPartInfo(batchInfos[selectedBatch], currentHeatMode))
		} else {
			partInfoLabel.SetText("Click a part to view its info.")
		}
		summaryLabel.SetText(formatModelHeatmapSummary(summary))
		statusLabel.SetText(meshPreviewControlsText())
	}

	var cancelLoadButton *widget.Button
	setBusy := func(busy bool) {
		loading.Store(busy)
		if busy {
			progressBar.Show()
			progressBar.Start()
			if cancelLoadButton != nil {
				cancelLoadButton.Show()
			}
			return
		}
		progressBar.Stop()
		progressBar.Hide()
		if cancelLoadButton != nil {
			cancelLoadButton.Hide()
		}
	}

	workspaceOnlyCheck := widget.NewCheck("Workspace only for .rbxl", nil)
	workspaceOnlyCheck.SetChecked(true)
	opacitySlider.OnChanged = func(value float64) {
		viewer.SetOpacity(value)
		opacityValueLabel.SetText(fmt.Sprintf("%d%%", int(math.Round(format.Clamp(value, 0.1, 1.0)*100))))
	}
	spreadSlider.OnChanged = func(value float64) {
		currentHeatSpread = format.Clamp(value, rbxlHeatmapMinSpread, rbxlHeatmapMaxSpread)
		spreadValueLabel.SetText(fmt.Sprintf("%.2fx", currentHeatSpread))
		if !loading.Load() {
			rerenderPreview("Updating heat spread...")
		}
	}
	heatModeSelect.OnChanged = func(value string) {
		currentHeatMode = normalizedModelHeatmapMode(modelHeatmapMode(strings.TrimSpace(value)))
		legendLabel.SetText(modelHeatmapLegendText(currentHeatMode))
		if !loading.Load() {
			rerenderPreview("Updating heat mode...")
		}
	}

	cancelActiveLoad := func() {
		if !loading.Load() {
			return
		}
		loadToken.Add(1)
		setBusy(false)
		statusLabel.SetText("Loading canceled")
	}

	loadModelHeatmap := func(filePath string) {
		trimmedPath := strings.TrimSpace(filePath)
		if !isRobloxDOMFilePath(trimmedPath) {
			statusLabel.SetText("Only .rbxl/.rbxm")
			return
		}

		selectedFilePath = trimmedPath
		filePathLabel.SetText(selectedFilePath)
		setBusy(true)
		setPreviewEmpty("Loading model heatmap...")
		summaryLabel.SetText("No model heatmap built.")
		statusLabel.SetText("Extracting MeshParts...")

		var pathPrefixes []string
		if workspaceOnlyCheck.Checked && strings.EqualFold(filepath.Ext(trimmedPath), ".rbxl") {
			pathPrefixes = []string{"Workspace"}
		}

		token := loadToken.Add(1)
		heatSpread := currentHeatSpread
		heatMode := currentHeatMode
		go func(expectedToken uint64, sourcePath string, prefixes []string, spread float64, mode modelHeatmapMode) {
			isCanceled := func() bool {
				return loadToken.Load() != expectedToken
			}

			var (
				refs     []positionedRustyAssetToolResult
				mapParts []mapRenderPartRustyAssetToolResult
				refsErr  error
				mapErr   error
			)
			var waitGroup sync.WaitGroup
			waitGroup.Add(2)
			go func() {
				defer waitGroup.Done()
				refs, refsErr = extractPositionedRefsWithRustyAssetTool(sourcePath, prefixes, nil)
			}()
			go func() {
				defer waitGroup.Done()
				mapParts, mapErr = extractMapRenderPartsWithRustyAssetTool(sourcePath, prefixes, nil)
			}()
			waitGroup.Wait()
			if isCanceled() {
				return
			}
			if refsErr != nil {
				fyne.Do(func() {
					if isCanceled() {
						return
					}
					setBusy(false)
					setPreviewEmpty("Failed to load model heatmap.")
					statusLabel.SetText(fmt.Sprintf("Load failed: %s", refsErr.Error()))
				})
				return
			}
			if mapErr != nil {
				fyne.Do(func() {
					if isCanceled() {
						return
					}
					setBusy(false)
					setPreviewEmpty("Failed to load model heatmap.")
					statusLabel.SetText(fmt.Sprintf("Map extraction failed: %s", mapErr.Error()))
				})
				return
			}

			instances := buildModelHeatmapInstances(mapParts, refs)
			if len(instances) == 0 {
				fyne.Do(func() {
					if isCanceled() {
						return
					}
					setBusy(false)
					setPreviewEmpty("No MeshParts with mesh content were found.")
					statusLabel.SetText("No renderable MeshParts found")
				})
				return
			}

			uniqueRefs := uniqueModelHeatmapReferences(instances)
			resolvedMeshes := resolveModelHeatmapMeshes(uniqueRefs, func(done int, total int) {
				fyne.Do(func() {
					if isCanceled() {
						return
					}
					statusLabel.SetText(fmt.Sprintf("Resolving mesh assets... %d/%d", done, total))
				})
			}, isCanceled)
			if isCanceled() {
				return
			}

			uniqueTextureRefs := uniqueModelHeatmapTextureReferences(instances)
			debug.Logf("model heatmap: %d unique texture refs across %d mesh instances", len(uniqueTextureRefs), len(instances))
			resolvedTextures := resolveModelHeatmapTextures(uniqueTextureRefs, func(done int, total int) {
				fyne.Do(func() {
					if isCanceled() {
						return
					}
					statusLabel.SetText(fmt.Sprintf("Resolving texture assets... %d/%d", done, total))
				})
			}, isCanceled)
			if isCanceled() {
				return
			}
			resolvedTextureBytes := int64(0)
			resolvedTextureHits := 0
			for _, textureData := range resolvedTextures {
				if textureData.BytesSize > 0 {
					resolvedTextureHits++
					resolvedTextureBytes += textureData.BytesSize
				}
			}
			debug.Logf("model heatmap: resolved %d/%d textures (%d bytes)", resolvedTextureHits, len(uniqueTextureRefs), resolvedTextureBytes)

			renderState, buildErr := buildModelHeatmapRenderState(instances, resolvedMeshes, resolvedTextures)
			if buildErr != nil {
				fyne.Do(func() {
					if isCanceled() {
						return
					}
					setBusy(false)
					setPreviewEmpty("No model heatmap preview could be built.")
					statusLabel.SetText(fmt.Sprintf("Build failed: %s", buildErr.Error()))
				})
				return
			}
			previewData, infos, summary, buildErr := buildModelHeatmapPreviewDataFromState(renderState, spread, mode)
			if buildErr != nil {
				fyne.Do(func() {
					if isCanceled() {
						return
					}
					setBusy(false)
					setPreviewEmpty("No model heatmap preview could be built.")
					statusLabel.SetText(fmt.Sprintf("Build failed: %s", buildErr.Error()))
				})
				return
			}

			fyne.Do(func() {
				if isCanceled() {
					return
				}
				currentRenderState = renderState
				setPreviewReady(previewData, infos, summary)
				setBusy(false)
				statusLabel.SetText(meshPreviewControlsText())
			})
		}(token, trimmedPath, pathPrefixes, heatSpread, heatMode)
	}

	browseButton := widget.NewButtonWithIcon("Choose File", theme.FolderOpenIcon(), func() {
		pickRBXLSource(window, loadModelHeatmap, func(err error) {
			statusLabel.SetText(fmt.Sprintf("Pick failed: %s", err.Error()))
		})
	})
	browseButton.Importance = widget.HighImportance
	cancelLoadButton = widget.NewButton("Cancel", func() {
		cancelActiveLoad()
	})
	cancelLoadButton.Importance = widget.DangerImportance
	cancelLoadButton.Hide()

	dropLabel := canvas.NewText("Drag RBXL/RBXM File Here", theme.ForegroundColor())
	dropLabel.Alignment = fyne.TextAlignCenter
	dropLabel.TextSize = 28
	dropLabel.TextStyle = fyne.TextStyle{Bold: true}

	previewStack := container.NewMax(
		container.NewCenter(placeholderLabel),
		container.NewPadded(viewer),
	)
	previewCard := container.NewBorder(
		container.NewVBox(
			container.NewHBox(
				widget.NewLabel("Background:"),
				container.NewGridWrap(fyne.NewSize(120, 36), backgroundSelect),
				widget.NewLabel("Heat Mode:"),
				container.NewGridWrap(fyne.NewSize(280, 36), heatModeSelect),
				widget.NewLabel("Opacity:"),
				container.NewGridWrap(fyne.NewSize(220, 36), opacitySlider),
				opacityValueLabel,
				widget.NewLabel("Heat Spread:"),
				container.NewGridWrap(fyne.NewSize(220, 36), spreadSlider),
				spreadValueLabel,
				layout.NewSpacer(),
			),
			widget.NewLabel(meshPreviewControlsText()),
		),
		nil,
		nil,
		nil,
		previewStack,
	)

	controls := container.NewVBox(
		container.NewCenter(dropLabel),
		container.NewCenter(container.NewHBox(progressBar, cancelLoadButton)),
		container.NewCenter(container.NewHBox(browseButton, workspaceOnlyCheck)),
		filePathLabel,
	)

	return container.NewBorder(
		controls,
		statusLabel,
		nil,
		nil,
		container.NewBorder(
			container.NewVBox(
				summaryLabel,
				legendLabel,
			),
			partInfoLabel,
			nil,
			nil,
			previewCard,
		),
	)
}

func buildModelHeatmapInstances(mapParts []mapRenderPartRustyAssetToolResult, refs []positionedRustyAssetToolResult) []modelHeatmapMeshInstance {
	meshRefsByPath := map[string][]heatmapAssetReference{}
	textureRefsByPath := map[string][]heatmapAssetReference{}
	textureSeenKeysByPath := map[string]map[string]struct{}{}
	for _, ref := range refs {
		propertyName := strings.ToLower(strings.TrimSpace(ref.PropertyName))
		originalInstanceType := ref.InstanceType
		instancePath, instanceType := reportGenerationRefTarget(ref)
		instancePath = strings.TrimSpace(instancePath)
		if instancePath == "" {
			continue
		}
		if normalizeReportGenerationInstanceType(instanceType) != "meshpart" {
			continue
		}
		reference := heatmapAssetReference{
			AssetID:    ref.ID,
			AssetInput: strings.TrimSpace(ref.RawContent),
		}
		switch {
		case isReportGenerationMeshContentProperty(propertyName):
			meshRefsByPath[instancePath] = append(meshRefsByPath[instancePath], reference)
		case isReportGenerationTextureContentProperty(propertyName),
			isReportGenerationSurfaceAppearanceProperty(propertyName, originalInstanceType):
			refKey := scanAssetReferenceKey(reference.AssetID, reference.AssetInput)
			if refKey == "" || refKey == "0" {
				continue
			}
			seen, ok := textureSeenKeysByPath[instancePath]
			if !ok {
				seen = map[string]struct{}{}
				textureSeenKeysByPath[instancePath] = seen
			}
			if _, alreadySeen := seen[refKey]; alreadySeen {
				continue
			}
			seen[refKey] = struct{}{}
			textureRefsByPath[instancePath] = append(textureRefsByPath[instancePath], reference)
		}
	}

	meshPartOccurrence := map[string]int{}
	instances := make([]modelHeatmapMeshInstance, 0, len(mapParts))
	for _, part := range mapParts {
		if normalizeReportGenerationInstanceType(part.InstanceType) != "meshpart" {
			continue
		}
		if part.CenterX == nil || part.CenterY == nil || part.CenterZ == nil {
			continue
		}
		if part.SizeX == nil || part.SizeY == nil || part.SizeZ == nil {
			continue
		}
		instancePath := strings.TrimSpace(part.InstancePath)
		refsForPath := meshRefsByPath[instancePath]
		if len(refsForPath) == 0 {
			continue
		}
		occurrenceIndex := meshPartOccurrence[instancePath]
		meshPartOccurrence[instancePath] = occurrenceIndex + 1
		meshReference := refsForPath[0]
		if occurrenceIndex < len(refsForPath) {
			meshReference = refsForPath[occurrenceIndex]
		}
		textureRefsForPart := []heatmapAssetReference(nil)
		if textureRefs := textureRefsByPath[instancePath]; len(textureRefs) > 0 {
			textureRefsForPart = append([]heatmapAssetReference(nil), textureRefs...)
		}
		sizeX := math.Abs(*part.SizeX)
		sizeY := math.Abs(*part.SizeY)
		sizeZ := math.Abs(*part.SizeZ)
		if sizeX <= 0 || sizeY <= 0 || sizeZ <= 0 {
			continue
		}
		instances = append(instances, modelHeatmapMeshInstance{
			InstancePath: instancePath,
			MeshRef:      meshReference,
			TextureRefs:  textureRefsForPart,
			CenterX:      *part.CenterX,
			CenterY:      *part.CenterY,
			CenterZ:      *part.CenterZ,
			SizeX:        sizeX,
			SizeY:        sizeY,
			SizeZ:        sizeZ,
			BasisSizeX:   positiveModelHeatmapBasisSize(part.BasisSizeX, sizeX),
			BasisSizeY:   positiveModelHeatmapBasisSize(part.BasisSizeY, sizeY),
			BasisSizeZ:   positiveModelHeatmapBasisSize(part.BasisSizeZ, sizeZ),
			YawDegrees:   dereferenceModelHeatmapFloat(part.YawDegrees),
			Rotation:     modelHeatmapRotationFromPart(part),
		})
	}
	return instances
}

func uniqueModelHeatmapReferences(instances []modelHeatmapMeshInstance) []heatmapAssetReference {
	unique := map[string]heatmapAssetReference{}
	for _, instance := range instances {
		key := scanAssetReferenceKey(instance.MeshRef.AssetID, instance.MeshRef.AssetInput)
		unique[key] = instance.MeshRef
	}
	references := make([]heatmapAssetReference, 0, len(unique))
	for _, reference := range unique {
		references = append(references, reference)
	}
	return references
}

func uniqueModelHeatmapTextureReferences(instances []modelHeatmapMeshInstance) []heatmapAssetReference {
	unique := map[string]heatmapAssetReference{}
	for _, instance := range instances {
		for _, textureRef := range instance.TextureRefs {
			if textureRef.AssetID == 0 && strings.TrimSpace(textureRef.AssetInput) == "" {
				continue
			}
			key := scanAssetReferenceKey(textureRef.AssetID, textureRef.AssetInput)
			unique[key] = textureRef
		}
	}
	references := make([]heatmapAssetReference, 0, len(unique))
	for _, reference := range unique {
		references = append(references, reference)
	}
	return references
}

func resolveModelHeatmapMeshes(references []heatmapAssetReference, onProgress func(done int, total int), shouldCancel func() bool) map[string]modelHeatmapResolvedMesh {
	if len(references) == 0 {
		return map[string]modelHeatmapResolvedMesh{}
	}

	jobs := make(chan heatmapAssetReference, len(references))
	for _, reference := range references {
		jobs <- reference
	}
	close(jobs)

	resolved := make(map[string]modelHeatmapResolvedMesh, len(references))
	var resolvedMutex sync.Mutex
	var completed atomic.Int64
	workerCount := min(runtime.NumCPU(), len(references))
	if workerCount <= 0 {
		workerCount = 1
	}

	var waitGroup sync.WaitGroup
	for workerIndex := 0; workerIndex < workerCount; workerIndex++ {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			for reference := range jobs {
				if shouldCancel != nil && shouldCancel() {
					return
				}

				key := scanAssetReferenceKey(reference.AssetID, reference.AssetInput)
				resolvedMesh := modelHeatmapResolvedMesh{Reference: reference}
				previewResult, previewErr := loadAssetStatsPreviewForReference(reference.AssetID, reference.AssetInput)
				if previewErr == nil && previewResult != nil && len(previewResult.DownloadBytes) > 0 {
					if meshInfo, meshErr := parseMeshHeader(previewResult.DownloadBytes); meshErr == nil {
						resolvedMesh.TriangleCount = meshInfo.NumFaces
					}
					if previewData, meshPreviewErr := extractMeshPreviewWithRustyAssetToolFromBytesWithLimit(previewResult.DownloadBytes, modelHeatmapMaxTrianglesPerMesh); meshPreviewErr == nil {
						resolvedMesh.Preview = previewData
						resolvedMesh.Bounds = computeModelHeatmapMeshBounds(previewData.RawPositions)
						if resolvedMesh.TriangleCount == 0 {
							resolvedMesh.TriangleCount = previewData.TriangleCount
						}
					}
				}

				resolvedMutex.Lock()
				resolved[key] = resolvedMesh
				resolvedMutex.Unlock()

				if onProgress != nil && (shouldCancel == nil || !shouldCancel()) {
					onProgress(int(completed.Add(1)), len(references))
				}
			}
		}()
	}
	waitGroup.Wait()
	return resolved
}

func resolveModelHeatmapTextures(references []heatmapAssetReference, onProgress func(done int, total int), shouldCancel func() bool) map[string]modelHeatmapResolvedTexture {
	if len(references) == 0 {
		return map[string]modelHeatmapResolvedTexture{}
	}

	jobs := make(chan heatmapAssetReference, len(references))
	for _, reference := range references {
		jobs <- reference
	}
	close(jobs)

	resolved := make(map[string]modelHeatmapResolvedTexture, len(references))
	var resolvedMutex sync.Mutex
	var completed atomic.Int64
	workerCount := min(runtime.NumCPU(), len(references))
	if workerCount <= 0 {
		workerCount = 1
	}

	var waitGroup sync.WaitGroup
	for workerIndex := 0; workerIndex < workerCount; workerIndex++ {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			for reference := range jobs {
				if shouldCancel != nil && shouldCancel() {
					return
				}

				key := scanAssetReferenceKey(reference.AssetID, reference.AssetInput)
				resolvedTexture := modelHeatmapResolvedTexture{Reference: reference}
				previewResult, previewErr := loadAssetStatsPreviewForReference(reference.AssetID, reference.AssetInput)
				if previewErr == nil && previewResult != nil {
					imageSource := previewResult.Image
					if imageSource == nil || imageSource.Width <= 0 || imageSource.Height <= 0 || imageSource.BytesSize <= 0 {
						imageSource = previewResult.Stats
					}
					if imageSource != nil && imageSource.Width > 0 && imageSource.Height > 0 {
						resolvedTexture.Width = imageSource.Width
						resolvedTexture.Height = imageSource.Height
						resolvedTexture.BytesSize = int64(imageSource.BytesSize)
					}
				}

				resolvedMutex.Lock()
				resolved[key] = resolvedTexture
				resolvedMutex.Unlock()

				if onProgress != nil && (shouldCancel == nil || !shouldCancel()) {
					onProgress(int(completed.Add(1)), len(references))
				}
			}
		}()
	}
	waitGroup.Wait()
	return resolved
}

func buildModelHeatmapPreviewData(instances []modelHeatmapMeshInstance, resolved map[string]modelHeatmapResolvedMesh) (meshPreviewData, []modelHeatmapBatchInfo, modelHeatmapSceneSummary, error) {
	return buildModelHeatmapPreviewDataWithMode(instances, resolved, nil, rbxlHeatmapDefaultSpread, modelHeatmapModeSizeScaledTriangles)
}

func buildModelHeatmapPreviewDataWithSpread(instances []modelHeatmapMeshInstance, resolved map[string]modelHeatmapResolvedMesh, heatSpread float64) (meshPreviewData, []modelHeatmapBatchInfo, modelHeatmapSceneSummary, error) {
	return buildModelHeatmapPreviewDataWithMode(instances, resolved, nil, heatSpread, modelHeatmapModeSizeScaledTriangles)
}

func buildModelHeatmapPreviewDataWithMode(instances []modelHeatmapMeshInstance, resolved map[string]modelHeatmapResolvedMesh, textures map[string]modelHeatmapResolvedTexture, heatSpread float64, heatMode modelHeatmapMode) (meshPreviewData, []modelHeatmapBatchInfo, modelHeatmapSceneSummary, error) {
	renderState, err := buildModelHeatmapRenderState(instances, resolved, textures)
	if err != nil {
		return meshPreviewData{}, nil, modelHeatmapSceneSummary{}, err
	}
	return buildModelHeatmapPreviewDataFromState(renderState, heatSpread, heatMode)
}

func buildModelHeatmapRenderState(instances []modelHeatmapMeshInstance, resolved map[string]modelHeatmapResolvedMesh, textures map[string]modelHeatmapResolvedTexture) (*modelHeatmapRenderState, error) {
	summary := modelHeatmapSceneSummary{
		MeshPartCount: len(instances),
	}
	if len(instances) == 0 {
		return nil, fmt.Errorf("no MeshParts found")
	}

	type preparedInstance struct {
		Instance       modelHeatmapMeshInstance
		Mesh           modelHeatmapResolvedMesh
		Density        float64
		TextureCount   int
		TextureBytes   int64
		TextureDensity float64
	}

	prepared := make([]preparedInstance, 0, len(instances))
	uniqueRenderedMeshes := map[string]struct{}{}
	uniqueRenderedTextures := map[string]struct{}{}
	for _, instance := range instances {
		key := scanAssetReferenceKey(instance.MeshRef.AssetID, instance.MeshRef.AssetInput)
		mesh, found := resolved[key]
		if !found || mesh.TriangleCount == 0 {
			summary.FailedMeshCount++
			continue
		}
		if !mesh.Preview.hasRenderableGeometry() {
			summary.FailedMeshCount++
			continue
		}
		density := modelHeatmapTriangleDensity(instance.SizeX, instance.SizeY, instance.SizeZ, mesh.TriangleCount)
		summary.MaxDensity = maxModelHeatmapFloat(summary.MaxDensity, density)

		textureCount := 0
		var textureBytes int64
		for _, textureRef := range instance.TextureRefs {
			if textureRef.AssetID == 0 && strings.TrimSpace(textureRef.AssetInput) == "" {
				continue
			}
			textureKey := scanAssetReferenceKey(textureRef.AssetID, textureRef.AssetInput)
			textureData, textureFound := textures[textureKey]
			if !textureFound || textureData.BytesSize <= 0 {
				continue
			}
			textureCount++
			textureBytes += textureData.BytesSize
			uniqueRenderedTextures[textureKey] = struct{}{}
		}
		textureDensity := modelHeatmapTextureDensity(instance.SizeX, instance.SizeY, instance.SizeZ, textureBytes)
		summary.MaxTextureDensity = maxModelHeatmapFloat(summary.MaxTextureDensity, textureDensity)

		prepared = append(prepared, preparedInstance{
			Instance:       instance,
			Mesh:           mesh,
			Density:        density,
			TextureCount:   textureCount,
			TextureBytes:   textureBytes,
			TextureDensity: textureDensity,
		})
		summary.RenderedMeshPartCount++
		summary.TriangleCount += mesh.TriangleCount
		summary.PreviewTriangleCount += uint32(len(mesh.Preview.RawIndices) / 3)
		summary.TextureBytes += textureBytes
		uniqueRenderedMeshes[key] = struct{}{}
	}
	summary.UniqueMeshCount = len(uniqueRenderedMeshes)
	summary.UniqueTextureCount = len(uniqueRenderedTextures)
	summary.MissingMeshRefCount = summary.MeshPartCount - summary.RenderedMeshPartCount - summary.FailedMeshCount
	if len(prepared) == 0 {
		return nil, fmt.Errorf("no MeshParts could be rendered")
	}

	batches := make([]meshPreviewBatchData, 0, len(prepared))
	debugInfos := make([]debugBatchInfo, 0, len(prepared))
	resultInfos := make([]modelHeatmapBatchInfo, 0, len(prepared))
	for _, preparedInstance := range prepared {
		batch := meshPreviewBatchData{}
		appendModelHeatmapMeshInstanceGeometry(&batch, preparedInstance.Instance, preparedInstance.Mesh)
		if len(batch.RawPositions) == 0 {
			continue
		}
		batches = append(batches, batch)
		debugInfos = append(debugInfos, debugBatchInfo{
			Instance: preparedInstance.Instance,
			Bounds:   preparedInstance.Mesh.Bounds,
		})
		resultInfos = append(resultInfos, modelHeatmapBatchInfo{
			InstancePath:   preparedInstance.Instance.InstancePath,
			MeshAssetID:    preparedInstance.Instance.MeshRef.AssetID,
			MeshAssetInput: preparedInstance.Instance.MeshRef.AssetInput,
			CenterX:        preparedInstance.Instance.CenterX,
			CenterY:        preparedInstance.Instance.CenterY,
			CenterZ:        preparedInstance.Instance.CenterZ,
			SizeX:          preparedInstance.Instance.SizeX,
			SizeY:          preparedInstance.Instance.SizeY,
			SizeZ:          preparedInstance.Instance.SizeZ,
			BasisSizeX:     preparedInstance.Instance.BasisSizeX,
			BasisSizeY:     preparedInstance.Instance.BasisSizeY,
			BasisSizeZ:     preparedInstance.Instance.BasisSizeZ,
			TriangleCount:  preparedInstance.Mesh.TriangleCount,
			Density:        preparedInstance.Density,
			TextureCount:   preparedInstance.TextureCount,
			TextureBytes:   preparedInstance.TextureBytes,
			TextureDensity: preparedInstance.TextureDensity,
		})
	}
	if len(batches) == 0 {
		return nil, fmt.Errorf("no scene batches were built")
	}

	debugExportModelHeatmapOBJ(batches, debugInfos)
	normalizeMeshPreviewSceneBatches(batches)
	previewData, buildErr := buildMeshPreviewSceneData(batches, summary.TriangleCount, summary.PreviewTriangleCount)
	if buildErr != nil {
		return nil, buildErr
	}
	return &modelHeatmapRenderState{
		Preview:    previewData,
		BatchInfos: resultInfos,
		Summary:    summary,
	}, nil
}

func buildModelHeatmapPreviewDataFromState(renderState *modelHeatmapRenderState, heatSpread float64, heatMode modelHeatmapMode) (meshPreviewData, []modelHeatmapBatchInfo, modelHeatmapSceneSummary, error) {
	if renderState == nil {
		return meshPreviewData{}, nil, modelHeatmapSceneSummary{}, fmt.Errorf("model heatmap state is empty")
	}
	if len(renderState.Preview.Batches) == 0 || len(renderState.BatchInfos) == 0 {
		return meshPreviewData{}, nil, renderState.Summary, fmt.Errorf("model heatmap preview is empty")
	}
	if len(renderState.Preview.Batches) != len(renderState.BatchInfos) {
		return meshPreviewData{}, nil, renderState.Summary, fmt.Errorf("model heatmap preview batch count mismatch")
	}

	summary := renderState.Summary
	summary.HeatMode = normalizedModelHeatmapMode(heatMode)
	summary.MaxHeatValue = 0
	heatValues := make([]float64, len(renderState.BatchInfos))
	for i, info := range renderState.BatchInfos {
		heatValues[i] = modelHeatmapValueFromInfo(summary.HeatMode, info)
		summary.MaxHeatValue = maxModelHeatmapFloat(summary.MaxHeatValue, heatValues[i])
	}

	previewData := cloneMeshPreviewDataWithSharedGeometry(renderState.Preview)
	for i, batch := range previewData.Batches {
		heatColor := modelHeatmapColor(heatValues[i], summary.MaxHeatValue, heatSpread)
		applyModelHeatmapBatchColor(&batch, heatColor)
		previewData.Batches[i] = batch
	}
	return previewData, renderState.BatchInfos, summary, nil
}

func appendModelHeatmapMeshInstance(batch *meshPreviewBatchData, instance modelHeatmapMeshInstance, mesh modelHeatmapResolvedMesh, heatColor color.NRGBA) {
	appendModelHeatmapMeshInstanceGeometry(batch, instance, mesh)
	applyModelHeatmapBatchColor(batch, heatColor)
}

func appendModelHeatmapMeshInstanceGeometry(batch *meshPreviewBatchData, instance modelHeatmapMeshInstance, mesh modelHeatmapResolvedMesh) {
	if batch == nil {
		return
	}
	baseVertexIndex := uint32(len(batch.RawPositions) / 3)
	bounds := mesh.Bounds
	rotation := instance.Rotation
	if rotation == ([9]float64{}) {
		rotation = modelHeatmapYawRotation(instance.YawDegrees)
	}

	for i := 0; i < len(mesh.Preview.RawPositions); i += 3 {
		localX := modelHeatmapAxisTransform(float64(mesh.Preview.RawPositions[i]), bounds.CenterX, bounds.SizeX, instance.SizeX)
		localY := modelHeatmapAxisTransform(float64(mesh.Preview.RawPositions[i+1]), bounds.CenterY, bounds.SizeY, instance.SizeY)
		localZ := modelHeatmapAxisTransform(float64(mesh.Preview.RawPositions[i+2]), bounds.CenterZ, bounds.SizeZ, instance.SizeZ)
		rotatedX, rotatedY, rotatedZ := applyModelHeatmapRotation(rotation, localX, localY, localZ)
		batch.RawPositions = append(batch.RawPositions,
			float32(rotatedX+instance.CenterX),
			float32(rotatedY+instance.CenterY),
			float32(rotatedZ+instance.CenterZ),
		)
	}
	for _, index := range mesh.Preview.RawIndices {
		batch.RawIndices = append(batch.RawIndices, baseVertexIndex+index)
	}
}

func applyModelHeatmapBatchColor(batch *meshPreviewBatchData, heatColor color.NRGBA) {
	if batch == nil {
		return
	}
	vertexCount := len(batch.RawPositions) / 3
	if vertexCount <= 0 {
		batch.RawColors = nil
		return
	}
	batch.RawColors = make([]uint8, vertexCount*4)
	for i := 0; i < vertexCount; i++ {
		base := i * 4
		batch.RawColors[base+0] = heatColor.R
		batch.RawColors[base+1] = heatColor.G
		batch.RawColors[base+2] = heatColor.B
		batch.RawColors[base+3] = 255
	}
}

func modelHeatmapRotationFromPart(part mapRenderPartRustyAssetToolResult) [9]float64 {
	if part.RotationXX == nil || part.RotationXY == nil || part.RotationXZ == nil ||
		part.RotationYX == nil || part.RotationYY == nil || part.RotationYZ == nil ||
		part.RotationZX == nil || part.RotationZY == nil || part.RotationZZ == nil {
		return modelHeatmapYawRotation(dereferenceModelHeatmapFloat(part.YawDegrees))
	}
	return [9]float64{
		*part.RotationXX, *part.RotationXY, *part.RotationXZ,
		*part.RotationYX, *part.RotationYY, *part.RotationYZ,
		*part.RotationZX, *part.RotationZY, *part.RotationZZ,
	}
}

func modelHeatmapYawRotation(yawDegrees float64) [9]float64 {
	yawRadians := yawDegrees * math.Pi / 180.0
	sinYaw := math.Sin(yawRadians)
	cosYaw := math.Cos(yawRadians)
	return [9]float64{
		cosYaw, 0, sinYaw,
		0, 1, 0,
		-sinYaw, 0, cosYaw,
	}
}

func applyModelHeatmapRotation(rotation [9]float64, localX float64, localY float64, localZ float64) (float64, float64, float64) {
	return rotation[0]*localX + rotation[1]*localY + rotation[2]*localZ,
		rotation[3]*localX + rotation[4]*localY + rotation[5]*localZ,
		rotation[6]*localX + rotation[7]*localY + rotation[8]*localZ
}

func modelHeatmapAxisTransform(vertex float64, boundsCenter float64, boundsExtent float64, instanceSize float64) float64 {
	if boundsExtent < 1e-6 {
		return 0
	}
	return ((vertex - boundsCenter) / boundsExtent) * instanceSize
}

func dereferenceModelHeatmapFloat(value *float64) float64 {
	if value == nil {
		return 0
	}
	return *value
}

func positiveModelHeatmapBasisSize(value *float64, fallback float64) float64 {
	if value != nil && *value > 0 {
		return *value
	}
	return fallback
}

func computeModelHeatmapMeshBounds(positions []float32) modelHeatmapMeshBounds {
	if len(positions) < 3 {
		return modelHeatmapMeshBounds{SizeX: 1, SizeY: 1, SizeZ: 1}
	}
	minX := float64(positions[0])
	maxX := minX
	minY := float64(positions[1])
	maxY := minY
	minZ := float64(positions[2])
	maxZ := minZ
	for i := 3; i+2 < len(positions); i += 3 {
		x := float64(positions[i])
		y := float64(positions[i+1])
		z := float64(positions[i+2])
		if x < minX {
			minX = x
		}
		if x > maxX {
			maxX = x
		}
		if y < minY {
			minY = y
		}
		if y > maxY {
			maxY = y
		}
		if z < minZ {
			minZ = z
		}
		if z > maxZ {
			maxZ = z
		}
	}
	return modelHeatmapMeshBounds{
		CenterX: (minX + maxX) * 0.5,
		CenterY: (minY + maxY) * 0.5,
		CenterZ: (minZ + maxZ) * 0.5,
		SizeX:   maxModelHeatmapFloat(maxX-minX, 1e-6),
		SizeY:   maxModelHeatmapFloat(maxY-minY, 1e-6),
		SizeZ:   maxModelHeatmapFloat(maxZ-minZ, 1e-6),
	}
}

func normalizeMeshPreviewSceneBatches(batches []meshPreviewBatchData) {
	if len(batches) == 0 {
		return
	}
	hasAny := false
	var minX, minY, minZ float64
	var maxX, maxY, maxZ float64
	for _, batch := range batches {
		for i := 0; i+2 < len(batch.RawPositions); i += 3 {
			x := float64(batch.RawPositions[i])
			y := float64(batch.RawPositions[i+1])
			z := float64(batch.RawPositions[i+2])
			if !hasAny {
				minX, maxX = x, x
				minY, maxY = y, y
				minZ, maxZ = z, z
				hasAny = true
				continue
			}
			if x < minX {
				minX = x
			}
			if x > maxX {
				maxX = x
			}
			if y < minY {
				minY = y
			}
			if y > maxY {
				maxY = y
			}
			if z < minZ {
				minZ = z
			}
			if z > maxZ {
				maxZ = z
			}
		}
	}
	if !hasAny {
		return
	}
	centerX := (minX + maxX) * 0.5
	centerY := (minY + maxY) * 0.5
	centerZ := (minZ + maxZ) * 0.5
	radius := 0.0
	for _, batch := range batches {
		for i := 0; i+2 < len(batch.RawPositions); i += 3 {
			x := float64(batch.RawPositions[i]) - centerX
			y := float64(batch.RawPositions[i+1]) - centerY
			z := float64(batch.RawPositions[i+2]) - centerZ
			distance := math.Sqrt(x*x + y*y + z*z)
			if distance > radius {
				radius = distance
			}
		}
	}
	if radius <= 0 {
		radius = 1
	}
	for batchIndex := range batches {
		for i := 0; i+2 < len(batches[batchIndex].RawPositions); i += 3 {
			batches[batchIndex].RawPositions[i] = float32((float64(batches[batchIndex].RawPositions[i]) - centerX) / radius)
			batches[batchIndex].RawPositions[i+1] = float32((float64(batches[batchIndex].RawPositions[i+1]) - centerY) / radius)
			batches[batchIndex].RawPositions[i+2] = float32((float64(batches[batchIndex].RawPositions[i+2]) - centerZ) / radius)
		}
	}
}

func modelHeatmapTriangleDensity(sizeX float64, sizeY float64, sizeZ float64, triangleCount uint32) float64 {
	surfaceArea := modelHeatmapSurfaceArea(sizeX, sizeY, sizeZ)
	if surfaceArea <= 0 {
		return 0
	}
	return float64(triangleCount) / surfaceArea
}

func modelHeatmapTextureDensity(sizeX float64, sizeY float64, sizeZ float64, textureBytes int64) float64 {
	if textureBytes <= 0 {
		return 0
	}
	surfaceArea := modelHeatmapSurfaceArea(sizeX, sizeY, sizeZ)
	if surfaceArea <= 0 {
		return 0
	}
	return float64(textureBytes) / surfaceArea
}

func modelHeatmapSurfaceArea(sizeX float64, sizeY float64, sizeZ float64) float64 {
	sizeX = math.Abs(sizeX)
	sizeY = math.Abs(sizeY)
	sizeZ = math.Abs(sizeZ)
	return 2 * ((sizeX * sizeY) + (sizeY * sizeZ) + (sizeX * sizeZ))
}

func normalizedModelHeatmapMode(mode modelHeatmapMode) modelHeatmapMode {
	switch mode {
	case modelHeatmapModeTriangles,
		modelHeatmapModeTexture,
		modelHeatmapModeSizeScaledTexture:
		return mode
	}
	return modelHeatmapModeSizeScaledTriangles
}

func modelHeatmapValueFromInfo(mode modelHeatmapMode, info modelHeatmapBatchInfo) float64 {
	switch normalizedModelHeatmapMode(mode) {
	case modelHeatmapModeTriangles:
		return float64(info.TriangleCount)
	case modelHeatmapModeTexture:
		return float64(info.TextureBytes)
	case modelHeatmapModeSizeScaledTexture:
		return info.TextureDensity
	}
	return info.Density
}

func modelHeatmapValue(mode modelHeatmapMode, density float64, triangleCount uint32) float64 {
	if normalizedModelHeatmapMode(mode) == modelHeatmapModeTriangles {
		return float64(triangleCount)
	}
	return density
}

func modelHeatmapColor(heatValue float64, maxHeatValue float64, heatSpread float64) color.NRGBA {
	stops := []struct {
		position float64
		color    color.NRGBA
	}{
		{position: 0.0, color: color.NRGBA{R: 53, G: 118, B: 255, A: 255}},
		{position: 0.35, color: color.NRGBA{R: 58, G: 205, B: 141, A: 255}},
		{position: 0.7, color: color.NRGBA{R: 255, G: 206, B: 84, A: 255}},
		{position: 1.0, color: color.NRGBA{R: 255, G: 82, B: 82, A: 255}},
	}
	if maxHeatValue <= 0 || heatValue <= 0 {
		return stops[0].color
	}
	ratio := math.Log1p(heatValue) / math.Log1p(maxHeatValue)
	ratio = applyHeatSpread(ratio, format.Clamp(heatSpread, rbxlHeatmapMinSpread, rbxlHeatmapMaxSpread))
	ratio = format.Clamp(ratio, 0, 1)
	for i := 1; i < len(stops); i++ {
		if ratio > stops[i].position {
			continue
		}
		start := stops[i-1]
		end := stops[i]
		localRatio := (ratio - start.position) / (end.position - start.position)
		return color.NRGBA{
			R: uint8(float64(start.color.R) + (float64(end.color.R)-float64(start.color.R))*localRatio),
			G: uint8(float64(start.color.G) + (float64(end.color.G)-float64(start.color.G))*localRatio),
			B: uint8(float64(start.color.B) + (float64(end.color.B)-float64(start.color.B))*localRatio),
			A: 255,
		}
	}
	return stops[len(stops)-1].color
}

func maxModelHeatmapFloat(left float64, right float64) float64 {
	if left > right {
		return left
	}
	return right
}

func cloneModelHeatmapResolvedMeshes(source map[string]modelHeatmapResolvedMesh) map[string]modelHeatmapResolvedMesh {
	if len(source) == 0 {
		return nil
	}
	cloned := make(map[string]modelHeatmapResolvedMesh, len(source))
	for key, mesh := range source {
		mesh.Preview = cloneMeshPreviewData(mesh.Preview)
		cloned[key] = mesh
	}
	return cloned
}

func formatModelHeatmapSummary(summary modelHeatmapSceneSummary) string {
	summaryParts := []string{
		fmt.Sprintf("Rendered %s/%s MeshParts", format.FormatIntCommas(int64(summary.RenderedMeshPartCount)), format.FormatIntCommas(int64(summary.MeshPartCount))),
		fmt.Sprintf("%s unique meshes", format.FormatIntCommas(int64(summary.UniqueMeshCount))),
		fmt.Sprintf("%s triangles", format.FormatIntCommas(int64(summary.TriangleCount))),
	}
	if summary.PreviewTriangleCount > 0 && summary.PreviewTriangleCount != summary.TriangleCount {
		summaryParts = append(summaryParts, fmt.Sprintf("%s preview triangles", format.FormatIntCommas(int64(summary.PreviewTriangleCount))))
	}
	if summary.UniqueTextureCount > 0 {
		summaryParts = append(summaryParts, fmt.Sprintf("%s unique textures", format.FormatIntCommas(int64(summary.UniqueTextureCount))))
	}
	if summary.TextureBytes > 0 {
		summaryParts = append(summaryParts, fmt.Sprintf("%s texture bytes", format.FormatSizeAuto(int(summary.TextureBytes))))
	}
	if summary.MaxHeatValue > 0 {
		switch normalizedModelHeatmapMode(summary.HeatMode) {
		case modelHeatmapModeTriangles:
			summaryParts = append(summaryParts, fmt.Sprintf("max mesh %s tris", format.FormatIntCommas(int64(math.Round(summary.MaxHeatValue)))))
		case modelHeatmapModeTexture:
			summaryParts = append(summaryParts, fmt.Sprintf("max mesh texture %s", format.FormatSizeAuto(int(math.Round(summary.MaxHeatValue)))))
		case modelHeatmapModeSizeScaledTexture:
			summaryParts = append(summaryParts, fmt.Sprintf("max texture density %s/stud^2", format.FormatSizeAuto(int(math.Round(summary.MaxTextureDensity)))))
		default:
			summaryParts = append(summaryParts, fmt.Sprintf("max density %.2f tri/stud^2", summary.MaxDensity))
		}
	}
	if summary.FailedMeshCount > 0 {
		summaryParts = append(summaryParts, fmt.Sprintf("%s failed", format.FormatIntCommas(int64(summary.FailedMeshCount))))
	}
	return strings.Join(summaryParts, " · ")
}

func modelHeatmapLegendText(mode modelHeatmapMode) string {
	switch normalizedModelHeatmapMode(mode) {
	case modelHeatmapModeTriangles:
		return "Blue = low triangle count, red = high triangle count. Heat ignores MeshPart size and uses each mesh's triangle total."
	case modelHeatmapModeTexture:
		return "Blue = low texture bytes, red = high texture bytes. Heat uses the total decoded image byte size of each MeshPart's textures."
	case modelHeatmapModeSizeScaledTexture:
		return "Blue = low texture density, red = high texture density. Density uses texture bytes per stud² of each MeshPart's bounds."
	}
	return "Blue = low triangle density, red = high triangle density. Density uses triangles per stud² of each MeshPart's bounds."
}

func formatModelHeatmapPartInfo(info modelHeatmapBatchInfo, mode modelHeatmapMode) string {
	triangleLine := fmt.Sprintf("Triangles: %s", format.FormatIntCommas(int64(info.TriangleCount)))
	densityLine := fmt.Sprintf("Density: %.2f tri/stud²", info.Density)
	textureLine := formatModelHeatmapTexturePartInfoLine(info)
	lines := []string{}
	switch normalizedModelHeatmapMode(mode) {
	case modelHeatmapModeTriangles:
		lines = append(lines, triangleLine, densityLine)
	case modelHeatmapModeTexture, modelHeatmapModeSizeScaledTexture:
		if textureLine != "" {
			lines = append(lines, textureLine)
		} else {
			lines = append(lines, "No textures on this MeshPart.")
		}
		lines = append(lines, triangleLine, densityLine)
		return strings.Join(lines, "\n")
	default:
		lines = append(lines, densityLine, triangleLine)
	}
	if textureLine != "" {
		lines = append(lines, textureLine)
	}
	return strings.Join(lines, "\n")
}

func formatModelHeatmapTexturePartInfoLine(info modelHeatmapBatchInfo) string {
	if info.TextureCount <= 0 {
		return ""
	}
	return fmt.Sprintf(
		"Textures: %d · bytes %s · density %s/stud²",
		info.TextureCount,
		format.FormatSizeAuto(int(info.TextureBytes)),
		format.FormatSizeAuto(int(math.Round(info.TextureDensity))),
	)
}

type debugBatchInfo struct {
	Instance modelHeatmapMeshInstance
	Bounds   modelHeatmapMeshBounds
}

func debugExportModelHeatmapOBJ(batches []meshPreviewBatchData, infos []debugBatchInfo) {
	tempPath := filepath.Join(os.TempDir(), "joxblox-heatmap-debug.obj")
	f, err := os.Create(tempPath)
	if err != nil {
		debug.Logf("debug OBJ export failed: %s", err.Error())
		return
	}
	defer f.Close()

	fmt.Fprintf(f, "# joxblox model heatmap debug export\n")
	fmt.Fprintf(f, "# %d batches (one per MeshPart instance)\n\n", len(batches))

	globalVertexOffset := 0
	for batchIdx, batch := range batches {
		vertexCount := len(batch.RawPositions) / 3
		fmt.Fprintf(f, "o batch_%d\n", batchIdx)
		if batchIdx < len(infos) {
			inst := infos[batchIdx].Instance
			bounds := infos[batchIdx].Bounds
			fmt.Fprintf(f, "# path: %s\n", inst.InstancePath)
			fmt.Fprintf(f, "# center: %.6f, %.6f, %.6f\n", inst.CenterX, inst.CenterY, inst.CenterZ)
			fmt.Fprintf(f, "# size: %.6f, %.6f, %.6f\n", inst.SizeX, inst.SizeY, inst.SizeZ)
			fmt.Fprintf(f, "# basisSize: %.6f, %.6f, %.6f\n", inst.BasisSizeX, inst.BasisSizeY, inst.BasisSizeZ)
			fmt.Fprintf(f, "# rotation: [%.6f %.6f %.6f] [%.6f %.6f %.6f] [%.6f %.6f %.6f]\n",
				inst.Rotation[0], inst.Rotation[1], inst.Rotation[2],
				inst.Rotation[3], inst.Rotation[4], inst.Rotation[5],
				inst.Rotation[6], inst.Rotation[7], inst.Rotation[8])
			fmt.Fprintf(f, "# meshBounds center: %.6f, %.6f, %.6f\n", bounds.CenterX, bounds.CenterY, bounds.CenterZ)
			fmt.Fprintf(f, "# meshBounds size: %.6f, %.6f, %.6f\n", bounds.SizeX, bounds.SizeY, bounds.SizeZ)
			fmt.Fprintf(f, "# meshAsset: %d / %s\n", inst.MeshRef.AssetID, inst.MeshRef.AssetInput)
		}
		for i := 0; i < len(batch.RawPositions); i += 3 {
			fmt.Fprintf(f, "v %.6f %.6f %.6f\n", batch.RawPositions[i], batch.RawPositions[i+1], batch.RawPositions[i+2])
		}
		for i := 0; i+2 < len(batch.RawIndices); i += 3 {
			a := int(batch.RawIndices[i]) + 1 + globalVertexOffset
			b := int(batch.RawIndices[i+1]) + 1 + globalVertexOffset
			c := int(batch.RawIndices[i+2]) + 1 + globalVertexOffset
			fmt.Fprintf(f, "f %d %d %d\n", a, b, c)
		}
		globalVertexOffset += vertexCount
	}

	debug.Logf("debug OBJ exported to %s (%d batches, %d vertices)", tempPath, len(batches), globalVertexOffset)
}
