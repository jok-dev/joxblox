package loader

import (
	"strings"

	"joxblox/internal/heatmap"
	"joxblox/internal/roblox/mesh"
)

func BuildAssetStatsFromPreview(assetID int64, previewResult *AssetPreviewResult) heatmap.AssetStats {
	stats := heatmap.AssetStats{AssetID: assetID}
	if previewResult == nil {
		return stats
	}
	stats.AssetTypeID = previewResult.AssetTypeID
	stats.AssetTypeName = strings.TrimSpace(previewResult.AssetTypeName)
	// Stats represents the source asset's dimensions (stable across
	// runs). Image is whatever preview actually loaded — could be a
	// smaller thumbnail when AssetDelivery falls back. Prefer Stats so
	// PixelCount / Width / Height reflect the source, not the preview.
	statsInfo := previewResult.Stats
	if statsInfo == nil {
		statsInfo = previewResult.Image
	}
	if statsInfo != nil {
		stats.TotalBytes = statsInfo.BytesSize
	}
	if statsInfo != nil && statsInfo.Width > 0 && statsInfo.Height > 0 {
		stats.TextureBytes = statsInfo.BytesSize
		stats.PixelCount = int64(statsInfo.Width * statsInfo.Height)
		stats.Width = statsInfo.Width
		stats.Height = statsInfo.Height
		stats.HasAlphaChannel = statsInfo.HasAlphaChannel
		stats.NonOpaqueAlphaPixels = statsInfo.NonOpaqueAlphaPixels
	}
	if mesh.IsMeshAssetType(previewResult.AssetTypeID) && len(previewResult.DownloadBytes) > 0 {
		stats.MeshBytes = stats.TotalBytes
		if meshInfo, meshErr := mesh.ParseHeader(previewResult.DownloadBytes); meshErr == nil {
			stats.TriangleCount = meshInfo.NumFaces
		}
	}
	return stats
}
