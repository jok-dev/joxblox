# Ideas

Running list of features we'd like to explore. Not a roadmap — just a parking lot.

## RenderDoc

- **Pair captured textures into PBR materials.** Inspect which textures bind together on the same draw call, then group them into material sets (color + normal + metallic/roughness). Show as a single material card in the Textures sub-tab.
- **Map captured meshes/textures back to Studio asset IDs by hashing.** Hash extracted mesh VB+IB / texture pixels and look the result up against the asset cache (or a known-asset database) so a captured draw call can be traced back to a workspace instance and an asset ID.

## Mesh / 3D

- **Mesh optimization.** Find duplicate meshes across a place, suggest LOD generation (e.g. via meshoptimizer), flag meshes that are higher-poly than they need to be at typical view distance.
- **Better mesh renderer.** Current preview is flat and hard to read — add basic shading (directional light + ambient), maybe a ground plane / grid, orbit controls that feel right, and a wireframe toggle. Goal: you can actually see the geometry, not just a silhouette.

## Reporting & Collaboration

- **Better / polished report generation.** Take the existing report tab further — exportable HTML or PDF with charts, asset thumbnails, before/after for optimizations, ready to hand to a producer or post in a Slack thread.
- **Asset tagging with portable bundles.** Let users tag assets (e.g. `reviewed`, `approved`, `needs-fix`, free-form notes) and persist that state. Then export a compressed `.jox` bundle containing the asset references, tags, notes, and maybe cached previews so a teammate can open it, action items, and send a bundle back.
  - Stretch: optional server-side sync for real-time collaboration (probably overkill for v1, but worth keeping in mind so the bundle format doesn't paint us into a corner).
