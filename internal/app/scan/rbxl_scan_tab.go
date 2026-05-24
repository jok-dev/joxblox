package scan

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"joxblox/internal/app/loader"
	"joxblox/internal/app/ui"
	"joxblox/internal/debug"
	"joxblox/internal/extractor"

	"fyne.io/fyne/v2"
	fyneDialog "fyne.io/fyne/v2/dialog"
	nativeDialog "github.com/sqweek/dialog"
)

const robloxDOMFileFilterLabel = "Roblox place/model files"

func newRBXLScanTab(
	window fyne.Window,
) (fyne.CanvasObject, ScanTabFileActionsProvider, []ScanTabFileActionsProvider, func(string)) {
	return buildScanModeTab(scanModeTabConfig{
		Window:        window,
		SingleLabel:   "Single File",
		DiffLabel:     "File Diff",
		SingleContext: ScanContextRBXLSingle,
		DiffContext:   ScanContextRBXLDiff,
		SingleOptions: assetScanTabOptions{
			NoSourceSelectedText:     "No .rbxl/.rbxm file selected.",
			SelectButtonText:         "Select .rbxl/.rbxm File",
			ReadyStatusText:          "Ready.",
			MissingSourceStatusText:  "Select an .rbxl or .rbxm file first.",
			ScanningStatusText:       "Scanning...",
			NoResultsStatusText:      "No results found.",
			MaxResultsDefault:        extractor.DefaultLimit,
			ScanContextKey:           ScanContextRBXLSingle,
			RecentFilesPreferenceKey: "scan.recent.rbxl.single",
			SelectSource:             pickRBXLSource,
			ExtractHits:              scanRBXLFileForAssetIDs,
			PathFilteredExtractHits:  scanRBXLFileForAssetIDsFiltered,
			BuildWarning:             buildRBXLScanWarning,
		},
		DiffOptions: assetScanTabOptions{
			NoSourceSelectedText:             "Baseline: no .rbxl/.rbxm file selected.",
			SelectButtonText:                 "Select Baseline",
			NoSecondarySourceText:            "Target: no .rbxl/.rbxm file selected.",
			SelectSecondaryButtonText:        "Select Target",
			ReadyStatusText:                  "Ready.",
			MissingSourceStatusText:          "Select baseline and target .rbxl/.rbxm files first.",
			MissingSecondarySourceStatusText: "Select a target .rbxl or .rbxm file.",
			ScanningStatusText:               "Diffing...",
			NoResultsStatusText:              "No new results found.",
			MaxResultsDefault:                extractor.DefaultLimit,
			ScanContextKey:                   ScanContextRBXLDiff,
			RecentFilesPreferenceKey:         "scan.recent.rbxl.diff",
			SelectSource:                     pickRBXLBaselineSource,
			SelectSecondarySource:            pickRBXLTargetSource,
			ExtractHits:                      scanRBXLFileDiffForAssetIDs,
			PathFilteredExtractHits:          scanRBXLFileDiffForAssetIDsFiltered,
			BuildWarning:                     buildRBXLScanDiffWarning,
		},
	})
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

func scanRBXLFileForAssetIDs(filePath string, limit int, stopChannel <-chan struct{}) ([]loader.ScanHit, error) {
	return loader.ScanRBXLFileForAssetIDs(filePath, limit, stopChannel)
}

func scanRBXLFileForAssetIDsFiltered(filePath string, pathPrefixes []string, limit int, stopChannel <-chan struct{}) ([]loader.ScanHit, error) {
	return loader.ScanRBXLFileForAssetIDsFiltered(filePath, pathPrefixes, limit, stopChannel)
}

func scanRBXLFileDiffForAssetIDs(sourcePath string, limit int, stopChannel <-chan struct{}) ([]loader.ScanHit, error) {
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
	debug.Logf(
		"RBXL file diff started: baseline=%s target=%s (limit=%d)",
		baselineFilePath,
		targetFilePath,
		limit,
	)

	baselineReferenceKeys := map[string]bool{}
	baselineExtractResult, baselineErr := extractor.ExtractAssetIDsWithCounts(baselineFilePath, 0, 0, stopChannel)
	if errors.Is(baselineErr, extractor.ErrCancelled) {
		baselineErr = loader.ErrScanStopped
	}
	if errors.Is(baselineErr, loader.ErrScanStopped) {
		return nil, loader.ErrScanStopped
	}
	if baselineErr != nil {
		return nil, baselineErr
	}
	for _, reference := range baselineExtractResult.References {
		if reference.ID <= 0 {
			continue
		}
		baselineReferenceKeys[extractor.AssetReferenceKey(reference.ID, reference.RawContent)] = true
	}

	results := []loader.ScanHit{}
	targetExtractResult, targetErr := extractor.ExtractAssetIDsWithCounts(targetFilePath, 0, 0, stopChannel)
	if errors.Is(targetErr, extractor.ErrCancelled) {
		targetErr = loader.ErrScanStopped
	}
	if errors.Is(targetErr, loader.ErrScanStopped) {
		return results, loader.ErrScanStopped
	}
	if targetErr != nil {
		return nil, targetErr
	}
	sceneSurfaceAreasByPath, surfaceErr := loader.LoadRBXLSceneSurfaceAreas(targetFilePath, nil, stopChannel)
	if errors.Is(surfaceErr, loader.ErrScanStopped) {
		return nil, loader.ErrScanStopped
	}
	filteredReferences := make([]extractor.Result, 0, len(targetExtractResult.References))
	for _, reference := range targetExtractResult.References {
		if reference.ID <= 0 {
			continue
		}
		if baselineReferenceKeys[extractor.AssetReferenceKey(reference.ID, reference.RawContent)] {
			continue
		}
		filteredReferences = append(filteredReferences, reference)
	}
	results = loader.BuildScanHitsFromRustReferences(filteredReferences, targetFilePath, sceneSurfaceAreasByPath, limit)

	debug.Logf("RBXL file diff completed with %d new unique asset IDs", len(results))
	return results, nil
}

func scanRBXLFileDiffForAssetIDsFiltered(sourcePath string, pathPrefixes []string, limit int, stopChannel <-chan struct{}) ([]loader.ScanHit, error) {
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
	debug.Logf("RBXL filtered file diff started: baseline=%s target=%s (prefixes=%v, limit=%d)", baselineFilePath, targetFilePath, pathPrefixes, limit)

	baselineRefs, baselineErr := extractor.ExtractFilteredRefs(baselineFilePath, pathPrefixes, stopChannel)
	if errors.Is(baselineErr, extractor.ErrCancelled) {
		baselineErr = loader.ErrScanStopped
	}
	if errors.Is(baselineErr, loader.ErrScanStopped) {
		return nil, loader.ErrScanStopped
	}
	if baselineErr != nil {
		return nil, baselineErr
	}
	baselineReferenceKeys := map[string]bool{}
	for _, reference := range baselineRefs {
		if reference.ID <= 0 {
			continue
		}
		baselineReferenceKeys[extractor.AssetReferenceKey(reference.ID, reference.RawContent)] = true
	}

	targetRefs, targetErr := extractor.ExtractFilteredRefs(targetFilePath, pathPrefixes, stopChannel)
	if errors.Is(targetErr, extractor.ErrCancelled) {
		targetErr = loader.ErrScanStopped
	}
	if errors.Is(targetErr, loader.ErrScanStopped) {
		return nil, loader.ErrScanStopped
	}
	if targetErr != nil {
		return nil, targetErr
	}
	sceneSurfaceAreasByPath, surfaceErr := loader.LoadRBXLSceneSurfaceAreas(targetFilePath, pathPrefixes, stopChannel)
	if errors.Is(surfaceErr, loader.ErrScanStopped) {
		return nil, loader.ErrScanStopped
	}
	filteredReferences := make([]extractor.Result, 0, len(targetRefs))
	for _, reference := range targetRefs {
		if reference.ID <= 0 {
			continue
		}
		if baselineReferenceKeys[extractor.AssetReferenceKey(reference.ID, reference.RawContent)] {
			continue
		}
		filteredReferences = append(filteredReferences, reference)
	}
	hits := loader.BuildScanHitsFromRustReferences(filteredReferences, targetFilePath, sceneSurfaceAreasByPath, limit)
	debug.Logf("RBXL filtered file diff completed with %d new unique asset IDs", len(hits))
	return hits, nil
}

func buildRBXLScanWarning(sourcePath string, pathPrefixes []string, stopChannel <-chan struct{}) (ui.MaterialVariantWarningData, error) {
	return ui.BuildRBXLMissingMaterialVariantWarning(sourcePath, pathPrefixes, stopChannel)
}

func buildRBXLScanDiffWarning(sourcePath string, pathPrefixes []string, stopChannel <-chan struct{}) (ui.MaterialVariantWarningData, error) {
	sourceParts := strings.SplitN(sourcePath, "\n", 2)
	if len(sourceParts) != 2 {
		return ui.MaterialVariantWarningData{}, fmt.Errorf("invalid file diff source format")
	}
	baselineFilePath := strings.TrimSpace(sourceParts[0])
	targetFilePath := strings.TrimSpace(sourceParts[1])
	if baselineFilePath == "" || targetFilePath == "" {
		return ui.MaterialVariantWarningData{}, fmt.Errorf("both baseline and target files are required")
	}

	baselineWarning, baselineErr := ui.BuildRBXLMissingMaterialVariantWarning(baselineFilePath, pathPrefixes, stopChannel)
	if baselineErr != nil {
		return ui.MaterialVariantWarningData{}, baselineErr
	}
	targetWarning, targetErr := ui.BuildRBXLMissingMaterialVariantWarning(targetFilePath, pathPrefixes, stopChannel)
	if targetErr != nil {
		return ui.MaterialVariantWarningData{}, targetErr
	}

	return ui.CombineMaterialVariantWarnings(baselineWarning, targetWarning), nil
}
