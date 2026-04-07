package app

import (
	"fmt"
	"strings"
	"time"

	"fyne.io/fyne/v2"
)

type scanResult struct {
	AssetID              int64
	AssetInput           string
	Side                 string
	UseCount             int
	FilePath             string
	FileSHA256           string
	InstanceType         string
	InstanceName         string
	InstancePath         string
	PropertyName         string
	Source               string
	State                string
	Width                int
	Height               int
	Duration             time.Duration
	BytesSize            int
	RecompressedPNGSize  int
	RecompressedJPEGSize int
	Format               string
	ContentType          string
	AssetTypeID          int
	AssetTypeName        string
	Warning              bool
	WarningCause         string
	WorldX               float64
	WorldY               float64
	WorldZ               float64
	TextureBytes         int
	MeshBytes            int
	PixelCount           int64
	SceneSurfaceArea     float64
	LargestSurfacePath   string
	LargeTextureScore    float64
	AssetDeliveryJSON    string
	ThumbnailJSON        string
	EconomyJSON          string
	RustyAssetToolJSON   string
	ReferencedAssetIDs   []int64
	ChildAssets          []childAssetInfo
	TotalBytesSize       int
	MeshNumFaces         uint32
	MeshNumVerts         uint32
	Resource             *fyne.StaticResource
	DownloadBytes        []byte
	DownloadFileName     string
	DownloadIsOriginal   bool
}

func loadAssetPreview(assetID int64) (*assetPreviewResult, error) {
	return loadAssetPreviewWithTrace(assetID, nil)
}

func loadAssetPreviewWithTrace(assetID int64, trace *assetRequestTrace) (*assetPreviewResult, error) {
	return loadBestImageInfoWithOptionsAndTrace(assetID, false, trace)
}

func loadScanPreview(hit scanHit) (*assetPreviewResult, error) {
	return loadAssetStatsPreviewForReference(hit.AssetID, hit.AssetInput)
}

func loadScanPreviewWithTrace(hit scanHit, trace *assetRequestTrace) (*assetPreviewResult, error) {
	return loadAssetStatsPreviewForReferenceWithTrace(hit.AssetID, hit.AssetInput, trace)
}

func loadAssetStatsOnly(assetID int64) (*assetPreviewResult, error) {
	return loadBestImageInfoWithOptions(assetID, true)
}

func loadScanResult(hit scanHit) (scanResult, error) {
	previewResult, err := loadScanPreview(hit)
	if err != nil {
		return scanResult{}, err
	}
	result := buildBaseScanResultFromHit(hit)
	return applyPreviewToScanResult(result, previewResult), nil
}

func loadScanResultWithRequestSource(hit scanHit) (scanResult, error, heatmapAssetRequestSource) {
	trace := &assetRequestTrace{}
	previewResult, err := loadScanPreviewWithTrace(hit, trace)
	if err != nil {
		return scanResult{}, err, trace.classifyRequestSource()
	}
	result := buildBaseScanResultFromHit(hit)
	return applyPreviewToScanResult(result, previewResult), nil, trace.classifyRequestSource()
}

func thumbnailTypeNameFromScanInput(assetID int64, assetInput string) string {
	loadRequest, err := buildSingleAssetLoadRequest(assetID, assetInput)
	if err != nil || loadRequest.ThumbnailRequest == nil {
		return ""
	}
	return "Thumbnail"
}

func compareScanResults(leftResult scanResult, rightResult scanResult, sortField string) int {
	switch sortField {
	case "Asset ID":
		return compareInt64(leftResult.AssetID, rightResult.AssetID)
	case "Use Count":
		return compareInt(leftResult.UseCount, rightResult.UseCount)
	case "Side":
		return strings.Compare(leftResult.Side, rightResult.Side)
	case "Width":
		return compareInt(leftResult.Width, rightResult.Width)
	case "Height":
		return compareInt(leftResult.Height, rightResult.Height)
	case "Dimensions":
		leftArea := leftResult.Width * leftResult.Height
		rightArea := rightResult.Width * rightResult.Height
		return compareInt(leftArea, rightArea)
	case "Type":
		typeCompare := strings.Compare(leftResult.AssetTypeName, rightResult.AssetTypeName)
		if typeCompare != 0 {
			return typeCompare
		}
		return compareInt(leftResult.AssetTypeID, rightResult.AssetTypeID)
	case "State":
		return strings.Compare(leftResult.State, rightResult.State)
	case "Source":
		return strings.Compare(leftResult.Source, rightResult.Source)
	case "Triangles":
		return compareUint32(leftResult.MeshNumFaces, rightResult.MeshNumFaces)
	case "Total Byte Size":
		return compareInt(leftResult.TotalBytesSize, rightResult.TotalBytesSize)
	case "Texture Bytes":
		return compareInt(leftResult.TextureBytes, rightResult.TextureBytes)
	case "Texture Pixels":
		return compareInt64(leftResult.PixelCount, rightResult.PixelCount)
	case "Mesh Bytes":
		return compareInt(leftResult.MeshBytes, rightResult.MeshBytes)
	case "Mesh Triangles":
		return compareUint32(leftResult.MeshNumFaces, rightResult.MeshNumFaces)
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
		return compareInt(leftResult.BytesSize, rightResult.BytesSize)
	}
}

func compareInt(left int, right int) int {
	if left < right {
		return -1
	}
	if left > right {
		return 1
	}
	return 0
}

func compareUint32(left uint32, right uint32) int {
	if left < right {
		return -1
	}
	if left > right {
		return 1
	}
	return 0
}

func compareInt64(left int64, right int64) int {
	if left < right {
		return -1
	}
	if left > right {
		return 1
	}
	return 0
}
