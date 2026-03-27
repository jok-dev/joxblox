package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAssetDownloadCachePathsStableAndDistinct(t *testing.T) {
	cacheFolder := t.TempDir()

	firstDataPath, firstMetadataPath := assetDownloadCachePaths(cacheFolder, "asset-file:id=1:type=4")
	secondDataPath, secondMetadataPath := assetDownloadCachePaths(cacheFolder, "asset-file:id=1:type=4")
	otherDataPath, otherMetadataPath := assetDownloadCachePaths(cacheFolder, "asset-file:id=2:type=4")

	if firstDataPath != secondDataPath || firstMetadataPath != secondMetadataPath {
		t.Fatal("expected identical cache keys to produce identical cache paths")
	}
	if firstDataPath == otherDataPath || firstMetadataPath == otherMetadataPath {
		t.Fatal("expected different cache keys to produce different cache paths")
	}
}

func TestAssetDownloadJSONCacheEntryRoundTrip(t *testing.T) {
	cacheFolder := t.TempDir()
	cacheKey := "thumbnail-meta:asset=123"
	expected := cachedThumbnailInfo{
		Info: thumbnailInfo{
			ImageURL: "https://example.com/thumb.png",
			State:    stateCompleted,
			Version:  "TN3",
		},
		RawJSON: `{"ok":true}`,
	}

	if err := writeAssetDownloadJSONCacheEntry(cacheFolder, cacheKey, expected); err != nil {
		t.Fatalf("writeAssetDownloadJSONCacheEntry returned error: %v", err)
	}

	var decoded cachedThumbnailInfo
	cacheHit, err := readAssetDownloadJSONCacheEntry(cacheFolder, cacheKey, time.Hour, &decoded)
	if err != nil {
		t.Fatalf("readAssetDownloadJSONCacheEntry returned error: %v", err)
	}
	if !cacheHit {
		t.Fatal("expected cache hit after writing JSON cache entry")
	}
	if decoded.Info.ImageURL != expected.Info.ImageURL || decoded.Info.Version != expected.Info.Version || decoded.RawJSON != expected.RawJSON {
		t.Fatalf("decoded JSON cache entry mismatch: got %#v want %#v", decoded, expected)
	}
}

func TestAssetDownloadJSONCacheEntryExpiresAfterMaxAge(t *testing.T) {
	cacheFolder := t.TempDir()
	cacheKey := "asset-delivery:id=123"
	cachePath := assetDownloadJSONCachePath(cacheFolder, cacheKey)

	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}

	envelopeBytes, err := json.Marshal(assetDownloadJSONCacheEnvelope{
		SavedAtUnix: time.Now().Add(-2 * time.Hour).Unix(),
		Payload:     json.RawMessage(`{"location":"https://example.com/file.bin","rawJSON":"{}","assetTypeID":63,"assetTypeName":"Type 63"}`),
	})
	if err != nil {
		t.Fatalf("json.Marshal returned error: %v", err)
	}
	if err := os.WriteFile(cachePath, envelopeBytes, 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	var decoded assetDeliveryInfo
	cacheHit, readErr := readAssetDownloadJSONCacheEntry(cacheFolder, cacheKey, time.Minute, &decoded)
	if readErr != nil {
		t.Fatalf("readAssetDownloadJSONCacheEntry returned error: %v", readErr)
	}
	if cacheHit {
		t.Fatal("expected stale JSON cache entry to miss")
	}
}
