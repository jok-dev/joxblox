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
	"sync/atomic"
	"time"

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
	defaultAssetScanLimit   = 100
	scanTableEmojiTextSize  = 18
	failedScanRowState      = "Failed"
	failedScanRowSource     = "Load Failed"
	scanFilterAllOption     = "All"
	minScanLoadWorkers      = 4
	maxScanLoadWorkers      = 16
	scanLoadUIBatchSize     = 25
	scanLoadUIFlushDelay    = 150 * time.Millisecond
	scanLoadUIRefreshDelay  = 400 * time.Millisecond
	scanSearchDebounceDelay = 180 * time.Millisecond
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
	ScanContextKey                   string
	RecentFilesPreferenceKey         string
	SelectSource                     func(window fyne.Window, onSelected func(string), onError func(error))
	SelectSecondarySource            func(window fyne.Window, onSelected func(string), onError func(error))
	ExtractHits                      func(sourcePath string, limit int, stopChannel <-chan struct{}) ([]scanHit, error)
}

type secondaryTappableTable struct {
	*widget.Table
	onSecondaryTap func()
}

func buildScanLoadingStatus(completedCount int, totalCount int, elapsed time.Duration) string {
	statusText := fmt.Sprintf("Loading results %d/%d...", completedCount, totalCount)
	if totalCount <= 0 || completedCount <= 0 || completedCount >= totalCount {
		return statusText
	}
	if completedCount < 5 || elapsed < 2*time.Second {
		return statusText
	}
	remainingCount := totalCount - completedCount
	estimatedRemaining := time.Duration(float64(elapsed) * float64(remainingCount) / float64(completedCount))
	return fmt.Sprintf("%s ETA %s", statusText, formatDurationCompact(estimatedRemaining))
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
	typeDisplayToValue := map[string]string{
		scanFilterAllOption: scanFilterAllOption,
	}
	instanceTypeFilterValue := scanFilterAllOption
	instanceTypeDisplayToValue := map[string]string{
		scanFilterAllOption: scanFilterAllOption,
	}
	propertyNameFilterValue := scanFilterAllOption
	propertyNameDisplayToValue := map[string]string{
		scanFilterAllOption: scanFilterAllOption,
	}
	suppressTypeFilterChange := false
	suppressInstanceTypeFilterChange := false
	suppressPropertyNameFilterChange := false
	recentLoadedFiles := loadRecentFilesFromPreferences(options.RecentFilesPreferenceKey)
	columnHeaders := []string{"Asset ID", "Use Count", "Type", "Self Size", "Dimensions", "Asset SHA256"}
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
	searchEntry.SetPlaceHolder("Search ID, type, source, hash, or path...")
	typeFilterSelect := widget.NewSelect([]string{scanFilterAllOption}, nil)
	typeFilterSelect.SetSelected(scanFilterAllOption)
	instanceTypeFilterSelect := widget.NewSelect([]string{scanFilterAllOption}, nil)
	instanceTypeFilterSelect.SetSelected(scanFilterAllOption)
	propertyNameFilterSelect := widget.NewSelect([]string{scanFilterAllOption}, nil)
	propertyNameFilterSelect.SetSelected(scanFilterAllOption)
	statusLabel := widget.NewLabel("Select a source and click Start Scan.")
	statsRowsLabel := widget.NewLabel("Rows: 0")
	statsShownLabel := widget.NewLabel("Shown: 0")
	statsFailedLabel := widget.NewLabel("Failed: 0")
	statsDuplicateLabel := widget.NewLabel("Duplicates: 0")
	statsDuplicateSizeLabel := widget.NewLabel("Duplicate Size: 0 B")
	statsSizeLabel := widget.NewLabel("Shown Size: 0 B")
	dragDropHintLabel := widget.NewLabel("Tip: drag and drop a results .json file onto the window to import.")

	assetDetailsView := newAssetView("Select a result row to preview", true)
	selectedResultFilePath := ""
	selectedTableColumn := 0
	clearPreview := func() {
		selectedResultFilePath = ""
		selectedTableColumn = 0
		assetDetailsView.Clear()
	}
	clearPreview()
	var explorerState *assetExplorerState
	var renderSelectedAsset func(selectedAssetID int64, selectedFilePath string, previewResult *assetPreviewResult)
	renderSelectedAsset = func(selectedAssetID int64, selectedFilePath string, previewResult *assetPreviewResult) {
		referenceInstanceType := ""
		referencePropertyName := ""
		referenceInstancePath := ""
		if explorerState != nil && selectedAssetID == explorerState.rootAssetID {
			for _, row := range allResults {
				if row.AssetID == selectedAssetID && row.FilePath == selectedFilePath {
					referenceInstanceType = row.InstanceType
					referencePropertyName = row.PropertyName
					referenceInstancePath = row.InstancePath
					if referenceInstancePath == "" {
						referenceInstancePath = row.InstanceName
					}
					break
				}
			}
		} else if explorerState != nil {
			if selectedRow, found := explorerState.getRow(selectedAssetID); found {
				referenceInstanceType = selectedRow.InstanceType
				referencePropertyName = selectedRow.PropertyName
				referenceInstancePath = explorerState.getInstancePath(selectedAssetID)
				if referenceInstancePath == "" {
					referenceInstancePath = selectedRow.InstancePath
				}
				if referenceInstancePath == "" {
					referenceInstancePath = selectedRow.InstanceName
				}
			}
		}
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
			referenceInstanceType,
			referencePropertyName,
			referenceInstancePath,
			previewResult.AssetTypeID,
			previewResult.AssetTypeName,
			previewResult.DownloadBytes,
			previewResult.DownloadFileName,
			previewResult.DownloadIsOriginal,
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
	duplicateRowsCount := 0
	duplicateBytesTotal := 0
	var searchChangeToken atomic.Uint64
	matchesActiveFilters := func(result scanResult, hashCounts map[string]int, ignoreTypeFilter bool, ignoreInstanceTypeFilter bool, ignorePropertyNameFilter bool) bool {
		if showOnlyDuplicates && !isDuplicateByHash(result, hashCounts) {
			return false
		}
		if !scanResultMatchesQuery(result, searchQuery) {
			return false
		}
		if !ignoreTypeFilter && typeFilterValue != scanFilterAllOption && !strings.EqualFold(scanResultTypeFilterLabel(result), typeFilterValue) {
			return false
		}
		if !ignoreInstanceTypeFilter && instanceTypeFilterValue != scanFilterAllOption && !strings.EqualFold(scanResultInstanceTypeLabel(result), instanceTypeFilterValue) {
			return false
		}
		if !ignorePropertyNameFilter && propertyNameFilterValue != scanFilterAllOption && !strings.EqualFold(scanResultPropertyNameLabel(result), propertyNameFilterValue) {
			return false
		}
		return true
	}
	countDuplicateRows := func(hashCounts map[string]int) int {
		duplicateCount := 0
		for _, row := range allResults {
			if isDuplicateByHash(row, hashCounts) {
				duplicateCount++
			}
		}
		return duplicateCount
	}
	countDuplicateBytes := func(hashCounts map[string]int) int {
		duplicateBytes := 0
		for _, row := range allResults {
			if !isDuplicateByHash(row, hashCounts) || row.BytesSize <= 0 {
				continue
			}
			duplicateBytes += row.BytesSize
		}
		return duplicateBytes
	}
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
		statsRowsLabel.SetText(fmt.Sprintf("Rows: %d", totalRowsCount))
		statsShownLabel.SetText(fmt.Sprintf("Shown: %d", shownRowsCount))
		statsFailedLabel.SetText(fmt.Sprintf("Failed: %d", failedRowsCount))
		statsDuplicateLabel.SetText(fmt.Sprintf("Duplicates: %d", duplicateRowsCount))
		statsDuplicateSizeLabel.SetText(fmt.Sprintf("Duplicate Size: %s", formatSizeAuto(duplicateBytesTotal)))
		statsSizeLabel.SetText(fmt.Sprintf("Shown Size: %s", formatSizeAuto(shownBytesTotal)))
	}
	updateFilterOptions := func(hashCounts map[string]int) {
		typeCounts := map[string]int{}
		instanceTypeCounts := map[string]int{}
		propertyNameCounts := map[string]int{}
		for _, row := range allResults {
			typeLabel := scanResultTypeFilterLabel(row)
			if typeLabel != "" && matchesActiveFilters(row, hashCounts, true, false, false) {
				typeCounts[typeLabel]++
			}
			instanceTypeLabel := scanResultInstanceTypeLabel(row)
			if instanceTypeLabel != "" && matchesActiveFilters(row, hashCounts, false, true, false) {
				instanceTypeCounts[instanceTypeLabel]++
			}
			propertyNameLabel := scanResultPropertyNameLabel(row)
			if propertyNameLabel != "" && matchesActiveFilters(row, hashCounts, false, false, true) {
				propertyNameCounts[propertyNameLabel]++
			}
		}
		if typeFilterValue != scanFilterAllOption {
			if _, found := typeCounts[typeFilterValue]; !found {
				typeCounts[typeFilterValue] = 0
			}
		}
		if instanceTypeFilterValue != scanFilterAllOption {
			if _, found := instanceTypeCounts[instanceTypeFilterValue]; !found {
				instanceTypeCounts[instanceTypeFilterValue] = 0
			}
		}
		if propertyNameFilterValue != scanFilterAllOption {
			if _, found := propertyNameCounts[propertyNameFilterValue]; !found {
				propertyNameCounts[propertyNameFilterValue] = 0
			}
		}
		typeOptions := []string{scanFilterAllOption}
		typeDisplayToValue = map[string]string{
			scanFilterAllOption: scanFilterAllOption,
		}
		typeLabels := make([]string, 0, len(typeCounts))
		for typeLabel := range typeCounts {
			typeLabels = append(typeLabels, typeLabel)
		}
		instanceTypeOptions := []string{scanFilterAllOption}
		instanceTypeDisplayToValue = map[string]string{
			scanFilterAllOption: scanFilterAllOption,
		}
		instanceTypeLabels := make([]string, 0, len(instanceTypeCounts))
		for instanceTypeLabel := range instanceTypeCounts {
			instanceTypeLabels = append(instanceTypeLabels, instanceTypeLabel)
		}
		propertyNameOptions := []string{scanFilterAllOption}
		propertyNameDisplayToValue = map[string]string{
			scanFilterAllOption: scanFilterAllOption,
		}
		propertyNameLabels := make([]string, 0, len(propertyNameCounts))
		for propertyNameLabel := range propertyNameCounts {
			propertyNameLabels = append(propertyNameLabels, propertyNameLabel)
		}
		sort.Strings(typeLabels)
		sort.Strings(instanceTypeLabels)
		sort.Strings(propertyNameLabels)
		for _, typeLabel := range typeLabels {
			displayLabel := fmt.Sprintf("%s (%d)", typeLabel, typeCounts[typeLabel])
			typeOptions = append(typeOptions, displayLabel)
			typeDisplayToValue[displayLabel] = typeLabel
		}
		if containsString(instanceTypeLabels, "Unknown") {
			instanceTypeOptions = append(instanceTypeOptions, fmt.Sprintf("Unknown (%d)", instanceTypeCounts["Unknown"]))
			instanceTypeDisplayToValue[instanceTypeOptions[len(instanceTypeOptions)-1]] = "Unknown"
		}
		for _, instanceTypeLabel := range instanceTypeLabels {
			if instanceTypeLabel == "Unknown" {
				continue
			}
			displayLabel := fmt.Sprintf("%s (%d)", instanceTypeLabel, instanceTypeCounts[instanceTypeLabel])
			instanceTypeOptions = append(instanceTypeOptions, displayLabel)
			instanceTypeDisplayToValue[displayLabel] = instanceTypeLabel
		}
		if containsString(propertyNameLabels, "Unknown") {
			propertyNameOptions = append(propertyNameOptions, fmt.Sprintf("Unknown (%d)", propertyNameCounts["Unknown"]))
			propertyNameDisplayToValue[propertyNameOptions[len(propertyNameOptions)-1]] = "Unknown"
		}
		for _, propertyNameLabel := range propertyNameLabels {
			if propertyNameLabel == "Unknown" {
				continue
			}
			displayLabel := fmt.Sprintf("%s (%d)", propertyNameLabel, propertyNameCounts[propertyNameLabel])
			propertyNameOptions = append(propertyNameOptions, displayLabel)
			propertyNameDisplayToValue[displayLabel] = propertyNameLabel
		}
		suppressTypeFilterChange = true
		typeFilterSelect.SetOptions(typeOptions)
		selectedTypeOption := scanFilterAllOption
		for optionLabel, optionValue := range typeDisplayToValue {
			if optionValue == typeFilterValue {
				selectedTypeOption = optionLabel
				break
			}
		}
		if !containsString(typeOptions, selectedTypeOption) {
			typeFilterValue = scanFilterAllOption
			selectedTypeOption = scanFilterAllOption
		}
		typeFilterSelect.SetSelected(selectedTypeOption)
		suppressTypeFilterChange = false
		suppressInstanceTypeFilterChange = true
		instanceTypeFilterSelect.SetOptions(instanceTypeOptions)
		selectedInstanceTypeOption := scanFilterAllOption
		for optionLabel, optionValue := range instanceTypeDisplayToValue {
			if optionValue == instanceTypeFilterValue {
				selectedInstanceTypeOption = optionLabel
				break
			}
		}
		if !containsString(instanceTypeOptions, selectedInstanceTypeOption) {
			instanceTypeFilterValue = scanFilterAllOption
			selectedInstanceTypeOption = scanFilterAllOption
		}
		instanceTypeFilterSelect.SetSelected(selectedInstanceTypeOption)
		suppressInstanceTypeFilterChange = false
		suppressPropertyNameFilterChange = true
		propertyNameFilterSelect.SetOptions(propertyNameOptions)
		selectedPropertyNameOption := scanFilterAllOption
		for optionLabel, optionValue := range propertyNameDisplayToValue {
			if optionValue == propertyNameFilterValue {
				selectedPropertyNameOption = optionLabel
				break
			}
		}
		if !containsString(propertyNameOptions, selectedPropertyNameOption) {
			propertyNameFilterValue = scanFilterAllOption
			selectedPropertyNameOption = scanFilterAllOption
		}
		propertyNameFilterSelect.SetSelected(selectedPropertyNameOption)
		suppressPropertyNameFilterChange = false
	}
	applySortAndFilters := func() {
		previousSelectedAssetID := selectedAssetID
		previousSelectedFilePath := selectedResultFilePath
		filteredResults := make([]scanResult, 0, len(allResults))
		hashCounts := buildHashCounts(allResults)
		duplicateRowsCount = countDuplicateRows(hashCounts)
		duplicateBytesTotal = countDuplicateBytes(hashCounts)
		updateFilterOptions(hashCounts)
		for _, result := range allResults {
			if !matchesActiveFilters(result, hashCounts, false, false, false) {
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
		nextSelectedRowIndex := -1
		if previousSelectedAssetID > 0 {
			for rowIndex, result := range displayResults {
				if result.AssetID == previousSelectedAssetID && result.FilePath == previousSelectedFilePath {
					nextSelectedRowIndex = rowIndex
					break
				}
			}
		}
		if table != nil {
			table.Refresh()
			if nextSelectedRowIndex >= 0 {
				table.Select(widget.TableCellID{Row: nextSelectedRowIndex, Col: selectedTableColumn})
			} else if previousSelectedAssetID > 0 {
				selectedAssetID = 0
				clearPreview()
			}
		}
		updateStatsLabels()
	}

	updatePreviewFromRow := func(rowIndex int) {
		if rowIndex < 0 || rowIndex >= len(displayResults) {
			selectedAssetID = 0
			selectedResultFilePath = ""
			selectedTableColumn = 0
			return
		}

		selectedResult := displayResults[rowIndex]
		selectedAssetID = selectedResult.AssetID
		selectedResultFilePath = selectedResult.FilePath
		rootPreview := &assetPreviewResult{
			Image: &imageInfo{
				Resource: selectedResult.Resource,
			},
			Stats: &imageInfo{
				Width:                    selectedResult.Width,
				Height:                   selectedResult.Height,
				Duration:                 selectedResult.Duration,
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
			DownloadBytes:      append([]byte(nil), selectedResult.DownloadBytes...),
			DownloadFileName:   selectedResult.DownloadFileName,
			DownloadIsOriginal: selectedResult.DownloadIsOriginal,
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
				if emojiText.Text != "" {
					emojiText.Text = ""
					emojiText.Refresh()
				}
				label.SetText("")
				return
			}

			row := displayResults[id.Row]
			if emojiText.Text != "" {
				emojiText.Text = ""
				emojiText.Refresh()
			}
			nextImportance := widget.MediumImportance
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
				nextEmoji := getAssetTypeEmoji(row.AssetTypeID)
				if emojiText.Text != nextEmoji {
					emojiText.Text = nextEmoji
					emojiText.Refresh()
				}
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
				if strings.TrimSpace(row.FileSHA256) == "" {
					label.SetText("-")
				} else {
					label.SetText(row.FileSHA256)
				}
			default:
				label.SetText("")
			}
			if label.Importance != nextImportance {
				label.Importance = nextImportance
				label.Refresh()
			}
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
		if id.Col >= 0 {
			selectedTableColumn = id.Col
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
	table.SetColumnWidth(5, 500)

	var scanInProgress bool
	var activeStopSignal *stopSignal
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
			instanceTypeFilterSelect.Disable()
			propertyNameFilterSelect.Disable()
		} else {
			scanButton.SetText("Start Scan")
			selectSourceButton.Enable()
			if hasSecondarySource {
				selectSecondarySourceButton.Enable()
			}
			limitEntry.Enable()
			searchEntry.Enable()
			typeFilterSelect.Enable()
			instanceTypeFilterSelect.Enable()
			propertyNameFilterSelect.Enable()
		}
	}
	requestStopScan := func() {
		if activeStopSignal == nil {
			return
		}
		localStopSignal := activeStopSignal
		activeStopSignal = nil
		localStopSignal.Stop()
	}
	finishScan := func(localStopSignal *stopSignal) {
		updateScanControls(false)
		if activeStopSignal == localStopSignal {
			activeStopSignal = nil
		}
		scanButton.Enable()
	}
	showOnlyDuplicatesCheck := widget.NewCheck("Show only duplicates", func(checked bool) {
		showOnlyDuplicates = checked
		applySortAndFilters()
		clearPreview()
	})
	showOnlyDuplicatesCheck.SetChecked(false)
	searchEntry.OnChanged = func(nextQuery string) {
		searchQuery = strings.TrimSpace(nextQuery)
		changeToken := searchChangeToken.Add(1)
		go func(expectedToken uint64) {
			time.Sleep(scanSearchDebounceDelay)
			fyne.Do(func() {
				if searchChangeToken.Load() != expectedToken {
					return
				}
				applySortAndFilters()
				clearPreview()
			})
		}(changeToken)
	}
	typeFilterSelect.OnChanged = func(nextFilterValue string) {
		if suppressTypeFilterChange {
			return
		}
		if strings.TrimSpace(nextFilterValue) == "" {
			typeFilterValue = scanFilterAllOption
		} else if mappedValue, found := typeDisplayToValue[nextFilterValue]; found {
			typeFilterValue = mappedValue
		} else {
			typeFilterValue = nextFilterValue
		}
		applySortAndFilters()
		clearPreview()
	}
	instanceTypeFilterSelect.OnChanged = func(nextFilterValue string) {
		if suppressInstanceTypeFilterChange {
			return
		}
		if strings.TrimSpace(nextFilterValue) == "" {
			instanceTypeFilterValue = scanFilterAllOption
		} else if mappedValue, found := instanceTypeDisplayToValue[nextFilterValue]; found {
			instanceTypeFilterValue = mappedValue
		} else {
			instanceTypeFilterValue = nextFilterValue
		}
		applySortAndFilters()
		clearPreview()
	}
	propertyNameFilterSelect.OnChanged = func(nextFilterValue string) {
		if suppressPropertyNameFilterChange {
			return
		}
		if strings.TrimSpace(nextFilterValue) == "" {
			propertyNameFilterValue = scanFilterAllOption
		} else if mappedValue, found := propertyNameDisplayToValue[nextFilterValue]; found {
			propertyNameFilterValue = mappedValue
		} else {
			propertyNameFilterValue = nextFilterValue
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
		saveRecentFilesToPreferences(options.RecentFilesPreferenceKey, recentLoadedFiles)
	}

	applyImportedResults := func(importedResults []scanResult, statusMessage string) {
		allResults = importedResults
		selectedAssetID = 0
		clearPreview()
		applySortAndFilters()
		if strings.TrimSpace(statusMessage) != "" {
			statusLabel.SetText(statusMessage)
		}
	}

	importResultsFromPath := func(importPath string) {
		statusLabel.SetText("Importing results...")
		progress := newProgressDialog(window, "Load JSON", "Reading scan results...")
		readProgress := progressRangeReporter(progress, 0, 0.3, "Reading scan results...")
		parseProgress := progressRangeReporter(progress, 0.3, 0.9, "Parsing scan results...")
		go func() {
			importBytes, readErr := readFileWithProgress(importPath, readProgress)
			if readErr != nil {
				progress.Hide()
				fyne.Do(func() {
					statusLabel.SetText(fmt.Sprintf("Import read failed: %s", readErr.Error()))
					fyneDialog.ShowError(fmt.Errorf("import read failed: %w", readErr), window)
				})
				return
			}

			var importedResults []scanResult
			importFormat := detectScanImportFormat(importBytes)
			progress.Update(0.3, "Parsing scan results...")
			switch importFormat {
			case scanImportFormatWorkspace:
				tablesByContext, parseErr := unmarshalScanWorkspace(importBytes, parseProgress)
				if parseErr != nil {
					progress.Hide()
					fyne.Do(func() {
						statusLabel.SetText(fmt.Sprintf("Import parse failed: %s", parseErr.Error()))
						fyneDialog.ShowError(fmt.Errorf("import parse failed: %w", parseErr), window)
					})
					return
				}
				importedResults = tablesByContext[options.ScanContextKey]
			case scanImportFormatTable:
				var parseErr error
				importedResults, parseErr = unmarshalScanTable(importBytes, parseProgress)
				if parseErr != nil {
					progress.Hide()
					fyne.Do(func() {
						statusLabel.SetText(fmt.Sprintf("Import parse failed: %s", parseErr.Error()))
						fyneDialog.ShowError(fmt.Errorf("import parse failed: %w", parseErr), window)
					})
					return
				}
			default:
				progress.Hide()
				fyne.Do(func() {
					statusLabel.SetText("Import parse failed: unsupported scan JSON format.")
					fyneDialog.ShowError(fmt.Errorf("import parse failed: unsupported scan JSON format"), window)
				})
				return
			}

			progress.Update(0.95, "Applying imported results...")
			fyne.Do(func() {
				progress.Hide()
				addRecentLoadedFile(importPath)
				applyImportedResults(importedResults, fmt.Sprintf("Imported %d results.", len(importedResults)))
				if importFormat == scanImportFormatWorkspace {
					logDebugf(
						"Scan workspace imported into context %s: %s (rows=%d)",
						options.ScanContextKey,
						importPath,
						len(importedResults),
					)
					return
				}
				logDebugf("Scan table imported: %s (rows=%d)", importPath, len(importedResults))
			})
		}()
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
		resultsToExport := append([]scanResult(nil), allResults...)
		statusLabel.SetText("Exporting results...")
		progress := newProgressDialog(window, "Save JSON", "Serializing scan results...")
		serializeProgress := progressRangeReporter(progress, 0.05, 0.8, "Serializing scan results...")
		writeProgress := progressRangeReporter(progress, 0.8, 1, "Writing JSON file...")
		go func() {
			exportBytes, marshalErr := marshalScanTable(resultsToExport, serializeProgress)
			if marshalErr != nil {
				progress.Hide()
				fyne.Do(func() {
					statusLabel.SetText(fmt.Sprintf("Export failed: %s", marshalErr.Error()))
				})
				return
			}
			if writeErr := writeFileWithProgress(selectedExportPath, exportBytes, writeProgress); writeErr != nil {
				progress.Hide()
				fyne.Do(func() {
					statusLabel.SetText(fmt.Sprintf("Export write failed: %s", writeErr.Error()))
				})
				return
			}

			fyne.Do(func() {
				progress.Hide()
				statusLabel.SetText(fmt.Sprintf("Saved %d results.", len(resultsToExport)))
				logDebugf("Scan table exported: %s (rows=%d)", selectedExportPath, len(resultsToExport))
			})
		}()
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

	handleDroppedURIs := func(uris []fyne.URI) {
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
	}
	if dropWindow, ok := window.(interface {
		SetOnDropped(func(position fyne.Position, uris []fyne.URI))
	}); ok {
		dropWindow.SetOnDropped(func(_ fyne.Position, uris []fyne.URI) {
			handleDroppedURIs(uris)
		})
	}

	scanButton.OnTapped = func() {
		if scanInProgress {
			logDebugf("Scan stop requested")
			requestStopScan()
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
		updateFilterOptions(map[string]int{})
		table.Refresh()
		clearPreview()
		logDebugf("Scan started for source: %s (limit=%d)", combinedSourcePath(), limitValue)
		statusLabel.SetText(options.ScanningStatusText)
		localStopSignal := newStopSignal()
		activeStopSignal = localStopSignal
		updateScanControls(true)

		go func() {
			hits, scanErr := options.ExtractHits(combinedSourcePath(), limitValue, localStopSignal.channel)
			if scanErr != nil {
				fyne.Do(func() {
					finishScan(localStopSignal)
					if errors.Is(scanErr, errScanStopped) {
						logDebugf("Scan stopped with %d loaded assets", len(allResults))
						statusLabel.SetText(fmt.Sprintf("Stopped. %d results loaded.", len(allResults)))
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
					finishScan(localStopSignal)
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
					case <-localStopSignal.channel:
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
			pendingRows := make([]scanResult, 0, scanLoadUIBatchSize)
			lastUIFlushAt := time.Now()
			lastScheduledResultsRefreshAt := time.Time{}
			lastScheduledFilterRefreshAt := time.Time{}
			loadStartedAt := time.Now()
			flushPendingRows := func(force bool) {
				if len(pendingRows) == 0 {
					return
				}
				if !force && len(pendingRows) < scanLoadUIBatchSize && time.Since(lastUIFlushAt) < scanLoadUIFlushDelay {
					return
				}
				rowsToPublish := append([]scanResult(nil), pendingRows...)
				publishedCount := completedCount
				pendingRows = pendingRows[:0]
				lastUIFlushAt = time.Now()
				refreshResults := force || len(displayResults) == 0 || time.Since(lastScheduledResultsRefreshAt) >= scanLoadUIRefreshDelay
				refreshFilters := force || len(allResults) == 0 || time.Since(lastScheduledFilterRefreshAt) >= scanLoadUIRefreshDelay
				if refreshResults {
					lastScheduledResultsRefreshAt = time.Now()
				}
				if refreshFilters {
					lastScheduledFilterRefreshAt = time.Now()
				}
				fyne.Do(func() {
					allResults = append(allResults, rowsToPublish...)
					if refreshResults {
						applySortAndFilters()
					} else if refreshFilters {
						updateFilterOptions(buildHashCounts(allResults))
					}
					statusLabel.SetText(buildScanLoadingStatus(publishedCount, len(hits), time.Since(loadStartedAt)))
				})
			}
			for loadOutcome := range loadOutcomes {
				completedCount++
				if loadOutcome.loadErr != nil {
					loadFailureCount++
					if firstLoadErr == nil {
						firstLoadErr = loadOutcome.loadErr
					}
				}
				pendingRows = append(pendingRows, loadOutcome.row)
				flushPendingRows(false)
			}
			flushPendingRows(true)
			select {
			case <-localStopSignal.channel:
				fyne.Do(func() {
					finishScan(localStopSignal)
					statusLabel.SetText(fmt.Sprintf("Stopped. %d results loaded.", len(allResults)))
				})
				return
			default:
			}

			fyne.Do(func() {
				finishScan(localStopSignal)
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
				statusLabel.SetText(fmt.Sprintf("Done. %d results loaded.", len(allResults)))
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
				container.NewGridWrap(fyne.NewSize(260, 36), typeFilterSelect),
				widget.NewLabel("Instance Type:"),
				container.NewGridWrap(fyne.NewSize(280, 36), instanceTypeFilterSelect),
				widget.NewLabel("Property Name:"),
				container.NewGridWrap(fyne.NewSize(280, 36), propertyNameFilterSelect),
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
			statsDuplicateSizeLabel,
			widget.NewSeparator(),
			statsSizeLabel,
		),
		dragDropHintLabel,
		container.NewVBox(sourceLabels...),
	)
	updateFilterOptions(map[string]int{})
	updateStatsLabels()

	previewContent := container.NewVBox(
		assetDetailsView.PreviewBox,
		assetDetailsView.HierarchySection,
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
		ContextKey:     options.ScanContextKey,
		SaveJSON:       saveResultsToJSON,
		LoadJSON:       loadResultsFromPicker,
		HandleDrop:     handleDroppedURIs,
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
		GetResults: func() []scanResult {
			rows := make([]scanResult, len(allResults))
			copy(rows, allResults)
			return rows
		},
		SetResults: func(rows []scanResult) {
			nextRows := make([]scanResult, len(rows))
			copy(nextRows, rows)
			applyImportedResults(nextRows, fmt.Sprintf("Loaded %d results.", len(nextRows)))
		},
		AddRecentFile: addRecentLoadedFile,
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
