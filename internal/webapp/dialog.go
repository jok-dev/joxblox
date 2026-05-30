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
	targetPath, dialogErr := wailsRuntime.SaveFileDialog(ctx, wailsRuntime.SaveDialogOptions{
		Title:           title,
		DefaultFilename: defaultName,
		Filters: []wailsRuntime.FileFilter{
			{DisplayName: filterName, Pattern: filterPattern},
		},
	})
	if dialogErr != nil {
		return false, dialogErr
	}
	if strings.TrimSpace(targetPath) == "" {
		return false, nil
	}
	if writeErr := os.WriteFile(targetPath, data, 0o644); writeErr != nil {
		return false, writeErr
	}
	return true, nil
}
