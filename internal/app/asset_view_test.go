package app

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"

	"joxblox/internal/app/loader"
	"joxblox/internal/app/ui"

	"fyne.io/fyne/v2"
	fyneapp "fyne.io/fyne/v2/app"
)

func TestAssetViewSetDataClearsStaleMeshPreviewStateForTextures(t *testing.T) {
	testApp := fyneapp.NewWithID("dev.jok.joxblox.test")
	defer testApp.Quit()

	view := newAssetView("No image loaded", false)
	view.currentMeshPreviewData = ui.MeshPreviewData{
		RawPositions: []float32{0, 0, 0},
		RawIndices:   []uint32{0, 0, 0},
	}

	textureBytes := mustEncodePNG(t)
	view.SetData(loader.AssetViewData{
		AssetID:       123,
		AssetTypeID:   1,
		AssetTypeName: "Image",
		PreviewImageInfo: &loader.ImageInfo{
			Resource:    fyne.NewStaticResource("texture.png", textureBytes),
			Width:       1,
			Height:      1,
			BytesSize:   len(textureBytes),
			Format:      "png",
			ContentType: "image/png",
		},
		StatsInfo: &loader.ImageInfo{
			Width:       1,
			Height:      1,
			BytesSize:   len(textureBytes),
			Format:      "png",
			ContentType: "image/png",
		},
		DownloadBytes:      textureBytes,
		DownloadFileName:   "texture.png",
		DownloadIsOriginal: true,
	})

	if len(view.currentMeshPreviewData.RawPositions) != 0 || len(view.currentMeshPreviewData.RawIndices) != 0 {
		t.Fatalf("expected stale mesh preview state to be cleared for texture assets")
	}
	if view.MeshPreview.Visible() {
		t.Fatalf("expected mesh preview to stay hidden for texture assets")
	}
}

func TestAssetViewSetDataShowsInGameSizeMetric(t *testing.T) {
	testApp := fyneapp.NewWithID("dev.jok.joxblox.test")
	defer testApp.Quit()

	view := newAssetView("No image loaded", false)
	textureBytes := mustEncodePNG(t)
	view.SetData(loader.AssetViewData{
		AssetID:            123,
		AssetTypeID:        1,
		AssetTypeName:      "Image",
		SceneSurfaceArea:   2,
		LargestSurfacePath: "Workspace.Building.Wall.Decal",
		LargeTextureScore:  4096,
		PreviewImageInfo: &loader.ImageInfo{
			Resource:    fyne.NewStaticResource("texture.png", textureBytes),
			Width:       1,
			Height:      1,
			BytesSize:   len(textureBytes),
			Format:      "png",
			ContentType: "image/png",
		},
		StatsInfo: &loader.ImageInfo{
			Width:       1,
			Height:      1,
			BytesSize:   len(textureBytes),
			Format:      "png",
			ContentType: "image/png",
		},
	})

	if got := view.InGameSizeValue.Text; got != "4.00 KB/stud^2 (2 stud^2 surface) at Workspace.Building.Wall.Decal" {
		t.Fatalf("expected in-game size text, got %q", got)
	}
}

func mustEncodePNG(t *testing.T) []byte {
	t.Helper()

	sourceImage := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	sourceImage.Set(0, 0, color.NRGBA{R: 255, G: 0, B: 0, A: 255})

	var output bytes.Buffer
	if err := png.Encode(&output, sourceImage); err != nil {
		t.Fatalf("png encode failed: %v", err)
	}
	return output.Bytes()
}
