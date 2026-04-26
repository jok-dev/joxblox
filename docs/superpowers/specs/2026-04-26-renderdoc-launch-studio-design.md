# RenderDoc Launch Studio

## Goal

Add a "Launch Studio with RenderDoc" button to the RenderDoc tab so the user can spawn Roblox Studio with the RenderDoc capture layer attached without dropping to a terminal. After Studio exits, the user clicks the existing "Load .rdc…" button in either sub-tab to inspect a capture; the file dialog already remembers its last directory, so this stays out of the auto-load business.

## Scope

Phase 1, just the launcher. Out of scope:
- On-demand "trigger capture" button while Studio is running (requires RenderDoc Target Control TCP protocol or window-keystroke injection — phase 2).
- Auto-loading the most recent `.rdc` after Studio exits.
- Linux/macOS Studio paths.

## UX

A single horizontal row mounted above the existing `AppTabs` (Textures | Meshes):

```
Studio: [/path/to/RobloxStudioBeta.exe        ] [Browse…]   [Launch with RenderDoc]   status…
```

- **Path entry** — pre-populated on construction. Editable.
- **Browse** — native file dialog filtered to `.exe`, sets the entry.
- **Launch** — validates the path, spawns `renderdoccmd capture <path>` detached, updates status. Re-enabled after ~1s to debounce double-clicks while still allowing legitimate re-launches.
- **Status label** — `Ready`, `Launched (PID 1234)`, or `Error: …`.

## Studio path resolution

Resolution order on construction:
1. Fyne preference key `renderdoc.studio_path` if non-empty AND the file still exists.
2. Env var `JOXBLOX_ROBLOX_STUDIO` if set AND the file exists.
3. Auto-detect: scan `%LOCALAPPDATA%\Roblox\Versions\*\RobloxStudioBeta.exe`, pick the one with the newest mtime.
4. Otherwise blank, with placeholder `Browse for RobloxStudioBeta.exe`.

Whatever the user types or browses to is persisted to `renderdoc.studio_path` on Launch, matching the preference pattern used in [internal/app/app_settings.go](internal/app/app_settings.go).

## RenderDoc invocation

Reuse the existing `locateRenderdoccmd()` helper in [internal/renderdoc/convert.go:69](internal/renderdoc/convert.go#L69) — already searches `RENDERDOC_CMD`, `PATH`, and the default Windows install.

Command line:
```
renderdoccmd capture <studioPath>
```

Notes:
- No `--wait-for-exit` — Studio runs independently; joxblox UI stays responsive.
- No `--capture-file` override — RenderDoc's default `%TEMP%\RenderDoc\<exe>_<timestamp>.rdc` is what users already expect, and the file picker remembers last-used directory after the first manual load.
- On Windows, spawn with `SysProcAttr{HideWindow: true}` so no console flashes.
- Stdout/stderr discarded (we don't need renderdoccmd's logs blocking on a pipe).

## Architecture

Two new files, one small edit:

- **`internal/renderdoc/launch.go`** *(new)* — pure logic, no UI deps.
- **`internal/app/ui/tabs/renderdoc/launcher_view.go`** *(new)* — Fyne row builder.
- **[internal/app/ui/tabs/renderdoc/renderdoc_tab.go:57](internal/app/ui/tabs/renderdoc/renderdoc_tab.go#L57)** — `NewRenderDocTab` wraps the existing `AppTabs` in `container.NewBorder(launcherRow, nil, nil, nil, appTabs)`.

### `internal/renderdoc/launch.go`

```go
// LocateRobloxStudio finds RobloxStudioBeta.exe.
// Resolution: env JOXBLOX_ROBLOX_STUDIO -> %LOCALAPPDATA%\Roblox\Versions\*\RobloxStudioBeta.exe (newest mtime).
// Returns a clear error mentioning the env var if none found.
func LocateRobloxStudio() (string, error)

// locateRobloxStudioIn is the testable seam: same logic but with explicit roots.
// Used by LocateRobloxStudio with %LOCALAPPDATA% and by tests with a temp dir.
func locateRobloxStudioIn(envValue, versionsRoot string) (string, error)

// LaunchStudioWithRenderDoc spawns `renderdoccmd capture <studioPath>` detached.
// Reuses locateRenderdoccmd(). Returns the started *exec.Cmd (not waited on)
// or nil + error.
func LaunchStudioWithRenderDoc(studioPath string) (*exec.Cmd, error)
```

### `internal/app/ui/tabs/renderdoc/launcher_view.go`

Exports `newLauncherRow(window fyne.Window) fyne.CanvasObject`. Internally:
- Builds the path entry, Browse button, Launch button, status label.
- On construction: resolve initial path via the order in *Studio path resolution*.
- Launch handler runs in a goroutine, validates path with `os.Stat`, calls `renderdoc.LaunchStudioWithRenderDoc`, and updates status via `fyne.Do`. Errors go to `fyneDialog.ShowError(window, err)`.

## Data flow

```
[Launch click]
  -> validate studio path (os.Stat)
  -> persist path to Fyne preference
  -> goroutine:
       LaunchStudioWithRenderDoc(path)
         -> locateRenderdoccmd()
         -> exec.Command(rdcmd, "capture", studioPath)
         -> cmd.Start()
       -> fyne.Do: status = "Launched (PID …)"
     on error -> fyne.Do: ShowError(window, err)
```

No interaction with `pickAndLoadCapture` or any sub-tab state.

## Error handling

- `renderdoccmd` missing → existing error from `locateRenderdoccmd` (mentions `RENDERDOC_CMD` and install URL).
- Studio path missing/invalid → `Studio executable not found at <path>. Set JOXBLOX_ROBLOX_STUDIO or pick a path with Browse.`
- `cmd.Start()` fails → surface OS error verbatim in a dialog.
- `LocateRobloxStudio` returns no candidates → entry starts blank with placeholder.

## Testing

`internal/renderdoc/launch_test.go`:
- `TestLocateRobloxStudio_PicksNewest` — temp `versions` dir with `version-A/RobloxStudioBeta.exe` and `version-B/RobloxStudioBeta.exe`, set mtimes so B is newer; expect B.
- `TestLocateRobloxStudio_RespectsEnvVar` — env value points to an existing file; expect that path verbatim, ignoring the versions root.
- `TestLocateRobloxStudio_EnvSetButMissing_FallsThroughToScan` — env points to a non-existent path; expect the scan result.
- `TestLocateRobloxStudio_NotFound` — empty env + empty root → clear error containing `JOXBLOX_ROBLOX_STUDIO`.

No automated UI tests for the launcher row (Fyne UI testing in this repo is light). Manual smoke: launch joxblox, click Launch, confirm Studio comes up and `renderdoccmd capture` is the parent process.

## New files

- `internal/renderdoc/launch.go`
- `internal/renderdoc/launch_test.go`
- `internal/app/ui/tabs/renderdoc/launcher_view.go`

## Modified files

- [internal/app/ui/tabs/renderdoc/renderdoc_tab.go](internal/app/ui/tabs/renderdoc/renderdoc_tab.go) — wrap `AppTabs` in a Border with the launcher row at top.
