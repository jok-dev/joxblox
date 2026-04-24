package renderdoc

import (
	"image"
	"testing"
)

// buildBC1SolidBlock returns an 8-byte BC1 block encoding a single solid
// color across all 16 pixels. color0 = color1 so the palette has duplicate
// entries and the index bits don't matter; index 0 is picked.
func buildBC1SolidBlock(r5, g6, b5 uint16) []byte {
	color := (r5 << 11) | (g6 << 5) | b5
	block := make([]byte, 8)
	block[0] = byte(color & 0xFF)
	block[1] = byte(color >> 8)
	block[2] = byte(color & 0xFF)
	block[3] = byte(color >> 8)
	// indices all zero → use palette[0] (the color we set)
	return block
}

func TestDecodeBC1SolidRed(t *testing.T) {
	// 31,0,0 in RGB565 = pure red. After 5→8 bit expansion: (31<<3)|(31>>2) = 255.
	block := buildBC1SolidBlock(31, 0, 0)
	img, err := decodeBC1(block, 4, 4)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	got := img.Pix[:4]
	if got[0] != 255 || got[1] != 0 || got[2] != 0 || got[3] != 255 {
		t.Errorf("expected solid red, got rgba=%v", got)
	}
}

func TestDecodeBC1PunchthroughAlpha(t *testing.T) {
	// color0 < color1 triggers 3-color + transparent mode in BC1. Set indices
	// = 0xFFFFFFFF so every pixel picks palette[3], which is transparent.
	// color0 = 0x0000 (black), color1 = 0x07E0 (green, RGB565).
	block := []byte{0x00, 0x00, 0xE0, 0x07, 0xFF, 0xFF, 0xFF, 0xFF}
	img, err := decodeBC1(block, 4, 4)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if img.Pix[3] != 0 {
		t.Errorf("expected alpha=0 in punchthrough mode, got %d", img.Pix[3])
	}
}

func TestDecodeBC3SolidAlpha(t *testing.T) {
	// BC3 block: 8-byte alpha + 8-byte BC1 color.
	// Alpha: a0=200, a1=200, indices all 0 → every pixel has alpha=200.
	// Color: pure white (all bits set in RGB565).
	block := make([]byte, 16)
	block[0] = 200
	block[1] = 200
	// 48 bits of 3-bit indices: all zeros → picks palette[0] = a0 = 200.
	whiteColor := uint16(0xFFFF)
	block[8] = byte(whiteColor & 0xFF)
	block[9] = byte(whiteColor >> 8)
	block[10] = byte(whiteColor & 0xFF)
	block[11] = byte(whiteColor >> 8)

	img, err := decodeBC3(block, 4, 4)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	got := img.Pix[:4]
	if got[3] != 200 {
		t.Errorf("expected alpha=200, got %v", got)
	}
	if got[0] < 240 || got[1] < 240 || got[2] < 240 {
		t.Errorf("expected near-white rgb, got %v", got)
	}
}

func TestDecodeBC1UndersizedInput(t *testing.T) {
	_, err := decodeBC1([]byte{0, 0, 0, 0}, 4, 4)
	if err == nil {
		t.Errorf("expected error for undersized BC1 input")
	}
}

func TestDecodeTexturePreviewUnsupportedFormat(t *testing.T) {
	tex := TextureInfo{
		Format:  "DXGI_FORMAT_R32G32B32A32_FLOAT",
		Uploads: []TextureUpload{{BufferID: "0", ByteLength: 16}},
	}
	_, err := decodeTextureBytes(tex, make([]byte, 1024), 1, 1)
	if err != ErrUnsupportedFormat {
		t.Errorf("expected ErrUnsupportedFormat, got %v", err)
	}
}

func TestHalfToFloat32KnownValues(t *testing.T) {
	cases := []struct {
		half uint16
		want float32
	}{
		{0x0000, 0.0},   // +zero
		{0x3C00, 1.0},   // +1.0
		{0xBC00, -1.0},  // -1.0
		{0x3800, 0.5},   // +0.5
		{0x4000, 2.0},   // +2.0
	}
	for _, c := range cases {
		got := halfToFloat32(c.half)
		if got != c.want {
			t.Errorf("halfToFloat32(0x%04x) = %v, want %v", c.half, got, c.want)
		}
	}
}

func TestDecodeR16G16FloatMapsChannelsToRG(t *testing.T) {
	// Two pixels: (R=1.0, G=0.5) and (R=0.0, G=1.0). All stored little-endian.
	data := []byte{
		0x00, 0x3C, 0x00, 0x38, // pixel 0: R=1.0, G=0.5
		0x00, 0x00, 0x00, 0x3C, // pixel 1: R=0.0, G=1.0
	}
	img, err := decodeR16G16Float(data, 2, 1)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	nrgba, ok := img.(*image.NRGBA)
	if !ok {
		t.Fatalf("expected *image.NRGBA, got %T", img)
	}
	// Pixel 0: R≈255, G≈128
	if nrgba.Pix[0] != 255 || nrgba.Pix[1] != 128 || nrgba.Pix[2] != 0 || nrgba.Pix[3] != 255 {
		t.Errorf("pixel 0: got %v, want [255, 128, 0, 255]", nrgba.Pix[0:4])
	}
	// Pixel 1: R=0, G=255
	if nrgba.Pix[4] != 0 || nrgba.Pix[5] != 255 || nrgba.Pix[6] != 0 || nrgba.Pix[7] != 255 {
		t.Errorf("pixel 1: got %v, want [0, 255, 0, 255]", nrgba.Pix[4:8])
	}
}
