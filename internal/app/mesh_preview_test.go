package app

import (
	"image"
	"image/color"
	"math"
	"testing"
)

func TestBuildMeshPreviewDataNormalizesBounds(t *testing.T) {
	data, err := buildMeshPreviewData(
		[]float32{
			10, 0, 0,
			12, 0, 0,
			10, 2, 0,
		},
		[]uint32{0, 1, 2},
		1,
		1,
	)
	if err != nil {
		t.Fatalf("buildMeshPreviewData returned error: %v", err)
	}
	if len(data.Positions) != 3 {
		t.Fatalf("expected 3 normalized positions, got %d", len(data.Positions))
	}
	if data.Positions[0].X >= 0 || data.Positions[1].X <= 0 {
		t.Fatalf("expected mesh positions to be centered around origin, got %+v", data.Positions)
	}
}

func TestRenderMeshPreviewImageProducesVisiblePixels(t *testing.T) {
	data, err := buildMeshPreviewData(
		[]float32{
			-1, -1, 0,
			1, -1, 0,
			0, 1, 0,
		},
		[]uint32{0, 1, 2},
		1,
		1,
	)
	if err != nil {
		t.Fatalf("buildMeshPreviewData returned error: %v", err)
	}
	rendered := renderMeshPreviewImage(data, 160, 120, 0, 0, 1.0, color.NRGBA{R: 14, G: 17, B: 22, A: 255})
	background := color.NRGBA{R: 14, G: 17, B: 22, A: 255}
	bounds := rendered.Bounds()
	foundForeground := false
	for y := bounds.Min.Y; y < bounds.Max.Y && !foundForeground; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			if color.NRGBAModel.Convert(rendered.At(x, y)) != background {
				foundForeground = true
				break
			}
		}
	}
	if !foundForeground {
		t.Fatal("expected rendered mesh preview to draw non-background pixels")
	}
}

func TestRasterizeMeshTriangleSharedEdgeHasNoBackgroundGap(t *testing.T) {
	const width = 80
	const height = 80
	background := color.NRGBA{R: 14, G: 17, B: 22, A: 255}
	fill := color.NRGBA{R: 112, G: 173, B: 255, A: 255}
	frame := image.NewRGBA(image.Rect(0, 0, width, height))
	fillImage(frame, background)
	zBuffer := make([]float64, width*height)
	for index := range zBuffer {
		zBuffer[index] = math.Inf(1)
	}

	topLeft := meshPreviewProjectedVertex{X: 10, Y: 10, Z: 1}
	topRight := meshPreviewProjectedVertex{X: 70, Y: 10, Z: 1}
	bottomRight := meshPreviewProjectedVertex{X: 70, Y: 70, Z: 1}
	bottomLeft := meshPreviewProjectedVertex{X: 10, Y: 70, Z: 1}

	rasterizeMeshTriangle(frame, zBuffer, width, height, topLeft, topRight, bottomRight, fill)
	rasterizeMeshTriangle(frame, zBuffer, width, height, topLeft, bottomRight, bottomLeft, fill)

	for offset := 0; offset <= 40; offset++ {
		x := 10 + offset
		y := 10 + offset
		if color.NRGBAModel.Convert(frame.At(x, y)) == background {
			t.Fatalf("expected shared edge pixel at (%d,%d) to be filled", x, y)
		}
	}
}
