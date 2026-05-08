package scan

import (
	"strings"

	"joxblox/internal/app/loader"
	"joxblox/internal/app/report"
	"joxblox/internal/extractor"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

const scanAssetTypeNoneLabel = "None"

const (
	scanSourceFolders = "Folders"
	scanSourceRBXL    = "RBXL/RBXM"
	scanModeSingle    = "Single"
	scanModeDiff      = "Diff"
)

type scanTabVariant struct {
	source  string
	mode    string
	content fyne.CanvasObject
	actions *ScanTabFileActions
}

type ScanLoadOptions struct {
	PathFilterText        string
	LargeTextureThreshold float64
	// AssetTypeLabel, when non-empty and matching a configured asset
	// type, drives the top-bar `Asset type` selector — picking it up
	// applies the type's banned-texture limit to result rows.
	AssetTypeLabel string
	// PrebuiltResults short-circuits the rescan: when non-empty the
	// rows are loaded directly into the explorer (same path as a JSON
	// import), so the caller's already-fetched preview data is reused
	// instead of refetching every asset.
	PrebuiltResults []loader.ScanResult
}

func NewScanTab(
	window fyne.Window,
) (fyne.CanvasObject, ScanTabFileActionsProvider, []ScanTabFileActionsProvider, func(string), func(string, ScanLoadOptions)) {
	folderSingleScan, folderSingleActions := newAssetScanTab(window, assetScanTabOptions{
		NoSourceSelectedText:     "No folder selected.",
		SelectButtonText:         "Select Folder",
		ReadyStatusText:          "Ready.",
		MissingSourceStatusText:  "Select a folder first.",
		ScanningStatusText:       "Scanning...",
		NoResultsStatusText:      "No results found.",
		MaxResultsDefault:        defaultAssetScanLimit,
		ScanContextKey:           ScanContextFolder,
		RecentFilesPreferenceKey: "scan.recent.folder",
		SelectSource:             pickFolderSource,
		ExtractHits:              loader.ScanFolderForAssetIDs,
	})
	folderDiffScan, folderDiffActions := newAssetScanTab(window, assetScanTabOptions{
		NoSourceSelectedText:             "Baseline: no folder selected.",
		SelectButtonText:                 "Select Baseline",
		NoSecondarySourceText:            "Target: no folder selected.",
		SelectSecondaryButtonText:        "Select Target",
		ReadyStatusText:                  "Ready.",
		MissingSourceStatusText:          "Select baseline and target folders first.",
		MissingSecondarySourceStatusText: "Select a target folder.",
		ScanningStatusText:               "Diffing...",
		NoResultsStatusText:              "No new results found.",
		MaxResultsDefault:                defaultAssetScanLimit,
		ScanContextKey:                   ScanContextFolderDiff,
		RecentFilesPreferenceKey:         "scan.recent.folder.diff",
		SelectSource:                     pickFolderBaselineSource,
		SelectSecondarySource:            pickFolderTargetSource,
		ExtractHits:                      loader.ScanFolderDiffForAssetIDs,
	})
	rbxlSingleScan, rbxlSingleActions := newAssetScanTab(window, assetScanTabOptions{
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
	})
	rbxlDiffScan, rbxlDiffActions := newAssetScanTab(window, assetScanTabOptions{
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
		PathFilteredExtractHits:          scanRBXLFileForAssetIDsFiltered,
	})

	variants := []scanTabVariant{
		{source: scanSourceFolders, mode: scanModeSingle, content: folderSingleScan, actions: folderSingleActions},
		{source: scanSourceFolders, mode: scanModeDiff, content: folderDiffScan, actions: folderDiffActions},
		{source: scanSourceRBXL, mode: scanModeSingle, content: rbxlSingleScan, actions: rbxlSingleActions},
		{source: scanSourceRBXL, mode: scanModeDiff, content: rbxlDiffScan, actions: rbxlDiffActions},
	}
	contentStack := container.NewStack(
		folderSingleScan,
		folderDiffScan,
		rbxlSingleScan,
		rbxlDiffScan,
	)
	sourceSwitch := widget.NewRadioGroup([]string{scanSourceRBXL, scanSourceFolders}, nil)
	sourceSwitch.Horizontal = true
	modeSwitch := widget.NewRadioGroup([]string{scanModeSingle, scanModeDiff}, nil)
	modeSwitch.Horizontal = true

	assetTypeOptions := []string{scanAssetTypeNoneLabel}
	limitByLabel := map[string]float64{scanAssetTypeNoneLabel: 0}
	for _, cfg := range report.AssetTypeConfigs {
		if strings.TrimSpace(cfg.Label) == "" {
			continue
		}
		assetTypeOptions = append(assetTypeOptions, cfg.Label)
		limitByLabel[cfg.Label] = cfg.BannedTextureSizeMB
	}
	assetTypeSelect := widget.NewSelect(assetTypeOptions, nil)
	assetTypeSelect.SetSelected(scanAssetTypeNoneLabel)
	applyAssetTypeToVariants := func(label string) {
		limit := limitByLabel[label]
		for _, variant := range variants {
			if variant.actions != nil && variant.actions.SetBannedTextureSizeMB != nil {
				variant.actions.SetBannedTextureSizeMB(limit)
			}
		}
	}
	assetTypeSelect.OnChanged = func(label string) {
		applyAssetTypeToVariants(label)
	}
	currentActions := rbxlSingleActions
	providers := make([]ScanTabFileActionsProvider, 0, len(variants))
	for _, variant := range variants {
		variantActions := variant.actions
		providers = append(providers, func() *ScanTabFileActions {
			return variantActions
		})
	}
	var selectContext func(string)

	getSelectedVariant := func(selectedSource string, selectedMode string) scanTabVariant {
		for _, variant := range variants {
			if strings.EqualFold(variant.source, selectedSource) && strings.EqualFold(variant.mode, selectedMode) {
				return variant
			}
		}
		return variants[0]
	}

	updateVisibleContent := func() {
		selectedSource := sourceSwitch.Selected
		selectedMode := modeSwitch.Selected
		if selectedSource == "" {
			selectedSource = scanSourceRBXL
		}
		if selectedMode == "" {
			selectedMode = scanModeSingle
		}

		selectedVariant := getSelectedVariant(selectedSource, selectedMode)
		for _, variant := range variants {
			variant.content.Hide()
		}
		selectedVariant.content.Show()
		currentActions = selectedVariant.actions
		contentStack.Refresh()
	}

	sourceSwitch.OnChanged = func(string) {
		updateVisibleContent()
	}
	modeSwitch.OnChanged = func(string) {
		updateVisibleContent()
	}

	sourceSwitch.SetSelected(scanSourceRBXL)
	modeSwitch.SetSelected(scanModeSingle)
	updateVisibleContent()

	selectContext = func(contextKey string) {
		switch strings.TrimSpace(contextKey) {
		case ScanContextFolder:
			sourceSwitch.SetSelected(scanSourceFolders)
			modeSwitch.SetSelected(scanModeSingle)
		case ScanContextFolderDiff:
			sourceSwitch.SetSelected(scanSourceFolders)
			modeSwitch.SetSelected(scanModeDiff)
		case ScanContextRBXLSingle:
			sourceSwitch.SetSelected(scanSourceRBXL)
			modeSwitch.SetSelected(scanModeSingle)
		case ScanContextRBXLDiff:
			sourceSwitch.SetSelected(scanSourceRBXL)
			modeSwitch.SetSelected(scanModeDiff)
		default:
			sourceSwitch.SetSelected(scanSourceRBXL)
			modeSwitch.SetSelected(scanModeSingle)
		}
	}

	content := container.NewBorder(
		container.NewHBox(
			widget.NewLabel("Source:"),
			sourceSwitch,
			widget.NewLabel("Mode:"),
			modeSwitch,
			widget.NewLabel("Asset type:"),
			assetTypeSelect,
		),
		nil,
		nil,
		nil,
		contentStack,
	)

	loadRBXLFile := func(path string, options ScanLoadOptions) {
		sourceSwitch.SetSelected(scanSourceRBXL)
		modeSwitch.SetSelected(scanModeSingle)
		updateVisibleContent()
		if rbxlSingleActions != nil && rbxlSingleActions.SetPathFilter != nil {
			filterEnabled := strings.TrimSpace(options.PathFilterText) != ""
			rbxlSingleActions.SetPathFilter(filterEnabled, options.PathFilterText)
		}
		if rbxlSingleActions != nil && rbxlSingleActions.SetLargeTextureThreshold != nil && options.LargeTextureThreshold > 0 {
			rbxlSingleActions.SetLargeTextureThreshold(options.LargeTextureThreshold)
		}
		if trimmedLabel := strings.TrimSpace(options.AssetTypeLabel); trimmedLabel != "" {
			if _, valid := limitByLabel[trimmedLabel]; valid {
				assetTypeSelect.SetSelected(trimmedLabel)
			}
		}
		if len(options.PrebuiltResults) > 0 {
			if rbxlSingleActions != nil && rbxlSingleActions.SetSourcePath != nil {
				rbxlSingleActions.SetSourcePath(path)
			}
			if rbxlSingleActions != nil && rbxlSingleActions.SetResults != nil {
				rbxlSingleActions.SetResults(options.PrebuiltResults)
			}
			return
		}
		if rbxlSingleActions != nil && rbxlSingleActions.LoadSource != nil {
			rbxlSingleActions.LoadSource(path)
		}
	}

	return content, func() *ScanTabFileActions {
		return currentActions
	}, providers, selectContext, loadRBXLFile
}
