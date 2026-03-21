package app

import (
	"encoding/json"
	"fmt"
	"image/color"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

const (
	hierarchyEmojiTextSize    = 28
	hierarchyEmojiSlotWidth   = 52
	hierarchyEmojiSlotHeight  = 40
	hierarchyEmojiYOffset     = 2
	maxJSONDisplayChars       = 4_000
	maxRustJSONDisplayChars   = 1_500
	maxReferencedIDsDisplay   = 250
	jsonAccordionPollInterval = 200 * time.Millisecond
)

type assetView struct {
	PreviewImage       *canvas.Image
	PreviewPlaceholder *widget.Label
	PreviewContainer   fyne.CanvasObject
	PreviewBox         fyne.CanvasObject
	HierarchySection   fyne.CanvasObject

	AssetIDValue               *widget.Label
	DimensionsValue            *widget.Label
	SelfSizeValue              *widget.Label
	TotalSizeValue             *widget.Label
	FormatValue                *widget.Label
	ContentTypeValue           *widget.Label
	AssetTypeValue             *widget.Label
	ReferencedAssetsCountValue *widget.Label
	StateValue                 *widget.Label
	SourceValue                *widget.Label
	FailureReasonValue         *widget.Label
	FileValue                  *widget.Label

	AssetDeliveryJSONValue *widget.Entry
	ThumbnailJSONValue     *widget.Entry
	EconomyJSONValue       *widget.Entry
	RustExtractorJSONValue *widget.Entry
	ReferencedAssetsValue  *widget.Entry
	JSONAccordion          *widget.Accordion
	NoteLabel              *widget.Label
	MetadataForm           fyne.CanvasObject

	expandImageButton         *widget.Button
	currentAssetID            int64
	hierarchyRows             []assetExplorerRow
	hierarchyTree             *widget.Tree
	hierarchyChildrenByID     map[string][]string
	hierarchyTextByID         map[string]string
	hierarchyEmojiByID        map[string]string
	hierarchySelectAsset      func(int64)
	suppressHierarchyCallback bool
	pendingAssetDeliveryJSON  string
	pendingThumbnailJSON      string
	pendingEconomyJSON        string
	pendingRustExtractorJSON  string
	pendingReferencedAssetIDs []int64
	lastJSONAccordionOpen     bool
}

type assetJSONExport struct {
	AssetID            int64   `json:"asset_id"`
	ExportedAtUTC      string  `json:"exported_at_utc"`
	AssetDeliveryJSON  any     `json:"asset_delivery_json"`
	ThumbnailJSON      any     `json:"thumbnail_json"`
	EconomyJSON        any     `json:"economy_json"`
	RustExtractorJSON  any     `json:"rust_extractor_json"`
	ReferencedAssetIDs []int64 `json:"referenced_asset_ids"`
}

func newAssetView(placeholderText string, includeFileRow bool) *assetView {
	previewImage := canvas.NewImageFromImage(nil)
	previewImage.FillMode = canvas.ImageFillContain
	previewImage.SetMinSize(fyne.NewSize(previewWidth, previewHeight))
	previewPlaceholder := widget.NewLabel(placeholderText)
	previewContainer := container.NewMax(
		container.NewCenter(previewPlaceholder),
		container.NewCenter(previewImage),
	)

	assetDeliveryJSONValue := widget.NewMultiLineEntry()
	assetDeliveryJSONValue.SetText("-")
	assetDeliveryJSONValue.Disable()
	assetDeliveryJSONValue.Wrapping = fyne.TextWrapBreak
	assetDeliveryJSONValue.SetMinRowsVisible(6)

	thumbnailJSONValue := widget.NewMultiLineEntry()
	thumbnailJSONValue.SetText("-")
	thumbnailJSONValue.Disable()
	thumbnailJSONValue.Wrapping = fyne.TextWrapBreak
	thumbnailJSONValue.SetMinRowsVisible(6)

	economyJSONValue := widget.NewMultiLineEntry()
	economyJSONValue.SetText("-")
	economyJSONValue.Disable()
	economyJSONValue.Wrapping = fyne.TextWrapBreak
	economyJSONValue.SetMinRowsVisible(6)

	referencedAssetsValue := widget.NewMultiLineEntry()
	referencedAssetsValue.SetText("-")
	referencedAssetsValue.Disable()
	referencedAssetsValue.Wrapping = fyne.TextWrapBreak
	referencedAssetsValue.SetMinRowsVisible(6)

	rustExtractorJSONValue := widget.NewMultiLineEntry()
	rustExtractorJSONValue.SetText("-")
	rustExtractorJSONValue.Disable()
	rustExtractorJSONValue.Wrapping = fyne.TextWrapBreak
	rustExtractorJSONValue.SetMinRowsVisible(6)
	saveJSONButton := widget.NewButton("Save Full JSON to File", nil)

	jsonAccordion := widget.NewAccordion(
		widget.NewAccordionItem(
			"API JSON Responses",
			container.NewVBox(
				container.NewHBox(layout.NewSpacer(), saveJSONButton),
				widget.NewLabel("AssetDelivery JSON:"),
				assetDeliveryJSONValue,
				widget.NewLabel("Thumbnail JSON:"),
				thumbnailJSONValue,
				widget.NewLabel("Economy Details JSON:"),
				economyJSONValue,
				widget.NewLabel("Rust Extractor JSON:"),
				rustExtractorJSONValue,
				widget.NewLabel("Referenced Asset IDs:"),
				referencedAssetsValue,
			),
		),
	)
	noteLabel := widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Italic: true})
	noteLabel.Importance = widget.DangerImportance
	noteLabel.Wrapping = fyne.TextWrapWord
	noteLabel.Hide()

	view := &assetView{
		PreviewImage:               previewImage,
		PreviewPlaceholder:         previewPlaceholder,
		PreviewContainer:           previewContainer,
		PreviewBox:                 nil,
		HierarchySection:           nil,
		AssetIDValue:               widget.NewLabel("-"),
		DimensionsValue:            widget.NewLabel("-"),
		SelfSizeValue:              widget.NewLabel("-"),
		TotalSizeValue:             widget.NewLabel("-"),
		FormatValue:                widget.NewLabel("-"),
		ContentTypeValue:           widget.NewLabel("-"),
		AssetTypeValue:             widget.NewLabel("-"),
		ReferencedAssetsCountValue: widget.NewLabel("-"),
		StateValue:                 widget.NewLabel("-"),
		SourceValue:                widget.NewLabel("-"),
		FailureReasonValue:         widget.NewLabel("-"),
		FileValue:                  nil,
		AssetDeliveryJSONValue:     assetDeliveryJSONValue,
		ThumbnailJSONValue:         thumbnailJSONValue,
		EconomyJSONValue:           economyJSONValue,
		RustExtractorJSONValue:     rustExtractorJSONValue,
		ReferencedAssetsValue:      referencedAssetsValue,
		JSONAccordion:              jsonAccordion,
		NoteLabel:                  noteLabel,
		expandImageButton:          nil,
		currentAssetID:             0,
		hierarchyRows:              []assetExplorerRow{},
		hierarchyTree:              nil,
		hierarchyChildrenByID:      map[string][]string{},
		hierarchyTextByID:          map[string]string{},
		hierarchyEmojiByID:         map[string]string{},
		hierarchySelectAsset:       nil,
		suppressHierarchyCallback:  false,
		pendingAssetDeliveryJSON:   "",
		pendingThumbnailJSON:       "",
		pendingEconomyJSON:         "",
		pendingRustExtractorJSON:   "",
		pendingReferencedAssetIDs:  []int64{},
	}
	view.FailureReasonValue.Wrapping = fyne.TextWrapWord
	saveJSONButton.OnTapped = func() {
		view.saveJSONExportToFile()
	}
	view.lastJSONAccordionOpen = view.isJSONAccordionOpen()
	view.startJSONAccordionWatcher()

	view.expandImageButton = widget.NewButtonWithIcon("", theme.ViewFullScreenIcon(), func() {
		if view.PreviewImage.Resource == nil {
			return
		}

		guiApp := fyne.CurrentApp()
		if guiApp == nil {
			return
		}

		imageWindow := guiApp.NewWindow(fmt.Sprintf("Asset %d", view.currentAssetID))
		imageCanvas := canvas.NewImageFromResource(view.PreviewImage.Resource)
		imageCanvas.FillMode = canvas.ImageFillContain
		imageWindow.SetContent(container.NewPadded(imageCanvas))
		imageWindow.Resize(fyne.NewSize(900, 700))
		imageWindow.Show()
	})
	view.expandImageButton.Disable()
	view.expandImageButton.Resize(fyne.NewSize(36, 36))
	expandButtonRow := container.NewHBox(layout.NewSpacer(), view.expandImageButton)
	view.PreviewBox = container.NewBorder(nil, expandButtonRow, nil, nil, view.PreviewContainer)
	view.hierarchyTree = widget.NewTree(
		func(uid string) []string {
			if childIDs, exists := view.hierarchyChildrenByID[uid]; exists {
				return childIDs
			}
			return []string{}
		},
		func(uid string) bool {
			childIDs, exists := view.hierarchyChildrenByID[uid]
			return exists && len(childIDs) > 0
		},
		func(branch bool) fyne.CanvasObject {
			emojiText := canvas.NewText("", theme.ForegroundColor())
			emojiText.TextSize = hierarchyEmojiTextSize
			emojiText.Alignment = fyne.TextAlignCenter
			emojiText.Resize(fyne.NewSize(hierarchyEmojiTextSize, hierarchyEmojiTextSize))
			emojiText.Move(fyne.NewPos(
				float32(hierarchyEmojiSlotWidth-hierarchyEmojiTextSize)/2,
				float32(hierarchyEmojiSlotHeight-hierarchyEmojiTextSize)/2+hierarchyEmojiYOffset,
			))
			emojiLayer := container.NewWithoutLayout(emojiText)
			emojiSlot := container.NewMax(
				func() *canvas.Rectangle {
					slotBackground := canvas.NewRectangle(color.Transparent)
					slotBackground.SetMinSize(fyne.NewSize(hierarchyEmojiSlotWidth, hierarchyEmojiSlotHeight))
					return slotBackground
				}(),
				emojiLayer,
			)
			rowLabel := widget.NewLabel("")
			return container.NewHBox(emojiSlot, rowLabel)
		},
		func(uid string, branch bool, object fyne.CanvasObject) {
			rowContainer, isContainer := object.(*fyne.Container)
			if !isContainer || len(rowContainer.Objects) < 2 {
				return
			}
			emojiSlot, isEmojiSlot := rowContainer.Objects[0].(*fyne.Container)
			if !isEmojiSlot || len(emojiSlot.Objects) < 2 {
				return
			}
			emojiLayer, isEmojiLayer := emojiSlot.Objects[1].(*fyne.Container)
			if !isEmojiLayer || len(emojiLayer.Objects) < 1 {
				return
			}
			emojiText, isEmojiText := emojiLayer.Objects[0].(*canvas.Text)
			rowLabel, isRowLabel := rowContainer.Objects[1].(*widget.Label)
			if !isEmojiText || !isRowLabel {
				return
			}
			if uid == "" {
				emojiText.Text = ""
				emojiText.Refresh()
				rowLabel.SetText("")
				return
			}
			rowEmoji, emojiExists := view.hierarchyEmojiByID[uid]
			if !emojiExists {
				rowEmoji = "🧩"
			}
			rowText, textExists := view.hierarchyTextByID[uid]
			if !textExists {
				rowText = uid
			}
			emojiText.Text = rowEmoji
			emojiText.Refresh()
			rowLabel.SetText(rowText)
		},
	)
	view.hierarchyTree.OnSelected = func(uid string) {
		if uid == "" || view.suppressHierarchyCallback || view.hierarchySelectAsset == nil {
			return
		}
		selectedAssetID, parseErr := strconv.ParseInt(uid, 10, 64)
		if parseErr != nil {
			return
		}
		view.hierarchySelectAsset(selectedAssetID)
	}
	hierarchyScroll := container.NewVScroll(view.hierarchyTree)
	hierarchyScroll.SetMinSize(fyne.NewSize(0, 140))
	view.HierarchySection = container.NewVBox(
		widget.NewLabel("Asset Hierarchy"),
		hierarchyScroll,
	)

	formItems := []fyne.CanvasObject{
		widget.NewLabel("Asset ID:"), view.AssetIDValue,
		widget.NewLabel("Dimensions:"), view.DimensionsValue,
		widget.NewLabel("Self Size:"), view.SelfSizeValue,
		widget.NewLabel("Total Size:"), view.TotalSizeValue,
		widget.NewLabel("Format:"), view.FormatValue,
		widget.NewLabel("Content-Type:"), view.ContentTypeValue,
		widget.NewLabel("Asset Type:"), view.AssetTypeValue,
		widget.NewLabel("Referenced Assets:"), view.ReferencedAssetsCountValue,
		widget.NewLabel("State:"), view.StateValue,
		widget.NewLabel("Source:"), view.SourceValue,
		widget.NewLabel("Failure Reason:"), view.FailureReasonValue,
	}
	if includeFileRow {
		view.FileValue = widget.NewLabel("-")
		view.FileValue.Wrapping = fyne.TextWrapWord
		formItems = append(formItems, widget.NewLabel("File:"), view.FileValue)
	}
	view.MetadataForm = container.New(layout.NewFormLayout(), formItems...)

	view.Clear()
	return view
}

func (view *assetView) Clear() {
	view.currentAssetID = 0
	view.PreviewImage.Resource = nil
	view.PreviewImage.Refresh()
	view.PreviewPlaceholder.Show()
	view.AssetIDValue.SetText("-")
	view.DimensionsValue.SetText("-")
	view.SelfSizeValue.SetText("-")
	view.TotalSizeValue.SetText("-")
	view.FormatValue.SetText("-")
	view.ContentTypeValue.SetText("-")
	view.AssetTypeValue.SetText("-")
	view.ReferencedAssetsCountValue.SetText("-")
	view.StateValue.SetText("-")
	view.SourceValue.SetText("-")
	view.FailureReasonValue.SetText("-")
	view.AssetDeliveryJSONValue.SetText("-")
	view.ThumbnailJSONValue.SetText("-")
	view.EconomyJSONValue.SetText("-")
	view.RustExtractorJSONValue.SetText("-")
	view.ReferencedAssetsValue.SetText("-")
	view.pendingAssetDeliveryJSON = ""
	view.pendingThumbnailJSON = ""
	view.pendingEconomyJSON = ""
	view.pendingRustExtractorJSON = ""
	view.pendingReferencedAssetIDs = []int64{}
	view.hierarchyRows = []assetExplorerRow{}
	view.hierarchyChildrenByID = map[string][]string{}
	view.hierarchyTextByID = map[string]string{}
	view.hierarchyEmojiByID = map[string]string{}
	view.hierarchySelectAsset = nil
	view.hierarchyTree.Refresh()
	if view.FileValue != nil {
		view.FileValue.SetText("-")
	}
	view.StateValue.Importance = widget.MediumImportance
	view.SourceValue.Importance = widget.MediumImportance
	view.StateValue.Refresh()
	view.SourceValue.Refresh()
	view.expandImageButton.Disable()
	view.NoteLabel.Hide()
	view.NoteLabel.SetText("")
}

func (view *assetView) SetData(assetID int64, filePath string, previewImageInfo *imageInfo, statsInfo *imageInfo, totalBytesSize int, sourceDescription string, stateDescription string, warningMessage string, assetDeliveryRawJSON string, thumbnailRawJSON string, economyRawJSON string, rustExtractorRawJSON string, referencedAssetIDs []int64, assetTypeID int, assetTypeName string) {
	if statsInfo == nil {
		statsInfo = previewImageInfo
	}

	view.currentAssetID = assetID
	view.AssetIDValue.SetText(strconv.FormatInt(assetID, 10))
	if statsInfo.Width > 0 && statsInfo.Height > 0 {
		view.DimensionsValue.SetText(fmt.Sprintf("%dx%d", statsInfo.Width, statsInfo.Height))
	} else {
		view.DimensionsValue.SetText("-")
	}
	view.SelfSizeValue.SetText(formatBytesSizeMB(statsInfo.BytesSize))
	if totalBytesSize <= 0 {
		totalBytesSize = statsInfo.BytesSize
	}
	view.TotalSizeValue.SetText(formatBytesSizeMB(totalBytesSize))
	if strings.TrimSpace(statsInfo.Format) != "" {
		view.FormatValue.SetText(statsInfo.Format)
	} else {
		view.FormatValue.SetText("-")
	}
	if strings.TrimSpace(statsInfo.ContentType) != "" {
		view.ContentTypeValue.SetText(statsInfo.ContentType)
	} else {
		view.ContentTypeValue.SetText("-")
	}
	if assetTypeID > 0 {
		view.AssetTypeValue.SetText(fmt.Sprintf("%s (%d)", assetTypeName, assetTypeID))
	} else {
		view.AssetTypeValue.SetText(assetTypeName)
	}
	if warningMessage != "" {
		view.FailureReasonValue.SetText(warningMessage)
	} else {
		view.FailureReasonValue.SetText("-")
	}
	view.pendingAssetDeliveryJSON = assetDeliveryRawJSON
	view.pendingThumbnailJSON = thumbnailRawJSON
	view.pendingEconomyJSON = economyRawJSON
	view.pendingRustExtractorJSON = rustExtractorRawJSON
	view.pendingReferencedAssetIDs = append([]int64(nil), referencedAssetIDs...)
	if len(referencedAssetIDs) > 0 {
		view.ReferencedAssetsCountValue.SetText(strconv.Itoa(len(referencedAssetIDs)))
	} else {
		view.ReferencedAssetsCountValue.SetText("0")
	}
	if view.isJSONAccordionOpen() {
		view.renderJSONDetails()
	} else {
		view.showLazyJSONPlaceholder()
	}
	if view.FileValue != nil {
		if strings.TrimSpace(filePath) == "" {
			view.FileValue.SetText("-")
		} else {
			view.FileValue.SetText(filePath)
		}
	}

	view.StateValue.SetText(stateDescription)
	view.StateValue.Importance = widget.MediumImportance
	view.SourceValue.SetText(sourceDescription)
	view.SourceValue.Importance = widget.MediumImportance
	view.NoteLabel.Hide()
	view.NoteLabel.SetText("")

	isThumbnailFallbackSource := isThumbnailFallback(sourceDescription)
	thumbnailStateNotCompleted := isThumbnailFallbackSource && !isCompletedState(stateDescription)
	if isThumbnailFallbackSource {
		view.SourceValue.SetText(fmt.Sprintf("⚠ %s", sourceDescription))
		view.SourceValue.Importance = widget.DangerImportance
	}
	if thumbnailStateNotCompleted {
		view.StateValue.SetText(fmt.Sprintf("⚠ %s", stateDescription))
		view.StateValue.Importance = widget.DangerImportance
	}
	if warningMessage != "" {
		view.NoteLabel.SetText(buildFallbackWarningText(warningMessage))
		view.NoteLabel.Show()
	}
	view.StateValue.Refresh()
	view.SourceValue.Refresh()

	view.PreviewImage.Resource = previewImageInfo.Resource
	view.PreviewImage.Refresh()
	view.PreviewPlaceholder.Hide()
	view.expandImageButton.Enable()
}

func formatBytesSizeMB(bytesSize int) string {
	return fmt.Sprintf("%.2f MB", float64(bytesSize)/megabyte)
}

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
	if view.pendingRustExtractorJSON != "" {
		view.RustExtractorJSONValue.SetText(truncateJSONForDisplay(view.pendingRustExtractorJSON, maxRustJSONDisplayChars))
	} else {
		view.RustExtractorJSONValue.SetText("-")
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
	view.RustExtractorJSONValue.SetText("(lazy-loaded) Open this section to view")
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
		RustExtractorJSON:  parseJSONOrRawString(view.pendingRustExtractorJSON),
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

func getPrimaryWindow() fyne.Window {
	currentApp := fyne.CurrentApp()
	if currentApp == nil {
		return nil
	}
	windows := currentApp.Driver().AllWindows()
	if len(windows) == 0 {
		return nil
	}
	return windows[0]
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

func (view *assetView) SetHierarchy(rows []assetExplorerRow, selectedAssetID int64, selectAsset func(int64)) {
	view.hierarchyRows = rows
	view.hierarchySelectAsset = selectAsset
	view.hierarchyChildrenByID = map[string][]string{}
	view.hierarchyTextByID = map[string]string{}
	view.hierarchyEmojiByID = map[string]string{}
	parentByDepth := map[int]string{}
	for _, row := range rows {
		assetUID := strconv.FormatInt(row.AssetID, 10)
		parentUID := ""
		if row.Depth > 0 {
			if knownParentUID, exists := parentByDepth[row.Depth-1]; exists {
				parentUID = knownParentUID
			}
		}
		view.hierarchyChildrenByID[parentUID] = append(view.hierarchyChildrenByID[parentUID], assetUID)
		if _, exists := view.hierarchyChildrenByID[assetUID]; !exists {
			view.hierarchyChildrenByID[assetUID] = []string{}
		}
		parentByDepth[row.Depth] = assetUID

		sizeText := "size unavailable"
		if row.SelfBytesSize > 0 {
			sizeText = formatBytesSizeMB(row.SelfBytesSize)
		}
		nodeIcon := getAssetTypeEmoji(row.AssetTypeID)
		view.hierarchyEmojiByID[assetUID] = nodeIcon
		view.hierarchyTextByID[assetUID] = fmt.Sprintf("%s (%s)", assetUID, sizeText)
	}

	view.hierarchyTree.Refresh()
	for _, row := range rows {
		if row.Depth == 0 {
			rootUID := strconv.FormatInt(row.AssetID, 10)
			view.hierarchyTree.OpenBranch(rootUID)
		}
	}

	selectedUID := strconv.FormatInt(selectedAssetID, 10)
	view.suppressHierarchyCallback = true
	view.hierarchyTree.Select(selectedUID)
	view.suppressHierarchyCallback = false
}
