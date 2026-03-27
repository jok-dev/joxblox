package app

import (
	"bytes"
	"crypto/rand"
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
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	fyneDialog "fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
	nativeDialog "github.com/sqweek/dialog"
	xdraw "golang.org/x/image/draw"
)

const (
	uploadDefaultImageCount = 10
	uploadUIRefreshEvery    = 5
	patternModeNoise        = "Random Noise (largest)"
	patternModeCheckered    = "Random Checkered (smaller)"
	uploadCreatorModeUser   = "User"
	uploadCreatorModeGroup  = "Group"
	defaultAssetNameBase    = "Joxblox Texture"
)

var imageSizeOptions = []string{
	"256x256",
	"512x512",
	"1024x1024",
	"2048x2048",
	"4096x4096",
}

const defaultImageSize = "1024x1024"

var sampleSizeOptions = []string{
	"Original",
	"3/4",
	"1/2",
	"1/4",
	"1/8",
}

const defaultSampleSize = "Original"

const (
	sampleModeNearestNeighbor = "Nearest Neighbor"
	sampleModeBilinear        = "Bilinear"
	sampleModeCatmullRom      = "Catmull-Rom"
	defaultSampleMode         = sampleModeCatmullRom
)

var sampleModeOptions = []string{
	sampleModeNearestNeighbor,
	sampleModeBilinear,
	sampleModeCatmullRom,
}

func sampleModeInterpolator(mode string) xdraw.Interpolator {
	switch mode {
	case sampleModeNearestNeighbor:
		return xdraw.NearestNeighbor
	case sampleModeBilinear:
		return xdraw.BiLinear
	default:
		return xdraw.CatmullRom
	}
}

func parseImageSize(sizeStr string) (int, int, error) {
	parts := strings.SplitN(sizeStr, "x", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid size format: %s", sizeStr)
	}
	w, err := strconv.Atoi(parts[0])
	if err != nil || w <= 0 {
		return 0, 0, fmt.Errorf("invalid width: %s", parts[0])
	}
	h, err := strconv.Atoi(parts[1])
	if err != nil || h <= 0 {
		return 0, 0, fmt.Errorf("invalid height: %s", parts[1])
	}
	return w, h, nil
}

func parseSampleFraction(s string) (numerator int, denominator int) {
	switch s {
	case "3/4":
		return 3, 4
	case "1/2":
		return 1, 2
	case "1/4":
		return 1, 4
	case "1/8":
		return 1, 8
	default:
		return 1, 1
	}
}

type sampleSpec struct {
	Label       string
	Resolution  string
	Numerator   int
	Denominator int
}

func buildSampleSpecs(labels []string, baseW, baseH int) []sampleSpec {
	specs := make([]sampleSpec, 0, len(labels))
	for _, label := range labels {
		num, den := parseSampleFraction(label)
		w := max(baseW*num/den, 1)
		h := max(baseH*num/den, 1)
		specs = append(specs, sampleSpec{
			Label:       label,
			Resolution:  fmt.Sprintf("%dx%d", w, h),
			Numerator:   num,
			Denominator: den,
		})
	}
	return specs
}

func newImageUploaderTab(window fyne.Window) fyne.CanvasObject {
	countEntry := widget.NewEntry()
	countEntry.SetText(strconv.Itoa(uploadDefaultImageCount))
	countEntry.SetPlaceHolder(strconv.Itoa(uploadDefaultImageCount))
	patternSelect := widget.NewSelect([]string{patternModeNoise, patternModeCheckered}, nil)
	patternSelect.SetSelected(patternModeNoise)
	sizeSelect := widget.NewSelect(imageSizeOptions, nil)
	sizeSelect.SetSelected(defaultImageSize)
	selectedSampleLabels := []string{defaultSampleSize}
	sampleListContainer := container.NewHBox()
	addSampleSelect := widget.NewSelect(sampleSizeOptions, nil)
	addSampleSelect.SetSelected(defaultSampleSize)
	addSampleButton := widget.NewButton("Add", nil)
	sampleModeSelect := widget.NewSelect(sampleModeOptions, nil)
	sampleModeSelect.SetSelected(defaultSampleMode)

	var inProgress bool
	var rebuildSampleList func()
	rebuildSampleList = func() {
		sampleListContainer.RemoveAll()
		for i, label := range selectedSampleLabels {
			idx := i
			lbl := label
			removeBtn := widget.NewButton("✕", func() {
				if inProgress || len(selectedSampleLabels) <= 1 {
					return
				}
				selectedSampleLabels = append(selectedSampleLabels[:idx], selectedSampleLabels[idx+1:]...)
				rebuildSampleList()
			})
			sampleListContainer.Add(container.NewHBox(widget.NewLabel(lbl), removeBtn))
		}
	}
	addSampleButton.OnTapped = func() {
		if inProgress {
			return
		}
		sel := addSampleSelect.Selected
		if sel == "" {
			return
		}
		for _, existing := range selectedSampleLabels {
			if existing == sel {
				return
			}
		}
		selectedSampleLabels = append(selectedSampleLabels, sel)
		rebuildSampleList()
	}
	rebuildSampleList()

	outputFolderEntry := widget.NewEntry()
	defaultOutputFolder := filepath.Join(".", "generated_images")
	outputFolderEntry.SetText(defaultOutputFolder)
	outputFolderEntry.SetPlaceHolder(defaultOutputFolder)

	uploadToRobloxCheck := widget.NewCheck("Upload to Roblox after generating", nil)
	apiKeyEntry := widget.NewPasswordEntry()
	apiKeyEntry.SetPlaceHolder("Open Cloud API key")
	rememberAPIKeyCheck := widget.NewCheck("Save API key to keychain", nil)
	creatorTypeSelect := widget.NewSelect([]string{uploadCreatorModeUser, uploadCreatorModeGroup}, nil)
	creatorTypeSelect.SetSelected(uploadCreatorModeUser)
	creatorIDEntry := widget.NewEntry()
	creatorIDEntry.SetPlaceHolder("Creator user/group ID")
	assetNameEntry := widget.NewEntry()
	assetNameEntry.SetText(defaultAssetNameBase)
	assetNameEntry.SetPlaceHolder(defaultAssetNameBase)
	descriptionEntry := widget.NewMultiLineEntry()
	descriptionEntry.SetPlaceHolder("Optional asset description")
	descriptionEntry.SetMinRowsVisible(2)

	if storedAPIKey, loadErr := LoadOpenCloudAPIKeyFromKeyring(); loadErr != nil {
		logDebugf("Failed to load Open Cloud API key from keychain: %s", loadErr.Error())
	} else if storedAPIKey != "" {
		apiKeyEntry.SetText(storedAPIKey)
		rememberAPIKeyCheck.SetChecked(true)
	}

	statusLabel := widget.NewLabel("Set count/folder and click Generate Images.")
	generatedPathsEntry := widget.NewMultiLineEntry()
	generatedPathsEntry.SetPlaceHolder("Generated file paths and uploaded asset IDs will appear here...")
	generatedPathsEntry.Disable()
	generatedPathsEntry.Wrapping = fyne.TextWrapBreak
	generatedPathsEntry.SetMinRowsVisible(10)

	generateButton := widget.NewButton("Generate Images", nil)
	selectFolderButton := widget.NewButton("Select Folder", nil)
	stopButton := widget.NewButton("Stop", nil)
	stopButton.Disable()

	var activeStopSignal *stopSignal
	updateUploadControls := func() {
		uploadEnabled := uploadToRobloxCheck.Checked && !inProgress
		if uploadEnabled {
			apiKeyEntry.Enable()
			rememberAPIKeyCheck.Enable()
			creatorTypeSelect.Enable()
			creatorIDEntry.Enable()
			assetNameEntry.Enable()
			descriptionEntry.Enable()
			return
		}
		apiKeyEntry.Disable()
		rememberAPIKeyCheck.Disable()
		creatorTypeSelect.Disable()
		creatorIDEntry.Disable()
		assetNameEntry.Disable()
		descriptionEntry.Disable()
	}
	updateButtons := func(running bool) {
		inProgress = running
		if running {
			generateButton.Disable()
			selectFolderButton.Disable()
			stopButton.Enable()
			countEntry.Disable()
			patternSelect.Disable()
			sizeSelect.Disable()
			addSampleSelect.Disable()
			addSampleButton.Disable()
			sampleModeSelect.Disable()
			outputFolderEntry.Disable()
			uploadToRobloxCheck.Disable()
			updateUploadControls()
			return
		}
		generateButton.Enable()
		selectFolderButton.Enable()
		stopButton.Disable()
		countEntry.Enable()
		patternSelect.Enable()
		sizeSelect.Enable()
		addSampleSelect.Enable()
		addSampleButton.Enable()
		sampleModeSelect.Enable()
		outputFolderEntry.Enable()
		uploadToRobloxCheck.Enable()
		updateUploadControls()
	}
	requestStopGeneration := func() {
		if activeStopSignal == nil {
			return
		}
		localStopSignal := activeStopSignal
		activeStopSignal = nil
		localStopSignal.Stop()
	}
	finishGeneration := func(localStopSignal *stopSignal, statusText string) {
		updateButtons(false)
		if activeStopSignal == localStopSignal {
			activeStopSignal = nil
		}
		statusLabel.SetText(statusText)
	}
	failGeneration := func(localStopSignal *stopSignal, statusText string, err error) {
		finishGeneration(localStopSignal, statusText)
		if err != nil {
			fyneDialog.ShowError(err, window)
		}
	}

	selectFolderButton.OnTapped = func() {
		selectedPath, pickerErr := nativeDialog.Directory().Title("Select output folder").Browse()
		if pickerErr == nil {
			outputFolderEntry.SetText(selectedPath)
			return
		}
		if errors.Is(pickerErr, nativeDialog.Cancelled) {
			return
		}
		statusLabel.SetText(fmt.Sprintf("Folder picker failed: %s", pickerErr.Error()))
	}

	stopButton.OnTapped = func() {
		requestStopGeneration()
		statusLabel.SetText("Stopping generation/upload...")
		stopButton.Disable()
	}
	uploadToRobloxCheck.OnChanged = func(bool) {
		updateUploadControls()
	}

	generateButton.OnTapped = func() {
		if inProgress {
			return
		}
		countValue, parseErr := strconv.Atoi(strings.TrimSpace(countEntry.Text))
		if parseErr != nil || countValue <= 0 {
			statusLabel.SetText("Count must be a positive integer.")
			return
		}
		selectedPatternMode := strings.TrimSpace(patternSelect.Selected)
		if selectedPatternMode == "" {
			selectedPatternMode = patternModeNoise
		}
		selectedSize := strings.TrimSpace(sizeSelect.Selected)
		if selectedSize == "" {
			selectedSize = defaultImageSize
		}
		imgWidth, imgHeight, sizeErr := parseImageSize(selectedSize)
		if sizeErr != nil {
			statusLabel.SetText("Invalid image size.")
			return
		}
		if len(selectedSampleLabels) == 0 {
			statusLabel.SetText("Add at least one sample size.")
			return
		}
		samples := buildSampleSpecs(selectedSampleLabels, imgWidth, imgHeight)
		interpolator := sampleModeInterpolator(strings.TrimSpace(sampleModeSelect.Selected))
		outputFolderPath := strings.TrimSpace(outputFolderEntry.Text)
		if outputFolderPath == "" {
			statusLabel.SetText("Output folder is required.")
			return
		}
		if mkdirErr := os.MkdirAll(outputFolderPath, 0755); mkdirErr != nil {
			statusLabel.SetText(fmt.Sprintf("Failed to create folder: %s", mkdirErr.Error()))
			return
		}

		uploadEnabled := uploadToRobloxCheck.Checked
		var uploadCreator robloxOpenCloudCreator
		apiKey := ""
		assetNameBase := strings.TrimSpace(assetNameEntry.Text)
		description := strings.TrimSpace(descriptionEntry.Text)
		if assetNameBase == "" {
			assetNameBase = defaultAssetNameBase
		}
		if uploadEnabled {
			apiKey = strings.TrimSpace(apiKeyEntry.Text)
			if apiKey == "" {
				statusLabel.SetText("Open Cloud API key is required for upload.")
				return
			}
			creatorID, parseErr := strconv.ParseInt(strings.TrimSpace(creatorIDEntry.Text), 10, 64)
			if parseErr != nil || creatorID <= 0 {
				statusLabel.SetText("Creator ID must be a positive integer for upload.")
				return
			}
			uploadCreator = robloxOpenCloudCreator{
				IsGroup: strings.TrimSpace(creatorTypeSelect.Selected) == uploadCreatorModeGroup,
				ID:      creatorID,
			}
			if rememberAPIKeyCheck.Checked {
				if saveErr := SaveOpenCloudAPIKeyToKeyring(apiKey); saveErr != nil {
					statusLabel.SetText(fmt.Sprintf("Failed to save API key: %s", saveErr.Error()))
					return
				}
			} else if deleteErr := DeleteOpenCloudAPIKeyFromKeyring(); deleteErr != nil {
				statusLabel.SetText(fmt.Sprintf("Failed to clear saved API key: %s", deleteErr.Error()))
				return
			}
		}

		generatedPathsEntry.SetText("")
		localStopSignal := newStopSignal()
		activeStopSignal = localStopSignal
		updateButtons(true)
		if uploadEnabled {
			statusLabel.SetText("Preparing generation and upload...")
		} else {
			statusLabel.SetText("Preparing generation...")
		}

		go func(
			totalCount int,
			outputFolder string,
			patternMode string,
			imageW int,
			imageH int,
			samples []sampleSpec,
			scaler xdraw.Interpolator,
			enableUpload bool,
			creator robloxOpenCloudCreator,
			openCloudAPIKey string,
			baseAssetName string,
			assetDescription string,
		) {
			multiSample := len(samples) > 1
			totalFiles := totalCount * len(samples)
			resultsBySample := make(map[string][]string)
			sampleOrder := make([]string, len(samples))
			for i, spec := range samples {
				resultsBySample[spec.Resolution] = make([]string, 0, totalCount)
				sampleOrder[i] = spec.Resolution
			}

			formatResults := func() string {
				if !multiSample {
					return strings.Join(resultsBySample[sampleOrder[0]], "\n")
				}
				var sb strings.Builder
				for i, label := range sampleOrder {
					if i > 0 {
						sb.WriteString("\n")
					}
					sb.WriteString(fmt.Sprintf("=== %s ===\n", label))
					sb.WriteString(strings.Join(resultsBySample[label], "\n"))
				}
				return sb.String()
			}

			fileIndex := 0
			uploadedCount := 0
			lastPublishedResultsText := ""

			for imageIdx := 0; imageIdx < totalCount; imageIdx++ {
				select {
				case <-localStopSignal.channel:
					fyne.Do(func() {
						finishGeneration(localStopSignal, stoppedImageUploadStatus(fileIndex, uploadedCount, totalFiles, enableUpload))
					})
					return
				default:
				}

				currentImageNum := imageIdx + 1
				if imageIdx == 0 || (imageIdx+1)%uploadUIRefreshEvery == 0 {
					fyne.Do(func() {
						statusLabel.SetText(fmt.Sprintf("Generating image %d/%d...", currentImageNum, totalCount))
					})
				}

				baseImg, compression, generateErr := generateBaseImage(imageW, imageH, patternMode)
				if generateErr != nil {
					fyne.Do(func() {
						failGeneration(localStopSignal, fmt.Sprintf("Generation failed: %s", generateErr.Error()), generateErr)
					})
					return
				}

				timestamp := time.Now().Unix()

				for _, spec := range samples {
					select {
					case <-localStopSignal.channel:
						fyne.Do(func() {
							finishGeneration(localStopSignal, stoppedImageUploadStatus(fileIndex, uploadedCount, totalFiles, enableUpload))
						})
						return
					default:
					}

					imageBytes, encodeErr := sampleAndEncodePNG(baseImg, compression, spec.Numerator, spec.Denominator, scaler)
					if encodeErr != nil {
						fyne.Do(func() {
							failGeneration(localStopSignal, fmt.Sprintf("Encode failed: %s", encodeErr.Error()), encodeErr)
						})
						return
					}

					var fileName string
					if multiSample {
						fileName = fmt.Sprintf("joxblox-%s-%d-%03d-%s.png", patternModeSlug(patternMode), timestamp, currentImageNum, sampleSlug(spec.Label))
					} else {
						fileName = fmt.Sprintf("joxblox-%s-%d-%03d.png", patternModeSlug(patternMode), timestamp, currentImageNum)
					}

					outputPath := filepath.Join(outputFolder, fileName)
					if writeErr := os.WriteFile(outputPath, imageBytes, 0644); writeErr != nil {
						fyne.Do(func() {
							failGeneration(localStopSignal, fmt.Sprintf("Write failed: %s", writeErr.Error()), writeErr)
						})
						return
					}

					resultLine := outputPath
					if enableUpload {
						resolution := spec.Resolution
						fyne.Do(func() {
							if multiSample {
								statusLabel.SetText(fmt.Sprintf("Uploading image %d/%d (%s)...", currentImageNum, totalCount, resolution))
							} else {
								statusLabel.SetText(fmt.Sprintf("Uploading %d/%d...", currentImageNum, totalCount))
							}
						})

						displayName := generatedAssetDisplayName(baseAssetName, currentImageNum)
						if multiSample {
							displayName = fmt.Sprintf("%s (%s)", displayName, spec.Resolution)
						}

						assetID, uploadErr := uploadDecalToRobloxOpenCloud(
							openCloudAPIKey,
							creator,
							displayName,
							assetDescription,
							fileName,
							imageBytes,
							localStopSignal.channel,
						)
						if uploadErr != nil {
							if errors.Is(uploadErr, errScanStopped) {
								fyne.Do(func() {
									finishGeneration(localStopSignal, stoppedImageUploadStatus(fileIndex, uploadedCount, totalFiles, true))
								})
								return
							}
							fyne.Do(func() {
								failGeneration(localStopSignal, fmt.Sprintf("Upload failed: %s", uploadErr.Error()), uploadErr)
							})
							return
						}
						uploadedCount++
						resultLine = strconv.FormatInt(assetID, 10)
					}

					resultsBySample[spec.Resolution] = append(resultsBySample[spec.Resolution], resultLine)
					fileIndex++

					if fileIndex%uploadUIRefreshEvery == 0 || fileIndex == totalFiles {
						lastPublishedResultsText = formatResults()
						pathsText := lastPublishedResultsText
						fyne.Do(func() {
							generatedPathsEntry.SetText(pathsText)
						})
					}
				}
			}

			fyne.Do(func() {
				finishGeneration(localStopSignal, completedImageUploadStatus(fileIndex, uploadedCount, enableUpload))
				if strings.TrimSpace(lastPublishedResultsText) == "" && fileIndex > 0 {
					generatedPathsEntry.SetText(formatResults())
				}
			})
		}(countValue, outputFolderPath, selectedPatternMode, imgWidth, imgHeight, samples, interpolator, uploadEnabled, uploadCreator, apiKey, assetNameBase, description)
	}

	controls := container.NewHBox(
		widget.NewLabel("Count:"),
		container.NewGridWrap(fyne.NewSize(80, 36), countEntry),
		widget.NewLabel("Pattern:"),
		container.NewGridWrap(fyne.NewSize(240, 36), patternSelect),
		widget.NewLabel("Size:"),
		container.NewGridWrap(fyne.NewSize(130, 36), sizeSelect),
		widget.NewLabel("Interpolation:"),
		container.NewGridWrap(fyne.NewSize(160, 36), sampleModeSelect),
		selectFolderButton,
		generateButton,
		stopButton,
	)
	sampleSizesRow := container.NewHBox(
		widget.NewLabel("Samples:"),
		sampleListContainer,
		container.NewGridWrap(fyne.NewSize(100, 36), addSampleSelect),
		addSampleButton,
	)
	infoText := widget.NewLabel(
		"Generates PNG files at the selected size. Optionally uploads each image to Roblox as a decal and lists the returned asset IDs. " +
			"When multiple sample sizes are selected, each image is generated once then downsampled to each size, and results are grouped by sample size. " +
			"Open Cloud upload requires an Assets API key plus the creator user/group ID. " +
			"See the Roblox Assets API docs: https://devforum.roblox.com/t/opencloud-assets-api/2298007",
	)
	infoText.Wrapping = fyne.TextWrapWord
	uploadSettingsCard := widget.NewCard(
		"Roblox Upload",
		"",
		container.NewVBox(
			uploadToRobloxCheck,
			container.NewBorder(nil, nil, widget.NewLabel("API Key:"), container.NewHBox(rememberAPIKeyCheck), apiKeyEntry),
			container.NewGridWithColumns(
				2,
				container.NewBorder(nil, nil, widget.NewLabel("Creator Type:"), nil, creatorTypeSelect),
				container.NewBorder(nil, nil, widget.NewLabel("Creator ID:"), nil, creatorIDEntry),
			),
			container.NewBorder(nil, nil, widget.NewLabel("Asset Name Base:"), nil, assetNameEntry),
			container.NewBorder(nil, nil, widget.NewLabel("Description:"), nil, descriptionEntry),
		),
	)
	updateUploadControls()

	uploadWarning := canvas.NewText("⚠ Please do not use your main account for this as you could be banned, please use an alt that you don't care about!", color.RGBA{R: 220, G: 40, B: 40, A: 255})
	uploadWarning.TextSize = 12
	uploadWarning.TextStyle = fyne.TextStyle{Bold: true}

	return container.NewVBox(
		infoText,
		container.NewBorder(nil, nil, widget.NewLabel("Output Folder:"), nil, outputFolderEntry),
		uploadSettingsCard,
		uploadWarning,
		controls,
		sampleSizesRow,
		statusLabel,
		widget.NewLabel("Generated File Paths / Uploaded Asset IDs:"),
		generatedPathsEntry,
	)
}

func generatedAssetDisplayName(baseName string, index int) string {
	trimmedBaseName := strings.TrimSpace(baseName)
	if trimmedBaseName == "" {
		trimmedBaseName = defaultAssetNameBase
	}
	return fmt.Sprintf("%s %03d", trimmedBaseName, index)
}

func stoppedImageUploadStatus(createdCount int, uploadedCount int, totalCount int, uploadEnabled bool) string {
	if uploadEnabled {
		return fmt.Sprintf(
			"Generation stopped. Created %d/%d images and uploaded %d asset IDs.",
			createdCount,
			totalCount,
			uploadedCount,
		)
	}
	return fmt.Sprintf("Generation stopped. Created %d/%d images.", createdCount, totalCount)
}

func completedImageUploadStatus(createdCount int, uploadedCount int, uploadEnabled bool) string {
	if uploadEnabled {
		return fmt.Sprintf(
			"Generation complete. Created %d images and uploaded %d asset IDs.",
			createdCount,
			uploadedCount,
		)
	}
	return fmt.Sprintf("Generation complete. Created %d images.", createdCount)
}

func generateBaseImage(width, height int, patternMode string) (*image.RGBA, png.CompressionLevel, error) {
	if patternMode == patternModeCheckered {
		img, err := generateCheckeredImage(width, height)
		return img, png.NoCompression, err
	}
	img, err := generateNoiseImage(width, height)
	return img, png.BestCompression, err
}

func sampleAndEncodePNG(img *image.RGBA, compression png.CompressionLevel, sampleNum, sampleDen int, scaler xdraw.Interpolator) ([]byte, error) {
	sampled := img
	if sampleNum != sampleDen {
		sampled = downsampleImage(img, sampleNum, sampleDen, scaler)
	}
	return encodePNG(sampled, compression)
}

func generatePNGBytes(width, height int, patternMode string, sampleNum, sampleDen int, scaler xdraw.Interpolator) ([]byte, error) {
	img, compression, err := generateBaseImage(width, height, patternMode)
	if err != nil {
		return nil, err
	}
	return sampleAndEncodePNG(img, compression, sampleNum, sampleDen, scaler)
}

func generateNoiseImage(width, height int) (*image.RGBA, error) {
	randomImage := image.NewRGBA(image.Rect(0, 0, width, height))
	if _, readErr := rand.Read(randomImage.Pix); readErr != nil {
		return nil, readErr
	}
	return randomImage, nil
}

func generateCheckeredImage(width, height int) (*image.RGBA, error) {
	checkeredImage := image.NewRGBA(image.Rect(0, 0, width, height))
	randomParams := make([]byte, 7)
	if _, readErr := rand.Read(randomParams); readErr != nil {
		return nil, readErr
	}

	tileSize := 8 + int(randomParams[0]%56) // 8..63
	colorA := [3]uint8{randomParams[1], randomParams[2], randomParams[3]}
	colorB := [3]uint8{randomParams[4], randomParams[5], randomParams[6]}
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			useA := ((x / tileSize) + (y / tileSize)) % 2
			pixelOffset := checkeredImage.PixOffset(x, y)
			if useA == 0 {
				checkeredImage.Pix[pixelOffset+0] = colorA[0]
				checkeredImage.Pix[pixelOffset+1] = colorA[1]
				checkeredImage.Pix[pixelOffset+2] = colorA[2]
			} else {
				checkeredImage.Pix[pixelOffset+0] = colorB[0]
				checkeredImage.Pix[pixelOffset+1] = colorB[1]
				checkeredImage.Pix[pixelOffset+2] = colorB[2]
			}
			checkeredImage.Pix[pixelOffset+3] = 255
		}
	}
	return checkeredImage, nil
}

func downsampleImage(src *image.RGBA, numerator, denominator int, scaler xdraw.Interpolator) *image.RGBA {
	srcBounds := src.Bounds()
	newW := srcBounds.Dx() * numerator / denominator
	newH := srcBounds.Dy() * numerator / denominator
	if newW < 1 {
		newW = 1
	}
	if newH < 1 {
		newH = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, newW, newH))
	scaler.Scale(dst, dst.Bounds(), src, srcBounds, xdraw.Over, nil)
	return dst
}

func encodePNG(img image.Image, compression png.CompressionLevel) ([]byte, error) {
	var buf bytes.Buffer
	encoder := png.Encoder{CompressionLevel: compression}
	if err := encoder.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func patternModeSlug(patternMode string) string {
	if patternMode == patternModeCheckered {
		return "checkered"
	}
	return "noise"
}

func sampleSlug(label string) string {
	switch label {
	case "Original":
		return "original"
	case "3/4":
		return "3q"
	case "1/2":
		return "half"
	case "1/4":
		return "quarter"
	case "1/8":
		return "eighth"
	default:
		return strings.ReplaceAll(strings.ToLower(label), "/", "-")
	}
}
