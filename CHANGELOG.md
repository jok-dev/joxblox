# Changelog

## Unreleased

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
