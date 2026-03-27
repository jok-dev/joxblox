package app

import (
	"strings"

	"fyne.io/fyne/v2"
)

type assetReferenceContext struct {
	FilePath              string
	FileSHA256            string
	UseCount              int
	ReferenceInstanceType string
	ReferencePropertyName string
	ReferenceInstancePath string
}

type assetViewData struct {
	AssetID               int64
	FilePath              string
	FileSHA256            string
	UseCount              int
	PreviewImageInfo      *imageInfo
	StatsInfo             *imageInfo
	TotalBytesSize        int
	SourceDescription     string
	StateDescription      string
	WarningMessage        string
	AssetDeliveryRawJSON  string
	ThumbnailRawJSON      string
	EconomyRawJSON        string
	RustyAssetToolRawJSON string
	ReferencedAssetIDs    []int64
	ReferenceInstanceType string
	ReferencePropertyName string
	ReferenceInstancePath string
	AssetTypeID           int
	AssetTypeName         string
	DownloadBytes         []byte
	DownloadFileName      string
	DownloadIsOriginal    bool
}

func previewSHA256(previewResult *assetPreviewResult) string {
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

func buildBaseScanResultFromHit(hit scanHit) scanResult {
	return scanResult{
		AssetID:      hit.AssetID,
		AssetInput:   strings.TrimSpace(hit.AssetInput),
		UseCount:     hit.UseCount,
		FilePath:     hit.FilePath,
		InstanceType: strings.TrimSpace(hit.InstanceType),
		InstanceName: strings.TrimSpace(hit.InstanceName),
		InstancePath: strings.TrimSpace(hit.InstancePath),
		PropertyName: strings.TrimSpace(hit.PropertyName),
	}
}

func buildFailedScanResultFromHit(hit scanHit, loadErr error) scanResult {
	result := buildBaseScanResultFromHit(hit)
	result.Source = failedScanRowSource
	result.State = failedScanRowState
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

func applyPreviewToScanResult(result scanResult, previewResult *assetPreviewResult) scanResult {
	statsInfo := previewResult.Stats
	if statsInfo == nil {
		statsInfo = previewResult.Image
	}
	if statsInfo == nil {
		statsInfo = &imageInfo{}
	}
	resource := (*fyne.StaticResource)(nil)
	if previewResult.Image != nil {
		resource = previewResult.Image.Resource
	}

	var meshFaces, meshVerts uint32
	if isMeshAssetType(previewResult.AssetTypeID) && len(previewResult.DownloadBytes) > 0 {
		if meshInfo, meshErr := parseMeshHeader(previewResult.DownloadBytes); meshErr == nil {
			meshFaces = meshInfo.NumFaces
			meshVerts = meshInfo.NumVerts
		}
	}

	warning := isThumbnailFallback(previewResult.Source) && !isCompletedState(previewResult.State)
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
	result.Resource = resource
	result.DownloadBytes = append([]byte(nil), previewResult.DownloadBytes...)
	result.DownloadFileName = previewResult.DownloadFileName
	result.DownloadIsOriginal = previewResult.DownloadIsOriginal
	return result
}

func scanResultToPreviewResult(result scanResult) *assetPreviewResult {
	return &assetPreviewResult{
		Image: &imageInfo{Resource: result.Resource, SHA256: result.FileSHA256},
		Stats: &imageInfo{
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

func buildAssetViewDataFromPreview(assetID int64, previewResult *assetPreviewResult, context assetReferenceContext) assetViewData {
	if previewResult == nil {
		previewResult = &assetPreviewResult{}
	}
	fileSHA256 := strings.TrimSpace(context.FileSHA256)
	if fileSHA256 == "" {
		fileSHA256 = previewSHA256(previewResult)
	}
	return assetViewData{
		AssetID:               assetID,
		FilePath:              strings.TrimSpace(context.FilePath),
		FileSHA256:            fileSHA256,
		UseCount:              context.UseCount,
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
}

func buildExplorerSelectionReferenceContext(state *assetExplorerState, selectedAssetID int64) assetReferenceContext {
	if state == nil {
		return assetReferenceContext{}
	}
	selectedRow, found := state.getRow(selectedAssetID)
	if !found {
		return assetReferenceContext{}
	}
	referenceInstancePath := state.getInstancePath(selectedAssetID)
	if referenceInstancePath == "" {
		referenceInstancePath = selectedRow.InstancePath
	}
	if referenceInstancePath == "" {
		referenceInstancePath = selectedRow.InstanceName
	}
	return assetReferenceContext{
		ReferenceInstanceType: selectedRow.InstanceType,
		ReferencePropertyName: selectedRow.PropertyName,
		ReferenceInstancePath: referenceInstancePath,
	}
}

func buildRootScanReferenceContext(rows []scanResult, selectedAssetID int64, selectedAssetInput string, selectedFilePath string, fallbackFileSHA string) assetReferenceContext {
	context := assetReferenceContext{
		FilePath:   selectedFilePath,
		FileSHA256: strings.TrimSpace(fallbackFileSHA),
	}
	for _, row := range rows {
		if row.AssetID != selectedAssetID || row.FilePath != selectedFilePath {
			continue
		}
		if scanAssetReferenceKey(row.AssetID, row.AssetInput) != scanAssetReferenceKey(selectedAssetID, selectedAssetInput) {
			continue
		}
		context.UseCount = row.UseCount
		context.ReferenceInstanceType = row.InstanceType
		context.ReferencePropertyName = row.PropertyName
		context.ReferenceInstancePath = firstNonEmptyString(row.InstancePath, row.InstanceName)
		if strings.TrimSpace(row.FileSHA256) != "" {
			context.FileSHA256 = strings.TrimSpace(row.FileSHA256)
		}
		break
	}
	return context
}
