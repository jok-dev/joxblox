package loader

import (
	"fmt"
	"strconv"
	"strings"

	"joxblox/internal/format"
	"joxblox/internal/roblox"
	"joxblox/internal/roblox/mesh"
)

// PopulateAssetViewDisplayFields derives the pre-formatted *Display strings on
// AssetViewData so MetadataSpec.ViewExtract functions can be pure readers.
// Safe to call on a zero-valued struct (everything becomes empty).
func PopulateAssetViewDisplayFields(data *AssetViewData) {
	if data == nil {
		return
	}
	statsInfo := ResolveStatsInfo(data.StatsInfo, data.PreviewImageInfo)

	data.DimensionsLabel, data.DimensionsDisplay = buildDimensionsDisplay(data, statsInfo)
	data.SelfSizeDisplay = formatSizeOrEmpty(statsInfo.BytesSize)
	totalBytesSize := data.TotalBytesSize
	if totalBytesSize <= 0 {
		totalBytesSize = statsInfo.BytesSize
	}
	data.TotalSizeDisplay = formatSizeOrEmpty(totalBytesSize)
	data.FormatDisplay = strings.TrimSpace(statsInfo.Format)
	data.ContentTypeDisplay = strings.TrimSpace(statsInfo.ContentType)
	data.AssetTypeDisplay = buildAssetTypeDisplay(data.AssetTypeName, data.AssetTypeID)

	data.ReferencedCount = len(data.ReferencedAssetIDs)
	if data.AssetID > 0 {
		data.ReferencedDisplay = strconv.Itoa(data.ReferencedCount)
	} else {
		data.ReferencedDisplay = ""
	}

	data.UseCountDisplay = ""
	if data.UseCount > 0 {
		data.UseCountDisplay = strconv.Itoa(data.UseCount)
	}

	data.InGameSizeDisplay = buildInGameSizeDisplay(data)
	data.FailureReasonText = strings.TrimSpace(data.WarningMessage)
	data.FileDisplay = strings.TrimSpace(data.FilePath)
	data.FileSHA256Display = strings.TrimSpace(data.FileSHA256)
	data.SourceDisplay = buildSourceDisplay(data.SourceDescription)
}

// ResolveStatsInfo picks the first non-nil ImageInfo, or returns a zero value
// so callers can read fields without nil checks.
func ResolveStatsInfo(primary *ImageInfo, fallback *ImageInfo) *ImageInfo {
	if primary != nil {
		return primary
	}
	if fallback != nil {
		return fallback
	}
	return &ImageInfo{}
}

func formatSizeOrEmpty(bytesSize int) string {
	if bytesSize <= 0 {
		return ""
	}
	return format.FormatSizeAuto(bytesSize)
}

func buildAssetTypeDisplay(name string, id int) string {
	trimmedName := strings.TrimSpace(name)
	if trimmedName == "" {
		return ""
	}
	if id > 0 {
		return fmt.Sprintf("%s (%d)", trimmedName, id)
	}
	return trimmedName
}

func buildDimensionsDisplay(data *AssetViewData, statsInfo *ImageInfo) (label string, value string) {
	if mesh.IsMeshAssetType(data.AssetTypeID) {
		label = "Mesh Info"
		if len(data.DownloadBytes) == 0 {
			return label, ""
		}
		info, err := mesh.ParseHeader(data.DownloadBytes)
		if err != nil {
			return label, ""
		}
		return label, mesh.FormatInfo(info)
	}
	label = "Dimensions"
	if IsAudioContent != nil && IsAudioContent(data.AssetTypeID, statsInfo.ContentType) {
		return label, ""
	}
	if statsInfo.Width > 0 && statsInfo.Height > 0 {
		return label, format.FormatDimensions(statsInfo.Width, statsInfo.Height)
	}
	return label, ""
}

func buildInGameSizeDisplay(data *AssetViewData) string {
	if data.LargeTextureScore <= 0 || data.SceneSurfaceArea <= 0 {
		return ""
	}
	text := fmt.Sprintf(
		"%s (%s surface)",
		FormatLargeTextureScore(data.LargeTextureScore),
		FormatSceneSurfaceArea(data.SceneSurfaceArea),
	)
	if data.LargestSurfacePath != "" {
		text = fmt.Sprintf("%s at %s", text, data.LargestSurfacePath)
	}
	return text
}

func buildSourceDisplay(source string) string {
	trimmed := strings.TrimSpace(source)
	if trimmed == "" {
		return ""
	}
	if roblox.IsThumbnailFallback(trimmed) {
		return "⚠ " + trimmed
	}
	return trimmed
}
