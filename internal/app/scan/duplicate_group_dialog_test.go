package scan

import (
	"testing"

	"joxblox/internal/app/loader"
	"joxblox/internal/roblox"
)

// TestFilterCandidateDuplicates_SHAFastPathRanksByteIdenticalRows asserts
// that two rows with the same FileSHA256 always come back at distance 0
// even when neither has a decodable preview Resource — the auto-tag
// case the user hits when right-clicking an SHA-clustered duplicate.
func TestFilterCandidateDuplicates_SHAFastPathRanksByteIdenticalRows(t *testing.T) {
	rows := []loader.ScanResult{
		{AssetID: 1, AssetTypeID: roblox.AssetTypeImage, FileSHA256: "sha-aaa"},
		{AssetID: 2, AssetTypeID: roblox.AssetTypeImage, FileSHA256: "sha-aaa"},
		{AssetID: 3, AssetTypeID: roblox.AssetTypeImage, FileSHA256: "sha-bbb"},
	}
	out, distances, _ := filterCandidateDuplicates(rows, 1)
	if len(out) == 0 {
		t.Fatalf("filterCandidateDuplicates returned no rows")
	}
	if out[0].AssetID != 1 {
		t.Errorf("primary should be first, got %d", out[0].AssetID)
	}
	if distances == nil {
		t.Fatalf("distancesByID should never be nil")
	}
	if d, ok := distances[1]; !ok || d != 0 {
		t.Errorf("primary distance = %d (ok=%v), want 0", d, ok)
	}
	if d, ok := distances[2]; !ok || d != 0 {
		t.Errorf("SHA-matched candidate distance = %d (ok=%v), want 0", d, ok)
	}
	if _, ok := distances[3]; ok {
		t.Errorf("non-matching SHA candidate (3) should NOT be in distances map (no preview to dHash)")
	}
}

// TestFilterCandidateDuplicates_KeepsCandidateWhenAssetTypeIDZero verifies
// that rows with AssetTypeID==0 (e.g. failed loads) aren't filtered out
// when the primary's type also can't be matched — sameContentType falls
// through to a tolerant ContentType check.
func TestFilterCandidateDuplicates_KeepsCandidateWhenAssetTypeIDZero(t *testing.T) {
	rows := []loader.ScanResult{
		{AssetID: 1, ContentType: "image/png", FileSHA256: "sha-aaa"},
		{AssetID: 2, ContentType: "image/png", FileSHA256: "sha-aaa"},
	}
	out, distances, _ := filterCandidateDuplicates(rows, 1)
	if len(out) != 2 {
		t.Fatalf("expected primary + 1 candidate, got %v", out)
	}
	if d, ok := distances[2]; !ok || d != 0 {
		t.Errorf("SHA-matched candidate distance = %d (ok=%v), want 0", d, ok)
	}
}

// TestFilterCandidateDuplicates_PrimaryAlwaysInDistances asserts that the
// distances map always contains an entry for the primary itself (at
// distance 0) so the cell renderer's "no map → blank" branch is dead.
func TestFilterCandidateDuplicates_PrimaryAlwaysInDistances(t *testing.T) {
	rows := []loader.ScanResult{
		{AssetID: 99, AssetTypeID: roblox.AssetTypeImage}, // no SHA, no Resource
	}
	_, distances, _ := filterCandidateDuplicates(rows, 99)
	if distances == nil {
		t.Fatalf("distancesByID should never be nil")
	}
	if d, ok := distances[99]; !ok || d != 0 {
		t.Errorf("primary should be in distances at 0, got d=%d ok=%v", d, ok)
	}
}
