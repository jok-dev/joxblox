package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	fyneDialog "fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	nativeDialog "github.com/sqweek/dialog"
)

const (
	defaultAssetScanLimit  = 100
	scanTableEmojiTextSize = 18
	failedScanRowState     = "Failed"
	failedScanRowSource    = "Load Failed"
	scanFilterAllOption    = "All"
	minScanLoadWorkers     = 4
	maxScanLoadWorkers     = 16
)

type assetScanTabOptions struct {
	NoSourceSelectedText             string
	SelectButtonText                 string
	NoSecondarySourceText            string
	SelectSecondaryButtonText        string
	ReadyStatusText                  string
	MissingSourceStatusText          string
	MissingSecondarySourceStatusText string
	ScanningStatusText               string
	NoResultsStatusText              string
	MaxResultsDefault                int
	SelectSource                     func(window fyne.Window, onSelected func(string), onError func(error))
	SelectSecondarySource            func(window fyne.Window, onSelected func(string), onError func(error))
	ExtractHits                      func(sourcePath string, limit int, stopChannel <-chan struct{}) ([]scanHit, error)
}

type secondaryTappableTable struct {
	*widget.Table
	onSecondaryTap func()
}

func (table *secondaryTappableTable) TappedSecondary(_ *fyne.PointEvent) {
	if table == nil || table.onSecondaryTap == nil {
		return
	}
	table.onSecondaryTap()
}

func newAssetScanTab(window fyne.Window, options assetScanTabOptions) (fyne.CanvasObject, *scanTabFileActions) {
	selectedSourcePath := ""
	selectedSecondarySourcePath := ""
	allResults := []scanResult{}
	displayResults := []scanResult{}
	showOnlyDuplicates := false
	searchQuery := ""
	typeFilterValue := scanFilterAllOption
	recentLoadedFiles := []string{}
	columnHeaders := []string{"Asset ID", "Use Count", "Type", "Self Size", "Dimensions", "State", "Asset SHA256"}
	sortField := "Self Size"
	sortDescending := true
	maxResultsDefault := options.MaxResultsDefault
	if maxResultsDefault <= 0 {
		maxResultsDefault = defaultAssetScanLimit
	}

	sourceLabel := widget.NewLabel(options.NoSourceSelectedText)
	secondarySourceText := options.NoSecondarySourceText
	if secondarySourceText == "" {
		secondarySourceText = "No secondary source selected."
	}
	secondarySourceLabel := widget.NewLabel(secondarySourceText)
	limitEntry := widget.NewEntry()
	limitEntry.SetText(strconv.Itoa(maxResultsDefault))
	limitEntry.SetPlaceHolder(strconv.Itoa(maxResultsDefault))
	searchEntry := widget.NewEntry()
	searchEntry.SetPlaceHolder("Search ID, type, state, source, hash, or path...")
	typeFilterSelect := widget.NewSelect([]string{scanFilterAllOption}, nil)
	typeFilterSelect.SetSelected(scanFilterAllOption)
	statusLabel := widget.NewLabel("Select a source and click Start Scan.")
	statsRowsLabel := widget.NewLabel("Rows: 0")
	statsShownLabel := widget.NewLabel("Shown: 0")
	statsFailedLabel := widget.NewLabel("Failed: 0")
	statsDuplicateLabel := widget.NewLabel("Duplicates: 0")
	statsSizeLabel := widget.NewLabel("Shown Size: 0 B")
	dragDropHintLabel := widget.NewLabel("Tip: drag and drop a results .json file onto the window to import.")

	assetDetailsView := newAssetView("Select a result row to preview", true)
	clearPreview := func() {
		assetDetailsView.Clear()
	}
	clearPreview()
	var explorerState *assetExplorerState
	var renderSelectedAsset func(selectedAssetID int64, selectedFilePath string, previewResult *assetPreviewResult)
	renderSelectedAsset = func(selectedAssetID int64, selectedFilePath string, previewResult *assetPreviewResult) {
		filePath := ""
		fileSHA256 := ""
		useCount := 0
		if explorerState != nil && selectedAssetID == explorerState.rootAssetID {
			filePath = selectedFilePath
			if len(allResults) > 0 {
				for _, row := range allResults {
					if row.AssetID == selectedAssetID && row.FilePath == selectedFilePath {
						fileSHA256 = row.FileSHA256
						useCount = row.UseCount
						break
					}
				}
			}
		}
		assetDetailsView.SetData(
			selectedAssetID,
			filePath,
			fileSHA256,
			useCount,
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

	var table *secondaryTappableTable
	selectedAssetID := int64(0)
	updateStatsLabels := func() {
		totalRowsCount := len(allResults)
		shownRowsCount := len(displayResults)
		failedRowsCount := 0
		shownBytesTotal := 0
		for _, row := range displayResults {
			if row.State == failedScanRowState {
				failedRowsCount++
			}
			if row.BytesSize > 0 {
				shownBytesTotal += row.BytesSize
			}
		}
		duplicateRowsCount := 0
		hashCounts := buildHashCounts(allResults)
		for _, row := range allResults {
			if isDuplicateByHash(row, hashCounts) {
				duplicateRowsCount++
			}
		}
		statsRowsLabel.SetText(fmt.Sprintf("Rows: %d", totalRowsCount))
		statsShownLabel.SetText(fmt.Sprintf("Shown: %d", shownRowsCount))
		statsFailedLabel.SetText(fmt.Sprintf("Failed: %d", failedRowsCount))
		statsDuplicateLabel.SetText(fmt.Sprintf("Duplicates: %d", duplicateRowsCount))
		statsSizeLabel.SetText(fmt.Sprintf("Shown Size: %s", formatSizeAuto(shownBytesTotal)))
	}
	updateFilterOptions := func() {
		uniqueTypes := map[string]bool{}
		for _, row := range allResults {
			typeLabel := scanResultTypeLabel(row)
			if typeLabel != "" {
				uniqueTypes[typeLabel] = true
			}
		}
		typeOptions := []string{scanFilterAllOption}
		for typeLabel := range uniqueTypes {
			typeOptions = append(typeOptions, typeLabel)
		}
		sort.Strings(typeOptions[1:])
		typeFilterSelect.SetOptions(typeOptions)
		if !containsString(typeOptions, typeFilterValue) {
			typeFilterValue = scanFilterAllOption
		}
		typeFilterSelect.SetSelected(typeFilterValue)
	}
	applySortAndFilters := func() {
		filteredResults := make([]scanResult, 0, len(allResults))
		hashCounts := buildHashCounts(allResults)
		for _, result := range allResults {
			if showOnlyDuplicates && !isDuplicateByHash(result, hashCounts) {
				continue
			}
			if typeFilterValue != scanFilterAllOption && !strings.EqualFold(scanResultTypeLabel(result), typeFilterValue) {
				continue
			}
			if !scanResultMatchesQuery(result, searchQuery) {
				continue
			}
			filteredResults = append(filteredResults, result)
		}
		sort.Slice(filteredResults, func(leftIndex int, rightIndex int) bool {
			leftResult := filteredResults[leftIndex]
			rightResult := filteredResults[rightIndex]
			compareResult := compareScanResults(leftResult, rightResult, sortField)
			if compareResult == 0 {
				return leftResult.AssetID < rightResult.AssetID
			}
			if sortDescending {
				return compareResult > 0
			}
			return compareResult < 0
		})
		displayResults = filteredResults
		if table != nil {
			table.Refresh()
		}
		updateStatsLabels()
	}

	updatePreviewFromRow := func(rowIndex int) {
		if rowIndex < 0 || rowIndex >= len(displayResults) {
			selectedAssetID = 0
			return
		}

		selectedResult := displayResults[rowIndex]
		selectedAssetID = selectedResult.AssetID
		rootPreview := &assetPreviewResult{
			Image: &imageInfo{
				Resource: selectedResult.Resource,
			},
			Stats: &imageInfo{
				Width:                    selectedResult.Width,
				Height:                   selectedResult.Height,
				BytesSize:                selectedResult.BytesSize,
				RecompressedPNGByteSize:  selectedResult.RecompressedPNGSize,
				RecompressedJPEGByteSize: selectedResult.RecompressedJPEGSize,
				Format:                   selectedResult.Format,
				ContentType:              selectedResult.ContentType,
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

	baseTable := widget.NewTableWithHeaders(
		func() (int, int) {
			return len(displayResults), len(columnHeaders)
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
			if id.Row < 0 || id.Row >= len(displayResults) || id.Col < 0 || id.Col >= len(columnHeaders) {
				emojiText.Text = ""
				emojiText.Refresh()
				label.SetText("")
				return
			}

			row := displayResults[id.Row]
			emojiText.Text = ""
			emojiText.Refresh()
			label.Importance = widget.MediumImportance
			switch id.Col {
			case 0:
				label.SetText(strconv.FormatInt(row.AssetID, 10))
			case 1:
				if row.UseCount > 0 {
					label.SetText(strconv.Itoa(row.UseCount))
				} else {
					label.SetText("-")
				}
			case 2:
				emojiText.Text = getAssetTypeEmoji(row.AssetTypeID)
				emojiText.Refresh()
				label.SetText(scanResultTypeLabel(row))
			case 3:
				label.SetText(formatSizeAuto(row.BytesSize))
			case 4:
				if row.Width > 0 && row.Height > 0 {
					label.SetText(fmt.Sprintf("%dx%d", row.Width, row.Height))
				} else {
					label.SetText("-")
				}
			case 5:
				if row.State == failedScanRowState {
					label.SetText(fmt.Sprintf("⚠ %s", row.State))
					label.Importance = widget.DangerImportance
				} else if isThumbnailFallback(row.Source) && !isCompletedState(row.State) {
					label.SetText(fmt.Sprintf("⚠ %s", row.State))
					label.Importance = widget.DangerImportance
				} else {
					label.SetText(row.State)
				}
			case 6:
				if strings.TrimSpace(row.FileSHA256) == "" {
					label.SetText("-")
				} else {
					label.SetText(row.FileSHA256)
				}
			default:
				label.SetText("")
			}
			label.Refresh()
		},
	)
	baseTable.CreateHeader = func() fyne.CanvasObject {
		return widget.NewButton("", nil)
	}
	baseTable.UpdateHeader = func(id widget.TableCellID, object fyne.CanvasObject) {
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
				applySortAndFilters()
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
	baseTable.OnSelected = func(id widget.TableCellID) {
		if id.Row < 0 {
			return
		}
		updatePreviewFromRow(id.Row)
	}
	table = &secondaryTappableTable{
		Table: baseTable,
		onSecondaryTap: func() {
			if selectedAssetID <= 0 {
				return
			}
			window.Clipboard().SetContent(strconv.FormatInt(selectedAssetID, 10))
			statusLabel.SetText(fmt.Sprintf("Copied asset ID %d to clipboard.", selectedAssetID))
		},
	}
	table.SetColumnWidth(0, 140)
	table.SetColumnWidth(1, 100)
	table.SetColumnWidth(2, 190)
	table.SetColumnWidth(3, 90)
	table.SetColumnWidth(4, 120)
	table.SetColumnWidth(5, 130)
	table.SetColumnWidth(6, 500)

	var scanInProgress bool
	var stopScanChannel chan struct{}
	scanButton := widget.NewButton("Start Scan", nil)
	hasSecondarySource := options.SelectSecondarySource != nil
	combinedSourcePath := func() string {
		if !hasSecondarySource {
			return selectedSourcePath
		}
		return fmt.Sprintf("%s\n%s", selectedSourcePath, selectedSecondarySourcePath)
	}
	updateReadyStatus := func() {
		if selectedSourcePath == "" {
			return
		}
		if !hasSecondarySource {
			statusLabel.SetText(options.ReadyStatusText)
			return
		}
		if selectedSecondarySourcePath == "" {
			statusLabel.SetText("Baseline selected. Select target to continue.")
			return
		}
		statusLabel.SetText(options.ReadyStatusText)
	}
	selectSourceButton := widget.NewButton(options.SelectButtonText, func() {
		options.SelectSource(
			window,
			func(selectedPath string) {
				selectedSourcePath = selectedPath
				sourceLabel.SetText(selectedSourcePath)
				updateReadyStatus()
			},
			func(err error) {
				if err == nil {
					return
				}
				statusLabel.SetText(fmt.Sprintf("Source picker failed: %s", err.Error()))
			},
		)
	})
	selectSecondaryButtonText := options.SelectSecondaryButtonText
	if selectSecondaryButtonText == "" {
		selectSecondaryButtonText = "Select Secondary Source"
	}
	selectSecondarySourceButton := widget.NewButton(selectSecondaryButtonText, nil)
	selectSecondarySourceButton.OnTapped = func() {
		if options.SelectSecondarySource == nil {
			return
		}
		options.SelectSecondarySource(
			window,
			func(selectedPath string) {
				selectedSecondarySourcePath = selectedPath
				secondarySourceLabel.SetText(selectedSecondarySourcePath)
				updateReadyStatus()
			},
			func(err error) {
				if err == nil {
					return
				}
				statusLabel.SetText(fmt.Sprintf("Secondary source picker failed: %s", err.Error()))
			},
		)
	}

	updateScanControls := func(inProgress bool) {
		scanInProgress = inProgress
		if inProgress {
			scanButton.SetText("Stop Scan")
			selectSourceButton.Disable()
			if hasSecondarySource {
				selectSecondarySourceButton.Disable()
			}
			limitEntry.Disable()
			searchEntry.Disable()
			typeFilterSelect.Disable()
		} else {
			scanButton.SetText("Start Scan")
			selectSourceButton.Enable()
			if hasSecondarySource {
				selectSecondarySourceButton.Enable()
			}
			limitEntry.Enable()
			searchEntry.Enable()
			typeFilterSelect.Enable()
		}
	}
	showOnlyDuplicatesCheck := widget.NewCheck("Show only duplicates", func(checked bool) {
		showOnlyDuplicates = checked
		applySortAndFilters()
		clearPreview()
	})
	showOnlyDuplicatesCheck.SetChecked(false)
	searchEntry.OnChanged = func(nextQuery string) {
		searchQuery = strings.TrimSpace(nextQuery)
		applySortAndFilters()
		clearPreview()
	}
	typeFilterSelect.OnChanged = func(nextFilterValue string) {
		if strings.TrimSpace(nextFilterValue) == "" {
			typeFilterValue = scanFilterAllOption
		} else {
			typeFilterValue = nextFilterValue
		}
		applySortAndFilters()
		clearPreview()
	}

	addRecentLoadedFile := func(importPath string) {
		normalizedPath := strings.TrimSpace(importPath)
		if normalizedPath == "" {
			return
		}
		nextRecent := []string{normalizedPath}
		for _, existingPath := range recentLoadedFiles {
			if strings.EqualFold(existingPath, normalizedPath) {
				continue
			}
			nextRecent = append(nextRecent, existingPath)
			if len(nextRecent) >= 10 {
				break
			}
		}
		recentLoadedFiles = nextRecent
	}

	importResultsFromPath := func(importPath string) {
		importBytes, readErr := os.ReadFile(importPath)
		if readErr != nil {
			statusLabel.SetText(fmt.Sprintf("Import read failed: %s", readErr.Error()))
			return
		}
		importedResults, parseErr := unmarshalScanTable(importBytes)
		if parseErr != nil {
			statusLabel.SetText(fmt.Sprintf("Import parse failed: %s", parseErr.Error()))
			return
		}

		allResults = importedResults
		selectedAssetID = 0
		clearPreview()
		addRecentLoadedFile(importPath)
		updateFilterOptions()
		applySortAndFilters()
		statusLabel.SetText(fmt.Sprintf("Imported %d scan rows.", len(allResults)))
		logDebugf("Scan table imported: %s (rows=%d)", importPath, len(allResults))
	}

	exportMarkdownResults := func() {
		if len(allResults) == 0 {
			statusLabel.SetText("Nothing to export yet. Run a scan or import results first.")
			return
		}
		selectedExportPath, pickerErr := nativeDialog.File().
			Filter("Markdown files", "md").
			Title("Export scan results as markdown").
			Save()
		if pickerErr != nil {
			if errors.Is(pickerErr, nativeDialog.Cancelled) {
				return
			}
			statusLabel.SetText(fmt.Sprintf("Markdown export picker failed: %s", pickerErr.Error()))
			return
		}
		if strings.TrimSpace(selectedExportPath) == "" {
			statusLabel.SetText("Markdown export canceled.")
			return
		}
		if !strings.HasSuffix(strings.ToLower(selectedExportPath), ".md") {
			selectedExportPath += ".md"
		}
		markdownBytes, markdownErr := marshalScanTableMarkdown(allResults)
		if markdownErr != nil {
			statusLabel.SetText(fmt.Sprintf("Markdown export failed: %s", markdownErr.Error()))
			return
		}
		if writeErr := os.WriteFile(selectedExportPath, markdownBytes, 0644); writeErr != nil {
			statusLabel.SetText(fmt.Sprintf("Markdown export write failed: %s", writeErr.Error()))
			return
		}
		statusLabel.SetText(fmt.Sprintf("Exported markdown for %d scan rows.", len(allResults)))
		logDebugf("Scan table markdown exported: %s (rows=%d)", selectedExportPath, len(allResults))
	}

	saveResultsToJSON := func() {
		if len(allResults) == 0 {
			statusLabel.SetText("Nothing to export yet. Run a scan or import a table first.")
			return
		}

		selectedExportPath, pickerErr := nativeDialog.File().
			Filter("JSON files", "json").
			Title("Export scan table").
			Save()
		if pickerErr != nil {
			if errors.Is(pickerErr, nativeDialog.Cancelled) {
				return
			}
			statusLabel.SetText(fmt.Sprintf("Export picker failed: %s", pickerErr.Error()))
			return
		}
		if strings.TrimSpace(selectedExportPath) == "" {
			statusLabel.SetText("Export canceled.")
			return
		}
		if !strings.HasSuffix(strings.ToLower(selectedExportPath), ".json") {
			selectedExportPath += ".json"
		}

		exportBytes, marshalErr := marshalScanTable(allResults)
		if marshalErr != nil {
			statusLabel.SetText(fmt.Sprintf("Export failed: %s", marshalErr.Error()))
			return
		}
		if writeErr := os.WriteFile(selectedExportPath, exportBytes, 0644); writeErr != nil {
			statusLabel.SetText(fmt.Sprintf("Export write failed: %s", writeErr.Error()))
			return
		}

		statusLabel.SetText(fmt.Sprintf("Exported %d scan rows.", len(allResults)))
		logDebugf("Scan table exported: %s (rows=%d)", selectedExportPath, len(allResults))
	}

	loadResultsFromPicker := func() {
		selectedImportPath, pickerErr := nativeDialog.File().
			Filter("JSON files", "json").
			Title("Import scan table").
			Load()
		if pickerErr != nil {
			if errors.Is(pickerErr, nativeDialog.Cancelled) {
				return
			}
			statusLabel.SetText(fmt.Sprintf("Import picker failed: %s", pickerErr.Error()))
			return
		}
		if strings.TrimSpace(selectedImportPath) == "" {
			statusLabel.SetText("Import canceled.")
			return
		}

		importResultsFromPath(selectedImportPath)
	}

	if dropWindow, ok := window.(interface {
		SetOnDropped(func(position fyne.Position, uris []fyne.URI))
	}); ok {
		dropWindow.SetOnDropped(func(_ fyne.Position, uris []fyne.URI) {
			if scanInProgress {
				statusLabel.SetText("Cannot import while scan is running.")
				return
			}
			for _, uri := range uris {
				if uri == nil {
					continue
				}
				candidatePath := strings.TrimSpace(uri.Path())
				if candidatePath == "" {
					continue
				}
				if !strings.EqualFold(filepath.Ext(candidatePath), ".json") {
					continue
				}
				importResultsFromPath(candidatePath)
				return
			}
			statusLabel.SetText("Drop a .json results file to import.")
		})
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
		if hasSecondarySource && selectedSecondarySourcePath == "" {
			if options.MissingSecondarySourceStatusText != "" {
				statusLabel.SetText(options.MissingSecondarySourceStatusText)
			} else {
				statusLabel.SetText("Please select a secondary source first.")
			}
			return
		}

		limitValue, err := strconv.Atoi(strings.TrimSpace(limitEntry.Text))
		if err != nil || limitValue <= 0 {
			statusLabel.SetText("Max results must be a positive number.")
			return
		}

		allResults = []scanResult{}
		displayResults = []scanResult{}
		selectedAssetID = 0
		updateFilterOptions()
		table.Refresh()
		clearPreview()
		logDebugf("Scan started for source: %s (limit=%d)", combinedSourcePath(), limitValue)
		statusLabel.SetText(options.ScanningStatusText)
		localStopScanChannel := make(chan struct{})
		stopScanChannel = localStopScanChannel
		updateScanControls(true)

		go func() {
			hits, scanErr := options.ExtractHits(combinedSourcePath(), limitValue, localStopScanChannel)
			if scanErr != nil {
				fyne.Do(func() {
					updateScanControls(false)
					if stopScanChannel == localStopScanChannel {
						stopScanChannel = nil
					}
					scanButton.Enable()
					if errors.Is(scanErr, errScanStopped) {
						logDebugf("Scan stopped with %d loaded assets", len(allResults))
						statusLabel.SetText(fmt.Sprintf("Scan stopped. Loaded %d assets.", len(allResults)))
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
			type scanLoadOutcome struct {
				row     scanResult
				loadErr error
			}
			workerCount := determineScanLoadWorkerCount(len(hits))
			hitJobs := make(chan scanHit)
			loadOutcomes := make(chan scanLoadOutcome, workerCount*2)
			var workerGroup sync.WaitGroup
			for workerIndex := 0; workerIndex < workerCount; workerIndex++ {
				workerGroup.Add(1)
				go func() {
					defer workerGroup.Done()
					for hit := range hitJobs {
						scanRow, loadErr := loadScanResult(hit)
						if loadErr != nil {
							logDebugf(
								"Scan result load failed for asset %d (file=%s, useCount=%d): %s",
								hit.AssetID,
								hit.FilePath,
								hit.UseCount,
								loadErr.Error(),
							)
							scanRow = buildFailedScanResult(hit, loadErr)
						}
						loadOutcomes <- scanLoadOutcome{
							row:     scanRow,
							loadErr: loadErr,
						}
					}
				}()
			}
			go func() {
				defer close(hitJobs)
				for _, hit := range hits {
					select {
					case <-localStopScanChannel:
						return
					case hitJobs <- hit:
					}
				}
			}()
			go func() {
				workerGroup.Wait()
				close(loadOutcomes)
			}()
			completedCount := 0
			for loadOutcome := range loadOutcomes {
				completedCount++
				if loadOutcome.loadErr != nil {
					loadFailureCount++
					if firstLoadErr == nil {
						firstLoadErr = loadOutcome.loadErr
					}
				}
				currentCount := completedCount
				scanRow := loadOutcome.row
				fyne.Do(func() {
					allResults = append(allResults, scanRow)
					updateFilterOptions()
					applySortAndFilters()
					statusLabel.SetText(fmt.Sprintf("Loaded %d/%d assets...", currentCount, len(hits)))
				})
			}
			select {
			case <-localStopScanChannel:
				fyne.Do(func() {
					updateScanControls(false)
					if stopScanChannel == localStopScanChannel {
						stopScanChannel = nil
					}
					scanButton.Enable()
					statusLabel.SetText(fmt.Sprintf("Scan stopped. Loaded %d assets.", len(allResults)))
				})
				return
			default:
			}

			fyne.Do(func() {
				updateScanControls(false)
				if stopScanChannel == localStopScanChannel {
					stopScanChannel = nil
				}
				scanButton.Enable()
				if len(allResults) == 0 && firstLoadErr != nil {
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
						len(allResults),
						loadFailureCount,
					))
					logDebugf(
						"Scan complete: %d assets shown, %d failed to load",
						len(allResults),
						loadFailureCount,
					)
					return
				}
				statusLabel.SetText(fmt.Sprintf("Scan complete. Showing %d assets.", len(allResults)))
				logDebugf("Scan complete: %d assets shown", len(allResults))
			})
		}()
	}

	controlButtons := []fyne.CanvasObject{
		selectSourceButton,
	}
	if hasSecondarySource {
		controlButtons = append(controlButtons, selectSecondarySourceButton)
	}
	controlButtons = append(
		controlButtons,
		widget.NewLabel("Max results:"),
		container.NewGridWrap(fyne.NewSize(80, 36), limitEntry),
		scanButton,
		layout.NewSpacer(),
	)
	sourceLabels := []fyne.CanvasObject{sourceLabel}
	if hasSecondarySource {
		sourceLabels = append(sourceLabels, secondarySourceLabel)
	}
	controls := container.NewVBox(
		container.NewHBox(controlButtons...),
		container.NewBorder(
			nil,
			nil,
			nil,
			container.NewHBox(
				widget.NewLabel("Type:"),
				container.NewGridWrap(fyne.NewSize(220, 36), typeFilterSelect),
				showOnlyDuplicatesCheck,
			),
			container.NewBorder(
				nil,
				nil,
				widget.NewLabel("Search:"),
				nil,
				searchEntry,
			),
		),
		container.NewHBox(
			statsRowsLabel,
			widget.NewSeparator(),
			statsShownLabel,
			widget.NewSeparator(),
			statsFailedLabel,
			widget.NewSeparator(),
			statsDuplicateLabel,
			widget.NewSeparator(),
			statsSizeLabel,
		),
		dragDropHintLabel,
		container.NewVBox(sourceLabels...),
	)
	updateFilterOptions()
	updateStatsLabels()

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

	content := container.NewBorder(
		controls,
		nil,
		nil,
		nil,
		container.NewBorder(statusLabel, nil, nil, nil, split),
	)
	fileActions := &scanTabFileActions{
		SaveJSON:       saveResultsToJSON,
		LoadJSON:       loadResultsFromPicker,
		ExportMarkdown: exportMarkdownResults,
		RecentFiles: func() []string {
			paths := make([]string, len(recentLoadedFiles))
			copy(paths, recentLoadedFiles)
			return paths
		},
		LoadRecent: func(path string) {
			if strings.TrimSpace(path) == "" {
				statusLabel.SetText("Select a recent file first.")
				return
			}
			importResultsFromPath(path)
		},
	}
	return content, fileActions
}

func determineScanLoadWorkerCount(totalHits int) int {
	if totalHits <= 0 {
		return 1
	}
	workerCount := runtime.NumCPU() * 4
	if workerCount < minScanLoadWorkers {
		workerCount = minScanLoadWorkers
	}
	if workerCount > maxScanLoadWorkers {
		workerCount = maxScanLoadWorkers
	}
	if workerCount > totalHits {
		workerCount = totalHits
	}
	return workerCount
}

func buildFailedScanResult(hit scanHit, loadErr error) scanResult {
	return scanResult{
		AssetID:       hit.AssetID,
		UseCount:      hit.UseCount,
		FilePath:      hit.FilePath,
		FileSHA256:    "",
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
