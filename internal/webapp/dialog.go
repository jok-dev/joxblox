package webapp

import (
	"context"
	"os"
	"strings"

	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// saveBytesViaDialog prompts for a destination through the native save dialog
// and writes data there. Returns false (without error) when the user cancels.
// Shared by the report exports and the Tag Report so every "Save as…" flow
// behaves identically.
func saveBytesViaDialog(ctx context.Context, title, defaultName, filterName, filterPattern string, data []byte) (bool, error) {
	savedPath, err := saveBytesViaDialogPath(ctx, title, defaultName, filterName, filterPattern, data)
	if err != nil {
		return false, err
	}
	return savedPath != "", nil
}

// saveBytesViaDialogPath is the path-returning variant of saveBytesViaDialog.
// It returns the absolute path the data was written to, or "" (without error)
// when the user cancels. Used by flows that need to report where the file
// landed (e.g. the asset download notification).
func saveBytesViaDialogPath(ctx context.Context, title, defaultName, filterName, filterPattern string, data []byte) (string, error) {
	targetPath, dialogErr := wailsRuntime.SaveFileDialog(ctx, wailsRuntime.SaveDialogOptions{
		Title:           title,
		DefaultFilename: defaultName,
		Filters: []wailsRuntime.FileFilter{
			{DisplayName: filterName, Pattern: filterPattern},
		},
	})
	if dialogErr != nil {
		return "", dialogErr
	}
	if strings.TrimSpace(targetPath) == "" {
		return "", nil
	}
	if writeErr := os.WriteFile(targetPath, data, 0o644); writeErr != nil {
		return "", writeErr
	}
	return targetPath, nil
}
