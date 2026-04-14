package heatmap

type AssetReference struct {
	AssetID    int64
	AssetInput string
}

type AssetStats struct {
	AssetID       int64
	AssetTypeID   int
	AssetTypeName string
	TotalBytes    int
	TextureBytes  int
	MeshBytes     int
	TriangleCount uint32
	PixelCount    int64
}

type Cell struct {
	Row        int
	Column     int
	Stats      Totals
	BaseStats  Totals
	DeltaStats Totals
	MinimumX   float64
	MaximumX   float64
	MinimumZ   float64
	MaximumZ   float64
}

type Totals struct {
	ReferenceCount     int64
	UniqueAssetCount   int64
	UniqueTextureCount int64
	UniqueMeshCount    int64
	TextureBytes       int64
	MeshBytes          int64
	TotalBytes         int64
	TriangleCount      int64
	PixelCount         int64
	MeshPartCount      int64
	PartCount          int64
	DrawCallCount      int64
}
