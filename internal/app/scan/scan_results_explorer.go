package scan

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"joxblox/internal/app/loader"
	"joxblox/internal/app/ui"
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

type ScanResultsExplorerVariant string

const (
	ScanResultsExplorerVariantScan    ScanResultsExplorerVariant = "scan"
	ScanResultsExplorerVariantHeatmap ScanResultsExplorerVariant = "heatmap"
)

type ScanResultsExplorerOptions struct {
	Variant            ScanResultsExplorerVariant
	PreviewPlaceholder string
	IncludeFileRow     bool
	InitialStatusText  string
	SearchPlaceholder  string
	HeaderContent      fyne.CanvasObject
	ShowDuplicateUI    bool
	ShowLargeTextureUI bool
}

type ScanResultsExplorer struct {
	window                           fyne.Window
	variant                          ScanResultsExplorerVariant
	content                          fyne.CanvasObject
	statusLabel                      *widget.Label
	allResults                       []loader.ScanResult
	displayResults                   []loader.ScanResult
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
	assetDetailsView                 *ui.AssetView
	explorerState                    *ui.AssetExplorerState
	table                            *secondaryTappableTable
	typeFilterSelect                 *widget.Select
	instanceTypeFilterSelect         *widget.Select
	propertyNameFilterSelect         *widget.Select
	statsFailedLabel                 *widget.Label
	statsDuplicateLabel              *widget.Label
	statsDuplicateSizeLabel          *widget.Label
	statsSizeLabel                   *widget.Label
	statsGPUMemoryLabel              *widget.Label
	statsTrianglesLabel              *widget.Label
	statsTotalTrianglesLabel         *widget.Label
	searchEntry                      *widget.Entry
	compiledSearchQuery              loader.ScanQuery
	versionIndex                     []string
	searchSuggestionsBox             *fyne.Container
	searchSuggestionsRow             fyne.CanvasObject
	showOnlyDuplicatesCheck          *widget.Check
	showOnlyLargeTexturesCheck       *widget.Check
	largeTextureThresholdEntry       *widget.Entry
}

func NewScanResultsExplorer(window fyne.Window, options ScanResultsExplorerOptions) *ScanResultsExplorer {
	previewPlaceholder := strings.TrimSpace(options.PreviewPlaceholder)
	if previewPlaceholder == "" {
		previewPlaceholder = "Select a result row to preview"
	}
	searchPlaceholder := strings.TrimSpace(options.SearchPlaceholder)
	if searchPlaceholder == "" {
		searchPlaceholder = `Search: v3.0 · type:mesh · size:>1mb · name:/v\d+\.\d+/`
	}
	explorer := &ScanResultsExplorer{
		window:                     window,
		variant:                    options.Variant,
		statusLabel:                widget.NewLabel(options.InitialStatusText),
		typeFilterValue:            loader.ScanFilterAllOption,
		typeDisplayToValue:         map[string]string{loader.ScanFilterAllOption: loader.ScanFilterAllOption},
		instanceTypeFilterValue:    loader.ScanFilterAllOption,
		instanceTypeDisplayToValue: map[string]string{loader.ScanFilterAllOption: loader.ScanFilterAllOption},
		propertyNameFilterValue:    loader.ScanFilterAllOption,
		propertyNameDisplayToValue: map[string]string{loader.ScanFilterAllOption: loader.ScanFilterAllOption},
		selectedTableColumn:        0,
		similarityMatchSet:         map[int]int{},
		largeTextureThreshold:      loader.DefaultLargeTextureThreshold,
		controlsEnabled:            true,
	}
	explorer.assetDetailsView = ui.NewAssetView(previewPlaceholder, options.IncludeFileRow)
	explorer.searchEntry = widget.NewEntry()
	explorer.searchEntry.SetPlaceHolder(searchPlaceholder)
	explorer.typeFilterSelect = widget.NewSelect([]string{loader.ScanFilterAllOption}, nil)
	explorer.typeFilterSelect.SetSelected(loader.ScanFilterAllOption)
	explorer.instanceTypeFilterSelect = widget.NewSelect([]string{loader.ScanFilterAllOption}, nil)
	explorer.instanceTypeFilterSelect.SetSelected(loader.ScanFilterAllOption)
	explorer.propertyNameFilterSelect = widget.NewSelect([]string{loader.ScanFilterAllOption}, nil)
	explorer.propertyNameFilterSelect.SetSelected(loader.ScanFilterAllOption)
	explorer.statsFailedLabel = widget.NewLabel("Failed: 0")
	explorer.statsDuplicateLabel = widget.NewLabel("Duplicates: 0")
	explorer.statsDuplicateSizeLabel = widget.NewLabel("Duplicate Size: 0 B")
	explorer.statsSizeLabel = widget.NewLabel("Shown Size: 0 B")
	explorer.statsGPUMemoryLabel = widget.NewLabel("Shown GPU Memory: 0 B")
	explorer.statsTrianglesLabel = widget.NewLabel("Shown Triangles: 0")
	explorer.statsTotalTrianglesLabel = widget.NewLabel("Shown Total Triangles: 0")
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
	explorer.largeTextureThresholdEntry.SetPlaceHolder(loader.FormatLargeTextureThreshold(loader.DefaultLargeTextureThreshold))
	explorer.largeTextureThresholdEntry.SetText(loader.FormatLargeTextureThreshold(loader.DefaultLargeTextureThreshold))
	explorer.largeTextureThresholdEntry.OnChanged = func(nextValue string) {
		if explorer.suppressLargeTextureFilterChange {
			return
		}
		explorer.largeTextureThreshold = loader.ParseLargeTextureThreshold(nextValue)
		explorer.applySortAndFilters()
		explorer.clearPreview()
	}
	explorer.searchEntry.OnChanged = func(nextQuery string) {
		explorer.searchQuery = strings.TrimSpace(nextQuery)
		explorer.compiledSearchQuery = loader.CompileScanQuery(explorer.searchQuery)
		explorer.refreshSearchSuggestions()
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
			explorer.typeFilterValue = loader.ScanFilterAllOption
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
			explorer.instanceTypeFilterValue = loader.ScanFilterAllOption
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
			explorer.propertyNameFilterValue = loader.ScanFilterAllOption
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

func (explorer *ScanResultsExplorer) Content() fyne.CanvasObject {
	return explorer.content
}

func (explorer *ScanResultsExplorer) SetStatus(text string) {
	if explorer == nil || explorer.statusLabel == nil {
		return
	}
	explorer.statusLabel.SetText(text)
}

func (explorer *ScanResultsExplorer) GetResults() []loader.ScanResult {
	rows := make([]loader.ScanResult, len(explorer.allResults))
	copy(rows, explorer.allResults)
	return rows
}

func (explorer *ScanResultsExplorer) SetResults(rows []loader.ScanResult) {
	if explorer == nil {
		return
	}
	nextRows := make([]loader.ScanResult, len(rows))
	copy(nextRows, rows)
	explorer.allResults = nextRows
	explorer.selectedAssetID = 0
	explorer.versionIndex = loader.ExtractVersionsFromResults(explorer.allResults)
	explorer.refreshSearchSuggestions()
	explorer.clearPreview()
	explorer.ClearSimilarity()
	explorer.applySortAndFilters()
	loader.PublishScanCompleted(explorer.allResults)
}

func (explorer *ScanResultsExplorer) AppendResults(rows []loader.ScanResult, refreshResults bool, refreshFilters bool) {
	if explorer == nil || len(rows) == 0 {
		return
	}
	explorer.allResults = append(explorer.allResults, rows...)
	if refreshResults {
		explorer.versionIndex = loader.ExtractVersionsFromResults(explorer.allResults)
		explorer.refreshSearchSuggestions()
		explorer.applySortAndFilters()
		return
	}
	if refreshFilters {
		hashCounts := loader.BuildHashCounts(explorer.allResults)
		explorer.duplicateRowsCount = explorer.countDuplicateRows(hashCounts)
		explorer.duplicateBytesTotal = explorer.countDuplicateBytes(hashCounts)
		explorer.updateFilterOptions(hashCounts)
		explorer.updateStatsLabels()
		explorer.versionIndex = loader.ExtractVersionsFromResults(explorer.allResults)
		explorer.refreshSearchSuggestions()
	}
}

// PublishCompleted notifies the loader event bus that a scan has fully
// completed and the current results are stable. Call this once at the
// end of a streaming scan; AppendResults intentionally no longer
// publishes on every batch since that swamped subscribers (notably the
// RenderDoc asset-ID corpus builder) with redundant rebuilds.
func (explorer *ScanResultsExplorer) PublishCompleted() {
	if explorer == nil {
		return
	}
	loader.PublishScanCompleted(explorer.allResults)
}

func (explorer *ScanResultsExplorer) ClearSimilarity() {
	explorer.similarityActive = false
	explorer.similarityMatchSet = map[int]int{}
	explorer.applySortAndFilters()
}

func (explorer *ScanResultsExplorer) SetSimilarityMatches(matchSet map[int]int) {
	explorer.similarityActive = true
	explorer.similarityMatchSet = matchSet
	explorer.applySortAndFilters()
}

func (explorer *ScanResultsExplorer) SetSort(field string, descending bool) {
	if explorer == nil || strings.TrimSpace(field) == "" {
		return
	}
	explorer.sortField = field
	explorer.sortDescending = descending
	explorer.applySortAndFilters()
}

func (explorer *ScanResultsExplorer) SetControlsEnabled(enabled bool) {
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

func (explorer *ScanResultsExplorer) buildContent(options ScanResultsExplorerOptions) {
	filterRow := container.NewHBox(
		widget.NewLabel("Type:"),
		container.NewGridWrap(fyne.NewSize(260, 36), explorer.typeFilterSelect),
		widget.NewLabel("Instance Type:"),
		container.NewGridWrap(fyne.NewSize(280, 36), explorer.instanceTypeFilterSelect),
		widget.NewLabel("Property Name:"),
		container.NewGridWrap(fyne.NewSize(280, 36), explorer.propertyNameFilterSelect),
	)
	// The filter row sums to ~1020px minimum across its fixed-width grids; a
	// bare HBox would force the whole window to stay that wide because AppTabs
	// inherits the largest MinSize across its children. Wrap it in an HScroll
	// so the window can shrink below that threshold (the row then scrolls
	// horizontally instead of pinning the window).
	filterRowScroll := container.NewHScroll(filterRow)
	if options.ShowDuplicateUI {
		filterRow.Add(explorer.showOnlyDuplicatesCheck)
	}
	if options.ShowLargeTextureUI {
		filterRow.Add(explorer.showOnlyLargeTexturesCheck)
		filterRow.Add(widget.NewLabel("Min B/stud^2:"))
		filterRow.Add(container.NewGridWrap(fyne.NewSize(110, 36), explorer.largeTextureThresholdEntry))
	}
	statsRow := container.NewHBox(
		explorer.statsFailedLabel,
		widget.NewSeparator(),
		explorer.statsDuplicateLabel,
		widget.NewSeparator(),
		explorer.statsDuplicateSizeLabel,
		widget.NewSeparator(),
		explorer.statsSizeLabel,
		widget.NewSeparator(),
		explorer.statsGPUMemoryLabel,
		widget.NewSeparator(),
		explorer.statsTrianglesLabel,
		widget.NewSeparator(),
		explorer.statsTotalTrianglesLabel,
	)
	controlItems := []fyne.CanvasObject{}
	if options.HeaderContent != nil {
		controlItems = append(controlItems, options.HeaderContent)
	}
	explorer.searchSuggestionsBox = container.NewHBox()
	explorer.searchSuggestionsRow = container.NewHScroll(explorer.searchSuggestionsBox)
	explorer.searchSuggestionsRow.Hide()
	// statsRow also strings many labels together horizontally; wrap for the
	// same reason.
	statsRowScroll := container.NewHScroll(statsRow)
	controlItems = append(controlItems,
		widget.NewLabel("Filters"),
		container.NewBorder(nil, nil, widget.NewLabel("Search:"), nil, explorer.searchEntry),
		explorer.searchSuggestionsRow,
		filterRowScroll,
		statsRowScroll,
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

func (explorer *ScanResultsExplorer) hasLargeTextureMetrics() bool {
	for _, row := range explorer.allResults {
		if row.SceneSurfaceArea > 0 && loader.ScanResultTextureByteCost(row) > 0 {
			return true
		}
	}
	return false
}

func (explorer *ScanResultsExplorer) updateLargeTextureFilterControls() {
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

func (explorer *ScanResultsExplorer) SetLargeTextureThreshold(threshold float64) {
	if explorer == nil {
		return
	}
	if threshold <= 0 {
		threshold = loader.DefaultLargeTextureThreshold
	}
	explorer.largeTextureThreshold = threshold
	if explorer.largeTextureThresholdEntry != nil {
		explorer.suppressLargeTextureFilterChange = true
		explorer.largeTextureThresholdEntry.SetText(loader.FormatLargeTextureThreshold(threshold))
		explorer.suppressLargeTextureFilterChange = false
	}
}

func (explorer *ScanResultsExplorer) buildTable() {
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

func (explorer *ScanResultsExplorer) defaultSortField() string {
	if explorer.variant == ScanResultsExplorerVariantHeatmap {
		return "Total Byte Size"
	}
	return "Self Size"
}

func (explorer *ScanResultsExplorer) currentColumnHeaders() []string {
	if explorer.variant == ScanResultsExplorerVariantHeatmap {
		headers := []string{"Asset ID", "Type", "Total Byte Size", "Texture Bytes", "Texture Pixels", "GPU Memory", "B/stud²", "Mesh Bytes", "Mesh Triangles", "Total Triangles", "Property", "Instance Path", "World Position"}
		for _, row := range explorer.allResults {
			if strings.TrimSpace(row.Side) != "" {
				return append([]string{"Side"}, headers...)
			}
		}
		return headers
	}
	if explorer.similarityActive {
		return []string{"Similarity", "Asset ID", "Use Count", "Type", "Self Size", "GPU Memory", "B/stud²", "Dimensions", "Triangles", "Total Triangles", "Asset SHA256"}
	}
	return []string{"Asset ID", "Use Count", "Type", "Self Size", "GPU Memory", "B/stud²", "Dimensions", "Triangles", "Total Triangles", "Asset SHA256"}
}

func (explorer *ScanResultsExplorer) columnWidths() map[string]float32 {
	if explorer.variant == ScanResultsExplorerVariantHeatmap {
		return map[string]float32{
			"Side":            90,
			"Asset ID":        120,
			"Type":            160,
			"Total Byte Size": 110,
			"Texture Bytes":   110,
			"Texture Pixels":  110,
			"GPU Memory":      140,
			"B/stud²":         120,
			"Mesh Bytes":      110,
			"Mesh Triangles":  110,
			"Total Triangles": 120,
			"Property":        120,
			"Instance Path":   520,
			"World Position":  220,
		}
	}
	return map[string]float32{
		"Similarity":      110,
		"Asset ID":        140,
		"Use Count":       100,
		"Type":            190,
		"Self Size":       90,
		"GPU Memory":      140,
		"B/stud²":         120,
		"Dimensions":      120,
		"Triangles":       100,
		"Total Triangles": 120,
		"Asset SHA256":    500,
	}
}

func (explorer *ScanResultsExplorer) applyColumnWidths() {
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

func (explorer *ScanResultsExplorer) applySortAndFilters() {
	previousSelectedAssetID := explorer.selectedAssetID
	previousSelectedFilePath := explorer.selectedResultFilePath
	previousSelectedAssetInput := explorer.selectedResultAssetInput
	explorer.updateLargeTextureFilterControls()
	filteredResults := make([]loader.ScanResult, 0, len(explorer.allResults))
	hashCounts := loader.BuildHashCounts(explorer.allResults)
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
	if explorer.similarityActive && explorer.variant == ScanResultsExplorerVariantScan {
		sort.Sort(loader.SimilarityRowSorter{Results: filteredResults, Indices: originalIndices, MatchSet: explorer.similarityMatchSet})
		distances := make([]int, len(filteredResults))
		for idx, origIdx := range originalIndices {
			distances[idx] = explorer.similarityMatchSet[origIdx]
		}
		explorer.displayDistances = distances
	} else {
		sort.Slice(filteredResults, func(leftIndex int, rightIndex int) bool {
			leftResult := filteredResults[leftIndex]
			rightResult := filteredResults[rightIndex]
			compareResult := loader.CompareScanResults(leftResult, rightResult, explorer.sortField)
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

func (explorer *ScanResultsExplorer) matchesActiveFilters(result loader.ScanResult, hashCounts map[string]int, ignoreTypeFilter bool, ignoreInstanceTypeFilter bool, ignorePropertyNameFilter bool) bool {
	if explorer.showOnlyDuplicates && !loader.IsDuplicateByHash(result, hashCounts) {
		return false
	}
	if explorer.showOnlyLargeTextures && !loader.IsLargeTexture(result, explorer.largeTextureThreshold) {
		return false
	}
	if !loader.ScanResultMatchesCompiledQuery(result, explorer.compiledSearchQuery, loader.ScanQueryContext{HashCounts: hashCounts}) {
		return false
	}
	if !ignoreTypeFilter && explorer.typeFilterValue != loader.ScanFilterAllOption && !strings.EqualFold(loader.ScanResultTypeFilterLabel(result), explorer.typeFilterValue) {
		return false
	}
	if !ignoreInstanceTypeFilter && explorer.instanceTypeFilterValue != loader.ScanFilterAllOption && !strings.EqualFold(loader.ScanResultInstanceTypeLabel(result), explorer.instanceTypeFilterValue) {
		return false
	}
	if !ignorePropertyNameFilter && explorer.propertyNameFilterValue != loader.ScanFilterAllOption && !strings.EqualFold(loader.ScanResultPropertyNameLabel(result), explorer.propertyNameFilterValue) {
		return false
	}
	return true
}

func (explorer *ScanResultsExplorer) countDuplicateRows(hashCounts map[string]int) int {
	duplicateCount := 0
	seenCounts := map[string]int{}
	for _, row := range explorer.allResults {
		normalizedHash := loader.NormalizeHash(row.FileSHA256)
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

func (explorer *ScanResultsExplorer) countDuplicateBytes(hashCounts map[string]int) int {
	duplicateBytes := 0
	seenCounts := map[string]int{}
	for _, row := range explorer.allResults {
		normalizedHash := loader.NormalizeHash(row.FileSHA256)
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

func (explorer *ScanResultsExplorer) updateStatsLabels() {
	failedRowsCount := 0
	shownBytesTotal := 0
	shownGPUBytesTotal := int64(0)
	shownTrianglesTotal := uint64(0)
	shownTotalTrianglesTotal := uint64(0)
	for _, row := range explorer.displayResults {
		if row.State == loader.FailedScanRowState {
			failedRowsCount++
		}
		if explorer.variant == ScanResultsExplorerVariantHeatmap {
			if row.TotalBytesSize > 0 {
				shownBytesTotal += row.TotalBytesSize
			}
		} else if row.BytesSize > 0 {
			shownBytesTotal += row.BytesSize
		}
		shownGPUBytesTotal += loader.ScanResultGPUMemoryBytes(row)
		if row.MeshNumFaces > 0 {
			shownTrianglesTotal += uint64(row.MeshNumFaces)
			if row.UseCount > 0 {
				shownTotalTrianglesTotal += uint64(row.MeshNumFaces) * uint64(row.UseCount)
			}
		}
	}
	explorer.statsFailedLabel.SetText(fmt.Sprintf("Failed: %d", failedRowsCount))
	explorer.statsDuplicateLabel.SetText(fmt.Sprintf("Duplicates: %d", explorer.duplicateRowsCount))
	explorer.statsDuplicateSizeLabel.SetText(fmt.Sprintf("Duplicate Size: %s", format.FormatSizeAuto(explorer.duplicateBytesTotal)))
	explorer.statsSizeLabel.SetText(fmt.Sprintf("Shown Size: %s", format.FormatSizeAuto(shownBytesTotal)))
	explorer.statsGPUMemoryLabel.SetText(fmt.Sprintf("Shown GPU Memory: %s", format.FormatSizeAuto64(shownGPUBytesTotal)))
	explorer.statsTrianglesLabel.SetText(fmt.Sprintf("Shown Triangles: %d", shownTrianglesTotal))
	explorer.statsTotalTrianglesLabel.SetText(fmt.Sprintf("Shown Total Triangles: %d", shownTotalTrianglesTotal))
}

func (explorer *ScanResultsExplorer) updateFilterOptions(hashCounts map[string]int) {
	typeCounts := map[string]int{}
	instanceTypeCounts := map[string]int{}
	propertyNameCounts := map[string]int{}
	for _, row := range explorer.allResults {
		typeLabel := loader.ScanResultTypeFilterLabel(row)
		if typeLabel != "" && explorer.matchesActiveFilters(row, hashCounts, true, false, false) {
			typeCounts[typeLabel]++
		}
		instanceTypeLabel := loader.ScanResultInstanceTypeLabel(row)
		if instanceTypeLabel != "" && explorer.matchesActiveFilters(row, hashCounts, false, true, false) {
			instanceTypeCounts[instanceTypeLabel]++
		}
		propertyNameLabel := loader.ScanResultPropertyNameLabel(row)
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
	if *selectedValue != loader.ScanFilterAllOption {
		if _, found := counts[*selectedValue]; !found {
			counts[*selectedValue] = 0
		}
	}
	options := []string{loader.ScanFilterAllOption}
	*valueMap = map[string]string{loader.ScanFilterAllOption: loader.ScanFilterAllOption}
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
	selectedOption := loader.ScanFilterAllOption
	for optionLabel, optionValue := range *valueMap {
		if optionValue == *selectedValue {
			selectedOption = optionLabel
			break
		}
	}
	if !loader.ContainsString(options, selectedOption) {
		*selectedValue = loader.ScanFilterAllOption
		selectedOption = loader.ScanFilterAllOption
	}
	selectWidget.SetSelected(selectedOption)
	*suppress = false
}

func (explorer *ScanResultsExplorer) clearPreview() {
	explorer.selectedResultFilePath = ""
	explorer.selectedResultAssetInput = ""
	explorer.selectedTableColumn = 0
	explorer.assetDetailsView.Clear()
}

func (explorer *ScanResultsExplorer) renderSelectedAsset(selectedAssetID int64, selectedFilePath string, previewResult *loader.AssetPreviewResult) {
	context := loader.AssetReferenceContext{}
	if explorer.explorerState != nil && selectedAssetID == explorer.explorerState.RootAssetID {
		context = loader.BuildRootScanReferenceContext(
			explorer.allResults,
			selectedAssetID,
			explorer.selectedResultAssetInput,
			selectedFilePath,
			loader.PreviewSHA256(previewResult),
		)
	} else {
		context = ui.BuildExplorerSelectionReferenceContext(explorer.explorerState, selectedAssetID)
		context.FileSHA256 = loader.PreviewSHA256(previewResult)
	}
	explorer.assetDetailsView.SetData(loader.BuildAssetViewDataFromPreview(selectedAssetID, previewResult, context))
	explorer.assetDetailsView.SetHierarchy(explorer.explorerState.GetRows(), selectedAssetID, func(assetID int64) {
		if explorer.explorerState == nil {
			return
		}
		explorer.statusLabel.SetText(fmt.Sprintf("Loading asset %d...", assetID))
		go func() {
			selectedPreview, selectErr, requestSource := explorer.explorerState.SelectAssetWithRequestSource(assetID)
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

func (explorer *ScanResultsExplorer) updatePreviewFromRow(rowIndex int) {
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
	rootPreview := loader.ScanResultToPreviewResult(selectedResult)
	explorer.explorerState = ui.NewAssetExplorerState(selectedResult.AssetID, rootPreview)
	explorer.renderSelectedAsset(selectedResult.AssetID, selectedResult.FilePath, rootPreview)
	needsFullPreview := selectedResult.AssetID > 0 && selectedResult.State != loader.FailedScanRowState &&
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
			selectedResult = loader.ApplyPreviewToScanResult(selectedResult, fullPreview)
			rootPreview := loader.ScanResultToPreviewResult(selectedResult)
			explorer.explorerState = ui.NewAssetExplorerState(assetToLoad, rootPreview)
			explorer.renderSelectedAsset(assetToLoad, filePathToLoad, rootPreview)
			explorer.statusLabel.SetText(fmt.Sprintf(
				"Showing asset %d. %s",
				assetToLoad,
				heatmap.FormatSingleRequestSourceBreakdown(trace.ClassifyRequestSource()),
			))
		})
	}()
}

func (explorer *ScanResultsExplorer) cellEmoji(row loader.ScanResult, columnName string) string {
	if explorer.variant == ScanResultsExplorerVariantScan && columnName == "Type" {
		return roblox.GetAssetTypeEmoji(row.AssetTypeID)
	}
	return ""
}

func (explorer *ScanResultsExplorer) columnValue(row loader.ScanResult, columnName string, rowIndex int) string {
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
		return loader.ScanResultTypeLabel(row)
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
	case "GPU Memory":
		return loader.FormatScanResultGPUMemory(row)
	case "B/stud²":
		return loader.FormatLargeTextureScore(row.LargeTextureScore)
	case "Mesh Bytes":
		return format.FormatSizeAuto(row.MeshBytes)
	case "Mesh Triangles", "Triangles":
		if row.MeshNumFaces > 0 {
			return format.FormatIntCommas(int64(row.MeshNumFaces))
		}
		return "-"
	case "Total Triangles":
		if row.MeshNumFaces > 0 && row.UseCount > 0 {
			return format.FormatIntCommas(int64(row.MeshNumFaces) * int64(row.UseCount))
		}
		return "-"
	case "Dimensions":
		if row.Width > 0 && row.Height > 0 {
			return format.FormatDimensions(row.Width, row.Height)
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

const maxSearchSuggestions = 8

func (explorer *ScanResultsExplorer) refreshSearchSuggestions() {
	if explorer == nil || explorer.searchSuggestionsBox == nil {
		return
	}
	suggestions := explorer.computeSearchSuggestions()
	explorer.searchSuggestionsBox.Objects = explorer.searchSuggestionsBox.Objects[:0]
	if len(suggestions) == 0 {
		explorer.searchSuggestionsRow.Hide()
		explorer.searchSuggestionsBox.Refresh()
		return
	}
	explorer.searchSuggestionsBox.Add(widget.NewLabel("Try:"))
	for _, suggestion := range suggestions {
		value := suggestion
		button := widget.NewButton(value, func() {
			explorer.applySuggestion(value)
		})
		button.Importance = widget.LowImportance
		explorer.searchSuggestionsBox.Add(button)
	}
	explorer.searchSuggestionsRow.Show()
	explorer.searchSuggestionsBox.Refresh()
}

func (explorer *ScanResultsExplorer) computeSearchSuggestions() []string {
	currentText := explorer.searchEntry.Text
	activeToken := lastToken(currentText)
	activeLower := strings.ToLower(activeToken)
	var suggestions []string

	colonIndex := strings.Index(activeToken, ":")
	if colonIndex > 0 {
		fieldName := strings.ToLower(strings.TrimPrefix(activeToken[:colonIndex], "-"))
		prefix := activeToken[:colonIndex+1]
		valuePart := strings.ToLower(activeToken[colonIndex+1:])
		if isNameLikeField(fieldName) || fieldName == "" {
			for _, version := range explorer.versionIndex {
				if valuePart == "" || strings.Contains(version, valuePart) {
					suggestions = append(suggestions, prefix+version)
					if len(suggestions) >= maxSearchSuggestions {
						break
					}
				}
			}
		}
		if fieldName != "" && len(suggestions) < maxSearchSuggestions {
			distinctValues := loader.DistinctScanFieldValues(fieldName, explorer.allResults, maxSearchSuggestions*3)
			for _, value := range distinctValues {
				if valuePart == "" || strings.Contains(strings.ToLower(value), valuePart) {
					suggestions = append(suggestions, prefix+quoteIfNeeded(value))
					if len(suggestions) >= maxSearchSuggestions {
						break
					}
				}
			}
		}
		if len(suggestions) == 0 {
			return nil
		}
		return dedupeSuggestions(suggestions, maxSearchSuggestions)
	}

	for _, field := range loader.ScanQueryFieldNames() {
		if activeLower == "" || strings.HasPrefix(field, activeLower) {
			suggestions = append(suggestions, field+":")
			if len(suggestions) >= maxSearchSuggestions {
				break
			}
		}
	}
	if len(suggestions) < maxSearchSuggestions {
		for _, version := range explorer.versionIndex {
			if activeLower == "" || strings.Contains(version, activeLower) {
				suggestions = append(suggestions, version)
				if len(suggestions) >= maxSearchSuggestions {
					break
				}
			}
		}
	}
	return dedupeSuggestions(suggestions, maxSearchSuggestions)
}

func (explorer *ScanResultsExplorer) applySuggestion(suggestion string) {
	currentText := explorer.searchEntry.Text
	lastSpace := strings.LastIndexAny(currentText, " \t")
	var nextText string
	if lastSpace < 0 {
		nextText = suggestion
	} else {
		nextText = currentText[:lastSpace+1] + suggestion
	}
	// Trailing colon means the user is still typing a value; leave the cursor
	// next to it. Completed values get a trailing space for the next token.
	if !strings.HasSuffix(suggestion, ":") {
		nextText += " "
	}
	explorer.searchEntry.SetText(nextText)
	explorer.searchEntry.CursorColumn = len(nextText)
	explorer.searchEntry.Refresh()
}

func lastToken(raw string) string {
	trimmed := strings.TrimLeft(raw, " \t")
	lastSpace := strings.LastIndexAny(trimmed, " \t")
	if lastSpace < 0 {
		return trimmed
	}
	return trimmed[lastSpace+1:]
}

func isNameLikeField(name string) bool {
	switch name {
	case "name", "input", "assetname", "path", "file", "filepath", "ipath", "instancepath", "iname", "instance", "instancename":
		return true
	}
	return false
}

func quoteIfNeeded(value string) string {
	if strings.ContainsAny(value, " \t\"") {
		escaped := strings.ReplaceAll(value, `"`, `\"`)
		return `"` + escaped + `"`
	}
	return value
}

func dedupeSuggestions(values []string, limit int) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
		if len(result) >= limit {
			break
		}
	}
	return result
}
