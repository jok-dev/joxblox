// Package renderdoc parses RenderDoc capture files (.rdc) converted to the
// zip.xml format by the renderdoccmd CLI, extracting per-texture metadata so
// Roblox captures can be inspected without opening the RenderDoc GUI.
package renderdoc

import "strings"

// formatBytesPerPixel returns the GPU byte cost per source pixel for a given
// DXGI format. Block-compressed formats are already expressed as bytes per
// pixel (0.5 for BC1/BC4, 1.0 for BC2/BC3/BC5/BC6H/BC7). Returns (0, false)
// for formats we do not model (treated as zero-bytes so they sort to the
// bottom but are still shown in the table with their format string).
func formatBytesPerPixel(format string) (float64, bool) {
	if bpp, ok := bcFormatBytes[format]; ok {
		return bpp, true
	}
	if bpp, ok := uncompressedFormatBytes[format]; ok {
		return float64(bpp), true
	}
	return 0, false
}

// isBlockCompressed reports whether the format uses 4x4 block compression.
func isBlockCompressed(format string) bool {
	_, ok := bcFormatBytes[format]
	return ok
}

// bcFormatHasAlpha reports whether a BC format stores a meaningful alpha
// channel. BC1's optional 1-bit alpha is treated as "no alpha" since it's
// essentially never used as transparency in modern asset pipelines. BC4/BC5
// are single/two-channel and don't carry alpha; BC6H is HDR color only.
// BC2/BC3/BC7 carry explicit alpha.
func bcFormatHasAlpha(format string) bool {
	switch format {
	case "DXGI_FORMAT_BC2_TYPELESS", "DXGI_FORMAT_BC2_UNORM", "DXGI_FORMAT_BC2_UNORM_SRGB",
		"DXGI_FORMAT_BC3_TYPELESS", "DXGI_FORMAT_BC3_UNORM", "DXGI_FORMAT_BC3_UNORM_SRGB",
		"DXGI_FORMAT_BC7_TYPELESS", "DXGI_FORMAT_BC7_UNORM", "DXGI_FORMAT_BC7_UNORM_SRGB":
		return true
	}
	return false
}

// FormatHasAlpha reports whether a DXGI format carries a meaningful alpha
// channel. Used by the UI to decide whether to offer the "A" channel
// toggle — BC1, single/double-channel raw formats, and HDR color formats
// have nothing useful on alpha. Unknown formats default to false so we
// hide the toggle rather than display garbage.
func FormatHasAlpha(format string) bool {
	if bcFormatHasAlpha(format) {
		return true
	}
	if _, isBC := bcFormatBytes[format]; isBC {
		return false
	}
	switch format {
	case "DXGI_FORMAT_A8_UNORM",
		"DXGI_FORMAT_R8G8B8A8_UNORM", "DXGI_FORMAT_R8G8B8A8_UNORM_SRGB",
		"DXGI_FORMAT_R8G8B8A8_TYPELESS", "DXGI_FORMAT_R8G8B8A8_UINT",
		"DXGI_FORMAT_R8G8B8A8_SNORM",
		"DXGI_FORMAT_B8G8R8A8_UNORM", "DXGI_FORMAT_B8G8R8A8_UNORM_SRGB",
		"DXGI_FORMAT_B8G8R8A8_TYPELESS",
		"DXGI_FORMAT_R10G10B10A2_UNORM", "DXGI_FORMAT_R10G10B10A2_TYPELESS",
		"DXGI_FORMAT_R16G16B16A16_UNORM", "DXGI_FORMAT_R16G16B16A16_FLOAT",
		"DXGI_FORMAT_R16G16B16A16_TYPELESS",
		"DXGI_FORMAT_R32G32B32A32_FLOAT", "DXGI_FORMAT_R32G32B32A32_UINT",
		"DXGI_FORMAT_R32G32B32A32_TYPELESS":
		return true
	}
	return false
}

// shortFormatName trims the verbose "DXGI_FORMAT_" prefix so the UI can show
// "BC7_UNORM_SRGB" instead of "DXGI_FORMAT_BC7_UNORM_SRGB".
func shortFormatName(format string) string {
	return strings.TrimPrefix(format, "DXGI_FORMAT_")
}

var bcFormatBytes = map[string]float64{
	"DXGI_FORMAT_BC1_TYPELESS":   0.5,
	"DXGI_FORMAT_BC1_UNORM":      0.5,
	"DXGI_FORMAT_BC1_UNORM_SRGB": 0.5,
	"DXGI_FORMAT_BC2_TYPELESS":   1.0,
	"DXGI_FORMAT_BC2_UNORM":      1.0,
	"DXGI_FORMAT_BC2_UNORM_SRGB": 1.0,
	"DXGI_FORMAT_BC3_TYPELESS":   1.0,
	"DXGI_FORMAT_BC3_UNORM":      1.0,
	"DXGI_FORMAT_BC3_UNORM_SRGB": 1.0,
	"DXGI_FORMAT_BC4_TYPELESS":   0.5,
	"DXGI_FORMAT_BC4_UNORM":      0.5,
	"DXGI_FORMAT_BC4_SNORM":      0.5,
	"DXGI_FORMAT_BC5_TYPELESS":   1.0,
	"DXGI_FORMAT_BC5_UNORM":      1.0,
	"DXGI_FORMAT_BC5_SNORM":      1.0,
	"DXGI_FORMAT_BC6H_TYPELESS":  1.0,
	"DXGI_FORMAT_BC6H_UF16":      1.0,
	"DXGI_FORMAT_BC6H_SF16":      1.0,
	"DXGI_FORMAT_BC7_TYPELESS":   1.0,
	"DXGI_FORMAT_BC7_UNORM":      1.0,
	"DXGI_FORMAT_BC7_UNORM_SRGB": 1.0,
}

var uncompressedFormatBytes = map[string]int{
	"DXGI_FORMAT_R8_UNORM":               1,
	"DXGI_FORMAT_R8_UINT":                1,
	"DXGI_FORMAT_R8_SNORM":               1,
	"DXGI_FORMAT_R8_SINT":                1,
	"DXGI_FORMAT_A8_UNORM":               1,
	"DXGI_FORMAT_R8G8_UNORM":             2,
	"DXGI_FORMAT_R8G8_UINT":              2,
	"DXGI_FORMAT_R16_UNORM":              2,
	"DXGI_FORMAT_R16_FLOAT":              2,
	"DXGI_FORMAT_R16_UINT":               2,
	"DXGI_FORMAT_D16_UNORM":              2,
	"DXGI_FORMAT_R8G8B8A8_UNORM":         4,
	"DXGI_FORMAT_R8G8B8A8_UNORM_SRGB":    4,
	"DXGI_FORMAT_R8G8B8A8_TYPELESS":      4,
	"DXGI_FORMAT_R8G8B8A8_UINT":          4,
	"DXGI_FORMAT_R8G8B8A8_SNORM":         4,
	"DXGI_FORMAT_B8G8R8A8_UNORM":         4,
	"DXGI_FORMAT_B8G8R8A8_UNORM_SRGB":    4,
	"DXGI_FORMAT_B8G8R8A8_TYPELESS":      4,
	"DXGI_FORMAT_B8G8R8X8_UNORM":         4,
	"DXGI_FORMAT_R10G10B10A2_UNORM":      4,
	"DXGI_FORMAT_R10G10B10A2_TYPELESS":   4,
	"DXGI_FORMAT_R11G11B10_FLOAT":        4,
	"DXGI_FORMAT_R16G16_UNORM":           4,
	"DXGI_FORMAT_R16G16_FLOAT":           4,
	"DXGI_FORMAT_R32_FLOAT":              4,
	"DXGI_FORMAT_R32_UINT":               4,
	"DXGI_FORMAT_D32_FLOAT":              4,
	"DXGI_FORMAT_D24_UNORM_S8_UINT":     4,
	"DXGI_FORMAT_R24G8_TYPELESS":         4,
	"DXGI_FORMAT_R16G16B16A16_UNORM":     8,
	"DXGI_FORMAT_R16G16B16A16_FLOAT":     8,
	"DXGI_FORMAT_R16G16B16A16_TYPELESS":  8,
	"DXGI_FORMAT_R32G32_FLOAT":           8,
	"DXGI_FORMAT_R32G32_UINT":            8,
	"DXGI_FORMAT_D32_FLOAT_S8X24_UINT":   8,
	"DXGI_FORMAT_R32G8X24_TYPELESS":      8,
	"DXGI_FORMAT_R32G32B32A32_FLOAT":     16,
	"DXGI_FORMAT_R32G32B32A32_UINT":      16,
	"DXGI_FORMAT_R32G32B32A32_TYPELESS":  16,
}
