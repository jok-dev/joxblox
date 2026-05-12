# Ideas

Running list of features we'd like to explore. Not a roadmap — just a parking lot.

## Mesh / 3D

- **Mesh optimization.** Find duplicate meshes across a place, suggest LOD generation (e.g. via meshoptimizer), flag meshes that are higher-poly than they need to be at typical view distance.

## Reporting & Collaboration

- **Better / polished report generation.** Take the existing report tab further — exportable HTML or PDF with charts, asset thumbnails, before/after for optimizations, ready to hand to a producer or post in a Slack thread.
- **Asset tagging with portable bundles.** Let users tag assets (e.g. `reviewed`, `approved`, `needs-fix`, free-form notes) and persist that state. Then export a compressed `.jox` bundle containing the asset references, tags, notes, and maybe cached previews so a teammate can open it, action items, and send a bundle back.
  - Stretch: optional server-side sync for real-time collaboration (probably overkill for v1, but worth keeping in mind so the bundle format doesn't paint us into a corner).

## RenderDoc (mesh-side asset mapping)

- **Map captured meshes back to Studio asset IDs.** Texture-side mapping is shipped; mesh-side hangs on whether GPU-uploaded vertex bytes match source `.mesh` bytes after engine processing. Spike scaffolded at [tools/mesh-hash-probe](../tools/mesh-hash-probe) — needs a manual run + findings doc before we know whether to spec the full feature.

## Optimization Loop

- **Apply tags — write back to the rbxl.** Today the `Downscale` / `Atlas` / `Decimate` / `Remove Alpha` / `Duplicated` / `Remove` tags are descriptive only. Build an `Apply tags` pass that actually downscales tagged textures, packs tagged atlas groups (rewriting Decal/SurfaceAppearance UVs and refs), decimates tagged meshes, strips alpha channels where flagged, collapses Duplicated groups to a single canonical asset (rewriting every reference), and removes flagged assets — emitting a new optimized `.rbxl` plus a diff report of bytes/GPU-memory saved. Turns Joxblox from an audit tool into the optimization tool. Cheapest tag to land first is probably `Duplicated` (pure ref-rewrite, no asset rebuild); `Atlas` is the hardest (UV remap + sampler awareness). Probably wants a dry-run preview tab so users can review changes before writing.

## Live Editor Integration

- **Studio plugin / live mode.** Replace the export-rbxl → scan → fix → re-export round-trip with live analysis against the open Studio session (Studio plugin via the plugin API, or the `Roblox_Studio` MCP bridge for a desktop-side variant). Grades update as the user edits; clicking a flagged asset selects it in Studio. Tight feedback loop is what makes lint-style tools sticky, and it sidesteps the "I have to remember to run a scan before shipping" problem. Open question: how much of the existing analysis pipeline can run incrementally per-edit vs needing a full re-scan, and whether the Studio plugin sandbox can call out to the Rust extractor / GPU memory model or has to reimplement them in Luau.
