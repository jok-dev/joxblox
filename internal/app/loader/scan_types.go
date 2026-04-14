package loader

import (
	"time"

	"fyne.io/fyne/v2"
)

const (
	FailedScanRowState  = "Failed"
	FailedScanRowSource = "Load Failed"
	ScanFilterAllOption = "All"
)

type ScanHit struct {
	AssetID            int64
	AssetInput         string
	FilePath           string
	UseCount           int
	InstanceType       string
	InstanceName       string
	InstancePath       string
	PropertyName       string
	AllInstancePaths   []string
	SceneSurfaceArea   float64
	LargestSurfacePath string
}

type ScanResult struct {
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
	ChildAssets          []ChildAssetInfo
	TotalBytesSize       int
	MeshNumFaces         uint32
	MeshNumVerts         uint32
	Resource             *fyne.StaticResource
	DownloadBytes        []byte
	DownloadFileName     string
	DownloadIsOriginal   bool
}

type ExtractedScanReference struct {
	AssetID    int64
	AssetInput string
}

type AssetReferenceContext struct {
	FilePath              string
	FileSHA256            string
	UseCount              int
	SceneSurfaceArea      float64
	LargestSurfacePath    string
	LargeTextureScore     float64
	ReferenceInstanceType string
	ReferencePropertyName string
	ReferenceInstancePath string
}

type AssetViewData struct {
	AssetID               int64
	FilePath              string
	FileSHA256            string
	UseCount              int
	SceneSurfaceArea      float64
	LargestSurfacePath    string
	LargeTextureScore     float64
	PreviewImageInfo      *ImageInfo
	StatsInfo             *ImageInfo
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
