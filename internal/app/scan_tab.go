package app

import (
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

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
	actions *scanTabFileActions
}

func newScanTab(
	window fyne.Window,
) (fyne.CanvasObject, scanTabFileActionsProvider, []scanTabFileActionsProvider, func(string), func(string)) {
	folderSingleScan, folderSingleActions := newAssetScanTab(window, assetScanTabOptions{
		NoSourceSelectedText:     "No folder selected.",
		SelectButtonText:         "Select Folder",
		ReadyStatusText:          "Ready.",
		MissingSourceStatusText:  "Select a folder first.",
		ScanningStatusText:       "Scanning...",
		NoResultsStatusText:      "No results found.",
		MaxResultsDefault:        defaultAssetScanLimit,
		ScanContextKey:           scanContextFolder,
		RecentFilesPreferenceKey: "scan.recent.folder",
		SelectSource:             pickFolderSource,
		ExtractHits:              scanFolderForAssetIDs,
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
		ScanContextKey:                   scanContextFolderDiff,
		RecentFilesPreferenceKey:         "scan.recent.folder.diff",
		SelectSource:                     pickFolderBaselineSource,
		SelectSecondarySource:            pickFolderTargetSource,
		ExtractHits:                      scanFolderDiffForAssetIDs,
	})
	rbxlSingleScan, rbxlSingleActions := newAssetScanTab(window, assetScanTabOptions{
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
		MaxResultsDefault:                rustyAssetToolDefaultLimit,
		ScanContextKey:                   scanContextRBXLDiff,
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
	currentActions := rbxlSingleActions
	providers := make([]scanTabFileActionsProvider, 0, len(variants))
	for _, variant := range variants {
		variantActions := variant.actions
		providers = append(providers, func() *scanTabFileActions {
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
		case scanContextFolder:
			sourceSwitch.SetSelected(scanSourceFolders)
			modeSwitch.SetSelected(scanModeSingle)
		case scanContextFolderDiff:
			sourceSwitch.SetSelected(scanSourceFolders)
			modeSwitch.SetSelected(scanModeDiff)
		case scanContextRBXLSingle:
			sourceSwitch.SetSelected(scanSourceRBXL)
			modeSwitch.SetSelected(scanModeSingle)
		case scanContextRBXLDiff:
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
		),
		nil,
		nil,
		nil,
		contentStack,
	)

	loadRBXLFile := func(path string) {
		sourceSwitch.SetSelected(scanSourceRBXL)
		modeSwitch.SetSelected(scanModeSingle)
		updateVisibleContent()
		if rbxlSingleActions != nil && rbxlSingleActions.LoadSource != nil {
			rbxlSingleActions.LoadSource(path)
		}
	}

	return content, func() *scanTabFileActions {
		return currentActions
	}, providers, selectContext, loadRBXLFile
}
