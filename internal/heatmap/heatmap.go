package heatmap

import "fmt"

type RequestSource int

const (
	SourceMemory  RequestSource = iota
	SourceDisk
	SourceNetwork
)

func FormatRequestSourceBreakdown(memoryCount int, diskCount int, networkCount int) string {
	return fmt.Sprintf("fetched from: mem %d, disk %d, net %d", memoryCount, diskCount, networkCount)
}

func FormatSingleRequestSourceBreakdown(requestSource RequestSource) string {
	switch requestSource {
	case SourceNetwork:
		return FormatRequestSourceBreakdown(0, 0, 1)
	case SourceDisk:
		return FormatRequestSourceBreakdown(0, 1, 0)
	default:
		return FormatRequestSourceBreakdown(1, 0, 0)
	}
}

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
