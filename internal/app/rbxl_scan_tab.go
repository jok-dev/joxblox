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

func newRBXLScanTab(
	window fyne.Window,
) (fyne.CanvasObject, scanTabFileActionsProvider, []scanTabFileActionsProvider, func(string)) {
	singleFileScan, singleFileActions := newAssetScanTab(window, assetScanTabOptions{
		NoSourceSelectedText:     "No .rbxl file selected.",
		SelectButtonText:         "Select .rbxl File",
		ReadyStatusText:          "Ready.",
		MissingSourceStatusText:  "Select an .rbxl file first.",
		ScanningStatusText:       "Scanning...",
		NoResultsStatusText:      "No results found.",
		MaxResultsDefault:        rustExtractorDefaultLimit,
		ScanContextKey:           scanContextRBXLSingle,
		RecentFilesPreferenceKey: "scan.recent.rbxl.single",
		SelectSource:             pickRBXLSource,
		ExtractHits:              scanRBXLFileForAssetIDs,
	})
	fileDiffScan, fileDiffActions := newAssetScanTab(window, assetScanTabOptions{
		NoSourceSelectedText:             "Baseline: no .rbxl file selected.",
		SelectButtonText:                 "Select Baseline",
		NoSecondarySourceText:            "Target: no .rbxl file selected.",
		SelectSecondaryButtonText:        "Select Target",
		ReadyStatusText:                  "Ready.",
		MissingSourceStatusText:          "Select baseline and target .rbxl files first.",
		MissingSecondarySourceStatusText: "Select a target .rbxl file.",
		ScanningStatusText:               "Diffing...",
		NoResultsStatusText:              "No new results found.",
		MaxResultsDefault:                rustExtractorDefaultLimit,
		ScanContextKey:                   scanContextRBXLDiff,
		RecentFilesPreferenceKey:         "scan.recent.rbxl.diff",
		SelectSource:                     pickRBXLBaselineSource,
		SelectSecondarySource:            pickRBXLTargetSource,
		ExtractHits:                      scanRBXLFileDiffForAssetIDs,
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
		Filter("Roblox place files", "rbxl").
		Title("Select .rbxl file to scan").
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
	selectedPath, pickerErr := pickRBXLFilePath("Select baseline .rbxl file (old)")
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
	selectedPath, pickerErr := pickRBXLFilePath("Select target .rbxl file (new)")
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
		Filter("Roblox place files", "rbxl").
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

	rustAssetIDs, useCountsByAssetID, extractedReferences, _, rustScanErr := extractAssetIDsWithRustFromFileWithCounts(filePath, 0, limit, stopChannel)
	if errors.Is(rustScanErr, errScanStopped) {
		logDebugf("RBXL scan stopped during Rust extraction")
		return nil, errScanStopped
	}
	if rustScanErr != nil {
		logDebugf("RBXL scan Rust extraction failed: %s", rustScanErr.Error())
		return nil, rustScanErr
	}

	hits := make([]scanHit, 0, len(rustAssetIDs))
	referenceByAssetID := map[int64]rustExtractorResult{}
	for _, extractedReference := range extractedReferences {
		if extractedReference.ID <= 0 {
			continue
		}
		if _, exists := referenceByAssetID[extractedReference.ID]; exists {
			continue
		}
		referenceByAssetID[extractedReference.ID] = extractedReference
	}
	for _, assetID := range rustAssetIDs {
		useCount := useCountsByAssetID[assetID]
		if useCount <= 0 {
			useCount = 1
		}
		referenceContext := referenceByAssetID[assetID]
		hits = append(hits, scanHit{
			AssetID:      assetID,
			FilePath:     filePath,
			UseCount:     useCount,
			InstanceType: strings.TrimSpace(referenceContext.InstanceType),
			InstanceName: strings.TrimSpace(referenceContext.InstanceName),
			InstancePath: strings.TrimSpace(referenceContext.InstancePath),
			PropertyName: strings.TrimSpace(referenceContext.PropertyName),
		})
	}
	logDebugf("RBXL scan completed with %d unique asset IDs", len(hits))
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

	baselineAssetIDs := map[int64]bool{}
	baselineFileAssetIDs, _, _, _, baselineErr := extractAssetIDsWithRustFromFileWithCounts(baselineFilePath, 0, 0, stopChannel)
	if errors.Is(baselineErr, errScanStopped) {
		return nil, errScanStopped
	}
	if baselineErr != nil {
		return nil, baselineErr
	}
	for _, assetID := range baselineFileAssetIDs {
		baselineAssetIDs[assetID] = true
	}

	results := []scanHit{}
	targetFileAssetIDs, targetUseCountsByAssetID, targetReferences, _, targetErr := extractAssetIDsWithRustFromFileWithCounts(targetFilePath, 0, 0, stopChannel)
	if errors.Is(targetErr, errScanStopped) {
		return results, errScanStopped
	}
	if targetErr != nil {
		return nil, targetErr
	}
	targetReferenceByAssetID := map[int64]rustExtractorResult{}
	for _, extractedReference := range targetReferences {
		if extractedReference.ID <= 0 {
			continue
		}
		if _, exists := targetReferenceByAssetID[extractedReference.ID]; exists {
			continue
		}
		targetReferenceByAssetID[extractedReference.ID] = extractedReference
	}
	seenTargetAssetIDs := map[int64]bool{}
	for _, assetID := range targetFileAssetIDs {
		if baselineAssetIDs[assetID] || seenTargetAssetIDs[assetID] {
			continue
		}
		seenTargetAssetIDs[assetID] = true
		useCount := targetUseCountsByAssetID[assetID]
		if useCount <= 0 {
			useCount = 1
		}
		referenceContext := targetReferenceByAssetID[assetID]
		results = append(results, scanHit{
			AssetID:      assetID,
			FilePath:     targetFilePath,
			UseCount:     useCount,
			InstanceType: strings.TrimSpace(referenceContext.InstanceType),
			InstanceName: strings.TrimSpace(referenceContext.InstanceName),
			InstancePath: strings.TrimSpace(referenceContext.InstancePath),
			PropertyName: strings.TrimSpace(referenceContext.PropertyName),
		})
		if limit > 0 && len(results) >= limit {
			break
		}
	}

	logDebugf("RBXL file diff completed with %d new unique asset IDs", len(results))
	return results, nil
}
