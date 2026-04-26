# RenderDoc Launch Studio Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a "Launch Studio with RenderDoc" button row above the existing RenderDoc sub-tabs that spawns Roblox Studio with `renderdoccmd capture`.

**Architecture:** Pure logic for studio-path discovery and launch in `internal/renderdoc/launch.go`. Fyne row in `internal/app/ui/tabs/renderdoc/launcher_view.go`. `NewRenderDocTab` wraps the launcher row above the existing `AppTabs`. Reuses the existing `locateRenderdoccmd` helper.

**Tech Stack:** Go, Fyne v2, `os/exec`, native `dialog` for file picking.

**Spec:** [docs/superpowers/specs/2026-04-26-renderdoc-launch-studio-design.md](../specs/2026-04-26-renderdoc-launch-studio-design.md)

---

## File Structure

- **Create:** `internal/renderdoc/launch.go` — `LocateRobloxStudio`, `locateRobloxStudioIn` (testable seam), `LaunchStudioWithRenderDoc`, `buildLaunchCommand` (testable command builder).
- **Create:** `internal/renderdoc/launch_test.go` — unit tests for path resolution and command construction.
- **Create:** `internal/app/ui/tabs/renderdoc/launcher_view.go` — Fyne row builder `newLauncherRow(window) fyne.CanvasObject`.
- **Modify:** `internal/app/ui/tabs/renderdoc/renderdoc_tab.go:57` — wrap `AppTabs` in a Border with the launcher row at top.

---

## Task 1: LocateRobloxStudio path resolution

**Files:**
- Create: `internal/renderdoc/launch.go`
- Test: `internal/renderdoc/launch_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/renderdoc/launch_test.go`:

```go
package renderdoc

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func mustWriteExe(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("MZ"), 0o755); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestLocateRobloxStudio_PicksNewest(t *testing.T) {
	root := t.TempDir()
	older := filepath.Join(root, "version-A", "RobloxStudioBeta.exe")
	newer := filepath.Join(root, "version-B", "RobloxStudioBeta.exe")
	mustWriteExe(t, older)
	mustWriteExe(t, newer)

	past := time.Now().Add(-1 * time.Hour)
	if err := os.Chtimes(older, past, past); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	got, err := locateRobloxStudioIn("", root)
	if err != nil {
		t.Fatalf("locate: %v", err)
	}
	if got != newer {
		t.Fatalf("got %q, want %q", got, newer)
	}
}

func TestLocateRobloxStudio_RespectsEnvVar(t *testing.T) {
	root := t.TempDir()
	mustWriteExe(t, filepath.Join(root, "version-A", "RobloxStudioBeta.exe"))

	envPath := filepath.Join(t.TempDir(), "Custom", "RobloxStudioBeta.exe")
	mustWriteExe(t, envPath)

	got, err := locateRobloxStudioIn(envPath, root)
	if err != nil {
		t.Fatalf("locate: %v", err)
	}
	if got != envPath {
		t.Fatalf("got %q, want %q", got, envPath)
	}
}

func TestLocateRobloxStudio_EnvSetButMissing_FallsThroughToScan(t *testing.T) {
	root := t.TempDir()
	scanned := filepath.Join(root, "version-A", "RobloxStudioBeta.exe")
	mustWriteExe(t, scanned)

	got, err := locateRobloxStudioIn(filepath.Join(t.TempDir(), "does-not-exist.exe"), root)
	if err != nil {
		t.Fatalf("locate: %v", err)
	}
	if got != scanned {
		t.Fatalf("got %q, want %q", got, scanned)
	}
}

func TestLocateRobloxStudio_NotFound(t *testing.T) {
	_, err := locateRobloxStudioIn("", t.TempDir())
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "JOXBLOX_ROBLOX_STUDIO") {
		t.Fatalf("error should mention env var, got: %v", err)
	}
}
```

- [ ] **Step 2: Run tests — verify they fail**

Run: `go test ./internal/renderdoc/ -run TestLocateRobloxStudio -v`
Expected: FAIL with `undefined: locateRobloxStudioIn`.

- [ ] **Step 3: Implement `launch.go`**

Create `internal/renderdoc/launch.go`:

```go
package renderdoc

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

const robloxStudioExeName = "RobloxStudioBeta.exe"

// LocateRobloxStudio finds RobloxStudioBeta.exe.
// Resolution: env JOXBLOX_ROBLOX_STUDIO -> %LOCALAPPDATA%\Roblox\Versions\*\RobloxStudioBeta.exe (newest mtime).
// Returns a clear error mentioning the env var if none found.
func LocateRobloxStudio() (string, error) {
	envValue := os.Getenv("JOXBLOX_ROBLOX_STUDIO")
	versionsRoot := filepath.Join(os.Getenv("LOCALAPPDATA"), "Roblox", "Versions")
	return locateRobloxStudioIn(envValue, versionsRoot)
}

// locateRobloxStudioIn is the testable seam for LocateRobloxStudio.
func locateRobloxStudioIn(envValue, versionsRoot string) (string, error) {
	if envValue != "" {
		if _, err := os.Stat(envValue); err == nil {
			return envValue, nil
		}
	}

	if versionsRoot != "" {
		entries, err := os.ReadDir(versionsRoot)
		if err == nil {
			type candidate struct {
				path  string
				mtime int64
			}
			var found []candidate
			for _, entry := range entries {
				if !entry.IsDir() {
					continue
				}
				exe := filepath.Join(versionsRoot, entry.Name(), robloxStudioExeName)
				info, statErr := os.Stat(exe)
				if statErr != nil {
					continue
				}
				found = append(found, candidate{path: exe, mtime: info.ModTime().UnixNano()})
			}
			if len(found) > 0 {
				sort.Slice(found, func(i, j int) bool { return found[i].mtime > found[j].mtime })
				return found[0].path, nil
			}
		}
	}

	return "", errors.New("RobloxStudioBeta.exe not found — install Roblox Studio or set the JOXBLOX_ROBLOX_STUDIO environment variable")
}

var _ = fmt.Sprintf // keep fmt import for future use; remove if unused after Task 2
```

Note: Drop the `var _ = fmt.Sprintf` line if you don't need `fmt` yet — it's a placeholder so adding the launch helper in Task 2 doesn't churn imports.

- [ ] **Step 4: Run tests — verify they pass**

Run: `go test ./internal/renderdoc/ -run TestLocateRobloxStudio -v`
Expected: all four pass.

- [ ] **Step 5: Commit**

```bash
git add internal/renderdoc/launch.go internal/renderdoc/launch_test.go
git commit -m "feat(renderdoc): locate RobloxStudioBeta.exe via env var or Versions scan"
```

---

## Task 2: LaunchStudioWithRenderDoc command builder

**Files:**
- Modify: `internal/renderdoc/launch.go`
- Modify: `internal/renderdoc/launch_test.go`

- [ ] **Step 1: Write failing tests**

Append to `internal/renderdoc/launch_test.go`:

```go
func TestBuildLaunchCommand_UsesCaptureSubcommand(t *testing.T) {
	cmd := buildLaunchCommand("/usr/bin/renderdoccmd", "/path/to/RobloxStudioBeta.exe")
	if cmd.Path != "/usr/bin/renderdoccmd" {
		t.Fatalf("Path = %q, want renderdoccmd", cmd.Path)
	}
	wantArgs := []string{"/usr/bin/renderdoccmd", "capture", "/path/to/RobloxStudioBeta.exe"}
	if len(cmd.Args) != len(wantArgs) {
		t.Fatalf("Args = %v, want %v", cmd.Args, wantArgs)
	}
	for i, a := range wantArgs {
		if cmd.Args[i] != a {
			t.Fatalf("Args[%d] = %q, want %q", i, cmd.Args[i], a)
		}
	}
}

func TestLaunchStudioWithRenderDoc_RejectsMissingStudio(t *testing.T) {
	_, err := LaunchStudioWithRenderDoc(filepath.Join(t.TempDir(), "no-such-studio.exe"))
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "Studio executable") {
		t.Fatalf("error should mention Studio executable, got: %v", err)
	}
}
```

- [ ] **Step 2: Run tests — verify they fail**

Run: `go test ./internal/renderdoc/ -run "TestBuildLaunchCommand|TestLaunchStudioWithRenderDoc" -v`
Expected: FAIL with `undefined: buildLaunchCommand` / `undefined: LaunchStudioWithRenderDoc`.

- [ ] **Step 3: Implement command builder and launch wrapper**

Replace the placeholder `var _ = fmt.Sprintf` line at the bottom of `internal/renderdoc/launch.go` with:

```go
// LaunchStudioWithRenderDoc spawns `renderdoccmd capture <studioPath>` detached.
// Returns the started *exec.Cmd (not waited on). The caller should not Wait()
// on it — Studio runs independently.
func LaunchStudioWithRenderDoc(studioPath string) (*exec.Cmd, error) {
	if _, err := os.Stat(studioPath); err != nil {
		return nil, fmt.Errorf("Studio executable not found at %q: %w", studioPath, err)
	}

	cmdPath, err := locateRenderdoccmd()
	if err != nil {
		return nil, err
	}

	cmd := buildLaunchCommand(cmdPath, studioPath)
	configureLaunchSysProcAttr(cmd)
	cmd.Stdout = nil
	cmd.Stderr = nil
	if startErr := cmd.Start(); startErr != nil {
		return nil, fmt.Errorf("start renderdoccmd: %w", startErr)
	}
	return cmd, nil
}

func buildLaunchCommand(renderdoccmdPath, studioPath string) *exec.Cmd {
	return exec.Command(renderdoccmdPath, "capture", studioPath)
}
```

Update the imports at the top of `internal/renderdoc/launch.go` to:

```go
import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
)
```

Create `internal/renderdoc/launch_windows.go`:

```go
//go:build windows

package renderdoc

import (
	"os/exec"
	"syscall"
)

func configureLaunchSysProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
}
```

Create `internal/renderdoc/launch_other.go`:

```go
//go:build !windows

package renderdoc

import "os/exec"

func configureLaunchSysProcAttr(cmd *exec.Cmd) {
	// no-op on non-Windows
	_ = cmd
}
```

- [ ] **Step 4: Run tests — verify they pass**

Run: `go test ./internal/renderdoc/ -v`
Expected: all tests pass (including pre-existing ones).

- [ ] **Step 5: Commit**

```bash
git add internal/renderdoc/launch.go internal/renderdoc/launch_test.go internal/renderdoc/launch_windows.go internal/renderdoc/launch_other.go
git commit -m "feat(renderdoc): launch Roblox Studio with renderdoccmd capture"
```

---

## Task 3: Launcher row UI

**Files:**
- Create: `internal/app/ui/tabs/renderdoc/launcher_view.go`

- [ ] **Step 1: Implement the launcher row**

Create `internal/app/ui/tabs/renderdoc/launcher_view.go`:

```go
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
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./...`
Expected: success.

- [ ] **Step 3: Commit**

```bash
git add internal/app/ui/tabs/renderdoc/launcher_view.go
git commit -m "feat(renderdoc-tab): launcher row with path entry, Browse, and Launch button"
```

---

## Task 4: Mount the launcher row in NewRenderDocTab

**Files:**
- Modify: `internal/app/ui/tabs/renderdoc/renderdoc_tab.go:57-64`

- [ ] **Step 1: Update `NewRenderDocTab`**

Replace the body of `NewRenderDocTab` in `internal/app/ui/tabs/renderdoc/renderdoc_tab.go` (currently lines 57–64) with:

```go
// NewRenderDocTab builds the RenderDoc tab. A launcher row sits above two
// sub-tabs: Textures (existing UI) and Meshes (new).
func NewRenderDocTab(window fyne.Window) fyne.CanvasObject {
	textures := newTexturesSubTab(window)
	meshes := newMeshesSubTab(window)
	tabs := container.NewAppTabs(
		container.NewTabItem("Textures", textures),
		container.NewTabItem("Meshes", meshes),
	)
	launcher := newLauncherRow(window)
	return container.NewBorder(launcher, nil, nil, nil, tabs)
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./...`
Expected: success.

- [ ] **Step 3: Run all tests**

Run: `go test ./...`
Expected: success.

- [ ] **Step 4: Commit**

```bash
git add internal/app/ui/tabs/renderdoc/renderdoc_tab.go
git commit -m "feat(renderdoc-tab): mount Launch with RenderDoc row above sub-tabs"
```

---

## Task 5: Manual smoke test

**Files:** none (verification only).

- [ ] **Step 1: Build the app**

Run: `go build -o joxblox.exe ./cmd/joxblox` (or whatever the existing build command is — check `latest.log` or `README.md` if unsure).
Expected: success.

- [ ] **Step 2: Open the RenderDoc tab and confirm**

Launch joxblox. On the RenderDoc tab, confirm:
- The launcher row appears above the Textures/Meshes sub-tabs.
- The Studio path entry is pre-populated with an existing `RobloxStudioBeta.exe` (or blank with the placeholder if no install detected).
- Browse… opens a native file dialog filtered to `.exe`.

- [ ] **Step 3: Click Launch — verify Studio comes up under renderdoccmd**

Click "Launch with RenderDoc". Expected:
- Status label updates to `Launched (PID …)`.
- Roblox Studio opens.
- In Task Manager, `RobloxStudioBeta.exe` has `renderdoccmd.exe` as its parent process (or the process tree shows the injection layer).
- F12 inside Studio still triggers a manual capture as normal — RenderDoc's own UI is not required.

- [ ] **Step 4: Click Launch with a bad path — verify error dialog**

Edit the entry to point at a non-existent path, click Launch. Expected: error dialog mentioning `Studio executable not found`. Status label shows `Error`. Restore the path afterwards.

- [ ] **Step 5: Quit Studio, click Launch again**

After Studio exits, the button should be re-enabled (it re-enables ~1s after click regardless of Studio lifecycle). Click again — expect a fresh Studio process.

---

## Self-Review Notes

- **Spec coverage:**
  - Goal/Scope → Tasks 1–4 implement; phase 2 features (auto-load, on-demand capture) explicitly excluded.
  - UX (path entry, Browse, Launch, status) → Task 3.
  - Studio path resolution (preference → env → scan → blank) → Task 3 (`initialStudioPath`) plus Task 1 (`LocateRobloxStudio` covers env+scan).
  - RenderDoc invocation (`renderdoccmd capture`, no `--wait-for-exit`, no `--capture-file`, hidden console) → Task 2.
  - Architecture (launch.go pure logic, launcher_view.go UI, renderdoc_tab.go mount) → Tasks 1–4.
  - Error handling (renderdoccmd missing, Studio missing, Start fails, no candidates) → Tasks 2–3.
  - Testing (`launch_test.go` with the four `LocateRobloxStudio` cases) → Task 1; manual smoke → Task 5.
- **Placeholders:** none.
- **Type consistency:** `LocateRobloxStudio` / `locateRobloxStudioIn` / `LaunchStudioWithRenderDoc` / `buildLaunchCommand` / `configureLaunchSysProcAttr` / `newLauncherRow` / `initialStudioPath` / `persistStudioPath` / `preferenceKeyRenderDocStudioPath` — all consistent across tasks.
