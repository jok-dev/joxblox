# Changelog

## Unreleased

### Added

- Diff tab: compare two .rbxl files and view added, removed, and changed instances with per-property diffs. Script files are ignored by default; toggle off to include them. The `TexturePack` property is never diffed — it's a server-side CDN wrapper that's not user-controlled and only produces noise. Right-click any row to copy the instance to the clipboard in Roblox Studio's native format — paste it back into Studio with Ctrl+V.

## v1.5.0 - 2026-05-08

### Added

- Scan: right-click any result row for a context menu with `Copy` (Asset ID / Asset Reference / SHA256) and `Tag` (Downscale, Duplicated, Decimate, Atlas, Remove Alpha, Remove). Active tags show in a new `Tags` column. A `HTML Tag Report` button on the scan toolbar then generates a self-contained HTML page with one section per tag, each showing the tagged assets as image cards (asset ID, type, dimensions, GPU texture memory, instance path). Tags live in memory for the session and are not persisted across rescans
- Scan: `Duplicated` tag opens a picker dialog instead of toggling on a single row. The right-clicked asset is preselected at the top with its preview already showing; the dialog has a side-by-side layout (candidate table on the left, asset preview on the right). Single-click any row to swap the preview to that asset; double-click to add or remove it from the group. Candidates are sorted by perceptual-hash similarity (dHash Hamming distance) to the primary asset, with a `Similarity` column showing each row's `100% (0)`-style score so the user can scan the top of the list for near-identical content; rows that can't be hashed (no decodable image bytes, or the primary itself is a non-image) fall back to SHA256 clustering. Editing an existing group reopens the same dialog with all current members preselected. The HTML Tag Report renders each curated group as its own `Group N — N copies` block so reviewers can see at a glance which copies are flagged as variants of which
- Scan: new `PDF Tag Report` button next to `HTML Tag Report` — exports the same per-tag breakdown (and curated `Group N — N copies` sub-blocks for Duplicated) as a self-contained PDF with embedded thumbnails. Built with `gofpdf` so it ships in the binary; no Chrome / external renderer required. The asset card grid wraps at four cards per A4 row, paginates automatically, and falls back to a "(no preview)" placeholder when an asset's `Resource` isn't a PDF-embeddable format
- Scan: SHA256-detected duplicate clusters auto-tag as `Duplicated` groups when a scan completes — the same grouping the picker dialog produces, but seeded from byte-identical files the loader already deduplicated. Manual groups created via the dialog always win: an auto-tag run skips any cluster whose members overlap a user-curated group, and re-running auto-tag on an already-grouped cluster is a no-op
- Scan: new `Show untagged` filter checkbox next to `Show duplicates` and `Show large textures` — hides any row that already carries at least one tag, so triagers can drill into "rows that still need a decision" after burning through the obvious tags. The existing `Show only duplicates` checkbox is renamed to `Show duplicates` for symmetry
- Scan: `Path Filter` checkbox now defaults on, scoped to `Workspace.*` and `MaterialService.*`, so fresh scans skip the editor-only / tooling tail of an rbxl by default. Untick it to walk the full instance tree
- Scan: default sort is now `GPU Memory` (descending) instead of `Self Size` — the metric that drives in-game VRAM cost surfaces first, so triagers see the heaviest textures at the top without having to re-sort. Heatmap variant still defaults to `Total Byte Size`

### Fixed

- Scan: Materials sub-tab's `Engine GPU memory` headline now matches the report tab's `GPU Texture Memory`. `ScanResult` collapses per-asset references into one row keyed by `(AssetID, AssetInput)`, so a Color asset shared across N SurfaceAppearance instances was previously projecting only its primary `InstancePath` into the engine-model materials map — losing the per-bundle relationship for the other N-1 instances and undercounting MR packs / normal-upscale pairings. `AllInstancePaths` now propagates from `ScanHit` into `ScanResult`, and the materials-map builder walks every owning path so every per-bundle MR pack and color-paired normal upscale is reconstructed correctly
- Scan: when an asset's full preview was lazy-loaded after row selection (because the streaming scan only got a thumbnail or no Source), the freshly-loaded `Resource` / `DownloadBytes` / `FileSHA256` / dimensions weren't being persisted back into the explorer's master row list. That made the duplicate-picker dialog's `Similarity` column render `-` for every row even when the primary and candidates had matching SHA256s — the SHA fast-path saw an empty primary SHA and the dHash path saw an empty primary `Resource`. The lazy-load completion now writes the loaded result back into `allResults` (matched by `AssetReferenceKey`) so subsequent reads see the populated row
- Scan: duplicate-picker dialog now synchronously lazy-fetches the primary's full preview when its bytes / SHA aren't already in `allResults`, so the `Similarity` column populates even on the first open before any async load has happened. The status line gained a `Ranked X/Y candidates` counter so users can tell at a glance whether the column is empty due to no matches versus a missing primary preview
- Scan: similarity dHash now falls back to the rendered `Resource.Content()` (PNG/JPEG) when the raw `DownloadBytes` from AssetDelivery aren't decodable by Go's stdlib `image` package — Roblox sometimes ships textures in formats stdlib doesn't read (KTX2, custom containers), and the rendered preview is the reliable fallback. Previously every row whose primary fell into that bucket rendered "-" in the Similarity column even when matches existed
- Scan: similarity ranking for normal-map slots now hashes the R and G channels independently and sums their Hamming distances, instead of using the standard luminance dHash. Normal maps encode XY surface deflection in R/G with the Z (blue) channel near-uniform, which dominated luminance and made every normal map look like every other one to the old hash; the dual-channel variant catches X-only vs Y-only deflection patterns that luminance collapses. Detected automatically from the row's `PropertyName` (NormalMap / NormalMapContent) and applied to every candidate so distances stay comparable; the `Similarity` column scales its percentage to the doubled bit space (112 vs 56) so identical files still read `100% (0)`
- Report Generation: `Export JSON` button — saves the full graded report (overall, per-grade letters/scores/values, summary stats, per-cell avg/p90/max metrics, mismatched-PBR / oversized-texture / duplicate-group drill-downs, asset type, source file metadata) as a single JSON file via the native save dialog. Versioned schema (`format_version`) so a future viewer / share-link server can render historical exports without depending on internal types

### Added

- Report Generation: per-asset-type hard cap on individual texture GPU-memory footprint. `DE Map` reports flag any texture whose largest per-slot GPU memory (BC1 or BC3, with mips) exceeds the configured limit and force the overall grade to F regardless of every other metric. A red banner above the grade table shows the offender count with a `View` button listing each texture's asset ID, resolution, GPU-bytes/format, and instance path, plus an `Ignore` button that suppresses the F override and reveals the natural grade (with a `Restore` button to undo)
- Scan: top-bar `Asset type` selector. When set to a type with a configured GPU-memory ban (e.g. `DE Map`), result rows whose largest per-slot GPU footprint exceeds the limit are highlighted in red so banned textures are immediately obvious in scan-mode triage
- Scan: new `Materials` sub-tab next to the asset table. Shows engine-deduplicated PBR materials — one row per unique (color/normal/metalness/roughness) asset combo — with thumbnail cells for each slot, the authored slot sizes, the engine-effective normal size (upscaled to its largest paired color), the MR-pack size (max of group normal/color/M/R), the BC1+BC3 GPU bytes the engine actually allocates for that material's share, and how many SurfaceAppearance instances reuse the bundle. The same table also surfaces every non-PBR Image asset (Decal/Texture/ImageLabel/MeshPart.TextureID/etc.) as a single-slot row tagged `image` in the PBR column, deduped per asset across all references, so users can see every image-bearing asset in one place. A filter entry above the table searches asset IDs and instance paths; clicking a row opens a 4-up Color/Normal/Metalness/Roughness preview pane on the right showing the authored sizes, GPU breakdown, mismatched-PBR flag, and full instance path. Visual shell mirrors the RenderDoc `Materials` sub-tab — both share the same preview pane, thumbnail cell, and decode-cache code in `internal/app/ui/materialscommon`. A header summary shows the engine GPU-memory total — dedupes shared normal assets, accounts for blank-MR packs, plus the non-PBR images at their authored BC1/BC3 size
- Material variant warnings: `Ignore` button next to the existing `View` button so the warning banner can be dismissed for the current load without dismissing the underlying data

### Changed

- Report Generation: `View in Scan` no longer re-fetches every asset — the previews already loaded for the report are handed off directly to the Scan tab, so the table appears immediately. The Scan `Asset type` selector is auto-set to match the report
- Report Generation: spatial grades now show two columns — `typical` (content-weighted average, Σv²/Σv — the cell value an average unit of content sits in, ignoring the long tail of near-empty cells) and `max/cell`. The `avg/cell` and `p90/cell` columns are gone
- Report Generation: row grades are now the average of the typical-cell sub-score and the p90-cell sub-score (max no longer contributes to the grade — it's informational only)
- Report Generation: `Mismatched PBR Maps` now counts unique asset combos rather than raw SurfaceAppearance instances — multiple instances sharing the same (color/normal/metalness/roughness) bundle collapse to one entry, since fixing the asset fixes every usage. The grade threshold grades against the unique-combo count, the value cell shows e.g. `3 (8.00 MB)`, and the `View` dialog shows one row per combo with an `×N instances / save N MB` suffix, sorted by per-combo savings desc (then instance count desc) so the highest-impact fixes float to the top
- Report Generation: `Mismatched PBR Maps` value cell now also shows the GPU-memory savings from downscaling each mismatched combo's bigger-than-color slots to match its color map. Computed via the engine allocation model with proper asset deduplication: a single oversized texture shared across many combos counts once, not once per usage; a shared asset still needed at full size by any matched combo correctly contributes zero
- Scan: result table now lives under an `Assets` sub-tab. The asset table itself is unchanged — every row keeps showing the literal authored size and per-asset GPU footprint

## v1.4.2 - 2026-04-28

### Added

- Single Asset tab: `Upload Image` button — pick a local PNG/JPEG/GIF/BMP/WEBP and the tab treats it like a Roblox image asset (preview, alpha analysis, downscale variants, recompressed-size readout) without round-tripping through Roblox

### Changed

- Report Generation: overall score now uses weighted grades instead of an even average — GPU Texture Memory and Mesh Complexity each weight 25%, Draw Calls 20%, the remaining nine grades split the last 30% evenly
- Report Generation: spatial-mode metrics now grade their avg/p90/max cell readings independently and average the three sub-scores into the row grade. Avg has its own (tighter) threshold; p90 and max share the existing headline threshold. So a uniformly busy scene grades worse than a single-hotspot scene even when their p90s match

## v1.4.1 - 2026-04-28

### Added

- `Materials` sub-tab in the RenderDoc tab — groups PS-bound textures into deduplicated PBR materials (Color + Normal + MR), with per-material draw counts, mesh usage, and VRAM totals
- Mesh previews now render with proper Phong lighting (key + camera-relative fill + rim) and contact shadows from a fixed key light, replacing the previous flat unlit silhouette
- Three viewmodes selectable from a small toolbar above every 3D mesh preview: `Lit` (neutral clay shading), `Color` (per-vertex colors), `Normals` (RGB-mapped surface normals for triangulation/seam debugging)
- Subtle ground grid for spatial reference in 3D previews
- Asset-ID mapping in the RenderDoc tab — when a place file is loaded in the Scan tab, the Materials and Textures sub-tabs show a `Studio Asset` column identifying captured textures by perceptual-hash match, plus a clickable "Open in Single Asset" button in the preview pane
- RenderDoc Recording mode — Record/Stop toggle that auto-fires F12 every Ns, processes captures in the background, deduplicates textures by perceptual hash, and shows the unique-texture aggregate in the Textures sub-tab. Source `.rdc`s are deleted after extraction so disk usage stays bounded.
- Report Generation: `Mismatched PBR Maps` grade — flags SurfaceAppearance materials whose color/normal/metalness/roughness textures aren't all at the same source resolution, with a `View` button listing each material's authored slot sizes
- Report Generation: `Instances` grade — total descendant count of the rbxl/rbxm tree (any class), graded on p90/cell of positionable instances when spatial data is available
- Report Generation: spatial grades now show `avg/cell`, `p90/cell`, and `max/cell` columns side-by-side; suppressed for whole-file asset types (vehicles)
- Report Generation: `View` buttons on `Oversized Textures` and `Duplicates` rows — open dialogs listing the offending textures (asset ID, resolution, size, score) and duplicate groups (copies, wasted bytes, asset IDs, sample path)

### Changed

- Report Generation: removed `Total Size` and `Texture Size` grades (subsumed by GPU Texture Memory and Mesh Size); replaced `Wasteful BC3 Textures` with `Mismatched PBR Maps`; reordered so Mismatched + Oversized sit just above Duplicates
- Heatmap 2D top-down map view is now unlit so part colors and heatmap tints render true-to-source instead of crushed near-black by the Phong shader's stale lighting state

## v1.1.0 - 2026-03-26

Expanded `Joxblox` with new RBXL analysis, optimization, and mesh-inspection workflows for larger asset review sessions.

### Added

- `Heatmap` tab for generating weighted `.rbxl` asset heatmaps, optional map underlays, and diff comparisons
- `Optimize Assets` tab for filtering place references, resizing image assets, and re-uploading optimized copies through Roblox Open Cloud
- Interactive mesh preview rendering and richer mesh stats in the asset preview flow
- Shared results explorer support for similarity-ranked asset browsing and richer scan/heatmap stats
- `Settings` dialog for configuring an on-disk asset download cache
- Go and Rust test coverage for the new request-source, mesh, cache, and HTTP helper paths

### Changed

- Improved `.rbxl` scanning and asset preview plumbing with shared request-source metadata, preview adapters, and download caching support
- Added startup validation and clearer status reporting for saved `.ROBLOSECURITY` cookies
- Expanded release packaging and CI coverage for the bundled Rust helper, changelog/license assets, and cross-language test runs

### Notes

- Release builds continue to embed the `joxblox-rusty-asset-tool` payload used for `.rbxl`, heatmap, and mesh workflows
- `COREMESH` mesh support remains most validated on Windows MSVC even though Linux/macOS paths are expected to work

## v1.0.0 - 2026-03-23

Initial public release of `Joxblox`.

### Added

- `Single Asset` tab for loading Roblox asset IDs with preview, metadata, hierarchy browsing, and raw API/ extractor JSON views
- `Scan` tab with `RBXL` and `Folders` sources, each supporting `Single` and `Diff` modes
- Sortable scan table with search, duplicate filtering, asset type filtering, instance type filtering, and property name filtering
- Scan stats for rows, shown rows, failed rows, duplicate count, duplicate size, and shown size
- Reference metadata display for instance type, property name, and instance path
- JSON import/export for scan tables and full scan workspaces
- Drag-and-drop JSON import support
- Recent file history for imported results
- Preview variant selection with displayed size and percentage change from the original asset
- Expanded preview window with zoom and pan support
- `Image Generator` tab for creating PNG files for manual Roblox upload workflows
- Optional `.ROBLOSECURITY` authentication with OS keychain / credential-store persistence
- Global `File` menu actions for saving, loading, and clearing scan results
- Global `Help` menu support for viewing the changelog, about info, and license details

### Changed

- Improved UI responsiveness by moving preview variant generation off the UI thread
- Reduced scan redraw churn with debounced searching and throttled refresh behavior
- Improved image rendering responsiveness using faster Fyne scaling settings
- Added determinate JSON save/load progress dialogs

### Notes

- Release builds package the changelog, license text, and RBXL extractor support directly into the shipped app binary
