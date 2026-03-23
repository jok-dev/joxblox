package app

import (
	"fmt"
	"image/color"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

const (
	hierarchyEmojiTextSize    = 28
	hierarchyEmojiSlotWidth   = 52
	hierarchyEmojiSlotHeight  = 40
	hierarchyEmojiYOffset     = 2
	metadataLabelColumnWidth  = 180
	metadataLabelRowHeight    = 24
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

	DimensionsLabel            *widget.Label
	AssetIDValue               *widget.Label
	DimensionsValue            *widget.Label
	SelfSizeValue              *widget.Label
	TotalSizeValue             *widget.Label
	FormatValue                *widget.Label
	ContentTypeValue           *widget.Label
	AssetTypeValue             *widget.Label
	ReferencedAssetsCountValue *widget.Label
	ReferenceInstanceTypeValue *widget.Label
	ReferencePropertyNameValue *widget.Label
	ReferenceInstancePathValue *widget.Label
	SourceValue                *widget.Label
	UseCountValue              *widget.Label
	FailureReasonValue         *widget.Label
	FileValue                  *widget.Label
	FileSHA256Value            *widget.Label

	AssetDeliveryJSONValue *widget.Entry
	ThumbnailJSONValue     *widget.Entry
	EconomyJSONValue       *widget.Entry
	RustExtractorJSONValue *widget.Entry
	ReferencedAssetsValue  *widget.Entry
	JSONAccordion          *widget.Accordion
	NoteLabel              *widget.Label
	MetadataForm           fyne.CanvasObject

	expandImageButton         *widget.Button
	downloadImageButton       *widget.Button
	previewVariantSelect      *widget.Select
	playAudioButton           *widget.Button
	stopAudioButton           *widget.Button
	audioProgressSlider       *widget.Slider
	audioCurrentTimeLabel     *widget.Label
	audioTotalTimeLabel       *widget.Label
	audioVolumeSlider         *widget.Slider
	audioVolumeValueLabel     *widget.Label
	audioControls             *fyne.Container
	audioPlayer               *assetAudioPlayer
	audioDuration             time.Duration
	suppressAudioSeekChange   bool
	audioSeekDragging         bool
	suppressAudioVolumeChange bool
	audioLoadToken            atomic.Uint64
	previewVariantBuildToken  atomic.Uint64
	currentAssetID            int64
	hierarchyRows             []assetExplorerRow
	hierarchyList             *fyne.Container
	hierarchySelectAsset      func(int64)
	selectedHierarchyAssetID  int64
	pendingAssetDeliveryJSON  string
	pendingThumbnailJSON      string
	pendingEconomyJSON        string
	pendingRustExtractorJSON  string
	pendingReferencedAssetIDs []int64
	lastJSONAccordionOpen     bool
	previewDownloadOptions    []previewDownloadOption
	selectedPreviewOption     string
	suppressPreviewVariant    bool
	assetDownloadBytes        []byte
	assetDownloadFileName     string
	downloadOriginalAsset     bool
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

type previewDownloadOption struct {
	labelText string
	fileName  string
	bytes     []byte
	width     int
	height    int
}

type zoomPanImage struct {
	widget.BaseWidget
	background *canvas.Rectangle
	image      *canvas.Image
	option     previewDownloadOption
	zoom       float64
	offsetX    float32
	offsetY    float32
}

func newAssetView(placeholderText string, includeFileRow bool) *assetView {
	previewImage := canvas.NewImageFromImage(nil)
	previewImage.FillMode = canvas.ImageFillContain
	previewImage.ScaleMode = canvas.ImageScaleFastest
	previewImage.SetMinSize(fyne.NewSize(previewWidth, previewHeight))
	previewPlaceholder := widget.NewLabel(placeholderText)
	previewContainer := container.NewMax(
		container.NewCenter(previewPlaceholder),
		container.NewCenter(previewImage),
	)

	assetDeliveryJSONValue := newReadOnlyMultilineEntry(6)
	thumbnailJSONValue := newReadOnlyMultilineEntry(6)
	economyJSONValue := newReadOnlyMultilineEntry(6)
	referencedAssetsValue := newReadOnlyMultilineEntry(6)
	rustExtractorJSONValue := newReadOnlyMultilineEntry(6)
	saveJSONButton := widget.NewButton("Save Full JSON to File", nil)
	dimensionsLabel := widget.NewLabel("Dimensions:")

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
	audioCurrentTimeLabel := widget.NewLabel("0:00")
	audioTotalTimeLabel := widget.NewLabel("0:00")
	audioVolumeValueLabel := widget.NewLabel("40%")
	audioProgressSlider := widget.NewSlider(0, 1)
	audioProgressSlider.Step = 0.001
	audioProgressSlider.Disable()
	audioVolumeSlider := widget.NewSlider(0, 1)
	audioVolumeSlider.Step = 0.01
	audioVolumeSlider.SetValue(defaultAudioVolume)
	audioVolumeSlider.Disable()

	view := &assetView{
		PreviewImage:               previewImage,
		PreviewPlaceholder:         previewPlaceholder,
		PreviewContainer:           previewContainer,
		PreviewBox:                 nil,
		HierarchySection:           nil,
		DimensionsLabel:            dimensionsLabel,
		AssetIDValue:               newMetadataValueLabel(),
		DimensionsValue:            newMetadataValueLabel(),
		SelfSizeValue:              newMetadataValueLabel(),
		TotalSizeValue:             newMetadataValueLabel(),
		FormatValue:                newMetadataValueLabel(),
		ContentTypeValue:           newMetadataValueLabel(),
		AssetTypeValue:             newMetadataValueLabel(),
		ReferencedAssetsCountValue: newMetadataValueLabel(),
		ReferenceInstanceTypeValue: newMetadataValueLabel(),
		ReferencePropertyNameValue: newMetadataValueLabel(),
		ReferenceInstancePathValue: newMetadataValueLabel(),
		SourceValue:                newMetadataValueLabel(),
		UseCountValue:              newMetadataValueLabel(),
		FailureReasonValue:         newMetadataValueLabel(),
		FileValue:                  nil,
		FileSHA256Value:            nil,
		AssetDeliveryJSONValue:     assetDeliveryJSONValue,
		ThumbnailJSONValue:         thumbnailJSONValue,
		EconomyJSONValue:           economyJSONValue,
		RustExtractorJSONValue:     rustExtractorJSONValue,
		ReferencedAssetsValue:      referencedAssetsValue,
		JSONAccordion:              jsonAccordion,
		NoteLabel:                  noteLabel,
		expandImageButton:          nil,
		downloadImageButton:        nil,
		previewVariantSelect:       nil,
		playAudioButton:            nil,
		stopAudioButton:            nil,
		audioProgressSlider:        audioProgressSlider,
		audioCurrentTimeLabel:      audioCurrentTimeLabel,
		audioTotalTimeLabel:        audioTotalTimeLabel,
		audioVolumeSlider:          audioVolumeSlider,
		audioVolumeValueLabel:      audioVolumeValueLabel,
		audioControls:              nil,
		audioPlayer:                nil,
		audioDuration:              0,
		suppressAudioSeekChange:    false,
		audioSeekDragging:          false,
		suppressAudioVolumeChange:  false,
		currentAssetID:             0,
		hierarchyRows:              []assetExplorerRow{},
		hierarchyList:              container.NewVBox(),
		hierarchySelectAsset:       nil,
		selectedHierarchyAssetID:   0,
		pendingAssetDeliveryJSON:   "",
		pendingThumbnailJSON:       "",
		pendingEconomyJSON:         "",
		pendingRustExtractorJSON:   "",
		pendingReferencedAssetIDs:  []int64{},
		previewDownloadOptions:     []previewDownloadOption{},
		selectedPreviewOption:      "",
		suppressPreviewVariant:     false,
		assetDownloadBytes:         []byte{},
		assetDownloadFileName:      "",
		downloadOriginalAsset:      false,
	}
	saveJSONButton.OnTapped = func() {
		view.saveJSONExportToFile()
	}
	view.lastJSONAccordionOpen = view.isJSONAccordionOpen()
	view.startJSONAccordionWatcher()

	view.expandImageButton = widget.NewButtonWithIcon("", theme.ViewFullScreenIcon(), func() {
		view.showExpandedImageWindow()
	})
	view.expandImageButton.Disable()
	view.expandImageButton.Resize(fyne.NewSize(36, 36))
	view.downloadImageButton = widget.NewButtonWithIcon("", theme.DownloadIcon(), func() {
		view.saveSelectedPreviewVariantToFile()
	})
	view.downloadImageButton.Disable()
	view.playAudioButton = widget.NewButtonWithIcon("Play", theme.MediaPlayIcon(), func() {
		if view.audioPlayer == nil {
			return
		}
		if err := view.audioPlayer.TogglePlayPause(); err != nil {
			view.updateAudioPlaybackState(audioPlayerStatus{
				Available: false,
				Message:   err.Error(),
			})
		}
	})
	view.playAudioButton.Disable()
	view.stopAudioButton = widget.NewButtonWithIcon("", theme.MediaStopIcon(), func() {
		if view.audioPlayer == nil {
			return
		}
		view.audioPlayer.Stop()
	})
	view.stopAudioButton.Disable()
	view.audioProgressSlider.OnChanged = func(value float64) {
		if view.suppressAudioSeekChange {
			return
		}
		view.audioSeekDragging = true
		view.audioCurrentTimeLabel.SetText(formatDurationCompact(time.Duration(value * float64(view.audioDuration))))
	}
	view.audioProgressSlider.OnChangeEnded = func(value float64) {
		if view.suppressAudioSeekChange || view.audioPlayer == nil {
			view.audioSeekDragging = false
			return
		}
		if err := view.audioPlayer.SeekToFraction(value); err != nil {
			view.audioSeekDragging = false
			view.updateAudioPlaybackState(audioPlayerStatus{
				Available: false,
				Message:   err.Error(),
			})
			return
		}
		view.audioSeekDragging = false
	}
	view.audioVolumeSlider.OnChanged = func(value float64) {
		if view.suppressAudioVolumeChange {
			return
		}
		view.audioVolumeValueLabel.SetText(fmt.Sprintf("%d%%", int(clampAudioSliderValue(value)*100)))
		if view.audioPlayer == nil {
			return
		}
		if err := view.audioPlayer.SetVolume(value); err != nil {
			view.updateAudioPlaybackState(audioPlayerStatus{
				Available: false,
				Message:   err.Error(),
			})
		}
	}
	playButtonWrap := container.NewGridWrap(fyne.NewSize(96, 36), view.playAudioButton)
	stopButtonWrap := container.NewGridWrap(fyne.NewSize(44, 36), view.stopAudioButton)
	buttonRow := container.NewHBox(playButtonWrap, stopButtonWrap)
	progressRow := container.NewBorder(nil, nil, view.audioCurrentTimeLabel, view.audioTotalTimeLabel, view.audioProgressSlider)
	volumeSliderWrap := container.NewGridWrap(fyne.NewSize(140, 36), view.audioVolumeSlider)
	volumeControls := container.NewHBox(widget.NewLabel("Volume"), volumeSliderWrap, view.audioVolumeValueLabel)
	view.audioControls = container.NewVBox(container.NewBorder(nil, nil, buttonRow, volumeControls, progressRow))
	view.audioControls.Hide()
	view.audioPlayer = newAssetAudioPlayer(view.updateAudioPlaybackState)
	view.previewVariantSelect = widget.NewSelect([]string{}, func(selectedLabel string) {
		if view.suppressPreviewVariant {
			return
		}
		view.selectedPreviewOption = selectedLabel
		view.applySelectedPreviewVariant()
	})
	view.previewVariantSelect.Disable()
	previewVariantControl := container.NewGridWrap(fyne.NewSize(240, 36), view.previewVariantSelect)
	expandButtonRow := container.NewHBox(layout.NewSpacer(), previewVariantControl, view.downloadImageButton, view.expandImageButton)
	previewBody := container.NewVBox(view.PreviewContainer, view.audioControls)
	view.PreviewBox = container.NewBorder(nil, expandButtonRow, nil, nil, previewBody)
	hierarchyMinHeight := canvas.NewRectangle(color.Transparent)
	hierarchyMinHeight.SetMinSize(fyne.NewSize(0, 140))
	hierarchyContent := container.NewMax(hierarchyMinHeight, view.hierarchyList)
	view.HierarchySection = container.NewVBox(
		widget.NewLabel("Asset Hierarchy"),
		hierarchyContent,
	)

	formItems := []fyne.CanvasObject{
		newMetadataRow("Dimensions:", view.DimensionsValue),
		newMetadataRow("Self Size:", view.SelfSizeValue),
		newMetadataRow("Total Size:", view.TotalSizeValue),
		newMetadataRow("Format:", view.FormatValue),
		newMetadataRow("Content-Type:", view.ContentTypeValue),
		newMetadataRow("Asset Type:", view.AssetTypeValue),
		newMetadataRow("Referenced Assets:", view.ReferencedAssetsCountValue),
		newMetadataRow("Reference Instance Type:", view.ReferenceInstanceTypeValue),
		newMetadataRow("Reference Property Name:", view.ReferencePropertyNameValue),
		newMetadataRow("Reference Instance Path:", view.ReferenceInstancePathValue),
		newMetadataRow("Image Source:", view.SourceValue),
		newMetadataRow("Use Count:", view.UseCountValue),
		newMetadataRow("Failure Reason:", view.FailureReasonValue),
	}
	if includeFileRow {
		view.FileValue = newMetadataValueLabel()
		view.FileSHA256Value = newMetadataValueLabel()
		formItems = append(
			formItems,
			newMetadataRow("File:", view.FileValue),
			newMetadataRow("Downloaded SHA256:", view.FileSHA256Value),
		)
	}
	view.MetadataForm = container.NewVBox(formItems...)

	view.Clear()
	return view
}

func (view *assetView) Clear() {
	view.currentAssetID = 0
	view.audioLoadToken.Add(1)
	view.previewVariantBuildToken.Add(1)
	view.PreviewImage.File = ""
	view.PreviewImage.Image = nil
	view.PreviewImage.Resource = nil
	view.PreviewImage.Refresh()
	view.PreviewImage.Hide()
	view.PreviewPlaceholder.Show()
	view.PreviewContainer.Refresh()
	view.AssetIDValue.SetText("-")
	view.DimensionsLabel.SetText("Dimensions:")
	view.DimensionsValue.SetText("-")
	view.SelfSizeValue.SetText("-")
	view.TotalSizeValue.SetText("-")
	view.FormatValue.SetText("-")
	view.ContentTypeValue.SetText("-")
	view.AssetTypeValue.SetText("-")
	view.ReferencedAssetsCountValue.SetText("-")
	view.ReferenceInstanceTypeValue.SetText("-")
	view.ReferencePropertyNameValue.SetText("-")
	view.ReferenceInstancePathValue.SetText("-")
	view.SourceValue.SetText("-")
	view.UseCountValue.SetText("-")
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
	view.previewDownloadOptions = []previewDownloadOption{}
	view.selectedPreviewOption = ""
	view.assetDownloadBytes = []byte{}
	view.assetDownloadFileName = ""
	view.downloadOriginalAsset = false
	view.selectedHierarchyAssetID = 0
	view.hierarchyRows = []assetExplorerRow{}
	view.hierarchySelectAsset = nil
	view.hierarchyList.Objects = nil
	view.hierarchyList.Refresh()
	if view.FileValue != nil {
		view.FileValue.SetText("-")
	}
	if view.FileSHA256Value != nil {
		view.FileSHA256Value.SetText("-")
	}
	view.SourceValue.Importance = widget.MediumImportance
	view.SourceValue.Refresh()
	view.suppressPreviewVariant = true
	view.previewVariantSelect.ClearSelected()
	view.previewVariantSelect.SetOptions([]string{})
	view.suppressPreviewVariant = false
	view.previewVariantSelect.Disable()
	view.expandImageButton.Disable()
	view.downloadImageButton.Disable()
	if view.audioPlayer != nil {
		view.audioPlayer.Reset()
	}
	view.resetAudioControls()
	view.audioControls.Hide()
	view.NoteLabel.Hide()
	view.NoteLabel.SetText("")
}

func newReadOnlyMultilineEntry(minRowsVisible int) *widget.Entry {
	entry := widget.NewMultiLineEntry()
	entry.SetText("-")
	entry.Disable()
	entry.Wrapping = fyne.TextWrapBreak
	entry.SetMinRowsVisible(minRowsVisible)
	return entry
}

func newMetadataValueLabel() *widget.Label {
	label := widget.NewLabel("-")
	label.Wrapping = fyne.TextWrapBreak
	return label
}

func newMetadataRow(labelText string, value fyne.CanvasObject) fyne.CanvasObject {
	labelSlot := container.NewGridWrap(
		fyne.NewSize(metadataLabelColumnWidth, metadataLabelRowHeight),
		widget.NewLabel(labelText),
	)
	return container.NewBorder(nil, nil, labelSlot, nil, value)
}

func setLabelTextOrDash(label *widget.Label, value string) {
	if label == nil {
		return
	}
	trimmedValue := strings.TrimSpace(value)
	if trimmedValue == "" {
		label.SetText("-")
		return
	}
	label.SetText(trimmedValue)
}

func (view *assetView) SetData(assetID int64, filePath string, fileSHA256 string, useCount int, previewImageInfo *imageInfo, statsInfo *imageInfo, totalBytesSize int, sourceDescription string, stateDescription string, warningMessage string, assetDeliveryRawJSON string, thumbnailRawJSON string, economyRawJSON string, rustExtractorRawJSON string, referencedAssetIDs []int64, referenceInstanceType string, referencePropertyName string, referenceInstancePath string, assetTypeID int, assetTypeName string, downloadBytes []byte, downloadFileName string, downloadIsOriginal bool) {
	previewBuildToken := view.previewVariantBuildToken.Add(1)
	if statsInfo == nil {
		statsInfo = previewImageInfo
	}
	if statsInfo == nil {
		statsInfo = &imageInfo{}
	}

	view.currentAssetID = assetID
	view.AssetIDValue.SetText(strconv.FormatInt(assetID, 10))
	view.DimensionsLabel.SetText("Dimensions:")
	if isAudioAssetContent(assetTypeID, statsInfo.ContentType) {
		view.DimensionsValue.SetText("-")
	} else {
		if statsInfo.Width > 0 && statsInfo.Height > 0 {
			view.DimensionsValue.SetText(fmt.Sprintf("%dx%d", statsInfo.Width, statsInfo.Height))
		} else {
			view.DimensionsValue.SetText("-")
		}
	}
	view.SelfSizeValue.SetText(formatSizeAuto(statsInfo.BytesSize))
	if totalBytesSize <= 0 {
		totalBytesSize = statsInfo.BytesSize
	}
	view.TotalSizeValue.SetText(formatSizeAuto(totalBytesSize))
	setLabelTextOrDash(view.FormatValue, statsInfo.Format)
	setLabelTextOrDash(view.ContentTypeValue, statsInfo.ContentType)
	if assetTypeID > 0 {
		view.AssetTypeValue.SetText(fmt.Sprintf("%s (%d)", assetTypeName, assetTypeID))
	} else {
		view.AssetTypeValue.SetText(assetTypeName)
	}
	setLabelTextOrDash(view.FailureReasonValue, warningMessage)
	view.pendingAssetDeliveryJSON = assetDeliveryRawJSON
	view.pendingThumbnailJSON = thumbnailRawJSON
	view.pendingEconomyJSON = economyRawJSON
	view.pendingRustExtractorJSON = rustExtractorRawJSON
	view.pendingReferencedAssetIDs = append([]int64(nil), referencedAssetIDs...)
	view.assetDownloadBytes = append([]byte(nil), downloadBytes...)
	view.assetDownloadFileName = strings.TrimSpace(downloadFileName)
	view.downloadOriginalAsset = downloadIsOriginal && len(downloadBytes) > 0
	if len(referencedAssetIDs) > 0 {
		view.ReferencedAssetsCountValue.SetText(strconv.Itoa(len(referencedAssetIDs)))
	} else {
		view.ReferencedAssetsCountValue.SetText("0")
	}
	setLabelTextOrDash(view.ReferenceInstanceTypeValue, referenceInstanceType)
	setLabelTextOrDash(view.ReferencePropertyNameValue, referencePropertyName)
	setLabelTextOrDash(view.ReferenceInstancePathValue, referenceInstancePath)
	if view.isJSONAccordionOpen() {
		view.renderJSONDetails()
	} else {
		view.showLazyJSONPlaceholder()
	}
	if view.FileValue != nil {
		setLabelTextOrDash(view.FileValue, filePath)
	}
	if view.FileSHA256Value != nil {
		setLabelTextOrDash(view.FileSHA256Value, fileSHA256)
	}

	view.SourceValue.SetText(sourceDescription)
	view.SourceValue.Importance = widget.MediumImportance
	if useCount > 0 {
		view.UseCountValue.SetText(strconv.Itoa(useCount))
	} else {
		view.UseCountValue.SetText("-")
	}
	view.NoteLabel.Hide()
	view.NoteLabel.SetText("")

	isThumbnailFallbackSource := isThumbnailFallback(sourceDescription)
	thumbnailStateNotCompleted := isThumbnailFallbackSource && !isCompletedState(stateDescription)
	if isThumbnailFallbackSource {
		view.SourceValue.SetText(fmt.Sprintf("⚠ %s", sourceDescription))
		view.SourceValue.Importance = widget.DangerImportance
	}
	if warningMessage != "" {
		view.NoteLabel.SetText(buildFallbackWarningText(warningMessage))
		view.NoteLabel.Show()
	} else if thumbnailStateNotCompleted {
		view.NoteLabel.SetText(buildFallbackWarningText(fmt.Sprintf("thumbnail state was %s", stateDescription)))
		view.NoteLabel.Show()
	}
	view.SourceValue.Refresh()
	view.configureAudioPlayback(statsInfo, assetTypeID)

	var previewResource fyne.Resource
	if previewImageInfo != nil && previewImageInfo.Resource != nil {
		previewResource = previewImageInfo.Resource
	}
	if previewResource != nil {
		originalPreviewOption := buildOriginalPreviewOption(previewResource, view.currentAssetID)
		originalPreviewOption.labelText = formatPreviewOptionLabel(originalPreviewOption.labelText, len(originalPreviewOption.bytes), len(originalPreviewOption.bytes))
		view.previewDownloadOptions = []previewDownloadOption{originalPreviewOption}
		view.selectedPreviewOption = originalPreviewOption.labelText
		view.PreviewImage.File = ""
		view.PreviewImage.Image = nil
		view.PreviewImage.Resource = previewResource
		view.PreviewImage.Refresh()
		view.PreviewImage.Show()
		view.PreviewPlaceholder.Hide()
		view.downloadImageButton.Enable()
		view.expandImageButton.Enable()
		view.suppressPreviewVariant = true
		view.previewVariantSelect.SetOptions([]string{originalPreviewOption.labelText})
		view.previewVariantSelect.SetSelected(originalPreviewOption.labelText)
		view.suppressPreviewVariant = false
		view.previewVariantSelect.Disable()
		if !view.downloadOriginalAsset {
			go func(selectedAssetID int64, buildToken uint64, resource fyne.Resource) {
				downloadOptions, buildErr := buildPreviewDownloadOptions(resource, selectedAssetID)
				if buildErr != nil || len(downloadOptions) == 0 {
					return
				}
				fyne.Do(func() {
					if view.previewVariantBuildToken.Load() != buildToken || view.currentAssetID != selectedAssetID {
						return
					}
					view.previewDownloadOptions = downloadOptions
					optionLabels := make([]string, 0, len(downloadOptions))
					for _, option := range downloadOptions {
						optionLabels = append(optionLabels, option.labelText)
					}
					selectedLabel := view.selectedPreviewOption
					if !containsString(optionLabels, selectedLabel) {
						selectedLabel = optionLabels[0]
					}
					view.suppressPreviewVariant = true
					view.previewVariantSelect.SetOptions(optionLabels)
					view.previewVariantSelect.SetSelected(selectedLabel)
					view.suppressPreviewVariant = false
					view.selectedPreviewOption = selectedLabel
					if len(optionLabels) > 1 {
						view.previewVariantSelect.Enable()
					} else {
						view.previewVariantSelect.Disable()
					}
					view.applySelectedPreviewVariant()
				})
			}(view.currentAssetID, previewBuildToken, previewResource)
		}
	} else {
		view.PreviewImage.Hide()
		view.PreviewPlaceholder.SetText("No preview image available")
		view.PreviewPlaceholder.Show()
		if view.downloadOriginalAsset && len(view.assetDownloadBytes) > 0 {
			view.setOriginalOnlyPreviewVariant()
			view.downloadImageButton.Enable()
		} else {
			view.suppressPreviewVariant = true
			view.previewVariantSelect.ClearSelected()
			view.previewVariantSelect.SetOptions([]string{})
			view.suppressPreviewVariant = false
			view.previewVariantSelect.Disable()
			view.downloadImageButton.Disable()
		}
		view.expandImageButton.Disable()
	}
	view.PreviewContainer.Refresh()
}

func formatSizeAuto(bytesSize int) string {
	if bytesSize >= megabyte {
		return fmt.Sprintf("%.2f MB", float64(bytesSize)/megabyte)
	}
	if bytesSize >= 1024 {
		return fmt.Sprintf("%.2f KB", float64(bytesSize)/1024.0)
	}
	return fmt.Sprintf("%d bytes", bytesSize)
}
