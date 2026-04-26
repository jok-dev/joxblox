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
	captureListMinHeight             = 80
	loadedIndicator                  = "● "
	notLoadedIndicator               = "   "
)

// captureEntry is one row in the captures list.
type captureEntry struct {
	Name    string
	Path    string
	ModTime time.Time
}

// capturesRootDirectory is the parent of every per-session captures dir.
// Persistent so older sessions remain reachable from the file system.
func capturesRootDirectory() string {
	return filepath.Join(os.TempDir(), "joxblox-renderdoc-captures")
}

// launcher owns the launcher row and exposes a small control surface so
// NewRenderDocTab can notify it about sub-tab loads and tab changes without
// the tab knowing the launcher's internal state.
type launcher struct {
	canvas fyne.CanvasObject

	// sessionDir is the per-app-launch directory we tell renderdoccmd to
	// write captures into via `-c`. Created once when the launcher is
	// constructed; old sessions' subdirs stay around under capturesRoot.
	sessionDir string

	mu              sync.Mutex
	captures        []captureEntry
	loadedBySubTab  map[int]string
	activeSubTabIdx int

	header        *widget.Label
	list          *widget.List
	refreshLister func()
}

// setLoaded records that the given sub-tab successfully loaded the given
// capture path. The list re-renders so the loaded indicator follows.
func (l *launcher) setLoaded(subTabIdx int, path string) {
	l.mu.Lock()
	l.loadedBySubTab[subTabIdx] = path
	l.mu.Unlock()
	fyne.Do(func() {
		l.list.Refresh()
	})
}

// setActiveSubTab updates which sub-tab the launcher considers "active" so
// the loaded indicator reflects that sub-tab's capture.
func (l *launcher) setActiveSubTab(idx int) {
	l.mu.Lock()
	l.activeSubTabIdx = idx
	l.mu.Unlock()
	fyne.Do(func() {
		l.list.Refresh()
	})
}

// activeLoadedPath returns the path currently loaded in the active sub-tab,
// or "" if nothing is loaded there.
func (l *launcher) activeLoadedPath() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.loadedBySubTab[l.activeSubTabIdx]
}

// newLauncher builds the launcher row. loadCapture is invoked when the user
// clicks Load on a list row; it should load the given path into whichever
// sub-tab is active.
func newLauncher(window fyne.Window, loadCapture func(path string)) *launcher {
	sessionDir := newSessionCapturesDir()

	l := &launcher{
		sessionDir:     sessionDir,
		loadedBySubTab: make(map[int]string),
	}

	studioLabel := widget.NewLabel(formatStudioVersionLabel(LoadStudioPath()))

	statusLabel := widget.NewLabel("Ready")
	statusLabel.Wrapping = fyne.TextWrapWord

	l.header = widget.NewLabel("Captures: 0")

	l.list = widget.NewList(
		func() int {
			l.mu.Lock()
			defer l.mu.Unlock()
			return len(l.captures)
		},
		func() fyne.CanvasObject {
			name := widget.NewLabel("")
			loadBtn := widget.NewButton("Load", nil)
			renameBtn := widget.NewButton("Rename", nil)
			return container.NewBorder(nil, nil, nil,
				container.NewHBox(loadBtn, renameBtn),
				name,
			)
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			row, ok := obj.(*fyne.Container)
			if !ok || len(row.Objects) < 2 {
				return
			}
			name, ok := row.Objects[0].(*widget.Label)
			if !ok {
				return
			}
			rightBox, ok := row.Objects[1].(*fyne.Container)
			if !ok || len(rightBox.Objects) < 2 {
				return
			}
			loadBtn := rightBox.Objects[0].(*widget.Button)
			renameBtn := rightBox.Objects[1].(*widget.Button)

			l.mu.Lock()
			if id < 0 || id >= len(l.captures) {
				l.mu.Unlock()
				return
			}
			entry := l.captures[id]
			loadedHere := l.loadedBySubTab[l.activeSubTabIdx] == entry.Path
			l.mu.Unlock()

			prefix := notLoadedIndicator
			if loadedHere {
				prefix = loadedIndicator
			}
			name.SetText(fmt.Sprintf("%s%s  ·  %s", prefix, entry.Name, entry.ModTime.Format("15:04:05")))

			loadBtn.OnTapped = func() {
				if loadCapture != nil {
					loadCapture(entry.Path)
				}
			}
			renameBtn.OnTapped = func() {
				promptRename(window, entry, l.refreshLister)
			}
		},
	)
	l.list.OnSelected = func(id widget.ListItemID) {
		// We use explicit Load/Rename buttons; clear the visual selection
		// so the row isn't permanently highlighted.
		l.list.Unselect(id)
	}

	captureListScroll := container.NewVScroll(l.list)
	captureListScroll.SetMinSize(fyne.NewSize(0, captureListMinHeight))

	l.refreshLister = func() {
		l.refreshCaptures()
	}

	openFolderButton := widget.NewButton("Open folder", func() {
		if err := os.MkdirAll(l.sessionDir, 0o755); err != nil {
			fyneDialog.ShowError(err, window)
			return
		}
		if err := exec.Command("explorer", l.sessionDir).Start(); err != nil {
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

		if err := os.MkdirAll(l.sessionDir, 0o755); err != nil {
			fyneDialog.ShowError(fmt.Errorf("create captures dir: %w", err), window)
			return
		}
		captureTemplate := filepath.Join(l.sessionDir, captureFileStem)

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

	startCaptureFolderWatcher(l.sessionDir, l.refreshLister)

	topRow := container.NewBorder(nil, nil, studioLabel, launchButton, statusLabel)
	capturesHeaderRow := container.NewBorder(nil, nil, nil, openFolderButton, l.header)
	l.canvas = container.NewVBox(topRow, capturesHeaderRow, captureListScroll)
	return l
}

// newSessionCapturesDir creates a fresh per-app-launch subdir under
// capturesRoot. Subdir name is timestamp-based so it's human-readable when
// the user opens the folder.
func newSessionCapturesDir() string {
	root := capturesRootDirectory()
	stamp := time.Now().Format("20060102-150405")
	dir := filepath.Join(root, "session-"+stamp)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return root
	}
	return dir
}

// refreshCaptures re-scans the session captures dir, sorts newest-first, and
// refreshes the list. Safe to call from any goroutine — UI work is queued via
// fyne.Do.
func (l *launcher) refreshCaptures() {
	entries, readErr := os.ReadDir(l.sessionDir)
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
				Path:    filepath.Join(l.sessionDir, name),
				ModTime: info.ModTime(),
			})
		}
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].ModTime.After(items[j].ModTime)
	})

	l.mu.Lock()
	l.captures = items
	count := len(l.captures)
	l.mu.Unlock()

	fyne.Do(func() {
		l.header.SetText(fmt.Sprintf("Captures: %d", count))
		l.list.Refresh()
	})
}

// startCaptureFolderWatcher launches an fsnotify watcher on the given dir
// in a background goroutine. Each .rdc-relevant filesystem event triggers
// onChange. Runs an initial onChange call before entering the event loop.
func startCaptureFolderWatcher(dir string, onChange func()) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
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

// promptRename opens a dialog for renaming a capture. On confirm, the .rdc
// extension is preserved and the file is renamed in place. fsnotify events
// from the rename trigger a list refresh; we also call onDone explicitly so
// the user sees the result without waiting on the watcher.
func promptRename(window fyne.Window, entry captureEntry, onDone func()) {
	currentBase := strings.TrimSuffix(entry.Name, filepath.Ext(entry.Name))
	input := widget.NewEntry()
	input.SetText(currentBase)
	input.SetPlaceHolder("New name (without .rdc)")

	form := container.NewVBox(
		widget.NewLabel(fmt.Sprintf("Renaming: %s", entry.Name)),
		input,
	)

	d := fyneDialog.NewCustomConfirm("Rename capture", "Rename", "Cancel", form,
		func(confirmed bool) {
			if !confirmed {
				return
			}
			newBase := strings.TrimSpace(input.Text)
			newBase = strings.TrimSuffix(newBase, ".rdc")
			if newBase == "" {
				fyneDialog.ShowError(errors.New("name cannot be empty"), window)
				return
			}
			if strings.ContainsAny(newBase, `\/:*?"<>|`) {
				fyneDialog.ShowError(errors.New(`name contains invalid characters (\ / : * ? " < > |)`), window)
				return
			}
			newPath := filepath.Join(filepath.Dir(entry.Path), newBase+".rdc")
			if newPath == entry.Path {
				return
			}
			if _, err := os.Stat(newPath); err == nil {
				fyneDialog.ShowError(fmt.Errorf("a capture named %q already exists", newBase+".rdc"), window)
				return
			}
			if err := os.Rename(entry.Path, newPath); err != nil {
				fyneDialog.ShowError(fmt.Errorf("rename: %w", err), window)
				return
			}
			if onDone != nil {
				onDone()
			}
		}, window)
	d.Show()
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
