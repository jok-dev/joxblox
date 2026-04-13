package app

import (
	"strings"

	"joxblox/internal/format"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

const progressDialogWidth = 420

type progressDialog struct {
	dialog   dialog.Dialog
	label    *widget.Label
	progress *widget.ProgressBar
	lastText string
	lastStep int
}

func newProgressDialog(window fyne.Window, title string, message string) *progressDialog {
	progressBar := widget.NewProgressBar()
	progressBar.SetValue(0)
	messageLabel := widget.NewLabel(strings.TrimSpace(message))
	messageLabel.Wrapping = fyne.TextWrapWord
	content := container.NewVBox(messageLabel, progressBar)
	progress := &progressDialog{
		dialog:   dialog.NewCustomWithoutButtons(title, content, window),
		label:    messageLabel,
		progress: progressBar,
		lastText: strings.TrimSpace(message),
		lastStep: 0,
	}
	progress.dialog.Resize(fyne.NewSize(progressDialogWidth, content.MinSize().Height))
	progress.dialog.Show()
	return progress
}

func (progress *progressDialog) Update(value float64, message string) {
	if progress == nil {
		return
	}
	nextMessage := strings.TrimSpace(message)
	if nextMessage == "" {
		nextMessage = progress.lastText
	}
	nextValue := format.Clamp(value, 0.0, 1.0)
	nextStep := int(nextValue * 1000)
	if nextMessage == progress.lastText && nextStep == progress.lastStep {
		return
	}
	progress.lastText = nextMessage
	progress.lastStep = nextStep
	fyne.Do(func() {
		progress.label.SetText(nextMessage)
		progress.progress.SetValue(nextValue)
	})
}

func (progress *progressDialog) Hide() {
	if progress == nil {
		return
	}
	fyne.Do(func() {
		progress.dialog.Hide()
	})
}

func progressRangeReporter(progress *progressDialog, start float64, end float64, message string) func(float64) {
	return func(value float64) {
		if progress == nil {
			return
		}
		clampedValue := format.Clamp(value, 0.0, 1.0)
		progress.Update(start+((end-start)*clampedValue), message)
	}
}
