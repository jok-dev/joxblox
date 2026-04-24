package renderdoc

import (
	"fmt"
	"image"
)

// decodeBC1 decodes a BC1-compressed (DXT1) mip level into an *image.NRGBA.
// data is the tightly-packed sequence of 8-byte blocks scanning left-to-right
// then top-to-bottom, 4x4 pixels per block. width and height are in pixels
// and need not be block-aligned; trailing pixels outside the image bounds are
// ignored.
func decodeBC1(data []byte, width, height int) (*image.NRGBA, error) {
	blockWidth := (width + 3) / 4
	blockHeight := (height + 3) / 4
	expectedBytes := blockWidth * blockHeight * 8
	if len(data) < expectedBytes {
		return nil, fmt.Errorf("BC1 decode: need %d bytes, got %d", expectedBytes, len(data))
	}
	dst := image.NewNRGBA(image.Rect(0, 0, width, height))
	var rgba [4][4][4]uint8
	for by := 0; by < blockHeight; by++ {
		for bx := 0; bx < blockWidth; bx++ {
			block := data[(by*blockWidth+bx)*8 : (by*blockWidth+bx)*8+8]
			decodeBC1Block(block, &rgba, true)
			blitBlock(dst, bx*4, by*4, &rgba, width, height)
		}
	}
	return dst, nil
}

// decodeBC3 decodes a BC3-compressed (DXT5) mip level. BC3 = 8-byte alpha
// block followed by 8-byte BC1-style color block. The color block always uses
// 4-color mode (never the transparent/punch-through mode of BC1).
func decodeBC3(data []byte, width, height int) (*image.NRGBA, error) {
	blockWidth := (width + 3) / 4
	blockHeight := (height + 3) / 4
	expectedBytes := blockWidth * blockHeight * 16
	if len(data) < expectedBytes {
		return nil, fmt.Errorf("BC3 decode: need %d bytes, got %d", expectedBytes, len(data))
	}
	dst := image.NewNRGBA(image.Rect(0, 0, width, height))
	var rgba [4][4][4]uint8
	var alphas [4][4]uint8
	for by := 0; by < blockHeight; by++ {
		for bx := 0; bx < blockWidth; bx++ {
			offset := (by*blockWidth + bx) * 16
			alphaBlock := data[offset : offset+8]
			colorBlock := data[offset+8 : offset+16]
			decodeBC1Block(colorBlock, &rgba, false)
			decodeBC3AlphaBlock(alphaBlock, &alphas)
			for y := 0; y < 4; y++ {
				for x := 0; x < 4; x++ {
					rgba[y][x][3] = alphas[y][x]
				}
			}
			blitBlock(dst, bx*4, by*4, &rgba, width, height)
		}
	}
	return dst, nil
}

// decodeBC1Block expands one 8-byte BC1 block into a 4x4 RGBA grid.
// The alpha channel is set to 255 everywhere unless punchthroughAlpha is true
// AND the block uses 3-color+transparent mode (color0 <= color1).
// For BC3's color sub-block, punchthroughAlpha must be false.
func decodeBC1Block(block []byte, out *[4][4][4]uint8, punchthroughAlpha bool) {
	color0 := uint16(block[0]) | uint16(block[1])<<8
	color1 := uint16(block[2]) | uint16(block[3])<<8

	var palette [4][4]uint8
	r0, g0, b0 := rgb565ToRGB(color0)
	r1, g1, b1 := rgb565ToRGB(color1)
	palette[0] = [4]uint8{r0, g0, b0, 255}
	palette[1] = [4]uint8{r1, g1, b1, 255}

	if punchthroughAlpha && color0 <= color1 {
		// 3-color + transparent mode.
		palette[2] = [4]uint8{
			uint8((uint16(r0) + uint16(r1)) / 2),
			uint8((uint16(g0) + uint16(g1)) / 2),
			uint8((uint16(b0) + uint16(b1)) / 2),
			255,
		}
		palette[3] = [4]uint8{0, 0, 0, 0}
	} else {
		// 4-color mode (also used for BC2/BC3 color blocks regardless of
		// color0/color1 ordering).
		palette[2] = [4]uint8{
			uint8((2*uint16(r0) + uint16(r1)) / 3),
			uint8((2*uint16(g0) + uint16(g1)) / 3),
			uint8((2*uint16(b0) + uint16(b1)) / 3),
			255,
		}
		palette[3] = [4]uint8{
			uint8((uint16(r0) + 2*uint16(r1)) / 3),
			uint8((uint16(g0) + 2*uint16(g1)) / 3),
			uint8((uint16(b0) + 2*uint16(b1)) / 3),
			255,
		}
	}

	indices := uint32(block[4]) | uint32(block[5])<<8 | uint32(block[6])<<16 | uint32(block[7])<<24
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			idx := (indices >> uint(2*(y*4+x))) & 0x3
			out[y][x] = palette[idx]
		}
	}
}

// decodeBC3AlphaBlock expands one 8-byte BC3 alpha block (same layout as BC4)
// into a 4x4 grid of 8-bit alpha values.
func decodeBC3AlphaBlock(block []byte, out *[4][4]uint8) {
	a0 := block[0]
	a1 := block[1]

	var palette [8]uint8
	palette[0] = a0
	palette[1] = a1
	if a0 > a1 {
		for i := 1; i <= 6; i++ {
			palette[i+1] = uint8((uint16(7-i)*uint16(a0) + uint16(i)*uint16(a1)) / 7)
		}
	} else {
		for i := 1; i <= 4; i++ {
			palette[i+1] = uint8((uint16(5-i)*uint16(a0) + uint16(i)*uint16(a1)) / 5)
		}
		palette[6] = 0
		palette[7] = 255
	}

	// 48 bits of 3-bit indices: 16 pixels × 3 bits = 48 bits.
	indices := uint64(block[2]) |
		uint64(block[3])<<8 |
		uint64(block[4])<<16 |
		uint64(block[5])<<24 |
		uint64(block[6])<<32 |
		uint64(block[7])<<40
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			idx := (indices >> uint(3*(y*4+x))) & 0x7
			out[y][x] = palette[idx]
		}
	}
}

func rgb565ToRGB(color uint16) (uint8, uint8, uint8) {
	r := uint8((color >> 11) & 0x1F)
	g := uint8((color >> 5) & 0x3F)
	b := uint8(color & 0x1F)
	// Expand 5/6-bit channels to 8-bit by bit-replication — standard BC1
	// reconstruction.
	return (r << 3) | (r >> 2), (g << 2) | (g >> 4), (b << 3) | (b >> 2)
}

// blitBlock copies one 4x4 block of RGBA into the destination image, clipping
// against (width, height) so block rows that fall outside the image (when
// dimensions aren't multiples of 4) are skipped.
func blitBlock(dst *image.NRGBA, dstX, dstY int, block *[4][4][4]uint8, width, height int) {
	for y := 0; y < 4; y++ {
		py := dstY + y
		if py >= height {
			break
		}
		for x := 0; x < 4; x++ {
			px := dstX + x
			if px >= width {
				break
			}
			offset := dst.PixOffset(px, py)
			dst.Pix[offset+0] = block[y][x][0]
			dst.Pix[offset+1] = block[y][x][1]
			dst.Pix[offset+2] = block[y][x][2]
			dst.Pix[offset+3] = block[y][x][3]
		}
	}
}
