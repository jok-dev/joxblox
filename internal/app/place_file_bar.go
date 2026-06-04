package app

import (
	"fmt"
	"path/filepath"
	"strings"

	"joxblox/internal/app/common"
	"joxblox/internal/debug"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

const placeFileBarEmptyText = "No place file loaded"

// buildPlaceFileBar creates the persistent place-file widget shown in the top
// header. Loading a file here prefills every RBXL-consuming tab (via the given
// prefill callbacks) so the same place file stays loaded as the user moves
// between tabs. It returns the bar plus a setter the drop handler can reuse to
// keep the bar in sync.
func buildPlaceFileBar(window fyne.Window, prefills []func(string)) (fyne.CanvasObject, func(string)) {
	fileLabel := widget.NewLabel(placeFileBarEmptyText)
	fileLabel.Wrapping = fyne.TextTruncate

	var clearButton *widget.Button

	setPath := func(path string) {
		trimmedPath := strings.TrimSpace(path)
		if trimmedPath == "" {
			fileLabel.SetText(placeFileBarEmptyText)
			clearButton.Disable()
		} else {
			fileLabel.SetText(filepath.Base(trimmedPath))
			clearButton.Enable()
			debug.Logf("Shared place file loaded: %s", trimmedPath)
		}
		for _, prefill := range prefills {
			if prefill != nil {
				prefill(trimmedPath)
			}
		}
	}

	loadButton := widget.NewButtonWithIcon("Load file…", theme.FolderOpenIcon(), func() {
		common.PickRBXLSource(window, setPath, func(err error) {
			dialog.ShowError(fmt.Errorf("file selection failed: %w", err), window)
		})
	})
	loadButton.Importance = widget.HighImportance

	clearButton = widget.NewButtonWithIcon("", theme.CancelIcon(), func() {
		setPath("")
	})
	clearButton.Disable()

	fileIcon := widget.NewIcon(theme.FileIcon())
	bar := container.NewBorder(
		nil,
		nil,
		container.NewHBox(fileIcon),
		container.NewHBox(loadButton, clearButton),
		fileLabel,
	)
	return bar, setPath
}
