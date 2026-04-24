package loader

import (
	"fmt"
	"math"
	"strings"

	"joxblox/internal/format"
)

// Byte rates (bytes per source pixel, excluding mipmaps) matching the formats
// Roblox picks at upload time. Block-compressed, fixed-rate on GPU.
const (
	bc1BytesPerPixel  = 0.5
	bc3BytesPerPixel  = 1.0
	gpuMipChainFactor = 4.0 / 3.0

	// WastefulBC3AlphaFraction is the threshold below which a BC3 texture
	// is considered to be wasting its alpha channel: if less than 5% of
	// pixels are non-opaque, the artist could likely drop the alpha and
	// halve the GPU footprint.
	WastefulBC3AlphaFraction = 0.05
)

// IsNormalMapProperty reports whether a SurfaceAppearance/MaterialVariant
// property name refers to a normal map slot. Normal maps bypass the usual
// alpha-based BC1/BC3 selection — Roblox encodes them as BC3 DXT5nm
// (swizzled XY) regardless of source alpha channel. Identifying them by
// property name avoids misclassifying opaque-alpha normal map sources as
// BC1 (which would understate their true GPU footprint by 2x).
//
// Accepts both the legacy "NormalMap" and the newer "NormalMapContent"
// Content-typed property introduced alongside SurfaceAppearance's
// *MapContent properties. Missing either name caused the report's BC3
// tally to drop normal-map pixels entirely.
func IsNormalMapProperty(propertyName string) bool {
	normalized := strings.ToLower(strings.TrimSpace(propertyName))
	return normalized == "normalmap" || normalized == "normalmapcontent"
}

// ClassifyAsBC3 reports whether Roblox would upload this texture as BC3.
// Normal map slots always go BC3 (DXT5nm). For every other slot, BC3 is
// picked only when the source has at least one non-opaque alpha pixel.
func ClassifyAsBC3(hasAlphaChannel bool, nonOpaqueAlphaPixels int64, propertyName string) bool {
	if IsNormalMapProperty(propertyName) {
		return true
	}
	return hasAlphaChannel && nonOpaqueAlphaPixels > 0
}

// IsWastefulBC3 reports whether a BC3-classified texture has so few
// non-opaque pixels (< WastefulBC3AlphaFraction of total) that it's
// probably not really using its alpha channel. Normal map slots always
// return false — BC3 is intentional there, and the "alpha" stores normal
// X data rather than transparency.
func IsWastefulBC3(hasAlphaChannel bool, nonOpaqueAlphaPixels, totalPixels int64, propertyName string) bool {
	if IsNormalMapProperty(propertyName) {
		return false
	}
	if !ClassifyAsBC3(hasAlphaChannel, nonOpaqueAlphaPixels, propertyName) || totalPixels <= 0 {
		return false
	}
	return float64(nonOpaqueAlphaPixels)/float64(totalPixels) < WastefulBC3AlphaFraction
}

// EstimateGPUTextureBytesFor computes the GPU VRAM footprint for one texture
// given its pixel count, alpha classification, and the property slot the
// texture is bound to. Includes the full mip chain.
func EstimateGPUTextureBytesFor(pixelCount int64, hasAlphaChannel bool, nonOpaqueAlphaPixels int64, propertyName string) int64 {
	if pixelCount <= 0 {
		return 0
	}
	bytesPerPixel := bc1BytesPerPixel
	if ClassifyAsBC3(hasAlphaChannel, nonOpaqueAlphaPixels, propertyName) {
		bytesPerPixel = bc3BytesPerPixel
	}
	return int64(math.Round(float64(pixelCount) * bytesPerPixel * gpuMipChainFactor))
}

// ScanResultGPUMemoryBytes returns the estimated on-GPU footprint (including
// the full mip chain) for the texture this scan result refers to.
func ScanResultGPUMemoryBytes(row ScanResult) int64 {
	return EstimateGPUTextureBytesFor(row.PixelCount, row.HasAlphaChannel, row.NonOpaqueAlphaPixels, row.PropertyName)
}

// ScanResultGPUMemoryFormatLabel returns the compact compression name:
//   - "BC1" when Roblox stores as BC1 (no alpha or all-opaque alpha)
//   - "BC3 (normal)" when the slot is a NormalMap (Roblox uses DXT5nm)
//   - "BC3" when Roblox stores as BC3 with meaningful alpha coverage
//   - "BC3 (N% alpha)" when BC3 but below the wasteful threshold
func ScanResultGPUMemoryFormatLabel(row ScanResult) string {
	if row.PixelCount <= 0 {
		return ""
	}
	if IsNormalMapProperty(row.PropertyName) {
		return "BC3 (normal)"
	}
	if !ClassifyAsBC3(row.HasAlphaChannel, row.NonOpaqueAlphaPixels, row.PropertyName) {
		return "BC1"
	}
	if IsWastefulBC3(row.HasAlphaChannel, row.NonOpaqueAlphaPixels, row.PixelCount, row.PropertyName) {
		pct := float64(row.NonOpaqueAlphaPixels) / float64(row.PixelCount) * 100
		return fmt.Sprintf("BC3 (%.1f%% alpha)", pct)
	}
	return "BC3"
}

// FormatScanResultGPUMemory produces a single display string in the form
// "1.33 MB (BC3)", or "-" for non-texture rows.
func FormatScanResultGPUMemory(row ScanResult) string {
	bytes := ScanResultGPUMemoryBytes(row)
	if bytes <= 0 {
		return "-"
	}
	return fmt.Sprintf("%s (%s)", format.FormatSizeAuto64(bytes), ScanResultGPUMemoryFormatLabel(row))
}
