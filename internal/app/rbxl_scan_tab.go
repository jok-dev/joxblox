package app

import (
	"errors"
	"os"

	"fyne.io/fyne/v2"
	fyneDialog "fyne.io/fyne/v2/dialog"
	nativeDialog "github.com/sqweek/dialog"
)

func newRBXLScanTab(window fyne.Window) fyne.CanvasObject {
	return newAssetScanTab(window, assetScanTabOptions{
		NoSourceSelectedText:    "No .rbxl file selected.",
		SelectButtonText:        "Select .rbxl File",
		ReadyStatusText:         "Ready to scan .rbxl file.",
		MissingSourceStatusText: "Please select an .rbxl file first.",
		ScanningStatusText:      "Extracting asset IDs from .rbxl...",
		NoResultsStatusText:     "No Roblox asset IDs found in this .rbxl file.",
		MaxResultsDefault:       rustExtractorDefaultLimit,
		SelectSource:            pickRBXLSource,
		ExtractHits:             scanRBXLFileForAssetIDs,
	})
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

func scanRBXLFileForAssetIDs(filePath string, limit int, stopChannel <-chan struct{}) ([]scanHit, error) {
	logDebugf("RBXL scan started: %s (limit=%d)", filePath, limit)
	if _, readErr := os.Stat(filePath); readErr != nil {
		logDebugf("RBXL scan read failed: %s", readErr.Error())
		return nil, readErr
	}

	rustAssetIDs, _, rustScanErr := extractAssetIDsWithRustFromFile(filePath, limit, stopChannel)
	if errors.Is(rustScanErr, errScanStopped) {
		logDebugf("RBXL scan stopped during Rust extraction")
		return nil, errScanStopped
	}
	if rustScanErr != nil {
		logDebugf("RBXL scan Rust extraction failed: %s", rustScanErr.Error())
		return nil, rustScanErr
	}

	hits := make([]scanHit, 0, len(rustAssetIDs))
	for _, assetID := range rustAssetIDs {
		hits = append(hits, scanHit{
			AssetID:  assetID,
			FilePath: filePath,
		})
	}
	logDebugf("RBXL scan completed with %d unique asset IDs", len(hits))
	return hits, nil
}
