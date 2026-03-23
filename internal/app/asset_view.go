package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	nativeDialog "github.com/sqweek/dialog"
	xdraw "golang.org/x/image/draw"
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

	DimensionsLabel            *widget.Label
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
		UseCountValue:              widget.NewLabel("-"),
		FailureReasonValue:         widget.NewLabel("-"),
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
	view.FailureReasonValue.Wrapping = fyne.TextWrapWord
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
		view.DimensionsLabel, view.DimensionsValue,
		widget.NewLabel("Self Size:"), view.SelfSizeValue,
		widget.NewLabel("Total Size:"), view.TotalSizeValue,
		widget.NewLabel("Format:"), view.FormatValue,
		widget.NewLabel("Content-Type:"), view.ContentTypeValue,
		widget.NewLabel("Asset Type:"), view.AssetTypeValue,
		widget.NewLabel("Referenced Assets:"), view.ReferencedAssetsCountValue,
		widget.NewLabel("State:"), view.StateValue,
		widget.NewLabel("Image Source:"), view.SourceValue,
		widget.NewLabel("Use Count:"), view.UseCountValue,
		widget.NewLabel("Failure Reason:"), view.FailureReasonValue,
	}
	if includeFileRow {
		view.FileValue = widget.NewLabel("-")
		view.FileValue.Wrapping = fyne.TextWrapWord
		view.FileSHA256Value = widget.NewLabel("-")
		view.FileSHA256Value.Wrapping = fyne.TextWrapWord
		formItems = append(
			formItems,
			widget.NewLabel("File:"), view.FileValue,
			widget.NewLabel("Downloaded SHA256:"), view.FileSHA256Value,
		)
	}
	view.MetadataForm = container.New(layout.NewFormLayout(), formItems...)

	view.Clear()
	return view
}

func newZoomPanImage(option previewDownloadOption) *zoomPanImage {
	viewer := &zoomPanImage{
		background: canvas.NewRectangle(color.Black),
		image:      canvas.NewImageFromResource(previewResourceForOption(option)),
		option:     option,
		zoom:       1.0,
		offsetX:    0,
		offsetY:    0,
	}
	viewer.image.FillMode = canvas.ImageFillStretch
	viewer.ExtendBaseWidget(viewer)
	return viewer
}

func (viewer *zoomPanImage) CreateRenderer() fyne.WidgetRenderer {
	content := container.NewWithoutLayout(viewer.background, viewer.image)
	return widget.NewSimpleRenderer(content)
}

func (viewer *zoomPanImage) MinSize() fyne.Size {
	return fyne.NewSize(240, 180)
}

func (viewer *zoomPanImage) Resize(size fyne.Size) {
	viewer.BaseWidget.Resize(size)
	viewer.updateLayout()
}

func (viewer *zoomPanImage) SetOption(option previewDownloadOption) {
	centerX, centerY := viewer.normalizedCenter()
	currentWidth, currentHeight := viewer.optionDimensions()
	nextWidth, nextHeight := previewOptionDimensions(option)
	viewer.option = option
	viewer.image.Resource = previewResourceForOption(option)
	if currentWidth > 0 && nextWidth > 0 {
		widthScale := float64(currentWidth) / float64(nextWidth)
		heightScale := 1.0
		if currentHeight > 0 && nextHeight > 0 {
			heightScale = float64(currentHeight) / float64(nextHeight)
		}
		viewer.zoom *= (widthScale + heightScale) / 2.0
		if viewer.zoom < 0.25 {
			viewer.zoom = 0.25
		}
		if viewer.zoom > 8.0 {
			viewer.zoom = 8.0
		}
	}
	viewer.setNormalizedCenter(centerX, centerY)
	viewer.updateLayout()
	viewer.image.Refresh()
}

func (viewer *zoomPanImage) SetZoom(nextZoom float64) {
	centerX, centerY := viewer.normalizedCenter()
	if nextZoom < 0.25 {
		nextZoom = 0.25
	}
	if nextZoom > 8.0 {
		nextZoom = 8.0
	}
	viewer.zoom = nextZoom
	viewer.setNormalizedCenter(centerX, centerY)
	viewer.updateLayout()
}

func (viewer *zoomPanImage) Dragged(event *fyne.DragEvent) {
	viewer.offsetX += event.Dragged.DX
	viewer.offsetY += event.Dragged.DY
	viewer.updateLayout()
}

func (viewer *zoomPanImage) DragEnd() {}

func (viewer *zoomPanImage) Scrolled(event *fyne.ScrollEvent) {
	if event == nil {
		return
	}
	if event.Scrolled.DY > 0 {
		viewer.SetZoom(viewer.zoom * 1.1)
		return
	}
	if event.Scrolled.DY < 0 {
		viewer.SetZoom(viewer.zoom / 1.1)
	}
}

func (viewer *zoomPanImage) updateLayout() {
	size := viewer.Size()
	viewer.background.Resize(size)
	viewer.background.Move(fyne.NewPos(0, 0))

	imageWidth := float32(viewer.option.width)
	imageHeight := float32(viewer.option.height)
	if imageWidth <= 0 {
		imageWidth = float32(previewWidth)
	}
	if imageHeight <= 0 {
		imageHeight = float32(previewHeight)
	}
	scaledWidth := imageWidth * float32(viewer.zoom)
	scaledHeight := imageHeight * float32(viewer.zoom)
	baseX := (size.Width - scaledWidth) / 2
	baseY := (size.Height - scaledHeight) / 2
	positionX := clampFloat32(baseX+viewer.offsetX, minFloat32(size.Width-scaledWidth, baseX), maxFloat32(0, baseX))
	positionY := clampFloat32(baseY+viewer.offsetY, minFloat32(size.Height-scaledHeight, baseY), maxFloat32(0, baseY))

	if scaledWidth <= size.Width {
		positionX = baseX
		viewer.offsetX = 0
	} else {
		viewer.offsetX = positionX - baseX
	}
	if scaledHeight <= size.Height {
		positionY = baseY
		viewer.offsetY = 0
	} else {
		viewer.offsetY = positionY - baseY
	}

	viewer.image.Resize(fyne.NewSize(scaledWidth, scaledHeight))
	viewer.image.Move(fyne.NewPos(positionX, positionY))
	canvas.Refresh(viewer)
}

func (viewer *zoomPanImage) normalizedCenter() (float32, float32) {
	size := viewer.Size()
	scaledWidth, scaledHeight := viewer.scaledDimensions()
	if scaledWidth <= 0 || scaledHeight <= 0 {
		return 0.5, 0.5
	}
	positionX, positionY, _, _ := viewer.layoutMetrics()
	centerX := (size.Width/2 - positionX) / scaledWidth
	centerY := (size.Height/2 - positionY) / scaledHeight
	return clampFloat32(centerX, 0, 1), clampFloat32(centerY, 0, 1)
}

func (viewer *zoomPanImage) setNormalizedCenter(centerX float32, centerY float32) {
	size := viewer.Size()
	scaledWidth, scaledHeight := viewer.scaledDimensions()
	baseX := (size.Width - scaledWidth) / 2
	baseY := (size.Height - scaledHeight) / 2
	desiredPositionX := size.Width/2 - clampFloat32(centerX, 0, 1)*scaledWidth
	desiredPositionY := size.Height/2 - clampFloat32(centerY, 0, 1)*scaledHeight
	viewer.offsetX = desiredPositionX - baseX
	viewer.offsetY = desiredPositionY - baseY
}

func (viewer *zoomPanImage) scaledDimensions() (float32, float32) {
	imageWidth, imageHeight := viewer.optionDimensions()
	return imageWidth * float32(viewer.zoom), imageHeight * float32(viewer.zoom)
}

func (viewer *zoomPanImage) layoutMetrics() (float32, float32, float32, float32) {
	size := viewer.Size()
	scaledWidth, scaledHeight := viewer.scaledDimensions()
	baseX := (size.Width - scaledWidth) / 2
	baseY := (size.Height - scaledHeight) / 2
	positionX := clampFloat32(baseX+viewer.offsetX, minFloat32(size.Width-scaledWidth, baseX), maxFloat32(0, baseX))
	positionY := clampFloat32(baseY+viewer.offsetY, minFloat32(size.Height-scaledHeight, baseY), maxFloat32(0, baseY))
	return positionX, positionY, scaledWidth, scaledHeight
}

func (viewer *zoomPanImage) optionDimensions() (float32, float32) {
	return previewOptionDimensions(viewer.option)
}

func previewOptionDimensions(option previewDownloadOption) (float32, float32) {
	imageWidth := float32(option.width)
	imageHeight := float32(option.height)
	if imageWidth <= 0 {
		imageWidth = float32(previewWidth)
	}
	if imageHeight <= 0 {
		imageHeight = float32(previewHeight)
	}
	return imageWidth, imageHeight
}

func (view *assetView) Clear() {
	view.currentAssetID = 0
	view.audioLoadToken.Add(1)
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
	view.StateValue.SetText("-")
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
	view.StateValue.Importance = widget.MediumImportance
	view.SourceValue.Importance = widget.MediumImportance
	view.StateValue.Refresh()
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

func (view *assetView) SetData(assetID int64, filePath string, fileSHA256 string, useCount int, previewImageInfo *imageInfo, statsInfo *imageInfo, totalBytesSize int, sourceDescription string, stateDescription string, warningMessage string, assetDeliveryRawJSON string, thumbnailRawJSON string, economyRawJSON string, rustExtractorRawJSON string, referencedAssetIDs []int64, assetTypeID int, assetTypeName string, downloadBytes []byte, downloadFileName string, downloadIsOriginal bool) {
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

	view.StateValue.SetText(stateDescription)
	view.StateValue.Importance = widget.MediumImportance
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
	view.configureAudioPlayback(statsInfo, assetTypeID)

	var previewResource fyne.Resource
	if previewImageInfo != nil && previewImageInfo.Resource != nil {
		previewResource = previewImageInfo.Resource
	}
	if previewResource != nil {
		downloadOptions, buildErr := buildPreviewDownloadOptions(previewResource, view.currentAssetID)
		if buildErr != nil {
			downloadOptions = []previewDownloadOption{}
		}
		view.previewDownloadOptions = downloadOptions
		if len(downloadOptions) > 0 {
			optionLabels := make([]string, 0, len(downloadOptions))
			for _, option := range downloadOptions {
				optionLabels = append(optionLabels, option.labelText)
			}
			selectedLabel := optionLabels[0]
			enableVariantSelect := true
			if view.downloadOriginalAsset {
				optionLabels = []string{"Original"}
				selectedLabel = "Original"
				enableVariantSelect = false
			}
			view.suppressPreviewVariant = true
			view.previewVariantSelect.SetOptions(optionLabels)
			view.previewVariantSelect.SetSelected(selectedLabel)
			view.suppressPreviewVariant = false
			view.selectedPreviewOption = selectedLabel
			if enableVariantSelect {
				view.previewVariantSelect.Enable()
			} else {
				view.previewVariantSelect.Disable()
			}
			view.applySelectedPreviewVariant()
		} else {
			view.PreviewImage.File = ""
			view.PreviewImage.Image = nil
			view.PreviewImage.Resource = previewResource
			view.PreviewImage.Refresh()
			view.PreviewImage.Show()
			view.PreviewPlaceholder.Hide()
			view.previewVariantSelect.Disable()
			view.downloadImageButton.Enable()
			view.expandImageButton.Enable()
			view.PreviewContainer.Refresh()
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

func (view *assetView) configureAudioPlayback(statsInfo *imageInfo, assetTypeID int) {
	if view.audioPlayer == nil {
		return
	}
	loadToken := view.audioLoadToken.Add(1)
	view.audioPlayer.Reset()
	view.resetAudioControls()
	view.audioControls.Hide()
	if statsInfo == nil || !isAudioAssetContent(assetTypeID, statsInfo.ContentType) {
		return
	}
	view.audioControls.Show()
	if len(view.assetDownloadBytes) == 0 {
		view.updateAudioPlaybackState(audioPlayerStatus{
			Available: false,
			Message:   "No audio bytes are available for playback.",
		})
		return
	}
	view.updateAudioPlaybackState(audioPlayerStatus{
		Available: false,
		Duration:  statsInfo.Duration,
		Volume:    defaultAudioVolume,
		Message:   "Loading audio...",
	})
	fileName := view.assetDownloadFileName
	contentType := statsInfo.ContentType
	audioBytes := append([]byte(nil), view.assetDownloadBytes...)
	go func(expectedLoadToken uint64, currentAssetID int64) {
		decodedAudio, decodeErr := decodeAudioBuffer(fileName, contentType, audioBytes)
		fyne.Do(func() {
			if view.audioLoadToken.Load() != expectedLoadToken || view.currentAssetID != currentAssetID {
				return
			}
			if decodeErr != nil {
				view.updateAudioPlaybackState(audioPlayerStatus{
					Available: false,
					Duration:  statsInfo.Duration,
					Volume:    defaultAudioVolume,
					Message:   fmt.Sprintf("Playback unavailable: %s", decodeErr.Error()),
				})
				return
			}
			if loadErr := view.audioPlayer.LoadDecoded(decodedAudio); loadErr != nil {
				view.updateAudioPlaybackState(audioPlayerStatus{
					Available: false,
					Duration:  statsInfo.Duration,
					Volume:    defaultAudioVolume,
					Message:   fmt.Sprintf("Playback unavailable: %s", loadErr.Error()),
				})
			}
		})
	}(loadToken, view.currentAssetID)
}

func (view *assetView) updateAudioPlaybackState(status audioPlayerStatus) {
	apply := func() {
		if view.playAudioButton == nil || view.stopAudioButton == nil || view.audioProgressSlider == nil || view.audioVolumeSlider == nil {
			return
		}
		if status.Playing && !status.Paused {
			view.playAudioButton.Text = "Pause"
			view.playAudioButton.Icon = theme.MediaPauseIcon()
		} else {
			view.playAudioButton.Text = "Play"
			view.playAudioButton.Icon = theme.MediaPlayIcon()
		}
		if status.Available {
			view.playAudioButton.Enable()
			view.audioProgressSlider.Enable()
			view.audioVolumeSlider.Enable()
			if status.Playing || status.Paused || status.Position > 0 {
				view.stopAudioButton.Enable()
			} else {
				view.stopAudioButton.Disable()
			}
		} else {
			view.playAudioButton.Disable()
			view.stopAudioButton.Disable()
			view.audioProgressSlider.Disable()
			view.audioVolumeSlider.Disable()
		}
		view.audioDuration = status.Duration
		if !view.audioSeekDragging {
			view.audioCurrentTimeLabel.SetText(formatDurationCompact(status.Position))
		}
		view.audioTotalTimeLabel.SetText(formatDurationCompact(status.Duration))
		if !view.audioSeekDragging {
			view.suppressAudioSeekChange = true
			if status.Duration > 0 {
				view.audioProgressSlider.SetValue(clampAudioSliderValue(float64(status.Position) / float64(status.Duration)))
			} else {
				view.audioProgressSlider.SetValue(0)
			}
			view.suppressAudioSeekChange = false
		}
		view.suppressAudioVolumeChange = true
		view.audioVolumeSlider.SetValue(clampAudioSliderValue(status.Volume))
		view.suppressAudioVolumeChange = false
		view.audioVolumeValueLabel.SetText(fmt.Sprintf("%d%%", int(clampAudioSliderValue(status.Volume)*100)))
		view.playAudioButton.Refresh()
		view.stopAudioButton.Refresh()
		if view.audioControls != nil {
			view.audioControls.Refresh()
		}
	}
	if fyne.CurrentApp() == nil {
		apply()
		return
	}
	fyne.Do(apply)
}

func (view *assetView) resetAudioControls() {
	if view.audioCurrentTimeLabel != nil {
		view.audioCurrentTimeLabel.SetText("0:00")
	}
	if view.audioTotalTimeLabel != nil {
		view.audioTotalTimeLabel.SetText("0:00")
	}
	view.audioDuration = 0
	if view.audioProgressSlider != nil {
		view.suppressAudioSeekChange = true
		view.audioSeekDragging = false
		view.audioProgressSlider.SetValue(0)
		view.suppressAudioSeekChange = false
		view.audioProgressSlider.Disable()
	}
	if view.audioVolumeSlider != nil {
		view.suppressAudioVolumeChange = true
		view.audioVolumeSlider.SetValue(defaultAudioVolume)
		view.suppressAudioVolumeChange = false
		view.audioVolumeSlider.Disable()
	}
	if view.audioVolumeValueLabel != nil {
		view.audioVolumeValueLabel.SetText("40%")
	}
}

func (view *assetView) setOriginalOnlyPreviewVariant() {
	view.suppressPreviewVariant = true
	view.previewVariantSelect.SetOptions([]string{"Original"})
	view.previewVariantSelect.SetSelected("Original")
	view.suppressPreviewVariant = false
	view.selectedPreviewOption = "Original"
	view.previewVariantSelect.Disable()
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

func (view *assetView) saveSelectedPreviewVariantToFile() {
	window := getPrimaryWindow()
	if window == nil {
		return
	}
	if view.downloadOriginalAsset && len(view.assetDownloadBytes) > 0 {
		downloadFileName := view.assetDownloadFileName
		if downloadFileName == "" {
			downloadFileName = fmt.Sprintf("asset_%d.bin", view.currentAssetID)
		}
		view.saveRawAssetToFile(downloadFileName, view.assetDownloadBytes)
		return
	}
	selectedOption, found := view.selectedPreviewDownloadOption()
	if !found {
		dialog.ShowInformation("Download Preview", "No preview image is available to save.", window)
		return
	}
	view.savePreviewVariantToFile(selectedOption)
}

func (view *assetView) saveRawAssetToFile(fileName string, fileBytes []byte) {
	window := getPrimaryWindow()
	if window == nil {
		return
	}
	if len(fileBytes) == 0 {
		dialog.ShowInformation("Save Asset", "No original asset bytes are available to save.", window)
		return
	}
	saved, saveErr := saveBytesWithNativeDialog("Save Asset", fileName, fileBytes)
	if saveErr != nil {
		dialog.ShowError(saveErr, window)
		return
	}
	if !saved {
		return
	}
	logDebugf("Saved original asset file for asset %d (%s)", view.currentAssetID, fileName)
}

func (view *assetView) showExpandedImageWindow() {
	selectedOption, found := view.selectedPreviewDownloadOption()
	if !found {
		return
	}

	guiApp := fyne.CurrentApp()
	if guiApp == nil {
		return
	}

	imageWindow := guiApp.NewWindow(fmt.Sprintf("Asset %d", view.currentAssetID))
	imageViewer := newZoomPanImage(selectedOption)
	currentOption := selectedOption

	applyExpandedPreviewState := func() {
		imageViewer.SetOption(currentOption)
	}

	variantLabels := make([]string, 0, len(view.previewDownloadOptions))
	for _, option := range view.previewDownloadOptions {
		variantLabels = append(variantLabels, option.labelText)
	}
	selectedVariantLabel := selectedOption.labelText
	variantSelect := widget.NewSelect(variantLabels, func(selectedLabel string) {
		for _, option := range view.previewDownloadOptions {
			if option.labelText != selectedLabel {
				continue
			}
			currentOption = option
			applyExpandedPreviewState()
			return
		}
	})
	if view.downloadOriginalAsset {
		variantLabels = []string{"Original"}
		selectedVariantLabel = "Original"
		variantSelect.SetOptions(variantLabels)
		variantSelect.Disable()
	}
	variantSelect.SetSelected(selectedVariantLabel)
	applyExpandedPreviewState()

	topBar := container.NewHBox(
		widget.NewLabel("Preview Version:"),
		container.NewGridWrap(fyne.NewSize(280, 36), variantSelect),
	)
	imageWindow.SetContent(container.NewBorder(topBar, nil, nil, nil, container.NewPadded(imageViewer)))
	imageWindow.Resize(fyne.NewSize(980, 760))
	imageWindow.Show()
}

func (view *assetView) savePreviewVariantToFile(option previewDownloadOption) {
	window := getPrimaryWindow()
	if window == nil {
		return
	}
	if len(option.bytes) == 0 {
		dialog.ShowInformation("Save Preview", "The selected preview variant has no image bytes to save.", window)
		return
	}
	saved, saveErr := saveBytesWithNativeDialog("Save Preview", option.fileName, option.bytes)
	if saveErr != nil {
		dialog.ShowError(saveErr, window)
		return
	}
	if !saved {
		return
	}
	logDebugf("Saved preview image for asset %d (%s)", view.currentAssetID, option.fileName)
}

func buildPreviewDownloadOptions(resource fyne.Resource, assetID int64) ([]previewDownloadOption, error) {
	if resource == nil {
		return nil, nil
	}
	resourceName := resource.Name()
	if strings.TrimSpace(resourceName) == "" {
		resourceName = fmt.Sprintf("asset_%d.png", assetID)
	}
	resourceBytes := resource.Content()
	if len(resourceBytes) == 0 {
		return nil, nil
	}

	decodedImage, imageFormat, decodeErr := image.Decode(bytes.NewReader(resourceBytes))
	if decodeErr != nil {
		return []previewDownloadOption{
			{
				labelText: fmt.Sprintf("Original (%s)", formatSizeInMB(len(resourceBytes))),
				fileName:  resourceName,
				bytes:     append([]byte(nil), resourceBytes...),
			},
		}, nil
	}
	baseBounds := decodedImage.Bounds()
	options := []previewDownloadOption{
		{
			labelText: fmt.Sprintf(
				"Original (%dx%d, %s)",
				baseBounds.Dx(),
				baseBounds.Dy(),
				formatSizeInMB(len(resourceBytes)),
			),
			fileName: resourceName,
			bytes:    append([]byte(nil), resourceBytes...),
			width:    baseBounds.Dx(),
			height:   baseBounds.Dy(),
		},
	}
	if baseBounds.Dx() <= 1 || baseBounds.Dy() <= 1 {
		return options, nil
	}
	baseExtension := previewOutputExtension(resourceName, imageFormat)
	threeQuarterBytes, threeQuarterErr := encodeScaledPreview(decodedImage, imageFormat, 0.75)
	if threeQuarterErr == nil {
		threeQuarterWidth := maxInt(1, int(float64(baseBounds.Dx())*0.75))
		threeQuarterHeight := maxInt(1, int(float64(baseBounds.Dy())*0.75))
		options = append(options, previewDownloadOption{
			labelText: fmt.Sprintf(
				"Three Quarters (%dx%d, %s)",
				threeQuarterWidth,
				threeQuarterHeight,
				formatSizeInMB(len(threeQuarterBytes)),
			),
			fileName: previewVariantFileName(resourceName, "three_quarters", baseExtension),
			bytes:    threeQuarterBytes,
			width:    threeQuarterWidth,
			height:   threeQuarterHeight,
		})
	}
	halfBytes, halfErr := encodeResizedPreview(decodedImage, imageFormat, 2)
	if halfErr == nil {
		halfWidth := maxInt(1, baseBounds.Dx()/2)
		halfHeight := maxInt(1, baseBounds.Dy()/2)
		options = append(options, previewDownloadOption{
			labelText: fmt.Sprintf("Half (%dx%d, %s)", halfWidth, halfHeight, formatSizeInMB(len(halfBytes))),
			fileName:  previewVariantFileName(resourceName, "half", baseExtension),
			bytes:     halfBytes,
			width:     halfWidth,
			height:    halfHeight,
		})
	}
	thirdBytes, thirdErr := encodeScaledPreview(decodedImage, imageFormat, 1.0/3.0)
	if thirdErr == nil {
		thirdWidth := maxInt(1, int(float64(baseBounds.Dx())/3.0))
		thirdHeight := maxInt(1, int(float64(baseBounds.Dy())/3.0))
		options = append(options, previewDownloadOption{
			labelText: fmt.Sprintf("Third (%dx%d, %s)", thirdWidth, thirdHeight, formatSizeInMB(len(thirdBytes))),
			fileName:  previewVariantFileName(resourceName, "third", baseExtension),
			bytes:     thirdBytes,
			width:     thirdWidth,
			height:    thirdHeight,
		})
	}
	quarterBytes, quarterErr := encodeResizedPreview(decodedImage, imageFormat, 4)
	if quarterErr == nil {
		quarterWidth := maxInt(1, baseBounds.Dx()/4)
		quarterHeight := maxInt(1, baseBounds.Dy()/4)
		options = append(options, previewDownloadOption{
			labelText: fmt.Sprintf(
				"Quarter (%dx%d, %s)",
				quarterWidth,
				quarterHeight,
				formatSizeInMB(len(quarterBytes)),
			),
			fileName: previewVariantFileName(resourceName, "quarter", baseExtension),
			bytes:    quarterBytes,
			width:    quarterWidth,
			height:   quarterHeight,
		})
	}
	return options, nil
}

func (view *assetView) selectedPreviewDownloadOption() (previewDownloadOption, bool) {
	selectedLabel := strings.TrimSpace(view.selectedPreviewOption)
	for _, option := range view.previewDownloadOptions {
		if option.labelText == selectedLabel {
			return option, true
		}
	}
	if len(view.previewDownloadOptions) > 0 {
		return view.previewDownloadOptions[0], true
	}
	return previewDownloadOption{}, false
}

func (view *assetView) applySelectedPreviewVariant() {
	selectedOption, found := view.selectedPreviewDownloadOption()
	if !found {
		view.PreviewImage.File = ""
		view.PreviewImage.Image = nil
		view.PreviewImage.Resource = nil
		view.PreviewImage.Refresh()
		view.PreviewImage.Hide()
		view.PreviewPlaceholder.SetText("No preview image available")
		view.PreviewPlaceholder.Show()
		view.expandImageButton.Disable()
		view.downloadImageButton.Disable()
		view.PreviewContainer.Refresh()
		return
	}

	view.PreviewImage.File = ""
	view.PreviewImage.Image = nil
	view.PreviewImage.Resource = fyne.NewStaticResource(selectedOption.fileName, selectedOption.bytes)
	view.PreviewImage.Refresh()
	view.PreviewImage.Show()
	view.PreviewPlaceholder.Hide()
	view.expandImageButton.Enable()
	view.downloadImageButton.Enable()
	view.PreviewContainer.Refresh()
}

func previewResourceForOption(option previewDownloadOption) fyne.Resource {
	return fyne.NewStaticResource(option.fileName, option.bytes)
}

func saveBytesWithNativeDialog(title string, fileName string, fileBytes []byte) (bool, error) {
	if len(fileBytes) == 0 {
		return false, fmt.Errorf("no file bytes available to save")
	}
	trimmedFileName := strings.TrimSpace(fileName)
	if trimmedFileName == "" {
		trimmedFileName = "download.bin"
	}
	fileExtension := strings.TrimPrefix(previewFileExtension(trimmedFileName), ".")
	selectedPath, pickerErr := nativeDialog.File().
		Title(title).
		Filter(strings.ToUpper(fileExtension)+" files", fileExtension).
		SetStartFile(trimmedFileName).
		Save()
	if pickerErr != nil {
		if errors.Is(pickerErr, nativeDialog.ErrCancelled) {
			return false, nil
		}
		return false, pickerErr
	}
	if strings.TrimSpace(selectedPath) == "" {
		return false, nil
	}
	if writeErr := os.WriteFile(selectedPath, fileBytes, 0644); writeErr != nil {
		return false, writeErr
	}
	return true, nil
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

func previewFileExtension(fileName string) string {
	extension := strings.TrimSpace(strings.ToLower(filepath.Ext(fileName)))
	if extension == "" {
		return ".png"
	}
	return extension
}

func previewOutputExtension(fileName string, imageFormat string) string {
	currentExtension := previewFileExtension(fileName)
	if currentExtension != ".png" && currentExtension != ".jpg" && currentExtension != ".jpeg" {
		switch strings.ToLower(strings.TrimSpace(imageFormat)) {
		case "jpeg":
			return ".jpg"
		default:
			return ".png"
		}
	}
	return currentExtension
}

func previewVariantFileName(baseFileName string, variantLabel string, extension string) string {
	baseName := strings.TrimSuffix(baseFileName, filepath.Ext(baseFileName))
	if strings.TrimSpace(baseName) == "" {
		baseName = "asset_preview"
	}
	if strings.TrimSpace(extension) == "" {
		extension = ".png"
	}
	return fmt.Sprintf("%s_%s%s", baseName, variantLabel, extension)
}

func encodeResizedPreview(sourceImage image.Image, imageFormat string, divisor int) ([]byte, error) {
	if sourceImage == nil {
		return nil, fmt.Errorf("preview image is unavailable")
	}
	if divisor <= 1 {
		return nil, fmt.Errorf("resize divisor must be greater than 1")
	}
	sourceBounds := sourceImage.Bounds()
	targetWidth := maxInt(1, sourceBounds.Dx()/divisor)
	targetHeight := maxInt(1, sourceBounds.Dy()/divisor)
	targetImage := image.NewRGBA(image.Rect(0, 0, targetWidth, targetHeight))
	xdraw.CatmullRom.Scale(targetImage, targetImage.Bounds(), sourceImage, sourceBounds, xdraw.Over, nil)

	var outputBuffer bytes.Buffer
	switch strings.ToLower(strings.TrimSpace(imageFormat)) {
	case "jpeg", "jpg":
		if encodeErr := jpeg.Encode(&outputBuffer, targetImage, &jpeg.Options{Quality: 95}); encodeErr != nil {
			return nil, encodeErr
		}
	default:
		encoder := png.Encoder{CompressionLevel: png.BestCompression}
		if encodeErr := encoder.Encode(&outputBuffer, targetImage); encodeErr != nil {
			return nil, encodeErr
		}
	}
	return outputBuffer.Bytes(), nil
}

func encodeScaledPreview(sourceImage image.Image, imageFormat string, scale float64) ([]byte, error) {
	if sourceImage == nil {
		return nil, fmt.Errorf("preview image is unavailable")
	}
	if scale <= 0 {
		return nil, fmt.Errorf("preview scale must be positive")
	}
	sourceBounds := sourceImage.Bounds()
	targetWidth := maxInt(1, int(float64(sourceBounds.Dx())*scale))
	targetHeight := maxInt(1, int(float64(sourceBounds.Dy())*scale))
	targetImage := image.NewRGBA(image.Rect(0, 0, targetWidth, targetHeight))
	xdraw.CatmullRom.Scale(targetImage, targetImage.Bounds(), sourceImage, sourceBounds, xdraw.Over, nil)

	var outputBuffer bytes.Buffer
	switch strings.ToLower(strings.TrimSpace(imageFormat)) {
	case "jpeg", "jpg":
		if encodeErr := jpeg.Encode(&outputBuffer, targetImage, &jpeg.Options{Quality: 95}); encodeErr != nil {
			return nil, encodeErr
		}
	default:
		encoder := png.Encoder{CompressionLevel: png.BestCompression}
		if encodeErr := encoder.Encode(&outputBuffer, targetImage); encodeErr != nil {
			return nil, encodeErr
		}
	}
	return outputBuffer.Bytes(), nil
}

func formatSizeInMB(byteCount int) string {
	return fmt.Sprintf("%.2f MB", float64(byteCount)/float64(megabyte))
}

func maxInt(left int, right int) int {
	if left > right {
		return left
	}
	return right
}

func minFloat32(left float32, right float32) float32 {
	if left < right {
		return left
	}
	return right
}

func maxFloat32(left float32, right float32) float32 {
	if left > right {
		return left
	}
	return right
}

func clampFloat32(value float32, minimum float32, maximum float32) float32 {
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}
	return value
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
	view.selectedHierarchyAssetID = selectedAssetID
	hierarchyItems := make([]fyne.CanvasObject, 0, len(rows))
	for _, row := range rows {
		rowCopy := row
		sizeText := "size unavailable"
		if row.SelfBytesSize > 0 {
			sizeText = formatSizeAuto(row.SelfBytesSize)
		}
		nodeIcon := getAssetTypeEmoji(row.AssetTypeID)
		rowText := fmt.Sprintf("%s %d (%s)", nodeIcon, row.AssetID, sizeText)
		rowButton := widget.NewButton(rowText, func() {
			if view.hierarchySelectAsset != nil {
				view.hierarchySelectAsset(rowCopy.AssetID)
			}
		})
		rowButton.Importance = widget.LowImportance
		if row.AssetID == selectedAssetID {
			rowButton.Importance = widget.HighImportance
		}
		indentSpacer := canvas.NewRectangle(color.Transparent)
		indentSpacer.SetMinSize(fyne.NewSize(float32(row.Depth*24), 1))
		hierarchyItems = append(hierarchyItems, container.NewHBox(indentSpacer, rowButton))
	}
	view.hierarchyList.Objects = hierarchyItems
	view.hierarchyList.Refresh()
}
