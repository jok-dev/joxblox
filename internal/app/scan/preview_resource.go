package scan

import (
	"fyne.io/fyne/v2"

	"joxblox/internal/app/loader"
)

// scanRowResource rebuilds a Fyne resource from a scan row's Fyne-free preview
// bytes (loader.ScanResult no longer carries a *fyne.StaticResource), or nil
// when the row has no preview image.
func scanRowResource(row loader.ScanResult) fyne.Resource {
	if len(row.PreviewBytes) == 0 {
		return nil
	}
	return fyne.NewStaticResource(row.PreviewName, row.PreviewBytes)
}
