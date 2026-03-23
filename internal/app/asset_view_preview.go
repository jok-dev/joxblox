package app

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
	nativeDialog "github.com/sqweek/dialog"
	xdraw "golang.org/x/image/draw"
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
		variantLabels = []string{selectedOption.labelText}
		selectedVariantLabel = selectedOption.labelText
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
	sizeText := formatSizeInMB(optionByteCount)
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

func buildPreviewDownloadOptions(resource fyne.Resource, assetID int64) ([]previewDownloadOption, error) {
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
	threeQuarterBytes, threeQuarterErr := encodeScaledPreview(decodedImage, imageFormat, 0.75)
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
	halfBytes, halfErr := encodeResizedPreview(decodedImage, imageFormat, 2)
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
	thirdBytes, thirdErr := encodeScaledPreview(decodedImage, imageFormat, 1.0/3.0)
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
	quarterBytes, quarterErr := encodeResizedPreview(decodedImage, imageFormat, 4)
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
		encoder := png.Encoder{CompressionLevel: png.BestSpeed}
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
		encoder := png.Encoder{CompressionLevel: png.BestSpeed}
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
