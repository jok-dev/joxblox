package renderdoctab

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"joxblox/internal/renderdoc"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	fyneDialog "fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

const preferenceKeyRenderDocStudioPath = "renderdoc.studio_path"

// newLauncherRow builds the "Launch Studio with RenderDoc" row that mounts
// above the RenderDoc sub-tabs. The Studio path is configured via the app
// settings dialog; this row only displays its version and triggers a launch.
func newLauncherRow(window fyne.Window) fyne.CanvasObject {
	studioLabel := widget.NewLabel(formatStudioVersionLabel(LoadStudioPath()))

	statusLabel := widget.NewLabel("Ready")
	statusLabel.Wrapping = fyne.TextWrapWord

	var launchButton *widget.Button
	launchButton = widget.NewButton("Launch with RenderDoc", func() {
		studioPath := strings.TrimSpace(LoadStudioPath())
		studioLabel.SetText(formatStudioVersionLabel(studioPath))

		if studioPath == "" {
			fyneDialog.ShowError(errors.New("Studio path is not configured — set it in Settings or via the JOXBLOX_ROBLOX_STUDIO environment variable"), window)
			return
		}
		if _, err := os.Stat(studioPath); err != nil {
			fyneDialog.ShowError(fmt.Errorf("Studio executable not found at %q: %w", studioPath, err), window)
			return
		}

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
			// Reap the renderdoccmd process so we don't leave a zombie/handle slot,
			// and reset the status label once Studio exits so a stale PID doesn't linger.
			if cmd != nil {
				go func() {
					_ = cmd.Wait()
					fyne.Do(func() {
						statusLabel.SetText("Ready")
					})
				}()
			}
			time.Sleep(1 * time.Second)
			fyne.Do(func() {
				launchButton.Enable()
				// After an error the dialog is dismissed and the button is re-enabled;
				// clear the "Error" status so the label tracks the button.
				if err != nil {
					statusLabel.SetText("Ready")
				}
			})
		}()
	})

	return container.NewBorder(nil, nil,
		studioLabel,
		launchButton,
		statusLabel,
	)
}

// formatStudioVersionLabel renders the Studio path as a short, user-facing
// label. The version is the parent directory name of the executable, which on
// a standard install is "version-<hash>". Returns "Studio: not configured"
// when the path is empty.
func formatStudioVersionLabel(path string) string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return "Studio: not configured"
	}
	parent := filepath.Base(filepath.Dir(trimmed))
	if parent == "" || parent == "." || parent == string(filepath.Separator) {
		return fmt.Sprintf("Studio: %s", filepath.Base(trimmed))
	}
	return fmt.Sprintf("Studio: %s", parent)
}

// LoadStudioPath returns the Studio executable path to use. Resolution order:
// persisted Fyne preference (if the file exists) -> renderdoc.LocateRobloxStudio()
// auto-detection -> "". Exported so the settings dialog can pre-fill its entry.
func LoadStudioPath() string {
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

// SaveStudioPath persists the Studio path. Empty string clears the preference.
// No-op if Fyne's app instance is unavailable.
func SaveStudioPath(path string) {
	currentApp := fyne.CurrentApp()
	if currentApp == nil {
		return
	}
	currentApp.Preferences().SetString(preferenceKeyRenderDocStudioPath, strings.TrimSpace(path))
}
