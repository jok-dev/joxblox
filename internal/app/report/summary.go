package report

type Summary struct {
	TotalBytes            int64
	TextureBytes          int64
	MeshBytes             int64
	TriangleCount         int64
	OversizedTextureCount int
	DrawCallCount         int64
	DuplicateCount        int64
	DuplicateSizeBytes    int64
	ReferenceCount        int64
	UniqueReferenceCount  int
	UniqueAssetCount      int
	ResolvedCount         int
	MeshPartCount         int
	PartCount             int
}
