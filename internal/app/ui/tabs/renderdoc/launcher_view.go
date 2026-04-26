package renderdoctab

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"joxblox/internal/renderdoc"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	fyneDialog "fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
	"github.com/fsnotify/fsnotify"
)

const (
	preferenceKeyRenderDocStudioPath = "renderdoc.studio_path"
	captureFileStem                  = "capture"
	captureListMinHeight             = 140
)

// captureEntry is one row in the captures list.
type captureEntry struct {
	Name    string
	Path    string
	ModTime time.Time
}

// capturesDirectory returns the per-app directory we tell renderdoccmd to
// write captures into via `-c`. Stable across launches so older captures
// remain reachable from the list and the "Open folder" button.
func capturesDirectory() string {
	return filepath.Join(os.TempDir(), "joxblox-renderdoc-captures")
}

// newLauncherRow builds the launcher row above the RenderDoc sub-tabs.
// loadCapture is invoked when the user clicks an entry in the captures list;
// it should load the given path into whichever sub-tab is active.
func newLauncherRow(window fyne.Window, loadCapture func(path string)) fyne.CanvasObject {
	studioLabel := widget.NewLabel(formatStudioVersionLabel(LoadStudioPath()))

	statusLabel := widget.NewLabel("Ready")
	statusLabel.Wrapping = fyne.TextWrapWord

	capturesHeader := widget.NewLabel("Captures: 0")

	var capturesMutex sync.Mutex
	var captures []captureEntry

	captureList := widget.NewList(
		func() int {
			capturesMutex.Lock()
			defer capturesMutex.Unlock()
			return len(captures)
		},
		func() fyne.CanvasObject {
			return widget.NewLabel("")
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			capturesMutex.Lock()
			defer capturesMutex.Unlock()
			if id < 0 || id >= len(captures) {
				return
			}
			label, ok := obj.(*widget.Label)
			if !ok {
				return
			}
			entry := captures[id]
			label.SetText(fmt.Sprintf("%s  ·  %s", entry.Name, entry.ModTime.Format("15:04:05")))
		},
	)
	captureList.OnSelected = func(id widget.ListItemID) {
		capturesMutex.Lock()
		var path string
		if id >= 0 && id < len(captures) {
			path = captures[id].Path
		}
		capturesMutex.Unlock()
		captureList.Unselect(id)
		if path != "" && loadCapture != nil {
			loadCapture(path)
		}
	}

	captureListScroll := container.NewVScroll(captureList)
	captureListScroll.SetMinSize(fyne.NewSize(0, captureListMinHeight))

	refreshList := func() {
		dir := capturesDirectory()
		entries, readErr := os.ReadDir(dir)
		var items []captureEntry
		if readErr == nil {
			for _, entry := range entries {
				if entry.IsDir() {
					continue
				}
				name := entry.Name()
				if !strings.HasSuffix(strings.ToLower(name), ".rdc") {
					continue
				}
				info, statErr := entry.Info()
				if statErr != nil {
					continue
				}
				items = append(items, captureEntry{
					Name:    name,
					Path:    filepath.Join(dir, name),
					ModTime: info.ModTime(),
				})
			}
		}
		sort.Slice(items, func(i, j int) bool {
			return items[i].ModTime.After(items[j].ModTime)
		})

		capturesMutex.Lock()
		captures = items
		count := len(captures)
		capturesMutex.Unlock()

		fyne.Do(func() {
			capturesHeader.SetText(fmt.Sprintf("Captures: %d", count))
			captureList.Refresh()
		})
	}

	openFolderButton := widget.NewButton("Open folder", func() {
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
				go func() {
					_ = cmd.Wait()
					fyne.Do(func() { statusLabel.SetText("Ready") })
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

	// Start the fsnotify watcher and run the initial scan once. The watcher
	// goroutine lives for the app's lifetime; the directory persists across
	// launches so older captures remain visible.
	startCaptureFolderWatcher(refreshList)

	topRow := container.NewBorder(nil, nil, studioLabel, launchButton, statusLabel)
	capturesHeaderRow := container.NewBorder(nil, nil, nil, openFolderButton, capturesHeader)
	return container.NewVBox(topRow, capturesHeaderRow, captureListScroll)
}

// startCaptureFolderWatcher launches an fsnotify watcher on the captures dir
// in a background goroutine. Each .rdc-relevant filesystem event triggers
// onChange. Runs an initial onChange call before entering the event loop so
// the list reflects existing files on first build.
func startCaptureFolderWatcher(onChange func()) {
	dir := capturesDirectory()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		// Without the directory we can't watch — leave the list empty and
		// rely on the user clicking Launch (which creates it) to re-init.
		return
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return
	}
	if addErr := watcher.Add(dir); addErr != nil {
		_ = watcher.Close()
		return
	}

	go func() {
		defer watcher.Close()
		onChange()
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if !strings.HasSuffix(strings.ToLower(event.Name), ".rdc") {
					continue
				}
				onChange()
			case _, ok := <-watcher.Errors:
				if !ok {
					return
				}
			}
		}
	}()
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
