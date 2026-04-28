package renderdoc

import (
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"math"
)

// ErrUnsupportedFormat is returned from DecodeTexturePreview when the texture
// format has no decoder wired up yet. The UI treats this as "preview not
// available" rather than a fatal error.
var ErrUnsupportedFormat = errors.New("texture format not supported for preview")

// ErrNoUploadData is returned when a texture has no UpdateSubresource chunks
// in the capture. This typically means the texture was written to by the GPU
// (render target) rather than uploaded from CPU, so we have no bytes to show.
var ErrNoUploadData = errors.New("texture has no upload data in capture (likely a render target)")

// DecodeTexturePreview reconstructs the base-level image of the given texture
// by reading its first uploaded mip from the BufferStore and running the
// appropriate decoder. Returns an *image.NRGBA that the UI can wrap in a
// fyne.canvas.Image.
func DecodeTexturePreview(texture TextureInfo, store *BufferStore) (image.Image, error) {
	if len(texture.Uploads) == 0 {
		return nil, ErrNoUploadData
	}
	baseUpload := findBaseLevelUpload(texture.Uploads)
	if baseUpload == nil {
		return nil, ErrNoUploadData
	}
	bytes, err := store.ReadBuffer(baseUpload.BufferID)
	if err != nil {
		return nil, fmt.Errorf("read buffer: %w", err)
	}
	return decodeTextureBytes(texture, bytes, texture.Width, texture.Height)
}

// findBaseLevelUpload returns the upload whose Subresource is 0 (mip 0 of
// array slice 0). Returns nil if no matching upload exists.
func findBaseLevelUpload(uploads []TextureUpload) *TextureUpload {
	for i := range uploads {
		if uploads[i].Subresource == 0 {
			return &uploads[i]
		}
	}
	return nil
}

// decodeTextureBytes dispatches to the right decoder for the given format.
func decodeTextureBytes(texture TextureInfo, data []byte, width, height int) (image.Image, error) {
	switch texture.Format {
	case "DXGI_FORMAT_BC1_UNORM", "DXGI_FORMAT_BC1_UNORM_SRGB", "DXGI_FORMAT_BC1_TYPELESS":
		return DecodeBC1(data, width, height)
	case "DXGI_FORMAT_BC3_UNORM", "DXGI_FORMAT_BC3_UNORM_SRGB", "DXGI_FORMAT_BC3_TYPELESS":
		return DecodeBC3(data, width, height)
	case "DXGI_FORMAT_R8G8B8A8_UNORM", "DXGI_FORMAT_R8G8B8A8_UNORM_SRGB", "DXGI_FORMAT_R8G8B8A8_TYPELESS":
		return decodeRGBA8(data, width, height, false)
	case "DXGI_FORMAT_B8G8R8A8_UNORM", "DXGI_FORMAT_B8G8R8A8_UNORM_SRGB", "DXGI_FORMAT_B8G8R8A8_TYPELESS":
		return decodeRGBA8(data, width, height, true)
	case "DXGI_FORMAT_R8_UNORM", "DXGI_FORMAT_A8_UNORM":
		return decodeR8(data, width, height)
	case "DXGI_FORMAT_R16G16_FLOAT":
		return decodeR16G16Float(data, width, height)
	}
	return nil, ErrUnsupportedFormat
}

// decodeRGBA8 copies a linear RGBA/BGRA byte stream into an *image.NRGBA,
// swapping channels if the source is BGRA.
func decodeRGBA8(data []byte, width, height int, bgra bool) (image.Image, error) {
	expectedBytes := width * height * 4
	if len(data) < expectedBytes {
		return nil, fmt.Errorf("RGBA8 decode: need %d bytes, got %d", expectedBytes, len(data))
	}
	dst := image.NewNRGBA(image.Rect(0, 0, width, height))
	if !bgra {
		copy(dst.Pix, data[:expectedBytes])
		return dst, nil
	}
	for i := 0; i < expectedBytes; i += 4 {
		dst.Pix[i+0] = data[i+2]
		dst.Pix[i+1] = data[i+1]
		dst.Pix[i+2] = data[i+0]
		dst.Pix[i+3] = data[i+3]
	}
	return dst, nil
}

// decodeR8 lifts a single-channel 8-bit image into an opaque grayscale RGBA
// so the UI's canvas.Image can render it without special-casing grayscale.
func decodeR8(data []byte, width, height int) (image.Image, error) {
	expectedBytes := width * height
	if len(data) < expectedBytes {
		return nil, fmt.Errorf("R8 decode: need %d bytes, got %d", expectedBytes, len(data))
	}
	dst := image.NewNRGBA(image.Rect(0, 0, width, height))
	for i := 0; i < expectedBytes; i++ {
		v := data[i]
		dst.Pix[i*4+0] = v
		dst.Pix[i*4+1] = v
		dst.Pix[i*4+2] = v
		dst.Pix[i*4+3] = 0xFF
	}
	return dst, nil
}

// decodeR16G16Float converts a two-channel half-float texture (4 bytes per
// pixel: R low/high, G low/high) into an *image.NRGBA with the two source
// channels mapped to red and green, clamped to [0, 1]. Values outside that
// range are clipped — R16G16_FLOAT is often used for signed data (normal
// map XY) but without semantic context we can't know the intended mapping,
// and a [0, 1] clamp is the least surprising default for visual inspection.
func decodeR16G16Float(data []byte, width, height int) (image.Image, error) {
	expectedBytes := width * height * 4
	if len(data) < expectedBytes {
		return nil, fmt.Errorf("R16G16_FLOAT decode: need %d bytes, got %d", expectedBytes, len(data))
	}
	dst := image.NewNRGBA(image.Rect(0, 0, width, height))
	for i := 0; i < width*height; i++ {
		src := data[i*4 : i*4+4]
		r := halfToFloat32(binary.LittleEndian.Uint16(src[0:2]))
		g := halfToFloat32(binary.LittleEndian.Uint16(src[2:4]))
		dst.Pix[i*4+0] = floatToByteClamped(r)
		dst.Pix[i*4+1] = floatToByteClamped(g)
		dst.Pix[i*4+2] = 0
		dst.Pix[i*4+3] = 0xFF
	}
	return dst, nil
}

// halfToFloat32 converts an IEEE 754 binary16 value to float32. Handles
// subnormals, infinities, and NaN — we don't expect those in texture data
// but the capture is treated as untrusted input so we do the full conversion.
func halfToFloat32(h uint16) float32 {
	sign := uint32(h>>15) & 0x1
	exp := uint32(h>>10) & 0x1F
	mant := uint32(h) & 0x3FF

	var bits uint32
	switch {
	case exp == 0:
		if mant == 0 {
			// +/- zero
			bits = sign << 31
		} else {
			// Subnormal: renormalize.
			e := int32(1)
			for mant&0x400 == 0 {
				mant <<= 1
				e--
			}
			mant &= 0x3FF
			bits = (sign << 31) | uint32(e+112)<<23 | (mant << 13)
		}
	case exp == 0x1F:
		// Inf or NaN — preserve mantissa bits so NaN survives.
		bits = (sign << 31) | (0xFF << 23) | (mant << 13)
	default:
		// Normal: re-bias exponent from 15 to 127 (+112) and shift mantissa.
		bits = (sign << 31) | (exp+112)<<23 | (mant << 13)
	}
	return math.Float32frombits(bits)
}

func floatToByteClamped(v float32) uint8 {
	if v <= 0 {
		return 0
	}
	if v >= 1 {
		return 0xFF
	}
	return uint8(v*255 + 0.5)
}
