package renderdoc

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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

// LaunchStudioWithRenderDoc spawns `renderdoccmd capture <studioPath>` detached.
// If captureFileTemplate is non-empty, it is passed as `-c <template>` so
// captures land at a predictable path stem (renderdoccmd appends its own
// suffix and `.rdc`). Returns the started *exec.Cmd (not waited on). The
// caller should not Wait() on it — Studio runs independently.
func LaunchStudioWithRenderDoc(studioPath, captureFileTemplate string) (*exec.Cmd, error) {
	if _, err := os.Stat(studioPath); err != nil {
		return nil, fmt.Errorf("Studio executable not found at %q: %w", studioPath, err)
	}

	cmdPath, err := locateRenderdoccmd()
	if err != nil {
		return nil, err
	}

	cmd := buildLaunchCommand(cmdPath, studioPath, captureFileTemplate)
	configureLaunchSysProcAttr(cmd)
	cmd.Stdout = nil
	cmd.Stderr = nil
	if startErr := cmd.Start(); startErr != nil {
		return nil, fmt.Errorf("start renderdoccmd: %w", startErr)
	}
	return cmd, nil
}

// LocateQRenderDoc finds qrenderdoc.exe (the full RenderDoc UI) so callers
// can open captures in it for deeper inspection. Searches next to
// renderdoccmd first, then PATH, then the default Windows install path.
func LocateQRenderDoc() (string, error) {
	if cmdPath, err := locateRenderdoccmd(); err == nil {
		candidate := filepath.Join(filepath.Dir(cmdPath), "qrenderdoc.exe")
		if _, statErr := os.Stat(candidate); statErr == nil {
			return candidate, nil
		}
	}
	if onPath, err := exec.LookPath("qrenderdoc"); err == nil {
		return onPath, nil
	}
	if runtime.GOOS == "windows" {
		candidate := `C:\Program Files\RenderDoc\qrenderdoc.exe`
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	return "", errors.New("qrenderdoc not found — install RenderDoc (https://renderdoc.org)")
}

func buildLaunchCommand(renderdoccmdPath, studioPath, captureFileTemplate string) *exec.Cmd {
	args := []string{"capture"}
	if captureFileTemplate != "" {
		args = append(args, "-c", captureFileTemplate)
	}
	args = append(args, studioPath)
	return exec.Command(renderdoccmdPath, args...)
}
