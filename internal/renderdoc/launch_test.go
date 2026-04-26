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
