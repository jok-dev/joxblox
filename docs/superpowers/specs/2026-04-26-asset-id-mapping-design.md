# Asset-ID Mapping — Design

**Date:** 2026-04-26
**Status:** Approved, ready for plan

## Goal

When the user has a `.rbxl/.rbxm` scan loaded (Scan tab) and a RenderDoc capture loaded, automatically identify which Roblox Studio asset IDs each captured texture corresponds to. Surface the matches in the RenderDoc Materials, Textures, and Meshes sub-tabs. Plus a small mesh-side feasibility experiment to decide whether mesh mapping warrants a follow-up spec.

## Non-goals

- Mesh-asset matching as a shipped feature (only the feasibility spike).
- Persistent on-disk corpus.
- Live re-matching as the user edits a place file.
- Confidence visualization beyond "first match wins, additional matches listed."
- Cross-place corpus building (matching one capture against multiple scanned places at once).
- Audio / sound asset matching.

## Architecture

A new `internal/assetmatch` package owns corpus build + lookup. It's tab-agnostic — built from a `[]loader.ScanResult` slice and queried by a perceptual hash. Dependency direction: `assetmatch` imports `loader` (for `ScanResult`, `DownloadRobloxContentBytesWithCacheKey`, and `ComputeImageDHash`) but is imported by `app/ui/tabs/renderdoc`. The `internal/renderdoc` package itself stays independent of `assetmatch` — it only gains a `DHash uint64` field on `TextureInfo`, which is just a number, no import needed.

The Scan tab fires a "scan completed" signal; the RenderDoc tab subscribes and rebuilds its match overlay. Both sides operate independently — the texture pipeline doesn't depend on a corpus existing, and the corpus build doesn't depend on a capture being loaded.

## Corpus build (textures)

```go
type TextureCorpus struct {
    byHash     map[uint64][]int64 // dHash → asset IDs (multiple IDs may share a hash)
    byAssetID  map[int64]uint64   // asset ID → dHash (reverse lookup for display)
    sourceFile string             // .rbxl/.rbxm this corpus was built from
    builtAt    time.Time
}

func BuildTextureCorpus(scan []loader.ScanResult, onProgress func(done, total int)) (*TextureCorpus, error)
func (c *TextureCorpus) Match(captureHash uint64) []int64
```

**Algorithm:**

1. Filter scan results to `AssetTypeName == "Image"`. Drop other types.
2. For each image asset (parallel workers, ~8 max — same pattern as `ComputeTextureHashes`):
   - Download bytes via existing `DownloadRobloxContentBytesWithCacheKey` (on-disk cache hit on subsequent runs).
   - Decode to RGBA.
   - Compute dHash via existing `ComputeImageDHash`.
   - Append `assetID` to `byHash[hash]`; set `byAssetID[assetID] = hash`.
3. Progress callback fires after each asset; intended for the launcher status label.

`Match` returns asset IDs whose dHash is within `defaultMatchHammingDistance = 6` of the query hash. Sorted by ascending Hamming distance (best match first). Empty slice if nothing within threshold. Per-call lookup is O(N) over the corpus — fine for the typical few-hundred-image case; if it becomes a bottleneck, switch to a BK-tree later.

The choice of dHash over an exact pixel-hash match is deliberate: GPU-decoded textures are BC-compressed by the engine before upload, so their decoded pixels carry compression artifacts the source PNG doesn't have. Exact hashing wouldn't match. dHash with a small Hamming distance is robust to that artifact pattern.

## Capture-side hashing

Add `DHash uint64` field to `renderdoc.TextureInfo` alongside the existing `PixelHash string` (truncated SHA-256 of decoded base mip).

`ComputeTextureHashes` populates both during the same decode pass — no extra image decoding work. The dHash uses the same `ComputeImageDHash` function as the corpus side, so capture and corpus hashes are directly comparable.

## Match overlay (per RenderDoc capture)

```go
// In renderdoctab — one of these per loaded capture, alongside the
// existing per-tab state structs.
type matchOverlay struct {
    byTextureID map[string]int64   // best match per captured texture resource ID
    byTextureID_all map[string][]int64 // all candidates within threshold
}
```

Computed once per (capture, corpus) pair:
- Triggered when capture loads OR when corpus build finishes (whichever is later).
- Walks `report.Textures`, queries `corpus.Match(tex.DHash)`, populates the overlay.
- Cells re-render via `table.Refresh()`.

No on-click work; everything is precomputed once both inputs are present.

## UI: where matches surface

### Column in each sub-tab's table

Add a `Studio Asset` column to the Materials, Textures, and Meshes sub-tabs.

| Sub-tab    | What the column shows                                                              |
|------------|------------------------------------------------------------------------------------|
| Textures   | Matched asset ID for the row's texture, or `—`. `(+N more)` suffix when multi-hit. |
| Materials  | Matched asset ID for the material's Color slot (most recognizable), or `—`.        |
| Meshes     | Empty in v1. Filled later if the mesh feasibility spike succeeds.                  |

Clicking the column header sorts; columns are filterable via the existing filter entry.

### Preview pane

Selected row's preview pane gains a labeled "Studio Asset" line:

- `Studio Asset: 12345 (image, 1024×1024)` — uses existing `loader.ScanResult` metadata for type/dimensions, looked up via the same scan that built the corpus.
- An "Open in Single Asset" button. Clicking switches to the Single Asset tab and triggers its existing load-by-ID flow.
- When multiple matches exist: list each with its Hamming distance, e.g. `Also: 67890 (d=3), 11111 (d=5)`.

### When no scan is loaded

Column shows `—` for all rows. Preview pane shows: `Load a place file in the Scan tab to identify assets.`

### When corpus is mid-build

Column shows `…` for all rows; preview pane shows progress (`Building corpus: 42/300`). When build completes, cells re-render with matches.

## Wiring (Scan tab → RenderDoc tab)

The Scan tab needs to expose its current results outside its own goroutines. Add a small global in `internal/app/loader`:

```go
// CurrentScan returns the most-recently-completed scan results, or nil
// if no scan has been run this session. Safe to call from any goroutine.
func CurrentScan() []ScanResult
func SubscribeScanCompleted(callback func()) (unsubscribe func())
```

The Scan tab's existing scan-completion code path calls a new `loader.PublishScanCompleted(results)` after a scan finishes. The RenderDoc tab subscribes at construction and rebuilds the corpus on each completion event.

Why not pass results through props/closures: the two tabs are constructed independently in `app.go`, and threading state through that wiring would touch many files. A simple package-level event is the lightest possible coupling.

## Mesh feasibility spike (separate, smaller deliverable)

Not a feature. A one-shot debug command at `tools/mesh-hash-probe/` that:

1. Takes a captured `.rdc` path and one or more known Roblox `.mesh` asset IDs (passed as args).
2. For each asset ID:
   - Downloads the source `.mesh` (cached) and parses it via existing `internal/roblox/mesh`.
   - Extracts captured mesh positions from the `.rdc` via existing `renderdoc.DecodePositions`.
   - Compares: exact byte hash, sorted-position hash, position count, bounding-box overlap.
3. Writes a Markdown findings report to `docs/superpowers/findings/2026-MM-DD-mesh-hash-feasibility.md` summarizing which comparisons succeeded.

Run manually against 5–10 mesh assets from a place + capture pair the user controls. Outcome shapes whether we write a follow-up spec for full mesh mapping. **Texture mapping ships regardless of spike outcome.**

## Caching

- **Asset bytes:** existing `DownloadRobloxContentBytesWithCacheKey` already caches PNG/JPG bytes on disk. No change.
- **Decoded images:** not cached (decode is fast, memory is the constraint).
- **Corpus itself:** in-memory only for v1. On app restart, the user re-runs their scan; corpus rebuild is fast because asset bytes are cached. Persisting the corpus to disk is a v2 concern.

## Edge cases

- **Capture loaded, no scan.** Column shows `—`. Preview pane shows the "load a scan" prompt. No corpus query attempted.
- **Scan loaded, no capture.** Corpus builds in background. Once a capture loads, matching runs immediately.
- **Corpus mid-build when capture loads.** Matching is deferred until the build callback completes; rows show `…` in the meantime.
- **Asset download fails for an individual entry.** That entry is skipped, logged at debug level. Other entries proceed.
- **Hash collision (different assets sharing a dHash).** Both surface in the multi-match list. The column shows the first by ID; the preview shows all.
- **`rbxthumb://` asset references.** Per CLAUDE.md, the prefix must be preserved when passed to the downloader. Corpus build feeds the asset's full reference (not just the bare ID) into `DownloadRobloxContentBytesWithCacheKey`.
- **Texture too small to perceptually hash meaningfully** (< 16×16). Skip from the corpus and from capture-side hashing — dHash on tiny images is essentially random.

## Testing

- `internal/assetmatch/corpus_test.go` (new):
  - Exact match (corpus and query produce identical hashes)
  - Near match within threshold
  - No match (distance > threshold)
  - Multi-match (two corpus IDs share a hash)
  - Empty corpus / nil query
  - Filter excludes non-image asset types
- `internal/renderdoc/hash_test.go` extension: asserts `DHash` is populated alongside `PixelHash` on the existing test fixture.
- `internal/app/loader/event_test.go` (new): `PublishScanCompleted` calls every active subscriber exactly once; `Subscribe` returns a working unsubscribe.
- No UI tests, consistent with existing pattern.

## Build sequence (rough)

1. `internal/app/loader` event bus: `CurrentScan`, `Subscribe`, `Publish`. Tests.
2. Wire Scan tab to call `Publish` after a successful scan.
3. `internal/assetmatch` package: `TextureCorpus` + `Match` + builder. Tests.
4. `renderdoc.TextureInfo.DHash` field + populate in `ComputeTextureHashes`. Test.
5. RenderDoc tab: subscribe to scan events, build corpus, populate match overlay.
6. Add Studio Asset column to Textures, Materials, Meshes sub-tabs.
7. Add Studio Asset preview-pane line + "Open in Single Asset" button.
8. Mesh feasibility spike: `tools/mesh-hash-probe/main.go`. Run manually. Findings doc.
