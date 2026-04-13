package app

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"joxblox/internal/app/loader"
	"joxblox/internal/extractor"
	"joxblox/internal/format"
	"joxblox/internal/heatmap"
	"joxblox/internal/roblox"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

type scanResultsExplorerVariant string

const (
	scanResultsExplorerVariantScan    scanResultsExplorerVariant = "scan"
	scanResultsExplorerVariantHeatmap scanResultsExplorerVariant = "heatmap"
)

type scanResultsExplorerOptions struct {
	Variant            scanResultsExplorerVariant
	PreviewPlaceholder string
	IncludeFileRow     bool
	InitialStatusText  string
	SearchPlaceholder  string
	HeaderContent      fyne.CanvasObject
	ShowDuplicateUI    bool
	ShowLargeTextureUI bool
}

type scanResultsExplorer struct {
	window                           fyne.Window
	variant                          scanResultsExplorerVariant
	content                          fyne.CanvasObject
	statusLabel                      *widget.Label
	allResults                       []scanResult
	displayResults                   []scanResult
	columnHeaders                    []string
	displayDistances                 []int
	showOnlyDuplicates               bool
	showOnlyLargeTextures            bool
	searchQuery                      string
	similarityActive                 bool
	similarityMatchSet               map[int]int
	sortField                        string
	sortDescending                   bool
	largeTextureThreshold            float64
	typeFilterValue                  string
	typeDisplayToValue               map[string]string
	instanceTypeFilterValue          string
	instanceTypeDisplayToValue       map[string]string
	propertyNameFilterValue          string
	propertyNameDisplayToValue       map[string]string
	suppressTypeFilterChange         bool
	suppressInstanceTypeFilterChange bool
	suppressPropertyNameFilterChange bool
	suppressLargeTextureFilterChange bool
	controlsEnabled                  bool
	selectedAssetID                  int64
	selectedResultFilePath           string
	selectedResultAssetInput         string
	selectedTableColumn              int
	duplicateRowsCount               int
	duplicateBytesTotal              int
	searchChangeToken                atomic.Uint64
	assetDetailsView                 *assetView
	explorerState                    *assetExplorerState
	table                            *secondaryTappableTable
	typeFilterSelect                 *widget.Select
	instanceTypeFilterSelect         *widget.Select
	propertyNameFilterSelect         *widget.Select
	statsRowsLabel                   *widget.Label
	statsShownLabel                  *widget.Label
	statsFailedLabel                 *widget.Label
	statsDuplicateLabel              *widget.Label
	statsDuplicateSizeLabel          *widget.Label
	statsSizeLabel                   *widget.Label
	statsTrianglesLabel              *widget.Label
	searchEntry                      *widget.Entry
	showOnlyDuplicatesCheck          *widget.Check
	showOnlyLargeTexturesCheck       *widget.Check
	largeTextureThresholdEntry       *widget.Entry
}

func newScanResultsExplorer(window fyne.Window, options scanResultsExplorerOptions) *scanResultsExplorer {
	previewPlaceholder := strings.TrimSpace(options.PreviewPlaceholder)
	if previewPlaceholder == "" {
		previewPlaceholder = "Select a result row to preview"
	}
	searchPlaceholder := strings.TrimSpace(options.SearchPlaceholder)
	if searchPlaceholder == "" {
		searchPlaceholder = "Search ID, type, source, hash, or path..."
	}
	explorer := &scanResultsExplorer{
		window:                     window,
		variant:                    options.Variant,
		statusLabel:                widget.NewLabel(options.InitialStatusText),
		typeFilterValue:            scanFilterAllOption,
		typeDisplayToValue:         map[string]string{scanFilterAllOption: scanFilterAllOption},
		instanceTypeFilterValue:    scanFilterAllOption,
		instanceTypeDisplayToValue: map[string]string{scanFilterAllOption: scanFilterAllOption},
		propertyNameFilterValue:    scanFilterAllOption,
		propertyNameDisplayToValue: map[string]string{scanFilterAllOption: scanFilterAllOption},
		selectedTableColumn:        0,
		similarityMatchSet:         map[int]int{},
		largeTextureThreshold:      defaultLargeTextureThreshold,
		controlsEnabled:            true,
	}
	explorer.assetDetailsView = newAssetView(previewPlaceholder, options.IncludeFileRow)
	explorer.searchEntry = widget.NewEntry()
	explorer.searchEntry.SetPlaceHolder(searchPlaceholder)
	explorer.typeFilterSelect = widget.NewSelect([]string{scanFilterAllOption}, nil)
	explorer.typeFilterSelect.SetSelected(scanFilterAllOption)
	explorer.instanceTypeFilterSelect = widget.NewSelect([]string{scanFilterAllOption}, nil)
	explorer.instanceTypeFilterSelect.SetSelected(scanFilterAllOption)
	explorer.propertyNameFilterSelect = widget.NewSelect([]string{scanFilterAllOption}, nil)
	explorer.propertyNameFilterSelect.SetSelected(scanFilterAllOption)
	explorer.statsRowsLabel = widget.NewLabel("Rows: 0")
	explorer.statsShownLabel = widget.NewLabel("Shown: 0")
	explorer.statsFailedLabel = widget.NewLabel("Failed: 0")
	explorer.statsDuplicateLabel = widget.NewLabel("Duplicates: 0")
	explorer.statsDuplicateSizeLabel = widget.NewLabel("Duplicate Size: 0 B")
	explorer.statsSizeLabel = widget.NewLabel("Shown Size: 0 B")
	explorer.statsTrianglesLabel = widget.NewLabel("Shown Triangles: 0")
	explorer.showOnlyDuplicatesCheck = widget.NewCheck("Show only duplicates", func(checked bool) {
		explorer.showOnlyDuplicates = checked
		explorer.applySortAndFilters()
		explorer.clearPreview()
	})
	explorer.showOnlyDuplicatesCheck.SetChecked(false)
	explorer.showOnlyLargeTexturesCheck = widget.NewCheck("Show large textures", func(checked bool) {
		if explorer.suppressLargeTextureFilterChange {
			return
		}
		explorer.showOnlyLargeTextures = checked
		explorer.applySortAndFilters()
		explorer.clearPreview()
	})
	explorer.showOnlyLargeTexturesCheck.SetChecked(false)
	explorer.largeTextureThresholdEntry = widget.NewEntry()
	explorer.largeTextureThresholdEntry.SetPlaceHolder(formatLargeTextureThreshold(defaultLargeTextureThreshold))
	explorer.largeTextureThresholdEntry.SetText(formatLargeTextureThreshold(defaultLargeTextureThreshold))
	explorer.largeTextureThresholdEntry.OnChanged = func(nextValue string) {
		if explorer.suppressLargeTextureFilterChange {
			return
		}
		explorer.largeTextureThreshold = parseLargeTextureThreshold(nextValue)
		explorer.applySortAndFilters()
		explorer.clearPreview()
	}
	explorer.searchEntry.OnChanged = func(nextQuery string) {
		explorer.searchQuery = strings.TrimSpace(nextQuery)
		changeToken := explorer.searchChangeToken.Add(1)
		go func(expectedToken uint64) {
			time.Sleep(scanSearchDebounceDelay)
			fyne.Do(func() {
				if explorer.searchChangeToken.Load() != expectedToken {
					return
				}
				explorer.applySortAndFilters()
				explorer.clearPreview()
			})
		}(changeToken)
	}
	explorer.typeFilterSelect.OnChanged = func(nextFilterValue string) {
		if explorer.suppressTypeFilterChange {
			return
		}
		if strings.TrimSpace(nextFilterValue) == "" {
			explorer.typeFilterValue = scanFilterAllOption
		} else if mappedValue, found := explorer.typeDisplayToValue[nextFilterValue]; found {
			explorer.typeFilterValue = mappedValue
		} else {
			explorer.typeFilterValue = nextFilterValue
		}
		explorer.applySortAndFilters()
		explorer.clearPreview()
	}
	explorer.instanceTypeFilterSelect.OnChanged = func(nextFilterValue string) {
		if explorer.suppressInstanceTypeFilterChange {
			return
		}
		if strings.TrimSpace(nextFilterValue) == "" {
			explorer.instanceTypeFilterValue = scanFilterAllOption
		} else if mappedValue, found := explorer.instanceTypeDisplayToValue[nextFilterValue]; found {
			explorer.instanceTypeFilterValue = mappedValue
		} else {
			explorer.instanceTypeFilterValue = nextFilterValue
		}
		explorer.applySortAndFilters()
		explorer.clearPreview()
	}
	explorer.propertyNameFilterSelect.OnChanged = func(nextFilterValue string) {
		if explorer.suppressPropertyNameFilterChange {
			return
		}
		if strings.TrimSpace(nextFilterValue) == "" {
			explorer.propertyNameFilterValue = scanFilterAllOption
		} else if mappedValue, found := explorer.propertyNameDisplayToValue[nextFilterValue]; found {
			explorer.propertyNameFilterValue = mappedValue
		} else {
			explorer.propertyNameFilterValue = nextFilterValue
		}
		explorer.applySortAndFilters()
		explorer.clearPreview()
	}
	explorer.sortField = explorer.defaultSortField()
	explorer.sortDescending = true
	explorer.buildTable()
	explorer.buildContent(options)
	explorer.updateFilterOptions(map[string]int{})
	explorer.updateStatsLabels()
	explorer.clearPreview()
	if strings.TrimSpace(options.InitialStatusText) == "" {
		explorer.statusLabel.SetText("Ready.")
	}
	return explorer
}

func (explorer *scanResultsExplorer) Content() fyne.CanvasObject {
	return explorer.content
}

func (explorer *scanResultsExplorer) SetStatus(text string) {
	if explorer == nil || explorer.statusLabel == nil {
		return
	}
	explorer.statusLabel.SetText(text)
}

func (explorer *scanResultsExplorer) GetResults() []scanResult {
	rows := make([]scanResult, len(explorer.allResults))
	copy(rows, explorer.allResults)
	return rows
}

func (explorer *scanResultsExplorer) SetResults(rows []scanResult) {
	if explorer == nil {
		return
	}
	nextRows := make([]scanResult, len(rows))
	copy(nextRows, rows)
	explorer.allResults = nextRows
	explorer.selectedAssetID = 0
	explorer.clearPreview()
	explorer.ClearSimilarity()
	explorer.applySortAndFilters()
}

func (explorer *scanResultsExplorer) AppendResults(rows []scanResult, refreshResults bool, refreshFilters bool) {
	if explorer == nil || len(rows) == 0 {
		return
	}
	explorer.allResults = append(explorer.allResults, rows...)
	if refreshResults {
		explorer.applySortAndFilters()
		return
	}
	if refreshFilters {
		hashCounts := buildHashCounts(explorer.allResults)
		explorer.duplicateRowsCount = explorer.countDuplicateRows(hashCounts)
		explorer.duplicateBytesTotal = explorer.countDuplicateBytes(hashCounts)
		explorer.updateFilterOptions(hashCounts)
		explorer.updateStatsLabels()
	}
}

func (explorer *scanResultsExplorer) ClearSimilarity() {
	explorer.similarityActive = false
	explorer.similarityMatchSet = map[int]int{}
	explorer.applySortAndFilters()
}

func (explorer *scanResultsExplorer) SetSimilarityMatches(matchSet map[int]int) {
	explorer.similarityActive = true
	explorer.similarityMatchSet = matchSet
	explorer.applySortAndFilters()
}

func (explorer *scanResultsExplorer) SetSort(field string, descending bool) {
	if explorer == nil || strings.TrimSpace(field) == "" {
		return
	}
	explorer.sortField = field
	explorer.sortDescending = descending
	explorer.applySortAndFilters()
}

func (explorer *scanResultsExplorer) SetControlsEnabled(enabled bool) {
	if explorer == nil {
		return
	}
	explorer.controlsEnabled = enabled
	if enabled {
		explorer.searchEntry.Enable()
		explorer.typeFilterSelect.Enable()
		explorer.instanceTypeFilterSelect.Enable()
		explorer.propertyNameFilterSelect.Enable()
		if explorer.showOnlyDuplicatesCheck != nil {
			explorer.showOnlyDuplicatesCheck.Enable()
		}
		explorer.updateLargeTextureFilterControls()
		return
	}
	explorer.searchEntry.Disable()
	explorer.typeFilterSelect.Disable()
	explorer.instanceTypeFilterSelect.Disable()
	explorer.propertyNameFilterSelect.Disable()
	if explorer.showOnlyDuplicatesCheck != nil {
		explorer.showOnlyDuplicatesCheck.Disable()
	}
	if explorer.showOnlyLargeTexturesCheck != nil {
		explorer.showOnlyLargeTexturesCheck.Disable()
	}
	if explorer.largeTextureThresholdEntry != nil {
		explorer.largeTextureThresholdEntry.Disable()
	}
}

func (explorer *scanResultsExplorer) buildContent(options scanResultsExplorerOptions) {
	filterRow := container.NewHBox(
		widget.NewLabel("Type:"),
		container.NewGridWrap(fyne.NewSize(260, 36), explorer.typeFilterSelect),
		widget.NewLabel("Instance Type:"),
		container.NewGridWrap(fyne.NewSize(280, 36), explorer.instanceTypeFilterSelect),
		widget.NewLabel("Property Name:"),
		container.NewGridWrap(fyne.NewSize(280, 36), explorer.propertyNameFilterSelect),
	)
	if options.ShowDuplicateUI {
		filterRow.Add(explorer.showOnlyDuplicatesCheck)
	}
	if options.ShowLargeTextureUI {
		filterRow.Add(explorer.showOnlyLargeTexturesCheck)
		filterRow.Add(widget.NewLabel("Min B/stud^2:"))
		filterRow.Add(container.NewGridWrap(fyne.NewSize(110, 36), explorer.largeTextureThresholdEntry))
	}
	statsRow := container.NewHBox(
		explorer.statsRowsLabel,
		widget.NewSeparator(),
		explorer.statsShownLabel,
		widget.NewSeparator(),
		explorer.statsFailedLabel,
		widget.NewSeparator(),
		explorer.statsDuplicateLabel,
		widget.NewSeparator(),
		explorer.statsDuplicateSizeLabel,
		widget.NewSeparator(),
		explorer.statsSizeLabel,
		widget.NewSeparator(),
		explorer.statsTrianglesLabel,
	)
	controlItems := []fyne.CanvasObject{}
	if options.HeaderContent != nil {
		controlItems = append(controlItems, options.HeaderContent)
	}
	controlItems = append(controlItems,
		widget.NewLabel("Filters"),
		container.NewBorder(nil, nil, widget.NewLabel("Search:"), nil, explorer.searchEntry),
		filterRow,
		statsRow,
	)
	controls := container.NewVBox(controlItems...)
	previewContent := container.NewVBox(
		explorer.assetDetailsView.PreviewBox,
		explorer.assetDetailsView.HierarchySection,
		explorer.assetDetailsView.MetadataForm,
		explorer.assetDetailsView.JSONAccordion,
		explorer.assetDetailsView.NoteLabel,
	)
	previewScroll := container.NewVScroll(previewContent)
	previewPanel := container.NewBorder(nil, nil, nil, nil, previewScroll)
	split := container.NewHSplit(explorer.table, previewPanel)
	split.Offset = 0.62
	explorer.content = container.NewBorder(
		controls,
		nil,
		nil,
		nil,
		container.NewBorder(explorer.statusLabel, nil, nil, nil, split),
	)
	explorer.updateLargeTextureFilterControls()
}

func (explorer *scanResultsExplorer) hasLargeTextureMetrics() bool {
	for _, row := range explorer.allResults {
		if row.SceneSurfaceArea > 0 && scanResultTextureByteCost(row) > 0 {
			return true
		}
	}
	return false
}

func (explorer *scanResultsExplorer) updateLargeTextureFilterControls() {
	if explorer == nil {
		return
	}
	available := explorer.hasLargeTextureMetrics()
	if !available && explorer.showOnlyLargeTextures {
		explorer.showOnlyLargeTextures = false
		if explorer.showOnlyLargeTexturesCheck != nil {
			explorer.suppressLargeTextureFilterChange = true
			explorer.showOnlyLargeTexturesCheck.SetChecked(false)
			explorer.suppressLargeTextureFilterChange = false
		}
	}
	if explorer.showOnlyLargeTexturesCheck != nil {
		if explorer.controlsEnabled && available {
			explorer.showOnlyLargeTexturesCheck.Enable()
		} else {
			explorer.showOnlyLargeTexturesCheck.Disable()
		}
	}
	if explorer.largeTextureThresholdEntry != nil {
		if explorer.controlsEnabled && available {
			explorer.largeTextureThresholdEntry.Enable()
		} else {
			explorer.largeTextureThresholdEntry.Disable()
		}
	}
}

func (explorer *scanResultsExplorer) buildTable() {
	baseTable := widget.NewTableWithHeaders(
		func() (int, int) {
			return len(explorer.displayResults), len(explorer.columnHeaders)
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
			if id.Row < 0 || id.Row >= len(explorer.displayResults) || id.Col < 0 || id.Col >= len(explorer.columnHeaders) {
				if emojiText.Text != "" {
					emojiText.Text = ""
					emojiText.Refresh()
				}
				label.SetText("")
				return
			}
			row := explorer.displayResults[id.Row]
			nextEmoji := explorer.cellEmoji(row, explorer.columnHeaders[id.Col])
			if emojiText.Text != nextEmoji {
				emojiText.Text = nextEmoji
				emojiText.Refresh()
			}
			label.SetText(explorer.columnValue(row, explorer.columnHeaders[id.Col], id.Row))
		},
	)
	baseTable.CreateHeader = func() fyne.CanvasObject {
		return widget.NewButton("", nil)
	}
	baseTable.UpdateHeader = func(id widget.TableCellID, object fyne.CanvasObject) {
		headerButton := object.(*widget.Button)
		if id.Row == -1 && id.Col >= 0 && id.Col < len(explorer.columnHeaders) {
			columnName := explorer.columnHeaders[id.Col]
			if explorer.sortField == columnName {
				sortIcon := "▲"
				if explorer.sortDescending {
					sortIcon = "▼"
				}
				headerButton.SetText(fmt.Sprintf("%s %s", columnName, sortIcon))
			} else {
				headerButton.SetText(columnName)
			}
			headerButton.OnTapped = func() {
				if explorer.sortField == columnName {
					explorer.sortDescending = !explorer.sortDescending
				} else {
					explorer.sortField = columnName
					explorer.sortDescending = true
				}
				explorer.applySortAndFilters()
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
			explorer.selectedTableColumn = id.Col
		}
		explorer.updatePreviewFromRow(id.Row)
	}
	explorer.table = &secondaryTappableTable{
		Table: baseTable,
		onSecondaryTap: func() {
			if explorer.selectedAssetID <= 0 || explorer.window == nil {
				return
			}
			clipboardValue := loader.ScanAssetReferenceDisplayInput(explorer.selectedAssetID, explorer.selectedResultAssetInput)
			explorer.window.Clipboard().SetContent(clipboardValue)
			if strings.TrimSpace(explorer.selectedResultAssetInput) != "" {
				explorer.statusLabel.SetText("Copied asset reference to clipboard.")
			} else {
				explorer.statusLabel.SetText(fmt.Sprintf("Copied asset ID %d to clipboard.", explorer.selectedAssetID))
			}
		},
	}
}

func (explorer *scanResultsExplorer) defaultSortField() string {
	if explorer.variant == scanResultsExplorerVariantHeatmap {
		return "Total Byte Size"
	}
	return "Self Size"
}

func (explorer *scanResultsExplorer) currentColumnHeaders() []string {
	if explorer.variant == scanResultsExplorerVariantHeatmap {
		headers := []string{"Asset ID", "Type", "Total Byte Size", "Texture Bytes", "Texture Pixels", "Mesh Bytes", "Mesh Triangles", "Property", "Instance Path", "World Position"}
		for _, row := range explorer.allResults {
			if strings.TrimSpace(row.Side) != "" {
				return append([]string{"Side"}, headers...)
			}
		}
		return headers
	}
	if explorer.similarityActive {
		return []string{"Similarity", "Asset ID", "Use Count", "Type", "Self Size", "Dimensions", "Triangles", "Asset SHA256"}
	}
	return []string{"Asset ID", "Use Count", "Type", "Self Size", "Dimensions", "Triangles", "Asset SHA256"}
}

func (explorer *scanResultsExplorer) columnWidths() map[string]float32 {
	if explorer.variant == scanResultsExplorerVariantHeatmap {
		return map[string]float32{
			"Side":            90,
			"Asset ID":        120,
			"Type":            160,
			"Total Byte Size": 110,
			"Texture Bytes":   110,
			"Texture Pixels":  110,
			"Mesh Bytes":      110,
			"Mesh Triangles":  110,
			"Property":        120,
			"Instance Path":   520,
			"World Position":  220,
		}
	}
	return map[string]float32{
		"Similarity":   110,
		"Asset ID":     140,
		"Use Count":    100,
		"Type":         190,
		"Self Size":    90,
		"Dimensions":   120,
		"Triangles":    100,
		"Asset SHA256": 500,
	}
}

func (explorer *scanResultsExplorer) applyColumnWidths() {
	if explorer.table == nil {
		return
	}
	widths := explorer.columnWidths()
	for i, name := range explorer.columnHeaders {
		if width, found := widths[name]; found {
			explorer.table.SetColumnWidth(i, width)
		}
	}
}

func (explorer *scanResultsExplorer) applySortAndFilters() {
	previousSelectedAssetID := explorer.selectedAssetID
	previousSelectedFilePath := explorer.selectedResultFilePath
	previousSelectedAssetInput := explorer.selectedResultAssetInput
	explorer.updateLargeTextureFilterControls()
	filteredResults := make([]scanResult, 0, len(explorer.allResults))
	hashCounts := buildHashCounts(explorer.allResults)
	explorer.duplicateRowsCount = explorer.countDuplicateRows(hashCounts)
	explorer.duplicateBytesTotal = explorer.countDuplicateBytes(hashCounts)
	explorer.updateFilterOptions(hashCounts)
	originalIndices := make([]int, 0, len(explorer.allResults))
	for index, result := range explorer.allResults {
		if explorer.similarityActive {
			if _, matched := explorer.similarityMatchSet[index]; !matched {
				continue
			}
		}
		if !explorer.matchesActiveFilters(result, hashCounts, false, false, false) {
			continue
		}
		filteredResults = append(filteredResults, result)
		originalIndices = append(originalIndices, index)
	}
	if explorer.similarityActive && explorer.variant == scanResultsExplorerVariantScan {
		sort.Sort(similaritySorter{results: filteredResults, indices: originalIndices, matchSet: explorer.similarityMatchSet})
		distances := make([]int, len(filteredResults))
		for idx, origIdx := range originalIndices {
			distances[idx] = explorer.similarityMatchSet[origIdx]
		}
		explorer.displayDistances = distances
	} else {
		sort.Slice(filteredResults, func(leftIndex int, rightIndex int) bool {
			leftResult := filteredResults[leftIndex]
			rightResult := filteredResults[rightIndex]
			compareResult := compareScanResults(leftResult, rightResult, explorer.sortField)
			if compareResult == 0 {
				return leftResult.AssetID < rightResult.AssetID
			}
			if explorer.sortDescending {
				return compareResult > 0
			}
			return compareResult < 0
		})
		explorer.displayDistances = nil
	}
	explorer.displayResults = filteredResults
	explorer.columnHeaders = explorer.currentColumnHeaders()
	nextSelectedRowIndex := -1
	if previousSelectedAssetID > 0 {
		for rowIndex, result := range explorer.displayResults {
			if result.AssetID == previousSelectedAssetID &&
				result.FilePath == previousSelectedFilePath &&
				extractor.AssetReferenceKey(result.AssetID, result.AssetInput) == extractor.AssetReferenceKey(previousSelectedAssetID, previousSelectedAssetInput) {
				nextSelectedRowIndex = rowIndex
				break
			}
		}
	}
	if explorer.table != nil {
		explorer.applyColumnWidths()
		explorer.table.Refresh()
		if nextSelectedRowIndex >= 0 {
			explorer.table.Select(widget.TableCellID{Row: nextSelectedRowIndex, Col: explorer.selectedTableColumn})
		} else if previousSelectedAssetID > 0 {
			explorer.selectedAssetID = 0
			explorer.clearPreview()
		}
	}
	explorer.updateStatsLabels()
}

func (explorer *scanResultsExplorer) matchesActiveFilters(result scanResult, hashCounts map[string]int, ignoreTypeFilter bool, ignoreInstanceTypeFilter bool, ignorePropertyNameFilter bool) bool {
	if explorer.showOnlyDuplicates && !isDuplicateByHash(result, hashCounts) {
		return false
	}
	if explorer.showOnlyLargeTextures && !isLargeTexture(result, explorer.largeTextureThreshold) {
		return false
	}
	if !scanResultMatchesQuery(result, explorer.searchQuery) {
		return false
	}
	if !ignoreTypeFilter && explorer.typeFilterValue != scanFilterAllOption && !strings.EqualFold(scanResultTypeFilterLabel(result), explorer.typeFilterValue) {
		return false
	}
	if !ignoreInstanceTypeFilter && explorer.instanceTypeFilterValue != scanFilterAllOption && !strings.EqualFold(scanResultInstanceTypeLabel(result), explorer.instanceTypeFilterValue) {
		return false
	}
	if !ignorePropertyNameFilter && explorer.propertyNameFilterValue != scanFilterAllOption && !strings.EqualFold(scanResultPropertyNameLabel(result), explorer.propertyNameFilterValue) {
		return false
	}
	return true
}

func (explorer *scanResultsExplorer) countDuplicateRows(hashCounts map[string]int) int {
	duplicateCount := 0
	seenCounts := map[string]int{}
	for _, row := range explorer.allResults {
		normalizedHash := normalizeHash(row.FileSHA256)
		if normalizedHash == "" || hashCounts[normalizedHash] < 2 {
			continue
		}
		if seenCounts[normalizedHash] >= 1 {
			duplicateCount++
		}
		seenCounts[normalizedHash]++
	}
	return duplicateCount
}

func (explorer *scanResultsExplorer) countDuplicateBytes(hashCounts map[string]int) int {
	duplicateBytes := 0
	seenCounts := map[string]int{}
	for _, row := range explorer.allResults {
		normalizedHash := normalizeHash(row.FileSHA256)
		if normalizedHash == "" || hashCounts[normalizedHash] < 2 {
			continue
		}
		if row.BytesSize <= 0 {
			seenCounts[normalizedHash]++
			continue
		}
		if seenCounts[normalizedHash] >= 1 {
			duplicateBytes += row.BytesSize
		}
		seenCounts[normalizedHash]++
	}
	return duplicateBytes
}

func (explorer *scanResultsExplorer) updateStatsLabels() {
	totalRowsCount := len(explorer.allResults)
	shownRowsCount := len(explorer.displayResults)
	failedRowsCount := 0
	shownBytesTotal := 0
	shownTrianglesTotal := uint64(0)
	for _, row := range explorer.displayResults {
		if row.State == failedScanRowState {
			failedRowsCount++
		}
		if explorer.variant == scanResultsExplorerVariantHeatmap {
			if row.TotalBytesSize > 0 {
				shownBytesTotal += row.TotalBytesSize
			}
		} else if row.BytesSize > 0 {
			shownBytesTotal += row.BytesSize
		}
		if row.MeshNumFaces > 0 {
			shownTrianglesTotal += uint64(row.MeshNumFaces)
		}
	}
	explorer.statsRowsLabel.SetText(fmt.Sprintf("Rows: %d", totalRowsCount))
	explorer.statsShownLabel.SetText(fmt.Sprintf("Shown: %d", shownRowsCount))
	explorer.statsFailedLabel.SetText(fmt.Sprintf("Failed: %d", failedRowsCount))
	explorer.statsDuplicateLabel.SetText(fmt.Sprintf("Duplicates: %d", explorer.duplicateRowsCount))
	explorer.statsDuplicateSizeLabel.SetText(fmt.Sprintf("Duplicate Size: %s", format.FormatSizeAuto(explorer.duplicateBytesTotal)))
	explorer.statsSizeLabel.SetText(fmt.Sprintf("Shown Size: %s", format.FormatSizeAuto(shownBytesTotal)))
	explorer.statsTrianglesLabel.SetText(fmt.Sprintf("Shown Triangles: %d", shownTrianglesTotal))
}

func (explorer *scanResultsExplorer) updateFilterOptions(hashCounts map[string]int) {
	typeCounts := map[string]int{}
	instanceTypeCounts := map[string]int{}
	propertyNameCounts := map[string]int{}
	for _, row := range explorer.allResults {
		typeLabel := scanResultTypeFilterLabel(row)
		if typeLabel != "" && explorer.matchesActiveFilters(row, hashCounts, true, false, false) {
			typeCounts[typeLabel]++
		}
		instanceTypeLabel := scanResultInstanceTypeLabel(row)
		if instanceTypeLabel != "" && explorer.matchesActiveFilters(row, hashCounts, false, true, false) {
			instanceTypeCounts[instanceTypeLabel]++
		}
		propertyNameLabel := scanResultPropertyNameLabel(row)
		if propertyNameLabel != "" && explorer.matchesActiveFilters(row, hashCounts, false, false, true) {
			propertyNameCounts[propertyNameLabel]++
		}
	}
	updateSelectWithCounts(explorer.typeFilterSelect, &explorer.suppressTypeFilterChange, &explorer.typeFilterValue, &explorer.typeDisplayToValue, typeCounts)
	updateSelectWithCounts(explorer.instanceTypeFilterSelect, &explorer.suppressInstanceTypeFilterChange, &explorer.instanceTypeFilterValue, &explorer.instanceTypeDisplayToValue, instanceTypeCounts)
	updateSelectWithCounts(explorer.propertyNameFilterSelect, &explorer.suppressPropertyNameFilterChange, &explorer.propertyNameFilterValue, &explorer.propertyNameDisplayToValue, propertyNameCounts)
}

func updateSelectWithCounts(selectWidget *widget.Select, suppress *bool, selectedValue *string, valueMap *map[string]string, counts map[string]int) {
	if selectWidget == nil || suppress == nil || selectedValue == nil || valueMap == nil {
		return
	}
	if *selectedValue != scanFilterAllOption {
		if _, found := counts[*selectedValue]; !found {
			counts[*selectedValue] = 0
		}
	}
	options := []string{scanFilterAllOption}
	*valueMap = map[string]string{scanFilterAllOption: scanFilterAllOption}
	labels := make([]string, 0, len(counts))
	for label := range counts {
		labels = append(labels, label)
	}
	sort.Strings(labels)
	for _, label := range labels {
		displayLabel := fmt.Sprintf("%s (%d)", label, counts[label])
		options = append(options, displayLabel)
		(*valueMap)[displayLabel] = label
	}
	*suppress = true
	selectWidget.SetOptions(options)
	selectedOption := scanFilterAllOption
	for optionLabel, optionValue := range *valueMap {
		if optionValue == *selectedValue {
			selectedOption = optionLabel
			break
		}
	}
	if !containsString(options, selectedOption) {
		*selectedValue = scanFilterAllOption
		selectedOption = scanFilterAllOption
	}
	selectWidget.SetSelected(selectedOption)
	*suppress = false
}

func (explorer *scanResultsExplorer) clearPreview() {
	explorer.selectedResultFilePath = ""
	explorer.selectedResultAssetInput = ""
	explorer.selectedTableColumn = 0
	explorer.assetDetailsView.Clear()
}

func (explorer *scanResultsExplorer) renderSelectedAsset(selectedAssetID int64, selectedFilePath string, previewResult *loader.AssetPreviewResult) {
	context := assetReferenceContext{}
	if explorer.explorerState != nil && selectedAssetID == explorer.explorerState.rootAssetID {
		context = buildRootScanReferenceContext(
			explorer.allResults,
			selectedAssetID,
			explorer.selectedResultAssetInput,
			selectedFilePath,
			previewSHA256(previewResult),
		)
	} else {
		context = buildExplorerSelectionReferenceContext(explorer.explorerState, selectedAssetID)
		context.FileSHA256 = previewSHA256(previewResult)
	}
	explorer.assetDetailsView.SetData(buildAssetViewDataFromPreview(selectedAssetID, previewResult, context))
	explorer.assetDetailsView.SetHierarchy(explorer.explorerState.getRows(), selectedAssetID, func(assetID int64) {
		if explorer.explorerState == nil {
			return
		}
		explorer.statusLabel.SetText(fmt.Sprintf("Loading asset %d...", assetID))
		go func() {
			selectedPreview, selectErr, requestSource := explorer.explorerState.selectAssetWithRequestSource(assetID)
			fyne.Do(func() {
				if selectErr != nil {
					explorer.statusLabel.SetText(selectErr.Error())
					return
				}
				explorer.renderSelectedAsset(assetID, selectedFilePath, selectedPreview)
				explorer.statusLabel.SetText(fmt.Sprintf(
					"Showing asset %d. %s",
					assetID,
					heatmap.FormatSingleRequestSourceBreakdown(requestSource),
				))
			})
		}()
	})
}

func (explorer *scanResultsExplorer) updatePreviewFromRow(rowIndex int) {
	if rowIndex < 0 || rowIndex >= len(explorer.displayResults) {
		explorer.selectedAssetID = 0
		explorer.selectedResultFilePath = ""
		explorer.selectedTableColumn = 0
		return
	}
	selectedResult := explorer.displayResults[rowIndex]
	explorer.selectedAssetID = selectedResult.AssetID
	explorer.selectedResultFilePath = selectedResult.FilePath
	explorer.selectedResultAssetInput = selectedResult.AssetInput
	rootPreview := scanResultToPreviewResult(selectedResult)
	explorer.explorerState = newAssetExplorerState(selectedResult.AssetID, rootPreview)
	explorer.renderSelectedAsset(selectedResult.AssetID, selectedResult.FilePath, rootPreview)
	needsFullPreview := selectedResult.AssetID > 0 && selectedResult.State != failedScanRowState &&
		(selectedResult.Resource == nil || selectedResult.Source == roblox.SourceNoThumbnail || strings.TrimSpace(selectedResult.Source) == "")
	if !needsFullPreview {
		return
	}
	assetToLoad := selectedResult.AssetID
	filePathToLoad := selectedResult.FilePath
	assetInputToLoad := selectedResult.AssetInput
	explorer.statusLabel.SetText(fmt.Sprintf("Loading asset %d...", assetToLoad))
	go func() {
		loadRequest, requestErr := loader.BuildSingleAssetLoadRequest(assetToLoad, assetInputToLoad)
		if requestErr != nil {
			return
		}
		trace := &loader.AssetRequestTrace{}
		fullPreview, loadErr := loader.LoadSingleAssetPreviewWithTrace(loadRequest, trace)
		fyne.Do(func() {
			if explorer.selectedAssetID != assetToLoad ||
				extractor.AssetReferenceKey(explorer.selectedAssetID, explorer.selectedResultAssetInput) != extractor.AssetReferenceKey(assetToLoad, assetInputToLoad) {
				return
			}
			if requestErr != nil || loadErr != nil || fullPreview == nil {
				return
			}
			selectedResult = applyPreviewToScanResult(selectedResult, fullPreview)
			rootPreview := scanResultToPreviewResult(selectedResult)
			explorer.explorerState = newAssetExplorerState(assetToLoad, rootPreview)
			explorer.renderSelectedAsset(assetToLoad, filePathToLoad, rootPreview)
			explorer.statusLabel.SetText(fmt.Sprintf(
				"Showing asset %d. %s",
				assetToLoad,
				heatmap.FormatSingleRequestSourceBreakdown(trace.ClassifyRequestSource()),
			))
		})
	}()
}

func (explorer *scanResultsExplorer) cellEmoji(row scanResult, columnName string) string {
	if explorer.variant == scanResultsExplorerVariantScan && columnName == "Type" {
		return roblox.GetAssetTypeEmoji(row.AssetTypeID)
	}
	return ""
}

func (explorer *scanResultsExplorer) columnValue(row scanResult, columnName string, rowIndex int) string {
	switch columnName {
	case "Similarity":
		if rowIndex < len(explorer.displayDistances) {
			dist := explorer.displayDistances[rowIndex]
			pct := int(100 - float64(dist)*100/64)
			return fmt.Sprintf("%d%% (%d)", pct, dist)
		}
		return "-"
	case "Side":
		if strings.TrimSpace(row.Side) == "" {
			return "-"
		}
		return row.Side
	case "Asset ID":
		return strconv.FormatInt(row.AssetID, 10)
	case "Use Count":
		if row.UseCount > 0 {
			return strconv.Itoa(row.UseCount)
		}
		return "-"
	case "Type":
		return scanResultTypeLabel(row)
	case "Self Size":
		return format.FormatSizeAuto(row.BytesSize)
	case "Total Byte Size":
		return format.FormatSizeAuto(row.TotalBytesSize)
	case "Texture Bytes":
		return format.FormatSizeAuto(row.TextureBytes)
	case "Texture Pixels":
		if row.PixelCount > 0 {
			return format.FormatIntCommas(row.PixelCount)
		}
		return "-"
	case "Mesh Bytes":
		return format.FormatSizeAuto(row.MeshBytes)
	case "Mesh Triangles", "Triangles":
		if row.MeshNumFaces > 0 {
			return format.FormatIntCommas(int64(row.MeshNumFaces))
		}
		return "-"
	case "Dimensions":
		if row.Width > 0 && row.Height > 0 {
			return fmt.Sprintf("%dx%d", row.Width, row.Height)
		}
		return "-"
	case "Asset SHA256":
		if strings.TrimSpace(row.FileSHA256) == "" {
			return "-"
		}
		return row.FileSHA256
	case "Property":
		if strings.TrimSpace(row.PropertyName) == "" {
			return "-"
		}
		return row.PropertyName
	case "Instance Path":
		if strings.TrimSpace(row.InstancePath) == "" {
			return "-"
		}
		return row.InstancePath
	case "World Position":
		return fmt.Sprintf("X %.1f, Y %.1f, Z %.1f", row.WorldX, row.WorldY, row.WorldZ)
	default:
		return ""
	}
}
