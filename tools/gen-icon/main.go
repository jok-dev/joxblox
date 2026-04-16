// Generates PNG icon files at standard Windows icon sizes from the app's SVG
// icon definition. Renders at high resolution and downscales for quality.
//
// Usage: go run ./tools/gen-icon <output-dir>
package main

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"

	"golang.org/x/image/draw"
)

const renderSize = 1024

var (
	colorOuter = color.NRGBA{0x11, 0x18, 0x27, 0xFF}
	colorMid   = color.NRGBA{0x1f, 0x29, 0x37, 0xFF}
	colorBlue  = color.NRGBA{0x0e, 0xa5, 0xe9, 0xFF}
	colorWhite = color.NRGBA{0xFF, 0xFF, 0xFF, 0xFF}
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: gen-icon <output-dir>")
		os.Exit(1)
	}
	outDir := os.Args[1]
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "mkdir %s: %v\n", outDir, err)
		os.Exit(1)
	}

	src := renderIcon(renderSize)

	for _, size := range []int{256, 64, 48, 32, 16} {
		dst := image.NewNRGBA(image.Rect(0, 0, size, size))
		draw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Over, nil)
		writeImage(filepath.Join(outDir, fmt.Sprintf("icon%d.png", size)), dst)
	}
}

func writeImage(path string, img image.Image) {
	f, err := os.Create(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "create %s: %v\n", path, err)
		os.Exit(1)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		fmt.Fprintf(os.Stderr, "encode %s: %v\n", path, err)
		os.Exit(1)
	}
}

func renderIcon(size int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	scale := float64(size) / 256.0
	diamondRadius := 32 * math.Sqrt2

	for y := range size {
		for x := range size {
			fx := float64(x) / scale
			fy := float64(y) / scale

			var c color.NRGBA
			if inRoundedRect(fx, fy, 0, 0, 256, 256, 56) {
				c = colorOuter
			}
			if inRoundedRect(fx, fy, 42, 42, 214, 214, 34) {
				c = colorMid
			}
			if inRoundedRect(fx, fy, 64, 64, 192, 192, 24) {
				c = colorBlue
			}
			if math.Abs(fx-128)+math.Abs(fy-128) <= diamondRadius {
				c = colorWhite
			}

			img.SetNRGBA(x, y, c)
		}
	}
	return img
}

func inRoundedRect(px, py, x0, y0, x1, y1, r float64) bool {
	if px < x0 || px > x1 || py < y0 || py > y1 {
		return false
	}
	type corner struct{ cx, cy float64 }
	checks := []struct {
		cond   bool
		corner corner
	}{
		{px < x0+r && py < y0+r, corner{x0 + r, y0 + r}},
		{px > x1-r && py < y0+r, corner{x1 - r, y0 + r}},
		{px < x0+r && py > y1-r, corner{x0 + r, y1 - r}},
		{px > x1-r && py > y1-r, corner{x1 - r, y1 - r}},
	}
	for _, ch := range checks {
		if ch.cond {
			dx, dy := px-ch.corner.cx, py-ch.corner.cy
			return dx*dx+dy*dy <= r*r
		}
	}
	return true
}
