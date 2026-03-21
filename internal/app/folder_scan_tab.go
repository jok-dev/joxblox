package app

import (
	"errors"

	"fyne.io/fyne/v2"
	fyneDialog "fyne.io/fyne/v2/dialog"
	nativeDialog "github.com/sqweek/dialog"
)

func newFolderScanTab(window fyne.Window) fyne.CanvasObject {
	return newAssetScanTab(window, assetScanTabOptions{
		NoSourceSelectedText:    "No folder selected.",
		SelectButtonText:        "Select Folder",
		ReadyStatusText:         "Ready to scan folder.",
		MissingSourceStatusText: "Please select a folder first.",
		ScanningStatusText:      "Scanning files for Roblox asset IDs...",
		NoResultsStatusText:     "No Roblox asset IDs found in text files.",
		MaxResultsDefault:       defaultAssetScanLimit,
		SelectSource:            pickFolderSource,
		ExtractHits:             scanFolderForAssetIDs,
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
