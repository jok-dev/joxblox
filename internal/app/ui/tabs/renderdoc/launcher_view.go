package renderdoctab

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"joxblox/internal/renderdoc"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	fyneDialog "fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
	nativeDialog "github.com/sqweek/dialog"
)

const preferenceKeyRenderDocStudioPath = "renderdoc.studio_path"

// newLauncherRow builds the "Launch Studio with RenderDoc" row that mounts
// above the RenderDoc sub-tabs. The window is used to parent dialogs.
func newLauncherRow(window fyne.Window) fyne.CanvasObject {
	pathEntry := widget.NewEntry()
	pathEntry.SetPlaceHolder("Browse for RobloxStudioBeta.exe")
	pathEntry.SetText(initialStudioPath())

	statusLabel := widget.NewLabel("Ready")
	statusLabel.Wrapping = fyne.TextWrapWord

	browseButton := widget.NewButton("Browse…", func() {
		picked, err := nativeDialog.File().
			Filter("Roblox Studio executable", "exe").
			Title("Select RobloxStudioBeta.exe").
			Load()
		if err != nil {
			if errors.Is(err, nativeDialog.Cancelled) {
				return
			}
			fyneDialog.ShowError(err, window)
			return
		}
		pathEntry.SetText(picked)
	})

	var launchButton *widget.Button
	launchButton = widget.NewButton("Launch with RenderDoc", func() {
		studioPath := strings.TrimSpace(pathEntry.Text)
		if studioPath == "" {
			fyneDialog.ShowError(errors.New("Studio path is empty — set JOXBLOX_ROBLOX_STUDIO or pick a path with Browse"), window)
			return
		}
		if _, err := os.Stat(studioPath); err != nil {
			fyneDialog.ShowError(fmt.Errorf("Studio executable not found at %q: %w", studioPath, err), window)
			return
		}

		persistStudioPath(studioPath)
		launchButton.Disable()
		statusLabel.SetText("Launching…")

		go func() {
			cmd, err := renderdoc.LaunchStudioWithRenderDoc(studioPath)
			fyne.Do(func() {
				if err != nil {
					statusLabel.SetText("Error")
					fyneDialog.ShowError(err, window)
				} else {
					statusLabel.SetText(fmt.Sprintf("Launched (PID %d)", cmd.Process.Pid))
				}
			})
			// Reap the renderdoccmd process in the background so we don't leave
			// a zombie/handle slot. Studio exit is observed but doesn't drive UI.
			if cmd != nil {
				go func() { _ = cmd.Wait() }()
			}
			time.Sleep(1 * time.Second)
			fyne.Do(func() {
				launchButton.Enable()
			})
		}()
	})

	pathRow := container.NewBorder(nil, nil,
		widget.NewLabel("Studio:"),
		browseButton,
		pathEntry,
	)
	return container.NewVBox(
		pathRow,
		container.NewBorder(nil, nil, nil, launchButton, statusLabel),
	)
}

// initialStudioPath resolves the path to show in the entry on first build.
// Order: persisted preference (if file exists) -> renderdoc.LocateRobloxStudio() -> "".
func initialStudioPath() string {
	if currentApp := fyne.CurrentApp(); currentApp != nil {
		stored := strings.TrimSpace(currentApp.Preferences().String(preferenceKeyRenderDocStudioPath))
		if stored != "" {
			if _, err := os.Stat(stored); err == nil {
				return stored
			}
		}
	}
	if detected, err := renderdoc.LocateRobloxStudio(); err == nil {
		return detected
	}
	return ""
}

func persistStudioPath(path string) {
	currentApp := fyne.CurrentApp()
	if currentApp == nil {
		return
	}
	currentApp.Preferences().SetString(preferenceKeyRenderDocStudioPath, path)
}
