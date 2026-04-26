package assetmatch

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"reflect"
	"testing"

	"joxblox/internal/app/loader"
)

func TestMatchExactHashReturnsAssetID(t *testing.T) {
	corpus := newTextureCorpusFromMap(map[uint64][]int64{
		0xDEADBEEFCAFEBABE: {12345},
	})
	got := corpus.Match(0xDEADBEEFCAFEBABE)
	if !reflect.DeepEqual(got, []int64{12345}) {
		t.Errorf("Match exact: got %v, want [12345]", got)
	}
}

func TestMatchNearHashWithinThreshold(t *testing.T) {
	corpus := newTextureCorpusFromMap(map[uint64][]int64{
		0xDEADBEEFCAFEBABE: {12345},
	})
	// Flip exactly DefaultMatchHammingDistance bits — at-threshold,
	// should still match. Test stays valid as the constant changes.
	var flipMask uint64
	for i := 0; i < DefaultMatchHammingDistance; i++ {
		flipMask |= 1 << i
	}
	near := uint64(0xDEADBEEFCAFEBABE) ^ flipMask
	got := corpus.Match(near)
	if !reflect.DeepEqual(got, []int64{12345}) {
		t.Errorf("Match near: got %v, want [12345]", got)
	}
}

func TestMatchBeyondThresholdReturnsNothing(t *testing.T) {
	corpus := newTextureCorpusFromMap(map[uint64][]int64{
		0xDEADBEEFCAFEBABE: {12345},
	})
	far := uint64(0xDEADBEEFCAFEBABE) ^ 0xFFFFFFFF
	got := corpus.Match(far)
	if len(got) != 0 {
		t.Errorf("Match far: got %v, want empty", got)
	}
}

func TestMatchMultipleCandidatesSortedByDistance(t *testing.T) {
	// Two candidates within threshold, sorted by distance ascending.
	// The second has distance 1 unconditionally so the test passes
	// at any threshold ≥ 1.
	corpus := newTextureCorpusFromMap(map[uint64][]int64{
		0xDEADBEEFCAFEBABE:               {12345},
		uint64(0xDEADBEEFCAFEBABE) ^ 0b1: {67890},
	})
	got := corpus.Match(0xDEADBEEFCAFEBABE)
	want := []int64{12345, 67890}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("multi-match order: got %v, want %v", got, want)
	}
}

func TestMatchSameHashYieldsAllAssetIDs(t *testing.T) {
	corpus := newTextureCorpusFromMap(map[uint64][]int64{
		0xDEADBEEFCAFEBABE: {12345, 67890},
	})
	got := corpus.Match(0xDEADBEEFCAFEBABE)
	if len(got) != 2 {
		t.Errorf("multiple IDs same hash: got %v, want 2 entries", got)
	}
}

func TestMatchEmptyCorpusReturnsNothing(t *testing.T) {
	corpus := newTextureCorpusFromMap(nil)
	if got := corpus.Match(0xCAFE); len(got) != 0 {
		t.Errorf("empty corpus: got %v, want empty", got)
	}
}

func TestHashForReturnsCorpusEntry(t *testing.T) {
	corpus := newTextureCorpusFromMap(map[uint64][]int64{
		0xCAFE: {12345},
	})
	if h, ok := corpus.HashFor(12345); !ok || h != 0xCAFE {
		t.Errorf("HashFor(12345) = (%x, %v), want (cafe, true)", h, ok)
	}
	if _, ok := corpus.HashFor(99999); ok {
		t.Errorf("HashFor(99999) = ok=true, want false (not in corpus)")
	}
}

// fakeAssetFetcher implements assetFetcher for tests by returning
// deterministic small PNG bytes per asset ID.
type fakeAssetFetcher struct {
	bytesByAssetID map[int64][]byte
}

func (f fakeAssetFetcher) FetchImageBytes(assetID int64, assetInput string) ([]byte, error) {
	bytes, ok := f.bytesByAssetID[assetID]
	if !ok {
		return nil, fmt.Errorf("asset %d not in fake fetcher", assetID)
	}
	return bytes, nil
}

func makeSyntheticPNG(width, height int, fillColor uint8) []byte {
	img := image.NewGray(image.Rect(0, 0, width, height))
	for i := range img.Pix {
		img.Pix[i] = fillColor
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

func TestBuildTextureCorpusSkipsNonImageAssets(t *testing.T) {
	scan := []loader.ScanResult{
		{AssetID: 1, AssetTypeName: "Image"},
		{AssetID: 2, AssetTypeName: "MeshPart"},
		{AssetID: 3, AssetTypeName: "Audio"},
	}
	fetcher := fakeAssetFetcher{
		bytesByAssetID: map[int64][]byte{
			1: makeSyntheticPNG(32, 32, 200),
		},
	}
	corpus, err := buildTextureCorpusWithFetcher(scan, fetcher, nil)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if corpus.Size() != 1 {
		t.Errorf("Size: got %d, want 1 (only the Image)", corpus.Size())
	}
	if _, ok := corpus.HashFor(1); !ok {
		t.Errorf("expected asset 1 in corpus")
	}
	if _, ok := corpus.HashFor(2); ok {
		t.Errorf("MeshPart leaked into image corpus")
	}
}

func TestBuildTextureCorpusReportsProgress(t *testing.T) {
	scan := []loader.ScanResult{
		{AssetID: 1, AssetTypeName: "Image"},
		{AssetID: 2, AssetTypeName: "Image"},
	}
	fetcher := fakeAssetFetcher{
		bytesByAssetID: map[int64][]byte{
			1: makeSyntheticPNG(16, 16, 100),
			2: makeSyntheticPNG(16, 16, 200),
		},
	}
	var maxDone, maxTotal int
	_, _ = buildTextureCorpusWithFetcher(scan, fetcher, func(done, total int) {
		if done > maxDone {
			maxDone = done
		}
		maxTotal = total
	})
	if maxTotal != 2 {
		t.Errorf("progress total: got %d, want 2", maxTotal)
	}
	if maxDone != 2 {
		t.Errorf("progress final done: got %d, want 2", maxDone)
	}
}

func TestBuildTextureCorpusDeduplicatesByAssetID(t *testing.T) {
	scan := []loader.ScanResult{
		{AssetID: 1, AssetTypeName: "Image"},
		{AssetID: 1, AssetTypeName: "Image"}, // duplicate reference
	}
	fetcher := fakeAssetFetcher{
		bytesByAssetID: map[int64][]byte{1: makeSyntheticPNG(16, 16, 100)},
	}
	corpus, _ := buildTextureCorpusWithFetcher(scan, fetcher, nil)
	if corpus.Size() != 1 {
		t.Errorf("dedup: got %d, want 1", corpus.Size())
	}
}
