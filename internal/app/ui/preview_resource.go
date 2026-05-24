package ui

import (
	"fyne.io/fyne/v2"

	"joxblox/internal/app/loader"
)

// imageInfoResource rebuilds a Fyne resource from the loader's Fyne-free preview
// bytes (loader.ImageInfo no longer carries a *fyne.StaticResource), or nil when
// the ImageInfo has no preview image.
func imageInfoResource(info *loader.ImageInfo) fyne.Resource {
	if info == nil || len(info.PreviewBytes) == 0 {
		return nil
	}
	return fyne.NewStaticResource(info.PreviewName, info.PreviewBytes)
}
