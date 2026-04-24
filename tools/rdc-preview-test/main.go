// rdc-preview-test: decode a single texture from a RenderDoc zip.xml capture
// and write it out as a PNG. Used during development to verify the BC1/BC3
// decoders produce sensible images before wiring into the UI.
package main

import (
	"fmt"
	"image/png"
	"os"

	"joxblox/internal/renderdoc"
)

func main() {
	if len(os.Args) != 4 {
		fmt.Fprintln(os.Stderr, "usage: rdc-preview-test <capture.zip.xml> <resource-id> <output.png>")
		os.Exit(1)
	}
	xmlPath, resourceID, outputPath := os.Args[1], os.Args[2], os.Args[3]

	report, err := renderdoc.ParseCaptureXMLFile(xmlPath)
	if err != nil {
		die("parse: %v", err)
	}
	var target *renderdoc.TextureInfo
	for i := range report.Textures {
		if report.Textures[i].ResourceID == resourceID {
			target = &report.Textures[i]
			break
		}
	}
	if target == nil {
		die("resource id %s not found in capture", resourceID)
	}

	store, err := renderdoc.OpenBufferStore(xmlPath)
	if err != nil {
		die("open buffer store: %v", err)
	}
	defer store.Close()

	img, err := renderdoc.DecodeTexturePreview(*target, store)
	if err != nil {
		die("decode: %v", err)
	}

	out, err := os.Create(outputPath)
	if err != nil {
		die("create output: %v", err)
	}
	defer out.Close()
	if err := png.Encode(out, img); err != nil {
		die("encode png: %v", err)
	}
	fmt.Printf("Wrote %dx%d %s preview to %s\n", target.Width, target.Height, target.ShortFormat, outputPath)
}

func die(fmtString string, args ...any) {
	fmt.Fprintf(os.Stderr, fmtString+"\n", args...)
	os.Exit(1)
}
