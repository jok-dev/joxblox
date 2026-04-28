package report

type Summary struct {
	TotalBytes        int64
	TextureBytes      int64
	TexturePixelCount int64
	BC1PixelCount     int64
	BC3PixelCount     int64
	// BC1BytesExact and BC3BytesExact are the byte-accurate VRAM footprints
	// (computed per-texture with BC 4x4-block minimums) that match what
	// RenderDoc reports. The pixel-count fields drive grading thresholds;
	// these byte fields drive the displayed headline number and log.
	BC1BytesExact int64
	BC3BytesExact int64
	// MismatchedPBRMaterialCount is the number of SurfaceAppearance materials
	// whose authored slots (color/normal/metalness/roughness) aren't all at the
	// same source resolution. PBRMaterialCount is the population those are
	// drawn from — materials with at least one authored map slot.
	MismatchedPBRMaterialCount int
	PBRMaterialCount           int
	MeshBytes                  int64
	TriangleCount              int64
	OversizedTextureCount      int
	DrawCallCount              int64
	DuplicateCount             int64
	DuplicateSizeBytes         int64
	ReferenceCount             int64
	UniqueReferenceCount       int
	UniqueAssetCount           int
	ResolvedCount              int
	MeshPartCount              int
	PartCount                  int
	// InstanceCount is the total descendant count from the rbxl/rbxm DOM
	// (filtered to the report's path prefixes), counting every instance
	// regardless of class — backs the "Instances" grade.
	InstanceCount int64
}
