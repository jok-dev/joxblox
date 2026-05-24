package app

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"joxblox/internal/app/common"
	"joxblox/internal/app/loader"
	"joxblox/internal/app/scan"
	"joxblox/internal/app/ui"
	"joxblox/internal/app/ui/menu"
	"joxblox/internal/app/ui/tabs/diff"
	"joxblox/internal/app/ui/tabs/heatmap"
	"joxblox/internal/app/ui/tabs/imageuploader"
	"joxblox/internal/app/ui/tabs/lodviewer"
	"joxblox/internal/app/ui/tabs/optimizeassets"
	renderdoctab "joxblox/internal/app/ui/tabs/renderdoc"
	"joxblox/internal/app/ui/tabs/reportgeneration"
	"joxblox/internal/app/ui/tabs/singleasset"
	"joxblox/internal/debug"
	"joxblox/internal/extractor"
	"joxblox/internal/roblox/mesh"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	fynetooltip "github.com/dweymouth/fyne-tooltip"
)

const (
	previewWidth  = ui.PreviewWidth
	previewHeight = ui.PreviewHeight
)

func getPrimaryWindow() fyne.Window {
	currentApp := fyne.CurrentApp()
	if currentApp == nil {
		return nil
	}
	windows := currentApp.Driver().AllWindows()
	if len(windows) == 0 {
		return nil
	}
	return windows[0]
}

//go:embed app_icon.svg
var appIconSVG []byte

func Run() {
	debug.Logf = ui.LogDebugf
	extractor.BinaryProvider = bundledRustyAssetToolBinary
	ui.MeshRendererBinaryProvider = bundledMeshRendererBinary
	mesh.CoreMeshFallback = extractor.ExtractMeshStatsFromBytes
	loader.LoadCacheSettings = func() loader.CacheSettings {
		settings := loadAssetDownloadCacheSettings()
		return loader.CacheSettings{Enabled: settings.Enabled, Folder: settings.Folder}
	}
	loader.IsAudioContent = ui.IsAudioContent
	loader.ExtractAudio = func(fileName, contentType string, data []byte) (*loader.AudioMetadata, error) {
		meta, err := ui.ExtractAudioMetadata(fileName, contentType, data)
		if err != nil {
			return nil, err
		}
		return &loader.AudioMetadata{Duration: meta.Duration, Format: meta.Format}, nil
	}
	ui.GetPrimaryWindow = getPrimaryWindow
	ui.LoadMouseLookSensitivity = loadMeshPreviewMouseLookSensitivity
	ui.GetRepositoryRootPath = getRepositoryRootPath
	ui.InitializeDebugLogFile()
	guiApp := app.NewWithID("dev.jok.joxblox")
	appIcon := fyne.NewStaticResource("app_icon.svg", appIconSVG)
	guiApp.SetIcon(appIcon)
	window := guiApp.NewWindow(fmt.Sprintf("%s %s", appDisplayName, appVersion))
	window.SetIcon(appIcon)
	window.Resize(fyne.NewSize(1280, 760))

	var viewInScanCallback func(string, bool, float64, []loader.ScanResult, string)
	var viewInHeatmapCallback func(string)
	reportGenerationContent, loadReportFile, setReportPlaceFile := reportgeneration.NewReportGenerationTab(
		window,
		func(path string, workspaceOnly bool, oversizedTextureThreshold float64, prebuiltResults []loader.ScanResult, assetTypeLabel string) {
			if viewInScanCallback != nil {
				viewInScanCallback(path, workspaceOnly, oversizedTextureThreshold, prebuiltResults, assetTypeLabel)
			}
		},
		func(path string) {
			if viewInHeatmapCallback != nil {
				viewInHeatmapCallback(path)
			}
		},
	)
	singleAssetContent, loadSingleAssetByID := singleasset.NewSingleAssetTab(window)
	scanContent, scanFileActions, allScanFileActions, selectScanContext, loadScanRBXLFile, setScanPlaceFile := scan.NewScanTab(window)
	rbxlHeatmapContent, loadHeatmapRBXLFile, setHeatmapPlaceFile := heatmaptab.NewRBXLHeatmapTab(window)

	nav := newNavigator([]navGroupDef{
		{title: "Core Analytics", items: []navItemDef{
			{key: tabTitleReportGeneration, title: tabTitleReportGeneration, content: reportGenerationContent},
			{key: tabTitleScan, title: tabTitleScan, content: scanContent},
			{key: tabTitleRBXLHeatmap, title: tabTitleRBXLHeatmap, content: rbxlHeatmapContent},
			{key: tabTitleModelHeatmap, title: tabTitleModelHeatmap, content: heatmaptab.NewModelHeatmapTab(window)},
		}},
		{title: "Asset Management", items: []navItemDef{
			{key: tabTitleSingleAsset, title: tabTitleSingleAsset, content: singleAssetContent},
			{key: tabTitleLodViewer, title: tabTitleLodViewer, content: lodviewer.NewLodViewerTab(window)},
			{key: tabTitleOptimizeAssets, title: tabTitleOptimizeAssets, content: optimizeassets.NewOptimizeAssetsTab(window)},
		}},
		{title: "Utilities", items: []navItemDef{
			{key: tabTitleImageGenerator, title: tabTitleImageGenerator, content: imageuploader.NewImageUploaderTab(window)},
			{key: tabTitleRenderDoc, title: tabTitleRenderDoc, content: renderdoctab.NewRenderDocTab(window)},
			{key: tabTitleDiff, title: tabTitleDiff, content: diff.NewDiffTab(window)},
		}},
	})
	nav.Select(tabTitleReportGeneration)
	ui.OpenSingleAsset = func(assetID int64) {
		nav.Select(tabTitleSingleAsset)
		if loadSingleAssetByID != nil {
			loadSingleAssetByID(assetID)
		}
	}
	viewInScanCallback = func(path string, workspaceOnly bool, oversizedTextureThreshold float64, prebuiltResults []loader.ScanResult, assetTypeLabel string) {
		nav.Select(tabTitleScan)
		if loadScanRBXLFile != nil {
			pathFilter := ""
			if workspaceOnly {
				pathFilter = "Workspace.*\nMaterialService.*"
			}
			loadScanRBXLFile(path, scan.ScanLoadOptions{
				PathFilterText:        pathFilter,
				LargeTextureThreshold: oversizedTextureThreshold,
				AssetTypeLabel:        assetTypeLabel,
				PrebuiltResults:       prebuiltResults,
			})
		}
	}
	viewInHeatmapCallback = func(path string) {
		nav.Select(tabTitleRBXLHeatmap)
		if loadHeatmapRBXLFile != nil {
			loadHeatmapRBXLFile(path)
		}
	}
	selectScanTab := func() { nav.Select(tabTitleScan) }
	placeFileBar, setSharedPlaceFile := buildPlaceFileBar(window, []func(string){
		setReportPlaceFile,
		setScanPlaceFile,
		setHeatmapPlaceFile,
	})
	auth := newAuthController(window)
	showSettings := func(w fyne.Window) {
		showSettingsDialog(w, auth.settingsSection())
	}
	helpAbout := menu.AboutMeta{
		DisplayName: appDisplayName,
		Version:     appVersion,
		Author:      appAuthorName,
		LicenseName: appLicenseName,
		SourceURL:   appSourceURL,
	}
	menu.BindMainFileMenu(
		window,
		selectScanTab,
		scanFileActions,
		allScanFileActions,
		selectScanContext,
		showSettings,
		func(w fyne.Window) *fyne.Menu {
			return menu.BuildHelpMenu(w, helpAbout, loadChangelogText, loadLicenseText)
		},
	)
	if dropWindow, ok := window.(interface {
		SetOnDropped(func(position fyne.Position, uris []fyne.URI))
	}); ok {
		dropWindow.SetOnDropped(func(_ fyne.Position, uris []fyne.URI) {
			for _, uri := range uris {
				if uri == nil {
					continue
				}
				candidatePath := strings.TrimSpace(uri.Path())
				if candidatePath == "" || !strings.EqualFold(filepath.Ext(candidatePath), ".json") {
					continue
				}
				go func(importPath string, droppedURIs []fyne.URI) {
					importBytes, readErr := os.ReadFile(importPath)
					if readErr != nil {
						fyne.Do(func() {
							dialog.ShowError(fmt.Errorf("drop read failed: %w", readErr), window)
						})
						return
					}
					importFormat := scan.DetectScanImportFormat(importBytes)
					fyne.Do(func() {
						nav.Select(tabTitleScan)
						if importFormat == scan.ScanImportFormatWorkspace {
							menu.LoadAllScanResultsFromPathAsync(window, allScanFileActions, importPath, func(selectedContext string, loaded bool) {
								if loaded && selectScanContext != nil && selectedContext != "" {
									selectScanContext(selectedContext)
								}
							})
							return
						}
						if scanFileActions == nil {
							return
						}
						if activeActions := scanFileActions(); activeActions != nil && activeActions.HandleDrop != nil {
							activeActions.HandleDrop(droppedURIs)
						}
					})
				}(candidatePath, uris)
				return
			}
			for _, uri := range uris {
				if uri == nil {
					continue
				}
				candidatePath := strings.TrimSpace(uri.Path())
				if !common.IsRobloxDOMFilePath(candidatePath) {
					continue
				}
				fyne.Do(func() {
					setSharedPlaceFile(candidatePath)
					nav.Select(tabTitleReportGeneration)
					if loadReportFile != nil {
						loadReportFile(candidatePath)
					}
				})
				return
			}
		})
	}
	header := buildHeaderBar(auth, showSettings, window, placeFileBar)
	mainContent := container.NewBorder(header, nil, nil, nil, nav.Root())
	window.SetContent(fynetooltip.AddWindowToolTipLayer(mainContent, window.Canvas()))
	window.CenterOnScreen()
	debug.Logf("WINFIT maincontent min -> %.0f x %.0f", mainContent.MinSize().Width, mainContent.MinSize().Height)
	debug.Logf("Application started")
	window.ShowAndRun()
}

// buildHeaderBar constructs the persistent top header: app brand on the left,
// the shared place-file bar in the center, and the auth-status indicator plus
// a Settings gear on the right.
func buildHeaderBar(auth *authController, showSettings func(fyne.Window), window fyne.Window, placeFileBar fyne.CanvasObject) fyne.CanvasObject {
	brand := widget.NewLabelWithStyle(appDisplayName, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	gearButton := widget.NewButtonWithIcon("", theme.SettingsIcon(), func() {
		if showSettings != nil {
			showSettings(window)
		}
	})
	gearButton.Importance = widget.LowImportance
	rightControls := container.NewHBox(auth.statusObject(), gearButton)
	headerRow := container.NewBorder(nil, nil, container.NewVBox(brand), rightControls, container.NewVBox(placeFileBar))
	return container.NewVBox(
		container.NewPadded(headerRow),
		widget.NewSeparator(),
	)
}
