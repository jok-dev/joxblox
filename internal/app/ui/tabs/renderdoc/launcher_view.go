package renderdoctab

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"joxblox/internal/renderdoc"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	fyneDialog "fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

const (
	preferenceKeyRenderDocStudioPath = "renderdoc.studio_path"
	captureFileStem                  = "capture"
)

// capturesDirectory returns the per-app directory we tell renderdoccmd to
// write captures into via `-c`. Stable across launches so the user can find
// older captures without hunting around.
func capturesDirectory() string {
	return filepath.Join(os.TempDir(), "joxblox-renderdoc-captures")
}

// newLauncherRow builds the "Launch Studio with RenderDoc" row that mounts
// above the RenderDoc sub-tabs. The Studio path is configured via the app
// settings dialog; this row only displays the version, triggers a launch,
// and shows captures observed during the running Studio session.
func newLauncherRow(window fyne.Window) fyne.CanvasObject {
	studioLabel := widget.NewLabel(formatStudioVersionLabel(LoadStudioPath()))

	statusLabel := widget.NewLabel("Ready")
	statusLabel.Wrapping = fyne.TextWrapWord

	capturesLabel := widget.NewLabel(formatCapturesLabel(0, ""))
	capturesLabel.Wrapping = fyne.TextWrapWord

	openFolderButton := widget.NewButton("Open captures folder", func() {
		dir := capturesDirectory()
		if err := os.MkdirAll(dir, 0o755); err != nil {
			fyneDialog.ShowError(err, window)
			return
		}
		if err := exec.Command("explorer", dir).Start(); err != nil {
			fyneDialog.ShowError(fmt.Errorf("open captures folder: %w", err), window)
		}
	})

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

		dir := capturesDirectory()
		if err := os.MkdirAll(dir, 0o755); err != nil {
			fyneDialog.ShowError(fmt.Errorf("create captures dir: %w", err), window)
			return
		}
		captureTemplate := filepath.Join(dir, captureFileStem)

		launchButton.Disable()
		statusLabel.SetText("Launching…")
		capturesLabel.SetText(formatCapturesLabel(0, ""))

		launchTime := time.Now()

		go func() {
			cmd, err := renderdoc.LaunchStudioWithRenderDoc(studioPath, captureTemplate)
			fyne.Do(func() {
				if err != nil {
					statusLabel.SetText("Error")
					fyneDialog.ShowError(err, window)
				} else {
					statusLabel.SetText(fmt.Sprintf("Launched (PID %d)", cmd.Process.Pid))
				}
			})

			if cmd != nil {
				done := make(chan struct{})
				go runCaptureWatcher(dir, launchTime, done, capturesLabel)
				go func() {
					_ = cmd.Wait()
					close(done)
					fyne.Do(func() {
						statusLabel.SetText("Ready")
					})
				}()
			}

			time.Sleep(1 * time.Second)
			fyne.Do(func() {
				launchButton.Enable()
				if err != nil {
					statusLabel.SetText("Ready")
				}
			})
		}()
	})

	topRow := container.NewBorder(nil, nil, studioLabel, launchButton, statusLabel)
	bottomRow := container.NewBorder(nil, nil, nil, openFolderButton, capturesLabel)
	return container.NewVBox(topRow, bottomRow)
}

// runCaptureWatcher polls the captures directory every second while Studio is
// running. New `.rdc` files (mtime >= launchTime) update the captures label.
// Stops when the done channel is closed.
func runCaptureWatcher(dir string, since time.Time, done <-chan struct{}, label *widget.Label) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	seen := make(map[string]struct{})
	count := 0
	last := ""
	// Round down by 1s to absorb filesystem mtime granularity differences.
	threshold := since.Add(-1 * time.Second)

	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			entries, err := os.ReadDir(dir)
			if err != nil {
				continue
			}
			changed := false
			for _, entry := range entries {
				if entry.IsDir() {
					continue
				}
				name := entry.Name()
				if !strings.HasSuffix(strings.ToLower(name), ".rdc") {
					continue
				}
				if _, ok := seen[name]; ok {
					continue
				}
				info, infoErr := entry.Info()
				if infoErr != nil {
					continue
				}
				if info.ModTime().Before(threshold) {
					continue
				}
				seen[name] = struct{}{}
				count++
				last = name
				changed = true
			}
			if changed {
				snapshotCount := count
				snapshotLast := last
				fyne.Do(func() {
					label.SetText(formatCapturesLabel(snapshotCount, snapshotLast))
				})
			}
		}
	}
}

func formatCapturesLabel(count int, last string) string {
	if count == 0 {
		return "Captures: 0"
	}
	if last == "" {
		return fmt.Sprintf("Captures: %d", count)
	}
	return fmt.Sprintf("Captures: %d — last: %s", count, last)
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
