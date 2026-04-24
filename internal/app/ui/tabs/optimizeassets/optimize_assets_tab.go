package optimizeassets

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
	nativeDialog "github.com/sqweek/dialog"
	xdraw "golang.org/x/image/draw"

	"joxblox/internal/app/common"
	"joxblox/internal/app/loader"
	"joxblox/internal/app/ui"
	"joxblox/internal/debug"
	"joxblox/internal/extractor"
	"joxblox/internal/format"
	"joxblox/internal/roblox"
	"joxblox/internal/roblox/opencloud"
)

const optimizeMaxRetries = 2

type optimizeScaleOption struct {
	Label string
	Scale float64
}

var optimizeScaleOptions = []optimizeScaleOption{
	{Label: "Three Quarters (75%)", Scale: 0.75},
	{Label: "Half (50%)", Scale: 0.50},
	{Label: "Quarter (25%)", Scale: 0.25},
}

type optimizeAssetResult struct {
	AssetID    int64
	NewAssetID int64
	Status     string // "ok", "skip", "fail"
	Message    string
	OrigBytes  int
	NewBytes   int
	OrigWidth  int
	OrigHeight int
	NewWidth   int
	NewHeight  int
}

const verifyMaxRetries = 3

func verifyAssetWithRetry(id int64, stopChannel <-chan struct{}) (*loader.AssetDeliveryInfo, error) {
	backoff := 2 * time.Second
	for attempt := 0; attempt <= verifyMaxRetries; attempt++ {
		select {
		case <-stopChannel:
			return nil, loader.ErrScanStopped
		default:
		}
		deliveryInfo, err := loader.FetchAssetDeliveryInfo(id)
		if err == nil {
			return deliveryInfo, nil
		}
		if attempt >= verifyMaxRetries {
			return deliveryInfo, err
		}
		debug.Logf("Verify asset %d attempt %d failed: %v (retrying in %v)", id, attempt+1, err, backoff)
		select {
		case <-stopChannel:
			return nil, loader.ErrScanStopped
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, 15*time.Second)
	}
	return nil, fmt.Errorf("unreachable")
}

// verifyImageAssets checks each candidate asset via the Asset Delivery API.
// Returns a map of confirmed image asset IDs to their cached CDN download URLs.
func verifyImageAssets(
	candidateIDs []int64,
	onProgress func(checked int, confirmed int, total int),
	stopChannel <-chan struct{},
) map[int64]string {
	total := len(candidateIDs)
	if total == 0 {
		return make(map[int64]string)
	}

	var mu sync.Mutex
	confirmed := make(map[int64]string)
	var checkedCount atomic.Int64
	var confirmedCount atomic.Int64
	var failedCount atomic.Int64

	jobs := make(chan int64, total)
	for _, id := range candidateIDs {
		jobs <- id
	}
	close(jobs)

	workerCount := min(10, total)
	var wg sync.WaitGroup
	for w := 0; w < workerCount; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for id := range jobs {
				select {
				case <-stopChannel:
					return
				default:
				}

				deliveryInfo, err := verifyAssetWithRetry(id, stopChannel)
				if errors.Is(err, loader.ErrScanStopped) {
					return
				}
				if err != nil {
					failedCount.Add(1)
					debug.Logf("Verify asset %d failed after retries: %v", id, err)
				} else if deliveryInfo != nil && deliveryInfo.AssetTypeID == roblox.AssetTypeImage {
					mu.Lock()
					confirmed[id] = deliveryInfo.Location
					mu.Unlock()
					confirmedCount.Add(1)
				}

				checked := checkedCount.Add(1)
				if onProgress != nil {
					onProgress(int(checked), int(confirmedCount.Load()), total)
				}
			}
		}()
	}
	wg.Wait()
	failed := failedCount.Load()
	if failed > 0 {
		debug.Logf("Verification complete: %d confirmed images, %d failed to verify out of %d candidates", len(confirmed), failed, total)
	}
	return confirmed
}

func NewOptimizeAssetsTab(window fyne.Window) fyne.CanvasObject {
	selectedFilePath := ""
	var scannedResults []extractor.Result
	var filteredResults []extractor.Result
	var verifiedImageURLs map[int64]string
	ignoredThumbnailAssets := 0
	inProgress := false
	var activeStopSignal *loader.StopSignal

	filePathLabel := widget.NewLabel("No file selected")
	filePathLabel.Wrapping = fyne.TextTruncate

	whitelistCheck := widget.NewCheck("Enable Path Whitelist", nil)
	whitelistEntry := widget.NewMultiLineEntry()
	whitelistEntry.SetText("Workspace.*\nMaterialService.*")
	whitelistEntry.SetMinRowsVisible(3)
	whitelistEntry.Wrapping = fyne.TextWrapOff
	whitelistEntry.Disable()

	blacklistCheck := widget.NewCheck("Enable Asset ID Blacklist", nil)
	blacklistEntry := widget.NewMultiLineEntry()
	blacklistEntry.SetPlaceHolder("One asset ID per line")
	blacklistEntry.SetMinRowsVisible(3)
	blacklistEntry.Wrapping = fyne.TextWrapOff
	blacklistEntry.Disable()

	sizeFilterCheck := widget.NewCheck("Ignore files under", nil)
	sizeFilterEntry := widget.NewEntry()
	sizeFilterEntry.SetText("10")
	sizeFilterEntry.Disable()
	sizeUnitSelect := widget.NewSelect([]string{"bytes", "KB", "MB"}, nil)
	sizeUnitSelect.SetSelected("KB")
	sizeUnitSelect.Disable()

	scaleLabels := make([]string, len(optimizeScaleOptions))
	for i, opt := range optimizeScaleOptions {
		scaleLabels[i] = opt.Label
	}
	scaleSelect := widget.NewSelect(scaleLabels, nil)
	scaleSelect.SetSelected(optimizeScaleOptions[0].Label)

	interpolationSelect := widget.NewSelect(ui.SampleModeOptions, nil)
	interpolationSelect.SetSelected(ui.DefaultSampleMode)

	workersEntry := widget.NewEntry()
	workersEntry.SetText("8")

	apiKeyEntry := widget.NewPasswordEntry()
	apiKeyEntry.SetPlaceHolder("Open Cloud API key")
	rememberKeyCheck := widget.NewCheck("Save to keychain", nil)
	if storedKey, loadErr := roblox.LoadOpenCloudAPIKeyFromKeyring(); loadErr == nil && storedKey != "" {
		apiKeyEntry.SetText(storedKey)
		rememberKeyCheck.SetChecked(true)
	}
	creatorTypeSelect := widget.NewSelect([]string{ui.UploadCreatorModeUser, ui.UploadCreatorModeGroup}, nil)
	creatorTypeSelect.SetSelected(ui.UploadCreatorModeUser)
	creatorIDEntry := widget.NewEntry()
	creatorIDEntry.SetPlaceHolder("Creator user/group ID")

	infoLabel := widget.NewLabel("Select an .rbxl file and scan to begin.")
	infoLabel.Wrapping = fyne.TextWrapWord

	progressBar := widget.NewProgressBar()
	progressBar.Hide()
	statusLabel := widget.NewLabel("")
	statusLabel.Wrapping = fyne.TextWrapWord
	etaLabel := widget.NewLabel("")

	rateLimitWarning := canvas.NewText("Rate limited by Roblox — progress paused, waiting for cooldown...", color.RGBA{R: 220, G: 40, B: 40, A: 255})
	rateLimitWarning.TextSize = 12
	rateLimitWarning.TextStyle = fyne.TextStyle{Bold: true}
	rateLimitWarning.Hide()

	resultsEntry := widget.NewMultiLineEntry()
	resultsEntry.Wrapping = fyne.TextWrapWord
	resultsEntry.SetMinRowsVisible(6)
	resultsEntry.Disable()

	scanButton := widget.NewButton("Scan", nil)
	scanButton.Disable()
	startButton := widget.NewButton("Start Optimization", nil)
	startButton.Disable()
	stopButton := widget.NewButton("Stop", nil)
	stopButton.Disable()

	appendResult := func(text string) {
		fyne.Do(func() {
			current := resultsEntry.Text
			if current != "" {
				current += "\n"
			}
			resultsEntry.Text = current + text
			resultsEntry.Refresh()
		})
	}

	parseBlacklistedIDs := func() map[int64]bool {
		blacklisted := make(map[int64]bool)
		if !blacklistCheck.Checked {
			return blacklisted
		}
		for _, line := range strings.Split(blacklistEntry.Text, "\n") {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" {
				continue
			}
			if id, err := strconv.ParseInt(trimmed, 10, 64); err == nil && id > 0 {
				blacklisted[id] = true
			}
		}
		return blacklisted
	}

	applyFilters := func() {
		if len(scannedResults) == 0 {
			filteredResults = nil
			return
		}
		blacklisted := parseBlacklistedIDs()
		seen := map[int64]bool{}
		filtered := make([]extractor.Result, 0, len(scannedResults))
		for _, result := range scannedResults {
			if result.ID <= 0 {
				continue
			}
			if seen[result.ID] {
				continue
			}
			if blacklisted[result.ID] {
				continue
			}
			if verifiedImageURLs != nil {
				if _, isImage := verifiedImageURLs[result.ID]; !isImage {
					continue
				}
			}
			if whitelistCheck.Checked && strings.TrimSpace(whitelistEntry.Text) != "" {
				if !common.MatchesAnyPathWhitelist(result.InstancePath, whitelistEntry.Text) {
					continue
				}
			}
			seen[result.ID] = true
			filtered = append(filtered, result)
		}
		filteredResults = filtered
	}

	updateInfoLabel := func() {
		if scannedResults == nil {
			infoLabel.SetText("Select an .rbxl file and scan to begin.")
			startButton.Disable()
			return
		}
		if verifiedImageURLs == nil {
			infoLabel.SetText("Filters changed. Click Scan to re-verify assets.")
			startButton.Disable()
			return
		}
		applyFilters()
		verified := len(verifiedImageURLs)
		afterFilter := len(filteredResults)
		if verified == 0 {
			if ignoredThumbnailAssets > 0 {
				infoLabel.SetText(fmt.Sprintf("No optimizable image assets found. Ignored %d rbxthumb assets.", ignoredThumbnailAssets))
			} else {
				infoLabel.SetText("No image assets found in this file.")
			}
			startButton.Disable()
			return
		}
		sizeNote := ""
		if sizeFilterCheck.Checked {
			sizeNote = " Size filter will be applied during processing."
		}
		ignoredNote := ""
		if ignoredThumbnailAssets > 0 {
			ignoredNote = fmt.Sprintf(", %d ignored rbxthumb assets", ignoredThumbnailAssets)
		}
		infoLabel.SetText(fmt.Sprintf(
			"%d verified image assets%s. %d will be processed.%s",
			verified, ignoredNote, afterFilter, sizeNote,
		))
		if afterFilter > 0 && !inProgress {
			startButton.Enable()
		} else {
			startButton.Disable()
		}
	}

	invalidateVerification := func() {
		scannedResults = nil
		verifiedImageURLs = nil
		filteredResults = nil
		ignoredThumbnailAssets = 0
		updateInfoLabel()
	}

	whitelistCheck.OnChanged = func(checked bool) {
		if checked {
			whitelistEntry.Enable()
		} else {
			whitelistEntry.Disable()
		}
		invalidateVerification()
	}
	whitelistEntry.OnChanged = func(_ string) {
		invalidateVerification()
	}
	blacklistCheck.OnChanged = func(checked bool) {
		if checked {
			blacklistEntry.Enable()
		} else {
			blacklistEntry.Disable()
		}
		updateInfoLabel()
	}
	blacklistEntry.OnChanged = func(_ string) {
		updateInfoLabel()
	}

	sizeFilterCheck.OnChanged = func(checked bool) {
		if checked {
			sizeFilterEntry.Enable()
			sizeUnitSelect.Enable()
		} else {
			sizeFilterEntry.Disable()
			sizeUnitSelect.Disable()
		}
	}

	browseButton := widget.NewButton("Browse...", func() {
		if inProgress {
			return
		}
		path, pickerErr := nativeDialog.File().
			Filter("Roblox place files", "rbxl").
			Title("Select .rbxl file to optimize").
			Load()
		if pickerErr != nil {
			if !errors.Is(pickerErr, nativeDialog.ErrCancelled) {
				dialog.ShowError(pickerErr, window)
			}
			return
		}
		selectedFilePath = path
		filePathLabel.SetText(filepath.Base(path))
		scanButton.Enable()
		scannedResults = nil
		filteredResults = nil
		verifiedImageURLs = nil
		infoLabel.SetText("File selected. Click Scan to find image assets.")
		startButton.Disable()
	})

	scanProgressBar := widget.NewProgressBarInfinite()
	scanProgressBar.Hide()

	scanButton.OnTapped = func() {
		if inProgress || selectedFilePath == "" {
			return
		}

		useWhitelist := whitelistCheck.Checked && strings.TrimSpace(whitelistEntry.Text) != ""
		whitelistText := whitelistEntry.Text

		scanButton.Disable()
		browseButton.Disable()
		startButton.Disable()
		infoLabel.SetText("Validating auth cookie...")
		resultsEntry.SetText("")
		scanProgressBar.Show()
		scanProgressBar.Start()
		go func() {
			if authErr := roblox.ValidateCurrentAuthCookie(); authErr != nil {
				fyne.Do(func() {
					scanProgressBar.Stop()
					scanProgressBar.Hide()
					scanButton.Enable()
					browseButton.Enable()
					infoLabel.SetText("Auth cookie expired or invalid")
					dialog.ShowError(authErr, window)
				})
				return
			}

			results := scannedResults
			if results == nil {
				fyne.Do(func() {
					infoLabel.SetText("Extracting asset IDs...")
				})
				neverStop := make(chan struct{})
				var scanErr error
				if useWhitelist {
					prefixes := common.WhitelistPatternsToPathPrefixes(whitelistText)
					results, scanErr = extractor.ExtractFilteredRefs(selectedFilePath, prefixes, neverStop)
				} else {
					var extractResult extractor.AssetIDsResult
					extractResult, scanErr = extractor.ExtractAssetIDsWithCounts(selectedFilePath, 0, 0, neverStop)
					if scanErr == nil {
						var allReferences []extractor.Result
						if jsonErr := json.Unmarshal([]byte(extractResult.CommandOutput), &allReferences); jsonErr != nil {
							scanErr = jsonErr
						} else {
							results = allReferences
						}
					}
				}
				if scanErr != nil {
					fyne.Do(func() {
						scanProgressBar.Stop()
						scanProgressBar.Hide()
						scanButton.Enable()
						browseButton.Enable()
						infoLabel.SetText(fmt.Sprintf("Scan failed: %s", scanErr.Error()))
						scannedResults = nil
						filteredResults = nil
						verifiedImageURLs = nil
						ignoredThumbnailAssets = 0
					})
					return
				}
			}

			candidateIDs, ignoredCount := collectOptimizableAssetIDs(results, useWhitelist, whitelistText)

			if len(candidateIDs) == 0 {
				fyne.Do(func() {
					scanProgressBar.Stop()
					scanProgressBar.Hide()
					scanButton.Enable()
					browseButton.Enable()
					scannedResults = results
					verifiedImageURLs = make(map[int64]string)
					ignoredThumbnailAssets = ignoredCount
					updateInfoLabel()
				})
				return
			}

			fyne.Do(func() {
				scanProgressBar.Stop()
				scanProgressBar.Hide()
				progressBar.SetValue(0)
				progressBar.Show()
				infoLabel.SetText(fmt.Sprintf("Verifying image assets... (0/%d checked)", len(candidateIDs)))
			})

			neverStop := make(chan struct{})
			confirmed := verifyImageAssets(candidateIDs, func(checked, confirmedN, total int) {
				fyne.Do(func() {
					progressBar.SetValue(float64(checked) / float64(total))
					ignoredNote := ""
					if ignoredCount > 0 {
						ignoredNote = fmt.Sprintf(", %d ignored rbxthumb assets", ignoredCount)
					}
					infoLabel.SetText(fmt.Sprintf("Verifying image assets... (%d/%d checked, %d confirmed%s)", checked, total, confirmedN, ignoredNote))
				})
			}, neverStop)

			fyne.Do(func() {
				progressBar.Hide()
				scanButton.Enable()
				browseButton.Enable()
				scannedResults = results
				verifiedImageURLs = confirmed
				ignoredThumbnailAssets = ignoredCount
				updateInfoLabel()
			})
		}()
	}

	getMinSizeBytes := func() int {
		if !sizeFilterCheck.Checked {
			return 0
		}
		val, parseErr := strconv.Atoi(strings.TrimSpace(sizeFilterEntry.Text))
		if parseErr != nil || val <= 0 {
			return 0
		}
		switch sizeUnitSelect.Selected {
		case "KB":
			return val * 1024
		case "MB":
			return val * 1024 * 1024
		default:
			return val
		}
	}

	getSelectedScale := func() float64 {
		for _, opt := range optimizeScaleOptions {
			if opt.Label == scaleSelect.Selected {
				return opt.Scale
			}
		}
		return 0.75
	}

	getWorkerCount := func() int {
		val, parseErr := strconv.Atoi(strings.TrimSpace(workersEntry.Text))
		if parseErr != nil || val < 1 {
			return 1
		}
		return val
	}

	setControlsEnabled := func(enabled bool) {
		if enabled {
			browseButton.Enable()
			scanButton.Enable()
			startButton.Enable()
			stopButton.Disable()
			whitelistCheck.Enable()
			if whitelistCheck.Checked {
				whitelistEntry.Enable()
			}
			blacklistCheck.Enable()
			if blacklistCheck.Checked {
				blacklistEntry.Enable()
			}
			sizeFilterCheck.Enable()
			if sizeFilterCheck.Checked {
				sizeFilterEntry.Enable()
				sizeUnitSelect.Enable()
			}
			scaleSelect.Enable()
			workersEntry.Enable()
			apiKeyEntry.Enable()
			rememberKeyCheck.Enable()
			creatorTypeSelect.Enable()
			creatorIDEntry.Enable()
		} else {
			browseButton.Disable()
			scanButton.Disable()
			startButton.Disable()
			stopButton.Enable()
			whitelistCheck.Disable()
			whitelistEntry.Disable()
			blacklistCheck.Disable()
			blacklistEntry.Disable()
			sizeFilterCheck.Disable()
			sizeFilterEntry.Disable()
			sizeUnitSelect.Disable()
			scaleSelect.Disable()
			workersEntry.Disable()
			apiKeyEntry.Disable()
			rememberKeyCheck.Disable()
			creatorTypeSelect.Disable()
			creatorIDEntry.Disable()
		}
	}

	stopButton.OnTapped = func() {
		if activeStopSignal != nil {
			activeStopSignal.Stop()
		}
		stopButton.Disable()
	}

	startButton.OnTapped = func() {
		if inProgress || len(filteredResults) == 0 || selectedFilePath == "" {
			return
		}

		apiKey := strings.TrimSpace(apiKeyEntry.Text)
		if apiKey == "" {
			dialog.ShowError(fmt.Errorf("Open Cloud API key is required"), window)
			return
		}
		creatorID, parseErr := strconv.ParseInt(strings.TrimSpace(creatorIDEntry.Text), 10, 64)
		if parseErr != nil || creatorID <= 0 {
			dialog.ShowError(fmt.Errorf("creator ID must be a positive integer"), window)
			return
		}
		creator := opencloud.Creator{
			IsGroup: creatorTypeSelect.Selected == ui.UploadCreatorModeGroup,
			ID:      creatorID,
		}
		scale := getSelectedScale()
		scaler := ui.SampleModeInterpolator(interpolationSelect.Selected)
		minSizeBytes := getMinSizeBytes()
		workerCount := getWorkerCount()
		assetsToProcess := make([]extractor.Result, len(filteredResults))
		copy(assetsToProcess, filteredResults)

		assetPaths := make(map[int64][]string)
		for _, r := range scannedResults {
			if r.ID > 0 && r.InstancePath != "" {
				assetPaths[r.ID] = append(assetPaths[r.ID], r.InstancePath)
			}
		}

		startButton.Disable()
		statusLabel.SetText("Validating auth cookie...")

		go func() {
			authErr := roblox.ValidateCurrentAuthCookie()
			fyne.Do(func() {
				if authErr != nil {
					statusLabel.SetText("Auth cookie expired or invalid")
					startButton.Enable()
					dialog.ShowError(authErr, window)
					return
				}
				statusLabel.SetText("")

				creatorLabel := fmt.Sprintf("User %d", creatorID)
				if creator.IsGroup {
					creatorLabel = fmt.Sprintf("Group %d", creatorID)
				}
				confirmMessage := fmt.Sprintf(
					"This will download, resize to %s, and re-upload %d image assets under %s using %d workers.\n\nContinue?",
					scaleSelect.Selected, len(assetsToProcess), creatorLabel, workerCount,
				)

				dialog.ShowConfirm("Start Optimization", confirmMessage, func(confirmed bool) {
					if !confirmed {
						startButton.Enable()
						return
					}

					if rememberKeyCheck.Checked {
						_ = roblox.SaveOpenCloudAPIKeyToKeyring(apiKey)
					} else {
						_ = roblox.DeleteOpenCloudAPIKeyFromKeyring()
					}

					inProgress = true
					localStopSignal := loader.NewStopSignal()
					activeStopSignal = localStopSignal
					setControlsEnabled(false)
					progressBar.SetValue(0)
					progressBar.Show()
					resultsEntry.SetText("")
					statusLabel.SetText("Starting optimization...")
					etaLabel.SetText("")

					go runOptimization(
						window, localStopSignal, assetsToProcess, assetPaths,
						verifiedImageURLs,
						apiKey, creator, scale, scaler, minSizeBytes, workerCount,
						selectedFilePath,
						progressBar, statusLabel, etaLabel, rateLimitWarning, appendResult,
						func() {
							inProgress = false
							activeStopSignal = nil
							setControlsEnabled(true)
							if len(filteredResults) == 0 {
								startButton.Disable()
							}
						},
					)
				}, window)
			})
		}()
	}

	fileRow := container.NewBorder(nil, nil,
		widget.NewLabel("RBXL File:"), container.NewHBox(browseButton, scanButton),
		filePathLabel,
	)
	whitelistRow := container.NewBorder(nil, nil,
		whitelistCheck, nil,
		whitelistEntry,
	)
	sizeFilterValueRow := container.NewBorder(nil, nil,
		nil, sizeUnitSelect,
		container.NewGridWrap(fyne.NewSize(80, sizeFilterEntry.MinSize().Height), sizeFilterEntry),
	)
	blacklistRow := container.NewBorder(nil, nil,
		blacklistCheck, nil,
		blacklistEntry,
	)
	sizeFilterRow := container.NewHBox(sizeFilterCheck, sizeFilterValueRow)
	filtersCard := widget.NewCard("Filters", "", container.NewVBox(whitelistRow, blacklistRow, sizeFilterRow))

	workersRow := container.NewBorder(nil, nil,
		widget.NewLabel("Workers:"), nil,
		container.NewGridWrap(fyne.NewSize(80, workersEntry.MinSize().Height), workersEntry),
	)

	uploadSettingsCard := widget.NewCard("Upload Settings", "", container.NewVBox(
		container.NewBorder(nil, nil, widget.NewLabel("API Key:"), rememberKeyCheck, apiKeyEntry),
		container.NewGridWithColumns(2,
			container.NewBorder(nil, nil, widget.NewLabel("Creator Type:"), nil, creatorTypeSelect),
			container.NewBorder(nil, nil, widget.NewLabel("Creator ID:"), nil, creatorIDEntry),
		),
		container.NewGridWithColumns(3,
			container.NewBorder(nil, nil, widget.NewLabel("Resize Scale:"), nil, scaleSelect),
			container.NewBorder(nil, nil, widget.NewLabel("Interpolation:"), nil, interpolationSelect),
			workersRow,
		),
	))

	uploadWarning := canvas.NewText("⚠ Please do not use your main account for this as you could be banned, please use an alt that you don't care about!", color.RGBA{R: 220, G: 40, B: 40, A: 255})
	uploadWarning.TextSize = 12
	uploadWarning.TextStyle = fyne.TextStyle{Bold: true}

	controlsRow := container.NewHBox(startButton, stopButton)
	progressSection := container.NewVBox(progressBar, statusLabel, etaLabel, rateLimitWarning)

	topSection := container.NewVBox(
		fileRow,
		scanProgressBar,
		filtersCard,
		uploadSettingsCard,
		uploadWarning,
		infoLabel,
		controlsRow,
		progressSection,
		widget.NewSeparator(),
		widget.NewLabel("Results:"),
	)

	// Wrap in VScroll so the tab's ~500px form-controls stack doesn't pin
	// the main window's minimum height (AppTabs inherits the max MinSize
	// across tabs). When the window is tall the scroll is invisible; when
	// it's short, a vertical scrollbar appears.
	return container.NewVScroll(container.NewBorder(topSection, nil, nil, nil, resultsEntry))
}

func runOptimization(
	window fyne.Window,
	localStopSignal *loader.StopSignal,
	assetsToProcess []extractor.Result,
	assetPaths map[int64][]string,
	cachedURLs map[int64]string,
	apiKey string,
	creator opencloud.Creator,
	scale float64,
	scaler xdraw.Interpolator,
	minSizeBytes int,
	workerCount int,
	selectedFilePath string,
	progressBar *widget.ProgressBar,
	statusLabel *widget.Label,
	etaLabel *widget.Label,
	rateLimitWarning *canvas.Text,
	appendResult func(string),
	onDone func(),
) {
	startTime := time.Now()
	totalAssets := len(assetsToProcess)

	var processedCount atomic.Int64
	var completedCount atomic.Int64
	var skippedCount atomic.Int64
	var failedCount atomic.Int64
	var totalOrigBytes atomic.Int64
	var totalNewBytes atomic.Int64

	var replacementsMu sync.Mutex
	replacements := make(map[int64]int64)

	var resultLinesMu sync.Mutex
	var resultLines []string
	recordResult := func(line string) {
		resultLinesMu.Lock()
		resultLines = append(resultLines, line)
		resultLinesMu.Unlock()
		appendResult(line)
	}

	updateProgress := func(status string) {
		processed := processedCount.Load()
		fraction := float64(processed) / float64(totalAssets)
		elapsed := time.Since(startTime)
		etaText := ""
		if processed > 0 {
			avgPerAsset := elapsed / time.Duration(processed)
			remaining := time.Duration(int64(totalAssets)-processed) * avgPerAsset
			etaText = fmt.Sprintf("Estimated time remaining: %s", formatDuration(remaining))
		}
		fyne.Do(func() {
			progressBar.SetValue(fraction)
			statusLabel.SetText(status)
			etaLabel.SetText(etaText)
		})
	}

	var rateLimitedCount atomic.Int64
	onRateLimitStart := func() {
		if rateLimitedCount.Add(1) == 1 {
			fyne.Do(func() { rateLimitWarning.Show() })
		}
	}
	onRateLimitEnd := func() {
		if rateLimitedCount.Add(-1) <= 0 {
			fyne.Do(func() { rateLimitWarning.Hide() })
		}
	}

	var fatalOnce sync.Once
	var fatalMsg string
	onFatalError := func(msg string) {
		fatalOnce.Do(func() { fatalMsg = msg })
	}

	jobs := make(chan extractor.Result, len(assetsToProcess))
	for _, asset := range assetsToProcess {
		jobs <- asset
	}
	close(jobs)

	var wg sync.WaitGroup
	for w := 0; w < workerCount; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for asset := range jobs {
				select {
				case <-localStopSignal.Channel:
					return
				default:
				}

				cachedURL := ""
				if cachedURLs != nil {
					cachedURL = cachedURLs[asset.ID]
				}
				result := processOptimizeAsset(asset, cachedURL, apiKey, creator, scale, scaler, minSizeBytes, localStopSignal, onRateLimitStart, onRateLimitEnd, onFatalError)
				processedCount.Add(1)

				switch result.Status {
				case "ok":
					completedCount.Add(1)
					totalOrigBytes.Add(int64(result.OrigBytes))
					totalNewBytes.Add(int64(result.NewBytes))
					replacementsMu.Lock()
					replacements[result.AssetID] = result.NewAssetID
					replacementsMu.Unlock()
					recordResult(fmt.Sprintf("OK   %d -> %d (%dx%d -> %dx%d, %s -> %s)",
						result.AssetID, result.NewAssetID,
						result.OrigWidth, result.OrigHeight, result.NewWidth, result.NewHeight,
						format.FormatSizeAuto(result.OrigBytes), format.FormatSizeAuto(result.NewBytes)))
				case "skip":
					skippedCount.Add(1)
					recordResult(fmt.Sprintf("SKIP %d: %s", result.AssetID, result.Message))
				case "fail":
					failedCount.Add(1)
					recordResult(fmt.Sprintf("FAIL %d: %s", result.AssetID, result.Message))
				}

				processed := processedCount.Load()
				updateProgress(fmt.Sprintf("Processing assets... (%d/%d)", processed, totalAssets))
			}
		}()
	}

	wg.Wait()

	fyne.Do(func() { rateLimitWarning.Hide() })

	if fatalMsg != "" {
		fyne.Do(func() {
			progressBar.SetValue(1)
			statusLabel.SetText(fmt.Sprintf("Stopped due to error. %d replaced, %d skipped, %d failed out of %d.",
				completedCount.Load(), skippedCount.Load(), failedCount.Load(), totalAssets))
			etaLabel.SetText("")
			dialog.ShowError(fmt.Errorf("Upload rejected by Roblox:\n%s\n\nCheck that your API key has the correct permissions and the creator ID is valid.", fatalMsg), window)
			onDone()
		})
		return
	}

	select {
	case <-localStopSignal.Channel:
		fyne.Do(func() {
			statusLabel.SetText(fmt.Sprintf("Stopped. %d replaced, %d skipped, %d failed out of %d.",
				completedCount.Load(), skippedCount.Load(), failedCount.Load(), totalAssets))
			etaLabel.SetText("")
			onDone()
		})
		return
	default:
	}

	replacementsMu.Lock()
	replacementsCopy := make(map[int64]int64, len(replacements))
	for k, v := range replacements {
		replacementsCopy[k] = v
	}
	replacementsMu.Unlock()

	if len(replacementsCopy) == 0 {
		fyne.Do(func() {
			progressBar.SetValue(1)
			statusLabel.SetText(fmt.Sprintf(
				"Complete. No assets were replaced. %d skipped, %d failed.",
				skippedCount.Load(), failedCount.Load()))
			etaLabel.SetText("")
			onDone()
		})
		return
	}

	fyne.Do(func() {
		statusLabel.SetText("Replacing asset IDs in .rbxl file...")
		etaLabel.SetText("")
	})

	baseName := strings.TrimSuffix(selectedFilePath, filepath.Ext(selectedFilePath))
	outputRBXLPath := baseName + "-optimized.rbxl"
	outputIDsPath := baseName + "-optimized-ids.txt"
	outputResultsPath := baseName + "-optimized-results.txt"

	replaceCount, replaceErr := extractor.ReplaceAssetIDs(
		selectedFilePath, outputRBXLPath, replacementsCopy, localStopSignal.Channel,
	)
	if replaceErr != nil {
		fyne.Do(func() {
			progressBar.SetValue(1)
			statusLabel.SetText(fmt.Sprintf("Replace failed: %s", replaceErr.Error()))
			etaLabel.SetText("")
			onDone()
		})
		return
	}

	var idsBuilder strings.Builder
	for oldID, newID := range replacementsCopy {
		paths := assetPaths[oldID]
		if len(paths) > 0 {
			for _, p := range paths {
				idsBuilder.WriteString(fmt.Sprintf("%s: %d -> %d\n", p, oldID, newID))
			}
		} else {
			idsBuilder.WriteString(fmt.Sprintf("%d -> %d\n", oldID, newID))
		}
	}
	writeErr := os.WriteFile(outputIDsPath, []byte(idsBuilder.String()), 0644)

	resultLinesMu.Lock()
	resultsText := strings.Join(resultLines, "\n")
	resultLinesMu.Unlock()
	writeResultsErr := os.WriteFile(outputResultsPath, []byte(resultsText), 0644)

	elapsed := time.Since(startTime)
	origTotal := totalOrigBytes.Load()
	newTotal := totalNewBytes.Load()
	completed := completedCount.Load()
	skipped := skippedCount.Load()
	failed := failedCount.Load()

	savingsText := ""
	if origTotal > 0 {
		reduction := float64(origTotal-newTotal) / float64(origTotal) * 100
		savingsText = fmt.Sprintf(" Total size: %s -> %s (%.0f%% reduction).",
			format.FormatSizeAuto(int(origTotal)), format.FormatSizeAuto(int(newTotal)), reduction)
	}

	fyne.Do(func() {
		progressBar.SetValue(1)
		summary := fmt.Sprintf(
			"Done in %s. %d assets replaced (%d properties updated), %d skipped, %d failed.%s",
			formatDuration(elapsed), completed, replaceCount, skipped, failed, savingsText,
		)
		statusLabel.SetText(summary)
		etaLabel.SetText("")
		appendResult("")
		appendResult(fmt.Sprintf("Saved: %s", filepath.Base(outputRBXLPath)))
		if writeErr == nil {
			appendResult(fmt.Sprintf("Saved: %s", filepath.Base(outputIDsPath)))
		}
		if writeResultsErr == nil {
			appendResult(fmt.Sprintf("Saved: %s", filepath.Base(outputResultsPath)))
		}
		onDone()
	})
}

func processOptimizeAsset(
	asset extractor.Result,
	cachedURL string,
	apiKey string,
	creator opencloud.Creator,
	scale float64,
	scaler xdraw.Interpolator,
	minSizeBytes int,
	stop *loader.StopSignal,
	onRateLimitStart func(),
	onRateLimitEnd func(),
	onFatalError func(string),
) optimizeAssetResult {
	fail := func(msg string) optimizeAssetResult {
		return optimizeAssetResult{AssetID: asset.ID, Status: "fail", Message: msg}
	}

	downloadFromURL := func(url string) ([]byte, error) {
		bodyBytes, _, err := loader.DownloadRobloxContentBytesWithCacheKey(
			url,
			loader.BuildAssetFileContentCacheKey(asset.ID, roblox.AssetTypeImage),
			30*time.Second,
		)
		if err != nil {
			return nil, err
		}
		return bodyBytes, nil
	}

	var imageBytes []byte
	var deliveryErr error

	if cachedURL != "" {
		imageBytes, deliveryErr = downloadFromURL(cachedURL)
		if deliveryErr == nil && len(imageBytes) > 0 {
			goto downloaded
		}
		debug.Logf("Optimize: asset %d cached URL failed, will re-fetch: %v", asset.ID, deliveryErr)
	}

	for attempt := 0; attempt <= optimizeMaxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * time.Second)
		}
		var deliveryInfo *loader.AssetDeliveryInfo
		deliveryInfo, deliveryErr = loader.FetchAssetDeliveryInfo(asset.ID)
		if deliveryErr != nil || deliveryInfo == nil || deliveryInfo.Location == "" {
			if deliveryErr == nil {
				deliveryErr = fmt.Errorf("no delivery URL")
			}
			debug.Logf("Optimize: asset %d delivery attempt %d failed: %s", asset.ID, attempt+1, deliveryErr.Error())
			continue
		}
		imageBytes, deliveryErr = downloadFromURL(deliveryInfo.Location)
		if deliveryErr != nil {
			continue
		}
		break
	}
	if deliveryErr != nil {
		return fail(fmt.Sprintf("download failed - %s", deliveryErr.Error()))
	}
	if len(imageBytes) == 0 {
		return fail("download returned empty response")
	}

downloaded:

	if minSizeBytes > 0 && len(imageBytes) < minSizeBytes {
		return optimizeAssetResult{
			AssetID: asset.ID,
			Status:  "skip",
			Message: fmt.Sprintf("%s (under %s threshold)", format.FormatSizeAuto(len(imageBytes)), format.FormatSizeAuto(minSizeBytes)),
		}
	}

	decodedImage, imageFormat, decodeErr := image.Decode(bytes.NewReader(imageBytes))
	if decodeErr != nil {
		if errors.Is(decodeErr, image.ErrFormat) {
			return optimizeAssetResult{
				AssetID: asset.ID,
				Status:  "skip",
				Message: "not an image asset",
			}
		}
		return fail(fmt.Sprintf("not a valid image - %s", decodeErr.Error()))
	}
	origBounds := decodedImage.Bounds()
	origWidth := origBounds.Dx()
	origHeight := origBounds.Dy()

	resizedBytes, resizeErr := ui.EncodeScaledPreview(decodedImage, imageFormat, scale, scaler)
	if resizeErr != nil {
		return fail(fmt.Sprintf("resize failed - %s", resizeErr.Error()))
	}

	newWidth := max(1, int(float64(origWidth)*scale))
	newHeight := max(1, int(float64(origHeight)*scale))

	uploadFileName := fmt.Sprintf("optimized_%d.png", asset.ID)

	var newAssetID int64
	var uploadErr error
	rateLimitBackoff := 5 * time.Second
	attempts := 0
	for {
		newAssetID, uploadErr = opencloud.UploadDecal(
			apiKey, creator,
			fmt.Sprintf("optimized_%d", asset.ID),
			"",
			uploadFileName,
			resizedBytes,
			stop.Channel,
		)
		if uploadErr == nil {
			break
		}
		if errors.Is(uploadErr, opencloud.ErrUploadCancelled) {
			return fail("stopped")
		}
		if errors.Is(uploadErr, opencloud.ErrUploadForbidden) {
			onFatalError(uploadErr.Error())
			stop.Stop()
			return fail(uploadErr.Error())
		}
		if errors.Is(uploadErr, opencloud.ErrRateLimited) {
			debug.Logf("Optimize: asset %d rate limited, backing off %s", asset.ID, rateLimitBackoff)
			onRateLimitStart()
			select {
			case <-stop.Channel:
				onRateLimitEnd()
				return fail("stopped")
			case <-time.After(rateLimitBackoff):
			}
			onRateLimitEnd()
			rateLimitBackoff = min(rateLimitBackoff*2, 60*time.Second)
			continue
		}
		attempts++
		debug.Logf("Optimize: asset %d upload attempt %d failed: %s", asset.ID, attempts, uploadErr.Error())
		if attempts > optimizeMaxRetries {
			break
		}
		select {
		case <-stop.Channel:
			return fail("stopped")
		case <-time.After(time.Duration(attempts) * time.Second):
		}
	}
	if uploadErr != nil {
		return fail(fmt.Sprintf("upload failed - %s", uploadErr.Error()))
	}

	return optimizeAssetResult{
		AssetID:    asset.ID,
		NewAssetID: newAssetID,
		Status:     "ok",
		OrigBytes:  len(imageBytes),
		NewBytes:   len(resizedBytes),
		OrigWidth:  origWidth,
		OrigHeight: origHeight,
		NewWidth:   newWidth,
		NewHeight:  newHeight,
	}
}

func collectOptimizableAssetIDs(results []extractor.Result, useWhitelist bool, whitelistText string) ([]int64, int) {
	type optimizeCandidateState struct {
		hasThumbReference   bool
		hasRegularReference bool
	}

	candidateStates := map[int64]optimizeCandidateState{}
	candidateOrder := []int64{}
	for _, result := range results {
		if result.ID <= 0 {
			continue
		}
		if useWhitelist && !common.MatchesAnyPathWhitelist(result.InstancePath, whitelistText) {
			continue
		}
		state, exists := candidateStates[result.ID]
		if !exists {
			candidateOrder = append(candidateOrder, result.ID)
		}
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(result.RawContent)), "rbxthumb://") {
			state.hasThumbReference = true
		} else {
			state.hasRegularReference = true
		}
		candidateStates[result.ID] = state
	}

	candidateIDs := make([]int64, 0, len(candidateOrder))
	ignoredThumbCount := 0
	for _, assetID := range candidateOrder {
		state := candidateStates[assetID]
		if state.hasRegularReference {
			candidateIDs = append(candidateIDs, assetID)
			continue
		}
		if state.hasThumbReference {
			ignoredThumbCount++
		}
	}
	return candidateIDs, ignoredThumbCount
}

func formatDuration(d time.Duration) string {
	if d < time.Second {
		return "< 1s"
	}
	d = d.Round(time.Second)
	minutes := int(d.Minutes())
	seconds := int(d.Seconds()) % 60
	if minutes > 0 {
		return fmt.Sprintf("%dm %ds", minutes, seconds)
	}
	return fmt.Sprintf("%ds", seconds)
}
