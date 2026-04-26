# RenderDoc PBR Material Pairing — Design

**Date:** 2026-04-26
**Status:** Approved, ready for plan

## Goal

Turn the existing RenderDoc Textures sub-tab's flat list into a structured "Materials" view. Each material is one `(Color, Normal, MR)` tuple, deduped across the capture, with the meshes that use it. Lets a reader answer "what does the GPU think this surface looks like" instead of "here are 600 unrelated textures."

## Non-goals

- Cross-tab navigation (click material → jump to mesh row). Useful, but adds shared state across sub-tabs. Defer.
- Editing or exporting materials.
- Materials view for non-D3D11 captures (Vulkan, GL).
- Hand-curating which built-in textures count as "scene-global." The 80% draw-call threshold plus existing built-in categories should cover it.
- Mapping materials back to Studio asset IDs. Tracked separately in [IDEAS.md](../../../IDEAS.md); needs its own spec.

## Parser additions (`internal/renderdoc/`)

### Track PS-bound textures

Extend [parse.go](../../../internal/renderdoc/parse.go) (or [meshes.go](../../../internal/renderdoc/meshes.go) — wherever the IA tracking sits) to handle two new chunks:

- `ID3D11Device::CreateShaderResourceView` — record SRV resource ID → underlying texture resource ID. D3D11 binds SRVs, not textures directly; without this map, PS bindings can't be resolved to the textures they cover.
- `ID3D11DeviceContext::PSSetShaderResources` — maintain a current-binding `[]string` of SRV IDs indexed by slot. Mirror the existing `IASetVertexBuffers` / `IASetIndexBuffer` pattern.

### Snapshot per draw call

Add `DrawCall.PSTextureIDs []string` (slot-indexed, length = highest bound slot + 1, with empty strings for unbound slots). At each `DrawIndexed` / `DrawIndexedInstanced`, copy the current PS bindings into the new draw call, resolving SRV IDs to texture IDs via the SRV map.

## Material building (`internal/renderdoc/materials.go`, new)

```go
type Material struct {
    ColorTextureID  string   // "" if none
    NormalTextureID string
    MRTextureID     string
    OtherTextureIDs []string // per-material PS textures we couldn't classify
    DrawCallCount   int
    TotalBytes      int64    // sum of bytes of unique maps
    MeshHashes      []string // dedup of meshes this material draws
}

func BuildMaterials(textures *Report, meshes *MeshReport) []Material
```

### Algorithm

1. **Build the "scene-global" texture-ID set.**
   - Any texture whose `Category` is `CategoryBuiltin*`, `CategoryRenderTgt`, `CategoryDepthTgt`, or `CategoryCubemap`.
   - **Plus** any texture bound to ≥80% of all draw calls (catches uncategorised globals like shadow maps, env probes, BRDF LUT not yet in the curated list).
2. **Per draw call**, drop scene-global IDs from `PSTextureIDs`. The remainder is the material's per-draw texture set.
3. **Classify** each remaining texture by its existing `Category`:
   - Normal slot ← `CategoryNormalDXT5nm`
   - MR slot ← `CategoryBlankMR` or `CategoryCustomMR`
   - Color slot ← `CategoryAssetOpaque` / `CategoryAssetAlpha` / `CategoryAssetRaw`
   - For each of the three slots: if multiple textures qualify, the one in the lowest PS slot index wins, the rest go to `OtherTextureIDs`.
   - Anything still unclassified (e.g. `CategoryUnknown`, `CategorySmallUtil`) also goes to `OtherTextureIDs`.
4. **Dedupe** materials by the `(ColorTextureID, NormalTextureID, MRTextureID)` tuple. For each unique tuple, aggregate:
   - `DrawCallCount` += 1 per draw
   - `TotalBytes` = sum of `Report.Textures[*].Bytes` for the three (or fewer) unique map IDs (counted once per material, not once per draw)
   - `MeshHashes` = union of `BuildMeshes`-derived mesh hashes for the draws using this material (linked via the existing per-draw mesh hash)

### Sort default

Descending by `TotalBytes`, so heavy materials surface first. Same convention as Textures and Meshes.

## UI: new "Materials" sub-tab (`internal/app/ui/tabs/renderdoc/materials_view.go`, new)

Mirrors [meshes_view.go](../../../internal/app/ui/tabs/renderdoc/meshes_view.go) structure exactly so it feels native.

### Top bar

- Path label (capture file)
- Summary: "X materials across Y draw calls, total Z MB"
- Filter entry: "Filter by texture ID or hash"

### Table (sortable, like the existing tabs)

Columns:

| Column        | Content                                             |
|---------------|-----------------------------------------------------|
| Color         | 32×32 thumbnail of decoded base mip (or "—")        |
| Normal        | 32×32 thumbnail (or "—")                            |
| MR            | 32×32 thumbnail (or "—")                            |
| Color Hash    | 16-char `PixelHash` from the existing texture row   |
| Draws         | `DrawCallCount`                                     |
| Meshes        | `len(MeshHashes)`                                   |
| VRAM          | `TotalBytes`, formatted via `internal/format`       |

Thumbnails decoded via existing `DecodeTexturePreview`. To keep table scrolling fast, decoded thumbnails are cached on the `materialsTabState` keyed by texture resource ID; populated once per material on first render.

### Preview pane (below the table)

Clicking a row shows:

- Three maps at 256×256 side-by-side, each labeled (Color / Normal / MR / Other-N).
- A multi-line read-only entry listing the mesh hashes that use this material — first 16 chars each, one per line.

## Sub-tab wiring (`renderdoc_tab.go`)

Add `materialsIndex = 2` to the existing `texturesIndex / meshesIndex` constants in [renderdoc_tab.go](../../../internal/app/ui/tabs/renderdoc/renderdoc_tab.go). Append a third sub-tab:

```go
container.NewTabItem("Materials", materialsView),
```

### Shared loader

Today the Textures and Meshes sub-tabs each parse the XML independently. With three sub-tabs all reading the same data, this becomes wasteful. Refactor:

```go
// in internal/renderdoc/load.go (new file)
type CaptureLoad struct {
    Report     *Report
    MeshReport *MeshReport
    Materials  []Material
    Store      *BufferStore
}

func LoadCapture(rdcPath string, onProgress func(stage string, done, total int)) (*CaptureLoad, error)
```

`LoadCapture` does: convert `.rdc` → XML, parse texture report, parse mesh report (single XML pass — both parsers can live in one walk later, but for v1 reuse the existing two `Parse*` functions sequentially), build buffer store, compute texture hashes, build meshes, build materials. The launcher invokes it once and dispatches the resulting `CaptureLoad` to all three sub-tabs.

This refactor also halves XML parse time on load — incidental win.

## Edge cases

- **Material with only Color, no PBR maps.** Show as-is — `NormalTextureID` and `MRTextureID` empty, thumbnails render as "—". Common on UI, billboards, decals.
- **No Color, only Normal/MR.** Rare but possible (debug shaders, depth-only passes that still bind textures). Surface as-is rather than dropping; helps catch pipeline weirdness.
- **Multiple plausible Color textures in one material.** Pick the lowest PS slot; remaining go to `OtherTextureIDs`. Visible in the preview pane so the user can investigate.
- **Capture without `PSSetShaderResources` chunks** (older RenderDoc, non-D3D11 capture). `BuildMaterials` returns an empty slice; the view shows "No material data in this capture."
- **Capture with PS bindings but no SRV-creation chunks parsed yet.** Same outcome — bindings can't resolve to textures, slice is empty. (Should not happen in practice; D3D11 always emits CreateShaderResourceView before bind.)

## Testing

- **`materials_test.go`** (new): hand-built `Report` + `MeshReport` covering:
  - pure-color material (no normal, no MR)
  - full PBR triple (color + normal + MR)
  - shared color across two materials with different normals (must produce two distinct materials)
  - scene-global texture excluded via the 80% threshold
  - scene-global texture excluded via category (`CategoryBuiltin`, `CategoryRenderTgt`)
  - multiple plausible Color textures → first goes to ColorTextureID, rest to `OtherTextureIDs`
- **`parse_test.go`** extension: small XML snippet containing `CreateShaderResourceView`, `PSSetShaderResources`, and `DrawIndexed`, asserting `DrawCall.PSTextureIDs` is populated correctly with resolved texture IDs (not SRV IDs).
- No UI tests, consistent with how Textures and Meshes views are tested today.

## Build sequence (rough)

1. Parser: `CreateShaderResourceView` + `PSSetShaderResources` + `DrawCall.PSTextureIDs`. Tests.
2. `materials.go` + `BuildMaterials` + tests.
3. `LoadCapture` shared loader; refactor Textures and Meshes sub-tabs to consume it (no behavior change).
4. `materials_view.go` + sub-tab wiring.
5. Manual smoke test on a real capture.
