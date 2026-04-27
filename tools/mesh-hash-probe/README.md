# mesh-hash-probe

Feasibility spike for the **mesh** side of asset-ID mapping. The texture
side ships in the same branch; whether the mesh side is worth doing
depends on whether GPU-side captured mesh bytes can be matched back to
source `.mesh` asset bytes (the engine may reformat positions, batch
meshes together, or otherwise transform vertex data before upload).

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
pair, write findings to
`docs/superpowers/findings/2026-MM-DD-mesh-hash-feasibility.md`:

- Most cases EXACT byte match → mesh mapping is straightforward; write
  a follow-up spec for full mesh asset-ID mapping (mirror of textures).
- Most cases SORTED-position match → mapping is feasible but needs
  position-set hashing instead of byte hashing; document the approach
  in the follow-up spec.
- Mostly count-match or no match → mapping is not feasible without
  reverse-engineering the engine's mesh upload format. Document why
  and shelve.

## Implementation notes for the executing engineer

- Live as its own module like `tools/mesh-renderer` so it can `go build`
  independently. Don't add `tools/mesh-hash-probe` to the joxblox
  `go.mod`.
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
