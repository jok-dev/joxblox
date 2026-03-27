package app

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/storage"
)

func (view *assetView) renderJSONDetails() {
	if view.pendingAssetDeliveryJSON != "" {
		view.AssetDeliveryJSONValue.SetText(truncateJSONForDisplay(view.pendingAssetDeliveryJSON, maxJSONDisplayChars))
	} else {
		view.AssetDeliveryJSONValue.SetText("-")
	}
	if view.pendingThumbnailJSON != "" {
		view.ThumbnailJSONValue.SetText(truncateJSONForDisplay(view.pendingThumbnailJSON, maxJSONDisplayChars))
	} else {
		view.ThumbnailJSONValue.SetText("-")
	}
	if view.pendingEconomyJSON != "" {
		view.EconomyJSONValue.SetText(truncateJSONForDisplay(view.pendingEconomyJSON, maxJSONDisplayChars))
	} else {
		view.EconomyJSONValue.SetText("-")
	}
	if view.pendingRustyAssetToolJSON != "" {
		view.RustyAssetToolJSONValue.SetText(truncateJSONForDisplay(view.pendingRustyAssetToolJSON, maxRustJSONDisplayChars))
	} else {
		view.RustyAssetToolJSONValue.SetText("-")
	}
	if len(view.pendingReferencedAssetIDs) > 0 {
		view.ReferencedAssetsValue.SetText(formatReferencedAssetIDsForDisplay(view.pendingReferencedAssetIDs))
	} else {
		view.ReferencedAssetsValue.SetText("-")
	}
}

func (view *assetView) showLazyJSONPlaceholder() {
	view.AssetDeliveryJSONValue.SetText("(lazy-loaded) Open this section to view")
	view.ThumbnailJSONValue.SetText("(lazy-loaded) Open this section to view")
	view.EconomyJSONValue.SetText("(lazy-loaded) Open this section to view")
	view.RustyAssetToolJSONValue.SetText("(lazy-loaded) Open this section to view")
	if len(view.pendingReferencedAssetIDs) > 0 {
		view.ReferencedAssetsValue.SetText("(lazy-loaded) Open this section to view")
	} else {
		view.ReferencedAssetsValue.SetText("-")
	}
}

func (view *assetView) isJSONAccordionOpen() bool {
	if len(view.JSONAccordion.Items) == 0 {
		return false
	}
	return view.JSONAccordion.Items[0].Open
}

func (view *assetView) startJSONAccordionWatcher() {
	go func() {
		pollTicker := time.NewTicker(jsonAccordionPollInterval)
		defer pollTicker.Stop()
		for range pollTicker.C {
			fyne.Do(func() {
				isAccordionOpen := view.isJSONAccordionOpen()
				if isAccordionOpen == view.lastJSONAccordionOpen {
					return
				}
				view.lastJSONAccordionOpen = isAccordionOpen
				if isAccordionOpen {
					view.renderJSONDetails()
					return
				}
				view.showLazyJSONPlaceholder()
			})
		}
	}()
}

func (view *assetView) saveJSONExportToFile() {
	window := getPrimaryWindow()
	if window == nil {
		return
	}

	exportPayload := assetJSONExport{
		AssetID:            view.currentAssetID,
		ExportedAtUTC:      time.Now().UTC().Format(time.RFC3339),
		AssetDeliveryJSON:  parseJSONOrRawString(view.pendingAssetDeliveryJSON),
		ThumbnailJSON:      parseJSONOrRawString(view.pendingThumbnailJSON),
		EconomyJSON:        parseJSONOrRawString(view.pendingEconomyJSON),
		RustyAssetToolJSON: parseJSONOrRawString(view.pendingRustyAssetToolJSON),
		ReferencedAssetIDs: append([]int64(nil), view.pendingReferencedAssetIDs...),
	}

	saveDialog := dialog.NewFileSave(func(writer fyne.URIWriteCloser, dialogErr error) {
		if dialogErr != nil {
			dialog.ShowError(dialogErr, window)
			return
		}
		if writer == nil {
			return
		}
		defer func() {
			_ = writer.Close()
		}()

		jsonBytes, marshalErr := json.MarshalIndent(exportPayload, "", "  ")
		if marshalErr != nil {
			dialog.ShowError(marshalErr, window)
			return
		}
		if _, writeErr := writer.Write(append(jsonBytes, '\n')); writeErr != nil {
			dialog.ShowError(writeErr, window)
			return
		}
		logDebugf("Saved JSON export for asset %d", view.currentAssetID)
	}, window)
	saveDialog.SetFilter(storage.NewExtensionFileFilter([]string{".json"}))
	saveDialog.SetFileName(fmt.Sprintf("asset-%d-details.json", view.currentAssetID))
	saveDialog.Show()
}

func parseJSONOrRawString(rawContent string) any {
	trimmedContent := strings.TrimSpace(rawContent)
	if trimmedContent == "" {
		return nil
	}
	var decodedJSON any
	if jsonErr := json.Unmarshal([]byte(trimmedContent), &decodedJSON); jsonErr == nil {
		return decodedJSON
	}
	return trimmedContent
}

func truncateJSONForDisplay(rawJSON string, maxDisplayChars int) string {
	if len(rawJSON) <= maxDisplayChars {
		return rawJSON
	}
	return fmt.Sprintf(
		"%s\n\n... [truncated %d chars]",
		rawJSON[:maxDisplayChars],
		len(rawJSON)-maxDisplayChars,
	)
}

func formatReferencedAssetIDsForDisplay(referencedAssetIDs []int64) string {
	displayCount := len(referencedAssetIDs)
	if displayCount > maxReferencedIDsDisplay {
		displayCount = maxReferencedIDsDisplay
	}
	referencedIDStrings := make([]string, 0, displayCount)
	for index := 0; index < displayCount; index++ {
		referencedIDStrings = append(referencedIDStrings, strconv.FormatInt(referencedAssetIDs[index], 10))
	}
	if len(referencedAssetIDs) <= maxReferencedIDsDisplay {
		return strings.Join(referencedIDStrings, "\n")
	}
	return strings.Join(referencedIDStrings, "\n") + fmt.Sprintf(
		"\n\n... [truncated %d ids]",
		len(referencedAssetIDs)-maxReferencedIDsDisplay,
	)
}
