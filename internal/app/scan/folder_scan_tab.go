package scan

import (
	"errors"
	"fmt"

	"joxblox/internal/app/loader"

	"fyne.io/fyne/v2"
	fyneDialog "fyne.io/fyne/v2/dialog"
	nativeDialog "github.com/sqweek/dialog"
)

func newFolderScanTab(
	window fyne.Window,
) (fyne.CanvasObject, ScanTabFileActionsProvider, []ScanTabFileActionsProvider, func(string)) {
	return buildScanModeTab(scanModeTabConfig{
		Window:        window,
		SingleLabel:   "Single Folder",
		DiffLabel:     "Folder Diff",
		SingleContext: ScanContextFolder,
		DiffContext:   ScanContextFolderDiff,
		SingleOptions: assetScanTabOptions{
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
		},
		DiffOptions: assetScanTabOptions{
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
		},
	})
}

func pickFolderSource(window fyne.Window, onSelected func(string), onError func(error)) {
	selectedPath, pickerErr := nativeDialog.Directory().Title("Select folder to scan").Browse()
	if pickerErr == nil {
		onSelected(selectedPath)
		return
	}

	if errors.Is(pickerErr, nativeDialog.Cancelled) {
		return
	}

	// Fallback to Fyne picker if native dialog fails unexpectedly.
	fyneDialog.ShowFolderOpen(func(uri fyne.ListableURI, err error) {
		if err != nil {
			onError(err)
			return
		}
		if uri == nil {
			return
		}

		onSelected(uri.Path())
	}, window)
}

func pickFolderBaselineSource(window fyne.Window, onSelected func(string), onError func(error)) {
	_ = window
	selectedPath, pickerErr := nativeDialog.Directory().Title("Select baseline folder (old)").Browse()
	if pickerErr == nil {
		onSelected(selectedPath)
		return
	}
	if errors.Is(pickerErr, nativeDialog.Cancelled) {
		return
	}
	onError(fmt.Errorf("baseline picker failed: %w", pickerErr))
}

func pickFolderTargetSource(window fyne.Window, onSelected func(string), onError func(error)) {
	_ = window
	selectedPath, pickerErr := nativeDialog.Directory().Title("Select target folder (new)").Browse()
	if pickerErr == nil {
		onSelected(selectedPath)
		return
	}
	if errors.Is(pickerErr, nativeDialog.Cancelled) {
		return
	}
	onError(fmt.Errorf("target picker failed: %w", pickerErr))
}
