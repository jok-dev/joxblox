package app

import "fmt"

type heatmapAssetRequestSource int

const (
	heatmapAssetRequestSourceMemory heatmapAssetRequestSource = iota
	heatmapAssetRequestSourceDisk
	heatmapAssetRequestSourceNetwork
)

func formatRequestSourceBreakdown(memoryCount int, diskCount int, networkCount int) string {
	return fmt.Sprintf("fetched from: mem %d, disk %d, net %d", memoryCount, diskCount, networkCount)
}

func formatSingleRequestSourceBreakdown(requestSource heatmapAssetRequestSource) string {
	switch requestSource {
	case heatmapAssetRequestSourceNetwork:
		return formatRequestSourceBreakdown(0, 0, 1)
	case heatmapAssetRequestSourceDisk:
		return formatRequestSourceBreakdown(0, 1, 0)
	default:
		return formatRequestSourceBreakdown(1, 0, 0)
	}
}
