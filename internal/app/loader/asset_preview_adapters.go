package loader

import (
	"bytes"
	"fmt"
	"image"
	"path/filepath"
	"strings"

	"joxblox/internal/extractor"
	"joxblox/internal/roblox"
	"joxblox/internal/roblox/mesh"

	"fyne.io/fyne/v2"
)

// BuildLocalImagePreview wraps a local image file's bytes in an
// AssetPreviewResult so the Single Asset tab (and any other asset-view
// surface) can render and operate on it as if it were a Roblox image
// asset — driving the same downscale/preview-variant pipeline. fileName
// becomes the resource label and the download filename. Pass any byte
// payload that Go's image package can decode (PNG, JPEG, GIF, etc.).
func BuildLocalImagePreview(fileName string, fileBytes []byte) (*AssetPreviewResult, error) {
	if len(fileBytes) == 0 {
		return nil, fmt.Errorf("image file is empty")
	}

	imageConfig, imageFormat, decodeErr := image.DecodeConfig(bytes.NewReader(fileBytes))
	if decodeErr != nil {
		return nil, fmt.Errorf("decode image: %w", decodeErr)
	}

	resourceName := strings.TrimSpace(filepath.Base(fileName))
	if resourceName == "" {
		resourceName = "local_image." + imageFormat
	}

	analysis, analysisErr := analyzeImage(fileBytes)
	if analysisErr != nil {
		analysis = imageAnalysis{}
	}

	imageInfo := &ImageInfo{
		Resource:                 fyne.NewStaticResource(resourceName, fileBytes),
		Width:                    imageConfig.Width,
		Height:                   imageConfig.Height,
		BytesSize:                len(fileBytes),
		RecompressedPNGByteSize:  analysis.RecompressedPNGBytes,
		RecompressedJPEGByteSize: analysis.RecompressedJPEGBytes,
		Format:                   strings.ToUpper(imageFormat),
		ContentType:              "image/" + strings.ToLower(imageFormat),
		SHA256:                   ComputeSHA256Hex(fileBytes),
		HasAlphaChannel:          analysis.HasAlphaChannel,
		NonOpaqueAlphaPixels:     analysis.NonOpaqueAlphaPixels,
	}

	return &AssetPreviewResult{
		Image:              imageInfo,
		Stats:              imageInfo,
		ReferencedAssetIDs: nil,
		ChildAssets:        nil,
		TotalBytesSize:     len(fileBytes),
		Source:             "Local file",
		State:              "Local",
		AssetTypeID:        roblox.AssetTypeImage,
		AssetTypeName:      "Image",
		DownloadBytes:      append([]byte(nil), fileBytes...),
		DownloadFileName:   resourceName,
		// DownloadIsOriginal=true gates the variant dropdown off (since real
		// Roblox assets shouldn't pretend a downscaled PNG is "the upload").
		// For a local image the variants ARE the point — the "Original" entry
		// is still prepended to the list, so the user keeps a 1:1 download
		// option alongside the resized previews.
		DownloadIsOriginal: false,
	}, nil
}

func PreviewSHA256(previewResult *AssetPreviewResult) string {
	if previewResult == nil {
		return ""
	}
	if previewResult.Stats != nil && strings.TrimSpace(previewResult.Stats.SHA256) != "" {
		return strings.TrimSpace(previewResult.Stats.SHA256)
	}
	if previewResult.Image != nil && strings.TrimSpace(previewResult.Image.SHA256) != "" {
		return strings.TrimSpace(previewResult.Image.SHA256)
	}
	return ""
}

func BuildBaseScanResultFromHit(hit ScanHit) ScanResult {
	return ScanResult{
		AssetID:            hit.AssetID,
		AssetInput:         strings.TrimSpace(hit.AssetInput),
		UseCount:           hit.UseCount,
		FilePath:           hit.FilePath,
		InstanceType:       strings.TrimSpace(hit.InstanceType),
		InstanceName:       strings.TrimSpace(hit.InstanceName),
		InstancePath:       strings.TrimSpace(hit.InstancePath),
		PropertyName:       strings.TrimSpace(hit.PropertyName),
		SceneSurfaceArea:   hit.SceneSurfaceArea,
		LargestSurfacePath: strings.TrimSpace(hit.LargestSurfacePath),
	}
}

func BuildFailedScanResultFromHit(hit ScanHit, loadErr error) ScanResult {
	result := BuildBaseScanResultFromHit(hit)
	result.Source = FailedScanRowSource
	result.State = FailedScanRowState
	result.Format = "-"
	result.ContentType = "-"
	result.Warning = true
	if loadErr != nil {
		result.WarningCause = loadErr.Error()
	}
	result.AssetTypeName = "Unknown"
	if thumbnailTypeName := thumbnailTypeNameFromScanInput(hit.AssetID, hit.AssetInput); thumbnailTypeName != "" {
		result.AssetTypeName = thumbnailTypeName
	}
	return result
}

func ApplyPreviewToScanResult(result ScanResult, previewResult *AssetPreviewResult) ScanResult {
	statsInfo := ResolveStatsInfo(previewResult.Stats, previewResult.Image)
	resource := (*fyne.StaticResource)(nil)
	if previewResult.Image != nil {
		resource = previewResult.Image.Resource
	}

	var meshFaces, meshVerts uint32
	var meshVersion string
	if mesh.IsMeshAssetType(previewResult.AssetTypeID) && len(previewResult.DownloadBytes) > 0 {
		if meshInfo, meshErr := mesh.ParseHeader(previewResult.DownloadBytes); meshErr == nil {
			meshFaces = meshInfo.NumFaces
			meshVerts = meshInfo.NumVerts
			meshVersion = meshInfo.Version
		}
	}

	warning := roblox.IsThumbnailFallback(previewResult.Source) && !roblox.IsCompletedState(previewResult.State)
	result.FileSHA256 = statsInfo.SHA256
	result.Source = previewResult.Source
	result.State = previewResult.State
	result.Width = statsInfo.Width
	result.Height = statsInfo.Height
	result.Duration = statsInfo.Duration
	result.BytesSize = statsInfo.BytesSize
	result.RecompressedPNGSize = statsInfo.RecompressedPNGByteSize
	result.RecompressedJPEGSize = statsInfo.RecompressedJPEGByteSize
	result.Format = statsInfo.Format
	result.ContentType = statsInfo.ContentType
	result.AssetTypeID = previewResult.AssetTypeID
	result.AssetTypeName = previewResult.AssetTypeName
	result.Warning = warning
	result.WarningCause = previewResult.WarningMessage
	result.AssetDeliveryJSON = previewResult.AssetDeliveryJSON
	result.ThumbnailJSON = previewResult.ThumbnailJSON
	result.EconomyJSON = previewResult.EconomyJSON
	result.RustyAssetToolJSON = previewResult.RustyAssetToolJSON
	result.ReferencedAssetIDs = previewResult.ReferencedAssetIDs
	result.ChildAssets = previewResult.ChildAssets
	result.TotalBytesSize = previewResult.TotalBytesSize
	result.MeshNumFaces = meshFaces
	result.MeshNumVerts = meshVerts
	result.MeshVersion = meshVersion
	result.MeshBytes = 0
	if meshFaces > 0 && result.TotalBytesSize > 0 {
		result.MeshBytes = result.TotalBytesSize
	}
	// Source-of-truth pixel/alpha info comes from statsInfo
	// (= previewResult.Stats with Image as a fallback). Don't overwrite
	// with previewResult.Image alone — when AssetDelivery falls back to
	// a thumbnail, Image's dimensions are the thumbnail's (~150×150)
	// instead of the asset's, which made Shown GPU Memory swing wildly
	// between runs depending on which fetch path won.
	if statsInfo.Width > 0 && statsInfo.Height > 0 {
		result.PixelCount = int64(statsInfo.Width * statsInfo.Height)
		result.TextureBytes = statsInfo.BytesSize
		result.HasAlphaChannel = statsInfo.HasAlphaChannel
		result.NonOpaqueAlphaPixels = statsInfo.NonOpaqueAlphaPixels
	}
	result.Resource = resource
	result.DownloadBytes = append([]byte(nil), previewResult.DownloadBytes...)
	result.DownloadFileName = previewResult.DownloadFileName
	result.DownloadIsOriginal = previewResult.DownloadIsOriginal
	return RefreshLargeTextureMetrics(result)
}

func ScanResultToPreviewResult(result ScanResult) *AssetPreviewResult {
	return &AssetPreviewResult{
		Image: &ImageInfo{Resource: result.Resource, SHA256: result.FileSHA256},
		Stats: &ImageInfo{
			Width:                    result.Width,
			Height:                   result.Height,
			Duration:                 result.Duration,
			BytesSize:                result.BytesSize,
			RecompressedPNGByteSize:  result.RecompressedPNGSize,
			RecompressedJPEGByteSize: result.RecompressedJPEGSize,
			Format:                   result.Format,
			ContentType:              result.ContentType,
			SHA256:                   result.FileSHA256,
		},
		ReferencedAssetIDs: result.ReferencedAssetIDs,
		ChildAssets:        result.ChildAssets,
		TotalBytesSize:     result.TotalBytesSize,
		Source:             result.Source,
		State:              result.State,
		WarningMessage:     result.WarningCause,
		AssetDeliveryJSON:  result.AssetDeliveryJSON,
		ThumbnailJSON:      result.ThumbnailJSON,
		EconomyJSON:        result.EconomyJSON,
		RustyAssetToolJSON: result.RustyAssetToolJSON,
		AssetTypeID:        result.AssetTypeID,
		AssetTypeName:      result.AssetTypeName,
		DownloadBytes:      append([]byte(nil), result.DownloadBytes...),
		DownloadFileName:   result.DownloadFileName,
		DownloadIsOriginal: result.DownloadIsOriginal,
	}
}

func BuildAssetViewDataFromPreview(assetID int64, previewResult *AssetPreviewResult, context AssetReferenceContext) AssetViewData {
	if previewResult == nil {
		previewResult = &AssetPreviewResult{}
	}
	fileSHA256 := strings.TrimSpace(context.FileSHA256)
	if fileSHA256 == "" {
		fileSHA256 = PreviewSHA256(previewResult)
	}
	data := AssetViewData{
		AssetID:               assetID,
		FilePath:              strings.TrimSpace(context.FilePath),
		FileSHA256:            fileSHA256,
		UseCount:              context.UseCount,
		SceneSurfaceArea:      context.SceneSurfaceArea,
		LargestSurfacePath:    strings.TrimSpace(context.LargestSurfacePath),
		LargeTextureScore:     context.LargeTextureScore,
		PreviewImageInfo:      previewResult.Image,
		StatsInfo:             previewResult.Stats,
		TotalBytesSize:        previewResult.TotalBytesSize,
		SourceDescription:     previewResult.Source,
		StateDescription:      previewResult.State,
		WarningMessage:        previewResult.WarningMessage,
		AssetDeliveryRawJSON:  previewResult.AssetDeliveryJSON,
		ThumbnailRawJSON:      previewResult.ThumbnailJSON,
		EconomyRawJSON:        previewResult.EconomyJSON,
		RustyAssetToolRawJSON: previewResult.RustyAssetToolJSON,
		ReferencedAssetIDs:    append([]int64(nil), previewResult.ReferencedAssetIDs...),
		ReferenceInstanceType: strings.TrimSpace(context.ReferenceInstanceType),
		ReferencePropertyName: strings.TrimSpace(context.ReferencePropertyName),
		ReferenceInstancePath: strings.TrimSpace(context.ReferenceInstancePath),
		AssetTypeID:           previewResult.AssetTypeID,
		AssetTypeName:         previewResult.AssetTypeName,
		DownloadBytes:         append([]byte(nil), previewResult.DownloadBytes...),
		DownloadFileName:      previewResult.DownloadFileName,
		DownloadIsOriginal:    previewResult.DownloadIsOriginal,
	}
	return data
}

func BuildRootScanReferenceContext(rows []ScanResult, selectedAssetID int64, selectedAssetInput string, selectedFilePath string, fallbackFileSHA string) AssetReferenceContext {
	context := AssetReferenceContext{
		FilePath:   selectedFilePath,
		FileSHA256: strings.TrimSpace(fallbackFileSHA),
	}
	for _, row := range rows {
		if row.AssetID != selectedAssetID || row.FilePath != selectedFilePath {
			continue
		}
		if extractor.AssetReferenceKey(row.AssetID, row.AssetInput) != extractor.AssetReferenceKey(selectedAssetID, selectedAssetInput) {
			continue
		}
		context.UseCount = row.UseCount
		context.SceneSurfaceArea = row.SceneSurfaceArea
		context.LargestSurfacePath = row.LargestSurfacePath
		context.LargeTextureScore = row.LargeTextureScore
		context.ReferenceInstanceType = row.InstanceType
		context.ReferencePropertyName = row.PropertyName
		context.ReferenceInstancePath = FirstNonEmptyString(row.InstancePath, row.InstanceName)
		if strings.TrimSpace(row.FileSHA256) != "" {
			context.FileSHA256 = strings.TrimSpace(row.FileSHA256)
		}
		break
	}
	return context
}
