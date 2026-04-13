package app

import (
	"errors"
	"fmt"
	"strings"

	"joxblox/internal/app/ui"
	"joxblox/internal/debug"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	nativeDialog "github.com/sqweek/dialog"
)

func bindMainFileMenu(
	window fyne.Window,
	tabs *container.AppTabs,
	scanTab *container.TabItem,
	scanFileActions scanTabFileActionsProvider,
	allScanFileActions []scanTabFileActionsProvider,
	selectScanContext func(string),
) {
	var rebuildMainMenu func()

	rebuildMainMenu = func() {
		fileMenu := fyne.NewMenu(
			"File",
			fyne.NewMenuItem("Save Results (.json)", func() {
				saveAllScanResults(window, allScanFileActions)
			}),
			fyne.NewMenuItem("Load Results (.json)", func() {
				loadAllScanResultsFromPickerAsync(window, allScanFileActions, func(selectedContext string, loaded bool) {
					if loaded {
						switchToContextTab(tabs, scanTab, selectScanContext, selectedContext)
						rebuildMainMenu()
					}
				})
			}),
			fyne.NewMenuItem("Clear All Results", func() {
				clearAllScanResults(window, allScanFileActions)
			}),
			fyne.NewMenuItemSeparator(),
			fyne.NewMenuItem("Settings", func() {
				showSettingsDialog(window)
			}),
			fyne.NewMenuItemSeparator(),
		)
		fileMenu.Items = append(fileMenu.Items, buildRecentFilesMenuItem(
			window,
			tabs,
			scanTab,
			selectScanContext,
			allScanFileActions,
		))
		window.SetMainMenu(fyne.NewMainMenu(fileMenu, buildHelpMenu(window)))
	}

	tabs.OnSelected = func(selectedTab *container.TabItem) {
		if selectedTab == nil {
			rebuildMainMenu()
			return
		}
		rebuildMainMenu()
	}

	rebuildMainMenu()
}

func getActiveFileActions(provider scanTabFileActionsProvider) *scanTabFileActions {
	if provider == nil {
		return nil
	}
	return provider()
}

func showFileActionsUnavailableDialog(window fyne.Window) {
	dialog.ShowInformation("File", "File actions are available in the Scan tab.", window)
}

func buildRecentFilesMenuItem(
	window fyne.Window,
	tabs *container.AppTabs,
	scanTab *container.TabItem,
	selectScanContext func(string),
	providers []scanTabFileActionsProvider,
) *fyne.MenuItem {
	recentFilesItem := fyne.NewMenuItem("Recent Files", nil)
	recentFilesMenu := fyne.NewMenu("Recent Files")
	recentPaths := collectRecentFiles(providers)
	for _, recentPath := range recentPaths {
		pathCopy := recentPath
		recentFilesMenu.Items = append(recentFilesMenu.Items, fyne.NewMenuItem(pathCopy, func() {
			loadAllScanResultsFromPathAsync(window, providers, pathCopy, func(selectedContext string, loaded bool) {
				if loaded {
					switchToContextTab(tabs, scanTab, selectScanContext, selectedContext)
				}
			})
		}))
	}
	if len(recentFilesMenu.Items) == 0 {
		noRecentItem := fyne.NewMenuItem("(none)", nil)
		noRecentItem.Disabled = true
		recentFilesMenu.Items = append(recentFilesMenu.Items, noRecentItem)
	}
	recentFilesItem.ChildMenu = recentFilesMenu
	return recentFilesItem
}

func collectScanFileActions(providers []scanTabFileActionsProvider) []*scanTabFileActions {
	actionsByContextKey := map[string]*scanTabFileActions{}
	collectedActions := make([]*scanTabFileActions, 0, len(providers))
	for _, provider := range providers {
		if provider == nil {
			continue
		}
		actions := provider()
		if actions == nil {
			continue
		}
		contextKey := strings.TrimSpace(actions.ContextKey)
		if contextKey == "" {
			continue
		}
		if _, exists := actionsByContextKey[contextKey]; exists {
			continue
		}
		actionsByContextKey[contextKey] = actions
		collectedActions = append(collectedActions, actions)
	}
	return collectedActions
}

func snapshotScanWorkspace(collectedActions []*scanTabFileActions) map[string][]scanResult {
	tablesByContext := map[string][]scanResult{}
	for _, actions := range collectedActions {
		if actions.GetResults == nil {
			continue
		}
		rows := actions.GetResults()
		tablesByContext[actions.ContextKey] = append([]scanResult(nil), rows...)
	}
	return tablesByContext
}

func clearAllScanResults(window fyne.Window, providers []scanTabFileActionsProvider) {
	collectedActions := collectScanFileActions(providers)
	if len(collectedActions) == 0 {
		dialog.ShowInformation("File", "No scan tables are available to clear yet.", window)
		return
	}

	totalRows := 0
	for _, actions := range collectedActions {
		if actions.GetResults == nil {
			continue
		}
		totalRows += len(actions.GetResults())
	}
	if totalRows == 0 {
		dialog.ShowInformation("File", "All scan tables are already empty.", window)
		return
	}

	confirmMessage := fmt.Sprintf(
		"Clear all scan results across every scan context?\n\nThis will remove %d loaded result(s) from the app.",
		totalRows,
	)
	dialog.ShowConfirm("Clear All Results", confirmMessage, func(confirmed bool) {
		if !confirmed {
			return
		}
		for _, actions := range collectedActions {
			if actions.SetResults == nil {
				continue
			}
			actions.SetResults(nil)
		}
		debug.Logf("All scan tables cleared from file menu (rows_cleared=%d)", totalRows)
	}, window)
}

func saveAllScanResults(window fyne.Window, providers []scanTabFileActionsProvider) {
	collectedActions := collectScanFileActions(providers)
	if len(collectedActions) == 0 {
		dialog.ShowInformation("File", "No scan tables are available to save yet.", window)
		return
	}

	selectedExportPath, pickerErr := nativeDialog.File().
		Filter("JSON files", "json").
		Title("Save all scan tables").
		Save()
	if pickerErr != nil {
		if errors.Is(pickerErr, nativeDialog.Cancelled) {
			return
		}
		dialog.ShowError(fmt.Errorf("save picker failed: %w", pickerErr), window)
		return
	}
	if strings.TrimSpace(selectedExportPath) == "" {
		return
	}
	if !strings.HasSuffix(strings.ToLower(selectedExportPath), ".json") {
		selectedExportPath += ".json"
	}
	tablesByContext := snapshotScanWorkspace(collectedActions)
	progress := ui.NewProgressDialog(window, "Save JSON", "Serializing all scan tables...")
	serializeProgress := ui.ProgressRangeReporter(progress, 0.05, 0.8, "Serializing all scan tables...")
	writeProgress := ui.ProgressRangeReporter(progress, 0.8, 1, "Writing JSON file...")
	go func() {
		payloadBytes, marshalErr := marshalScanWorkspace(tablesByContext, serializeProgress)
		if marshalErr != nil {
			progress.Hide()
			fyne.Do(func() {
				dialog.ShowError(fmt.Errorf("save failed: %w", marshalErr), window)
			})
			return
		}
		if writeErr := writeFileWithProgress(selectedExportPath, payloadBytes, writeProgress); writeErr != nil {
			progress.Hide()
			fyne.Do(func() {
				dialog.ShowError(fmt.Errorf("save write failed: %w", writeErr), window)
			})
			return
		}
		progress.Hide()
		debug.Logf("Scan workspace exported: %s", selectedExportPath)
	}()
}

func loadAllScanResultsFromPickerAsync(
	window fyne.Window,
	providers []scanTabFileActionsProvider,
	onComplete func(string, bool),
) {
	collectedActions := collectScanFileActions(providers)
	if len(collectedActions) == 0 {
		dialog.ShowInformation("File", "No scan tables are available to load into yet.", window)
		if onComplete != nil {
			onComplete("", false)
		}
		return
	}

	selectedImportPath, pickerErr := nativeDialog.File().
		Filter("JSON files", "json").
		Title("Load all scan tables").
		Load()
	if pickerErr != nil {
		if errors.Is(pickerErr, nativeDialog.Cancelled) {
			if onComplete != nil {
				onComplete("", false)
			}
			return
		}
		dialog.ShowError(fmt.Errorf("load picker failed: %w", pickerErr), window)
		if onComplete != nil {
			onComplete("", false)
		}
		return
	}
	if strings.TrimSpace(selectedImportPath) == "" {
		if onComplete != nil {
			onComplete("", false)
		}
		return
	}
	loadAllScanResultsFromPathWithActionsAsync(window, collectedActions, selectedImportPath, onComplete)
}

func loadAllScanResultsFromPathAsync(
	window fyne.Window,
	providers []scanTabFileActionsProvider,
	importPath string,
	onComplete func(string, bool),
) {
	collectedActions := collectScanFileActions(providers)
	if len(collectedActions) == 0 {
		dialog.ShowInformation("File", "No scan tables are available to load into yet.", window)
		if onComplete != nil {
			onComplete("", false)
		}
		return
	}
	loadAllScanResultsFromPathWithActionsAsync(window, collectedActions, importPath, onComplete)
}

func loadAllScanResultsFromPathWithActionsAsync(
	window fyne.Window,
	collectedActions []*scanTabFileActions,
	importPath string,
	onComplete func(string, bool),
) {
	progress := ui.NewProgressDialog(window, "Load JSON", "Reading scan tables...")
	readProgress := ui.ProgressRangeReporter(progress, 0, 0.3, "Reading scan tables...")
	parseProgress := ui.ProgressRangeReporter(progress, 0.3, 0.9, "Parsing scan tables...")
	go func() {
		payloadBytes, readErr := readFileWithProgress(importPath, readProgress)
		if readErr != nil {
			progress.Hide()
			fyne.Do(func() {
				dialog.ShowError(fmt.Errorf("load read failed: %w", readErr), window)
				if onComplete != nil {
					onComplete("", false)
				}
			})
			return
		}
		progress.Update(0.3, "Parsing scan tables...")
		tablesByContext, parseErr := unmarshalScanWorkspace(payloadBytes, parseProgress)
		if parseErr != nil {
			progress.Hide()
			fyne.Do(func() {
				dialog.ShowError(fmt.Errorf("load parse failed: %w", parseErr), window)
				if onComplete != nil {
					onComplete("", false)
				}
			})
			return
		}
		progress.Update(0.95, "Applying imported tables...")
		fyne.Do(func() {
			progress.Hide()
			firstContextWithRows := ""
			for _, actions := range collectedActions {
				if actions.SetResults == nil {
					continue
				}
				rows, exists := tablesByContext[actions.ContextKey]
				if !exists {
					rows = []scanResult{}
				}
				actions.SetResults(rows)
				if len(rows) > 0 && firstContextWithRows == "" {
					firstContextWithRows = actions.ContextKey
				}
				if actions.AddRecentFile != nil {
					actions.AddRecentFile(importPath)
				}
			}
			debug.Logf("Scan workspace imported: %s", importPath)
			if onComplete != nil {
				onComplete(firstContextWithRows, true)
			}
		})
	}()
}

func collectRecentFiles(providers []scanTabFileActionsProvider) []string {
	collectedActions := collectScanFileActions(providers)
	seen := map[string]bool{}
	recentPaths := make([]string, 0, 16)
	for _, actions := range collectedActions {
		if actions.RecentFiles == nil {
			continue
		}
		for _, recentPath := range actions.RecentFiles() {
			trimmedPath := strings.TrimSpace(recentPath)
			if trimmedPath == "" {
				continue
			}
			normalizedKey := strings.ToLower(trimmedPath)
			if seen[normalizedKey] {
				continue
			}
			seen[normalizedKey] = true
			recentPaths = append(recentPaths, trimmedPath)
		}
	}
	return recentPaths
}

func switchToContextTab(
	tabs *container.AppTabs,
	scanTab *container.TabItem,
	selectScanContext func(string),
	contextKey string,
) {
	switch strings.TrimSpace(contextKey) {
	case scanContextFolder, scanContextFolderDiff, scanContextRBXLSingle, scanContextRBXLDiff:
		if scanTab != nil {
			tabs.Select(scanTab)
		}
		if selectScanContext != nil {
			selectScanContext(contextKey)
		}
	}
}
