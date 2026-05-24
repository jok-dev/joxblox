package app

import (
	"fmt"
	"strings"

	"joxblox/internal/debug"
	"joxblox/internal/roblox"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// authController owns the persistent .ROBLOSECURITY widgets and auth state.
// It is constructed once at startup so the auth status can be reflected in the
// top header while the editable controls live inside the Settings dialog.
type authController struct {
	window fyne.Window

	cookieEntry       *widget.Entry
	rememberAuthCheck *widget.Check

	// Header indicator (always visible in the top bar).
	headerDot   *canvas.Circle
	headerLabel *widget.Label
	// Settings-dialog status label (mirrors the header status text).
	dialogStatusLabel *widget.Label

	isAuthSaved          bool
	authValidationFailed bool
}

func newAuthController(window fyne.Window) *authController {
	controller := &authController{
		window:            window,
		cookieEntry:       widget.NewPasswordEntry(),
		rememberAuthCheck: widget.NewCheck("Save to keychain", nil),
		headerDot:         canvas.NewCircle(theme.Color(theme.ColorNameError)),
		headerLabel:       widget.NewLabel("Auth: Disabled"),
		dialogStatusLabel: widget.NewLabel(""),
	}
	controller.cookieEntry.SetPlaceHolder("Optional .ROBLOSECURITY cookie value")
	controller.dialogStatusLabel.Wrapping = fyne.TextWrapWord

	controller.loadStoredCookie()
	return controller
}

// applyIndicator refreshes both the header and dialog status text to match the
// current auth state.
func (c *authController) applyIndicator() {
	statusText := "Auth: Disabled"
	dotColor := theme.Color(theme.ColorNameError)
	switch {
	case c.authValidationFailed:
		statusText = "Auth: Failed"
	case roblox.IsAuthenticationEnabled():
		if c.isAuthSaved {
			statusText = "Auth: Saved"
		} else {
			statusText = "Auth: Enabled"
		}
		dotColor = theme.Color(theme.ColorNameSuccess)
	}
	c.setStatusText(statusText)
	c.headerDot.FillColor = dotColor
	c.headerDot.Refresh()
}

func (c *authController) setStatusText(text string) {
	c.headerLabel.SetText(text)
	c.dialogStatusLabel.SetText(text)
}

// statusObject returns the compact status indicator for the top header.
func (c *authController) statusObject() fyne.CanvasObject {
	dotWrapper := container.NewCenter(container.NewGridWrap(fyne.NewSize(10, 10), c.headerDot))
	return container.NewHBox(dotWrapper, c.headerLabel)
}

// settingsSection returns the editable auth controls for embedding in the
// Settings dialog.
func (c *authController) settingsSection() fyne.CanvasObject {
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
				"When 'Save to keychain' is enabled, this app stores the cookie in your OS secure credential store "+
				"(for example, Keychain on macOS). It is not saved in plaintext project files.\n"+
				"Using Clear Auth removes the in-memory cookie and deletes the saved keychain credential.",
			c.window,
		)
	})

	applyButton := widget.NewButton("Apply Auth", c.applyAuth)
	applyButton.Importance = widget.HighImportance
	clearButton := widget.NewButton("Clear Auth", c.clearAuth)

	labeledEntry := container.NewBorder(
		nil,
		nil,
		widget.NewLabel("Auth Cookie:"),
		container.NewHBox(c.rememberAuthCheck),
		c.cookieEntry,
	)
	buttonRow := container.NewHBox(helpButton, applyButton, clearButton)

	return container.NewVBox(
		labeledEntry,
		buttonRow,
		c.dialogStatusLabel,
	)
}

func (c *authController) applyAuth() {
	c.authValidationFailed = false
	rawCookie := strings.TrimSpace(c.cookieEntry.Text)
	if rawCookie == "" {
		debug.Logf("Auth cookie cleared via Apply Auth")
		roblox.ClearRoblosecurityCookie()
		c.isAuthSaved = false
		_ = roblox.DeleteRoblosecurityCookieFromKeyring()
		c.rememberAuthCheck.SetChecked(false)
		c.applyIndicator()
		return
	}

	validationErr := roblox.ValidateRoblosecurityCookie(rawCookie)
	if validationErr != nil {
		debug.Logf("Auth validation failed: %s", roblox.SanitizeAuthErrorMessage(validationErr))
		roblox.ClearRoblosecurityCookie()
		c.isAuthSaved = false
		c.authValidationFailed = true
		c.applyIndicator()
		c.setStatusText(fmt.Sprintf("Auth: Failed (%s)", roblox.SanitizeAuthErrorMessage(validationErr)))
		return
	}

	roblox.SetRoblosecurityCookie(rawCookie)
	debug.Logf("Auth cookie applied successfully")
	c.isAuthSaved = false

	if c.rememberAuthCheck.Checked {
		if err := roblox.SaveRoblosecurityCookieToKeyring(rawCookie); err != nil {
			debug.Logf("Auth keychain save failed: %s", err.Error())
			c.applyIndicator()
			c.setStatusText(fmt.Sprintf("Auth: Enabled (save failed: %s)", err.Error()))
			return
		}
		c.isAuthSaved = true
		debug.Logf("Auth cookie saved to secure keychain")
	} else {
		_ = roblox.DeleteRoblosecurityCookieFromKeyring()
	}

	c.applyIndicator()
}

func (c *authController) clearAuth() {
	debug.Logf("Auth cleared")
	roblox.ClearRoblosecurityCookie()
	c.authValidationFailed = false
	deleteErr := roblox.DeleteRoblosecurityCookieFromKeyring()
	if deleteErr == nil {
		c.isAuthSaved = false
	}
	c.cookieEntry.SetText("")
	c.applyIndicator()
	if deleteErr != nil {
		c.setStatusText(fmt.Sprintf("Auth: Disabled (clear failed: %s)", deleteErr.Error()))
		return
	}
	c.rememberAuthCheck.SetChecked(false)
}

func (c *authController) loadStoredCookie() {
	loadErrorMessage := ""
	storedCookie, loadErr := roblox.LoadRoblosecurityCookieFromKeyring()
	if loadErr != nil {
		debug.Logf("Failed to load auth cookie from keychain: %s", loadErr.Error())
		loadErrorMessage = fmt.Sprintf("Auth: Disabled (load failed: %s)", loadErr.Error())
	} else if storedCookie != "" {
		debug.Logf("Loaded auth cookie from keychain")
		roblox.SetRoblosecurityCookie(storedCookie)
		c.cookieEntry.SetText(storedCookie)
		c.rememberAuthCheck.SetChecked(true)
		c.isAuthSaved = true
		c.authValidationFailed = false
	}
	c.applyIndicator()
	if loadErrorMessage != "" {
		c.setStatusText(loadErrorMessage)
	}

	if storedCookie == "" {
		return
	}

	c.setStatusText("Auth: Validating...")
	// Defer until the Fyne driver loop is running, otherwise fyne.Do may
	// execute inline on the background goroutine and trip the 2.6 thread
	// check when touching widgets.
	fyne.CurrentApp().Lifecycle().SetOnStarted(func() {
		go func() {
			validationErr := roblox.ValidateRoblosecurityCookie(storedCookie)
			fyne.Do(func() {
				if validationErr != nil {
					debug.Logf("Startup auth validation failed: %s", roblox.SanitizeAuthErrorMessage(validationErr))
					c.authValidationFailed = true
					c.applyIndicator()
					c.setStatusText(fmt.Sprintf("Auth: Expired (%s)", roblox.SanitizeAuthErrorMessage(validationErr)))
					dialog.ShowError(
						fmt.Errorf("your saved auth cookie is expired or invalid — please update it in Settings → Authentication"),
						c.window,
					)
				} else {
					debug.Logf("Startup auth validation succeeded")
					c.authValidationFailed = false
					c.applyIndicator()
				}
			})
		}()
	})
}
