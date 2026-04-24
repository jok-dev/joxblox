package heatmap

type AssetReference struct {
	AssetID    int64
	AssetInput string
}

type AssetStats struct {
	AssetID              int64
	AssetTypeID          int
	AssetTypeName        string
	TotalBytes           int
	TextureBytes         int
	MeshBytes            int
	TriangleCount        uint32
	PixelCount           int64
	// Width and Height are the texture's base-mip dimensions. Populated
	// for image assets so exact per-mip VRAM can be computed (BC formats
	// require a minimum 4x4 block per mip, which the aggregate 4/3 mip
	// factor understates by ~10-30 B per texture). Zero for non-images.
	Width                int
	Height               int
	HasAlphaChannel      bool
	NonOpaqueAlphaPixels int64
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
	ReferenceCount         int64
	UniqueAssetCount       int64
	UniqueTextureCount     int64
	UniqueMeshCount        int64
	TextureBytes           int64
	MeshBytes              int64
	TotalBytes             int64
	TriangleCount          int64
	PixelCount             int64
	// BC1PixelCount is the sum of pixels from textures that Roblox would encode
	// as BC1 (no alpha channel in the source image) — 0.5 bytes/pixel on GPU.
	BC1PixelCount int64
	// BC3PixelCount is the sum of pixels from textures that Roblox would encode
	// as BC3 (alpha channel present in the source image) — 1.0 bytes/pixel on GPU.
	BC3PixelCount int64
	// WastefulBC3PixelCount is the subset of BC3PixelCount from textures whose
	// alpha channel is entirely opaque — they pay BC3 cost but could be BC1.
	WastefulBC3PixelCount int64
	MeshPartCount         int64
	PartCount             int64
	DrawCallCount         int64
}
