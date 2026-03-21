package app

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	fyneDialog "fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
	nativeDialog "github.com/sqweek/dialog"
)

const (
	defaultScanLimit = 100
)

type scanResult struct {
	AssetID           int64
	FilePath          string
	Source            string
	State             string
	Width             int
	Height            int
	BytesSize         int
	Format            string
	ContentType       string
	Warning           bool
	WarningCause      string
	AssetDeliveryJSON string
	ThumbnailJSON     string
	Resource          *fyne.StaticResource
}

func createFolderScanTab(window fyne.Window) fyne.CanvasObject {
	selectedFolderPath := ""
	results := []scanResult{}
	columnHeaders := []string{"Asset ID", "Size MB", "Dimensions", "State", "Source"}
	sortField := "Size MB"
	sortDescending := true

	folderLabel := widget.NewLabel("No folder selected.")
	limitEntry := widget.NewEntry()
	limitEntry.SetText(strconv.Itoa(defaultScanLimit))
	limitEntry.SetPlaceHolder("100")
	statusLabel := widget.NewLabel("Select a folder and click Start Scan.")

	previewImage := canvas.NewImageFromImage(nil)
	previewImage.FillMode = canvas.ImageFillContain
	previewImage.SetMinSize(fyne.NewSize(previewWidth, previewHeight))
	previewPlaceholder := widget.NewLabel("Select a result row to preview")
	previewBox := container.NewMax(
		container.NewCenter(previewPlaceholder),
		container.NewCenter(previewImage),
	)

	previewAssetID := widget.NewLabel("-")
	previewDimensions := widget.NewLabel("-")
	previewSize := widget.NewLabel("-")
	previewFormat := widget.NewLabel("-")
	previewContentType := widget.NewLabel("-")
	previewState := widget.NewLabel("-")
	previewSource := widget.NewLabel("-")
	previewFailureReason := widget.NewLabel("-")
	previewFailureReason.Wrapping = fyne.TextWrapWord
	previewAssetDeliveryJSON := widget.NewMultiLineEntry()
	previewAssetDeliveryJSON.SetText("-")
	previewAssetDeliveryJSON.Disable()
	previewAssetDeliveryJSON.SetMinRowsVisible(6)
	previewThumbnailJSON := widget.NewMultiLineEntry()
	previewThumbnailJSON.SetText("-")
	previewThumbnailJSON.Disable()
	previewThumbnailJSON.SetMinRowsVisible(6)
	previewJSONAccordion := widget.NewAccordion(
		widget.NewAccordionItem(
			"API JSON Responses",
			container.NewVBox(
				widget.NewLabel("AssetDelivery JSON:"),
				previewAssetDeliveryJSON,
				widget.NewLabel("Thumbnail JSON:"),
				previewThumbnailJSON,
			),
		),
	)
	previewFile := widget.NewLabel("-")
	previewFile.Wrapping = fyne.TextWrapWord
	previewNote := widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Italic: true})
	previewNote.Importance = widget.DangerImportance
	previewNote.Wrapping = fyne.TextWrapWord
	previewNote.Hide()

	clearPreview := func() {
		previewImage.Resource = nil
		previewImage.Refresh()
		previewPlaceholder.Show()
		previewAssetID.SetText("-")
		previewDimensions.SetText("-")
		previewSize.SetText("-")
		previewFormat.SetText("-")
		previewContentType.SetText("-")
		previewState.SetText("-")
		previewSource.SetText("-")
		previewFailureReason.SetText("-")
		previewAssetDeliveryJSON.SetText("-")
		previewThumbnailJSON.SetText("-")
		previewFile.SetText("-")
		previewState.Importance = widget.MediumImportance
		previewSource.Importance = widget.MediumImportance
		previewState.Refresh()
		previewSource.Refresh()
		previewNote.Hide()
		previewNote.SetText("")
	}
	clearPreview()

	var table *widget.Table

	applySort := func() {
		sort.Slice(results, func(leftIndex int, rightIndex int) bool {
			leftResult := results[leftIndex]
			rightResult := results[rightIndex]
			compareResult := compareScanResults(leftResult, rightResult, sortField)
			if compareResult == 0 {
				return leftResult.AssetID < rightResult.AssetID
			}

			if sortDescending {
				return compareResult > 0
			}
			return compareResult < 0
		})
		if table != nil {
			table.Refresh()
		}
	}

	updatePreviewFromRow := func(rowIndex int) {
		if rowIndex < 0 || rowIndex >= len(results) {
			return
		}

		selectedResult := results[rowIndex]
		previewAssetID.SetText(strconv.FormatInt(selectedResult.AssetID, 10))
		previewDimensions.SetText(fmt.Sprintf("%dx%d", selectedResult.Width, selectedResult.Height))
		previewSize.SetText(fmt.Sprintf("%.2f MB", float64(selectedResult.BytesSize)/megabyte))
		previewFormat.SetText(selectedResult.Format)
		previewContentType.SetText(selectedResult.ContentType)
		previewState.SetText(selectedResult.State)
		previewSource.SetText(selectedResult.Source)
		if selectedResult.WarningCause != "" {
			previewFailureReason.SetText(selectedResult.WarningCause)
		} else {
			previewFailureReason.SetText("-")
		}
		if selectedResult.AssetDeliveryJSON != "" {
			previewAssetDeliveryJSON.SetText(selectedResult.AssetDeliveryJSON)
		} else {
			previewAssetDeliveryJSON.SetText("-")
		}
		if selectedResult.ThumbnailJSON != "" {
			previewThumbnailJSON.SetText(selectedResult.ThumbnailJSON)
		} else {
			previewThumbnailJSON.SetText("-")
		}
		previewFile.SetText(selectedResult.FilePath)
		previewState.Importance = widget.MediumImportance
		previewSource.Importance = widget.MediumImportance
		previewState.Refresh()
		previewSource.Refresh()
		previewNote.Hide()
		previewNote.SetText("")

		if selectedResult.Warning {
			previewState.SetText(fmt.Sprintf("⚠ %s", selectedResult.State))
			previewState.Importance = widget.DangerImportance
			previewState.Refresh()
		}
		if selectedResult.WarningCause != "" {
			previewNote.SetText(buildFallbackWarningText(selectedResult.WarningCause))
			previewNote.Show()
		}
		if strings.EqualFold(selectedResult.Source, "Thumbnails API (Fallback)") {
			previewSource.SetText(fmt.Sprintf("⚠ %s", selectedResult.Source))
			previewSource.Importance = widget.DangerImportance
			previewSource.Refresh()
		}

		previewImage.Resource = selectedResult.Resource
		previewImage.Refresh()
		previewPlaceholder.Hide()
	}

	table = widget.NewTableWithHeaders(
		func() (int, int) {
			return len(results), len(columnHeaders)
		},
		func() fyne.CanvasObject {
			return widget.NewLabel("")
		},
		func(id widget.TableCellID, object fyne.CanvasObject) {
			label := object.(*widget.Label)
			if id.Row < 0 || id.Row >= len(results) || id.Col < 0 || id.Col >= len(columnHeaders) {
				label.SetText("")
				return
			}

			row := results[id.Row]
			label.Importance = widget.MediumImportance
			switch id.Col {
			case 0:
				label.SetText(strconv.FormatInt(row.AssetID, 10))
			case 1:
				label.SetText(fmt.Sprintf("%.2f", float64(row.BytesSize)/megabyte))
			case 2:
				label.SetText(fmt.Sprintf("%dx%d", row.Width, row.Height))
			case 3:
				if strings.EqualFold(row.Source, "Thumbnails API (Fallback)") && !strings.EqualFold(row.State, "Completed") {
					label.SetText(fmt.Sprintf("⚠ %s", row.State))
					label.Importance = widget.DangerImportance
				} else {
					label.SetText(row.State)
				}
			case 4:
				if strings.EqualFold(row.Source, "Thumbnails API (Fallback)") {
					label.SetText(fmt.Sprintf("⚠ %s", row.Source))
					label.Importance = widget.DangerImportance
				} else {
					label.SetText(row.Source)
				}
			default:
				label.SetText("")
			}
			label.Refresh()
		},
	)
	table.CreateHeader = func() fyne.CanvasObject {
		return widget.NewButton("", nil)
	}
	table.UpdateHeader = func(id widget.TableCellID, object fyne.CanvasObject) {
		headerButton := object.(*widget.Button)
		if id.Row == -1 && id.Col >= 0 && id.Col < len(columnHeaders) {
			columnName := columnHeaders[id.Col]
			if sortField == columnName {
				sortIcon := "▲"
				if sortDescending {
					sortIcon = "▼"
				}
				headerButton.SetText(fmt.Sprintf("%s %s", columnName, sortIcon))
			} else {
				headerButton.SetText(columnName)
			}

			headerButton.OnTapped = func() {
				if sortField == columnName {
					sortDescending = !sortDescending
				} else {
					sortField = columnName
					sortDescending = true
				}
				applySort()
			}
			return
		}

		if id.Col == -1 && id.Row >= 0 {
			headerButton.SetText(strconv.Itoa(id.Row + 1))
		} else {
			headerButton.SetText("")
		}
		headerButton.OnTapped = nil
	}
	table.OnSelected = func(id widget.TableCellID) {
		if id.Row < 0 {
			return
		}
		updatePreviewFromRow(id.Row)
	}
	table.SetColumnWidth(0, 140)
	table.SetColumnWidth(1, 90)
	table.SetColumnWidth(2, 120)
	table.SetColumnWidth(3, 130)
	table.SetColumnWidth(4, 280)

	var scanInProgress bool
	var stopScanChannel chan struct{}
	scanButton := widget.NewButton("Start Scan", nil)
	selectFolderButton := widget.NewButton("Select Folder", func() {
		selectedPath, pickerErr := nativeDialog.Directory().Title("Select folder to scan").Browse()
		if pickerErr == nil {
			selectedFolderPath = selectedPath
			folderLabel.SetText(selectedFolderPath)
			return
		}

		if errors.Is(pickerErr, nativeDialog.Cancelled) {
			return
		}

		// Fallback to Fyne picker if native dialog fails unexpectedly.
		fyneDialog.ShowFolderOpen(func(uri fyne.ListableURI, err error) {
			if err != nil {
				statusLabel.SetText(fmt.Sprintf("Folder picker failed: %s", err.Error()))
				return
			}
			if uri == nil {
				return
			}

			selectedFolderPath = uri.Path()
			folderLabel.SetText(selectedFolderPath)
		}, window)
	})

	updateScanControls := func(inProgress bool) {
		scanInProgress = inProgress
		if inProgress {
			scanButton.SetText("Stop Scan")
			selectFolderButton.Disable()
			limitEntry.Disable()
		} else {
			scanButton.SetText("Start Scan")
			selectFolderButton.Enable()
			limitEntry.Enable()
		}
	}

	scanButton.OnTapped = func() {
		if scanInProgress {
			if stopScanChannel != nil {
				close(stopScanChannel)
			}
			statusLabel.SetText("Stopping scan...")
			scanButton.Disable()
			return
		}

		if selectedFolderPath == "" {
			statusLabel.SetText("Please select a folder first.")
			return
		}

		limitValue, err := strconv.Atoi(strings.TrimSpace(limitEntry.Text))
		if err != nil || limitValue <= 0 {
			statusLabel.SetText("Max results must be a positive number.")
			return
		}

		results = []scanResult{}
		table.Refresh()
		clearPreview()
		statusLabel.SetText("Scanning files for Roblox asset IDs...")
		localStopScanChannel := make(chan struct{})
		stopScanChannel = localStopScanChannel
		updateScanControls(true)

		go func() {
			hits, scanErr := scanFolderForAssetIDs(selectedFolderPath, limitValue, localStopScanChannel)
			if scanErr != nil {
				fyne.Do(func() {
					updateScanControls(false)
					if stopScanChannel == localStopScanChannel {
						stopScanChannel = nil
					}
					scanButton.Enable()
					if errors.Is(scanErr, errScanStopped) {
						statusLabel.SetText(fmt.Sprintf("Scan stopped. Loaded %d assets.", len(results)))
						return
					}
					statusLabel.SetText(fmt.Sprintf("Scan failed: %s", scanErr.Error()))
				})
				return
			}

			if len(hits) == 0 {
				fyne.Do(func() {
					updateScanControls(false)
					if stopScanChannel == localStopScanChannel {
						stopScanChannel = nil
					}
					scanButton.Enable()
					if errors.Is(scanErr, errScanStopped) {
						statusLabel.SetText("Scan stopped.")
					} else {
						statusLabel.SetText("No Roblox asset IDs found in text files.")
					}
				})
				return
			}

			for hitIndex, hit := range hits {
				select {
				case <-localStopScanChannel:
					fyne.Do(func() {
						updateScanControls(false)
						if stopScanChannel == localStopScanChannel {
							stopScanChannel = nil
						}
						scanButton.Enable()
						statusLabel.SetText(fmt.Sprintf("Scan stopped. Loaded %d assets.", len(results)))
					})
					return
				default:
				}

				image, source, state, warningMessage, assetDeliveryRawJSON, thumbnailRawJSON, loadErr := loadBestImageInfo(hit.AssetID)
				if loadErr != nil {
					continue
				}

				warning := strings.EqualFold(source, "Thumbnails API (Fallback)") && !strings.EqualFold(state, "Completed")
				scanRow := scanResult{
					AssetID:           hit.AssetID,
					FilePath:          hit.FilePath,
					Source:            source,
					State:             state,
					Width:             image.Width,
					Height:            image.Height,
					BytesSize:         image.BytesSize,
					Format:            image.Format,
					ContentType:       image.ContentType,
					Warning:           warning,
					WarningCause:      warningMessage,
					AssetDeliveryJSON: assetDeliveryRawJSON,
					ThumbnailJSON:     thumbnailRawJSON,
					Resource:          image.Resource,
				}

				fyne.Do(func() {
					results = append(results, scanRow)
					applySort()
					statusLabel.SetText(fmt.Sprintf("Loaded %d/%d assets...", hitIndex+1, len(hits)))
				})
			}

			fyne.Do(func() {
				updateScanControls(false)
				if stopScanChannel == localStopScanChannel {
					stopScanChannel = nil
				}
				scanButton.Enable()
				statusLabel.SetText(fmt.Sprintf("Scan complete. Showing %d assets.", len(results)))
			})
		}()
	}

	controls := container.NewVBox(
		container.NewHBox(
			selectFolderButton,
			widget.NewLabel("Max results:"),
			container.NewGridWrap(fyne.NewSize(80, 36), limitEntry),
			scanButton,
		),
		folderLabel,
	)

	previewMetadata := container.New(layout.NewFormLayout(),
		widget.NewLabel("Asset ID:"), previewAssetID,
		widget.NewLabel("Dimensions:"), previewDimensions,
		widget.NewLabel("Size:"), previewSize,
		widget.NewLabel("Format:"), previewFormat,
		widget.NewLabel("Content-Type:"), previewContentType,
		widget.NewLabel("State:"), previewState,
		widget.NewLabel("Source:"), previewSource,
		widget.NewLabel("Failure Reason:"), previewFailureReason,
		widget.NewLabel("File:"), previewFile,
	)
	previewContent := container.NewVBox(previewBox, previewMetadata, previewJSONAccordion, previewNote)
	previewScroll := container.NewVScroll(previewContent)
	previewPanel := container.NewBorder(
		nil,
		nil,
		nil,
		nil,
		previewScroll,
	)

	split := container.NewHSplit(table, previewPanel)
	split.Offset = 0.62

	return container.NewBorder(
		controls,
		nil,
		nil,
		nil,
		container.NewBorder(statusLabel, nil, nil, nil, split),
	)
}

func compareScanResults(leftResult scanResult, rightResult scanResult, sortField string) int {
	switch sortField {
	case "Asset ID":
		return compareInt64(leftResult.AssetID, rightResult.AssetID)
	case "Width":
		return compareInt(leftResult.Width, rightResult.Width)
	case "Height":
		return compareInt(leftResult.Height, rightResult.Height)
	case "Dimensions":
		leftArea := leftResult.Width * leftResult.Height
		rightArea := rightResult.Width * rightResult.Height
		return compareInt(leftArea, rightArea)
	case "State":
		return strings.Compare(leftResult.State, rightResult.State)
	case "Source":
		return strings.Compare(leftResult.Source, rightResult.Source)
	default:
		return compareInt(leftResult.BytesSize, rightResult.BytesSize)
	}
}

func compareInt(left int, right int) int {
	if left < right {
		return -1
	}
	if left > right {
		return 1
	}
	return 0
}

func compareInt64(left int64, right int64) int {
	if left < right {
		return -1
	}
	if left > right {
		return 1
	}
	return 0
}
