package app

import (
	"fmt"
	"net/url"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

const (
	helpDocumentDialogWidth  = 760
	helpDocumentDialogHeight = 620
	aboutDialogWidth         = 540
	aboutDialogHeight        = 360
)

func buildHelpMenu(window fyne.Window) *fyne.Menu {
	return fyne.NewMenu(
		"Help",
		fyne.NewMenuItem("Changelog", func() {
			showChangelogDialog(window)
		}),
		fyne.NewMenuItem("About", func() {
			showAboutDialog(window)
		}),
	)
}

func showChangelogDialog(window fyne.Window) {
	showDocumentDialog(window, "Changelog", loadChangelogText())
}

func showLicenseDialog(window fyne.Window) {
	showDocumentDialog(window, "License", loadLicenseText())
}

func showDocumentDialog(window fyne.Window, title string, content string) {
	documentView := widget.NewTextGridFromString(content)
	scroll := container.NewVScroll(documentView)
	scroll.SetMinSize(fyne.NewSize(helpDocumentDialogWidth-40, helpDocumentDialogHeight-90))
	documentDialog := dialog.NewCustom(title, "Close", container.NewPadded(scroll), window)
	documentDialog.Resize(fyne.NewSize(helpDocumentDialogWidth, helpDocumentDialogHeight))
	documentDialog.Show()
}

func showAboutDialog(window fyne.Window) {
	titleLabel := widget.NewLabel(fmt.Sprintf("%s %s", appDisplayName, appVersion))
	titleLabel.TextStyle = fyne.TextStyle{Bold: true}

	authorLabel := widget.NewLabel(fmt.Sprintf("Author: %s", appAuthorName))
	versionLabel := widget.NewLabel(fmt.Sprintf("Version: %s", appVersion))
	licenseLabel := widget.NewLabel(fmt.Sprintf("License: %s", appLicenseName))
	noticeLabel := widget.NewLabel("This program is free software and comes with ABSOLUTELY NO WARRANTY.")
	noticeLabel.Wrapping = fyne.TextWrapWord

	var sourceObject fyne.CanvasObject = widget.NewLabel(fmt.Sprintf("Source: %s", appSourceURL))
	if parsedURL, err := url.Parse(appSourceURL); err == nil {
		sourceObject = container.NewHBox(
			widget.NewLabel("Source:"),
			widget.NewHyperlink(appSourceURL, parsedURL),
		)
	}

	buttonRow := container.NewHBox(
		widget.NewButton("View Changelog", func() {
			showChangelogDialog(window)
		}),
		widget.NewButton("View License", func() {
			showLicenseDialog(window)
		}),
	)

	content := container.NewPadded(container.NewVBox(
		titleLabel,
		authorLabel,
		versionLabel,
		licenseLabel,
		sourceObject,
		widget.NewSeparator(),
		noticeLabel,
		widget.NewSeparator(),
		buttonRow,
	))
	aboutDialog := dialog.NewCustom("About", "Close", content, window)
	aboutDialog.Resize(fyne.NewSize(aboutDialogWidth, aboutDialogHeight))
	aboutDialog.Show()
}
