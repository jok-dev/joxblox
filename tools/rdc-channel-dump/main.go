// rdc-channel-dump: writes one channel of a decoded texture as grayscale.
// Dev utility for inspecting packed textures (normal XY in RG, alpha data,
// etc.).
package main

import (
	"fmt"
	"image"
	"image/png"
	"os"

	"joxblox/internal/renderdoc"
)

func main() {
	if len(os.Args) != 5 {
		fmt.Fprintln(os.Stderr, "usage: rdc-channel-dump <capture.zip.xml> <resource-id> <channel 0-3> <out.png>")
		os.Exit(1)
	}
	report, err := renderdoc.ParseCaptureXMLFile(os.Args[1])
	if err != nil {
		die("%v", err)
	}
	var target *renderdoc.TextureInfo
	for i := range report.Textures {
		if report.Textures[i].ResourceID == os.Args[2] {
			target = &report.Textures[i]
			break
		}
	}
	if target == nil {
		die("resource %s not found", os.Args[2])
	}
	store, err := renderdoc.OpenBufferStore(os.Args[1])
	if err != nil {
		die("%v", err)
	}
	defer store.Close()
	img, err := renderdoc.DecodeTexturePreview(*target, store)
	if err != nil {
		die("%v", err)
	}
	nrgba, ok := img.(*image.NRGBA)
	if !ok {
		die("unexpected image type")
	}
	var ch int
	if _, err := fmt.Sscanf(os.Args[3], "%d", &ch); err != nil || ch < 0 || ch > 3 {
		die("channel must be 0..3")
	}
	out := image.NewNRGBA(nrgba.Rect)
	for i := 0; i < len(nrgba.Pix); i += 4 {
		v := nrgba.Pix[i+ch]
		out.Pix[i] = v
		out.Pix[i+1] = v
		out.Pix[i+2] = v
		out.Pix[i+3] = 0xFF
	}
	f, err := os.Create(os.Args[4])
	if err != nil {
		die("%v", err)
	}
	defer f.Close()
	if err := png.Encode(f, out); err != nil {
		die("%v", err)
	}
	fmt.Printf("wrote channel %d of %dx%d %s to %s\n", ch, target.Width, target.Height, target.ShortFormat, os.Args[4])
}

func die(fmtStr string, args ...any) {
	fmt.Fprintf(os.Stderr, fmtStr+"\n", args...)
	os.Exit(1)
}
