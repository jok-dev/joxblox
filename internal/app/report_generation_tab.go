package app

import (
	"fmt"
	"image/color"
	"math"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	ttwidget "github.com/dweymouth/fyne-tooltip/widget"
)

type reportGenerationSummary struct {
	TotalBytes            int64
	TextureBytes          int64
	MeshBytes             int64
	TriangleCount         int64
	OversizedTextureCount int
	DrawCallCount         int64
	DuplicateCount        int64
	DuplicateSizeBytes    int64
	ReferenceCount        int64
	UniqueReferenceCount  int
	UniqueAssetCount      int
	ResolvedCount         int
	MeshPartCount         int
	PartCount             int
}

type reportGenerationResolvedAsset struct {
	Stats      rbxlHeatmapAssetStats
	FileSHA256 string
}

type reportGenerationInstanceRenderInfo struct {
	InstanceType           string
	InstancePath           string
	MaterialKey            string
	MeshContentKey         string
	TextureContentKey      string
	SurfaceAppearanceProps map[string]string
	X                      float64
	Z                      float64
	HasPosition            bool
}

const reportGenerationCellSizeStuds = 1000.0

func newReportGenerationTab(window fyne.Window, onViewInScan func(string), onViewInHeatmap func(string)) (fyne.CanvasObject, func(string)) {
	selectedFilePath := ""
	currentSummary := reportGenerationSummary{}
	currentCells := []rbxlHeatmapCell{}
	currentAssetType := reportGenerationAssetTypeConfig{}
	hasSummary := false
	var loadToken atomic.Uint64
	var loading atomic.Bool
	var cancelLoadButton *widget.Button

	filePathLabel := widget.NewLabel("Drop .rbxl/.rbxm or choose file")
	filePathLabel.Wrapping = fyne.TextTruncate
	warningBanner := newMaterialVariantWarningBanner(window)

	statusLabel := widget.NewLabel("")
	statusLabel.Wrapping = fyne.TextWrapWord

	progressBar := widget.NewProgressBarInfinite()
	progressBar.Hide()

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

	workspaceOnlyCheck := widget.NewCheck("Workspace & MaterialService only", nil)
	workspaceOnlyCheck.SetChecked(true)

	profileContainer := container.NewVBox()
	profileContainer.Hide()
	setWarning := func(warningData materialVariantWarningData) { warningBanner.SetWarning(warningData) }

	refreshProfile := func() {
		profileContainer.RemoveAll()
		if !hasSummary {
			profileContainer.Hide()
			return
		}
		assetType := currentAssetType
		if assetType.ID == "" {
			assetType = defaultReportGenerationAssetType()
		}
		percentiles := computeReportCellPercentiles(assetType, currentCells, currentSummary)
		grades := computePerformanceProfileForAssetType(assetType, percentiles, currentSummary)
		hasDuplicates := currentSummary.DuplicateCount > 0
		overall := overallPerformanceGrade(grades, hasDuplicates)
		overallScore := overallPerformanceScorePercent(grades, hasDuplicates)
		profileContainer.Add(buildPerformanceProfileUI(assetType.Label, overall, overallScore, grades, percentiles))
		if onViewInScan != nil || onViewInHeatmap != nil {
			navButtons := container.NewHBox()
			if onViewInScan != nil {
				viewInScanButton := widget.NewButtonWithIcon("View in Scan", theme.SearchIcon(), func() {
					onViewInScan(selectedFilePath)
				})
				navButtons.Add(viewInScanButton)
			}
			if onViewInHeatmap != nil {
				viewInHeatmapButton := widget.NewButtonWithIcon("View in Heatmap", theme.ColorPaletteIcon(), func() {
					onViewInHeatmap(selectedFilePath)
				})
				navButtons.Add(viewInHeatmapButton)
			}
			profileContainer.Add(widget.NewSeparator())
			profileContainer.Add(container.NewCenter(navButtons))
		}
		profileContainer.Show()
	}

	var loadReportFile func(string)
	var assetTypeDialog dialog.Dialog
	var showAssetTypeDialog func()
	cancelActiveLoad := func(showRetryDialog bool) {
		if !loading.Load() {
			return
		}
		loadToken.Add(1)
		currentAssetType = reportGenerationAssetTypeConfig{}
		hasSummary = false
		currentSummary = reportGenerationSummary{}
		currentCells = nil
		profileContainer.Hide()
		setWarning(materialVariantWarningData{})
		setBusy(false)
		statusLabel.SetText("Loading canceled")
		if showRetryDialog {
			showAssetTypeDialog()
		}
	}
	startReportLoad := func(assetTypeID string) {
		assetType, found := reportGenerationAssetTypeByID(assetTypeID)
		if !found {
			statusLabel.SetText("Choose a valid asset type")
			return
		}
		if strings.TrimSpace(selectedFilePath) == "" {
			statusLabel.SetText("Drop .rbxl/.rbxm or choose file")
			return
		}

		if assetTypeDialog != nil {
			assetTypeDialog.Hide()
			assetTypeDialog = nil
		}
		currentAssetType = assetType
		hasSummary = false
		currentSummary = reportGenerationSummary{}
		currentCells = nil
		profileContainer.Hide()
		setWarning(materialVariantWarningData{})
		statusLabel.SetText(fmt.Sprintf("Loading %s asset...", assetType.Label))
		setBusy(true)

		trimmedPath := selectedFilePath
		filePathLabel.SetText(trimmedPath)

		var pathPrefixes []string
		if workspaceOnlyCheck.Checked && strings.ToLower(filepath.Ext(trimmedPath)) == ".rbxl" {
			pathPrefixes = []string{"Workspace", "MaterialService"}
		}

		token := loadToken.Add(1)
		go func(expectedToken uint64, sourcePath string, prefixes []string, selectedAssetType reportGenerationAssetTypeConfig) {
			isCanceled := func() bool {
				return loadToken.Load() != expectedToken
			}

			positionedRefs, extractErr := extractPositionedRefsWithRustyAssetTool(sourcePath, prefixes, nil)
			if extractErr != nil {
				fyne.Do(func() {
					if isCanceled() {
						return
					}
					setBusy(false)
					statusLabel.SetText(fmt.Sprintf("Load failed: %s", extractErr.Error()))
				})
				return
			}
			if isCanceled() {
				return
			}

			mapPartsRaw, mapPartsErr := extractMapRenderPartsWithRustyAssetTool(sourcePath, prefixes, nil)
			if mapPartsErr != nil {
				logDebugf("Report generation map extraction failed for %s: %s", sourcePath, mapPartsErr.Error())
			}
			if isCanceled() {
				return
			}
			warningData, warningErr := buildRBXLMissingMaterialVariantWarning(sourcePath, prefixes, nil)
			if warningErr != nil {
				logDebugf("Report generation material warning extraction failed for %s: %s", sourcePath, warningErr.Error())
			}
			if isCanceled() {
				return
			}
			mapParts := convertRustMapParts(mapPartsRaw)
			reportMeshPartCount, reportPartCount := countReportGenerationParts(mapParts, positionedRefs)
			logDebugf(
				"Report generation extracted %d positioned refs and %d map parts for %s (meshparts=%d, parts=%d)",
				len(positionedRefs),
				len(mapParts),
				sourcePath,
				reportMeshPartCount,
				reportPartCount,
			)

			if len(positionedRefs) == 0 {
				fyne.Do(func() {
					if loadToken.Load() != expectedToken {
						return
					}
					setWarning(warningData)
					setBusy(false)
					statusLabel.SetText("No assets found")
				})
				return
			}

			uniqueRefsByKey := map[string]heatmapAssetReference{}
			for _, ref := range positionedRefs {
				if ref.ID <= 0 {
					continue
				}
				key := scanAssetReferenceKey(ref.ID, ref.RawContent)
				uniqueRefsByKey[key] = heatmapAssetReference{
					AssetID:    ref.ID,
					AssetInput: strings.TrimSpace(ref.RawContent),
				}
			}
			refsToResolve := make([]heatmapAssetReference, 0, len(uniqueRefsByKey))
			for _, ref := range uniqueRefsByKey {
				refsToResolve = append(refsToResolve, ref)
			}

			resolved := resolveReportGenerationAssets(refsToResolve, func(done int, total int, memoryCacheHits int, diskCacheHits int, networkFetches int) {
				fyne.Do(func() {
					if isCanceled() {
						return
					}
					statusLabel.SetText(fmt.Sprintf("Loading... %d/%d", done, total))
				})
			}, isCanceled)
			if isCanceled() {
				return
			}

			summary, points := buildReportSummaryAndPoints(positionedRefs, resolved, mapParts, assetType.OversizedTextureThreshold)
			cells := buildReportGenerationCells(points, mapParts, positionedRefs)
			logDebugf(
				"Report generation summary for %s (%s): meshparts=%d parts=%d points=%d cells=%d resolved=%d unique-assets=%d",
				sourcePath,
				selectedAssetType.ID,
				summary.MeshPartCount,
				summary.PartCount,
				len(points),
				len(cells),
				summary.ResolvedCount,
				summary.UniqueAssetCount,
			)

			fyne.Do(func() {
				if isCanceled() {
					return
				}
				currentAssetType = selectedAssetType
				currentSummary = summary
				currentCells = cells
				hasSummary = true
				setWarning(warningData)
				refreshProfile()
				statusLabel.SetText("")
				setBusy(false)
			})
		}(token, trimmedPath, pathPrefixes, assetType)
	}

	showAssetTypeDialog = func() {
		if strings.TrimSpace(selectedFilePath) == "" || loading.Load() {
			return
		}
		if assetTypeDialog != nil {
			assetTypeDialog.Hide()
		}
		buttons := make([]fyne.CanvasObject, 0, len(reportGenerationAssetTypeConfigs)+2)
		description := widget.NewLabel("Choose the asset type for this report.")
		description.Alignment = fyne.TextAlignCenter
		description.Wrapping = fyne.TextWrapWord
		buttons = append(buttons, description)
		for _, assetType := range reportGenerationAssetTypeConfigs {
			assetType := assetType
			button := widget.NewButton(assetType.Label, func() {
				startReportLoad(assetType.ID)
			})
			button.Importance = widget.HighImportance
			buttons = append(buttons, button)
		}
		cancelButton := widget.NewButton("Cancel", func() {
			if assetTypeDialog != nil {
				assetTypeDialog.Hide()
				assetTypeDialog = nil
			}
			statusLabel.SetText("File selected. Choose an asset type to continue")
		})
		buttons = append(buttons, cancelButton)
		content := container.NewVBox(buttons...)
		assetTypeDialog = dialog.NewCustomWithoutButtons("Choose Asset Type", content, window)
		assetTypeDialog.Resize(fyne.NewSize(340, content.MinSize().Height+20))
		assetTypeDialog.Show()
	}

	loadReportFile = func(filePath string) {
		trimmedPath := strings.TrimSpace(filePath)
		if !isRobloxDOMFilePath(trimmedPath) {
			statusLabel.SetText("Only .rbxl/.rbxm")
			return
		}

		selectedFilePath = trimmedPath
		filePathLabel.SetText(selectedFilePath)
		currentAssetType = reportGenerationAssetTypeConfig{}
		hasSummary = false
		currentSummary = reportGenerationSummary{}
		currentCells = nil
		profileContainer.Hide()
		setWarning(materialVariantWarningData{})
		statusLabel.SetText("Choose an asset type to generate the report")
		setBusy(false)
		showAssetTypeDialog()
	}

	browseButton := widget.NewButtonWithIcon("Choose File", theme.FolderOpenIcon(), func() {
		pickRBXLSource(window, loadReportFile, func(err error) {
			statusLabel.SetText(fmt.Sprintf("Pick failed: %s", err.Error()))
		})
	})
	browseButton.Importance = widget.HighImportance
	cancelLoadButton = widget.NewButton("Cancel", func() {
		cancelActiveLoad(true)
	})
	cancelLoadButton.Importance = widget.DangerImportance
	cancelLoadButton.Hide()

	dropLabel := canvas.NewText("Drag RBXL/RBXM File Here", theme.ForegroundColor())
	dropLabel.Alignment = fyne.TextAlignCenter
	dropLabel.TextSize = 28
	dropLabel.TextStyle = fyne.TextStyle{Bold: true}

	controls := container.NewVBox(
		container.NewCenter(dropLabel),
		container.NewCenter(container.NewHBox(progressBar, cancelLoadButton)),
		container.NewCenter(container.NewHBox(browseButton, workspaceOnlyCheck)),
		filePathLabel,
		warningBanner.root,
	)

	return container.NewBorder(
		controls,
		statusLabel,
		nil,
		nil,
		container.NewVScroll(profileContainer),
	), loadReportFile
}

func resolveReportGenerationAssets(references []heatmapAssetReference, onProgress func(done int, total int, memoryRequestCount int, diskRequestCount int, networkRequestCount int), shouldCancel func() bool) map[string]reportGenerationResolvedAsset {
	if len(references) == 0 {
		return map[string]reportGenerationResolvedAsset{}
	}

	jobs := make(chan heatmapAssetReference, len(references))
	for _, reference := range references {
		jobs <- reference
	}
	close(jobs)

	resolvedByReferenceKey := make(map[string]reportGenerationResolvedAsset, len(references))
	var resolvedMutex sync.Mutex
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
				if shouldCancel != nil && shouldCancel() {
					return
				}
				referenceKey := scanAssetReferenceKey(reference.AssetID, reference.AssetInput)
				trace := &assetRequestTrace{}
				previewResult, previewErr := loadAssetStatsPreviewForReferenceWithTrace(reference.AssetID, reference.AssetInput, trace)
				if shouldCancel != nil && shouldCancel() {
					return
				}
				resolvedAsset := reportGenerationResolvedAsset{
					Stats: rbxlHeatmapAssetStats{AssetID: reference.AssetID},
				}
				if previewErr == nil && previewResult != nil {
					resolvedAsset = reportGenerationResolvedAsset{
						Stats:      buildReportGenerationStatsFromPreview(reference.AssetID, previewResult),
						FileSHA256: normalizeHash(previewSHA256(previewResult)),
					}
				}
				resolvedMutex.Lock()
				resolvedByReferenceKey[referenceKey] = resolvedAsset
				resolvedMutex.Unlock()
				if onProgress != nil && (shouldCancel == nil || !shouldCancel()) {
					switch trace.classifyRequestSource() {
					case heatmapAssetRequestSourceNetwork:
						networkRequestCount.Add(1)
					case heatmapAssetRequestSourceDisk:
						diskRequestCount.Add(1)
					default:
						memoryRequestCount.Add(1)
					}
					onProgress(
						int(completed.Add(1)),
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
	return resolvedByReferenceKey
}

func buildReportGenerationCells(points []rbxlHeatmapPoint, mapParts []rbxlHeatmapMapPart, refs []positionedRustyAssetToolResult) []rbxlHeatmapCell {
	if len(points) == 0 && len(mapParts) == 0 {
		return nil
	}

	minX, maxX, minZ, maxZ := heatmapSceneBounds(points, mapParts)
	rangeX := maxX - minX
	rangeZ := maxZ - minZ
	longestRange := math.Max(rangeX, rangeZ)
	if longestRange <= 0 {
		longestRange = reportGenerationCellSizeStuds
	}

	gridDivisions := maxInt(1, int(math.Ceil(longestRange/reportGenerationCellSizeStuds)))
	paddedLongestRange := float64(gridDivisions) * reportGenerationCellSizeStuds
	scene := &rbxlHeatmapScene{
		Points:   points,
		MinimumX: minX,
		MaximumX: maxX,
		MinimumZ: minZ,
		MaximumZ: maxZ,
	}
	if rangeX >= rangeZ {
		scene.MaximumX = scene.MinimumX + paddedLongestRange
	} else {
		scene.MaximumZ = scene.MinimumZ + paddedLongestRange
	}

	cells, cellSizeWorld, columnCount, rowCount, _ := buildHeatmapCells(scene, gridDivisions)
	if cellSizeWorld <= 0 {
		cellSizeWorld = reportGenerationCellSizeStuds
	}

	renderInfos := buildReportGenerationRenderInfos(mapParts, refs)
	partCountsByCell := map[string]rbxlHeatmapTotals{}
	meshDrawKeysByCell := map[string]map[string]struct{}{}
	for _, renderInfo := range renderInfos {
		if !renderInfo.HasPosition {
			continue
		}
		column := clampHeatmapInt(int(math.Floor((renderInfo.X-scene.MinimumX)/cellSizeWorld)), 0, columnCount-1)
		row := clampHeatmapInt(int(math.Floor((renderInfo.Z-scene.MinimumZ)/cellSizeWorld)), 0, rowCount-1)
		cellKey := heatmapCellKey(row, column)
		counts := partCountsByCell[cellKey]
		switch normalizeReportGenerationInstanceType(renderInfo.InstanceType) {
		case "meshpart":
			counts.MeshPartCount++
			drawCallKey := estimateMeshPartDrawCallKey(renderInfo)
			if drawCallKey == "" {
				counts.DrawCallCount++
			} else {
				drawKeys := meshDrawKeysByCell[cellKey]
				if drawKeys == nil {
					drawKeys = map[string]struct{}{}
					meshDrawKeysByCell[cellKey] = drawKeys
				}
				drawKeys[drawCallKey] = struct{}{}
			}
		case "part":
			counts.PartCount++
			counts.DrawCallCount++
		case "":
		default:
			counts.DrawCallCount++
		}
		partCountsByCell[cellKey] = counts
	}
	for cellKey, drawKeys := range meshDrawKeysByCell {
		counts := partCountsByCell[cellKey]
		counts.DrawCallCount += int64(len(drawKeys))
		partCountsByCell[cellKey] = counts
	}

	cellIndexByKey := make(map[string]int, len(cells))
	for index, cell := range cells {
		cellKey := heatmapCellKey(cell.Row, cell.Column)
		cellIndexByKey[cellKey] = index
	}
	for cellKey, counts := range partCountsByCell {
		index, found := cellIndexByKey[cellKey]
		if !found {
			var row, column int
			if _, scanErr := fmt.Sscanf(cellKey, "%d:%d", &row, &column); scanErr != nil {
				continue
			}
			cells = append(cells, rbxlHeatmapCell{
				Row:      row,
				Column:   column,
				MinimumX: scene.MinimumX + float64(column)*cellSizeWorld,
				MaximumX: scene.MinimumX + float64(column+1)*cellSizeWorld,
				MinimumZ: scene.MinimumZ + float64(row)*cellSizeWorld,
				MaximumZ: scene.MinimumZ + float64(row+1)*cellSizeWorld,
			})
			index = len(cells) - 1
			cellIndexByKey[cellKey] = index
		}
		cells[index].Stats.MeshPartCount = counts.MeshPartCount
		cells[index].Stats.PartCount = counts.PartCount
		cells[index].Stats.DrawCallCount = counts.DrawCallCount
	}
	sort.Slice(cells, func(left int, right int) bool {
		if cells[left].Row == cells[right].Row {
			return cells[left].Column < cells[right].Column
		}
		return cells[left].Row < cells[right].Row
	})
	return cells
}

func buildReportGenerationStatsFromPreview(assetID int64, previewResult *assetPreviewResult) rbxlHeatmapAssetStats {
	stats := rbxlHeatmapAssetStats{AssetID: assetID}
	if previewResult == nil {
		return stats
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
	return stats
}

func buildReportSummaryAndPoints(refs []positionedRustyAssetToolResult, resolved map[string]reportGenerationResolvedAsset, mapParts []rbxlHeatmapMapPart, oversizedTextureThreshold float64) (reportGenerationSummary, []rbxlHeatmapPoint) {
	summary := reportGenerationSummary{}
	uniqueAssetIDs := map[int64]struct{}{}
	uniqueReferenceKeys := map[string]struct{}{}
	hashCounts := map[string]int{}
	resolvedUniqueKeys := make([]string, 0, len(resolved))
	seenResolvedKeys := map[string]struct{}{}
	for _, ref := range refs {
		if ref.ID <= 0 {
			continue
		}

		summary.ReferenceCount++
		uniqueAssetIDs[ref.ID] = struct{}{}
		key := scanAssetReferenceKey(ref.ID, ref.RawContent)
		if _, seen := uniqueReferenceKeys[key]; !seen {
			uniqueReferenceKeys[key] = struct{}{}
			summary.UniqueReferenceCount++
		}
		asset, found := resolved[key]
		if !found {
			continue
		}
		stats := asset.Stats
		if stats.TotalBytes <= 0 && stats.TextureBytes <= 0 && stats.MeshBytes <= 0 && stats.TriangleCount == 0 {
			continue
		}
		if reportGenerationMeshTriangleInstanceKey(ref) != "" && stats.TriangleCount > 0 {
			summary.TriangleCount += int64(stats.TriangleCount)
		}
		if _, seen := seenResolvedKeys[key]; seen {
			continue
		}
		seenResolvedKeys[key] = struct{}{}
		if asset.FileSHA256 != "" {
			hashCounts[asset.FileSHA256]++
			resolvedUniqueKeys = append(resolvedUniqueKeys, key)
		}
		summary.TotalBytes += int64(stats.TotalBytes)
		summary.TextureBytes += int64(stats.TextureBytes)
		summary.MeshBytes += int64(stats.MeshBytes)
		summary.ResolvedCount++
	}

	seenCounts := map[string]int{}
	for _, key := range resolvedUniqueKeys {
		asset := resolved[key]
		if asset.FileSHA256 == "" {
			continue
		}
		if hashCounts[asset.FileSHA256] < 2 {
			continue
		}
		if seenCounts[asset.FileSHA256] >= 1 {
			summary.DuplicateCount++
			summary.DuplicateSizeBytes += int64(asset.Stats.TotalBytes)
		}
		seenCounts[asset.FileSHA256]++
	}
	summary.UniqueAssetCount = len(uniqueAssetIDs)
	summary.OversizedTextureCount = countReportGenerationOversizedTextures(refs, resolved, mapParts, oversizedTextureThreshold)

	points := make([]rbxlHeatmapPoint, 0, len(refs))
	for _, ref := range refs {
		if ref.ID <= 0 || ref.WorldX == nil || ref.WorldY == nil || ref.WorldZ == nil {
			continue
		}
		key := scanAssetReferenceKey(ref.ID, ref.RawContent)
		asset, found := resolved[key]
		if !found || asset.Stats.TotalBytes <= 0 {
			continue
		}
		points = append(points, rbxlHeatmapPoint{
			AssetID:      ref.ID,
			AssetInput:   strings.TrimSpace(ref.RawContent),
			InstanceType: strings.TrimSpace(ref.InstanceType),
			InstanceName: strings.TrimSpace(ref.InstanceName),
			InstancePath: strings.TrimSpace(ref.InstancePath),
			PropertyName: strings.TrimSpace(ref.PropertyName),
			Stats:        asset.Stats,
			X:            *ref.WorldX,
			Y:            *ref.WorldY,
			Z:            *ref.WorldZ,
		})
	}
	summary.MeshPartCount, summary.PartCount = countReportGenerationParts(mapParts, refs)
	summary.DrawCallCount = int64(countEstimatedDrawCalls(mapParts, refs))
	return summary, points
}

func countReportGenerationOversizedTextures(refs []positionedRustyAssetToolResult, resolved map[string]reportGenerationResolvedAsset, mapParts []rbxlHeatmapMapPart, threshold float64) int {
	if threshold <= 0 {
		threshold = defaultLargeTextureThreshold
	}
	if len(refs) == 0 || len(resolved) == 0 {
		return 0
	}

	areaByPath := buildSceneSurfaceAreaIndexFromHeatmapParts(mapParts)
	maxAreaByReferenceKey := map[string]float64{}
	for _, ref := range refs {
		if ref.ID <= 0 {
			continue
		}
		referenceKey := scanAssetReferenceKey(ref.ID, ref.RawContent)
		area := estimateSceneSurfaceAreaForPaths(strings.TrimSpace(ref.InstancePath), nil, areaByPath)
		maxAreaByReferenceKey[referenceKey] = maxPositiveFloat64(maxAreaByReferenceKey[referenceKey], area)
	}

	oversizedTextureCount := 0
	for referenceKey, resolvedAsset := range resolved {
		textureBytes := resolvedAsset.Stats.TextureBytes
		if textureBytes <= 0 {
			continue
		}
		if computeLargeTextureScore(textureBytes, maxAreaByReferenceKey[referenceKey]) >= threshold {
			oversizedTextureCount++
		}
	}
	return oversizedTextureCount
}

func countReportGenerationParts(mapParts []rbxlHeatmapMapPart, refs []positionedRustyAssetToolResult) (int, int) {
	if len(mapParts) > 0 {
		meshPartCount := 0
		partCount := 0
		for _, part := range mapParts {
			partType := strings.ToLower(strings.TrimSpace(part.InstanceType))
			switch partType {
			case "meshpart", "part":
			default:
				continue
			}
			switch partType {
			case "meshpart":
				meshPartCount++
			case "part":
				partCount++
			}
		}
		return meshPartCount, partCount
	}

	meshPartCount := 0
	partCount := 0
	seenInstancePaths := map[string]struct{}{}
	for _, ref := range refs {
		instancePath := strings.TrimSpace(ref.InstancePath)
		if instancePath == "" {
			continue
		}
		if _, seen := seenInstancePaths[instancePath]; seen {
			continue
		}
		seenInstancePaths[instancePath] = struct{}{}
		switch strings.ToLower(strings.TrimSpace(ref.InstanceType)) {
		case "meshpart":
			meshPartCount++
		case "part":
			partCount++
		}
	}
	return meshPartCount, partCount
}

func countEstimatedDrawCalls(mapParts []rbxlHeatmapMapPart, refs []positionedRustyAssetToolResult) int {
	renderInfos := buildReportGenerationRenderInfos(mapParts, refs)
	if len(renderInfos) == 0 {
		return 0
	}
	drawCalls := 0
	meshDrawKeys := map[string]struct{}{}
	for _, renderInfo := range renderInfos {
		switch normalizeReportGenerationInstanceType(renderInfo.InstanceType) {
		case "meshpart":
			drawCallKey := estimateMeshPartDrawCallKey(renderInfo)
			if drawCallKey == "" {
				drawCalls++
				continue
			}
			meshDrawKeys[drawCallKey] = struct{}{}
		case "":
		default:
			drawCalls++
		}
	}
	return drawCalls + len(meshDrawKeys)
}

func buildReportGenerationRenderInfos(mapParts []rbxlHeatmapMapPart, refs []positionedRustyAssetToolResult) map[string]reportGenerationInstanceRenderInfo {
	renderInfos := map[string]reportGenerationInstanceRenderInfo{}
	for _, part := range mapParts {
		instancePath := strings.TrimSpace(part.InstancePath)
		if instancePath == "" {
			continue
		}
		renderInfos[instancePath] = reportGenerationInstanceRenderInfo{
			InstanceType: strings.TrimSpace(part.InstanceType),
			InstancePath: instancePath,
			MaterialKey:  strings.TrimSpace(part.MaterialKey),
			X:            part.CenterX,
			Z:            part.CenterZ,
			HasPosition:  true,
		}
	}
	for _, ref := range refs {
		instancePath, instanceType := reportGenerationRefTarget(ref)
		if instancePath == "" {
			continue
		}
		if !isReportGenerationFallbackRenderableType(instanceType) {
			if len(mapParts) == 0 {
				continue
			}
			if _, found := renderInfos[instancePath]; !found {
				continue
			}
		}
		renderInfo := renderInfos[instancePath]
		if renderInfo.InstancePath == "" {
			renderInfo.InstancePath = instancePath
		}
		if renderInfo.InstanceType == "" {
			renderInfo.InstanceType = instanceType
		}
		if !renderInfo.HasPosition && ref.WorldX != nil && ref.WorldZ != nil {
			renderInfo.X = *ref.WorldX
			renderInfo.Z = *ref.WorldZ
			renderInfo.HasPosition = true
		}
		contentKey := reportGenerationAssetContentKey(ref.ID, ref.RawContent)
		normalizedPropertyName := strings.ToLower(strings.TrimSpace(ref.PropertyName))
		switch {
		case isReportGenerationMeshContentProperty(normalizedPropertyName):
			renderInfo.MeshContentKey = contentKey
		case isReportGenerationTextureContentProperty(normalizedPropertyName):
			renderInfo.TextureContentKey = contentKey
		case isReportGenerationSurfaceAppearanceProperty(normalizedPropertyName, ref.InstanceType):
			if renderInfo.SurfaceAppearanceProps == nil {
				renderInfo.SurfaceAppearanceProps = map[string]string{}
			}
			renderInfo.SurfaceAppearanceProps[normalizedPropertyName] = contentKey
		}
		renderInfos[instancePath] = renderInfo
	}
	return renderInfos
}

func reportGenerationRefTarget(ref positionedRustyAssetToolResult) (string, string) {
	instancePath := strings.TrimSpace(ref.InstancePath)
	instanceType := strings.TrimSpace(ref.InstanceType)
	if strings.EqualFold(instanceType, "SurfaceAppearance") {
		parentPath := parentReportGenerationInstancePath(instancePath)
		if parentPath != "" {
			return parentPath, "MeshPart"
		}
	}
	return instancePath, instanceType
}

func reportGenerationMeshTriangleInstanceKey(ref positionedRustyAssetToolResult) string {
	if !isReportGenerationMeshContentProperty(strings.ToLower(strings.TrimSpace(ref.PropertyName))) {
		return ""
	}
	instancePath, _ := reportGenerationRefTarget(ref)
	return strings.TrimSpace(instancePath)
}

func parentReportGenerationInstancePath(instancePath string) string {
	trimmedPath := strings.TrimSpace(instancePath)
	if trimmedPath == "" {
		return ""
	}
	lastDotIndex := strings.LastIndex(trimmedPath, ".")
	if lastDotIndex <= 0 {
		return ""
	}
	return strings.TrimSpace(trimmedPath[:lastDotIndex])
}

func reportGenerationAssetContentKey(assetID int64, rawContent string) string {
	return scanAssetReferenceKey(assetID, rawContent)
}

func normalizeReportGenerationInstanceType(instanceType string) string {
	return strings.ToLower(strings.TrimSpace(instanceType))
}

func isReportGenerationFallbackRenderableType(instanceType string) bool {
	switch normalizeReportGenerationInstanceType(instanceType) {
	case "meshpart", "part":
		return true
	default:
		return false
	}
}

func isReportGenerationMeshContentProperty(propertyName string) bool {
	return propertyName == "meshid" || propertyName == "meshcontent"
}

func isReportGenerationTextureContentProperty(propertyName string) bool {
	return propertyName == "textureid" || propertyName == "texturecontent"
}

func isReportGenerationSurfaceAppearanceProperty(propertyName string, instanceType string) bool {
	normalizedInstanceType := normalizeReportGenerationInstanceType(instanceType)
	return normalizedInstanceType == "surfaceappearance" || strings.Contains(propertyName, "mapcontent")
}

func estimateMeshPartDrawCallKey(renderInfo reportGenerationInstanceRenderInfo) string {
	instancePath := strings.TrimSpace(renderInfo.InstancePath)
	meshContentKey := strings.TrimSpace(renderInfo.MeshContentKey)
	if meshContentKey == "" {
		if instancePath == "" {
			return ""
		}
		return "meshpart:" + instancePath
	}
	surfaceAppearanceKey := reportGenerationSurfaceAppearanceKey(renderInfo.SurfaceAppearanceProps)
	if surfaceAppearanceKey != "" {
		return "meshpart:" + meshContentKey + "|surface:" + surfaceAppearanceKey
	}
	textureContentKey := strings.TrimSpace(renderInfo.TextureContentKey)
	if textureContentKey != "" {
		return "meshpart:" + meshContentKey + "|texture:" + textureContentKey
	}
	materialKey := strings.TrimSpace(renderInfo.MaterialKey)
	if materialKey != "" {
		return "meshpart:" + meshContentKey + "|material:" + materialKey
	}
	if instancePath == "" {
		return ""
	}
	return "meshpart:" + instancePath
}

func reportGenerationSurfaceAppearanceKey(properties map[string]string) string {
	if len(properties) == 0 {
		return ""
	}
	propertyNames := make([]string, 0, len(properties))
	for propertyName := range properties {
		propertyNames = append(propertyNames, propertyName)
	}
	sort.Strings(propertyNames)
	parts := make([]string, 0, len(propertyNames))
	for _, propertyName := range propertyNames {
		parts = append(parts, propertyName+"="+properties[propertyName])
	}
	return strings.Join(parts, "|")
}

func isRobloxDOMFilePath(filePath string) bool {
	extension := strings.ToLower(filepath.Ext(strings.TrimSpace(filePath)))
	return extension == ".rbxl" || extension == ".rbxm"
}

func buildPerformanceProfileUI(assetTypeLabel string, overallGrade string, overallScore int, grades []performanceGrade, percentiles reportCellPercentiles) fyne.CanvasObject {
	headingText := "Performance Profile"
	if strings.TrimSpace(assetTypeLabel) != "" {
		headingText = fmt.Sprintf("Performance Profile (%s)", assetTypeLabel)
	}
	heading := canvas.NewText(headingText, theme.ForegroundColor())
	heading.TextSize = 20
	heading.TextStyle = fyne.TextStyle{Bold: true}

	rangeNote := widget.NewLabel("")
	if percentiles.WholeFileMode {
		rangeNote.SetText("Graded over the whole file as one cell")
	} else if percentiles.CellCount > 0 {
		rangeNote.SetText(fmt.Sprintf("Graded over %d cells (%.0fm x %.0fm)", percentiles.CellCount, percentiles.CellSizeStuds, percentiles.CellSizeStuds))
	} else {
		rangeNote.SetText("Graded from raw totals (no spatial data)")
	}
	rangeNote.Alignment = fyne.TextAlignCenter

	overallColor := gradeColor(overallGrade)
	overallText := canvas.NewText(fmt.Sprintf("Overall: %s (%d%%)", overallGrade, overallScore), overallColor)
	overallText.TextSize = 28
	overallText.TextStyle = fyne.TextStyle{Bold: true}

	content := container.NewVBox(
		widget.NewSeparator(),
		container.NewCenter(heading),
		container.NewCenter(rangeNote),
		container.NewCenter(overallText),
		widget.NewSeparator(),
	)
	for _, g := range grades {
		gradeText := canvas.NewText(g.Grade, gradeColor(g.Grade))
		gradeText.TextSize = 18
		gradeText.TextStyle = fyne.TextStyle{Bold: true}

		labelText := ttwidget.NewLabel(g.Label)
		labelText.TextStyle = fyne.TextStyle{Bold: true}
		if g.MetricDescription != "" {
			labelText.SetToolTip(g.MetricDescription)
		}

		valueText := ttwidget.NewLabel(g.Value)
		valueText.SetToolTip(g.Description)

		totalText := ttwidget.NewLabel("")
		if g.TotalValue != "" {
			totalText.SetText(g.TotalValue)
		}

		row := container.NewHBox(
			container.NewGridWrap(fyne.NewSize(30, 30), container.NewCenter(gradeText)),
			container.NewGridWrap(fyne.NewSize(160, 30), labelText),
			container.NewGridWrap(fyne.NewSize(130, 30), valueText),
			container.NewGridWrap(fyne.NewSize(140, 30), totalText),
		)
		content.Add(row)
		content.Add(widget.NewSeparator())
	}
	return content
}

func gradeColor(grade string) color.Color {
	switch grade {
	case gradeAPlus:
		return color.RGBA{R: 0, G: 200, B: 83, A: 255}
	case gradeA:
		return color.RGBA{R: 76, G: 175, B: 80, A: 255}
	case gradeB:
		return color.RGBA{R: 139, G: 195, B: 74, A: 255}
	case gradeC:
		return color.RGBA{R: 255, G: 193, B: 7, A: 255}
	case gradeD:
		return color.RGBA{R: 255, G: 152, B: 0, A: 255}
	case gradeE:
		return color.RGBA{R: 255, G: 87, B: 34, A: 255}
	default:
		return color.RGBA{R: 244, G: 67, B: 54, A: 255}
	}
}
