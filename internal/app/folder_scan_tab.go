package app

import (
	"errors"
	"fmt"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	fyneDialog "fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
	nativeDialog "github.com/sqweek/dialog"
)

func newFolderScanTab(
	window fyne.Window,
) (fyne.CanvasObject, scanTabFileActionsProvider, []scanTabFileActionsProvider, func(string)) {
	singleFolderScan, singleFolderActions := newAssetScanTab(window, assetScanTabOptions{
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

	modeLabel := widget.NewLabel("Mode:")
	modeSwitch := widget.NewRadioGroup([]string{"Single Folder", "Folder Diff"}, nil)
	modeSwitch.Horizontal = true
	modeSwitch.SetSelected("Single Folder")
	contentStack := container.NewStack(singleFolderScan, folderDiffScan)
	contentStack.Objects[1].Hide()
	currentActions := singleFolderActions
	modeSwitch.OnChanged = func(selectedMode string) {
		if strings.EqualFold(selectedMode, "Folder Diff") {
			contentStack.Objects[0].Hide()
			contentStack.Objects[1].Show()
			currentActions = folderDiffActions
			contentStack.Refresh()
			return
		}
		contentStack.Objects[1].Hide()
		contentStack.Objects[0].Show()
		currentActions = singleFolderActions
		contentStack.Refresh()
	}
	selectContext := func(contextKey string) {
		switch strings.TrimSpace(contextKey) {
		case scanContextFolderDiff:
			modeSwitch.SetSelected("Folder Diff")
		default:
			modeSwitch.SetSelected("Single Folder")
		}
	}
	content := container.NewBorder(
		container.NewHBox(modeLabel, modeSwitch),
		nil,
		nil,
		nil,
		contentStack,
	)
	singleProvider := func() *scanTabFileActions { return singleFolderActions }
	diffProvider := func() *scanTabFileActions { return folderDiffActions }
	return content, func() *scanTabFileActions {
		return currentActions
	}, []scanTabFileActionsProvider{singleProvider, diffProvider}, selectContext
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
