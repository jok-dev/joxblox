package reportgeneration

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

	"joxblox/internal/app/common"
	"joxblox/internal/app/loader"
	"joxblox/internal/app/report"
	"joxblox/internal/app/ui"
	"joxblox/internal/app/ui/tabs/heatmap"
	"joxblox/internal/debug"
	"joxblox/internal/extractor"
	"joxblox/internal/format"
	"joxblox/internal/heatmap"
)

type reportGenerationResolvedAsset struct {
	Stats      heatmap.AssetStats
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

func NewReportGenerationTab(window fyne.Window, onViewInScan func(path string, workspaceOnly bool, oversizedTextureThreshold float64), onViewInHeatmap func(string)) (fyne.CanvasObject, func(string)) {
	selectedFilePath := ""
	currentSummary := report.Summary{}
	currentCells := []heatmap.Cell{}
	currentAssetType := report.AssetTypeConfig{}
	currentMismatchedPBR := []report.MismatchedPBRMaterialDetail{}
	currentOversizedTextures := []oversizedTextureDetail{}
	currentDuplicateGroups := []duplicateGroupDetail{}
	hasSummary := false
	var loadToken atomic.Uint64
	var loading atomic.Bool
	var cancelLoadButton *widget.Button

	filePathLabel := widget.NewLabel("Drop .rbxl/.rbxm or choose file")
	filePathLabel.Wrapping = fyne.TextTruncate
	warningBanner := ui.NewMaterialVariantWarningBanner(window)

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
	setWarning := func(warningData ui.MaterialVariantWarningData) { warningBanner.SetWarning(warningData) }

	refreshProfile := func() {
		profileContainer.RemoveAll()
		if !hasSummary {
			profileContainer.Hide()
			return
		}
		assetType := currentAssetType
		if assetType.Label == "" {
			assetType = report.DefaultAssetType()
		}
		percentiles := report.ComputeReportCellPercentiles(assetType, currentCells, currentSummary)
		grades := report.ComputePerformanceProfileForAssetType(assetType, percentiles, currentSummary)
		hasDuplicates := currentSummary.DuplicateCount > 0
		overall := report.OverallPerformanceGrade(grades, hasDuplicates)
		overallScore := report.OverallPerformanceScorePercent(grades, hasDuplicates)
		var onViewMismatchedPBR func()
		if len(currentMismatchedPBR) > 0 {
			details := currentMismatchedPBR
			onViewMismatchedPBR = func() { showMismatchedPBRDialog(window, details) }
		}
		var onViewOversized func()
		if len(currentOversizedTextures) > 0 {
			details := currentOversizedTextures
			onViewOversized = func() { showOversizedTexturesDialog(window, details) }
		}
		var onViewDuplicates func()
		if len(currentDuplicateGroups) > 0 {
			groups := currentDuplicateGroups
			onViewDuplicates = func() { showDuplicatesDialog(window, groups) }
		}
		profileContainer.Add(buildPerformanceProfileUI(assetType.Label, overall, overallScore, grades, percentiles, onViewMismatchedPBR, onViewOversized, onViewDuplicates))
		if onViewInScan != nil || onViewInHeatmap != nil {
			navButtons := container.NewHBox()
			if onViewInScan != nil {
				viewInScanButton := widget.NewButtonWithIcon("View in Scan", theme.SearchIcon(), func() {
					onViewInScan(selectedFilePath, workspaceOnlyCheck.Checked, currentAssetType.OversizedTextureThreshold)
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
		currentAssetType = report.AssetTypeConfig{}
		hasSummary = false
		currentSummary = report.Summary{}
		currentCells = nil
		currentMismatchedPBR = nil
		currentOversizedTextures = nil
		currentDuplicateGroups = nil
		profileContainer.Hide()
		setWarning(ui.MaterialVariantWarningData{})
		setBusy(false)
		statusLabel.SetText("Loading canceled")
		if showRetryDialog {
			showAssetTypeDialog()
		}
	}
	startReportLoad := func(assetType report.AssetTypeConfig) {
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
		currentSummary = report.Summary{}
		currentCells = nil
		currentMismatchedPBR = nil
		currentOversizedTextures = nil
		currentDuplicateGroups = nil
		profileContainer.Hide()
		setWarning(ui.MaterialVariantWarningData{})
		statusLabel.SetText(fmt.Sprintf("Loading %s asset...", assetType.Label))
		setBusy(true)

		trimmedPath := selectedFilePath
		filePathLabel.SetText(trimmedPath)

		var pathPrefixes []string
		if workspaceOnlyCheck.Checked && strings.ToLower(filepath.Ext(trimmedPath)) == ".rbxl" {
			pathPrefixes = []string{"Workspace", "MaterialService"}
		}

		token := loadToken.Add(1)
		go func(expectedToken uint64, sourcePath string, prefixes []string, selectedAssetType report.AssetTypeConfig) {
			isCanceled := func() bool {
				return loadToken.Load() != expectedToken
			}

			positionedRefs, extractErr := extractor.ExtractPositionedRefs(sourcePath, prefixes, nil)
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

			mapPartsRaw, mapPartsErr := extractor.ExtractMapRenderParts(sourcePath, prefixes, nil)
			if mapPartsErr != nil {
				debug.Logf("Report generation map extraction failed for %s: %s", sourcePath, mapPartsErr.Error())
			}
			if isCanceled() {
				return
			}
			instanceCount, instanceCountErr := extractor.ExtractInstanceCount(sourcePath, prefixes, nil)
			if instanceCountErr != nil {
				debug.Logf("Report generation instance-count extraction failed for %s: %s", sourcePath, instanceCountErr.Error())
			}
			if isCanceled() {
				return
			}
			warningData, warningErr := ui.BuildRBXLMissingMaterialVariantWarning(sourcePath, prefixes, nil)
			if warningErr != nil {
				debug.Logf("Report generation material warning extraction failed for %s: %s", sourcePath, warningErr.Error())
			}
			if isCanceled() {
				return
			}
			mapParts := heatmaptab.ConvertRustMapParts(mapPartsRaw)
			reportMeshPartCount, reportPartCount := countReportGenerationParts(mapParts, positionedRefs)
			debug.Logf(
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

			uniqueRefsByKey := map[string]heatmap.AssetReference{}
			for _, ref := range positionedRefs {
				if ref.ID <= 0 {
					continue
				}
				key := extractor.AssetReferenceKey(ref.ID, ref.RawContent)
				uniqueRefsByKey[key] = heatmap.AssetReference{
					AssetID:    ref.ID,
					AssetInput: strings.TrimSpace(ref.RawContent),
				}
			}
			refsToResolve := make([]heatmap.AssetReference, 0, len(uniqueRefsByKey))
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

			summary, points, mismatchedPBRDetails, oversizedDetails, duplicateGroups := buildReportSummaryAndPoints(positionedRefs, resolved, mapParts, assetType.OversizedTextureThreshold)
			summary.InstanceCount = instanceCount.Count
			cells := buildReportGenerationCells(points, mapParts, positionedRefs, resolved, instanceCount.Positions)
			debug.Logf(
				"Report generation summary for %s (%s): meshparts=%d parts=%d points=%d cells=%d resolved=%d unique-assets=%d",
				sourcePath,
				selectedAssetType.Label,
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
				currentMismatchedPBR = mismatchedPBRDetails
				currentOversizedTextures = oversizedDetails
				currentDuplicateGroups = duplicateGroups
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
		buttons := make([]fyne.CanvasObject, 0, len(report.AssetTypeConfigs)+2)
		description := widget.NewLabel("Choose the asset type for this report.")
		description.Alignment = fyne.TextAlignCenter
		description.Wrapping = fyne.TextWrapWord
		buttons = append(buttons, description)
		for _, assetType := range report.AssetTypeConfigs {
			assetType := assetType
			button := widget.NewButton(assetType.Label, func() {
				startReportLoad(assetType)
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
		if !common.IsRobloxDOMFilePath(trimmedPath) {
			statusLabel.SetText("Only .rbxl/.rbxm")
			return
		}

		selectedFilePath = trimmedPath
		filePathLabel.SetText(selectedFilePath)
		currentAssetType = report.AssetTypeConfig{}
		hasSummary = false
		currentSummary = report.Summary{}
		currentCells = nil
		currentMismatchedPBR = nil
		currentOversizedTextures = nil
		currentDuplicateGroups = nil
		profileContainer.Hide()
		setWarning(ui.MaterialVariantWarningData{})
		statusLabel.SetText("Choose an asset type to generate the report")
		setBusy(false)
		showAssetTypeDialog()
	}

	browseButton := widget.NewButtonWithIcon("Choose File", theme.FolderOpenIcon(), func() {
		common.PickRBXLSource(window, loadReportFile, func(err error) {
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
		warningBanner.BannerRoot(),
	)

	return container.NewBorder(
		controls,
		statusLabel,
		nil,
		nil,
		container.NewVScroll(profileContainer),
	), loadReportFile
}

func resolveReportGenerationAssets(references []heatmap.AssetReference, onProgress func(done int, total int, memoryRequestCount int, diskRequestCount int, networkRequestCount int), shouldCancel func() bool) map[string]reportGenerationResolvedAsset {
	if len(references) == 0 {
		return map[string]reportGenerationResolvedAsset{}
	}

	jobs := make(chan heatmap.AssetReference, len(references))
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
				referenceKey := extractor.AssetReferenceKey(reference.AssetID, reference.AssetInput)
				trace := &loader.AssetRequestTrace{}
				previewResult, previewErr := loader.LoadAssetStatsPreviewForReferenceWithTrace(reference.AssetID, reference.AssetInput, trace)
				if shouldCancel != nil && shouldCancel() {
					return
				}
				resolvedAsset := reportGenerationResolvedAsset{
					Stats: heatmap.AssetStats{AssetID: reference.AssetID},
				}
				if previewErr == nil && previewResult != nil {
					resolvedAsset = reportGenerationResolvedAsset{
						Stats:      buildReportGenerationStatsFromPreview(reference.AssetID, previewResult),
						FileSHA256: loader.NormalizeHash(loader.PreviewSHA256(previewResult)),
					}
				}
				resolvedMutex.Lock()
				resolvedByReferenceKey[referenceKey] = resolvedAsset
				resolvedMutex.Unlock()
				if onProgress != nil && (shouldCancel == nil || !shouldCancel()) {
					switch trace.ClassifyRequestSource() {
					case heatmap.SourceNetwork:
						networkRequestCount.Add(1)
					case heatmap.SourceDisk:
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

func buildReportGenerationCells(points []heatmaptab.RBXLHeatmapPoint, mapParts []heatmaptab.RBXLHeatmapMapPart, refs []extractor.PositionedResult, resolved map[string]reportGenerationResolvedAsset, instancePositions []extractor.InstancePosition) []heatmap.Cell {
	if len(points) == 0 && len(mapParts) == 0 {
		return nil
	}

	minX, maxX, minZ, maxZ := heatmaptab.HeatmapSceneBounds(points, mapParts)
	rangeX := maxX - minX
	rangeZ := maxZ - minZ
	longestRange := math.Max(rangeX, rangeZ)
	if longestRange <= 0 {
		longestRange = reportGenerationCellSizeStuds
	}

	gridDivisions := max(1, int(math.Ceil(longestRange/reportGenerationCellSizeStuds)))
	paddedLongestRange := float64(gridDivisions) * reportGenerationCellSizeStuds
	scene := &heatmaptab.RBXLHeatmapScene{
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

	cells, cellSizeWorld, columnCount, rowCount, _ := heatmaptab.BuildHeatmapCells(scene, gridDivisions)
	if cellSizeWorld <= 0 {
		cellSizeWorld = reportGenerationCellSizeStuds
	}

	renderInfos := buildReportGenerationRenderInfos(mapParts, refs)
	partCountsByCell := map[string]heatmap.Totals{}
	meshDrawKeysByCell := map[string]map[string]struct{}{}
	for _, renderInfo := range renderInfos {
		if !renderInfo.HasPosition {
			continue
		}
		column := format.Clamp(int(math.Floor((renderInfo.X-scene.MinimumX)/cellSizeWorld)), 0, columnCount-1)
		row := format.Clamp(int(math.Floor((renderInfo.Z-scene.MinimumZ)/cellSizeWorld)), 0, rowCount-1)
		cellKey := heatmaptab.HeatmapCellKey(row, column)
		counts := partCountsByCell[cellKey]
		switch report.NormalizeInstanceType(renderInfo.InstanceType) {
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
		cellKey := heatmaptab.HeatmapCellKey(cell.Row, cell.Column)
		cellIndexByKey[cellKey] = index
	}
	for cellKey, counts := range partCountsByCell {
		index, found := cellIndexByKey[cellKey]
		if !found {
			var row, column int
			if _, scanErr := fmt.Sscanf(cellKey, "%d:%d", &row, &column); scanErr != nil {
				continue
			}
			cells = append(cells, heatmap.Cell{
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

	applyPerCellSurfaceAppearanceCorrection(cells, cellIndexByKey, refs, resolved, renderInfos, scene, cellSizeWorld, columnCount, rowCount)

	for _, pos := range instancePositions {
		x := float64(pos.X)
		z := float64(pos.Z)
		if x < scene.MinimumX || x >= scene.MinimumX+float64(columnCount)*cellSizeWorld {
			continue
		}
		if z < scene.MinimumZ || z >= scene.MinimumZ+float64(rowCount)*cellSizeWorld {
			continue
		}
		column := format.Clamp(int(math.Floor((x-scene.MinimumX)/cellSizeWorld)), 0, columnCount-1)
		row := format.Clamp(int(math.Floor((z-scene.MinimumZ)/cellSizeWorld)), 0, rowCount-1)
		cellKey := heatmaptab.HeatmapCellKey(row, column)
		index, found := cellIndexByKey[cellKey]
		if !found {
			cells = append(cells, heatmap.Cell{
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
		cells[index].Stats.InstanceCount++
	}

	sort.Slice(cells, func(left int, right int) bool {
		if cells[left].Row == cells[right].Row {
			return cells[left].Column < cells[right].Column
		}
		return cells[left].Row < cells[right].Row
	})
	return cells
}

// applyPerCellSurfaceAppearanceCorrection adjusts each cell's
// BC1PixelCount so per-cell GPU-memory grading sees engine-allocated
// MR-pack VRAM (one BC1 per unique normal map in the cell) instead of
// the raw per-asset tally produced by BuildHeatmapCells.
func applyPerCellSurfaceAppearanceCorrection(
	cells []heatmap.Cell,
	cellIndexByKey map[string]int,
	refs []extractor.PositionedResult,
	resolved map[string]reportGenerationResolvedAsset,
	renderInfos map[string]reportGenerationInstanceRenderInfo,
	scene *heatmaptab.RBXLHeatmapScene,
	cellSizeWorld float64,
	columnCount, rowCount int,
) {
	if len(cells) == 0 || len(refs) == 0 || cellSizeWorld <= 0 {
		return
	}
	cellKeyForRef := func(ref extractor.PositionedResult) (string, bool) {
		x, z, ok := refWorldXZ(ref, renderInfos)
		if !ok {
			return "", false
		}
		column := format.Clamp(int(math.Floor((x-scene.MinimumX)/cellSizeWorld)), 0, columnCount-1)
		row := format.Clamp(int(math.Floor((z-scene.MinimumZ)/cellSizeWorld)), 0, rowCount-1)
		return heatmaptab.HeatmapCellKey(row, column), true
	}
	materialsByCell := buildSurfaceAppearanceMaterialsByOwner(refs, resolved, cellKeyForRef)
	for cellKey, materials := range materialsByCell {
		index, ok := cellIndexByKey[cellKey]
		if !ok {
			continue
		}
		delta := report.ComputeSurfaceAppearanceMemoryCorrection(materials)
		report.ApplyDeltaClamped(&cells[index].Stats.BC1PixelCount, delta.NetBC1Pixels())
		report.ApplyDeltaClamped(&cells[index].Stats.BC3PixelCount, delta.NetBC3Pixels())
	}
}

// refWorldXZ returns the (x, z) world position to attribute a ref to. If
// the ref carries world coords directly it uses those; otherwise it
// looks up the owning part via renderInfos.
func refWorldXZ(ref extractor.PositionedResult, renderInfos map[string]reportGenerationInstanceRenderInfo) (float64, float64, bool) {
	if ref.WorldX != nil && ref.WorldZ != nil {
		return *ref.WorldX, *ref.WorldZ, true
	}
	ownerPath, _ := report.PositionedRefTarget(ref)
	if info, ok := renderInfos[ownerPath]; ok && info.HasPosition {
		return info.X, info.Z, true
	}
	return 0, 0, false
}

func buildReportGenerationStatsFromPreview(assetID int64, previewResult *loader.AssetPreviewResult) heatmap.AssetStats {
	return loader.BuildAssetStatsFromPreview(assetID, previewResult)
}

type duplicateGroupDetail struct {
	FileSHA256        string
	AssetBytes        int64
	AssetIDs          []int64
	SampleInstancePath string
}

func buildReportSummaryAndPoints(refs []extractor.PositionedResult, resolved map[string]reportGenerationResolvedAsset, mapParts []heatmaptab.RBXLHeatmapMapPart, oversizedTextureThreshold float64) (report.Summary, []heatmaptab.RBXLHeatmapPoint, []report.MismatchedPBRMaterialDetail, []oversizedTextureDetail, []duplicateGroupDetail) {
	summary := report.Summary{}
	uniqueAssetIDs := map[int64]struct{}{}
	uniqueReferenceKeys := map[string]struct{}{}
	hashCounts := map[string]int{}
	resolvedUniqueKeys := make([]string, 0, len(resolved))
	seenResolvedKeys := map[string]struct{}{}
	instancePathByKey := map[string]string{}
	assetIDByKey := map[string]int64{}
	for _, ref := range refs {
		if ref.ID <= 0 {
			continue
		}

		summary.ReferenceCount++
		uniqueAssetIDs[ref.ID] = struct{}{}
		key := extractor.AssetReferenceKey(ref.ID, ref.RawContent)
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
		assetIDByKey[key] = ref.ID
		if instancePath := strings.TrimSpace(ref.InstancePath); instancePath != "" {
			instancePathByKey[key] = instancePath
		}
		if asset.FileSHA256 != "" {
			hashCounts[asset.FileSHA256]++
			resolvedUniqueKeys = append(resolvedUniqueKeys, key)
		}
		summary.TotalBytes += int64(stats.TotalBytes)
		summary.TextureBytes += int64(stats.TextureBytes)
		summary.TexturePixelCount += stats.PixelCount
		if stats.PixelCount > 0 {
			isBC3 := loader.ClassifyAsBC3(stats.HasAlphaChannel, stats.NonOpaqueAlphaPixels, ref.PropertyName)
			exactBytes := report.EstimateGPUTextureBytesExact(stats.Width, stats.Height, isBC3)
			if isBC3 {
				summary.BC3PixelCount += stats.PixelCount
				summary.BC3BytesExact += exactBytes
			} else {
				summary.BC1PixelCount += stats.PixelCount
				summary.BC1BytesExact += exactBytes
			}
		}
		summary.MeshBytes += int64(stats.MeshBytes)
		summary.ResolvedCount++
	}

	seenCounts := map[string]int{}
	duplicateGroupsByHash := map[string]*duplicateGroupDetail{}
	for _, key := range resolvedUniqueKeys {
		asset := resolved[key]
		if asset.FileSHA256 == "" {
			continue
		}
		if hashCounts[asset.FileSHA256] < 2 {
			continue
		}
		if seenCounts[asset.FileSHA256] >= 1 && asset.Stats.TotalBytes >= 10*1024 {
			summary.DuplicateCount++
			summary.DuplicateSizeBytes += int64(asset.Stats.TotalBytes)
		}
		seenCounts[asset.FileSHA256]++

		if asset.Stats.TotalBytes < 10*1024 {
			continue
		}
		group, ok := duplicateGroupsByHash[asset.FileSHA256]
		if !ok {
			group = &duplicateGroupDetail{FileSHA256: asset.FileSHA256, AssetBytes: int64(asset.Stats.TotalBytes)}
			duplicateGroupsByHash[asset.FileSHA256] = group
		}
		group.AssetIDs = append(group.AssetIDs, assetIDByKey[key])
		if group.SampleInstancePath == "" {
			group.SampleInstancePath = instancePathByKey[key]
		}
	}
	duplicateGroups := make([]duplicateGroupDetail, 0, len(duplicateGroupsByHash))
	for _, group := range duplicateGroupsByHash {
		if len(group.AssetIDs) < 2 {
			continue
		}
		duplicateGroups = append(duplicateGroups, *group)
	}
	sort.Slice(duplicateGroups, func(i, j int) bool {
		wastedI := duplicateGroups[i].AssetBytes * int64(len(duplicateGroups[i].AssetIDs)-1)
		wastedJ := duplicateGroups[j].AssetBytes * int64(len(duplicateGroups[j].AssetIDs)-1)
		return wastedI > wastedJ
	})
	summary.UniqueAssetCount = len(uniqueAssetIDs)
	oversizedDetails := collectReportGenerationOversizedTextures(refs, resolved, mapParts, oversizedTextureThreshold)
	summary.OversizedTextureCount = len(oversizedDetails)
	materials := collectSurfaceAppearanceMaterialSlots(refs, resolved)
	summary.MismatchedPBRMaterialCount, summary.PBRMaterialCount = report.CountMismatchedPBRMaterials(materials)
	mismatchedPBRDetails := report.CollectMismatchedPBRMaterials(materials)
	correction := report.ApplySurfaceAppearanceMemoryCorrections(&summary, materials)
	logGPUTextureMemoryBreakdown(summary, correction)

	points := make([]heatmaptab.RBXLHeatmapPoint, 0, len(refs))
	for _, ref := range refs {
		if ref.ID <= 0 || ref.WorldX == nil || ref.WorldY == nil || ref.WorldZ == nil {
			continue
		}
		key := extractor.AssetReferenceKey(ref.ID, ref.RawContent)
		asset, found := resolved[key]
		if !found || asset.Stats.TotalBytes <= 0 {
			continue
		}
		points = append(points, heatmaptab.RBXLHeatmapPoint{
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
	return summary, points, mismatchedPBRDetails, oversizedDetails, duplicateGroups
}

type oversizedTextureDetail struct {
	AssetID          int64
	InstancePath     string
	Width            int
	Height           int
	TextureBytes     int64
	SceneSurfaceArea float64
	Score            float64
}

func collectReportGenerationOversizedTextures(refs []extractor.PositionedResult, resolved map[string]reportGenerationResolvedAsset, mapParts []heatmaptab.RBXLHeatmapMapPart, threshold float64) []oversizedTextureDetail {
	if threshold <= 0 {
		threshold = loader.DefaultLargeTextureThreshold
	}
	if len(refs) == 0 || len(resolved) == 0 {
		return nil
	}

	areaByPath := loader.BuildSceneSurfaceAreaIndex(mapParts)
	maxAreaByReferenceKey := map[string]float64{}
	type refContext struct {
		assetID      int64
		instancePath string
		area         float64
	}
	bestRefByKey := map[string]refContext{}
	for _, ref := range refs {
		if ref.ID <= 0 {
			continue
		}
		referenceKey := extractor.AssetReferenceKey(ref.ID, ref.RawContent)
		instancePath := strings.TrimSpace(ref.InstancePath)
		area := loader.EstimateSceneSurfaceAreaForPaths(instancePath, nil, areaByPath)
		maxAreaByReferenceKey[referenceKey] = loader.MaxPositiveFloat64(maxAreaByReferenceKey[referenceKey], area)
		if existing, ok := bestRefByKey[referenceKey]; !ok || area > existing.area {
			bestRefByKey[referenceKey] = refContext{assetID: ref.ID, instancePath: instancePath, area: area}
		}
	}

	const minOversizedTextureBytes = 100 * 1024
	details := make([]oversizedTextureDetail, 0)
	for referenceKey, resolvedAsset := range resolved {
		textureBytes := resolvedAsset.Stats.TextureBytes
		if textureBytes < minOversizedTextureBytes {
			continue
		}
		score := loader.ComputeLargeTextureScore(textureBytes, maxAreaByReferenceKey[referenceKey])
		if score < threshold {
			continue
		}
		ctx := bestRefByKey[referenceKey]
		details = append(details, oversizedTextureDetail{
			AssetID:          ctx.assetID,
			InstancePath:     ctx.instancePath,
			Width:            resolvedAsset.Stats.Width,
			Height:           resolvedAsset.Stats.Height,
			TextureBytes:     int64(textureBytes),
			SceneSurfaceArea: maxAreaByReferenceKey[referenceKey],
			Score:            score,
		})
	}
	sort.Slice(details, func(i, j int) bool { return details[i].Score > details[j].Score })
	return details
}

func countReportGenerationParts(mapParts []heatmaptab.RBXLHeatmapMapPart, refs []extractor.PositionedResult) (int, int) {
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

func countEstimatedDrawCalls(mapParts []heatmaptab.RBXLHeatmapMapPart, refs []extractor.PositionedResult) int {
	renderInfos := buildReportGenerationRenderInfos(mapParts, refs)
	if len(renderInfos) == 0 {
		return 0
	}
	drawCalls := 0
	meshDrawKeys := map[string]struct{}{}
	for _, renderInfo := range renderInfos {
		switch report.NormalizeInstanceType(renderInfo.InstanceType) {
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

func logGPUTextureMemoryBreakdown(summary report.Summary, correction report.SurfaceAppearanceMemoryCorrectionSummary) {
	totalBytes := summary.BC1BytesExact + summary.BC3BytesExact
	debug.Logf(
		"GPU texture memory breakdown (exact per-mip, matches RenderDoc):\n"+
			"  raw per-asset BC1: %s (%d pixels)\n"+
			"  + MR packs (1 BC1 per unique normal map): %d blank + %d custom = +%s (%d pixels)\n"+
			"  - standalone M/R BC1s baked into MR packs: -%s (%d pixels)\n"+
			"  corrected BC1: %s (%d pixels)\n"+
			"  raw per-asset BC3: %s (%d pixels)\n"+
			"  + normal upscale to paired color (across %d normals): +%s (%d pixels)\n"+
			"  corrected BC3: %s (%d pixels)\n"+
			"  total GPU texture memory: %s",
		format.FormatSizeAuto64(correction.PreCorrectionBC1Bytes),
		correction.PreCorrectionBC1Pixels,
		correction.BlankMRGroupCount,
		correction.CustomMRGroupCount,
		format.FormatSizeAuto64(correction.AddedMRPackBytes),
		correction.AddedMRPackPixels,
		format.FormatSizeAuto64(correction.SubtractedStandaloneBytes),
		correction.SubtractedStandalonePixels,
		format.FormatSizeAuto64(summary.BC1BytesExact),
		summary.BC1PixelCount,
		format.FormatSizeAuto64(correction.PreCorrectionBC3Bytes),
		correction.PreCorrectionBC3Pixels,
		correction.UpscaledNormalCount,
		format.FormatSizeAuto64(correction.AddedNormalUpscaleBytes),
		correction.AddedNormalUpscalePixels,
		format.FormatSizeAuto64(summary.BC3BytesExact),
		summary.BC3PixelCount,
		format.FormatSizeAuto64(totalBytes),
	)
}

func collectSurfaceAppearanceMaterialSlots(refs []extractor.PositionedResult, resolved map[string]reportGenerationResolvedAsset) map[string]report.SurfaceAppearanceMaterialSlots {
	allInOneOwner := func(extractor.PositionedResult) (string, bool) { return "", true }
	return buildSurfaceAppearanceMaterialsByOwner(refs, resolved, allInOneOwner)[""]
}

// buildSurfaceAppearanceMaterialsByOwner walks SA-related refs once and
// groups them into per-owner material maps. ownerKeyFn returns the
// owner identifier for a ref (a constant for all-in-one-bucket, or a
// per-cell key for spatial bucketing) — return false to skip the ref.
//
// Two MeshParts named identically (e.g. both "MeshPart") produce the
// same instancePath for their child SurfaceAppearance, so the path
// alone can't be a material key. The rusty extractor emits each
// material's refs as a contiguous block (TexturePack, Normal, Color,
// then the next material's TexturePack, Normal, Color, …); when a slot
// we already filled is about to be overwritten, that's the boundary
// into a new SA on the same path — we suffix the material key with
// #1, #2, … to keep them distinct.
func buildSurfaceAppearanceMaterialsByOwner(
	refs []extractor.PositionedResult,
	resolved map[string]reportGenerationResolvedAsset,
	ownerKeyFn func(ref extractor.PositionedResult) (string, bool),
) map[string]map[string]report.SurfaceAppearanceMaterialSlots {
	materialsByOwner := map[string]map[string]report.SurfaceAppearanceMaterialSlots{}
	type pathState struct {
		currentKey string
		counter    int
	}
	stateByOwnerPath := map[string]map[string]*pathState{}
	for _, ref := range refs {
		if ref.ID <= 0 {
			continue
		}
		normalizedProperty := strings.ToLower(strings.TrimSpace(ref.PropertyName))
		if !report.IsSurfaceAppearanceProperty(normalizedProperty, ref.InstanceType) {
			continue
		}
		instancePath := strings.TrimSpace(ref.InstancePath)
		if instancePath == "" {
			continue
		}
		ownerKey, ok := ownerKeyFn(ref)
		if !ok {
			continue
		}
		refKey := extractor.AssetReferenceKey(ref.ID, ref.RawContent)
		asset, found := resolved[refKey]
		if !found || asset.Stats.PixelCount <= 0 {
			continue
		}
		slot := report.SurfaceAppearanceMaterialSlot{
			AssetKey:   refKey,
			Width:      asset.Stats.Width,
			Height:     asset.Stats.Height,
			PixelCount: asset.Stats.PixelCount,
		}

		materials := materialsByOwner[ownerKey]
		if materials == nil {
			materials = map[string]report.SurfaceAppearanceMaterialSlots{}
			materialsByOwner[ownerKey] = materials
		}
		pathStates := stateByOwnerPath[ownerKey]
		if pathStates == nil {
			pathStates = map[string]*pathState{}
			stateByOwnerPath[ownerKey] = pathStates
		}
		state, hasState := pathStates[instancePath]
		if !hasState {
			state = &pathState{currentKey: instancePath}
			pathStates[instancePath] = state
		}
		slots := materials[state.currentKey]
		if !slots.TryAssignByProperty(normalizedProperty, slot) {
			state.counter++
			state.currentKey = fmt.Sprintf("%s#%d", instancePath, state.counter)
			slots = materials[state.currentKey]
			slots.TryAssignByProperty(normalizedProperty, slot)
		}
		materials[state.currentKey] = slots
	}
	return materialsByOwner
}

func buildReportGenerationRenderInfos(mapParts []heatmaptab.RBXLHeatmapMapPart, refs []extractor.PositionedResult) map[string]reportGenerationInstanceRenderInfo {
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
		instancePath, instanceType := report.PositionedRefTarget(ref)
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
		case report.IsMeshContentProperty(normalizedPropertyName):
			renderInfo.MeshContentKey = contentKey
		case report.IsTextureContentProperty(normalizedPropertyName):
			renderInfo.TextureContentKey = contentKey
		case report.IsSurfaceAppearanceProperty(normalizedPropertyName, ref.InstanceType):
			if renderInfo.SurfaceAppearanceProps == nil {
				renderInfo.SurfaceAppearanceProps = map[string]string{}
			}
			renderInfo.SurfaceAppearanceProps[normalizedPropertyName] = contentKey
		}
		renderInfos[instancePath] = renderInfo
	}
	return renderInfos
}

func reportGenerationMeshTriangleInstanceKey(ref extractor.PositionedResult) string {
	if !report.IsMeshContentProperty(strings.ToLower(strings.TrimSpace(ref.PropertyName))) {
		return ""
	}
	instancePath, _ := report.PositionedRefTarget(ref)
	return strings.TrimSpace(instancePath)
}

func reportGenerationAssetContentKey(assetID int64, rawContent string) string {
	return extractor.AssetReferenceKey(assetID, rawContent)
}

func isReportGenerationFallbackRenderableType(instanceType string) bool {
	switch report.NormalizeInstanceType(instanceType) {
	case "meshpart", "part":
		return true
	default:
		return false
	}
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

func buildPerformanceProfileUI(assetTypeLabel string, overallGrade string, overallScore int, grades []report.PerformanceGrade, percentiles report.CellPercentiles, onViewMismatchedPBR func(), onViewOversized func(), onViewDuplicates func()) fyne.CanvasObject {
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

	if percentiles.CellCount > 0 && !percentiles.WholeFileMode {
		columnHeader := func(text string) fyne.CanvasObject {
			label := widget.NewLabel(text)
			label.TextStyle = fyne.TextStyle{Italic: true}
			return label
		}
		content.Add(container.NewHBox(
			container.NewGridWrap(fyne.NewSize(30, 30), widget.NewLabel("")),
			container.NewGridWrap(fyne.NewSize(160, 30), widget.NewLabel("")),
			container.NewGridWrap(fyne.NewSize(120, 30), widget.NewLabel("")),
			container.NewGridWrap(fyne.NewSize(95, 30), columnHeader("avg/cell")),
			container.NewGridWrap(fyne.NewSize(95, 30), columnHeader("p90/cell")),
			container.NewGridWrap(fyne.NewSize(95, 30), columnHeader("max/cell")),
			container.NewGridWrap(fyne.NewSize(90, 30), widget.NewLabel("")),
		))
	}

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

		avgText := ttwidget.NewLabel(g.AvgCellValue)
		if g.AvgCellValue != "" {
			avgText.SetToolTip("avg per cell")
		}
		totalText := ttwidget.NewLabel(g.TotalValue)
		if g.TotalValue != "" {
			totalText.SetToolTip("p90 per cell — graded on this")
		}
		maxText := ttwidget.NewLabel(g.MaxCellValue)
		if g.MaxCellValue != "" {
			maxText.SetToolTip("max (worst) cell")
		}

		var actionCell fyne.CanvasObject = widget.NewLabel("")
		switch {
		case g.Label == "Mismatched PBR Maps" && onViewMismatchedPBR != nil:
			actionCell = widget.NewButtonWithIcon("View", theme.SearchIcon(), onViewMismatchedPBR)
		case g.Label == "Oversized Textures" && onViewOversized != nil:
			actionCell = widget.NewButtonWithIcon("View", theme.SearchIcon(), onViewOversized)
		case g.Label == "Duplicates" && onViewDuplicates != nil:
			actionCell = widget.NewButtonWithIcon("View", theme.SearchIcon(), onViewDuplicates)
		}

		row := container.NewHBox(
			container.NewGridWrap(fyne.NewSize(30, 30), container.NewCenter(gradeText)),
			container.NewGridWrap(fyne.NewSize(160, 30), labelText),
			container.NewGridWrap(fyne.NewSize(120, 30), valueText),
			container.NewGridWrap(fyne.NewSize(95, 30), avgText),
			container.NewGridWrap(fyne.NewSize(95, 30), totalText),
			container.NewGridWrap(fyne.NewSize(95, 30), maxText),
			container.NewGridWrap(fyne.NewSize(90, 30), actionCell),
		)
		content.Add(row)
		content.Add(widget.NewSeparator())
	}
	return content
}

func showMismatchedPBRDialog(window fyne.Window, details []report.MismatchedPBRMaterialDetail) {
	if len(details) == 0 {
		return
	}
	rows := container.NewVBox()
	for _, detail := range details {
		path := strings.TrimSpace(detail.InstancePath)
		if path == "" {
			path = "(unknown path)"
		}
		header := widget.NewLabel(path)
		header.TextStyle = fyne.TextStyle{Bold: true}
		rows.Add(header)
		rows.Add(widget.NewLabel("    " + formatMismatchedPBRSlots(detail)))
	}
	scroll := container.NewVScroll(rows)
	scroll.SetMinSize(fyne.NewSize(640, 360))
	dialog.ShowCustom(fmt.Sprintf("Mismatched PBR Maps (%d)", len(details)), "Close", scroll, window)
}

func showOversizedTexturesDialog(window fyne.Window, details []oversizedTextureDetail) {
	if len(details) == 0 {
		return
	}
	rows := container.NewVBox()
	for _, d := range details {
		header := widget.NewLabel(formatOversizedTextureHeader(d))
		header.TextStyle = fyne.TextStyle{Bold: true}
		rows.Add(header)
		path := strings.TrimSpace(d.InstancePath)
		if path == "" {
			path = "(no instance path)"
		}
		rows.Add(widget.NewLabel("    " + path))
	}
	scroll := container.NewVScroll(rows)
	scroll.SetMinSize(fyne.NewSize(720, 360))
	dialog.ShowCustom(fmt.Sprintf("Oversized Textures (%d)", len(details)), "Close", scroll, window)
}

func formatOversizedTextureHeader(d oversizedTextureDetail) string {
	parts := []string{}
	if d.AssetID > 0 {
		parts = append(parts, fmt.Sprintf("rbxassetid://%d", d.AssetID))
	}
	if d.Width > 0 && d.Height > 0 {
		parts = append(parts, fmt.Sprintf("%d×%d", d.Width, d.Height))
	}
	if d.TextureBytes > 0 {
		parts = append(parts, format.FormatSizeAuto64(d.TextureBytes))
	}
	parts = append(parts, fmt.Sprintf("score %s", loader.FormatLargeTextureScore(d.Score)))
	if d.SceneSurfaceArea > 0 {
		parts = append(parts, fmt.Sprintf("over %s", loader.FormatSceneSurfaceArea(d.SceneSurfaceArea)))
	}
	return strings.Join(parts, "   ")
}

func showDuplicatesDialog(window fyne.Window, groups []duplicateGroupDetail) {
	if len(groups) == 0 {
		return
	}
	rows := container.NewVBox()
	for _, g := range groups {
		copies := len(g.AssetIDs)
		wasted := g.AssetBytes * int64(copies-1)
		header := widget.NewLabel(fmt.Sprintf("%d× copies   %s each   %s wasted", copies, format.FormatSizeAuto64(g.AssetBytes), format.FormatSizeAuto64(wasted)))
		header.TextStyle = fyne.TextStyle{Bold: true}
		rows.Add(header)
		idStrings := make([]string, 0, len(g.AssetIDs))
		seen := map[int64]struct{}{}
		for _, id := range g.AssetIDs {
			if id <= 0 {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			idStrings = append(idStrings, fmt.Sprintf("rbxassetid://%d", id))
		}
		if len(idStrings) > 0 {
			rows.Add(widget.NewLabel("    " + strings.Join(idStrings, "  ")))
		}
		if path := strings.TrimSpace(g.SampleInstancePath); path != "" {
			rows.Add(widget.NewLabel("    sample: " + path))
		}
	}
	scroll := container.NewVScroll(rows)
	scroll.SetMinSize(fyne.NewSize(720, 360))
	dialog.ShowCustom(fmt.Sprintf("Duplicate Assets (%d groups)", len(groups)), "Close", scroll, window)
}

func formatMismatchedPBRSlots(detail report.MismatchedPBRMaterialDetail) string {
	parts := make([]string, 0, 4)
	appendSlot := func(name string, w, h int) {
		if w <= 0 || h <= 0 {
			return
		}
		parts = append(parts, fmt.Sprintf("%s %d×%d", name, w, h))
	}
	appendSlot("Color", detail.ColorWidth, detail.ColorHeight)
	appendSlot("Normal", detail.NormalWidth, detail.NormalHeight)
	appendSlot("Metalness", detail.MetalnessWidth, detail.MetalnessHeight)
	appendSlot("Roughness", detail.RoughnessWidth, detail.RoughnessHeight)
	return strings.Join(parts, "   ")
}

func gradeColor(grade string) color.Color {
	switch grade {
	case "A+":
		return color.RGBA{R: 0, G: 200, B: 83, A: 255}
	case "A":
		return color.RGBA{R: 76, G: 175, B: 80, A: 255}
	case "B":
		return color.RGBA{R: 139, G: 195, B: 74, A: 255}
	case "C":
		return color.RGBA{R: 255, G: 193, B: 7, A: 255}
	case "D":
		return color.RGBA{R: 255, G: 152, B: 0, A: 255}
	case "E":
		return color.RGBA{R: 255, G: 87, B: 34, A: 255}
	default:
		return color.RGBA{R: 244, G: 67, B: 54, A: 255}
	}
}
