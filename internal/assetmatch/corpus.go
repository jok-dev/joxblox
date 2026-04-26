// Package assetmatch maps captured GPU assets back to Roblox Studio asset
// IDs by perceptual-hash lookup. The corpus is built from a scan of a
// .rbxl/.rbxm place file (every Image asset reference); the query side
// hashes each captured texture's decoded base mip with the same algorithm
// and looks up matches within a small Hamming-distance threshold.
package assetmatch

import (
	"math/bits"
	"sort"
	"time"
)

// DefaultMatchHammingDistance is the maximum bit-difference between two
// 64-bit dHashes for them to be considered a match. Empirical: BC1/BC3
// roundtrip noise typically perturbs ~3-5 bits of the perceptual hash;
// 6 leaves headroom while keeping false positives low for visually
// distinct textures.
const DefaultMatchHammingDistance = 6

// TextureCorpus is an immutable lookup table from dHash → Roblox asset
// IDs. Build via BuildTextureCorpus from a slice of scan results, then
// query via Match. Safe for concurrent reads.
type TextureCorpus struct {
	byHash     map[uint64][]int64
	byAssetID  map[int64]uint64
	sourceFile string
	builtAt    time.Time
}

// SourceFile returns the .rbxl/.rbxm path the corpus was built from
// (informational, for status display). Empty if not set by the builder.
func (c *TextureCorpus) SourceFile() string {
	if c == nil {
		return ""
	}
	return c.sourceFile
}

// BuiltAt returns when the corpus build finished. Zero time if not set.
func (c *TextureCorpus) BuiltAt() time.Time {
	if c == nil {
		return time.Time{}
	}
	return c.builtAt
}

// Size returns the number of asset IDs known to the corpus.
func (c *TextureCorpus) Size() int {
	if c == nil {
		return 0
	}
	return len(c.byAssetID)
}

// HashFor returns the dHash recorded for the given asset ID, if present.
// Useful for surfacing "this corpus knows about asset X with hash Y" in
// debug overlays.
func (c *TextureCorpus) HashFor(assetID int64) (uint64, bool) {
	if c == nil {
		return 0, false
	}
	h, ok := c.byAssetID[assetID]
	return h, ok
}

// Match returns asset IDs whose stored dHash is within
// DefaultMatchHammingDistance of queryHash, sorted by ascending distance
// (best match first). Asset IDs sharing the exact same hash are returned
// in numeric order. Returns nil when no candidate is within threshold.
func (c *TextureCorpus) Match(queryHash uint64) []int64 {
	if c == nil || len(c.byHash) == 0 {
		return nil
	}
	type candidate struct {
		assetID  int64
		distance int
	}
	var candidates []candidate
	for hash, ids := range c.byHash {
		d := bits.OnesCount64(hash ^ queryHash)
		if d > DefaultMatchHammingDistance {
			continue
		}
		sortedIDs := append([]int64(nil), ids...)
		sort.Slice(sortedIDs, func(i, j int) bool { return sortedIDs[i] < sortedIDs[j] })
		for _, id := range sortedIDs {
			candidates = append(candidates, candidate{assetID: id, distance: d})
		}
	}
	if len(candidates) == 0 {
		return nil
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].distance < candidates[j].distance
	})
	out := make([]int64, len(candidates))
	for i, c := range candidates {
		out[i] = c.assetID
	}
	return out
}

// newTextureCorpusFromMap is the test entry point — construct a corpus
// directly from a hash→IDs map without going through the builder. Not
// exported because production code should always go through
// BuildTextureCorpus.
func newTextureCorpusFromMap(byHash map[uint64][]int64) *TextureCorpus {
	c := &TextureCorpus{
		byHash:    map[uint64][]int64{},
		byAssetID: map[int64]uint64{},
	}
	for hash, ids := range byHash {
		c.byHash[hash] = append([]int64(nil), ids...)
		for _, id := range ids {
			c.byAssetID[id] = hash
		}
	}
	return c
}
