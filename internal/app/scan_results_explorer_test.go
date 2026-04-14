package app

import (
	"testing"

	"joxblox/internal/app/loader"
	"joxblox/internal/format"
)

func TestScanResultsExplorerDuplicateStatsCountOnlyExtras(t *testing.T) {
	explorer := &scanResultsExplorer{
		allResults: []loader.ScanResult{
			{AssetID: 1, FileSHA256: "same-hash", BytesSize: 5 * format.Megabyte},
			{AssetID: 2, FileSHA256: "same-hash", BytesSize: 5 * format.Megabyte},
			{AssetID: 3, FileSHA256: "same-hash", BytesSize: 5 * format.Megabyte},
			{AssetID: 4, FileSHA256: "different-hash", BytesSize: 7 * format.Megabyte},
		},
	}

	hashCounts := loader.BuildHashCounts(explorer.allResults)

	if duplicateCount := explorer.countDuplicateRows(hashCounts); duplicateCount != 2 {
		t.Fatalf("expected duplicate count 2, got %d", duplicateCount)
	}
	if duplicateBytes := explorer.countDuplicateBytes(hashCounts); duplicateBytes != 10*format.Megabyte {
		t.Fatalf("expected duplicate bytes %d, got %d", 10*format.Megabyte, duplicateBytes)
	}
}
