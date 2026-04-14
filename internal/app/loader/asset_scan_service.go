package loader

import (
	"fmt"
	"strings"

	"joxblox/internal/heatmap"
)

func LoadAssetPreview(assetID int64) (*AssetPreviewResult, error) {
	return LoadAssetPreviewWithTrace(assetID, nil)
}

func LoadAssetPreviewWithTrace(assetID int64, trace *AssetRequestTrace) (*AssetPreviewResult, error) {
	return LoadBestImageInfoWithOptionsAndTrace(assetID, false, trace)
}

func loadScanPreview(hit ScanHit) (*AssetPreviewResult, error) {
	return LoadAssetStatsPreviewForReference(hit.AssetID, hit.AssetInput)
}

func loadScanPreviewWithTrace(hit ScanHit, trace *AssetRequestTrace) (*AssetPreviewResult, error) {
	return LoadAssetStatsPreviewForReferenceWithTrace(hit.AssetID, hit.AssetInput, trace)
}

func LoadAssetStatsOnly(assetID int64) (*AssetPreviewResult, error) {
	return LoadBestImageInfoWithOptions(assetID, true)
}

func LoadScanResult(hit ScanHit) (ScanResult, error) {
	previewResult, err := loadScanPreview(hit)
	if err != nil {
		return ScanResult{}, err
	}
	result := BuildBaseScanResultFromHit(hit)
	return ApplyPreviewToScanResult(result, previewResult), nil
}

func LoadScanResultWithRequestSource(hit ScanHit) (ScanResult, error, heatmap.RequestSource) {
	trace := &AssetRequestTrace{}
	previewResult, err := loadScanPreviewWithTrace(hit, trace)
	if err != nil {
		return ScanResult{}, err, trace.ClassifyRequestSource()
	}
	result := BuildBaseScanResultFromHit(hit)
	return ApplyPreviewToScanResult(result, previewResult), nil, trace.ClassifyRequestSource()
}

func thumbnailTypeNameFromScanInput(assetID int64, assetInput string) string {
	loadRequest, err := BuildSingleAssetLoadRequest(assetID, assetInput)
	if err != nil || loadRequest.ThumbnailRequest == nil {
		return ""
	}
	return "Thumbnail"
}

func CompareScanResults(leftResult ScanResult, rightResult ScanResult, sortField string) int {
	switch sortField {
	case "Asset ID":
		return CompareInt64(leftResult.AssetID, rightResult.AssetID)
	case "Use Count":
		return CompareInt(leftResult.UseCount, rightResult.UseCount)
	case "Side":
		return strings.Compare(leftResult.Side, rightResult.Side)
	case "Width":
		return CompareInt(leftResult.Width, rightResult.Width)
	case "Height":
		return CompareInt(leftResult.Height, rightResult.Height)
	case "Dimensions":
		leftArea := leftResult.Width * leftResult.Height
		rightArea := rightResult.Width * rightResult.Height
		return CompareInt(leftArea, rightArea)
	case "Type":
		typeCompare := strings.Compare(leftResult.AssetTypeName, rightResult.AssetTypeName)
		if typeCompare != 0 {
			return typeCompare
		}
		return CompareInt(leftResult.AssetTypeID, rightResult.AssetTypeID)
	case "State":
		return strings.Compare(leftResult.State, rightResult.State)
	case "Source":
		return strings.Compare(leftResult.Source, rightResult.Source)
	case "Triangles":
		return CompareUint32(leftResult.MeshNumFaces, rightResult.MeshNumFaces)
	case "Total Byte Size":
		return CompareInt(leftResult.TotalBytesSize, rightResult.TotalBytesSize)
	case "Texture Bytes":
		return CompareInt(leftResult.TextureBytes, rightResult.TextureBytes)
	case "Texture Pixels":
		return CompareInt64(leftResult.PixelCount, rightResult.PixelCount)
	case "B/stud²":
		return CompareFloat64(leftResult.LargeTextureScore, rightResult.LargeTextureScore)
	case "Mesh Bytes":
		return CompareInt(leftResult.MeshBytes, rightResult.MeshBytes)
	case "Mesh Triangles":
		return CompareUint32(leftResult.MeshNumFaces, rightResult.MeshNumFaces)
	case "Instance Type":
		return strings.Compare(leftResult.InstanceType, rightResult.InstanceType)
	case "Property":
		return strings.Compare(leftResult.PropertyName, rightResult.PropertyName)
	case "Instance Path":
		return strings.Compare(leftResult.InstancePath, rightResult.InstancePath)
	case "World Position":
		leftPosition := fmt.Sprintf("%.4f:%.4f:%.4f", leftResult.WorldX, leftResult.WorldY, leftResult.WorldZ)
		rightPosition := fmt.Sprintf("%.4f:%.4f:%.4f", rightResult.WorldX, rightResult.WorldY, rightResult.WorldZ)
		return strings.Compare(leftPosition, rightPosition)
	case "Asset SHA256":
		return strings.Compare(leftResult.FileSHA256, rightResult.FileSHA256)
	default:
		return CompareInt(leftResult.BytesSize, rightResult.BytesSize)
	}
}

func CompareInt(left int, right int) int {
	if left < right {
		return -1
	}
	if left > right {
		return 1
	}
	return 0
}

func CompareUint32(left uint32, right uint32) int {
	if left < right {
		return -1
	}
	if left > right {
		return 1
	}
	return 0
}

func CompareFloat64(left float64, right float64) int {
	if left < right {
		return -1
	}
	if left > right {
		return 1
	}
	return 0
}

func CompareInt64(left int64, right int64) int {
	if left < right {
		return -1
	}
	if left > right {
		return 1
	}
	return 0
}
