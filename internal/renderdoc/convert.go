package renderdoc

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"joxblox/internal/procutil"
)

// ConvertToXML runs `renderdoccmd convert` on the given .rdc file, producing a
// temporary .zip.xml the caller must delete. The returned path is absolute.
//
// ConvertToXML searches, in order: the RENDERDOC_CMD env var, a renderdoccmd
// on PATH, and the default install path on Windows. A clear error is returned
// if none exist so the UI can prompt the user to install RenderDoc.
func ConvertToXML(rdcPath string) (string, error) {
	rdcPath = strings.TrimSpace(rdcPath)
	if rdcPath == "" {
		return "", errors.New("no capture file path provided")
	}
	if _, err := os.Stat(rdcPath); err != nil {
		return "", fmt.Errorf("capture file: %w", err)
	}

	cmdPath, err := locateRenderdoccmd()
	if err != nil {
		return "", err
	}

	tempDir, err := os.MkdirTemp("", "joxblox-rdc-")
	if err != nil {
		return "", fmt.Errorf("create temp dir: %w", err)
	}
	outputPath := filepath.Join(tempDir, filepath.Base(rdcPath)+".zip.xml")

	cmd := exec.Command(cmdPath, "convert",
		"-f", rdcPath,
		"-o", outputPath,
		"-c", "zip.xml",
	)
	procutil.HideWindow(cmd)
	output, runErr := cmd.CombinedOutput()
	if runErr != nil {
		_ = os.RemoveAll(tempDir)
		return "", fmt.Errorf("renderdoccmd convert failed: %w\n%s", runErr, strings.TrimSpace(string(output)))
	}
	if _, statErr := os.Stat(outputPath); statErr != nil {
		_ = os.RemoveAll(tempDir)
		return "", fmt.Errorf("renderdoccmd produced no output file: %w", statErr)
	}
	return outputPath, nil
}

// RemoveConvertedOutput cleans up the temp directory that ConvertToXML wrote
// into. Intended for deferred cleanup in callers.
func RemoveConvertedOutput(path string) {
	if path == "" {
		return
	}
	dir := filepath.Dir(path)
	if !strings.Contains(filepath.Base(dir), "joxblox-rdc-") {
		return
	}
	_ = os.RemoveAll(dir)
}

func locateRenderdoccmd() (string, error) {
	if envPath := strings.TrimSpace(os.Getenv("RENDERDOC_CMD")); envPath != "" {
		if _, err := os.Stat(envPath); err == nil {
			return envPath, nil
		}
	}
	if onPath, err := exec.LookPath("renderdoccmd"); err == nil {
		return onPath, nil
	}
	if runtime.GOOS == "windows" {
		candidate := `C:\Program Files\RenderDoc\renderdoccmd.exe`
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	return "", errors.New("renderdoccmd not found — install RenderDoc (https://renderdoc.org) or set the RENDERDOC_CMD environment variable")
}
