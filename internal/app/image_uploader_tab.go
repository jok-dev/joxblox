package app

import (
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	fyneDialog "fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
	nativeDialog "github.com/sqweek/dialog"
)

const (
	uploadImageWidth        = 1024
	uploadImageHeight       = 1024
	uploadDefaultImageCount = 10
	uploadUIRefreshEvery    = 5
	patternModeNoise        = "Random Noise (largest)"
	patternModeCheckered    = "Random Checkered (smaller)"
)

func newImageUploaderTab(window fyne.Window) fyne.CanvasObject {
	countEntry := widget.NewEntry()
	countEntry.SetText(strconv.Itoa(uploadDefaultImageCount))
	countEntry.SetPlaceHolder(strconv.Itoa(uploadDefaultImageCount))
	patternSelect := widget.NewSelect([]string{patternModeNoise, patternModeCheckered}, nil)
	patternSelect.SetSelected(patternModeNoise)

	outputFolderEntry := widget.NewEntry()
	defaultOutputFolder := filepath.Join(".", "generated_images")
	outputFolderEntry.SetText(defaultOutputFolder)
	outputFolderEntry.SetPlaceHolder(defaultOutputFolder)

	statusLabel := widget.NewLabel("Set count/folder and click Generate Images.")
	generatedPathsEntry := widget.NewMultiLineEntry()
	generatedPathsEntry.SetPlaceHolder("Generated file paths will appear here...")
	generatedPathsEntry.Disable()
	generatedPathsEntry.Wrapping = fyne.TextWrapBreak
	generatedPathsEntry.SetMinRowsVisible(10)

	generateButton := widget.NewButton("Generate Images", nil)
	selectFolderButton := widget.NewButton("Select Folder", nil)
	stopButton := widget.NewButton("Stop", nil)
	stopButton.Disable()

	var inProgress bool
	var activeStopSignal *stopSignal
	updateButtons := func(running bool) {
		inProgress = running
		if running {
			generateButton.Disable()
			selectFolderButton.Disable()
			stopButton.Enable()
			countEntry.Disable()
			patternSelect.Disable()
			outputFolderEntry.Disable()
			return
		}
		generateButton.Enable()
		selectFolderButton.Enable()
		stopButton.Disable()
		countEntry.Enable()
		patternSelect.Enable()
		outputFolderEntry.Enable()
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
		statusLabel.SetText("Stopping generation...")
		stopButton.Disable()
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
		outputFolderPath := strings.TrimSpace(outputFolderEntry.Text)
		if outputFolderPath == "" {
			statusLabel.SetText("Output folder is required.")
			return
		}
		if mkdirErr := os.MkdirAll(outputFolderPath, 0755); mkdirErr != nil {
			statusLabel.SetText(fmt.Sprintf("Failed to create folder: %s", mkdirErr.Error()))
			return
		}

		generatedPathsEntry.SetText("")
		localStopSignal := newStopSignal()
		activeStopSignal = localStopSignal
		updateButtons(true)
		statusLabel.SetText("Preparing generation...")

		go func(totalCount int, outputFolder string, patternMode string) {
			generatedPaths := make([]string, 0, totalCount)
			lastPublishedPathsText := ""
			for index := 0; index < totalCount; index++ {
				select {
				case <-localStopSignal.channel:
					fyne.Do(func() {
						finishGeneration(
							localStopSignal,
							fmt.Sprintf("Generation stopped. Created %d/%d images.", len(generatedPaths), totalCount),
						)
					})
					return
				default:
				}

				if index == 0 || (index+1)%uploadUIRefreshEvery == 0 {
					currentIndex := index + 1
					fyne.Do(func() {
						statusLabel.SetText(fmt.Sprintf("Generating %d/%d...", currentIndex, totalCount))
					})
				}

				imageBytes, generateErr := generatePNGBytes(uploadImageWidth, uploadImageHeight, patternMode)
				if generateErr != nil {
					fyne.Do(func() {
						failGeneration(localStopSignal, fmt.Sprintf("Generation failed: %s", generateErr.Error()), generateErr)
					})
					return
				}

				fileName := fmt.Sprintf("joxblox-%s-%d-%03d.png", patternModeSlug(patternMode), time.Now().Unix(), index+1)
				outputPath := filepath.Join(outputFolder, fileName)
				if writeErr := os.WriteFile(outputPath, imageBytes, 0644); writeErr != nil {
					fyne.Do(func() {
						failGeneration(
							localStopSignal,
							fmt.Sprintf("Write failed on item %d: %s", index+1, writeErr.Error()),
							writeErr,
						)
					})
					return
				}

				generatedPaths = append(generatedPaths, outputPath)
				if (index+1)%uploadUIRefreshEvery == 0 || index+1 == totalCount {
					lastPublishedPathsText = strings.Join(generatedPaths, "\n")
					pathsText := lastPublishedPathsText
					fyne.Do(func() {
						generatedPathsEntry.SetText(pathsText)
					})
				}
			}

			fyne.Do(func() {
				finishGeneration(localStopSignal, fmt.Sprintf("Generation complete. Created %d images.", len(generatedPaths)))
				if strings.TrimSpace(lastPublishedPathsText) == "" && len(generatedPaths) > 0 {
					generatedPathsEntry.SetText(strings.Join(generatedPaths, "\n"))
				}
			})
		}(countValue, outputFolderPath, selectedPatternMode)
	}

	controls := container.NewHBox(
		widget.NewLabel("Count:"),
		container.NewGridWrap(fyne.NewSize(80, 36), countEntry),
		widget.NewLabel("Pattern:"),
		container.NewGridWrap(fyne.NewSize(240, 36), patternSelect),
		selectFolderButton,
		generateButton,
		stopButton,
	)
	infoText := widget.NewLabel("Generates 1024x1024 PNG files for manual Roblox upload.")
	infoText.Wrapping = fyne.TextWrapWord

	return container.NewVBox(
		infoText,
		container.NewBorder(nil, nil, widget.NewLabel("Output Folder:"), nil, outputFolderEntry),
		controls,
		statusLabel,
		widget.NewLabel("Generated File Paths:"),
		generatedPathsEntry,
	)
}

func generatePNGBytes(width int, height int, patternMode string) ([]byte, error) {
	if patternMode == patternModeCheckered {
		return generateCheckeredPNGBytes(width, height)
	}
	return generateNoisePNGBytes(width, height)
}

func generateNoisePNGBytes(width int, height int) ([]byte, error) {
	randomImage := image.NewRGBA(image.Rect(0, 0, width, height))
	if _, readErr := rand.Read(randomImage.Pix); readErr != nil {
		return nil, readErr
	}

	var outputBuffer bytes.Buffer
	encoder := png.Encoder{CompressionLevel: png.BestCompression}
	if encodeErr := encoder.Encode(&outputBuffer, randomImage); encodeErr != nil {
		return nil, encodeErr
	}
	return outputBuffer.Bytes(), nil
}

func generateCheckeredPNGBytes(width int, height int) ([]byte, error) {
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

	var outputBuffer bytes.Buffer
	encoder := png.Encoder{CompressionLevel: png.NoCompression}
	if encodeErr := encoder.Encode(&outputBuffer, checkeredImage); encodeErr != nil {
		return nil, encodeErr
	}
	return outputBuffer.Bytes(), nil
}

func patternModeSlug(patternMode string) string {
	if patternMode == patternModeCheckered {
		return "checkered"
	}
	return "noise"
}
