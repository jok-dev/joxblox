# RenderDoc Meshes View

## Goal

Add a "Meshes" view to the existing RenderDoc tab. Answers the same question the Textures view does, but for meshes: what vertex/index buffers are on the GPU, which are duplicated, and what does a given mesh actually look like?

## UX

The current RenderDoc tab becomes a container with two sub-tabs: **Textures** (existing UI, unchanged) and **Meshes** (new). One `.rdc` load feeds both.

Meshes sub-tab layout mirrors Textures:
- Table of unique meshes, sortable/filterable.
- Columns: `ID`, `Verts`, `Tris`, `VB bytes`, `IB bytes`, `Draws` (how many `DrawIndexed` calls used this pair), `Layout` (short format string), `Hash`.
- Click a row → 3D preview on the right using the existing `ui.MeshPreviewWidget`.

## Dedup rule

A "mesh" = the set of vertex buffers bound at a `DrawIndexed` call + the index buffer bound at that call. Dedup by content hash (SHA-256 of concatenated VB slot bytes + IB bytes, truncated to 16 hex). The same geometry uploaded 50× shows as one row with `Draws=50`.

## Parse additions

New `MeshReport` alongside `Report`, built from:
- `CreateBuffer` (size, bind flags, usage, resource ID)
- `CreateInputLayout` (element → slot + byte offset + format)
- `IASetVertexBuffers` / `IASetIndexBuffer` / `IASetInputLayout` (binding state)
- `DrawIndexed` + `DrawIndexedInstanced` (pair VBs + IB by replaying binding state at each draw)

## Preview decode

Preview scope for v1: positions as `R32G32B32_FLOAT` only. Any other position format → preview pane shows "format not supported yet"; row still renders with stats. Indices handled for both `R16_UINT` and `R32_UINT` (trivial).

## New files

- `internal/renderdoc/meshes.go` + `meshes_test.go` — parse + dedup
- `internal/renderdoc/decode_position.go` + test — position-stream extraction
- `internal/app/ui/tabs/renderdoc/meshes_view.go` — the Meshes sub-tab
- `internal/app/ui/tabs/renderdoc/renderdoc_tab.go` (modified) — wrap existing content in sub-tab container

## Out of scope (v1)

- Non-`R32G32B32_FLOAT` position formats
- Per-draw-call analysis view (shader pair, constant buffer contents, etc.)
- Roblox built-in mesh detection via hash allowlist
- Correlating mesh to rbxl MeshPart asset ID
