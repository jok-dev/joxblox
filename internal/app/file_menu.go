package app

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
)

func bindMainFileMenu(
	window fyne.Window,
	tabs *container.AppTabs,
	folderScanFileActions scanTabFileActionsProvider,
	rbxlScanFileActions scanTabFileActionsProvider,
) {
	activeFileActionsProvider := scanTabFileActionsProvider(nil)
	var rebuildMainMenu func()

	runFileAction := func(refreshMenuAfterAction bool, action func(fileActions *scanTabFileActions)) {
		fileActions := getActiveFileActions(activeFileActionsProvider)
		if fileActions == nil {
			showFileActionsUnavailableDialog(window)
			return
		}

		action(fileActions)
		if refreshMenuAfterAction {
			rebuildMainMenu()
		}
	}

	rebuildMainMenu = func() {
		fileMenu := fyne.NewMenu(
			"File",
			fyne.NewMenuItem("Save Results (.json)", func() {
				runFileAction(false, func(fileActions *scanTabFileActions) {
					fileActions.SaveJSON()
				})
			}),
			fyne.NewMenuItem("Load Results (.json)", func() {
				runFileAction(true, func(fileActions *scanTabFileActions) {
					fileActions.LoadJSON()
				})
			}),
			fyne.NewMenuItem("Export Results (.md)", func() {
				runFileAction(false, func(fileActions *scanTabFileActions) {
					fileActions.ExportMarkdown()
				})
			}),
			fyne.NewMenuItemSeparator(),
		)
		fileMenu.Items = append(fileMenu.Items, buildRecentFilesMenuItem(activeFileActionsProvider, runFileAction))
		window.SetMainMenu(fyne.NewMainMenu(fileMenu))
	}

	tabs.OnSelected = func(selectedTab *container.TabItem) {
		if selectedTab == nil {
			activeFileActionsProvider = nil
			rebuildMainMenu()
			return
		}

		switch selectedTab.Text {
		case "Folder Scan":
			activeFileActionsProvider = folderScanFileActions
		case "RBXL Scan":
			activeFileActionsProvider = rbxlScanFileActions
		default:
			activeFileActionsProvider = nil
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
	dialog.ShowInformation("File", "File actions are available in Folder Scan and RBXL Scan tabs.", window)
}

func buildRecentFilesMenuItem(
	provider scanTabFileActionsProvider,
	runFileAction func(refreshMenuAfterAction bool, action func(fileActions *scanTabFileActions)),
) *fyne.MenuItem {
	recentFilesItem := fyne.NewMenuItem("Recent Files", nil)
	recentFilesMenu := fyne.NewMenu("Recent Files")
	fileActions := getActiveFileActions(provider)
	if fileActions != nil {
		recentPaths := fileActions.RecentFiles()
		for _, recentPath := range recentPaths {
			pathCopy := recentPath
			recentFilesMenu.Items = append(recentFilesMenu.Items, fyne.NewMenuItem(pathCopy, func() {
				runFileAction(true, func(fileActions *scanTabFileActions) {
					fileActions.LoadRecent(pathCopy)
				})
			}))
		}
	}
	if len(recentFilesMenu.Items) == 0 {
		noRecentItem := fyne.NewMenuItem("(none)", nil)
		noRecentItem.Disabled = true
		recentFilesMenu.Items = append(recentFilesMenu.Items, noRecentItem)
	}
	recentFilesItem.ChildMenu = recentFilesMenu
	return recentFilesItem
}
