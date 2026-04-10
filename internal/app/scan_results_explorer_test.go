package app

import "testing"

func TestScanResultsExplorerDuplicateStatsCountOnlyExtras(t *testing.T) {
	explorer := &scanResultsExplorer{
		allResults: []scanResult{
			{AssetID: 1, FileSHA256: "same-hash", BytesSize: 5 * megabyte},
			{AssetID: 2, FileSHA256: "same-hash", BytesSize: 5 * megabyte},
			{AssetID: 3, FileSHA256: "same-hash", BytesSize: 5 * megabyte},
			{AssetID: 4, FileSHA256: "different-hash", BytesSize: 7 * megabyte},
		},
	}

	hashCounts := buildHashCounts(explorer.allResults)

	if duplicateCount := explorer.countDuplicateRows(hashCounts); duplicateCount != 2 {
		t.Fatalf("expected duplicate count 2, got %d", duplicateCount)
	}
	if duplicateBytes := explorer.countDuplicateBytes(hashCounts); duplicateBytes != 10*megabyte {
		t.Fatalf("expected duplicate bytes %d, got %d", 10*megabyte, duplicateBytes)
	}
}
