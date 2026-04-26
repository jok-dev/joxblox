package assetmatch

import (
	"reflect"
	"testing"
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
	near := uint64(0xDEADBEEFCAFEBABE) ^ 0b1111
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
	corpus := newTextureCorpusFromMap(map[uint64][]int64{
		0xDEADBEEFCAFEBABE:                 {12345},
		uint64(0xDEADBEEFCAFEBABE) ^ 0b1:   {67890},
		uint64(0xDEADBEEFCAFEBABE) ^ 0b111: {11111},
	})
	got := corpus.Match(0xDEADBEEFCAFEBABE)
	want := []int64{12345, 67890, 11111}
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
