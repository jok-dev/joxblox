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
