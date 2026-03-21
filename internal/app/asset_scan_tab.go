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
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

const (
	defaultAssetScanLimit  = 100
	scanTableEmojiTextSize = 18
	failedScanRowState     = "Failed"
	failedScanRowSource    = "Load Failed"
)

type assetScanTabOptions struct {
	NoSourceSelectedText    string
	SelectButtonText        string
	ReadyStatusText         string
	MissingSourceStatusText string
	ScanningStatusText      string
	NoResultsStatusText     string
	MaxResultsDefault       int
	SelectSource            func(window fyne.Window, onSelected func(string), onError func(error))
	ExtractHits             func(sourcePath string, limit int, stopChannel <-chan struct{}) ([]scanHit, error)
}

func newAssetScanTab(window fyne.Window, options assetScanTabOptions) fyne.CanvasObject {
	selectedSourcePath := ""
	results := []scanResult{}
	columnHeaders := []string{"Asset ID", "Type", "Self Size MB", "Dimensions", "State", "Source"}
	sortField := "Self Size MB"
	sortDescending := true
	maxResultsDefault := options.MaxResultsDefault
	if maxResultsDefault <= 0 {
		maxResultsDefault = defaultAssetScanLimit
	}

	sourceLabel := widget.NewLabel(options.NoSourceSelectedText)
	limitEntry := widget.NewEntry()
	limitEntry.SetText(strconv.Itoa(maxResultsDefault))
	limitEntry.SetPlaceHolder(strconv.Itoa(maxResultsDefault))
	statusLabel := widget.NewLabel("Select a source and click Start Scan.")

	assetDetailsView := newAssetView("Select a result row to preview", true)
	clearPreview := func() {
		assetDetailsView.Clear()
	}
	clearPreview()
	var explorerState *assetExplorerState
	var renderSelectedAsset func(selectedAssetID int64, selectedFilePath string, previewResult *assetPreviewResult)
	renderSelectedAsset = func(selectedAssetID int64, selectedFilePath string, previewResult *assetPreviewResult) {
		filePath := ""
		if explorerState != nil && selectedAssetID == explorerState.rootAssetID {
			filePath = selectedFilePath
		}
		assetDetailsView.SetData(
			selectedAssetID,
			filePath,
			previewResult.Image,
			previewResult.Stats,
			previewResult.TotalBytesSize,
			previewResult.Source,
			previewResult.State,
			previewResult.WarningMessage,
			previewResult.AssetDeliveryJSON,
			previewResult.ThumbnailJSON,
			previewResult.EconomyJSON,
			previewResult.RustExtractorJSON,
			previewResult.ReferencedAssetIDs,
			previewResult.AssetTypeID,
			previewResult.AssetTypeName,
		)
		assetDetailsView.SetHierarchy(explorerState.getRows(), selectedAssetID, func(assetID int64) {
			if explorerState == nil {
				return
			}
			statusLabel.SetText(fmt.Sprintf("Loading asset %d...", assetID))
			go func() {
				selectedPreview, selectErr := explorerState.selectAsset(assetID)
				fyne.Do(func() {
					if selectErr != nil {
						statusLabel.SetText(selectErr.Error())
						return
					}
					renderSelectedAsset(assetID, selectedFilePath, selectedPreview)
					statusLabel.SetText(fmt.Sprintf("Showing asset %d.", assetID))
				})
			}()
		})
	}

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
		rootPreview := &assetPreviewResult{
			Image: &imageInfo{
				Resource: selectedResult.Resource,
			},
			Stats: &imageInfo{
				Width:       selectedResult.Width,
				Height:      selectedResult.Height,
				BytesSize:   selectedResult.BytesSize,
				Format:      selectedResult.Format,
				ContentType: selectedResult.ContentType,
			},
			ReferencedAssetIDs: selectedResult.ReferencedAssetIDs,
			ChildAssets:        selectedResult.ChildAssets,
			TotalBytesSize:     selectedResult.TotalBytesSize,
			Source:             selectedResult.Source,
			State:              selectedResult.State,
			WarningMessage:     selectedResult.WarningCause,
			AssetDeliveryJSON:  selectedResult.AssetDeliveryJSON,
			ThumbnailJSON:      selectedResult.ThumbnailJSON,
			EconomyJSON:        selectedResult.EconomyJSON,
			RustExtractorJSON:  selectedResult.RustExtractorJSON,
			AssetTypeID:        selectedResult.AssetTypeID,
			AssetTypeName:      selectedResult.AssetTypeName,
		}
		explorerState = newAssetExplorerState(selectedResult.AssetID, rootPreview)
		renderSelectedAsset(selectedResult.AssetID, selectedResult.FilePath, rootPreview)
	}

	table = widget.NewTableWithHeaders(
		func() (int, int) {
			return len(results), len(columnHeaders)
		},
		func() fyne.CanvasObject {
			emojiText := canvas.NewText("", theme.ForegroundColor())
			emojiText.TextSize = scanTableEmojiTextSize
			cellLabel := widget.NewLabel("")
			return container.NewHBox(emojiText, cellLabel)
		},
		func(id widget.TableCellID, object fyne.CanvasObject) {
			cellContainer, isContainer := object.(*fyne.Container)
			if !isContainer || len(cellContainer.Objects) < 2 {
				return
			}
			emojiText, isEmojiText := cellContainer.Objects[0].(*canvas.Text)
			label, isLabel := cellContainer.Objects[1].(*widget.Label)
			if !isEmojiText || !isLabel {
				return
			}
			if id.Row < 0 || id.Row >= len(results) || id.Col < 0 || id.Col >= len(columnHeaders) {
				emojiText.Text = ""
				emojiText.Refresh()
				label.SetText("")
				return
			}

			row := results[id.Row]
			emojiText.Text = ""
			emojiText.Refresh()
			label.Importance = widget.MediumImportance
			switch id.Col {
			case 0:
				label.SetText(strconv.FormatInt(row.AssetID, 10))
			case 1:
				emojiText.Text = getAssetTypeEmoji(row.AssetTypeID)
				emojiText.Refresh()
				if row.AssetTypeID > 0 {
					label.SetText(fmt.Sprintf("%s (%d)", row.AssetTypeName, row.AssetTypeID))
				} else {
					label.SetText(row.AssetTypeName)
				}
			case 2:
				label.SetText(fmt.Sprintf("%.2f", float64(row.BytesSize)/megabyte))
			case 3:
				if row.Width > 0 && row.Height > 0 {
					label.SetText(fmt.Sprintf("%dx%d", row.Width, row.Height))
				} else {
					label.SetText("-")
				}
			case 4:
				if row.State == failedScanRowState {
					label.SetText(fmt.Sprintf("⚠ %s", row.State))
					label.Importance = widget.DangerImportance
				} else if isThumbnailFallback(row.Source) && !isCompletedState(row.State) {
					label.SetText(fmt.Sprintf("⚠ %s", row.State))
					label.Importance = widget.DangerImportance
				} else {
					label.SetText(row.State)
				}
			case 5:
				if row.Source == failedScanRowSource {
					label.SetText(fmt.Sprintf("⚠ %s", row.Source))
					label.Importance = widget.DangerImportance
				} else if isThumbnailFallback(row.Source) {
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
	table.SetColumnWidth(1, 190)
	table.SetColumnWidth(2, 90)
	table.SetColumnWidth(3, 120)
	table.SetColumnWidth(4, 130)
	table.SetColumnWidth(5, 280)

	var scanInProgress bool
	var stopScanChannel chan struct{}
	scanButton := widget.NewButton("Start Scan", nil)
	selectSourceButton := widget.NewButton(options.SelectButtonText, func() {
		options.SelectSource(
			window,
			func(selectedPath string) {
				selectedSourcePath = selectedPath
				sourceLabel.SetText(selectedSourcePath)
				statusLabel.SetText(options.ReadyStatusText)
			},
			func(err error) {
				if err == nil {
					return
				}
				statusLabel.SetText(fmt.Sprintf("Source picker failed: %s", err.Error()))
			},
		)
	})

	updateScanControls := func(inProgress bool) {
		scanInProgress = inProgress
		if inProgress {
			scanButton.SetText("Stop Scan")
			selectSourceButton.Disable()
			limitEntry.Disable()
		} else {
			scanButton.SetText("Start Scan")
			selectSourceButton.Enable()
			limitEntry.Enable()
		}
	}

	scanButton.OnTapped = func() {
		if scanInProgress {
			logDebugf("Scan stop requested")
			if stopScanChannel != nil {
				close(stopScanChannel)
			}
			statusLabel.SetText("Stopping scan...")
			scanButton.Disable()
			return
		}

		if selectedSourcePath == "" {
			statusLabel.SetText(options.MissingSourceStatusText)
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
		logDebugf("Scan started for source: %s (limit=%d)", selectedSourcePath, limitValue)
		statusLabel.SetText(options.ScanningStatusText)
		localStopScanChannel := make(chan struct{})
		stopScanChannel = localStopScanChannel
		updateScanControls(true)

		go func() {
			hits, scanErr := options.ExtractHits(selectedSourcePath, limitValue, localStopScanChannel)
			if scanErr != nil {
				fyne.Do(func() {
					updateScanControls(false)
					if stopScanChannel == localStopScanChannel {
						stopScanChannel = nil
					}
					scanButton.Enable()
					if errors.Is(scanErr, errScanStopped) {
						logDebugf("Scan stopped with %d loaded assets", len(results))
						statusLabel.SetText(fmt.Sprintf("Scan stopped. Loaded %d assets.", len(results)))
						return
					}
					logDebugf("Scan failed: %s", scanErr.Error())
					statusLabel.SetText(fmt.Sprintf("Scan failed: %s", scanErr.Error()))
					fyneDialog.ShowError(scanErr, window)
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
					statusLabel.SetText(options.NoResultsStatusText)
					logDebugf("Scan finished: no results")
				})
				return
			}

			loadFailureCount := 0
			var firstLoadErr error
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

				scanRow, loadErr := loadScanResult(hit)
				if loadErr != nil {
					loadFailureCount++
					if firstLoadErr == nil {
						firstLoadErr = loadErr
					}
					logDebugf("Scan result load failed for asset %d: %s", hit.AssetID, loadErr.Error())
					scanRow = scanResult{
						AssetID:       hit.AssetID,
						FilePath:      hit.FilePath,
						Source:        failedScanRowSource,
						State:         failedScanRowState,
						BytesSize:     0,
						Format:        "-",
						ContentType:   "-",
						AssetTypeID:   0,
						AssetTypeName: "Unknown",
						Warning:       true,
						WarningCause:  loadErr.Error(),
						Resource:      nil,
					}
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
				if len(results) == 0 && firstLoadErr != nil {
					statusLabel.SetText(fmt.Sprintf(
						"Scan extracted %d IDs but could not load any assets. First error: %s",
						len(hits),
						firstLoadErr.Error(),
					))
					fyneDialog.ShowError(firstLoadErr, window)
					logDebugf(
						"Scan complete with 0 shown assets (extracted=%d, load failures=%d)",
						len(hits),
						loadFailureCount,
					)
					return
				}
				if loadFailureCount > 0 {
					statusLabel.SetText(fmt.Sprintf(
						"Scan complete. Showing %d assets (%d failed to load).",
						len(results),
						loadFailureCount,
					))
					logDebugf(
						"Scan complete: %d assets shown, %d failed to load",
						len(results),
						loadFailureCount,
					)
					return
				}
				statusLabel.SetText(fmt.Sprintf("Scan complete. Showing %d assets.", len(results)))
				logDebugf("Scan complete: %d assets shown", len(results))
			})
		}()
	}

	controls := container.NewVBox(
		container.NewHBox(
			selectSourceButton,
			widget.NewLabel("Max results:"),
			container.NewGridWrap(fyne.NewSize(80, 36), limitEntry),
			scanButton,
		),
		sourceLabel,
	)

	previewContent := container.NewVBox(
		assetDetailsView.HierarchySection,
		assetDetailsView.PreviewBox,
		assetDetailsView.MetadataForm,
		assetDetailsView.JSONAccordion,
		assetDetailsView.NoteLabel,
	)
	previewScroll := container.NewVScroll(previewContent)
	previewPanel := container.NewBorder(nil, nil, nil, nil, previewScroll)
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
