package app

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	fyneDialog "fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
	nativeDialog "github.com/sqweek/dialog"
)

const robloxDOMFileFilterLabel = "Roblox place/model files"

func newRBXLScanTab(
	window fyne.Window,
) (fyne.CanvasObject, scanTabFileActionsProvider, []scanTabFileActionsProvider, func(string)) {
	singleFileScan, singleFileActions := newAssetScanTab(window, assetScanTabOptions{
		NoSourceSelectedText:     "No .rbxl/.rbxm file selected.",
		SelectButtonText:         "Select .rbxl/.rbxm File",
		ReadyStatusText:          "Ready.",
		MissingSourceStatusText:  "Select an .rbxl or .rbxm file first.",
		ScanningStatusText:       "Scanning...",
		NoResultsStatusText:      "No results found.",
		MaxResultsDefault:        rustyAssetToolDefaultLimit,
		ScanContextKey:           scanContextRBXLSingle,
		RecentFilesPreferenceKey: "scan.recent.rbxl.single",
		SelectSource:             pickRBXLSource,
		ExtractHits:              scanRBXLFileForAssetIDs,
		PathFilteredExtractHits:  scanRBXLFileForAssetIDsFiltered,
		BuildWarning:             buildRBXLScanWarning,
	})
	fileDiffScan, fileDiffActions := newAssetScanTab(window, assetScanTabOptions{
		NoSourceSelectedText:             "Baseline: no .rbxl/.rbxm file selected.",
		SelectButtonText:                 "Select Baseline",
		NoSecondarySourceText:            "Target: no .rbxl/.rbxm file selected.",
		SelectSecondaryButtonText:        "Select Target",
		ReadyStatusText:                  "Ready.",
		MissingSourceStatusText:          "Select baseline and target .rbxl/.rbxm files first.",
		MissingSecondarySourceStatusText: "Select a target .rbxl or .rbxm file.",
		ScanningStatusText:               "Diffing...",
		NoResultsStatusText:              "No new results found.",
		MaxResultsDefault:                rustyAssetToolDefaultLimit,
		ScanContextKey:                   scanContextRBXLDiff,
		RecentFilesPreferenceKey:         "scan.recent.rbxl.diff",
		SelectSource:                     pickRBXLBaselineSource,
		SelectSecondarySource:            pickRBXLTargetSource,
		ExtractHits:                      scanRBXLFileDiffForAssetIDs,
		PathFilteredExtractHits:          scanRBXLFileForAssetIDsFiltered,
		BuildWarning:                     buildRBXLScanDiffWarning,
	})

	modeLabel := widget.NewLabel("Mode:")
	modeSwitch := widget.NewRadioGroup([]string{"Single File", "File Diff"}, nil)
	modeSwitch.Horizontal = true
	modeSwitch.SetSelected("Single File")
	contentStack := container.NewStack(singleFileScan, fileDiffScan)
	contentStack.Objects[1].Hide()
	currentActions := singleFileActions
	singleFileProvider := func() *scanTabFileActions { return singleFileActions }
	fileDiffProvider := func() *scanTabFileActions { return fileDiffActions }
	modeSwitch.OnChanged = func(selectedMode string) {
		if strings.EqualFold(selectedMode, "File Diff") {
			contentStack.Objects[0].Hide()
			contentStack.Objects[1].Show()
			currentActions = fileDiffActions
			contentStack.Refresh()
			return
		}
		contentStack.Objects[1].Hide()
		contentStack.Objects[0].Show()
		currentActions = singleFileActions
		contentStack.Refresh()
	}
	selectContext := func(contextKey string) {
		switch strings.TrimSpace(contextKey) {
		case scanContextRBXLDiff:
			modeSwitch.SetSelected("File Diff")
		default:
			modeSwitch.SetSelected("Single File")
		}
	}
	content := container.NewBorder(
		container.NewHBox(modeLabel, modeSwitch),
		nil,
		nil,
		nil,
		contentStack,
	)
	return content, func() *scanTabFileActions {
		return currentActions
	}, []scanTabFileActionsProvider{singleFileProvider, fileDiffProvider}, selectContext
}

func pickRBXLSource(window fyne.Window, onSelected func(string), onError func(error)) {
	selectedPath, pickerErr := nativeDialog.File().
		Filter(robloxDOMFileFilterLabel, "rbxl", "rbxm").
		Title("Select .rbxl or .rbxm file to scan").
		Load()
	if pickerErr == nil {
		onSelected(selectedPath)
		return
	}

	if errors.Is(pickerErr, nativeDialog.Cancelled) {
		return
	}

	// Fallback to Fyne picker if native dialog fails unexpectedly.
	fyneDialog.ShowFileOpen(func(fileURI fyne.URIReadCloser, err error) {
		if err != nil {
			onError(err)
			return
		}
		if fileURI == nil {
			return
		}
		defer fileURI.Close()

		onSelected(fileURI.URI().Path())
	}, window)
}

func pickRBXLBaselineSource(window fyne.Window, onSelected func(string), onError func(error)) {
	_ = window
	selectedPath, pickerErr := pickRBXLFilePath("Select baseline .rbxl or .rbxm file (old)")
	if pickerErr == nil {
		onSelected(selectedPath)
		return
	}
	if errors.Is(pickerErr, nativeDialog.Cancelled) {
		return
	}
	onError(pickerErr)
}

func pickRBXLTargetSource(window fyne.Window, onSelected func(string), onError func(error)) {
	_ = window
	selectedPath, pickerErr := pickRBXLFilePath("Select target .rbxl or .rbxm file (new)")
	if pickerErr == nil {
		onSelected(selectedPath)
		return
	}
	if errors.Is(pickerErr, nativeDialog.Cancelled) {
		return
	}
	onError(pickerErr)
}

func pickRBXLFilePath(title string) (string, error) {
	selectedPath, pickerErr := nativeDialog.File().
		Filter(robloxDOMFileFilterLabel, "rbxl", "rbxm").
		Title(title).
		Load()
	if pickerErr == nil {
		return selectedPath, nil
	}
	return "", pickerErr
}

func scanRBXLFileForAssetIDs(filePath string, limit int, stopChannel <-chan struct{}) ([]scanHit, error) {
	logDebugf("RBXL scan started: %s (limit=%d)", filePath, limit)
	if _, readErr := os.Stat(filePath); readErr != nil {
		logDebugf("RBXL scan read failed: %s", readErr.Error())
		return nil, readErr
	}

	_, _, extractedReferences, _, rustScanErr := extractAssetIDsWithRustyAssetToolFromFileWithCounts(filePath, 0, limit, stopChannel)
	if errors.Is(rustScanErr, errScanStopped) {
		logDebugf("RBXL scan stopped during Rust extraction")
		return nil, errScanStopped
	}
	if rustScanErr != nil {
		logDebugf("RBXL scan Rust extraction failed: %s", rustScanErr.Error())
		return nil, rustScanErr
	}
	sceneSurfaceAreasByPath, surfaceErr := loadRBXLSceneSurfaceAreas(filePath, nil, stopChannel)
	if errors.Is(surfaceErr, errScanStopped) {
		return nil, errScanStopped
	}

	hits := buildScanHitsFromRustReferences(extractedReferences, filePath, sceneSurfaceAreasByPath, limit)
	logDebugf("RBXL scan completed with %d unique asset IDs", len(hits))
	return hits, nil
}

func scanRBXLFileForAssetIDsFiltered(filePath string, pathPrefixes []string, limit int, stopChannel <-chan struct{}) ([]scanHit, error) {
	logDebugf("RBXL filtered scan started: %s (prefixes=%v, limit=%d)", filePath, pathPrefixes, limit)
	if _, readErr := os.Stat(filePath); readErr != nil {
		return nil, readErr
	}
	refs, err := extractFilteredRefsWithRustyAssetTool(filePath, pathPrefixes, stopChannel)
	if errors.Is(err, errScanStopped) {
		return nil, errScanStopped
	}
	if err != nil {
		return nil, err
	}
	sceneSurfaceAreasByPath, surfaceErr := loadRBXLSceneSurfaceAreas(filePath, pathPrefixes, stopChannel)
	if errors.Is(surfaceErr, errScanStopped) {
		return nil, errScanStopped
	}

	hits := buildScanHitsFromRustReferences(refs, filePath, sceneSurfaceAreasByPath, limit)
	logDebugf("RBXL filtered scan completed with %d unique asset IDs from %d references", len(hits), len(refs))
	return hits, nil
}

func scanRBXLFileDiffForAssetIDs(sourcePath string, limit int, stopChannel <-chan struct{}) ([]scanHit, error) {
	sourceParts := strings.SplitN(sourcePath, "\n", 2)
	if len(sourceParts) != 2 {
		return nil, fmt.Errorf("invalid file diff source format")
	}
	baselineFilePath := strings.TrimSpace(sourceParts[0])
	targetFilePath := strings.TrimSpace(sourceParts[1])
	if baselineFilePath == "" || targetFilePath == "" {
		return nil, fmt.Errorf("both baseline and target files are required")
	}
	if _, readErr := os.Stat(baselineFilePath); readErr != nil {
		return nil, readErr
	}
	if _, readErr := os.Stat(targetFilePath); readErr != nil {
		return nil, readErr
	}
	logDebugf(
		"RBXL file diff started: baseline=%s target=%s (limit=%d)",
		baselineFilePath,
		targetFilePath,
		limit,
	)

	baselineReferenceKeys := map[string]bool{}
	_, _, baselineReferences, _, baselineErr := extractAssetIDsWithRustyAssetToolFromFileWithCounts(baselineFilePath, 0, 0, stopChannel)
	if errors.Is(baselineErr, errScanStopped) {
		return nil, errScanStopped
	}
	if baselineErr != nil {
		return nil, baselineErr
	}
	for _, reference := range baselineReferences {
		if reference.ID <= 0 {
			continue
		}
		baselineReferenceKeys[scanAssetReferenceKey(reference.ID, reference.RawContent)] = true
	}

	results := []scanHit{}
	_, _, targetReferences, _, targetErr := extractAssetIDsWithRustyAssetToolFromFileWithCounts(targetFilePath, 0, 0, stopChannel)
	if errors.Is(targetErr, errScanStopped) {
		return results, errScanStopped
	}
	if targetErr != nil {
		return nil, targetErr
	}
	sceneSurfaceAreasByPath, surfaceErr := loadRBXLSceneSurfaceAreas(targetFilePath, nil, stopChannel)
	if errors.Is(surfaceErr, errScanStopped) {
		return nil, errScanStopped
	}
	filteredReferences := make([]rustyAssetToolResult, 0, len(targetReferences))
	for _, reference := range targetReferences {
		if reference.ID <= 0 {
			continue
		}
		if baselineReferenceKeys[scanAssetReferenceKey(reference.ID, reference.RawContent)] {
			continue
		}
		filteredReferences = append(filteredReferences, reference)
	}
	results = buildScanHitsFromRustReferences(filteredReferences, targetFilePath, sceneSurfaceAreasByPath, limit)

	logDebugf("RBXL file diff completed with %d new unique asset IDs", len(results))
	return results, nil
}

func buildRBXLScanWarning(sourcePath string, pathPrefixes []string, stopChannel <-chan struct{}) (materialVariantWarningData, error) {
	return buildRBXLMissingMaterialVariantWarning(sourcePath, pathPrefixes, stopChannel)
}

func buildRBXLScanDiffWarning(sourcePath string, pathPrefixes []string, stopChannel <-chan struct{}) (materialVariantWarningData, error) {
	sourceParts := strings.SplitN(sourcePath, "\n", 2)
	if len(sourceParts) != 2 {
		return materialVariantWarningData{}, fmt.Errorf("invalid file diff source format")
	}
	baselineFilePath := strings.TrimSpace(sourceParts[0])
	targetFilePath := strings.TrimSpace(sourceParts[1])
	if baselineFilePath == "" || targetFilePath == "" {
		return materialVariantWarningData{}, fmt.Errorf("both baseline and target files are required")
	}

	baselineWarning, baselineErr := buildRBXLMissingMaterialVariantWarning(baselineFilePath, pathPrefixes, stopChannel)
	if baselineErr != nil {
		return materialVariantWarningData{}, baselineErr
	}
	targetWarning, targetErr := buildRBXLMissingMaterialVariantWarning(targetFilePath, pathPrefixes, stopChannel)
	if targetErr != nil {
		return materialVariantWarningData{}, targetErr
	}

	return combineMaterialVariantWarnings(baselineWarning, targetWarning), nil
}

func buildScanHitsFromRustReferences(references []rustyAssetToolResult, filePath string, sceneSurfaceAreasByPath map[string]float64, limit int) []scanHit {
	type hitBuilder struct {
		hit     scanHit
		pathSet map[string]bool
	}

	builders := map[string]*hitBuilder{}
	order := make([]string, 0, len(references))
	for _, reference := range references {
		if reference.ID <= 0 {
			continue
		}

		assetInput := strings.TrimSpace(reference.RawContent)
		referenceKey := scanAssetReferenceKey(reference.ID, assetInput)
		builder, exists := builders[referenceKey]
		if !exists {
			builder = &hitBuilder{
				hit: scanHit{
					AssetID:          reference.ID,
					AssetInput:       assetInput,
					FilePath:         filePath,
					InstanceType:     strings.TrimSpace(reference.InstanceType),
					InstanceName:     strings.TrimSpace(reference.InstanceName),
					InstancePath:     strings.TrimSpace(reference.InstancePath),
					PropertyName:     strings.TrimSpace(reference.PropertyName),
					SceneSurfaceArea: 0,
				},
				pathSet: map[string]bool{},
			}
			builder.hit.SceneSurfaceArea, builder.hit.LargestSurfacePath = estimateSceneSurfaceAreaAndPathForPaths(
				reference.InstancePath,
				nil,
				sceneSurfaceAreasByPath,
			)
			builders[referenceKey] = builder
			order = append(order, referenceKey)
		}

		builder.hit.UseCount += rustReferenceUseCount(reference)
		addRustReferencePaths(&builder.hit, builder.pathSet, reference, sceneSurfaceAreasByPath)
	}

	hits := make([]scanHit, 0, len(order))
	for _, referenceKey := range order {
		hits = append(hits, builders[referenceKey].hit)
		if limit > 0 && len(hits) >= limit {
			break
		}
	}
	return hits
}

func rustReferenceUseCount(reference rustyAssetToolResult) int {
	if reference.Used > 0 {
		return reference.Used
	}
	return 1
}

func addRustReferencePaths(hit *scanHit, pathSet map[string]bool, reference rustyAssetToolResult, sceneSurfaceAreasByPath map[string]float64) {
	if hit == nil {
		return
	}

	allPaths := append([]string(nil), reference.AllInstancePaths...)
	if len(allPaths) == 0 {
		allPaths = append(allPaths, reference.InstancePath)
	}
	for _, instancePath := range allPaths {
		trimmedPath := strings.TrimSpace(instancePath)
		if trimmedPath == "" || pathSet[trimmedPath] {
			continue
		}
		pathSet[trimmedPath] = true
		hit.AllInstancePaths = append(hit.AllInstancePaths, trimmedPath)
		nextArea, nextPath := estimateSceneSurfaceAreaAndPathForPaths(trimmedPath, nil, sceneSurfaceAreasByPath)
		if nextArea > hit.SceneSurfaceArea {
			hit.SceneSurfaceArea = nextArea
			hit.LargestSurfacePath = strings.TrimSpace(nextPath)
		} else if hit.SceneSurfaceArea <= 0 && strings.TrimSpace(hit.LargestSurfacePath) == "" {
			hit.LargestSurfacePath = strings.TrimSpace(nextPath)
		}
	}
}

func loadRBXLSceneSurfaceAreas(filePath string, pathPrefixes []string, stopChannel <-chan struct{}) (map[string]float64, error) {
	mapParts, err := extractMapRenderPartsWithRustyAssetTool(filePath, pathPrefixes, stopChannel)
	if errors.Is(err, errScanStopped) {
		return nil, errScanStopped
	}
	if err != nil {
		logDebugf("RBXL map render extraction failed for large textures: %s", err.Error())
		return map[string]float64{}, nil
	}
	return buildSceneSurfaceAreaIndexFromMapRenderParts(mapParts), nil
}
