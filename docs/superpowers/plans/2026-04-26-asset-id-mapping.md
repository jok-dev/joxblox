# Asset-ID Mapping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** When the user has a Roblox place file scanned (Scan tab) and a RenderDoc capture loaded, automatically identify which Studio asset IDs each captured texture corresponds to. Show the matched ID in a new Studio Asset column on the Materials, Textures, and Meshes sub-tabs, plus a clickable "Open in Single Asset" action in the preview pane.

**Architecture:** A new `internal/assetmatch` package holds a perceptual-hash (dHash) corpus built from a `[]loader.ScanResult` slice. A small package-level event bus in `internal/app/loader` lets the Scan tab publish "scan completed" once results are in; the RenderDoc tab subscribes, rebuilds the corpus, and populates a per-capture match overlay. Capture-side textures gain a `DHash uint64` field populated by the existing `ComputeTextureHashes` pass. A separate one-shot CLI in `tools/mesh-hash-probe/` runs a feasibility experiment for the mesh side and writes a Markdown findings doc.

**Tech Stack:** Go 1.23+, Fyne v2 widgets, existing `loader.ComputeImageDHash` for perceptual hashing, existing `DownloadRobloxContentBytesWithCacheKey` for cached asset fetches.

**Spec:** [docs/superpowers/specs/2026-04-26-asset-id-mapping-design.md](../specs/2026-04-26-asset-id-mapping-design.md)

---

## File Structure

**Create:**
- `internal/app/loader/scan_events.go` — `CurrentScan`, `Subscribe`, `Publish` event bus.
- `internal/app/loader/scan_events_test.go`
- `internal/assetmatch/corpus.go` — `TextureCorpus` type, `BuildTextureCorpus`, `Match`.
- `internal/assetmatch/corpus_test.go`
- `tools/mesh-hash-probe/main.go` — feasibility CLI.
- `tools/mesh-hash-probe/go.mod` — separate module (mirrors `tools/mesh-renderer`).

**Modify:**
- `internal/renderdoc/parse.go` — add `DHash uint64` field to `TextureInfo`.
- `internal/renderdoc/hash.go` — populate `DHash` in `ComputeTextureHashes` alongside `PixelHash`.
- `internal/renderdoc/hash_test.go` (or `parse_test.go` — wherever the existing fixture lives) — assert `DHash` populated.
- `internal/app/scan/scan_results_explorer.go` — call `loader.PublishScanCompleted` after `SetResults`.
- `internal/app/ui/tabs/renderdoc/renderdoc_tab.go` — Textures sub-tab: subscribe to scan events, build corpus, add Studio Asset column + preview-pane line.
- `internal/app/ui/tabs/renderdoc/materials_view.go` — Materials sub-tab: same plumbing, Color-slot match.
- `internal/app/ui/tabs/renderdoc/meshes_view.go` — Meshes sub-tab: placeholder Studio Asset column (always `—` in v1).

---

## Task 1: Loader scan-completion event bus

**Files:**
- Create: `internal/app/loader/scan_events.go`
- Create: `internal/app/loader/scan_events_test.go`

**Why:** Lets the RenderDoc tab learn about scan completions without coupling to the Scan tab's internals. Plays the same role as `OnDataChanged` in many GUIs but kept tiny — three exported functions, no objects.

- [ ] **Step 1: Write the failing test**

Create `internal/app/loader/scan_events_test.go`:

```go
package loader

import (
	"sync"
	"testing"
)

func TestPublishScanCompletedNotifiesAllSubscribers(t *testing.T) {
	resetScanEventState()
	var mu sync.Mutex
	var calls int
	unsub1 := SubscribeScanCompleted(func() { mu.Lock(); calls++; mu.Unlock() })
	unsub2 := SubscribeScanCompleted(func() { mu.Lock(); calls++; mu.Unlock() })
	defer unsub1()
	defer unsub2()

	PublishScanCompleted([]ScanResult{{AssetID: 1}, {AssetID: 2}})

	if calls != 2 {
		t.Errorf("subscribers called %d times, want 2", calls)
	}
	current := CurrentScan()
	if len(current) != 2 || current[0].AssetID != 1 {
		t.Errorf("CurrentScan returned %+v, want 2 results starting with AssetID=1", current)
	}
}

func TestUnsubscribeStopsFurtherCalls(t *testing.T) {
	resetScanEventState()
	var calls int
	unsub := SubscribeScanCompleted(func() { calls++ })
	unsub()
	PublishScanCompleted(nil)
	if calls != 0 {
		t.Errorf("calls after unsubscribe: got %d, want 0", calls)
	}
}

func TestCurrentScanReturnsNilWhenNothingPublished(t *testing.T) {
	resetScanEventState()
	if got := CurrentScan(); got != nil {
		t.Errorf("CurrentScan before any publish: got %v, want nil", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/app/loader/ -run "TestPublishScanCompleted|TestUnsubscribe|TestCurrentScan" -v`
Expected: FAIL — symbols undefined.

- [ ] **Step 3: Implement the event bus**

Create `internal/app/loader/scan_events.go`:

```go
package loader

import "sync"

// scanEvents is a tiny package-level publish/subscribe channel for "the
// most recently completed scan changed." Used by the RenderDoc tab to
// rebuild its asset-ID match overlay without coupling to the Scan tab's
// internals. Only one publisher (the Scan tab); fan-out to N subscribers.
type scanEvents struct {
	mu          sync.RWMutex
	current     []ScanResult
	subscribers map[int]func()
	nextID      int
}

var scanEventState = &scanEvents{subscribers: map[int]func(){}}

// CurrentScan returns the most recently published scan results, or nil if
// nothing has been published this session. The slice is shared — callers
// must not mutate it.
func CurrentScan() []ScanResult {
	scanEventState.mu.RLock()
	defer scanEventState.mu.RUnlock()
	return scanEventState.current
}

// SubscribeScanCompleted registers a callback that fires every time a new
// scan completes. Returns a function to unregister. Safe to call from any
// goroutine; callbacks fire on the publishing goroutine.
func SubscribeScanCompleted(callback func()) (unsubscribe func()) {
	scanEventState.mu.Lock()
	id := scanEventState.nextID
	scanEventState.nextID++
	scanEventState.subscribers[id] = callback
	scanEventState.mu.Unlock()
	return func() {
		scanEventState.mu.Lock()
		delete(scanEventState.subscribers, id)
		scanEventState.mu.Unlock()
	}
}

// PublishScanCompleted records the new results as the current scan and
// notifies every active subscriber. Pass nil to indicate "results were
// cleared" (CurrentScan will then return nil).
func PublishScanCompleted(results []ScanResult) {
	scanEventState.mu.Lock()
	scanEventState.current = results
	subscribers := make([]func(), 0, len(scanEventState.subscribers))
	for _, cb := range scanEventState.subscribers {
		subscribers = append(subscribers, cb)
	}
	scanEventState.mu.Unlock()
	for _, cb := range subscribers {
		cb()
	}
}

// resetScanEventState is for tests only — restores the bus to its
// initial empty state so tests don't see each other's residue.
func resetScanEventState() {
	scanEventState.mu.Lock()
	scanEventState.current = nil
	scanEventState.subscribers = map[int]func(){}
	scanEventState.nextID = 0
	scanEventState.mu.Unlock()
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/app/loader/ -run "TestPublishScanCompleted|TestUnsubscribe|TestCurrentScan" -v`
Expected: PASS for all three.

- [ ] **Step 5: Commit**

```bash
git add internal/app/loader/scan_events.go internal/app/loader/scan_events_test.go
git commit -m "feat(loader): scan-completion event bus for cross-tab subscribers"
```

---

## Task 2: Scan tab publishes results on every update

**Files:**
- Modify: `internal/app/scan/scan_results_explorer.go`

**Why:** Hooks the existing `SetResults` and `AppendResults` paths so any code that updates the explorer's results also publishes them on the loader bus. Keeps the publish point single-source so we never miss a code path.

- [ ] **Step 1: Add publish call to `SetResults`**

In `internal/app/scan/scan_results_explorer.go`, find:

```go
func (explorer *ScanResultsExplorer) SetResults(rows []loader.ScanResult) {
	if explorer == nil {
		return
	}
	nextRows := make([]loader.ScanResult, len(rows))
	copy(nextRows, rows)
	explorer.allResults = nextRows
	explorer.selectedAssetID = 0
	explorer.versionIndex = loader.ExtractVersionsFromResults(explorer.allResults)
	explorer.refreshSearchSuggestions()
	explorer.clearPreview()
	explorer.ClearSimilarity()
	explorer.applySortAndFilters()
}
```

Append a publish call at the bottom of the function body:

```go
	loader.PublishScanCompleted(explorer.allResults)
}
```

- [ ] **Step 2: Add publish call to `AppendResults`**

Find `AppendResults` (right below `SetResults`). After the existing logic, before the function returns, add the same publish call. Both branches (refreshResults true/false) should publish. Restructure to:

```go
func (explorer *ScanResultsExplorer) AppendResults(rows []loader.ScanResult, refreshResults bool, refreshFilters bool) {
	if explorer == nil || len(rows) == 0 {
		return
	}
	explorer.allResults = append(explorer.allResults, rows...)
	if refreshResults {
		explorer.versionIndex = loader.ExtractVersionsFromResults(explorer.allResults)
		explorer.refreshSearchSuggestions()
		explorer.applySortAndFilters()
	} else if refreshFilters {
		// (whatever existing logic was here — keep it)
	}
	loader.PublishScanCompleted(explorer.allResults)
}
```

(If the existing `else if` / branching is more complex than shown, keep it intact and add the `PublishScanCompleted` call as the LAST statement before the closing brace.)

- [ ] **Step 3: Build**

Run: `go build ./...`
Expected: PASS.

- [ ] **Step 4: Run existing scan tests to confirm no regressions**

Run: `go test ./internal/app/scan/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/app/scan/scan_results_explorer.go
git commit -m "feat(scan): publish completed scan results on the loader event bus"
```

---

## Task 3: Add `DHash` to `TextureInfo`

**Files:**
- Modify: `internal/renderdoc/parse.go`
- Modify: `internal/renderdoc/hash.go`
- Modify: `internal/renderdoc/hash_test.go` (or extend an existing test file)

**Why:** Capture-side textures need a perceptual hash directly comparable to the corpus's. Computed in the same pass as the existing `PixelHash` so we don't decode twice.

- [ ] **Step 1: Add the field to `TextureInfo`**

In `internal/renderdoc/parse.go`, find `type TextureInfo struct` and add a `DHash` field next to `PixelHash`:

```go
type TextureInfo struct {
	// ... existing fields ...
	PixelHash string
	// DHash is the 64-bit perceptual hash (dHash) of the decoded base
	// mip, computed in the same pass as PixelHash. Used for cross-
	// referencing captured textures against a Roblox place's scan
	// results — exact pixel hashes don't survive BC compression
	// roundtripping but dHash with a small Hamming threshold does.
	DHash uint64
}
```

- [ ] **Step 2: Populate `DHash` inside `ComputeTextureHashes`**

In `internal/renderdoc/hash.go`, find the worker goroutine inside `ComputeTextureHashes` that calls `DecodeTexturePreview` and then `HashImagePixels`. Right after `report.Textures[idx].PixelHash = HashImagePixels(img)`, add:

```go
if dHash, dHashErr := computeImageDHash(img); dHashErr == nil {
	report.Textures[idx].DHash = dHash
}
```

Then add the helper at the bottom of `hash.go`:

```go
// computeImageDHash returns a 64-bit perceptual hash of img using the same
// 9×8 grayscale-difference algorithm as loader.ComputeImageDHash, but
// taking an already-decoded image so we don't waste a second decode pass.
// Returns 0 + error when the image is too small to hash meaningfully.
func computeImageDHash(img image.Image) (uint64, error) {
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
```

Add the imports to the top of `hash.go`:

```go
import (
	// ... existing imports ...
	"fmt"
	xdraw "golang.org/x/image/draw"
)
```

- [ ] **Step 3: Add a test**

Open `internal/renderdoc/hash_test.go`. Add (or modify the existing test that calls `ComputeTextureHashes`) to assert `DHash` is non-zero on a real fixture:

```go
func TestComputeTextureHashesPopulatesDHash(t *testing.T) {
	report, store := loadHashTestFixture(t) // existing helper, or build inline
	defer store.Close()
	ComputeTextureHashes(report, store, nil)
	var sawDHash bool
	for _, tex := range report.Textures {
		if tex.DHash != 0 {
			sawDHash = true
			break
		}
	}
	if !sawDHash {
		t.Errorf("expected at least one texture to have a non-zero DHash, got none")
	}
}
```

(If `loadHashTestFixture` doesn't exist, scan the existing tests in the file for the pattern they use — the codebase uses synthetic mini-fixtures for renderdoc tests. Adapt to whichever pattern is present. If there's no real fixture and tests use mock data, skip this step and let the next task's tests cover the new field.)

- [ ] **Step 4: Run tests + full renderdoc suite**

Run: `go test ./internal/renderdoc/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/renderdoc/parse.go internal/renderdoc/hash.go internal/renderdoc/hash_test.go
git commit -m "feat(renderdoc): populate DHash alongside PixelHash on every texture"
```

---

## Task 4: `assetmatch` package — corpus type + lookup

**Files:**
- Create: `internal/assetmatch/corpus.go`
- Create: `internal/assetmatch/corpus_test.go`

**Why:** The pure-data side of the feature: a corpus of `dHash → asset IDs` and a `Match(hash)` query. No I/O, no UI dependencies; trivial to test exhaustively.

- [ ] **Step 1: Write the failing tests**

Create `internal/assetmatch/corpus_test.go`:

```go
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
	// Flip 4 bits — well within DefaultMatchHammingDistance.
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
	// Flip many bits — way past the threshold.
	far := uint64(0xDEADBEEFCAFEBABE) ^ 0xFFFFFFFF
	got := corpus.Match(far)
	if len(got) != 0 {
		t.Errorf("Match far: got %v, want empty", got)
	}
}

func TestMatchMultipleCandidatesSortedByDistance(t *testing.T) {
	corpus := newTextureCorpusFromMap(map[uint64][]int64{
		0xDEADBEEFCAFEBABE:               {12345},
		uint64(0xDEADBEEFCAFEBABE) ^ 0b1: {67890}, // distance 1 from query below
		uint64(0xDEADBEEFCAFEBABE) ^ 0b111: {11111}, // distance 3
	})
	// Query == first key; 12345 is exact, 67890 is d=1, 11111 is d=3.
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/assetmatch/...`
Expected: FAIL — package doesn't exist yet.

- [ ] **Step 3: Implement the corpus**

Create `internal/assetmatch/corpus.go`:

```go
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
		// Sort the IDs at this hash for stable output.
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
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/assetmatch/...`
Expected: PASS for all 7 tests.

- [ ] **Step 5: Commit**

```bash
git add internal/assetmatch/corpus.go internal/assetmatch/corpus_test.go
git commit -m "feat(assetmatch): TextureCorpus + Match with Hamming-distance threshold"
```

---

## Task 5: `BuildTextureCorpus` from scan results

**Files:**
- Modify: `internal/assetmatch/corpus.go`
- Modify: `internal/assetmatch/corpus_test.go`

**Why:** Wires the corpus to actual scan data + asset downloads. The builder is what the RenderDoc tab will call when scan results arrive.

- [ ] **Step 1: Write the failing test**

Append to `internal/assetmatch/corpus_test.go`:

```go
import (
	"bytes"
	"fmt"
	"image"
	"image/png"

	"joxblox/internal/app/loader"
)

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
```

- [ ] **Step 2: Run test to verify failure**

Run: `go test ./internal/assetmatch/ -run TestBuildTextureCorpus -v`
Expected: FAIL — `assetFetcher`, `buildTextureCorpusWithFetcher` undefined.

- [ ] **Step 3: Implement the builder + fetcher abstraction**

Append to `internal/assetmatch/corpus.go`:

```go
import (
	"image"
	_ "image/jpeg"
	_ "image/png"
	"runtime"
	"sync"

	"joxblox/internal/app/loader"
)

// assetFetcher is the seam BuildTextureCorpus uses to obtain raw asset
// bytes. The production implementation calls into loader's existing
// download cache; tests inject a synthetic fetcher with hand-rolled bytes.
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
```

Add the missing imports at the top of `corpus.go`:

```go
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
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/assetmatch/...`
Expected: PASS for all corpus tests.

- [ ] **Step 5: Commit**

```bash
git add internal/assetmatch/corpus.go internal/assetmatch/corpus_test.go
git commit -m "feat(assetmatch): BuildTextureCorpus from scan results via cached download fetcher"
```

---

## Task 6: RenderDoc Textures sub-tab — column + corpus subscription

**Files:**
- Modify: `internal/app/ui/tabs/renderdoc/renderdoc_tab.go`

**Why:** First sub-tab to wire end-to-end. Same pattern repeats for Materials and Meshes.

- [ ] **Step 1: Add Studio Asset to the Textures column header list**

Find the column header constant block in `renderdoc_tab.go` (`var columnHeaders = []string{...}`). Add `"Studio Asset"` as the last entry.

- [ ] **Step 2: Add corpus + match overlay to the textures sub-tab state**

In `renderdocTabState`, add fields:

```go
corpus       *assetmatch.TextureCorpus
matchByTexID map[string]int64   // best match per texture resource ID
matchAllByTexID map[string][]int64 // all candidates within threshold
```

Initialize the maps in `newTexturesSubTab` next to the other state init.

Add the import at the top:

```go
"joxblox/internal/assetmatch"
```

- [ ] **Step 3: Subscribe to scan-completion events on tab construction**

Inside `newTexturesSubTab`, after the state is initialized, add:

```go
unsubscribeScan := loader.SubscribeScanCompleted(func() {
	scan := loader.CurrentScan()
	go func() {
		corpus, err := assetmatch.BuildTextureCorpus(scan, nil)
		if err != nil {
			debug.Logf("textures sub-tab: corpus build failed: %s", err.Error())
			return
		}
		fyne.Do(func() {
			state.corpus = corpus
			recomputeTextureMatches(state)
			table.Refresh()
		})
	}()
})
_ = unsubscribeScan // for v1 the sub-tab lives for the whole app session

// If a scan is already loaded by the time this tab is constructed,
// kick off an initial corpus build right away.
if existing := loader.CurrentScan(); len(existing) > 0 {
	go func() {
		corpus, err := assetmatch.BuildTextureCorpus(existing, nil)
		if err != nil {
			return
		}
		fyne.Do(func() {
			state.corpus = corpus
			recomputeTextureMatches(state)
			table.Refresh()
		})
	}()
}
```

(Place this after `table` is constructed so it can be referenced.)

Add the imports to the file:

```go
"joxblox/internal/app/loader"
"joxblox/internal/debug"
```

(Skip whichever are already present.)

- [ ] **Step 4: Implement `recomputeTextureMatches`**

At file scope, near the other helper functions:

```go
func recomputeTextureMatches(state *renderdocTabState) {
	state.matchByTexID = map[string]int64{}
	state.matchAllByTexID = map[string][]int64{}
	if state.corpus == nil || state.report == nil {
		return
	}
	for _, tex := range state.report.Textures {
		if tex.DHash == 0 {
			continue
		}
		matches := state.corpus.Match(tex.DHash)
		if len(matches) == 0 {
			continue
		}
		state.matchByTexID[tex.ResourceID] = matches[0]
		state.matchAllByTexID[tex.ResourceID] = matches
	}
}
```

Also call `recomputeTextureMatches(state)` immediately after the existing `state.report = report` line in `onLoadFinished`, so newly loaded captures pick up the corpus matches.

- [ ] **Step 5: Render the Studio Asset cell**

Find `columnValue` (the function that maps `(TextureInfo, columnName) → string`). Add a case for `"Studio Asset"`:

```go
case "Studio Asset":
	id, ok := state.matchByTexID[texture.ResourceID]
	if !ok {
		if state.corpus == nil {
			return "—"
		}
		return "—"
	}
	if extras := len(state.matchAllByTexID[texture.ResourceID]) - 1; extras > 0 {
		return fmt.Sprintf("%d (+%d more)", id, extras)
	}
	return strconv.FormatInt(id, 10)
```

(`columnValue` currently doesn't take state — refactor to pass state in, or close over state in `newTexturesSubTab` if columnValue is defined inline. Do whichever is mechanically smaller given the existing code's structure.)

- [ ] **Step 6: Add the Studio Asset line to the preview pane**

Find the preview text-building site (`triggerPreview` / `previewInfoText`). After the existing dimension/format line, append:

```go
if id, ok := state.matchByTexID[texture.ResourceID]; ok {
	scan := loader.CurrentScan()
	name := assetNameFromScan(scan, id)
	infoLines = append(infoLines, fmt.Sprintf("Studio Asset: %d %s", id, name))
	if all := state.matchAllByTexID[texture.ResourceID]; len(all) > 1 {
		var extras []string
		for _, candidate := range all[1:] {
			extras = append(extras, strconv.FormatInt(candidate, 10))
		}
		infoLines = append(infoLines, "Also: "+strings.Join(extras, ", "))
	}
} else if state.corpus != nil {
	infoLines = append(infoLines, "Studio Asset: not identified")
} else {
	infoLines = append(infoLines, "Studio Asset: load a place file in the Scan tab to identify")
}
```

(Adapt the exact mechanics — `infoLines` slice, `previewInfoText` builder etc. — to whatever the existing code uses.)

Add the helper:

```go
func assetNameFromScan(scan []loader.ScanResult, assetID int64) string {
	for _, r := range scan {
		if r.AssetID == assetID && strings.TrimSpace(r.InstanceName) != "" {
			return "(" + r.InstanceName + ")"
		}
	}
	return ""
}
```

- [ ] **Step 7: Build + run tests**

Run: `go build ./... && go test ./internal/...`
Expected: PASS (modulo the pre-existing `TestPrivateTriangleCounts` unrelated failure).

- [ ] **Step 8: Commit**

```bash
git add internal/app/ui/tabs/renderdoc/renderdoc_tab.go
git commit -m "feat(renderdoc-tab): Studio Asset column + match overlay on Textures sub-tab"
```

---

## Task 7: Materials sub-tab — same pattern

**Files:**
- Modify: `internal/app/ui/tabs/renderdoc/materials_view.go`

**Why:** Identical wiring on the Materials sub-tab so the matched ID surfaces alongside the per-material Color/Normal/MR thumbnails.

- [ ] **Step 1: Add column header**

In the file's `materialColumnHeaders` slice, append `"Studio Asset"`.

- [ ] **Step 2: Add corpus + match maps to materialsTabState**

```go
corpus            *assetmatch.TextureCorpus
matchByTexID      map[string]int64
matchAllByTexID   map[string][]int64
```

Initialize in `newMaterialsSubTab` next to existing state init.

- [ ] **Step 3: Subscribe to scan events**

In `newMaterialsSubTab`, add the same subscription block as Task 6 Step 3, but storing into this tab's `state` and refreshing this tab's `table`. (Keep the unsubscribe-on-app-exit a known-leak for v1.)

- [ ] **Step 4: Implement `recomputeMaterialMatches`**

```go
func recomputeMaterialMatches(state *materialsTabState) {
	state.matchByTexID = map[string]int64{}
	state.matchAllByTexID = map[string][]int64{}
	if state.corpus == nil || state.textureReport == nil {
		return
	}
	for _, tex := range state.textureReport.Textures {
		if tex.DHash == 0 {
			continue
		}
		matches := state.corpus.Match(tex.DHash)
		if len(matches) == 0 {
			continue
		}
		state.matchByTexID[tex.ResourceID] = matches[0]
		state.matchAllByTexID[tex.ResourceID] = matches
	}
}
```

Call it from `onLoadFinished` right after `state.textureReport = textureReport`.

- [ ] **Step 5: Render the Studio Asset cell**

In `renderMaterialCell`, add a case for `"Studio Asset"`:

```go
case "Studio Asset":
	if mat.ColorTextureID == "" {
		label.SetText("—")
		label.Show()
		return
	}
	id, ok := state.matchByTexID[mat.ColorTextureID]
	if !ok {
		label.SetText("—")
	} else if extras := len(state.matchAllByTexID[mat.ColorTextureID]) - 1; extras > 0 {
		label.SetText(fmt.Sprintf("%d (+%d)", id, extras))
	} else {
		label.SetText(strconv.FormatInt(id, 10))
	}
	label.Show()
```

Add the column width in `applyMaterialColumnWidths`:

```go
table.SetColumnWidth(7, 110) // Studio Asset
```

- [ ] **Step 6: Update the preview-pane text**

In `updateMaterialPreview`, after the existing `Draws/Meshes/VRAM` line, add:

```go
if id, ok := state.matchByTexID[mat.ColorTextureID]; ok {
	fmt.Fprintf(&b, "Studio Asset (Color): %d\n", id)
	if all := state.matchAllByTexID[mat.ColorTextureID]; len(all) > 1 {
		var extras []string
		for _, c := range all[1:] {
			extras = append(extras, strconv.FormatInt(c, 10))
		}
		fmt.Fprintf(&b, "Also: %s\n", strings.Join(extras, ", "))
	}
} else if state.corpus != nil {
	b.WriteString("Studio Asset: not identified\n")
} else {
	b.WriteString("Studio Asset: load a place file in the Scan tab to identify\n")
}
```

- [ ] **Step 7: Build + smoke-build**

Run: `go build ./...`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/app/ui/tabs/renderdoc/materials_view.go
git commit -m "feat(renderdoc-tab): Studio Asset column + preview line on Materials sub-tab"
```

---

## Task 8: Meshes sub-tab — placeholder column

**Files:**
- Modify: `internal/app/ui/tabs/renderdoc/meshes_view.go`

**Why:** Spec says the Meshes sub-tab gets the column too, but it's empty in v1 (filled in only if the mesh feasibility spike succeeds and we ship a follow-up). Keeping the column in v1 means the column count stays consistent across sub-tabs and the future fill-in is a one-line change.

- [ ] **Step 1: Add column header**

Append `"Studio Asset"` to `meshColumnHeaders`.

- [ ] **Step 2: Render `—` for the new cell**

In the function that maps mesh column names to display strings, add:

```go
case "Studio Asset":
	return "—"
```

- [ ] **Step 3: Add a column width**

In `applyMeshColumnWidths` (or wherever mesh column widths are set), append a width for the new column:

```go
table.SetColumnWidth(8, 110) // Studio Asset
```

(Use the actual next column index — depends on the existing count.)

- [ ] **Step 4: Build**

Run: `go build ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/app/ui/tabs/renderdoc/meshes_view.go
git commit -m "feat(renderdoc-tab): placeholder Studio Asset column on Meshes sub-tab"
```

---

## Task 9: "Open in Single Asset" button on the preview pane

**Files:**
- Modify: `internal/app/ui/mesh_preview.go` (or wherever the existing `GetPrimaryWindow` package-level hook lives) — add a sibling `OpenSingleAsset` hook.
- Modify: `internal/app/app.go` — set the hook during app construction.
- Modify: `internal/app/ui/tabs/renderdoc/renderdoc_tab.go`
- Modify: `internal/app/ui/tabs/renderdoc/materials_view.go`

**Why:** Closing the loop — after seeing the matched ID, the user can jump to the existing Single Asset tab to inspect the source. Avoids an import cycle by using the same package-level-function-variable pattern as the existing `GetPrimaryWindow` hook in `internal/app/ui` (set by `app.go` at startup, called by tabs without importing `internal/app`).

- [ ] **Step 1: Add the package-level hook in `internal/app/ui`**

In `internal/app/ui/mesh_preview.go` (next to the existing `var GetPrimaryWindow func() fyne.Window` declaration around line 50), add:

```go
// OpenSingleAsset asks the main app to switch to the Single Asset tab
// and start loading the given asset ID. Set by app.go at startup;
// callers must nil-check.
var OpenSingleAsset func(assetID int64)
```

- [ ] **Step 2: Wire the hook from `app.go`**

In `internal/app/app.go`, find where the main `container.NewAppTabs` is constructed and the Single Asset tab item is held in a variable. After construction, set the hook:

```go
ui.OpenSingleAsset = func(assetID int64) {
	mainTabs.Select(singleAssetTabItem)
	if singleAssetTab != nil {
		singleAssetTab.LoadAssetByID(assetID)
	}
}
```

If `singleAssetTab` doesn't already expose `LoadAssetByID`, add a thin function-variable hook to the Single Asset tab the same way (a `var LoadCallback func(int64)` set by the constructor, called by the hook). Match the existing patterns in this codebase — don't introduce new ones.

(Substitute the actual variable names used in `app.go`. If single-asset tab construction doesn't expose enough surface for this, the smallest fix is a one-line capture: store the constructed tab into a package-level `var singleAssetTabRef *singleassettab.SingleAssetTab` so app.go and the hook closure can both reach it.)

- [ ] **Step 3: Add the button to the textures preview pane**

In `newTexturesSubTab`, where the preview pane is built, add an `Open in Single Asset` button:

```go
openInSingleAssetButton := widget.NewButton("Open in Single Asset", func() {
	if state.selectedRow < 0 || state.selectedRow >= len(state.displayTextures) {
		return
	}
	tex := state.displayTextures[state.selectedRow]
	id, ok := state.matchByTexID[tex.ResourceID]
	if !ok || ui.OpenSingleAsset == nil {
		return
	}
	ui.OpenSingleAsset(id)
})
openInSingleAssetButton.Hide()
```

(`ui` is already imported by this file via the existing helper imports — confirm before adding. The button hides when no match exists; toggle its visibility from `triggerPreview` based on `state.matchByTexID[tex.ResourceID]`.)

Add the button to the preview pane's container. Place it under the info label.

- [ ] **Step 4: Same button in Materials preview pane**

Mirror Step 3 inside `newMaterialsSubTab`, placed under the existing preview info entry. Show when `state.matchByTexID[mat.ColorTextureID]` exists; hide otherwise. Toggle visibility from `updateMaterialPreview`.

- [ ] **Step 4: Build**

Run: `go build ./...`
Expected: PASS.

- [ ] **Step 5: Manual smoke test**

Run: `go run ./cmd/joxblox`
1. Scan a place file (Scan tab) and wait for results to populate.
2. Switch to RenderDoc, load a `.rdc` capture from gameplay in that place.
3. Open the Textures sub-tab — Studio Asset column should show matched IDs for textures that came from the place. Click a row → preview pane shows the asset ID and the Open button.
4. Click Open in Single Asset → main UI switches to the Single Asset tab, asset ID is auto-loaded.

- [ ] **Step 6: Commit**

```bash
git add internal/app/ui/tabs/renderdoc/renderdoc_tab.go internal/app/ui/tabs/renderdoc/materials_view.go internal/app/app.go
git commit -m "feat(renderdoc-tab): Open in Single Asset button on preview panes"
```

---

## Task 10: Mesh feasibility spike — README

**Files:**
- Create: `tools/mesh-hash-probe/README.md`

**Why:** The mesh feasibility spike was scoped in the spec as a separate, manual-run experiment with manual findings. To keep this plan executable end-to-end without unfinished code stubs, this task drops a README that scopes the spike — the actual probe implementation and findings doc become a separate follow-up after this branch lands.

- [ ] **Step 1: Create the README**

Create `tools/mesh-hash-probe/README.md`:

```markdown
# mesh-hash-probe

Feasibility spike for the **mesh** side of asset-ID mapping. The texture
side ships in this branch; whether the mesh side is worth doing depends
on whether GPU-side captured mesh bytes can be matched back to source
`.mesh` asset bytes (the engine may reformat positions, batch meshes
together, or otherwise transform vertex data before upload).

## What this tool will do (when implemented)

Given a captured `.rdc` (already converted to `zip.xml` via
`renderdoccmd convert`) and one or more known Roblox `.mesh` asset IDs:

1. Parse the captured XML for every Vertex Buffer's `InitialData` blob
   and decode its bytes as `R32G32B32_FLOAT` position triples (skip
   buffers with mismatched stride).
2. For each provided asset ID: download via the Roblox asset delivery
   endpoint, parse the `.mesh` binary, extract the source position list.
3. Compare each (captured VB, source mesh) pair under three criteria:
   - **Exact byte match** of position bytes
   - **Sorted-position match** (same positions, possibly reordered)
   - **Same position count** (likely reformatted, not byte-identical)
4. Print a Markdown summary to stdout.

## Decision criteria

After running against 5–10 mesh assets from a known place + capture
pair the engineer controls, write findings to
`docs/superpowers/findings/2026-MM-DD-mesh-hash-feasibility.md`:

- Most cases EXACT byte match → mesh mapping is straightforward; write a
  follow-up spec for full mesh asset-ID mapping (mirror of textures).
- Most cases SORTED-position match → mapping is feasible but needs
  position-set hashing instead of byte hashing; document the approach
  in the follow-up spec.
- Mostly count-match or no match → mapping is not feasible without
  reverse-engineering the engine's mesh upload format. Document why and
  shelve.

## Implementation notes for the executing engineer

- Live as its own module like `tools/mesh-renderer` so it can `go build`
  independently. Don't add `tools/mesh-hash-probe` to the joxblox `go.mod`.
- The renderdoc XML position-decoding logic lives in
  [internal/renderdoc/parse.go](../../internal/renderdoc/parse.go) and
  [internal/renderdoc/buffers.go](../../internal/renderdoc/buffers.go);
  inline a minimal stripped-down version of just what's needed to read
  `<chunk name="ID3D11Device::CreateBuffer">` blobs.
- The `.mesh` binary parsing logic lives in
  [internal/roblox/mesh/mesh.go](../../internal/roblox/mesh/mesh.go);
  inline just the position-extraction path.
- HTTP fetch URL: `https://assetdelivery.roblox.com/v1/asset/?id=<id>`
  (public assets; for private use the `.ROBLOSECURITY` cookie via the
  same `Cookie` header `internal/roblox/auth.go` constructs).
- This is investigative code, not production. Hard-code stuff, log
  liberally, throw away when the answer is found.
```

- [ ] **Step 2: Commit**

```bash
git add tools/mesh-hash-probe/README.md
git commit -m "docs(tools): mesh-hash-probe README + decision criteria"
```

---

## Task 11: Final verification + CHANGELOG

- [ ] **Step 1: Run the full test suite**

Run: `go test ./... && cd tools/mesh-renderer && go test ./... && cd ../mesh-hash-probe && go build .`
Expected: PASS (modulo the pre-existing `TestPrivateTriangleCounts` failure in `internal/app/loader`, which is unrelated to this work).

- [ ] **Step 2: Vet**

Run: `go vet ./...`
Expected: no warnings.

- [ ] **Step 3: Update CHANGELOG**

Edit `CHANGELOG.md`. Under the existing `## Unreleased` / `### Added` block, append:

```markdown
- Asset-ID mapping in the RenderDoc tab — when a place file is loaded in the Scan tab, the Materials and Textures sub-tabs show a `Studio Asset` column identifying captured textures by perceptual-hash match, plus a clickable "Open in Single Asset" link in the preview pane
```

- [ ] **Step 4: Commit**

```bash
git add CHANGELOG.md
git commit -m "docs(changelog): note asset-ID mapping for RenderDoc captures"
```
