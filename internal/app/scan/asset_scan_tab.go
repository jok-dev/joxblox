package scan

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"joxblox/internal/app/common"
	"joxblox/internal/app/loader"
	"joxblox/internal/app/ui"
	"joxblox/internal/debug"
	"joxblox/internal/heatmap"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	fyneDialog "fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
	nativeDialog "github.com/sqweek/dialog"
)

const (
	defaultAssetScanLimit   = 100
	scanTableEmojiTextSize  = 18
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
	ExtractHits                      func(sourcePath string, limit int, stopChannel <-chan struct{}) ([]loader.ScanHit, error)
	PathFilteredExtractHits          func(sourcePath string, pathPrefixes []string, limit int, stopChannel <-chan struct{}) ([]loader.ScanHit, error)
	BuildWarning                     func(sourcePath string, pathPrefixes []string, stopChannel <-chan struct{}) (ui.MaterialVariantWarningData, error)
}

type secondaryTappableTable struct {
	*widget.Table
	onSecondaryTap func()
}

func buildScanLoadingStatus(completedCount int, totalCount int, elapsed time.Duration, memoryRequestCount int, diskRequestCount int, networkRequestCount int) string {
	statusText := fmt.Sprintf("Loading results %d/%d...", completedCount, totalCount)
	statusText = fmt.Sprintf(
		"%s fetched from: mem %d, disk %d, net %d",
		statusText,
		memoryRequestCount,
		diskRequestCount,
		networkRequestCount,
	)
	if totalCount <= 0 || completedCount <= 0 || completedCount >= totalCount {
		return statusText
	}
	if completedCount < 5 || elapsed < 2*time.Second {
		return statusText
	}
	remainingCount := totalCount - completedCount
	estimatedRemaining := time.Duration(float64(elapsed) * float64(remainingCount) / float64(completedCount))
	return fmt.Sprintf("%s ETA %s", statusText, ui.FormatDurationCompact(estimatedRemaining))
}

func (table *secondaryTappableTable) TappedSecondary(_ *fyne.PointEvent) {
	if table == nil || table.onSecondaryTap == nil {
		return
	}
	table.onSecondaryTap()
}

func newAssetScanTab(window fyne.Window, options assetScanTabOptions) (fyne.CanvasObject, *ScanTabFileActions) {
	selectedSourcePath := ""
	selectedSecondarySourcePath := ""
	pathWhitelistEnabled := false
	pathWhitelistText := ""
	recentLoadedFiles := common.LoadRecentFilesFromPreferences(options.RecentFilesPreferenceKey)
	maxResultsDefault := options.MaxResultsDefault
	if maxResultsDefault <= 0 {
		maxResultsDefault = defaultAssetScanLimit
	}
	explorer := NewScanResultsExplorer(window, ScanResultsExplorerOptions{
		Variant:            ScanResultsExplorerVariantScan,
		PreviewPlaceholder: "Select a result row to preview",
		IncludeFileRow:     true,
		InitialStatusText:  "Select a source and click Start Scan.",
		SearchPlaceholder:  "Search ID, type, source, hash, or path...",
		ShowDuplicateUI:    true,
		ShowLargeTextureUI: true,
	})

	sourceLabel := widget.NewLabel(options.NoSourceSelectedText)
	secondarySourceText := options.NoSecondarySourceText
	if secondarySourceText == "" {
		secondarySourceText = "No secondary source selected."
	}
	secondarySourceLabel := widget.NewLabel(secondarySourceText)
	warningBanner := ui.NewMaterialVariantWarningBanner(window)
	limitEntry := widget.NewEntry()
	limitEntry.SetText(strconv.Itoa(maxResultsDefault))
	limitEntry.SetPlaceHolder(strconv.Itoa(maxResultsDefault))
	pathWhitelistEntry := widget.NewMultiLineEntry()
	pathWhitelistEntry.SetText("Workspace.*\nMaterialService.*")
	pathWhitelistEntry.Wrapping = fyne.TextWrapOff
	pathWhitelistEntry.Disable()
	pathWhitelistCheck := widget.NewCheck("Path Filter", func(checked bool) {
		pathWhitelistEnabled = checked
		if checked {
			pathWhitelistEntry.Enable()
			return
		}
		pathWhitelistEntry.Disable()
	})
	pathWhitelistEntry.OnChanged = func(text string) {
		pathWhitelistText = text
	}
	pathWhitelistText = pathWhitelistEntry.Text

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
			explorer.SetStatus(options.ReadyStatusText)
			return
		}
		if selectedSecondarySourcePath == "" {
			explorer.SetStatus("Baseline selected. Select target to continue.")
			return
		}
		explorer.SetStatus(options.ReadyStatusText)
	}
	setWarning := func(warningData ui.MaterialVariantWarningData) {
		warningBanner.SetWarning(warningData)
	}

	selectSourceButton := widget.NewButton(options.SelectButtonText, func() {
		options.SelectSource(window, func(selectedPath string) {
			selectedSourcePath = selectedPath
			sourceLabel.SetText(selectedSourcePath)
			setWarning(ui.MaterialVariantWarningData{})
			updateReadyStatus()
		}, func(err error) {
			if err != nil {
				explorer.SetStatus(fmt.Sprintf("Source picker failed: %s", err.Error()))
			}
		})
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
		options.SelectSecondarySource(window, func(selectedPath string) {
			selectedSecondarySourcePath = selectedPath
			secondarySourceLabel.SetText(selectedSecondarySourcePath)
			setWarning(ui.MaterialVariantWarningData{})
			updateReadyStatus()
		}, func(err error) {
			if err != nil {
				explorer.SetStatus(fmt.Sprintf("Secondary source picker failed: %s", err.Error()))
			}
		})
	}

	similarityFileLabel := widget.NewLabel("")
	similarityFileLabel.Hide()
	similarityClearButton := widget.NewButton("Clear", nil)
	similarityClearButton.Hide()
	similarityStatusLabel := widget.NewLabel("")
	similarityStatusLabel.Hide()
	clearSimilaritySearch := func() {
		explorer.ClearSimilarity()
		similarityFileLabel.Hide()
		similarityClearButton.Hide()
		similarityStatusLabel.Hide()
	}
	similarityClearButton.OnTapped = func() {
		clearSimilaritySearch()
	}
	findSimilarButton := widget.NewButton("Find Similar...", func() {
		allResults := explorer.GetResults()
		if len(allResults) == 0 {
			explorer.SetStatus("Run a scan first before searching for similar images.")
			return
		}
		selectedPath, pickerErr := nativeDialog.File().
			Filter("Image files", "png", "jpg", "jpeg").
			Title("Select an image to find similar assets").
			Load()
		if pickerErr != nil {
			if errors.Is(pickerErr, nativeDialog.Cancelled) {
				return
			}
			explorer.SetStatus(fmt.Sprintf("File picker failed: %s", pickerErr.Error()))
			return
		}
		queryBytes, readErr := os.ReadFile(selectedPath)
		if readErr != nil {
			explorer.SetStatus(fmt.Sprintf("Failed to read image: %s", readErr.Error()))
			return
		}
		queryHash, hashErr := loader.ComputeImageDHash(queryBytes)
		if hashErr != nil {
			explorer.SetStatus(fmt.Sprintf("Failed to decode image: %s", hashErr.Error()))
			return
		}
		querySHA := loader.ComputeSHA256Hex(queryBytes)
		explorer.SetStatus("Computing similarity scores...")
		similarityFileLabel.SetText(filepath.Base(selectedPath))
		similarityFileLabel.Show()
		similarityClearButton.Show()
		similarityStatusLabel.Show()
		go func(results []loader.ScanResult) {
			matches := loader.ComputeSimilarityScores(queryHash, querySHA, results)
			matchSet := make(map[int]int, len(matches))
			exactCount := 0
			for _, match := range matches {
				matchSet[match.ResultIndex] = match.Distance
				if match.ExactMatch {
					exactCount++
				}
			}
			fyne.Do(func() {
				explorer.SetSimilarityMatches(matchSet)
				matchText := fmt.Sprintf("%d similar", len(matches))
				if exactCount > 0 {
					matchText += fmt.Sprintf(" (%d exact)", exactCount)
				}
				similarityStatusLabel.SetText(matchText)
				explorer.SetStatus(fmt.Sprintf("Found %d similar images to %s.", len(matches), filepath.Base(selectedPath)))
			})
		}(allResults)
	})

	var scanInProgress bool
	var activeStopSignal *loader.StopSignal
	scanButton := widget.NewButton("Start Scan", nil)
	updateScanControls := func(inProgress bool) {
		scanInProgress = inProgress
		explorer.SetControlsEnabled(!inProgress)
		if inProgress {
			scanButton.SetText("Stop Scan")
			selectSourceButton.Disable()
			if hasSecondarySource {
				selectSecondarySourceButton.Disable()
			}
			limitEntry.Disable()
			findSimilarButton.Disable()
			similarityClearButton.Disable()
			pathWhitelistCheck.Disable()
			pathWhitelistEntry.Disable()
			return
		}
		scanButton.SetText("Start Scan")
		selectSourceButton.Enable()
		if hasSecondarySource {
			selectSecondarySourceButton.Enable()
		}
		limitEntry.Enable()
		findSimilarButton.Enable()
		similarityClearButton.Enable()
		pathWhitelistCheck.Enable()
		if pathWhitelistEnabled {
			pathWhitelistEntry.Enable()
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
	finishScan := func(localStopSignal *loader.StopSignal) {
		updateScanControls(false)
		if activeStopSignal == localStopSignal {
			activeStopSignal = nil
		}
		scanButton.Enable()
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
		common.SaveRecentFilesToPreferences(options.RecentFilesPreferenceKey, recentLoadedFiles)
	}
	applyImportedResults := func(importedResults []loader.ScanResult, statusMessage string) {
		setWarning(ui.MaterialVariantWarningData{})
		explorer.SetResults(importedResults)
		if strings.TrimSpace(statusMessage) != "" {
			explorer.SetStatus(statusMessage)
		}
	}
	importResultsFromPath := func(importPath string) {
		explorer.SetStatus("Importing results...")
		progress := ui.NewProgressDialog(window, "Load JSON", "Reading scan results...")
		readProgress := ui.ProgressRangeReporter(progress, 0, 0.3, "Reading scan results...")
		parseProgress := ui.ProgressRangeReporter(progress, 0.3, 0.9, "Parsing scan results...")
		go func() {
			importBytes, readErr := ReadFileWithProgress(importPath, readProgress)
			if readErr != nil {
				progress.Hide()
				fyne.Do(func() {
					explorer.SetStatus(fmt.Sprintf("Import read failed: %s", readErr.Error()))
					fyneDialog.ShowError(fmt.Errorf("import read failed: %w", readErr), window)
				})
				return
			}
			var importedResults []loader.ScanResult
			importFormat := DetectScanImportFormat(importBytes)
			progress.Update(0.3, "Parsing scan results...")
			switch importFormat {
			case ScanImportFormatWorkspace:
				tablesByContext, parseErr := UnmarshalScanWorkspace(importBytes, parseProgress)
				if parseErr != nil {
					progress.Hide()
					fyne.Do(func() {
						explorer.SetStatus(fmt.Sprintf("Import parse failed: %s", parseErr.Error()))
						fyneDialog.ShowError(fmt.Errorf("import parse failed: %w", parseErr), window)
					})
					return
				}
				importedResults = tablesByContext[options.ScanContextKey]
			case ScanImportFormatTable:
				var parseErr error
				importedResults, parseErr = unmarshalScanTable(importBytes, parseProgress)
				if parseErr != nil {
					progress.Hide()
					fyne.Do(func() {
						explorer.SetStatus(fmt.Sprintf("Import parse failed: %s", parseErr.Error()))
						fyneDialog.ShowError(fmt.Errorf("import parse failed: %w", parseErr), window)
					})
					return
				}
			default:
				progress.Hide()
				fyne.Do(func() {
					explorer.SetStatus("Import parse failed: unsupported scan JSON format.")
					fyneDialog.ShowError(fmt.Errorf("import parse failed: unsupported scan JSON format"), window)
				})
				return
			}
			progress.Update(0.95, "Applying imported results...")
			fyne.Do(func() {
				progress.Hide()
				addRecentLoadedFile(importPath)
				applyImportedResults(importedResults, fmt.Sprintf("Imported %d results.", len(importedResults)))
				if importFormat == ScanImportFormatWorkspace {
					debug.Logf("Scan workspace imported into context %s: %s (rows=%d)", options.ScanContextKey, importPath, len(importedResults))
					return
				}
				debug.Logf("Scan table imported: %s (rows=%d)", importPath, len(importedResults))
			})
		}()
	}
	saveResultsToJSON := func() {
		resultsToExport := explorer.GetResults()
		if len(resultsToExport) == 0 {
			explorer.SetStatus("Nothing to export yet. Run a scan or import a table first.")
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
			explorer.SetStatus(fmt.Sprintf("Export picker failed: %s", pickerErr.Error()))
			return
		}
		if strings.TrimSpace(selectedExportPath) == "" {
			explorer.SetStatus("Export canceled.")
			return
		}
		if !strings.HasSuffix(strings.ToLower(selectedExportPath), ".json") {
			selectedExportPath += ".json"
		}
		explorer.SetStatus("Exporting results...")
		progress := ui.NewProgressDialog(window, "Save JSON", "Serializing scan results...")
		serializeProgress := ui.ProgressRangeReporter(progress, 0.05, 0.8, "Serializing scan results...")
		writeProgress := ui.ProgressRangeReporter(progress, 0.8, 1, "Writing JSON file...")
		go func() {
			exportBytes, marshalErr := marshalScanTable(resultsToExport, serializeProgress)
			if marshalErr != nil {
				progress.Hide()
				fyne.Do(func() {
					explorer.SetStatus(fmt.Sprintf("Export failed: %s", marshalErr.Error()))
				})
				return
			}
			if writeErr := WriteFileWithProgress(selectedExportPath, exportBytes, writeProgress); writeErr != nil {
				progress.Hide()
				fyne.Do(func() {
					explorer.SetStatus(fmt.Sprintf("Export write failed: %s", writeErr.Error()))
				})
				return
			}
			fyne.Do(func() {
				progress.Hide()
				explorer.SetStatus(fmt.Sprintf("Saved %d results.", len(resultsToExport)))
				debug.Logf("Scan table exported: %s (rows=%d)", selectedExportPath, len(resultsToExport))
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
			explorer.SetStatus(fmt.Sprintf("Import picker failed: %s", pickerErr.Error()))
			return
		}
		if strings.TrimSpace(selectedImportPath) == "" {
			explorer.SetStatus("Import canceled.")
			return
		}
		importResultsFromPath(selectedImportPath)
	}
	handleDroppedURIs := func(uris []fyne.URI) {
		if scanInProgress {
			explorer.SetStatus("Cannot import while scan is running.")
			return
		}
		for _, uri := range uris {
			if uri == nil {
				continue
			}
			candidatePath := strings.TrimSpace(uri.Path())
			if candidatePath == "" || !strings.EqualFold(filepath.Ext(candidatePath), ".json") {
				continue
			}
			importResultsFromPath(candidatePath)
			return
		}
		explorer.SetStatus("Drop a .json results file to import.")
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
			debug.Logf("Scan stop requested")
			requestStopScan()
			explorer.SetStatus("Stopping scan...")
			scanButton.Disable()
			return
		}
		if selectedSourcePath == "" {
			explorer.SetStatus(options.MissingSourceStatusText)
			return
		}
		if hasSecondarySource && selectedSecondarySourcePath == "" {
			if options.MissingSecondarySourceStatusText != "" {
				explorer.SetStatus(options.MissingSecondarySourceStatusText)
			} else {
				explorer.SetStatus("Please select a secondary source first.")
			}
			return
		}
		limitValue, err := strconv.Atoi(strings.TrimSpace(limitEntry.Text))
		if err != nil || limitValue <= 0 {
			explorer.SetStatus("Max results must be a positive number.")
			return
		}
		explorer.SetResults([]loader.ScanResult{})
		clearSimilaritySearch()
		setWarning(ui.MaterialVariantWarningData{})
		debug.Logf("Scan started for source: %s (limit=%d)", combinedSourcePath(), limitValue)
		explorer.SetStatus(options.ScanningStatusText)
		localStopSignal := loader.NewStopSignal()
		activeStopSignal = localStopSignal
		updateScanControls(true)
		go func() {
			var hits []loader.ScanHit
			var scanErr error
			warningData := ui.MaterialVariantWarningData{}
			useFilteredExtraction := pathWhitelistEnabled &&
				strings.TrimSpace(pathWhitelistText) != "" &&
				options.PathFilteredExtractHits != nil
			var activePrefixes []string
			if useFilteredExtraction {
				activePrefixes = common.WhitelistPatternsToPathPrefixes(pathWhitelistText)
				hits, scanErr = options.PathFilteredExtractHits(combinedSourcePath(), activePrefixes, limitValue, localStopSignal.Channel)
			} else {
				hits, scanErr = options.ExtractHits(combinedSourcePath(), limitValue, localStopSignal.Channel)
			}
			if options.BuildWarning != nil {
				nextWarning, warningErr := options.BuildWarning(combinedSourcePath(), activePrefixes, localStopSignal.Channel)
				if errors.Is(warningErr, loader.ErrScanStopped) {
					scanErr = loader.ErrScanStopped
				} else if warningErr != nil {
					debug.Logf("Scan warning build failed: %s", warningErr.Error())
				} else {
					warningData = nextWarning
				}
			}
			if scanErr != nil {
				fyne.Do(func() {
					finishScan(localStopSignal)
					if errors.Is(scanErr, loader.ErrScanStopped) {
						explorer.SetStatus(fmt.Sprintf("Stopped. %d results loaded.", len(explorer.GetResults())))
						return
					}
					explorer.SetStatus(fmt.Sprintf("Scan failed: %s", scanErr.Error()))
					fyneDialog.ShowError(scanErr, window)
				})
				return
			}
			if !useFilteredExtraction && pathWhitelistEnabled && strings.TrimSpace(pathWhitelistText) != "" {
				filteredHits := make([]loader.ScanHit, 0, len(hits))
				for _, hit := range hits {
					if common.ScanHitMatchesPathWhitelist(hit, pathWhitelistText) {
						filteredHits = append(filteredHits, hit)
					}
				}
				debug.Logf("Path filter: %d -> %d hits", len(hits), len(filteredHits))
				hits = filteredHits
			}
			if len(hits) == 0 {
				fyne.Do(func() {
					finishScan(localStopSignal)
					setWarning(warningData)
					explorer.SetStatus(options.NoResultsStatusText)
				})
				return
			}
			loadFailureCount := 0
			var firstLoadErr error
			type scanLoadOutcome struct {
				row           loader.ScanResult
				loadErr       error
				requestSource heatmap.RequestSource
			}
			workerCount := determineScanLoadWorkerCount(len(hits))
			hitJobs := make(chan loader.ScanHit)
			loadOutcomes := make(chan scanLoadOutcome, workerCount*2)
			var workerGroup sync.WaitGroup
			for workerIndex := 0; workerIndex < workerCount; workerIndex++ {
				workerGroup.Add(1)
				go func() {
					defer workerGroup.Done()
					for hit := range hitJobs {
						scanRow, loadErr, requestSource := loader.LoadScanResultWithRequestSource(hit)
						if loadErr != nil {
							debug.Logf("Scan result load failed for asset %d (file=%s, useCount=%d): %s", hit.AssetID, hit.FilePath, hit.UseCount, loadErr.Error())
							scanRow = loader.BuildFailedScanResultFromHit(hit, loadErr)
						}
						loadOutcomes <- scanLoadOutcome{row: scanRow, loadErr: loadErr, requestSource: requestSource}
					}
				}()
			}
			go func() {
				defer close(hitJobs)
				for _, hit := range hits {
					select {
					case <-localStopSignal.Channel:
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
			pendingRows := make([]loader.ScanResult, 0, scanLoadUIBatchSize)
			lastUIFlushAt := time.Now()
			lastScheduledResultsRefreshAt := time.Time{}
			lastScheduledFilterRefreshAt := time.Time{}
			loadStartedAt := time.Now()
			memoryRequestCount := 0
			diskRequestCount := 0
			networkRequestCount := 0
			flushPendingRows := func(force bool) {
				if len(pendingRows) == 0 {
					return
				}
				if !force && len(pendingRows) < scanLoadUIBatchSize && time.Since(lastUIFlushAt) < scanLoadUIFlushDelay {
					return
				}
				rowsToPublish := append([]loader.ScanResult(nil), pendingRows...)
				publishedCount := completedCount
				pendingRows = pendingRows[:0]
				lastUIFlushAt = time.Now()
				refreshResults := force || time.Since(lastScheduledResultsRefreshAt) >= scanLoadUIRefreshDelay
				refreshFilters := force || time.Since(lastScheduledFilterRefreshAt) >= scanLoadUIRefreshDelay
				if refreshResults {
					lastScheduledResultsRefreshAt = time.Now()
				}
				if refreshFilters {
					lastScheduledFilterRefreshAt = time.Now()
				}
				fyne.Do(func() {
					explorer.AppendResults(rowsToPublish, refreshResults, refreshFilters)
					explorer.SetStatus(buildScanLoadingStatus(
						publishedCount,
						len(hits),
						time.Since(loadStartedAt),
						memoryRequestCount,
						diskRequestCount,
						networkRequestCount,
					))
				})
			}
			for loadOutcome := range loadOutcomes {
				completedCount++
				switch loadOutcome.requestSource {
				case heatmap.SourceNetwork:
					networkRequestCount++
				case heatmap.SourceDisk:
					diskRequestCount++
				default:
					memoryRequestCount++
				}
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
			case <-localStopSignal.Channel:
				fyne.Do(func() {
					finishScan(localStopSignal)
					explorer.SetStatus(fmt.Sprintf("Stopped. %d results loaded.", len(explorer.GetResults())))
				})
				return
			default:
			}
			fyne.Do(func() {
				finishScan(localStopSignal)
				setWarning(warningData)
				resultCount := len(explorer.GetResults())
				if resultCount == 0 && firstLoadErr != nil {
					explorer.SetStatus(fmt.Sprintf("Scan extracted %d IDs but could not load any assets. First error: %s", len(hits), firstLoadErr.Error()))
					fyneDialog.ShowError(firstLoadErr, window)
					return
				}
				if loadFailureCount > 0 {
					explorer.SetStatus(fmt.Sprintf(
						"Scan complete. Showing %d assets (%d failed to load). Fetched from: mem %d, disk %d, net %d.",
						resultCount,
						loadFailureCount,
						memoryRequestCount,
						diskRequestCount,
						networkRequestCount,
					))
					return
				}
				explorer.SetStatus(fmt.Sprintf(
					"Done. %d results loaded. Fetched from: mem %d, disk %d, net %d.",
					resultCount,
					memoryRequestCount,
					diskRequestCount,
					networkRequestCount,
				))
			})
		}()
	}

	controlButtons := []fyne.CanvasObject{selectSourceButton}
	if hasSecondarySource {
		controlButtons = append(controlButtons, selectSecondarySourceButton)
	}
	controlButtons = append(
		controlButtons,
		widget.NewLabel("Max results:"),
		container.NewGridWrap(fyne.NewSize(80, 36), limitEntry),
		scanButton,
		layout.NewSpacer(),
		findSimilarButton,
		similarityFileLabel,
		similarityClearButton,
		similarityStatusLabel,
	)
	sourceLabels := []fyne.CanvasObject{sourceLabel}
	if hasSecondarySource {
		sourceLabels = append(sourceLabels, secondarySourceLabel)
	}
	pathWhitelistRow := container.NewBorder(nil, nil, pathWhitelistCheck, nil, pathWhitelistEntry)
	topControls := container.NewVBox(
		container.NewHBox(controlButtons...),
		pathWhitelistRow,
		container.NewVBox(sourceLabels...),
		warningBanner.BannerRoot(),
	)
	content := container.NewBorder(topControls, nil, nil, nil, explorer.Content())
	loadSourceAndScan := func(path string) {
		trimmedPath := strings.TrimSpace(path)
		if trimmedPath == "" {
			return
		}
		if scanInProgress {
			requestStopScan()
		}
		selectedSourcePath = trimmedPath
		sourceLabel.SetText(selectedSourcePath)
		setWarning(ui.MaterialVariantWarningData{})
		updateReadyStatus()
		if scanButton.OnTapped != nil {
			scanButton.OnTapped()
		}
	}
	fileActions := &ScanTabFileActions{
		ContextKey: options.ScanContextKey,
		SaveJSON:   saveResultsToJSON,
		LoadJSON:   loadResultsFromPicker,
		HandleDrop: handleDroppedURIs,
		LoadSource: loadSourceAndScan,
		RecentFiles: func() []string {
			paths := make([]string, len(recentLoadedFiles))
			copy(paths, recentLoadedFiles)
			return paths
		},
		LoadRecent: func(path string) {
			if strings.TrimSpace(path) == "" {
				explorer.SetStatus("Select a recent file first.")
				return
			}
			importResultsFromPath(path)
		},
		GetResults: func() []loader.ScanResult {
			return explorer.GetResults()
		},
		SetResults: func(rows []loader.ScanResult) {
			nextRows := make([]loader.ScanResult, len(rows))
			copy(nextRows, rows)
			applyImportedResults(nextRows, fmt.Sprintf("Loaded %d results.", len(nextRows)))
		},
		AddRecentFile: addRecentLoadedFile,
		SetPathFilter: func(enabled bool, text string) {
			pathWhitelistEnabled = enabled
			pathWhitelistCheck.SetChecked(enabled)
			if enabled {
				pathWhitelistText = text
				pathWhitelistEntry.SetText(text)
				pathWhitelistEntry.Enable()
			} else {
				pathWhitelistEntry.Disable()
			}
		},
		SetLargeTextureThreshold: func(threshold float64) {
			explorer.SetLargeTextureThreshold(threshold)
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
