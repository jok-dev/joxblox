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

	type hitBuilder struct {
		hit     scanHit
		pathSet map[string]bool
	}
	builders := map[string]*hitBuilder{}
	order := []string{}
	for _, extractedReference := range extractedReferences {
		if extractedReference.ID <= 0 {
			continue
		}
		assetInput := strings.TrimSpace(extractedReference.RawContent)
		referenceKey := scanAssetReferenceKey(extractedReference.ID, assetInput)
		builder, exists := builders[referenceKey]
		if !exists {
			builder = &hitBuilder{
				hit: scanHit{
					AssetID:      extractedReference.ID,
					AssetInput:   assetInput,
					FilePath:     filePath,
					InstanceType: strings.TrimSpace(extractedReference.InstanceType),
					InstanceName: strings.TrimSpace(extractedReference.InstanceName),
					InstancePath: strings.TrimSpace(extractedReference.InstancePath),
					PropertyName: strings.TrimSpace(extractedReference.PropertyName),
				},
				pathSet: map[string]bool{},
			}
			builders[referenceKey] = builder
			order = append(order, referenceKey)
		}
		builder.hit.UseCount++
		for _, instancePath := range extractedReference.AllInstancePaths {
			trimmedPath := strings.TrimSpace(instancePath)
			if trimmedPath == "" || builder.pathSet[trimmedPath] {
				continue
			}
			builder.pathSet[trimmedPath] = true
			builder.hit.AllInstancePaths = append(builder.hit.AllInstancePaths, trimmedPath)
		}
	}
	hits := make([]scanHit, 0, len(order))
	for _, referenceKey := range order {
		hits = append(hits, builders[referenceKey].hit)
		if limit > 0 && len(hits) >= limit {
			break
		}
	}
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

	type hitBuilder struct {
		hit     scanHit
		pathSet map[string]bool
	}
	builders := map[string]*hitBuilder{}
	var order []string
	for _, ref := range refs {
		if ref.ID <= 0 {
			continue
		}
		assetInput := strings.TrimSpace(ref.RawContent)
		referenceKey := scanAssetReferenceKey(ref.ID, assetInput)
		b, exists := builders[referenceKey]
		if !exists {
			b = &hitBuilder{
				hit: scanHit{
					AssetID:      ref.ID,
					AssetInput:   assetInput,
					FilePath:     filePath,
					InstanceType: strings.TrimSpace(ref.InstanceType),
					InstanceName: strings.TrimSpace(ref.InstanceName),
					InstancePath: strings.TrimSpace(ref.InstancePath),
					PropertyName: strings.TrimSpace(ref.PropertyName),
				},
				pathSet: map[string]bool{},
			}
			builders[referenceKey] = b
			order = append(order, referenceKey)
		}
		b.hit.UseCount++
		trimmedPath := strings.TrimSpace(ref.InstancePath)
		if trimmedPath != "" && !b.pathSet[trimmedPath] {
			b.pathSet[trimmedPath] = true
			b.hit.AllInstancePaths = append(b.hit.AllInstancePaths, trimmedPath)
		}
	}

	hits := make([]scanHit, 0, len(order))
	for _, referenceKey := range order {
		hits = append(hits, builders[referenceKey].hit)
		if limit > 0 && len(hits) >= limit {
			break
		}
	}
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
	type hitBuilder struct {
		hit     scanHit
		pathSet map[string]bool
	}
	builders := map[string]*hitBuilder{}
	order := []string{}
	for _, reference := range targetReferences {
		if reference.ID <= 0 {
			continue
		}
		assetInput := strings.TrimSpace(reference.RawContent)
		referenceKey := scanAssetReferenceKey(reference.ID, assetInput)
		if baselineReferenceKeys[referenceKey] {
			continue
		}
		builder, exists := builders[referenceKey]
		if !exists {
			builder = &hitBuilder{
				hit: scanHit{
					AssetID:      reference.ID,
					AssetInput:   assetInput,
					FilePath:     targetFilePath,
					InstanceType: strings.TrimSpace(reference.InstanceType),
					InstanceName: strings.TrimSpace(reference.InstanceName),
					InstancePath: strings.TrimSpace(reference.InstancePath),
					PropertyName: strings.TrimSpace(reference.PropertyName),
				},
				pathSet: map[string]bool{},
			}
			builders[referenceKey] = builder
			order = append(order, referenceKey)
		}
		builder.hit.UseCount++
		for _, instancePath := range reference.AllInstancePaths {
			trimmedPath := strings.TrimSpace(instancePath)
			if trimmedPath == "" || builder.pathSet[trimmedPath] {
				continue
			}
			builder.pathSet[trimmedPath] = true
			builder.hit.AllInstancePaths = append(builder.hit.AllInstancePaths, trimmedPath)
		}
	}
	for _, referenceKey := range order {
		results = append(results, builders[referenceKey].hit)
		if limit > 0 && len(results) >= limit {
			break
		}
	}

	logDebugf("RBXL file diff completed with %d new unique asset IDs", len(results))
	return results, nil
}
