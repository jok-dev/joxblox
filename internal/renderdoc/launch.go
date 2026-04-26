package renderdoc

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
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

// LaunchStudioWithRenderDoc spawns `renderdoccmd capture <studioPath>` detached.
// Returns the started *exec.Cmd (not waited on). The caller should not Wait()
// on it — Studio runs independently. Note: the Studio path resolution order
// (preference -> env -> scan) is the caller's responsibility; this function
// just launches whatever path it's given.
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
