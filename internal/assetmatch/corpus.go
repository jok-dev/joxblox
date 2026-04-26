// Package assetmatch maps captured GPU assets back to Roblox Studio asset
// IDs by perceptual-hash lookup. The corpus is built from a scan of a
// .rbxl/.rbxm place file (every Image asset reference); the query side
// hashes each captured texture's decoded base mip with the same algorithm
// and looks up matches within a small Hamming-distance threshold.
package assetmatch

import (
	"bytes"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"math/bits"
	"runtime"
	"sort"
	"sync"
	"time"

	"joxblox/internal/app/loader"

	xdraw "golang.org/x/image/draw"
)

// DefaultMatchHammingDistance is the maximum bit-difference between two
// 64-bit dHashes for them to be considered a match. 0 = exact-only —
// every bit of the perceptual hash must match. Strictest possible
// setting; will miss matches where BC roundtrip noise flips even one
// bit. Bump to 1+ if too many real matches go missing.
const DefaultMatchHammingDistance = 0

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

// assetFetcher is the seam BuildTextureCorpus uses to obtain raw asset
// bytes. The production implementation calls into loader's existing
// download flow; tests inject a synthetic fetcher with hand-rolled bytes.
type assetFetcher interface {
	FetchImageBytes(assetID int64, assetInput string) ([]byte, error)
}

type loaderAssetFetcher struct{}

func (loaderAssetFetcher) FetchImageBytes(assetID int64, assetInput string) ([]byte, error) {
	preview, err := loader.LoadAssetStatsPreviewForReference(assetID, assetInput)
	if err != nil {
		return nil, err
	}
	if preview == nil || len(preview.DownloadBytes) == 0 {
		return nil, fmt.Errorf("asset %d returned no download bytes", assetID)
	}
	return preview.DownloadBytes, nil
}

// BuildTextureCorpus walks scanResults, filters to Image-type assets,
// downloads + dHashes each, and returns a queryable TextureCorpus.
// onProgress (if non-nil) is called from worker goroutines each time an
// asset finishes — UI callers should fyne.Do the update.
func BuildTextureCorpus(scanResults []loader.ScanResult, onProgress func(done, total int)) (*TextureCorpus, error) {
	return buildTextureCorpusWithFetcher(scanResults, loaderAssetFetcher{}, onProgress)
}

func buildTextureCorpusWithFetcher(scanResults []loader.ScanResult, fetcher assetFetcher, onProgress func(done, total int)) (*TextureCorpus, error) {
	type job struct {
		assetID    int64
		assetInput string
	}
	jobs := make([]job, 0, len(scanResults))
	seen := map[int64]bool{}
	for _, r := range scanResults {
		if r.AssetTypeName != "Image" {
			continue
		}
		if r.AssetID <= 0 || seen[r.AssetID] {
			continue
		}
		seen[r.AssetID] = true
		jobs = append(jobs, job{assetID: r.AssetID, assetInput: r.AssetInput})
	}
	total := len(jobs)
	corpus := &TextureCorpus{
		byHash:    map[uint64][]int64{},
		byAssetID: map[int64]uint64{},
		builtAt:   time.Now(),
	}
	if total == 0 {
		return corpus, nil
	}

	workerCount := runtime.NumCPU() / 2
	if workerCount < 1 {
		workerCount = 1
	}
	if workerCount > 8 {
		workerCount = 8
	}
	if workerCount > total {
		workerCount = total
	}

	type result struct {
		assetID int64
		hash    uint64
		ok      bool
	}
	jobCh := make(chan job, total)
	resultCh := make(chan result, total)
	var wg sync.WaitGroup
	for w := 0; w < workerCount; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobCh {
				bytesData, err := fetcher.FetchImageBytes(j.assetID, j.assetInput)
				if err != nil {
					resultCh <- result{assetID: j.assetID, ok: false}
					continue
				}
				img, _, decErr := image.Decode(bytes.NewReader(bytesData))
				if decErr != nil {
					resultCh <- result{assetID: j.assetID, ok: false}
					continue
				}
				h, hashErr := computeImageDHashFromImage(img)
				if hashErr != nil {
					resultCh <- result{assetID: j.assetID, ok: false}
					continue
				}
				resultCh <- result{assetID: j.assetID, hash: h, ok: true}
			}
		}()
	}
	for _, j := range jobs {
		jobCh <- j
	}
	close(jobCh)
	go func() { wg.Wait(); close(resultCh) }()

	done := 0
	for r := range resultCh {
		done++
		if r.ok {
			corpus.byHash[r.hash] = append(corpus.byHash[r.hash], r.assetID)
			corpus.byAssetID[r.assetID] = r.hash
		}
		if onProgress != nil {
			onProgress(done, total)
		}
	}
	return corpus, nil
}

// computeImageDHashFromImage mirrors renderdoc.computeImageDHash —
// duplicated here to avoid an internal/renderdoc import from
// internal/assetmatch (assetmatch is upstream of renderdoc consumers).
// 9×8 grayscale-difference algorithm; identical bit layout to
// loader.ComputeImageDHash so corpus and capture hashes are comparable.
func computeImageDHashFromImage(img image.Image) (uint64, error) {
	const dHashWidth = 9
	const dHashHeight = 8
	bounds := img.Bounds()
	if bounds.Dx() < dHashWidth || bounds.Dy() < dHashHeight {
		return 0, fmt.Errorf("image too small for dHash (%dx%d)", bounds.Dx(), bounds.Dy())
	}
	resized := image.NewGray(image.Rect(0, 0, dHashWidth, dHashHeight))
	xdraw.BiLinear.Scale(resized, resized.Bounds(), img, bounds, xdraw.Over, nil)

	var hash uint64
	bitIndex := 0
	for y := 0; y < dHashHeight; y++ {
		for x := 0; x < dHashWidth-1; x++ {
			left := resized.GrayAt(x, y).Y
			right := resized.GrayAt(x+1, y).Y
			if left < right {
				hash |= 1 << bitIndex
			}
			bitIndex++
		}
	}
	return hash, nil
}
