package menu

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

// AboutMeta is static application identity text shown in the About dialog.
type AboutMeta struct {
	DisplayName string
	Version     string
	Author      string
	LicenseName string
	SourceURL   string
}

func BuildHelpMenu(window fyne.Window, meta AboutMeta, changelogText, licenseText func() string) *fyne.Menu {
	return fyne.NewMenu(
		"Help",
		fyne.NewMenuItem("Changelog", func() {
			text := ""
			if changelogText != nil {
				text = changelogText()
			}
			showDocumentDialog(window, "Changelog", text)
		}),
		fyne.NewMenuItem("About", func() {
			showAboutDialog(window, meta, changelogText, licenseText)
		}),
	)
}

func showLicenseDialog(window fyne.Window, licenseText func() string) {
	text := ""
	if licenseText != nil {
		text = licenseText()
	}
	showDocumentDialog(window, "License", text)
}

func showDocumentDialog(window fyne.Window, title string, content string) {
	documentView := widget.NewTextGridFromString(content)
	scroll := container.NewVScroll(documentView)
	scroll.SetMinSize(fyne.NewSize(helpDocumentDialogWidth-40, helpDocumentDialogHeight-90))
	documentDialog := dialog.NewCustom(title, "Close", container.NewPadded(scroll), window)
	documentDialog.Resize(fyne.NewSize(helpDocumentDialogWidth, helpDocumentDialogHeight))
	documentDialog.Show()
}

func showAboutDialog(window fyne.Window, meta AboutMeta, changelogText, licenseText func() string) {
	titleLabel := widget.NewLabel(fmt.Sprintf("%s %s", meta.DisplayName, meta.Version))
	titleLabel.TextStyle = fyne.TextStyle{Bold: true}

	authorLabel := widget.NewLabel(fmt.Sprintf("Author: %s", meta.Author))
	versionLabel := widget.NewLabel(fmt.Sprintf("Version: %s", meta.Version))
	licenseLabel := widget.NewLabel(fmt.Sprintf("License: %s", meta.LicenseName))
	noticeLabel := widget.NewLabel("This program is free software and comes with ABSOLUTELY NO WARRANTY.")
	noticeLabel.Wrapping = fyne.TextWrapWord

	var sourceObject fyne.CanvasObject = widget.NewLabel(fmt.Sprintf("Source: %s", meta.SourceURL))
	if parsedURL, err := url.Parse(meta.SourceURL); err == nil {
		sourceObject = container.NewHBox(
			widget.NewLabel("Source:"),
			widget.NewHyperlink(meta.SourceURL, parsedURL),
		)
	}

	buttonRow := container.NewHBox(
		widget.NewButton("View Changelog", func() {
			changelog := ""
			if changelogText != nil {
				changelog = changelogText()
			}
			showDocumentDialog(window, "Changelog", changelog)
		}),
		widget.NewButton("View License", func() {
			showLicenseDialog(window, licenseText)
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
