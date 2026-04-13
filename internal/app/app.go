package app

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"joxblox/internal/debug"
	"joxblox/internal/extractor"
	"joxblox/internal/roblox"
	"joxblox/internal/roblox/mesh"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	fynetooltip "github.com/dweymouth/fyne-tooltip"
)

const (
	previewWidth  = 440
	previewHeight = 300
)

//go:embed app_icon.svg
var appIconSVG []byte

func Run() {
	debug.Logf = logDebugf
	extractor.BinaryProvider = bundledRustyAssetToolBinary
	mesh.CoreMeshFallback = extractor.ExtractMeshStatsFromBytes
	initializeDebugLogFile()
	guiApp := app.NewWithID("dev.jok.joxblox")
	appIcon := fyne.NewStaticResource("app_icon.svg", appIconSVG)
	guiApp.SetIcon(appIcon)
	window := guiApp.NewWindow(appDisplayName)
	window.SetIcon(appIcon)
	window.Resize(fyne.NewSize(1350, 900))

	var viewInScanCallback func(string)
	var viewInHeatmapCallback func(string)
	reportGenerationContent, loadReportFile := newReportGenerationTab(
		window,
		func(path string) {
			if viewInScanCallback != nil {
				viewInScanCallback(path)
			}
		},
		func(path string) {
			if viewInHeatmapCallback != nil {
				viewInHeatmapCallback(path)
			}
		},
	)
	reportGenerationTab := container.NewTabItem(tabTitleReportGeneration, reportGenerationContent)
	singleAssetTab := container.NewTabItem(tabTitleSingleAsset, newSingleAssetTab(window))
	scanContent, scanFileActions, allScanFileActions, selectScanContext, loadScanRBXLFile := newScanTab(window)
	scanTab := container.NewTabItem(tabTitleScan, scanContent)
	rbxlHeatmapContent, loadHeatmapRBXLFile := newRBXLHeatmapTab(window)
	rbxlHeatmapTab := container.NewTabItem(tabTitleRBXLHeatmap, rbxlHeatmapContent)
	modelHeatmapTab := container.NewTabItem(tabTitleModelHeatmap, newModelHeatmapTab(window))
	optimizeTab := container.NewTabItem(tabTitleOptimizeAssets, newOptimizeAssetsTab(window))
	imageUploaderTab := container.NewTabItem(tabTitleImageGenerator, newImageUploaderTab(window))
	tabs := container.NewAppTabs(reportGenerationTab, singleAssetTab, scanTab, rbxlHeatmapTab, modelHeatmapTab, optimizeTab, imageUploaderTab)
	tabs.Select(reportGenerationTab)
	viewInScanCallback = func(path string) {
		tabs.Select(scanTab)
		if loadScanRBXLFile != nil {
			loadScanRBXLFile(path)
		}
	}
	viewInHeatmapCallback = func(path string) {
		tabs.Select(rbxlHeatmapTab)
		if loadHeatmapRBXLFile != nil {
			loadHeatmapRBXLFile(path)
		}
	}
	bindMainFileMenu(
		window,
		tabs,
		scanTab,
		scanFileActions,
		allScanFileActions,
		selectScanContext,
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
					importFormat := detectScanImportFormat(importBytes)
					fyne.Do(func() {
						tabs.Select(scanTab)
						if importFormat == scanImportFormatWorkspace {
							loadAllScanResultsFromPathAsync(window, allScanFileActions, importPath, func(selectedContext string, loaded bool) {
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
				if !isRobloxDOMFilePath(candidatePath) {
					continue
				}
				fyne.Do(func() {
					tabs.Select(reportGenerationTab)
					if loadReportFile != nil {
						loadReportFile(candidatePath)
					}
				})
				return
			}
		})
	}
	authPanel := newAuthPanel(window)
	mainContent := container.NewBorder(nil, authPanel, nil, nil, tabs)
	var setLayoutMode func(showConsole bool)
	debugConsolePanel := newDebugConsolePanel(func(showConsole bool) {
		if setLayoutMode != nil {
			setLayoutMode(showConsole)
		}
	})
	resizableLayout := container.NewVSplit(mainContent, debugConsolePanel)
	resizableLayout.Offset = 0.82
	collapsedLayout := container.NewBorder(nil, debugConsolePanel, nil, nil, mainContent)
	setLayoutMode = func(showConsole bool) {
		if showConsole {
			window.SetContent(fynetooltip.AddWindowToolTipLayer(resizableLayout, window.Canvas()))
			return
		}
		window.SetContent(fynetooltip.AddWindowToolTipLayer(collapsedLayout, window.Canvas()))
	}
	setLayoutMode(false)
	debug.Logf("Application started")
	window.ShowAndRun()
}

func newAuthPanel(window fyne.Window) fyne.CanvasObject {
	statusLabel := widget.NewLabel("Auth: Disabled")
	statusDot := canvas.NewCircle(theme.Color(theme.ColorNameError))
	statusDotWrapper := container.NewCenter(container.NewGridWrap(fyne.NewSize(10, 10), statusDot))
	cookieEntry := widget.NewPasswordEntry()
	cookieEntry.SetPlaceHolder("Optional .ROBLOSECURITY cookie value")
	rememberAuthCheck := widget.NewCheck("Save to keychain", nil)
	isAuthSaved := false
	authValidationFailed := false
	applyAuthIndicator := func() {
		if authValidationFailed {
			statusLabel.SetText("Auth: Failed")
			statusDot.FillColor = theme.Color(theme.ColorNameError)
			statusDot.Refresh()
			return
		}
		if roblox.IsAuthenticationEnabled() {
			if isAuthSaved {
				statusLabel.SetText("Auth: Saved")
			} else {
				statusLabel.SetText("Auth: Enabled")
			}
			statusDot.FillColor = theme.Color(theme.ColorNameSuccess)
		} else {
			statusLabel.SetText("Auth: Disabled")
			statusDot.FillColor = theme.Color(theme.ColorNameError)
		}
		statusDot.Refresh()
	}
	helpButton := widget.NewButton("?", func() {
		dialog.ShowInformation(
			".ROBLOSECURITY Help",
			"How to get it:\n"+
				"1) Sign in at https://www.roblox.com in your browser.\n"+
				"2) Open browser developer tools.\n"+
				"3) Go to Storage/Application -> Cookies -> .roblox.com.\n"+
				"4) Copy the .ROBLOSECURITY cookie value.\n"+
				"5) Paste it here and click Apply Auth.\n\n"+
				"Security note: This cookie grants account access. Treat it like a password and do not share it.\n\n"+
				"When 'Remember Auth' is enabled, this app stores the cookie in your OS secure credential store "+
				"(for example, Keychain on macOS). It is not saved in plaintext project files.\n"+
				"Using Clear Auth removes the in-memory cookie and deletes the saved keychain credential.",
			window,
		)
	})
	helpButton.Resize(fyne.NewSize(32, 32))

	applyButton := widget.NewButton("Apply Auth", func() {
		authValidationFailed = false
		rawCookie := strings.TrimSpace(cookieEntry.Text)
		if rawCookie == "" {
			debug.Logf("Auth cookie cleared via Apply Auth")
			roblox.ClearRoblosecurityCookie()
			isAuthSaved = false
			_ = roblox.DeleteRoblosecurityCookieFromKeyring()
			rememberAuthCheck.SetChecked(false)
			applyAuthIndicator()
			return
		}

		validationErr := roblox.ValidateRoblosecurityCookie(rawCookie)
		if validationErr != nil {
			debug.Logf("Auth validation failed: %s", roblox.SanitizeAuthErrorMessage(validationErr))
			roblox.ClearRoblosecurityCookie()
			isAuthSaved = false
			authValidationFailed = true
			applyAuthIndicator()
			statusLabel.SetText(fmt.Sprintf("Auth: Failed (%s)", roblox.SanitizeAuthErrorMessage(validationErr)))
			return
		}

		roblox.SetRoblosecurityCookie(rawCookie)
		debug.Logf("Auth cookie applied successfully")
		isAuthSaved = false

		if rememberAuthCheck.Checked {
			if err := roblox.SaveRoblosecurityCookieToKeyring(rawCookie); err != nil {
				debug.Logf("Auth keychain save failed: %s", err.Error())
				applyAuthIndicator()
				statusLabel.SetText(fmt.Sprintf("Auth: Enabled (save failed: %s)", err.Error()))
				return
			}
			isAuthSaved = true
			debug.Logf("Auth cookie saved to secure keychain")
		} else {
			_ = roblox.DeleteRoblosecurityCookieFromKeyring()
		}

		applyAuthIndicator()
	})
	clearButton := widget.NewButton("Clear Auth", func() {
		debug.Logf("Auth cleared")
		roblox.ClearRoblosecurityCookie()
		authValidationFailed = false
		deleteErr := roblox.DeleteRoblosecurityCookieFromKeyring()
		if deleteErr == nil {
			isAuthSaved = false
		}
		cookieEntry.SetText("")
		applyAuthIndicator()
		if deleteErr != nil {
			statusLabel.SetText(fmt.Sprintf("Auth: Disabled (clear failed: %s)", deleteErr.Error()))
			return
		}
		rememberAuthCheck.SetChecked(false)
	})

	labeledEntry := container.NewBorder(
		nil,
		nil,
		widget.NewLabel("Auth Cookie:"),
		container.NewHBox(rememberAuthCheck),
		cookieEntry,
	)
	leftControls := container.NewBorder(
		nil,
		nil,
		nil,
		container.NewHBox(helpButton, applyButton, clearButton),
		labeledEntry,
	)
	rightStatus := container.NewHBox(statusDotWrapper, statusLabel)
	footerRow := container.NewBorder(nil, nil, nil, rightStatus, leftControls)

	loadErrorMessage := ""
	storedCookie, loadErr := roblox.LoadRoblosecurityCookieFromKeyring()
	if loadErr != nil {
		debug.Logf("Failed to load auth cookie from keychain: %s", loadErr.Error())
		loadErrorMessage = fmt.Sprintf("Auth: Disabled (load failed: %s)", loadErr.Error())
	} else if storedCookie != "" {
		debug.Logf("Loaded auth cookie from keychain")
		roblox.SetRoblosecurityCookie(storedCookie)
		cookieEntry.SetText(storedCookie)
		rememberAuthCheck.SetChecked(true)
		isAuthSaved = true
		authValidationFailed = false
	}
	applyAuthIndicator()
	if loadErrorMessage != "" {
		statusLabel.SetText(loadErrorMessage)
	}

	if storedCookie != "" {
		statusLabel.SetText("Auth: Validating...")
		// Defer until the Fyne driver loop is running, otherwise fyne.Do may
		// execute inline on the background goroutine and trip the 2.6 thread
		// check when touching widgets.
		fyne.CurrentApp().Lifecycle().SetOnStarted(func() {
			go func() {
				validationErr := roblox.ValidateRoblosecurityCookie(storedCookie)
				fyne.Do(func() {
					if validationErr != nil {
						debug.Logf("Startup auth validation failed: %s", roblox.SanitizeAuthErrorMessage(validationErr))
						authValidationFailed = true
						applyAuthIndicator()
						statusLabel.SetText(fmt.Sprintf("Auth: Expired (%s)", roblox.SanitizeAuthErrorMessage(validationErr)))
						dialog.ShowError(
							fmt.Errorf("your saved auth cookie is expired or invalid — please update it in the Auth panel below"),
							window,
						)
					} else {
						debug.Logf("Startup auth validation succeeded")
						authValidationFailed = false
						applyAuthIndicator()
					}
				})
			}()
		})
	}

	return container.NewVBox(
		widget.NewSeparator(),
		widget.NewCard("", "", footerRow),
	)
}
