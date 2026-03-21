package app

import (
	_ "embed"
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

const (
	previewWidth  = 360
	previewHeight = 240
	megabyte      = 1024 * 1024
)

//go:embed app_icon.svg
var appIconSVG []byte

func Run() {
	initializeDebugLogFile()
	guiApp := app.New()
	appIcon := fyne.NewStaticResource("app_icon.svg", appIconSVG)
	guiApp.SetIcon(appIcon)
	window := guiApp.NewWindow("Joxblox")
	window.SetIcon(appIcon)
	window.Resize(fyne.NewSize(1100, 700))

	singleAssetTab := container.NewTabItem("Single Asset", newSingleAssetTab(window))
	folderScanTab := container.NewTabItem("Folder Scan", newFolderScanTab(window))
	rbxlScanTab := container.NewTabItem("RBXL Scan", newRBXLScanTab(window))
	tabs := container.NewAppTabs(singleAssetTab, folderScanTab, rbxlScanTab)
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
			window.SetContent(resizableLayout)
			return
		}
		window.SetContent(collapsedLayout)
	}
	setLayoutMode(false)
	logDebugf("Application started")
	window.ShowAndRun()
}

func newAuthPanel(window fyne.Window) fyne.CanvasObject {
	statusLabel := widget.NewLabel("Auth: Disabled")
	statusDot := canvas.NewCircle(theme.Color(theme.ColorNameError))
	statusDotWrapper := container.NewCenter(container.NewGridWrap(fyne.NewSize(10, 10), statusDot))
	cookieEntry := widget.NewPasswordEntry()
	cookieEntry.SetPlaceHolder("Optional .ROBLOSECURITY cookie value")
	rememberAuthCheck := widget.NewCheck("Remember Auth (secure OS keychain)", nil)
	isAuthSaved := false
	authValidationFailed := false
	updateAuthIndicator := func() {
		if authValidationFailed {
			statusLabel.SetText("Auth: Failed")
			statusDot.FillColor = theme.Color(theme.ColorNameError)
			statusDot.Refresh()
			return
		}
		if IsAuthenticationEnabled() {
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
		normalizedCookie := normalizeRoblosecurityCookie(cookieEntry.Text)
		if normalizedCookie == "" {
			logDebugf("Auth cookie cleared via Apply Auth")
			ClearRoblosecurityCookie()
			isAuthSaved = false
			rememberAuthCheck.SetChecked(false)
			_ = DeleteRoblosecurityCookieFromKeyring()
			updateAuthIndicator()
			return
		}

		validationErr := validateRoblosecurityCookie(normalizedCookie)
		if validationErr != nil {
			logDebugf("Auth validation failed: %s", sanitizeAuthErrorMessage(validationErr))
			ClearRoblosecurityCookie()
			isAuthSaved = false
			authValidationFailed = true
			updateAuthIndicator()
			statusLabel.SetText(fmt.Sprintf("Auth: Failed (%s)", sanitizeAuthErrorMessage(validationErr)))
			return
		}

		SetRoblosecurityCookie(normalizedCookie)
		logDebugf("Auth cookie applied successfully")
		isAuthSaved = false

		if rememberAuthCheck.Checked {
			if err := SaveRoblosecurityCookieToKeyring(normalizedCookie); err != nil {
				logDebugf("Auth keychain save failed: %s", err.Error())
				updateAuthIndicator()
				statusLabel.SetText(fmt.Sprintf("Auth: Enabled (save failed: %s)", err.Error()))
				return
			}
			isAuthSaved = true
			logDebugf("Auth cookie saved to secure keychain")
		} else {
			_ = DeleteRoblosecurityCookieFromKeyring()
		}

		updateAuthIndicator()
	})
	clearButton := widget.NewButton("Clear Auth", func() {
		logDebugf("Auth cleared")
		ClearRoblosecurityCookie()
		cookieEntry.SetText("")
		authValidationFailed = false
		if err := DeleteRoblosecurityCookieFromKeyring(); err != nil {
			updateAuthIndicator()
			statusLabel.SetText(fmt.Sprintf("Auth: Disabled (clear failed: %s)", err.Error()))
			return
		}
		isAuthSaved = false
		updateAuthIndicator()
		rememberAuthCheck.SetChecked(false)
	})

	labeledEntry := container.NewBorder(nil, nil, widget.NewLabel("Auth Cookie:"), nil, cookieEntry)
	leftControls := container.NewBorder(
		nil,
		nil,
		nil,
		container.NewHBox(helpButton, applyButton, clearButton),
		container.NewVBox(labeledEntry, rememberAuthCheck),
	)
	rightStatus := container.NewHBox(statusDotWrapper, statusLabel)
	footerRow := container.NewBorder(nil, nil, nil, rightStatus, leftControls)

	loadErrorMessage := ""
	storedCookie, loadErr := LoadRoblosecurityCookieFromKeyring()
	if loadErr != nil {
		logDebugf("Failed to load auth cookie from keychain: %s", loadErr.Error())
		loadErrorMessage = fmt.Sprintf("Auth: Disabled (load failed: %s)", loadErr.Error())
	} else if storedCookie != "" {
		logDebugf("Loaded auth cookie from keychain")
		SetRoblosecurityCookie(storedCookie)
		cookieEntry.SetText(storedCookie)
		rememberAuthCheck.SetChecked(true)
		isAuthSaved = true
		authValidationFailed = false
	}
	updateAuthIndicator()
	if loadErrorMessage != "" {
		statusLabel.SetText(loadErrorMessage)
	}

	return container.NewVBox(
		widget.NewSeparator(),
		widget.NewCard("", "", footerRow),
	)
}
