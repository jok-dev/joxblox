package app

import (
	"strings"

	"joxblox/internal/heatmap"
	"joxblox/internal/roblox/mesh"
)

func buildAssetStatsFromPreview(assetID int64, previewResult *assetPreviewResult) heatmap.AssetStats {
	stats := heatmap.AssetStats{AssetID: assetID}
	if previewResult == nil {
		return stats
	}
	stats.AssetTypeID = previewResult.AssetTypeID
	stats.AssetTypeName = strings.TrimSpace(previewResult.AssetTypeName)
	statsInfo := previewResult.Stats
	if statsInfo == nil {
		statsInfo = previewResult.Image
	}
	if statsInfo != nil {
		stats.TotalBytes = statsInfo.BytesSize
	}
	if previewResult.Image != nil && previewResult.Image.Width > 0 && previewResult.Image.Height > 0 {
		stats.TextureBytes = previewResult.Image.BytesSize
		stats.PixelCount = int64(previewResult.Image.Width * previewResult.Image.Height)
	}
	if mesh.IsMeshAssetType(previewResult.AssetTypeID) && len(previewResult.DownloadBytes) > 0 {
		stats.MeshBytes = stats.TotalBytes
		if meshInfo, meshErr := mesh.ParseHeader(previewResult.DownloadBytes); meshErr == nil {
			stats.TriangleCount = meshInfo.NumFaces
		}
	}
	return stats
}
