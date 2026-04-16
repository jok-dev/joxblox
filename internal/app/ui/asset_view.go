package ui

import (
	"fmt"
	"image/color"
	"strings"
	"sync/atomic"
	"time"

	"joxblox/internal/app/loader"
	"joxblox/internal/debug"
	"joxblox/internal/format"
	"joxblox/internal/roblox"
	"joxblox/internal/roblox/mesh"

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

type AssetView struct {
	PreviewImage       *canvas.Image
	MeshPreview        *MeshPreviewWidget
	PreviewPlaceholder *widget.Label
	PreviewContainer   fyne.CanvasObject
	PreviewBox         fyne.CanvasObject
	HierarchySection   fyne.CanvasObject

	metadataRows map[string]*metadataRow

	AssetDeliveryJSONValue  *widget.Entry
	ThumbnailJSONValue      *widget.Entry
	EconomyJSONValue        *widget.Entry
	RustyAssetToolJSONValue *widget.Entry
	ReferencedAssetsValue   *widget.Entry
	JSONAccordion           *widget.Accordion
	NoteLabel               *widget.Label
	MetadataForm            fyne.CanvasObject

	expandImageButton         *widget.Button
	downloadImageButton       *widget.Button
	uploadImageButton         *widget.Button
	previewVariantSelect      *widget.Select
	playAudioButton           *widget.Button
	stopAudioButton           *widget.Button
	audioProgressSlider       *widget.Slider
	audioCurrentTimeLabel     *widget.Label
	audioTotalTimeLabel       *widget.Label
	audioVolumeSlider         *widget.Slider
	audioVolumeValueLabel     *widget.Label
	audioControls             *fyne.Container
	audioPlayer               *AssetAudioPlayer
	audioDuration             time.Duration
	suppressAudioSeekChange   bool
	audioSeekDragging         bool
	suppressAudioVolumeChange bool
	audioLoadToken            atomic.Uint64
	previewVariantBuildToken  atomic.Uint64
	currentAssetID            int64
	hierarchyRows             []AssetExplorerRow
	hierarchyList             *fyne.Container
	hierarchySelectAsset      func(int64)
	selectedHierarchyAssetID  int64
	pendingAssetDeliveryJSON  string
	pendingThumbnailJSON      string
	pendingEconomyJSON        string
	pendingRustyAssetToolJSON string
	pendingReferencedAssetIDs []int64
	lastJSONAccordionOpen     bool
	previewDownloadOptions    []PreviewDownloadOption
	selectedPreviewOption     string
	suppressPreviewVariant    bool
	assetDownloadBytes        []byte
	assetDownloadFileName     string
	downloadOriginalAsset     bool
	interpolationSelect       *widget.Select
	currentPreviewResource    fyne.Resource
	meshPreviewLoadToken      atomic.Uint64
	currentMeshPreviewData    MeshPreviewData
}

type assetJSONExport struct {
	AssetID            int64   `json:"asset_id"`
	ExportedAtUTC      string  `json:"exported_at_utc"`
	AssetDeliveryJSON  any     `json:"asset_delivery_json"`
	ThumbnailJSON      any     `json:"thumbnail_json"`
	EconomyJSON        any     `json:"economy_json"`
	RustyAssetToolJSON any     `json:"rust_extractor_json"`
	ReferencedAssetIDs []int64 `json:"referenced_asset_ids"`
}

type PreviewDownloadOption struct {
	LabelText string
	FileName  string
	Bytes     []byte
	Width     int
	Height    int
}

type ZoomPanImage struct {
	widget.BaseWidget
	background    *canvas.Rectangle
	image         *canvas.Image
	Option        PreviewDownloadOption
	zoom          float64
	offsetX       float32
	offsetY       float32
	hoverCallback func(imageX float64, imageY float64, pointer fyne.Position, inside bool)
	tapCallback   func(imageX float64, imageY float64, pointer fyne.Position, inside bool)
}

func NewAssetView(placeholderText string, includeFileRow bool) *AssetView {
	previewImage := canvas.NewImageFromImage(nil)
	previewImage.FillMode = canvas.ImageFillContain
	previewImage.ScaleMode = canvas.ImageScaleFastest
	previewImage.SetMinSize(fyne.NewSize(PreviewWidth, PreviewHeight))
	meshPreview := NewMeshPreviewWidget()
	meshPreview.Hide()
	previewPlaceholder := widget.NewLabel(placeholderText)
	previewContainer := container.NewMax(
		container.NewCenter(previewPlaceholder),
		container.NewCenter(previewImage),
		container.NewCenter(meshPreview),
	)

	assetDeliveryJSONValue := newReadOnlyMultilineEntry(6)
	thumbnailJSONValue := newReadOnlyMultilineEntry(6)
	economyJSONValue := newReadOnlyMultilineEntry(6)
	referencedAssetsValue := newReadOnlyMultilineEntry(6)
	rustyAssetToolJSONValue := newReadOnlyMultilineEntry(6)
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
				widget.NewLabel("Rusty Asset Tool JSON:"),
				rustyAssetToolJSONValue,
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
	audioVolumeSlider.SetValue(DefaultAudioVolume)
	audioVolumeSlider.Disable()

	view := &AssetView{
		PreviewImage:               previewImage,
		MeshPreview:                meshPreview,
		PreviewPlaceholder:         previewPlaceholder,
		PreviewContainer:           previewContainer,
		PreviewBox:                 nil,
		HierarchySection:           nil,
		AssetDeliveryJSONValue:     assetDeliveryJSONValue,
		ThumbnailJSONValue:         thumbnailJSONValue,
		EconomyJSONValue:           economyJSONValue,
		RustyAssetToolJSONValue:    rustyAssetToolJSONValue,
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
		hierarchyRows:              []AssetExplorerRow{},
		hierarchyList:              container.NewVBox(),
		hierarchySelectAsset:       nil,
		selectedHierarchyAssetID:   0,
		pendingAssetDeliveryJSON:   "",
		pendingThumbnailJSON:       "",
		pendingEconomyJSON:         "",
		pendingRustyAssetToolJSON:  "",
		pendingReferencedAssetIDs:  []int64{},
		previewDownloadOptions:     []PreviewDownloadOption{},
		selectedPreviewOption:      "",
		suppressPreviewVariant:     false,
		assetDownloadBytes:         []byte{},
		assetDownloadFileName:      "",
		downloadOriginalAsset:      false,
		currentMeshPreviewData:     MeshPreviewData{},
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
	view.uploadImageButton = widget.NewButtonWithIcon("", theme.UploadIcon(), func() {
		view.uploadSelectedPreviewVariant()
	})
	view.uploadImageButton.Disable()
	view.playAudioButton = widget.NewButtonWithIcon("Play", theme.MediaPlayIcon(), func() {
		if view.audioPlayer == nil {
			return
		}
		if err := view.audioPlayer.TogglePlayPause(); err != nil {
			view.updateAudioPlaybackState(AudioPlayerStatus{
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
		view.audioCurrentTimeLabel.SetText(FormatDurationCompact(time.Duration(value * float64(view.audioDuration))))
	}
	view.audioProgressSlider.OnChangeEnded = func(value float64) {
		if view.suppressAudioSeekChange || view.audioPlayer == nil {
			view.audioSeekDragging = false
			return
		}
		if err := view.audioPlayer.SeekToFraction(value); err != nil {
			view.audioSeekDragging = false
			view.updateAudioPlaybackState(AudioPlayerStatus{
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
		view.audioVolumeValueLabel.SetText(fmt.Sprintf("%d%%", int(format.Clamp(value, 0.0, 1.0)*100)))
		if view.audioPlayer == nil {
			return
		}
		if err := view.audioPlayer.SetVolume(value); err != nil {
			view.updateAudioPlaybackState(AudioPlayerStatus{
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
	view.audioPlayer = NewAssetAudioPlayer(view.updateAudioPlaybackState)
	view.previewVariantSelect = widget.NewSelect([]string{}, func(selectedLabel string) {
		if view.suppressPreviewVariant {
			return
		}
		view.selectedPreviewOption = selectedLabel
		view.applySelectedPreviewVariant()
	})
	view.previewVariantSelect.Disable()
	view.interpolationSelect = widget.NewSelect(SampleModeOptions, func(string) {
		view.rebuildPreviewVariants()
	})
	view.interpolationSelect.SetSelected(DefaultSampleMode)
	view.interpolationSelect.Disable()
	previewVariantControl := container.NewGridWrap(fyne.NewSize(240, 36), view.previewVariantSelect)
	interpolationControl := container.NewGridWrap(fyne.NewSize(160, 36), view.interpolationSelect)
	expandButtonRow := container.NewHBox(layout.NewSpacer(), previewVariantControl, interpolationControl, view.uploadImageButton, view.downloadImageButton, view.expandImageButton)
	previewBody := container.NewVBox(view.PreviewContainer, view.audioControls)
	view.PreviewBox = container.NewBorder(nil, expandButtonRow, nil, nil, previewBody)
	hierarchyMinHeight := canvas.NewRectangle(color.Transparent)
	hierarchyMinHeight.SetMinSize(fyne.NewSize(0, 140))
	hierarchyContent := container.NewMax(hierarchyMinHeight, view.hierarchyList)
	view.HierarchySection = container.NewVBox(
		widget.NewLabel("Asset Hierarchy"),
		hierarchyContent,
	)

	form, rows := buildMetadataForm(loader.AssetMetadataSchema(), includeFileRow)
	view.MetadataForm = form
	view.metadataRows = rows

	view.Clear()
	return view
}

func (view *AssetView) Clear() {
	view.currentAssetID = 0
	view.audioLoadToken.Add(1)
	view.previewVariantBuildToken.Add(1)
	view.PreviewImage.File = ""
	view.PreviewImage.Image = nil
	view.PreviewImage.Resource = nil
	view.PreviewImage.Refresh()
	view.PreviewImage.Hide()
	view.MeshPreview.Clear()
	view.MeshPreview.Hide()
	view.currentMeshPreviewData = MeshPreviewData{}
	view.PreviewPlaceholder.Show()
	view.PreviewContainer.Refresh()
	updateMetadataRows(view.metadataRows, loader.AssetViewData{})
	view.AssetDeliveryJSONValue.SetText("-")
	view.ThumbnailJSONValue.SetText("-")
	view.EconomyJSONValue.SetText("-")
	view.RustyAssetToolJSONValue.SetText("-")
	view.ReferencedAssetsValue.SetText("-")
	view.pendingAssetDeliveryJSON = ""
	view.pendingThumbnailJSON = ""
	view.pendingEconomyJSON = ""
	view.pendingRustyAssetToolJSON = ""
	view.pendingReferencedAssetIDs = []int64{}
	view.previewDownloadOptions = []PreviewDownloadOption{}
	view.selectedPreviewOption = ""
	view.assetDownloadBytes = []byte{}
	view.assetDownloadFileName = ""
	view.downloadOriginalAsset = false
	view.currentPreviewResource = nil
	view.currentMeshPreviewData = MeshPreviewData{}
	view.meshPreviewLoadToken.Add(1)
	view.interpolationSelect.Disable()
	view.selectedHierarchyAssetID = 0
	view.hierarchyRows = []AssetExplorerRow{}
	view.hierarchySelectAsset = nil
	view.hierarchyList.Objects = nil
	view.hierarchyList.Refresh()
	view.clearPreviewVariantSelect()
	view.expandImageButton.Disable()
	view.downloadImageButton.Disable()
	view.uploadImageButton.Disable()
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

func (view *AssetView) clearPreviewVariantSelect() {
	view.suppressPreviewVariant = true
	view.previewVariantSelect.ClearSelected()
	view.previewVariantSelect.SetOptions([]string{})
	view.suppressPreviewVariant = false
	view.previewVariantSelect.Disable()
}

func (view *AssetView) setSinglePreviewVariant(label string) {
	view.suppressPreviewVariant = true
	view.previewVariantSelect.SetOptions([]string{label})
	view.previewVariantSelect.SetSelected(label)
	view.suppressPreviewVariant = false
	view.previewVariantSelect.Disable()
}

func (view *AssetView) SetData(data loader.AssetViewData) {
	view.previewVariantBuildToken.Add(1)
	view.meshPreviewLoadToken.Add(1)
	loader.PopulateAssetViewDisplayFields(&data)

	assetID := data.AssetID
	previewImageInfo := data.PreviewImageInfo
	statsInfo := loader.ResolveStatsInfo(data.StatsInfo, previewImageInfo)
	sourceDescription := data.SourceDescription
	stateDescription := data.StateDescription
	warningMessage := data.WarningMessage
	referencedAssetIDs := data.ReferencedAssetIDs
	assetTypeID := data.AssetTypeID
	downloadBytes := data.DownloadBytes
	downloadFileName := data.DownloadFileName
	downloadIsOriginal := data.DownloadIsOriginal

	view.currentAssetID = assetID
	updateMetadataRows(view.metadataRows, data)
	applySourceRowImportance(view.metadataRows, sourceDescription)

	view.pendingAssetDeliveryJSON = data.AssetDeliveryRawJSON
	view.pendingThumbnailJSON = data.ThumbnailRawJSON
	view.pendingEconomyJSON = data.EconomyRawJSON
	view.pendingRustyAssetToolJSON = data.RustyAssetToolRawJSON
	view.pendingReferencedAssetIDs = append([]int64(nil), referencedAssetIDs...)
	view.assetDownloadBytes = append([]byte(nil), downloadBytes...)
	view.assetDownloadFileName = strings.TrimSpace(downloadFileName)
	view.downloadOriginalAsset = downloadIsOriginal && len(downloadBytes) > 0

	if view.isJSONAccordionOpen() {
		view.renderJSONDetails()
	} else {
		view.showLazyJSONPlaceholder()
	}

	view.NoteLabel.Hide()
	view.NoteLabel.SetText("")
	isThumbnailFallbackSource := roblox.IsThumbnailFallback(sourceDescription)
	thumbnailStateNotCompleted := isThumbnailFallbackSource && !roblox.IsCompletedState(stateDescription)
	if warningMessage != "" {
		view.NoteLabel.SetText(loader.BuildFallbackWarningText(warningMessage))
		view.NoteLabel.Show()
	} else if thumbnailStateNotCompleted {
		view.NoteLabel.SetText(loader.BuildFallbackWarningText(fmt.Sprintf("thumbnail state was %s", stateDescription)))
		view.NoteLabel.Show()
	}
	view.configureAudioPlayback(statsInfo, assetTypeID)

	var previewResource fyne.Resource
	if previewImageInfo != nil && previewImageInfo.Resource != nil {
		previewResource = previewImageInfo.Resource
	}
	view.currentMeshPreviewData = MeshPreviewData{}
	view.MeshPreview.Clear()
	view.MeshPreview.Hide()
	if mesh.IsMeshAssetType(assetTypeID) && len(downloadBytes) > 0 {
		view.showMeshPreview(downloadBytes)
	} else if previewResource != nil {
		view.currentPreviewResource = previewResource
		originalPreviewOption := buildOriginalPreviewOption(previewResource, view.currentAssetID)
		originalPreviewOption.LabelText = formatPreviewOptionLabel(originalPreviewOption.LabelText, len(originalPreviewOption.Bytes), len(originalPreviewOption.Bytes))
		view.previewDownloadOptions = []PreviewDownloadOption{originalPreviewOption}
		view.selectedPreviewOption = originalPreviewOption.LabelText
		view.PreviewImage.File = ""
		view.PreviewImage.Image = nil
		view.PreviewImage.Resource = previewResource
		view.PreviewImage.Refresh()
		view.PreviewImage.Show()
		view.PreviewPlaceholder.Hide()
		view.downloadImageButton.Enable()
		view.uploadImageButton.Enable()
		view.expandImageButton.Enable()
		view.setSinglePreviewVariant(originalPreviewOption.LabelText)
		if !view.downloadOriginalAsset {
			view.interpolationSelect.Enable()
			view.rebuildPreviewVariants()
		}
	} else {
		view.currentPreviewResource = nil
		view.interpolationSelect.Disable()
		view.PreviewImage.Hide()
		view.PreviewPlaceholder.SetText("No preview image available")
		view.PreviewPlaceholder.Show()
		if view.downloadOriginalAsset && len(view.assetDownloadBytes) > 0 {
			view.setOriginalOnlyPreviewVariant()
			view.downloadImageButton.Enable()
			view.uploadImageButton.Enable()
		} else {
			view.clearPreviewVariantSelect()
			view.downloadImageButton.Disable()
			view.uploadImageButton.Disable()
		}
		view.expandImageButton.Disable()
	}
	view.PreviewContainer.Refresh()
}

func (view *AssetView) showMeshPreview(downloadBytes []byte) {
	view.currentPreviewResource = nil
	view.currentMeshPreviewData = MeshPreviewData{}
	view.interpolationSelect.Disable()
	view.PreviewImage.Hide()
	view.MeshPreview.Clear()
	view.MeshPreview.Hide()
	view.expandImageButton.Disable()
	if view.downloadOriginalAsset && len(view.assetDownloadBytes) > 0 {
		view.setOriginalOnlyPreviewVariant()
		view.downloadImageButton.Enable()
		view.uploadImageButton.Enable()
	} else {
		view.clearPreviewVariantSelect()
		view.downloadImageButton.Disable()
		view.uploadImageButton.Disable()
	}
	view.PreviewPlaceholder.SetText("Rendering mesh preview...")
	view.PreviewPlaceholder.Show()

	selectedAssetID := view.currentAssetID
	loadToken := view.meshPreviewLoadToken.Add(1)
	meshBytes := append([]byte(nil), downloadBytes...)
	go func() {
		meshData, previewErr := ExtractMeshPreviewFromBytes(meshBytes)
		fyne.Do(func() {
			if view.currentAssetID != selectedAssetID || view.meshPreviewLoadToken.Load() != loadToken {
				return
			}
			if previewErr == nil {
				view.currentMeshPreviewData = meshData
				view.MeshPreview.SetData(meshData)
				view.MeshPreview.Show()
				view.PreviewImage.Hide()
				view.PreviewPlaceholder.Hide()
				view.expandImageButton.Enable()
				view.PreviewContainer.Refresh()
				return
			}
			debug.Logf("Mesh preview unavailable for asset %d: %s", selectedAssetID, previewErr.Error())
			view.PreviewImage.Hide()
			view.MeshPreview.Hide()
			view.PreviewPlaceholder.SetText(friendlyMeshPreviewError(previewErr))
			view.PreviewPlaceholder.Show()
			view.PreviewContainer.Refresh()
		})
	}()
}

func (view *AssetView) showImagePreviewFallback(previewResource fyne.Resource) {
	view.currentPreviewResource = previewResource
	view.currentMeshPreviewData = MeshPreviewData{}
	originalPreviewOption := buildOriginalPreviewOption(previewResource, view.currentAssetID)
	originalPreviewOption.LabelText = formatPreviewOptionLabel(originalPreviewOption.LabelText, len(originalPreviewOption.Bytes), len(originalPreviewOption.Bytes))
	view.previewDownloadOptions = []PreviewDownloadOption{originalPreviewOption}
	view.selectedPreviewOption = originalPreviewOption.LabelText
	view.PreviewImage.File = ""
	view.PreviewImage.Image = nil
	view.PreviewImage.Resource = previewResource
	view.PreviewImage.Refresh()
	view.PreviewImage.Show()
	view.MeshPreview.Hide()
	view.PreviewPlaceholder.Hide()
	view.downloadImageButton.Enable()
	view.uploadImageButton.Enable()
	view.expandImageButton.Enable()
	view.setSinglePreviewVariant(originalPreviewOption.LabelText)
	if !view.downloadOriginalAsset {
		view.interpolationSelect.Enable()
		view.rebuildPreviewVariants()
	}
	view.PreviewContainer.Refresh()
}

func friendlyMeshPreviewError(err error) string {
	msg := err.Error()
	if strings.Contains(msg, "unsupported mesh preview format: version") {
		version := strings.TrimPrefix(msg[strings.Index(msg, "version"):], "version ")
		return fmt.Sprintf("Mesh preview not supported for v%s meshes (only v7 is supported)", version)
	}
	if strings.Contains(msg, "mesh data is empty") {
		return "Mesh preview failed: file is empty"
	}
	if strings.Contains(msg, "binary not found") || strings.Contains(msg, "not found in") {
		return "Mesh preview failed: asset tool not found"
	}
	return fmt.Sprintf("Mesh preview failed: %s", msg)
}
