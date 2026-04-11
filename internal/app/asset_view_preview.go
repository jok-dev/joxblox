package app

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
	nativeDialog "github.com/sqweek/dialog"
	xdraw "golang.org/x/image/draw"
)

var (
	lastUploadCreatorType = uploadCreatorModeUser
	lastUploadCreatorID   = ""
)

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

func (view *assetView) uploadSelectedPreviewVariant() {
	window := getPrimaryWindow()
	if window == nil {
		return
	}

	var uploadBytes []byte
	var uploadFileName string

	if view.downloadOriginalAsset && len(view.assetDownloadBytes) > 0 {
		uploadBytes = view.assetDownloadBytes
		uploadFileName = view.assetDownloadFileName
		if uploadFileName == "" {
			uploadFileName = fmt.Sprintf("asset_%d.bin", view.currentAssetID)
		}
	} else {
		selectedOption, found := view.selectedPreviewDownloadOption()
		if !found {
			dialog.ShowInformation("Upload", "No preview image is available to upload.", window)
			return
		}
		uploadBytes = selectedOption.bytes
		uploadFileName = selectedOption.fileName
	}

	if len(uploadBytes) == 0 {
		dialog.ShowInformation("Upload", "No image bytes available to upload.", window)
		return
	}

	apiKeyEntry := widget.NewPasswordEntry()
	apiKeyEntry.SetPlaceHolder("Open Cloud API key")
	if storedKey, loadErr := LoadOpenCloudAPIKeyFromKeyring(); loadErr == nil && storedKey != "" {
		apiKeyEntry.SetText(storedKey)
	}

	creatorTypeSelect := widget.NewSelect([]string{uploadCreatorModeUser, uploadCreatorModeGroup}, nil)
	creatorTypeSelect.SetSelected(lastUploadCreatorType)

	creatorIDEntry := widget.NewEntry()
	creatorIDEntry.SetPlaceHolder("Creator user/group ID")
	creatorIDEntry.SetText(lastUploadCreatorID)

	displayNameEntry := widget.NewEntry()
	displayNameEntry.SetText(fmt.Sprintf("Asset %d reupload", view.currentAssetID))

	content := container.NewVBox(
		container.NewBorder(nil, nil, widget.NewLabel("API Key:"), nil, apiKeyEntry),
		container.NewGridWithColumns(2,
			container.NewBorder(nil, nil, widget.NewLabel("Creator Type:"), nil, creatorTypeSelect),
			container.NewBorder(nil, nil, widget.NewLabel("Creator ID:"), nil, creatorIDEntry),
		),
		container.NewBorder(nil, nil, widget.NewLabel("Display Name:"), nil, displayNameEntry),
	)

	dialog.ShowCustomConfirm("Upload to Roblox", "Upload", "Cancel", content, func(confirmed bool) {
		if !confirmed {
			return
		}

		apiKey := strings.TrimSpace(apiKeyEntry.Text)
		if apiKey == "" {
			dialog.ShowError(fmt.Errorf("API key is required"), window)
			return
		}

		creatorID, parseErr := strconv.ParseInt(strings.TrimSpace(creatorIDEntry.Text), 10, 64)
		if parseErr != nil || creatorID <= 0 {
			dialog.ShowError(fmt.Errorf("creator ID must be a positive integer"), window)
			return
		}

		displayName := strings.TrimSpace(displayNameEntry.Text)
		if displayName == "" {
			displayName = fmt.Sprintf("Asset %d", view.currentAssetID)
		}

		lastUploadCreatorType = creatorTypeSelect.Selected
		lastUploadCreatorID = strings.TrimSpace(creatorIDEntry.Text)

		creator := robloxOpenCloudCreator{
			IsGroup: creatorTypeSelect.Selected == uploadCreatorModeGroup,
			ID:      creatorID,
		}

		progressDialog := dialog.NewCustomWithoutButtons("Uploading...", widget.NewProgressBarInfinite(), window)
		progressDialog.Show()

		go func() {
			stopCh := make(chan struct{})
			assetID, uploadErr := uploadDecalToRobloxOpenCloud(
				apiKey,
				creator,
				displayName,
				"",
				uploadFileName,
				uploadBytes,
				stopCh,
			)

			fyne.Do(func() {
				progressDialog.Hide()
				if uploadErr != nil {
					dialog.ShowError(fmt.Errorf("upload failed: %w", uploadErr), window)
					return
				}

				assetIDStr := strconv.FormatInt(assetID, 10)
				idEntry := widget.NewEntry()
				idEntry.SetText(assetIDStr)

				resultContent := container.NewVBox(
					widget.NewLabel("Uploaded successfully! Asset ID:"),
					idEntry,
				)
				dialog.ShowCustom("Upload Complete", "Close", resultContent, window)
			})
		}()
	}, window)
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

func formatVariantSizeInfo(option previewDownloadOption) string {
	sizeText := formatSizeAuto(len(option.bytes))
	if option.width > 0 && option.height > 0 {
		return fmt.Sprintf("%dx%d · %s", option.width, option.height, sizeText)
	}
	return sizeText
}

func findOptionIndex(options []previewDownloadOption, label string) int {
	for i, opt := range options {
		if opt.labelText == label {
			return i
		}
	}
	return 0
}

func (view *assetView) showExpandedImageWindow() {
	if view.MeshPreview.Visible() && len(view.currentMeshPreviewData.RawPositions) > 0 && len(view.currentMeshPreviewData.RawIndices) > 0 {
		view.showExpandedMeshWindow()
		return
	}

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
	localOptions := append([]previewDownloadOption{}, view.previewDownloadOptions...)
	selectedIndex := findOptionIndex(localOptions, selectedOption.labelText)
	fileSizeLabel := widget.NewLabel(formatVariantSizeInfo(currentOption))
	capturedAssetID := view.currentAssetID
	capturedResource := view.currentPreviewResource

	applyExpandedPreviewState := func() {
		imageViewer.SetOption(currentOption)
		fileSizeLabel.SetText(formatVariantSizeInfo(currentOption))
	}

	variantLabels := make([]string, 0, len(localOptions))
	for _, option := range localOptions {
		variantLabels = append(variantLabels, option.labelText)
	}

	variantSelect := widget.NewSelect(variantLabels, nil)
	interpolationSelect := widget.NewSelect(sampleModeOptions, nil)
	backgroundSelect := widget.NewSelect([]string{expandedBackgroundBlack, expandedBackgroundWhite}, nil)
	interpolationSelect.SetSelected(view.interpolationSelect.Selected)
	backgroundSelect.SetSelected(expandedBackgroundBlack)

	variantSelect.OnChanged = func(selectedLabel string) {
		for i, option := range localOptions {
			if option.labelText != selectedLabel {
				continue
			}
			currentOption = option
			selectedIndex = i
			applyExpandedPreviewState()
			return
		}
	}

	interpolationSelect.OnChanged = func(mode string) {
		view.interpolationSelect.SetSelected(mode)
		if capturedResource == nil {
			return
		}
		scaler := sampleModeInterpolator(mode)
		go func() {
			newOptions, buildErr := buildPreviewDownloadOptions(capturedResource, capturedAssetID, scaler)
			if buildErr != nil || len(newOptions) == 0 {
				return
			}
			fyne.Do(func() {
				localOptions = newOptions
				newLabels := make([]string, 0, len(newOptions))
				for _, opt := range newOptions {
					newLabels = append(newLabels, opt.labelText)
				}
				idx := selectedIndex
				if idx >= len(newOptions) {
					idx = 0
				}
				currentOption = newOptions[idx]
				selectedIndex = idx
				variantSelect.SetOptions(newLabels)
				variantSelect.SetSelected(newLabels[idx])
				applyExpandedPreviewState()
			})
		}()
	}

	backgroundSelect.OnChanged = func(mode string) {
		imageViewer.SetBackground(mode)
	}

	if view.downloadOriginalAsset {
		variantSelect.SetOptions([]string{selectedOption.labelText})
		variantSelect.Disable()
		interpolationSelect.Disable()
	}
	variantSelect.SetSelected(selectedOption.labelText)
	imageViewer.SetBackground(backgroundSelect.Selected)
	applyExpandedPreviewState()

	topBar := container.NewHBox(
		widget.NewLabel("Preview Version:"),
		container.NewGridWrap(fyne.NewSize(280, 36), variantSelect),
		widget.NewLabel("Interpolation:"),
		container.NewGridWrap(fyne.NewSize(160, 36), interpolationSelect),
		fileSizeLabel,
	)
	bottomBar := container.NewHBox(
		widget.NewLabel("View Background:"),
		container.NewGridWrap(fyne.NewSize(120, 36), backgroundSelect),
	)
	imageWindow.SetContent(container.NewBorder(topBar, bottomBar, nil, nil, container.NewPadded(imageViewer)))
	imageWindow.Resize(fyne.NewSize(980, 760))
	imageWindow.Show()
}

func (view *assetView) showExpandedMeshWindow() {
	guiApp := fyne.CurrentApp()
	if guiApp == nil {
		return
	}

	meshWindow := guiApp.NewWindow(fmt.Sprintf("Asset %d Model", view.currentAssetID))
	meshViewer := newMeshPreviewWidget()
	meshViewer.SetFocusCanvas(meshWindow.Canvas())
	meshViewer.SetData(view.currentMeshPreviewData)
	backgroundSelect := widget.NewSelect([]string{expandedBackgroundBlack, expandedBackgroundWhite}, nil)
	backgroundSelect.SetSelected(expandedBackgroundBlack)
	backgroundSelect.OnChanged = func(mode string) {
		meshViewer.SetBackground(zoomPanBackgroundColor(mode))
	}
	meshViewer.SetBackground(zoomPanBackgroundColor(backgroundSelect.Selected))

	meshInfoText := fmt.Sprintf(
		"Shown Triangles: %s / %s",
		formatIntCommas(int64(view.currentMeshPreviewData.PreviewTriangleCount)),
		formatIntCommas(int64(view.currentMeshPreviewData.TriangleCount)),
	)
	if view.currentMeshPreviewData.PreviewTriangleCount == 0 || view.currentMeshPreviewData.PreviewTriangleCount == view.currentMeshPreviewData.TriangleCount {
		meshInfoText = fmt.Sprintf(
			"Triangles: %s",
			formatIntCommas(int64(view.currentMeshPreviewData.TriangleCount)),
		)
	}
	topBar := container.NewHBox(
		widget.NewLabel(meshInfoText),
		widget.NewLabel("View Background:"),
		container.NewGridWrap(fyne.NewSize(120, 36), backgroundSelect),
		layout.NewSpacer(),
		widget.NewLabel(meshPreviewControlsText()),
	)
	meshWindow.SetContent(container.NewBorder(topBar, nil, nil, nil, meshViewer))
	meshWindow.Resize(fyne.NewSize(980, 760))
	meshWindow.SetOnClosed(func() {
		meshViewer.Clear()
	})
	meshWindow.Show()
	go func() {
		for attempt := 0; attempt < 12; attempt++ {
			time.Sleep(25 * time.Millisecond)
			rendered := false
			fyne.Do(func() {
				size := meshViewer.Size()
				if size.Width < minMeshPreviewRenderDimension || size.Height < minMeshPreviewRenderDimension {
					return
				}
				meshViewer.render()
				rendered = true
			})
			if rendered {
				return
			}
		}
	}()
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

func buildOriginalPreviewOption(resource fyne.Resource, assetID int64) previewDownloadOption {
	resourceName := ""
	resourceBytes := []byte(nil)
	if resource != nil {
		resourceName = strings.TrimSpace(resource.Name())
		resourceBytes = resource.Content()
	}
	if resourceName == "" {
		resourceName = fmt.Sprintf("asset_%d.png", assetID)
	}
	return previewDownloadOption{
		labelText: "Original",
		fileName:  resourceName,
		bytes:     append([]byte(nil), resourceBytes...),
	}
}

func formatPreviewOptionLabel(baseLabel string, optionByteCount int, originalByteCount int) string {
	trimmedBaseLabel := strings.TrimSpace(baseLabel)
	if trimmedBaseLabel == "" {
		trimmedBaseLabel = "Preview"
	}
	sizeText := formatSizeAuto(optionByteCount)
	if originalByteCount <= 0 || optionByteCount <= 0 || optionByteCount == originalByteCount {
		return fmt.Sprintf("%s (%s)", trimmedBaseLabel, sizeText)
	}
	deltaPercent := int(((float64(optionByteCount) - float64(originalByteCount)) / float64(originalByteCount)) * 100.0)
	return fmt.Sprintf("%s (%s, %d%%)", trimmedBaseLabel, sizeText, deltaPercent)
}

func applyPreviewOptionLabels(options []previewDownloadOption) []previewDownloadOption {
	if len(options) == 0 {
		return options
	}
	originalByteCount := len(options[0].bytes)
	labeledOptions := make([]previewDownloadOption, 0, len(options))
	for _, option := range options {
		option.labelText = formatPreviewOptionLabel(option.labelText, len(option.bytes), originalByteCount)
		labeledOptions = append(labeledOptions, option)
	}
	return labeledOptions
}

func buildPreviewDownloadOptions(resource fyne.Resource, assetID int64, scaler xdraw.Interpolator) ([]previewDownloadOption, error) {
	if resource == nil {
		return nil, nil
	}
	originalOption := buildOriginalPreviewOption(resource, assetID)
	resourceName := originalOption.fileName
	resourceBytes := resource.Content()
	if len(resourceBytes) == 0 {
		return nil, nil
	}

	decodedImage, imageFormat, decodeErr := image.Decode(bytes.NewReader(resourceBytes))
	if decodeErr != nil {
		return []previewDownloadOption{originalOption}, nil
	}
	baseBounds := decodedImage.Bounds()
	originalOption.width = baseBounds.Dx()
	originalOption.height = baseBounds.Dy()
	options := []previewDownloadOption{originalOption}
	if baseBounds.Dx() <= 1 || baseBounds.Dy() <= 1 {
		return options, nil
	}
	baseExtension := previewOutputExtension(resourceName, imageFormat)
	threeQuarterBytes, threeQuarterErr := encodeScaledPreview(decodedImage, imageFormat, 0.75, scaler)
	if threeQuarterErr == nil {
		threeQuarterWidth := maxInt(1, int(float64(baseBounds.Dx())*0.75))
		threeQuarterHeight := maxInt(1, int(float64(baseBounds.Dy())*0.75))
		options = append(options, previewDownloadOption{
			labelText: "Three Quarters",
			fileName:  previewVariantFileName(resourceName, "three_quarters", baseExtension),
			bytes:     threeQuarterBytes,
			width:     threeQuarterWidth,
			height:    threeQuarterHeight,
		})
	}
	halfBytes, halfErr := encodeResizedPreview(decodedImage, imageFormat, 2, scaler)
	if halfErr == nil {
		halfWidth := maxInt(1, baseBounds.Dx()/2)
		halfHeight := maxInt(1, baseBounds.Dy()/2)
		options = append(options, previewDownloadOption{
			labelText: "Half",
			fileName:  previewVariantFileName(resourceName, "half", baseExtension),
			bytes:     halfBytes,
			width:     halfWidth,
			height:    halfHeight,
		})
	}
	thirdBytes, thirdErr := encodeScaledPreview(decodedImage, imageFormat, 1.0/3.0, scaler)
	if thirdErr == nil {
		thirdWidth := maxInt(1, int(float64(baseBounds.Dx())/3.0))
		thirdHeight := maxInt(1, int(float64(baseBounds.Dy())/3.0))
		options = append(options, previewDownloadOption{
			labelText: "Third",
			fileName:  previewVariantFileName(resourceName, "third", baseExtension),
			bytes:     thirdBytes,
			width:     thirdWidth,
			height:    thirdHeight,
		})
	}
	quarterBytes, quarterErr := encodeResizedPreview(decodedImage, imageFormat, 4, scaler)
	if quarterErr == nil {
		quarterWidth := maxInt(1, baseBounds.Dx()/4)
		quarterHeight := maxInt(1, baseBounds.Dy()/4)
		options = append(options, previewDownloadOption{
			labelText: "Quarter",
			fileName:  previewVariantFileName(resourceName, "quarter", baseExtension),
			bytes:     quarterBytes,
			width:     quarterWidth,
			height:    quarterHeight,
		})
	}
	return applyPreviewOptionLabels(options), nil
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

func (view *assetView) setOriginalOnlyPreviewVariant() {
	selectedLabel := "Original"
	if len(view.previewDownloadOptions) > 0 {
		selectedLabel = view.previewDownloadOptions[0].labelText
	}
	view.suppressPreviewVariant = true
	view.previewVariantSelect.SetOptions([]string{selectedLabel})
	view.previewVariantSelect.SetSelected(selectedLabel)
	view.suppressPreviewVariant = false
	view.selectedPreviewOption = selectedLabel
	view.previewVariantSelect.Disable()
}

func (view *assetView) rebuildPreviewVariants() {
	resource := view.currentPreviewResource
	if resource == nil || view.downloadOriginalAsset {
		return
	}
	selectedAssetID := view.currentAssetID
	previousIndex := findOptionIndex(view.previewDownloadOptions, view.selectedPreviewOption)
	buildToken := view.previewVariantBuildToken.Add(1)
	scaler := sampleModeInterpolator(view.interpolationSelect.Selected)
	go func() {
		downloadOptions, buildErr := buildPreviewDownloadOptions(resource, selectedAssetID, scaler)
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
			idx := previousIndex
			if idx >= len(optionLabels) {
				idx = 0
			}
			selectedLabel := optionLabels[idx]
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
	}()
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
		view.uploadImageButton.Disable()
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
	view.uploadImageButton.Enable()
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

func previewOutputExtension(_ string, _ string) string {
	return ".png"
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

func encodeResizedPreview(sourceImage image.Image, _ string, divisor int, scaler xdraw.Interpolator) ([]byte, error) {
	if sourceImage == nil {
		return nil, fmt.Errorf("preview image is unavailable")
	}
	if divisor <= 1 {
		return nil, fmt.Errorf("resize divisor must be greater than 1")
	}
	sourceBounds := sourceImage.Bounds()
	targetWidth := maxInt(1, sourceBounds.Dx()/divisor)
	targetHeight := maxInt(1, sourceBounds.Dy()/divisor)
	return encodeScaledPNG(sourceImage, targetWidth, targetHeight, scaler)
}

func encodeScaledPreview(sourceImage image.Image, _ string, scale float64, scaler xdraw.Interpolator) ([]byte, error) {
	if sourceImage == nil {
		return nil, fmt.Errorf("preview image is unavailable")
	}
	if scale <= 0 {
		return nil, fmt.Errorf("preview scale must be positive")
	}
	sourceBounds := sourceImage.Bounds()
	targetWidth := maxInt(1, int(float64(sourceBounds.Dx())*scale))
	targetHeight := maxInt(1, int(float64(sourceBounds.Dy())*scale))
	return encodeScaledPNG(sourceImage, targetWidth, targetHeight, scaler)
}

func encodeScaledPNG(sourceImage image.Image, targetWidth int, targetHeight int, scaler xdraw.Interpolator) ([]byte, error) {
	preparedImage := cloneImageToNRGBA(sourceImage)
	if preparedImage == nil {
		return nil, fmt.Errorf("preview image is unavailable")
	}
	preparedImage = bleedTransparentPixels(preparedImage)

	targetImage := image.NewNRGBA(image.Rect(0, 0, targetWidth, targetHeight))
	scaler.Scale(targetImage, targetImage.Bounds(), preparedImage, preparedImage.Bounds(), xdraw.Src, nil)
	targetImage = bleedTransparentPixels(targetImage)

	var outputBuffer bytes.Buffer
	encoder := png.Encoder{CompressionLevel: png.BestSpeed}
	if encodeErr := encoder.Encode(&outputBuffer, targetImage); encodeErr != nil {
		return nil, encodeErr
	}
	return outputBuffer.Bytes(), nil
}

func cloneImageToNRGBA(sourceImage image.Image) *image.NRGBA {
	if sourceImage == nil {
		return nil
	}
	sourceBounds := sourceImage.Bounds()
	width := sourceBounds.Dx()
	height := sourceBounds.Dy()
	clonedImage := image.NewNRGBA(image.Rect(0, 0, width, height))

	switch typedImage := sourceImage.(type) {
	case *image.NRGBA:
		for y := 0; y < height; y++ {
			srcOffset := typedImage.PixOffset(sourceBounds.Min.X, sourceBounds.Min.Y+y)
			dstOffset := clonedImage.PixOffset(0, y)
			copy(clonedImage.Pix[dstOffset:dstOffset+width*4], typedImage.Pix[srcOffset:srcOffset+width*4])
		}
		return clonedImage
	default:
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				clonedImage.SetNRGBA(x, y, color.NRGBAModel.Convert(sourceImage.At(sourceBounds.Min.X+x, sourceBounds.Min.Y+y)).(color.NRGBA))
			}
		}
		return clonedImage
	}
}

type transparentPixelPoint struct {
	x int
	y int
}

// Fill transparent RGB with nearby visible colors so later filtering does not pull in black edges.
func bleedTransparentPixels(sourceImage *image.NRGBA) *image.NRGBA {
	if sourceImage == nil {
		return nil
	}
	bounds := sourceImage.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()
	if width == 0 || height == 0 {
		return sourceImage
	}

	resultImage := image.NewNRGBA(image.Rect(0, 0, width, height))
	copy(resultImage.Pix, sourceImage.Pix)

	visited := make([]bool, width*height)
	queue := make([]transparentPixelPoint, 0, width)
	neighborOffsets := [8][2]int{
		{-1, -1}, {0, -1}, {1, -1},
		{-1, 0}, {1, 0},
		{-1, 1}, {0, 1}, {1, 1},
	}

	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			pixelOffset := sourceImage.PixOffset(x, y)
			if sourceImage.Pix[pixelOffset+3] != 0 {
				continue
			}

			var redSum int
			var greenSum int
			var blueSum int
			neighborCount := 0
			for _, offset := range neighborOffsets {
				neighborX := x + offset[0]
				neighborY := y + offset[1]
				if neighborX < 0 || neighborX >= width || neighborY < 0 || neighborY >= height {
					continue
				}
				neighborOffset := sourceImage.PixOffset(neighborX, neighborY)
				if sourceImage.Pix[neighborOffset+3] == 0 {
					continue
				}
				redSum += int(sourceImage.Pix[neighborOffset+0])
				greenSum += int(sourceImage.Pix[neighborOffset+1])
				blueSum += int(sourceImage.Pix[neighborOffset+2])
				neighborCount++
			}
			if neighborCount == 0 {
				continue
			}

			resultImage.Pix[pixelOffset+0] = uint8(redSum / neighborCount)
			resultImage.Pix[pixelOffset+1] = uint8(greenSum / neighborCount)
			resultImage.Pix[pixelOffset+2] = uint8(blueSum / neighborCount)
			visited[y*width+x] = true
			queue = append(queue, transparentPixelPoint{x: x, y: y})
		}
	}

	for queueIndex := 0; queueIndex < len(queue); queueIndex++ {
		currentPoint := queue[queueIndex]
		currentOffset := resultImage.PixOffset(currentPoint.x, currentPoint.y)
		for _, offset := range neighborOffsets {
			neighborX := currentPoint.x + offset[0]
			neighborY := currentPoint.y + offset[1]
			if neighborX < 0 || neighborX >= width || neighborY < 0 || neighborY >= height {
				continue
			}
			visitedIndex := neighborY*width + neighborX
			if visited[visitedIndex] {
				continue
			}
			neighborOffset := resultImage.PixOffset(neighborX, neighborY)
			if resultImage.Pix[neighborOffset+3] != 0 {
				continue
			}

			resultImage.Pix[neighborOffset+0] = resultImage.Pix[currentOffset+0]
			resultImage.Pix[neighborOffset+1] = resultImage.Pix[currentOffset+1]
			resultImage.Pix[neighborOffset+2] = resultImage.Pix[currentOffset+2]
			visited[visitedIndex] = true
			queue = append(queue, transparentPixelPoint{x: neighborX, y: neighborY})
		}
	}

	return resultImage
}

func maxInt(left int, right int) int {
	if left > right {
		return left
	}
	return right
}
