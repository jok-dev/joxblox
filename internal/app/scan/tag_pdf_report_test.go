package scan

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"

	"fyne.io/fyne/v2"

	"joxblox/internal/app/loader"
)

// pngBytesForTest produces a tiny valid PNG so the PDF builder can
// embed it as a real image instead of falling through to the
// placeholder branch.
func pngBytesForTest(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			img.SetRGBA(x, y, color.RGBA{R: 255, G: 0, B: 0, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png.Encode: %s", err)
	}
	return buf.Bytes()
}

func TestBuildTagPDFReportProducesValidPDFBytes(t *testing.T) {
	store := NewScanTagStore()
	store.Toggle(101, ScanTagDownscale)
	store.SetDuplicateGroup([]int64{202, 203})
	results := []loader.ScanResult{
		{
			AssetID: 101, AssetTypeName: "Texture", Width: 1024, Height: 1024,
			BytesSize: 2_500_000, PixelCount: 1024 * 1024, PropertyName: "ColorMapContent",
			Resource:    fyne.NewStaticResource("101.png", pngBytesForTest(t)),
			ContentType: "image/png",
		},
		{
			AssetID: 202, AssetTypeName: "Texture", Width: 512, Height: 512,
			BytesSize: 600_000, PixelCount: 512 * 512, PropertyName: "ColorMapContent",
			Resource:    fyne.NewStaticResource("202.png", pngBytesForTest(t)),
			ContentType: "image/png",
		},
		{
			AssetID: 203, AssetTypeName: "Texture", Width: 512, Height: 512,
			BytesSize: 600_000, PixelCount: 512 * 512, PropertyName: "ColorMapContent",
		},
	}
	got, err := BuildTagPDFReport(results, store, TagPDFReportOptions{
		Title:       "Test PDF Report",
		SourcePath:  "C:/scan/place.rbxl",
		EmbedImages: true,
	})
	if err != nil {
		t.Fatalf("BuildTagPDFReport: %s", err)
	}
	if !bytes.HasPrefix(got, []byte("%PDF-")) {
		t.Errorf("output doesn't start with the PDF magic header — got %q", got[:min(8, len(got))])
	}
	if !bytes.Contains(got, []byte("%%EOF")) {
		t.Errorf("output missing PDF EOF marker (file likely truncated)")
	}
	if len(got) < 1500 {
		t.Errorf("PDF suspiciously small (%d bytes) — header + one card should be larger", len(got))
	}
}

func TestBuildTagPDFReportEmptyStoreEmitsEmptyMessage(t *testing.T) {
	got, err := BuildTagPDFReport(nil, NewScanTagStore(), TagPDFReportOptions{})
	if err != nil {
		t.Fatalf("BuildTagPDFReport: %s", err)
	}
	if !bytes.HasPrefix(got, []byte("%PDF-")) {
		t.Errorf("empty-store PDF should still be a valid document")
	}
}

func TestBuildTagPDFReport_EmbedImagesFalseSkipsThumbnails(t *testing.T) {
	store := NewScanTagStore()
	store.Toggle(101, ScanTagDownscale)
	results := []loader.ScanResult{
		{AssetID: 101, AssetTypeName: "Texture", Width: 1024, Height: 1024, PixelCount: 1024 * 1024,
			Resource: fyne.NewStaticResource("101.png", pngBytesForTest(t)), ContentType: "image/png"},
	}
	withImages, err := BuildTagPDFReport(results, store, TagPDFReportOptions{EmbedImages: true})
	if err != nil {
		t.Fatalf("with images: %s", err)
	}
	withoutImages, err := BuildTagPDFReport(results, store, TagPDFReportOptions{EmbedImages: false})
	if err != nil {
		t.Fatalf("without images: %s", err)
	}
	if len(withImages) <= len(withoutImages) {
		t.Errorf("PDF with embedded images (%d B) should be larger than without (%d B)", len(withImages), len(withoutImages))
	}
}

func TestPDFImageTypeFor(t *testing.T) {
	cases := []struct {
		name     string
		mime     string
		content  []byte
		expected string
	}{
		{"png by mime", "image/png", nil, "PNG"},
		{"jpeg by mime", "image/jpeg", nil, "JPG"},
		{"gif by mime", "image/gif", nil, "GIF"},
		{"png by magic", "", []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}, "PNG"},
		{"jpeg by magic", "", []byte{0xFF, 0xD8, 0xFF, 0xE0, 0, 0, 0, 0}, "JPG"},
		{"unknown", "image/webp", []byte{0x52, 0x49, 0x46, 0x46, 0, 0, 0, 0}, ""},
		{"empty", "", nil, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := pdfImageTypeFor(tc.mime, tc.content); got != tc.expected {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
		})
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
