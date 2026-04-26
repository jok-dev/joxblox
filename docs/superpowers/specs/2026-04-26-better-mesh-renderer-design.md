# Better Mesh Renderer — Design

**Date:** 2026-04-26
**Status:** Approved, ready for plan

## Goal

Make the mesh preview actually read as 3D geometry instead of a flat silhouette. Wire up the existing-but-dead shading infrastructure (custom Phong shader, shadow-map framework, depth pass) in [tools/mesh-renderer/main.go](../../../tools/mesh-renderer/main.go), add a ground grid for spatial reference, and let the user switch between three viewmodes for different inspection needs.

## Non-goals

- Shadow map filtering tuning beyond what's already in the existing PCF kernel.
- Ambient occlusion / SSAO.
- HDR / tonemapping.
- Matcap mode (considered, dropped — least useful for asset inspection of the four candidates).
- Upgrading the orthographic render path (`handleRenderOrtho`); only the perspective `handleRender` gets the changes in this pass.

## Background: what's already wired vs not

In [tools/mesh-renderer/main.go](../../../tools/mesh-renderer/main.go) the following all exist but never run on a draw:

- Custom GLSL vertex+fragment shader implementing key + fill + rim Phong with PCF shadow sampling. Loaded into `mainShader` at startup; never bound to any model material.
- Depth-pass shader (`depthShader`). Loaded; never used.
- 2048×2048 shadow render texture (`shadowMap`). Allocated; never rendered into.
- `computeVertexNormals` — already runs on every load, normals are uploaded per vertex.

`createModelFromRawData` calls `LoadModelFromMesh` which gives the model Raylib's default flat shader. That's why everything looks unlit. The fix is making the existing code paths actually run, not writing new shaders.

## Renderer subprocess changes (`tools/mesh-renderer/main.go`)

### Bind the main shader to every loaded model

In `createModelFromRawData`, after `model = rl.LoadModelFromMesh(mesh)`:

```go
rl.SetMaterialShader(&model.Materials[0], mainShader)
```

Now `rl.DrawModel` uses the Phong shader instead of the default.

### Add a shadow pass before the main pass in `handleRender`

Before the existing `BeginTextureMode(renderTarget)` block:

1. Compute `lightVP := buildLightVP(keyLightDir)` once.
2. `BeginTextureMode(shadowMap)` → `ClearBackground(white)` → `BeginMode3D` with a camera positioned along `keyLightDir` looking at origin.
3. Swap each loaded model's material shader to `depthShader`, draw all models with `DrawModel`, restore the original (`mainShader`) after.
4. `EndMode3D` / `EndTextureMode`.

Then in the main pass, before the per-batch draw loop:

```go
rl.SetShaderValue(mainShader, lightDirLoc, keyLightDirArr, rl.ShaderUniformVec3)
rl.SetShaderValue(mainShader, fillLightDirLoc, fillLightDirArr, rl.ShaderUniformVec3)
rl.SetShaderValue(mainShader, viewPosLoc, viewPosArr, rl.ShaderUniformVec3)
rl.SetShaderValue(mainShader, ambientLoc, ambientColArr, rl.ShaderUniformVec3)
rl.SetShaderValue(mainShader, lightColLoc, lightColArr, rl.ShaderUniformVec3)
rl.SetShaderValueMatrix(mainShader, lightVPLoc, lightVP)
rl.SetShaderValue(mainShader, shadowBiasLoc, []float32{0.0015}, rl.ShaderUniformFloat)
rl.SetShaderValueTexture(mainShader, shadowMapLoc, shadowMap.Texture)
```

`baseCol` is set per-model just before each `DrawModel`, since it differs by viewmode (see Viewmodes below).

### Lighting layout (hybrid)

- `keyLightDir` — fixed world direction `normalize(-0.4, 0.8, 0.5)` (upper-front-left). Casts the shadow.
- `fillLightDir` — recomputed each frame from the camera. Take the camera's forward vector, lift its Y component slightly so the fill comes from over-the-shoulder rather than straight ahead, normalize. Provides "headlamp" coverage so faces pointed at the viewer always read.
- Intensities use the existing shader weights (`fill * 0.35`, `rim * 0.15`, `spec * 0.3`).

### Fragment shader: multiply by vertex color

Current shader has `finalColor = vec4(lit * baseCol, 1.0)`. Add the `vertexColor` attribute so vertex-color mode can ride the same shader:

```glsl
in vec4 vertexColor; // (vertex shader pass-through)
// ...
finalColor = vec4(lit * baseCol * vertexColor.rgb, 1.0);
```

The vertex shader gets a matching `in vec4 vertexColor; out vec4 fragColor;` pass-through. Lit-clay mode sets `baseCol = clay_gray` and ignores vertex colors by uploading white per-vertex; vertex-color mode sets `baseCol = (1,1,1)` so the per-vertex value carries.

### Add the viewmode branch

Add a `uniform int viewmode;` to the fragment shader. Switch on it:

```glsl
if (viewmode == 2) {
    // Normals mode: skip lighting entirely.
    finalColor = vec4(norm * 0.5 + 0.5, 1.0);
    return;
}
// ...existing Phong path uses lit * baseCol * vertexColor.rgb
```

`viewmode == 0` → vertex color (existing behavior, lit). `viewmode == 1` → lit clay (lit, baseCol overridden). `viewmode == 2` → normals (no lighting).

### Ground grid

After the existing `EndShaderMode` (or before `EndMode3D` if no explicit `EndShaderMode` is called), draw the grid:

```go
rl.DrawGrid(20, 1.0)
```

Position offset: temporarily push the matrix down to `y = sceneBoundingBox.Min.Y` so the grid sits flush under the lowest point of the visible geometry rather than at the origin. This needs `rl.PushMatrix` / `rl.Translatef` / `rl.PopMatrix` around the `DrawGrid` call. Always on (no toggle); the lines are subtle and informative.

### Protocol extension

The `RENDER` command currently takes 12 args (`width height cam_x cam_y cam_z selected_batch yaw pitch zoom opacity bg_hex` plus optional `wireframe`). Add a 13th arg: `viewmode` int. The parser already tolerates a missing `wireframe`; do the same for viewmode (default `0` = vertex color, today's behavior). This keeps an old-Go / new-subprocess pairing working without crashes.

## Go-side changes (`internal/app/ui/`)

### `mesh_preview.go`

- New constants:
  ```go
  const (
      MeshViewmodeVertexColor = 0
      MeshViewmodeLitClay     = 1
      MeshViewmodeNormals     = 2
  )
  ```
- New field on `MeshPreviewWidget`: `viewmode int`.
- New method `SetViewmode(mode int)` → updates field and calls `render()`.
- `applyData` picks the default for fresh data: `MeshViewmodeVertexColor` when `len(data.Batches) > 0`, else `MeshViewmodeLitClay`.
- `render()` snapshots `viewmode` alongside the other state values and passes it through to the subprocess.

### `meshRendererProcess.render`

Append the viewmode int to the `RENDER` command line:

```go
fmt.Sprintf("RENDER %d %d %f %f %f %d %f %f %f %f %s %d %d\n",
    width, height, camX, camY, camZ, selectedBatch, yaw, pitch, zoom, opacity, bgHex,
    wireframeFlag, viewmode)
```

Update the function signature to accept `viewmode int`.

### Viewmode toolbar

`MeshPreviewWidget` is embedded in five consumers:

- [internal/app/ui/asset_view.go](../../../internal/app/ui/asset_view.go) and [asset_view_preview.go](../../../internal/app/ui/asset_view_preview.go) — single-asset preview
- [internal/app/ui/tabs/heatmap/model_heatmap_tab.go](../../../internal/app/ui/tabs/heatmap/model_heatmap_tab.go)
- [internal/app/ui/tabs/lodviewer/lod_viewer_tab.go](../../../internal/app/ui/tabs/lodviewer/lod_viewer_tab.go)
- [internal/app/ui/tabs/renderdoc/meshes_view.go](../../../internal/app/ui/tabs/renderdoc/meshes_view.go)

To avoid wiring the toolbar in five places, expose one helper in [mesh_preview.go](../../../internal/app/ui/mesh_preview.go):

```go
// NewMeshPreviewWithToolbar wraps a MeshPreviewWidget in a vertical
// container with a viewmode segmented control above it. Returns both
// the container (for layout) and the inner widget (for SetData / etc.).
func NewMeshPreviewWithToolbar() (fyne.CanvasObject, *MeshPreviewWidget)
```

The segmented control reuses the `channelModeButton` styling from [internal/app/ui/tabs/renderdoc/renderdoc_tab.go](../../../internal/app/ui/tabs/renderdoc/renderdoc_tab.go) — three buttons labeled `Lit` · `Color` · `Normals`. Tapping a button calls `MeshPreviewWidget.SetViewmode(...)`.

Each existing call site swaps `NewMeshPreviewWidget()` for `NewMeshPreviewWithToolbar()` and adjusts its layout to use the returned container. Functionally tiny per call site, mechanically consistent.

The Materials sub-tab does not currently embed a 3D mesh preview (it shows the texture maps), so no change there.

## Edge cases

- **Vertex color mode + single mesh with no per-vertex colors.** `buildDefaultColors` returns white, so `lit * baseCol * white = lit * baseCol`. Looks similar to lit-clay but with `baseCol = (1,1,1)` instead of clay_gray — slightly brighter. Acceptable; the user explicitly picked vertex-color mode.
- **Normals mode + mesh with bad or missing normals.** Will show banded/flat colors. That's the diagnostic value of the mode — surfaces broken normals immediately.
- **Shadow pass on transparent geometry (`opacity < 1`).** Skip the shadow pass entirely in that branch (just clear the shadow map to white = "no shadow"). Depth test is already disabled in the transparent path; shadows on alpha-blended geometry would look wrong.
- **Very large scenes.** The shadow pass loops over every model. Cost = `len(loadedScene)` extra `DrawModel` calls per frame. For thousand-batch scenes this could add up. Not solving today; if it becomes a problem, downscale the shadow map (1024² or 512²) or merge the shadow-pass meshes.
- **Empty scene.** `handleRender` already returns early. Leave that path alone.

## Testing

Mostly manual visual verification against a real mesh:
1. Load a single-mesh capture (e.g. one of the meshes from a batcave .rdc). Default viewmode = Lit Clay. Confirm shape reads, shadows ground the mesh, fill light keeps front-facing geometry visible as the camera orbits.
2. Switch to Vertex Color. Confirm per-batch coloring works in a multi-batch scene (load a Roblox scene capture).
3. Switch to Normals. Confirm RGB color matches expected face directions: top-facing → green-dominant, side-facing → red/blue dominant.
4. Toggle wireframe in each mode — should compose with all three.

One automated test worth writing: in the subprocess, parse a RENDER line with and without the viewmode arg; assert the parser tolerates the missing arg and defaults to vertex-color mode (back-compat).

## Build sequence (rough)

1. Subprocess: bind main shader to model materials, run shadow pass, set per-frame uniforms. Verify Lit Clay default looks right with no other changes.
2. Subprocess: add viewmode uniform + fragment-shader branch. Wire the protocol arg through. Verify Normals mode renders.
3. Subprocess: ground grid.
4. Subprocess: hybrid fill light (camera-relative).
5. Go side: viewmode field, `SetViewmode`, default selection by data type, RENDER arg threading.
6. Go side: segmented control widget on Meshes sub-tab and asset_view mesh previews.
7. Manual smoke test on real captures.
