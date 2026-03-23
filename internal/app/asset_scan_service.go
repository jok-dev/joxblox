package app

import (
	"strings"
	"time"

	"fyne.io/fyne/v2"
)

type scanResult struct {
	AssetID              int64
	UseCount             int
	FilePath             string
	FileSHA256           string
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
	AssetDeliveryJSON    string
	ThumbnailJSON        string
	EconomyJSON          string
	RustExtractorJSON    string
	ReferencedAssetIDs   []int64
	ChildAssets          []childAssetInfo
	TotalBytesSize       int
	Resource             *fyne.StaticResource
	DownloadBytes        []byte
	DownloadFileName     string
	DownloadIsOriginal   bool
}

func loadAssetPreview(assetID int64) (*assetPreviewResult, error) {
	return loadBestImageInfo(assetID)
}

func loadScanResult(hit scanHit) (scanResult, error) {
	previewResult, err := loadAssetPreview(hit.AssetID)
	if err != nil {
		return scanResult{}, err
	}
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

	warning := isThumbnailFallback(previewResult.Source) && !isCompletedState(previewResult.State)
	return scanResult{
		AssetID:              hit.AssetID,
		UseCount:             hit.UseCount,
		FilePath:             hit.FilePath,
		FileSHA256:           statsInfo.SHA256,
		Source:               previewResult.Source,
		State:                previewResult.State,
		Width:                statsInfo.Width,
		Height:               statsInfo.Height,
		Duration:             statsInfo.Duration,
		BytesSize:            statsInfo.BytesSize,
		RecompressedPNGSize:  statsInfo.RecompressedPNGByteSize,
		RecompressedJPEGSize: statsInfo.RecompressedJPEGByteSize,
		Format:               statsInfo.Format,
		ContentType:          statsInfo.ContentType,
		AssetTypeID:          previewResult.AssetTypeID,
		AssetTypeName:        previewResult.AssetTypeName,
		Warning:              warning,
		WarningCause:         previewResult.WarningMessage,
		AssetDeliveryJSON:    previewResult.AssetDeliveryJSON,
		ThumbnailJSON:        previewResult.ThumbnailJSON,
		EconomyJSON:          previewResult.EconomyJSON,
		RustExtractorJSON:    previewResult.RustExtractorJSON,
		ReferencedAssetIDs:   previewResult.ReferencedAssetIDs,
		ChildAssets:          previewResult.ChildAssets,
		TotalBytesSize:       previewResult.TotalBytesSize,
		Resource:             resource,
		DownloadBytes:        append([]byte(nil), previewResult.DownloadBytes...),
		DownloadFileName:     previewResult.DownloadFileName,
		DownloadIsOriginal:   previewResult.DownloadIsOriginal,
	}, nil
}

func compareScanResults(leftResult scanResult, rightResult scanResult, sortField string) int {
	switch sortField {
	case "Asset ID":
		return compareInt64(leftResult.AssetID, rightResult.AssetID)
	case "Use Count":
		return compareInt(leftResult.UseCount, rightResult.UseCount)
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

func compareInt64(left int64, right int64) int {
	if left < right {
		return -1
	}
	if left > right {
		return 1
	}
	return 0
}
