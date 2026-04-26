# Better Mesh Renderer Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Wire up the existing-but-unbound Phong + shadow-map shader infrastructure in the mesh renderer subprocess, add a ground grid, hybrid (fixed-key + camera-relative-fill) lighting, and three user-selectable viewmodes (Lit Clay, Vertex Color, Normals) exposed via a small toolbar above every mesh preview in the app.

**Architecture:** The subprocess (`tools/mesh-renderer/main.go`) loads custom GLSL shaders at startup but never binds them to any model — Raylib falls back to its flat default shader. We bind `mainShader` to every loaded model, run a shadow-map pass before the main pass, set per-frame uniforms (lights, view position, shadow matrix), and add a `viewmode` uniform + fragment-shader branch for the Normals debug view. The Go widget gets a `viewmode` field, a `SetViewmode` method, and a new `NewMeshPreviewWithToolbar` helper so all five consumers pick up the segmented Lit/Color/Normals toggle without per-call-site UI rewrites.

**Tech Stack:** Go 1.23+, Raylib (gen2brain/raylib-go) GLSL 330 shaders, Fyne v2 widgets.

**Spec:** [docs/superpowers/specs/2026-04-26-better-mesh-renderer-design.md](../specs/2026-04-26-better-mesh-renderer-design.md)

---

## File Structure

**Modify:**
- `tools/mesh-renderer/main.go` — bind shader, run shadow pass, set uniforms, add viewmode branch + protocol arg, ground grid, hybrid fill light, vertex shader passes vertex color
- `internal/app/ui/mesh_preview.go` — viewmode field + setter, default by data type, thread viewmode through `RENDER`, add `NewMeshPreviewWithToolbar`
- `internal/app/ui/asset_view_preview.go` — switch to `NewMeshPreviewWithToolbar`
- `internal/app/ui/asset_view.go` — switch to `NewMeshPreviewWithToolbar` (if directly constructing the widget)
- `internal/app/ui/tabs/heatmap/model_heatmap_tab.go` — switch to `NewMeshPreviewWithToolbar`
- `internal/app/ui/tabs/lodviewer/lod_viewer_tab.go` — switch to `NewMeshPreviewWithToolbar`
- `internal/app/ui/tabs/renderdoc/meshes_view.go` — switch to `NewMeshPreviewWithToolbar`

**Test:**
- `tools/mesh-renderer/render_args_test.go` (new) — parser back-compat for missing viewmode arg

---

## Task 1: Bind mainShader to every loaded model

**Files:**
- Modify: `tools/mesh-renderer/main.go`

**Why:** Today the model uses Raylib's default shader. Binding our existing Phong shader is the foundational step that makes every other lighting change visible.

- [ ] **Step 1: Bind the shader after `LoadModelFromMesh`**

In `createModelFromRawData`, immediately after `modelData.model = rl.LoadModelFromMesh(mesh)`, add:

```go
// Override the default unlit material shader with our Phong shader so
// DrawModel actually runs the lighting code we already loaded.
materials := modelData.model.GetMaterials()
if len(materials) > 0 {
	materials[0].Shader = mainShader
}
```

- [ ] **Step 2: Set per-frame Phong uniforms in `handleRender` before drawing**

Inside `handleRender`, after `rl.BeginMode3D(camera)` and BEFORE the `for _, batchIndex := range drawOrder` loop, add the uniform setup. Define the helper values right before:

```go
keyLightDir := rl.Vector3Normalize(rl.NewVector3(-0.4, 0.8, 0.5))
fillLightDirVec := rl.Vector3Normalize(rl.NewVector3(
	float32(camera.Target.X-camera.Position.X),
	float32(camera.Target.Y-camera.Position.Y)+0.4,
	float32(camera.Target.Z-camera.Position.Z),
))
ambient := []float32{0.18, 0.19, 0.22}
lightCol := []float32{1.0, 0.97, 0.92}
viewPos := []float32{float32(cameraX), float32(cameraY), float32(cameraZ)}

rl.SetShaderValue(mainShader, lightDirLoc, []float32{keyLightDir.X, keyLightDir.Y, keyLightDir.Z}, rl.ShaderUniformVec3)
rl.SetShaderValue(mainShader, fillLightDirLoc, []float32{fillLightDirVec.X, fillLightDirVec.Y, fillLightDirVec.Z}, rl.ShaderUniformVec3)
rl.SetShaderValue(mainShader, viewPosLoc, viewPos, rl.ShaderUniformVec3)
rl.SetShaderValue(mainShader, ambientLoc, ambient, rl.ShaderUniformVec3)
rl.SetShaderValue(mainShader, lightColLoc, lightCol, rl.ShaderUniformVec3)
```

- [ ] **Step 3: Set per-model `baseCol` uniform inside the draw loop**

Inside the `for _, batchIndex := range drawOrder` loop, BEFORE the `if wireframe { ... } else { rl.DrawModel(...) }` block, push `baseCol` for this model:

```go
baseCol := meshModel.baseColor
rl.SetShaderValue(mainShader, baseColLoc, []float32{baseCol[0], baseCol[1], baseCol[2]}, rl.ShaderUniformVec3)
```

- [ ] **Step 4: Provide neutral defaults for shadow-map uniforms (so shader doesn't sample garbage)**

Still inside `handleRender`, alongside the other uniform sets, add:

```go
// Shadows wired in Task 2; until then, bind an identity light matrix and
// a max-distance bias so the calcShadow() helper never reports occlusion.
identity := rl.MatrixIdentity()
rl.SetShaderValueMatrix(mainShader, lightVPLoc, identity)
rl.SetShaderValue(mainShader, shadowBiasLoc, []float32{1.0}, rl.ShaderUniformFloat)
```

- [ ] **Step 5: Patch the fragment shader so vertex color is multiplied in**

Replace the existing `mainFragSrc` with:

```go
const mainFragSrc = `
#version 330
in vec3 fragPosition;
in vec3 fragNormal;
in vec4 fragColor;
in vec4 fragPosLightSpace;
uniform vec3 lightDir;
uniform vec3 fillLightDir;
uniform vec3 viewPos;
uniform vec3 ambientCol;
uniform vec3 lightCol;
uniform vec3 baseCol;
uniform sampler2D shadowMap;
uniform float shadowBias;
out vec4 finalColor;

float calcShadow(vec4 posLS) {
    vec3 proj = posLS.xyz / posLS.w;
    proj = proj * 0.5 + 0.5;
    if (proj.x < 0.0 || proj.x > 1.0 || proj.y < 0.0 || proj.y > 1.0 || proj.z > 1.0)
        return 0.0;
    float currentDepth = proj.z;
    float shadow = 0.0;
    vec2 texelSize = 1.0 / textureSize(shadowMap, 0);
    for (int x = -1; x <= 1; x++) {
        for (int y = -1; y <= 1; y++) {
            float closestDepth = texture(shadowMap, proj.xy + vec2(x, y) * texelSize).r;
            shadow += (currentDepth - shadowBias > closestDepth) ? 1.0 : 0.0;
        }
    }
    return shadow / 9.0;
}

void main() {
    vec3 norm = normalize(fragNormal);
    vec3 viewDir = normalize(viewPos - fragPosition);
    float diff = max(dot(norm, lightDir), 0.0);
    vec3 halfDir = normalize(lightDir + viewDir);
    float spec = pow(max(dot(norm, halfDir), 0.0), 32.0);
    vec3 key = diff * lightCol + spec * lightCol * 0.3;
    float fillDiff = max(dot(norm, fillLightDir), 0.0);
    vec3 fill = fillDiff * lightCol * 0.35;
    float rim = 1.0 - max(dot(norm, viewDir), 0.0);
    rim = pow(rim, 3.0) * 0.15;
    float shadow = calcShadow(fragPosLightSpace);
    vec3 lit = ambientCol + key * (1.0 - shadow * 0.65) + fill + rim;
    finalColor = vec4(lit * baseCol * fragColor.rgb, 1.0);
}
`
```

- [ ] **Step 6: Patch the vertex shader to pass the vertex color through**

Replace `mainVertSrc` with:

```go
const mainVertSrc = `
#version 330
in vec3 vertexPosition;
in vec3 vertexNormal;
in vec4 vertexColor;
uniform mat4 mvp;
uniform mat4 matModel;
uniform mat3 matNormal;
uniform mat4 lightVP;
out vec3 fragPosition;
out vec3 fragNormal;
out vec4 fragColor;
out vec4 fragPosLightSpace;
void main() {
    vec4 worldPos = matModel * vec4(vertexPosition, 1.0);
    fragPosition = worldPos.xyz;
    fragNormal = normalize(matNormal * vertexNormal);
    fragColor = vertexColor;
    fragPosLightSpace = lightVP * worldPos;
    gl_Position = mvp * vec4(vertexPosition, 1.0);
}
`
```

- [ ] **Step 7: Build the subprocess**

Run: `cd tools/mesh-renderer && go build -o joxblox-mesh-renderer.exe .`
Expected: build succeeds, no errors.

- [ ] **Step 8: Manually smoke test**

Run from the repo root: `go run ./cmd/joxblox`
Open the RenderDoc tab, switch to the Meshes sub-tab, load a `.rdc` (e.g. the batcave one in Downloads), select a mesh row.
Expected: the preview now shows shaded geometry — visible light/dark surfaces depending on face orientation — rather than a flat silhouette. (Shadows not yet visible; that's Task 2.)

- [ ] **Step 9: Commit**

```bash
git add tools/mesh-renderer/main.go tools/mesh-renderer/joxblox-mesh-renderer.exe
git commit -m "feat(mesh-renderer): bind Phong shader to model materials"
```

---

## Task 2: Run the shadow pass

**Files:**
- Modify: `tools/mesh-renderer/main.go`

**Why:** With shaders wired, we can now populate the shadow map and sample it. Adds contact shadows that ground the model.

- [ ] **Step 1: Add a shadow-pass helper**

Add a new function at file scope, near `handleRender`:

```go
// renderShadowPass populates the global shadowMap with depth values from
// keyLightDir's perspective. Callers must invoke this before the main pass
// and pass the same lightVP into the main shader's uniform.
func renderShadowPass(keyLightDir rl.Vector3) rl.Matrix {
	lightVP := buildLightVP(keyLightDir)

	// Build a camera that matches lightVP so BeginMode3D produces the same
	// transform when Raylib applies its own MVP.
	lightCamera := rl.Camera3D{
		Position:   rl.Vector3{X: keyLightDir.X * 4, Y: keyLightDir.Y * 4, Z: keyLightDir.Z * 4},
		Target:     rl.Vector3{},
		Up:         rl.Vector3{X: 0, Y: 1, Z: 0},
		Fovy:       4.0, // matches MatrixOrtho extents above; small fovy is unused for ortho
		Projection: rl.CameraOrthographic,
	}

	rl.BeginTextureMode(shadowMap)
	rl.ClearBackground(rl.Color{R: 255, G: 255, B: 255, A: 255})
	rl.BeginMode3D(lightCamera)

	// Swap each model's shader to the depth shader, draw, then restore.
	for _, meshModel := range loadedScene {
		mats := meshModel.model.GetMaterials()
		if len(mats) == 0 {
			continue
		}
		original := mats[0].Shader
		mats[0].Shader = depthShader
		rl.DrawModel(meshModel.model, rl.Vector3{}, 1.0, rl.White)
		mats[0].Shader = original
	}

	rl.EndMode3D()
	rl.EndTextureMode()

	return lightVP
}
```

- [ ] **Step 2: Call the shadow pass from `handleRender`**

In `handleRender`, BEFORE `rl.BeginTextureMode(renderTarget)`, add:

```go
keyLightDir := rl.Vector3Normalize(rl.NewVector3(-0.4, 0.8, 0.5))
var lightVP rl.Matrix
if !transparent {
	lightVP = renderShadowPass(keyLightDir)
} else {
	// Skip shadows for transparent geometry — depth-test is already off
	// in this branch and self-shadowing alpha looks wrong. Pass an
	// identity matrix + max bias so calcShadow() returns 0.
	lightVP = rl.MatrixIdentity()
}
```

Note: `transparent` is currently declared inside the `BeginTextureMode(renderTarget)` block. Move that declaration out so we can branch on it before the shadow pass:

```go
// Find the existing line:
//   transparent := opacity < 0.999
// Move it to here, before the shadow-pass call.
```

- [ ] **Step 3: Replace the placeholder uniforms with real shadow values**

Inside `handleRender`, find the Step 4 placeholder from Task 1:

```go
identity := rl.MatrixIdentity()
rl.SetShaderValueMatrix(mainShader, lightVPLoc, identity)
rl.SetShaderValue(mainShader, shadowBiasLoc, []float32{1.0}, rl.ShaderUniformFloat)
```

Replace with:

```go
rl.SetShaderValueMatrix(mainShader, lightVPLoc, lightVP)
shadowBiasValue := float32(0.0015)
if transparent {
	shadowBiasValue = 1.0 // disables shadows in calcShadow()
}
rl.SetShaderValue(mainShader, shadowBiasLoc, []float32{shadowBiasValue}, rl.ShaderUniformFloat)
rl.SetShaderValueTexture(mainShader, shadowMapLoc, shadowMap.Texture)
```

- [ ] **Step 4: Re-derive `keyLightDir` for the uniform set**

The `keyLightDir` computed in Step 2 is the same value used in the existing Task 1 uniform set. Verify both sites reference the same variable. If the Task 1 code computes `keyLightDir` inside the same `handleRender` function and the new Step 2 code also computes it before, deduplicate so there's exactly one declaration.

After this step, the uniform set should look like (single source of truth):

```go
// (Already declared earlier as part of the shadow pass call.)
// keyLightDir was computed before renderShadowPass; reuse it.

fillLightDirVec := rl.Vector3Normalize(rl.NewVector3(
	float32(camera.Target.X-camera.Position.X),
	float32(camera.Target.Y-camera.Position.Y)+0.4,
	float32(camera.Target.Z-camera.Position.Z),
))
// ... rest of uniform set as before
rl.SetShaderValue(mainShader, lightDirLoc, []float32{keyLightDir.X, keyLightDir.Y, keyLightDir.Z}, rl.ShaderUniformVec3)
```

- [ ] **Step 5: Build the subprocess**

Run: `cd tools/mesh-renderer && go build -o joxblox-mesh-renderer.exe .`
Expected: build succeeds.

- [ ] **Step 6: Manually smoke test**

Run: `go run ./cmd/joxblox`, load a mesh.
Expected: contact shadows visible under the mesh — surfaces facing away from the upper-left key light should be darker, and self-occlusion (e.g. an arm shadow falling on a torso) should be visible.

- [ ] **Step 7: Commit**

```bash
git add tools/mesh-renderer/main.go tools/mesh-renderer/joxblox-mesh-renderer.exe
git commit -m "feat(mesh-renderer): run shadow pass + sample shadow map in main shader"
```

---

## Task 3: Add viewmode protocol arg + Normals fragment branch

**Files:**
- Modify: `tools/mesh-renderer/main.go`
- Test: `tools/mesh-renderer/render_args_test.go` (new)

**Why:** Lets the user pick between Lit Clay (default for single mesh), Vertex Color (default for scene), and Normals (debug view).

- [ ] **Step 1: Write the failing parser back-compat test**

Create `tools/mesh-renderer/render_args_test.go`:

```go
package main

import (
	"strings"
	"testing"
)

// parseRenderArgs is the pure-function extraction of the arg parsing in
// handleRender — created in Step 3 below so it can be tested in isolation.
func TestParseRenderArgsDefaultsViewmodeWhenMissing(t *testing.T) {
	// 12-arg form: width height cam_x cam_y cam_z selected_batch yaw pitch zoom opacity bg_hex
	parts := strings.Fields("RENDER 800 600 0 0 5 -1 0 0 1 1 222222")
	args, err := parseRenderArgs(parts)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if args.Viewmode != ViewmodeVertexColor {
		t.Errorf("default viewmode: got %d, want %d (vertex color)", args.Viewmode, ViewmodeVertexColor)
	}
	if args.Wireframe {
		t.Errorf("wireframe should default to false")
	}
}

func TestParseRenderArgsAcceptsViewmodeArg(t *testing.T) {
	// 13-arg form with viewmode = 2 (Normals).
	parts := strings.Fields("RENDER 800 600 0 0 5 -1 0 0 1 1 222222 0 2")
	args, err := parseRenderArgs(parts)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if args.Viewmode != ViewmodeNormals {
		t.Errorf("viewmode: got %d, want %d (Normals)", args.Viewmode, ViewmodeNormals)
	}
}

func TestParseRenderArgsClampsUnknownViewmode(t *testing.T) {
	parts := strings.Fields("RENDER 800 600 0 0 5 -1 0 0 1 1 222222 0 99")
	args, _ := parseRenderArgs(parts)
	if args.Viewmode != ViewmodeVertexColor {
		t.Errorf("unknown viewmode should fall back to vertex color, got %d", args.Viewmode)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd tools/mesh-renderer && go test -run TestParseRenderArgs -v`
Expected: FAIL — `parseRenderArgs` and viewmode constants don't exist.

- [ ] **Step 3: Extract `parseRenderArgs` from `handleRender` and add viewmode constants**

In `tools/mesh-renderer/main.go`, add at file scope:

```go
const (
	ViewmodeVertexColor = 0
	ViewmodeLitClay     = 1
	ViewmodeNormals     = 2
)

type renderArgs struct {
	Width         int
	Height        int
	CameraX       float64
	CameraY       float64
	CameraZ       float64
	SelectedBatch int
	Yaw           float64
	Pitch         float64
	Zoom          float64
	Opacity       float64
	BgHex         string
	Wireframe     bool
	Viewmode      int
}

func parseRenderArgs(parts []string) (renderArgs, error) {
	if len(parts) < 12 {
		return renderArgs{}, fmt.Errorf("RENDER requires at least 11 args after the command")
	}
	args := renderArgs{
		BgHex:    parts[11],
		Viewmode: ViewmodeVertexColor,
	}
	args.Width, _ = strconv.Atoi(parts[1])
	args.Height, _ = strconv.Atoi(parts[2])
	args.CameraX, _ = strconv.ParseFloat(parts[3], 64)
	args.CameraY, _ = strconv.ParseFloat(parts[4], 64)
	args.CameraZ, _ = strconv.ParseFloat(parts[5], 64)
	args.SelectedBatch, _ = strconv.Atoi(parts[6])
	args.Yaw, _ = strconv.ParseFloat(parts[7], 64)
	args.Pitch, _ = strconv.ParseFloat(parts[8], 64)
	args.Zoom, _ = strconv.ParseFloat(parts[9], 64)
	args.Opacity, _ = strconv.ParseFloat(parts[10], 64)
	if len(parts) >= 13 {
		flag, _ := strconv.Atoi(parts[12])
		args.Wireframe = flag != 0
	}
	if len(parts) >= 14 {
		mode, _ := strconv.Atoi(parts[13])
		switch mode {
		case ViewmodeVertexColor, ViewmodeLitClay, ViewmodeNormals:
			args.Viewmode = mode
		default:
			args.Viewmode = ViewmodeVertexColor
		}
	}
	return args, nil
}
```

Update `handleRender` to call it instead of doing inline parsing. Replace the existing block at the top of `handleRender`:

```go
// Old:
//   width, _ := strconv.Atoi(parts[1])
//   height, _ := strconv.Atoi(parts[2])
//   ... etc through wireframe parsing

// New:
args, err := parseRenderArgs(parts)
if err != nil {
	respond("ERR " + err.Error())
	return
}
width := args.Width
height := args.Height
cameraX := args.CameraX
cameraY := args.CameraY
cameraZ := args.CameraZ
selectedBatch := args.SelectedBatch
yaw := args.Yaw
pitch := args.Pitch
zoom := args.Zoom
opacity := args.Opacity
bgHex := args.BgHex
wireframe := args.Wireframe
viewmode := args.Viewmode
```

(Keep the existing clamping of width/height/zoom/opacity below this block.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd tools/mesh-renderer && go test -run TestParseRenderArgs -v`
Expected: PASS for all three.

- [ ] **Step 5: Add `viewmode` uniform to the fragment shader**

Update `mainFragSrc`. Add the uniform declaration near the others:

```glsl
uniform int viewmode;
```

Replace `void main()` body with:

```glsl
void main() {
    vec3 norm = normalize(fragNormal);
    if (viewmode == 2) {
        // Normals debug view: skip lighting entirely.
        finalColor = vec4(norm * 0.5 + 0.5, 1.0);
        return;
    }
    vec3 viewDir = normalize(viewPos - fragPosition);
    float diff = max(dot(norm, lightDir), 0.0);
    vec3 halfDir = normalize(lightDir + viewDir);
    float spec = pow(max(dot(norm, halfDir), 0.0), 32.0);
    vec3 key = diff * lightCol + spec * lightCol * 0.3;
    float fillDiff = max(dot(norm, fillLightDir), 0.0);
    vec3 fill = fillDiff * lightCol * 0.35;
    float rim = 1.0 - max(dot(norm, viewDir), 0.0);
    rim = pow(rim, 3.0) * 0.15;
    float shadow = calcShadow(fragPosLightSpace);
    vec3 lit = ambientCol + key * (1.0 - shadow * 0.65) + fill + rim;
    vec3 vertCol = (viewmode == 1) ? vec3(1.0) : fragColor.rgb;
    finalColor = vec4(lit * baseCol * vertCol, 1.0);
}
```

(Lit Clay sets `viewmode == 1`, which forces vertex color to white so the gray clay `baseCol` reads cleanly. Vertex Color leaves `viewmode == 0`, using the per-vertex color as a tint.)

- [ ] **Step 6: Add `viewmodeLoc` shader location and look it up at startup**

In the var block at the top of the file (where `lightDirLoc`, etc. are declared), add:

```go
viewmodeLoc int32
```

In `main()`, after the other `GetShaderLocation` calls, add:

```go
viewmodeLoc = rl.GetShaderLocation(mainShader, "viewmode")
```

- [ ] **Step 7: Set viewmode + per-mode `baseCol` in `handleRender`**

In `handleRender`, inside the per-batch draw loop, REPLACE the per-model `baseCol` push from Task 1:

```go
// Old (Task 1):
//   baseCol := meshModel.baseColor
//   rl.SetShaderValue(mainShader, baseColLoc, []float32{baseCol[0], baseCol[1], baseCol[2]}, rl.ShaderUniformVec3)

// New:
var baseCol [3]float32
switch viewmode {
case ViewmodeLitClay:
	baseCol = [3]float32{0.78, 0.78, 0.80}
case ViewmodeVertexColor, ViewmodeNormals:
	baseCol = meshModel.baseColor
default:
	baseCol = meshModel.baseColor
}
rl.SetShaderValue(mainShader, baseColLoc, []float32{baseCol[0], baseCol[1], baseCol[2]}, rl.ShaderUniformVec3)
```

Also set the viewmode uniform once per frame (before the draw loop, alongside the other once-per-frame uniforms):

```go
rl.SetShaderValue(mainShader, viewmodeLoc, []float32{float32(viewmode)}, rl.ShaderUniformInt)
```

- [ ] **Step 8: Build + smoke test**

Run: `cd tools/mesh-renderer && go build -o joxblox-mesh-renderer.exe . && cd ../.. && go run ./cmd/joxblox`
Open Meshes sub-tab, load a mesh. Default mode (vertex color) should look like Task 2's output. The viewmode arg defaults to 0; Normals/Lit Clay are not yet picker-driven (Task 6 wires the UI).

- [ ] **Step 9: Commit**

```bash
git add tools/mesh-renderer/main.go tools/mesh-renderer/render_args_test.go tools/mesh-renderer/joxblox-mesh-renderer.exe
git commit -m "feat(mesh-renderer): viewmode protocol arg + Normals fragment branch"
```

---

## Task 4: Add ground grid

**Files:**
- Modify: `tools/mesh-renderer/main.go`

- [ ] **Step 1: Compute scene bounding box once per render**

In `handleRender`, near the top (after `args` is parsed, before the shadow pass), add:

```go
sceneMinY := float32(0)
if len(loadedScene) > 0 {
	first := true
	for _, meshModel := range loadedScene {
		minY := meshModel.bounds.Min.Y
		if first || minY < sceneMinY {
			sceneMinY = minY
			first = false
		}
	}
}
```

- [ ] **Step 2: Draw the grid after the model loop, before `EndMode3D`**

In `handleRender`, find the existing block:

```go
rl.EndMode3D()
rl.EndTextureMode()
```

Insert the grid call BEFORE `rl.EndMode3D()`:

```go
// Subtle ground reference. Drawn after geometry so it doesn't fight depth.
rl.DrawGrid(20, 1.0)
_ = sceneMinY // grid sits at y=0; raylib has no easy push-translate-pop on the matrix stack from Go bindings, so for v1 we keep the grid at the world origin. If meshes float well above/below origin, revisit.
```

(Note: the spec called for translating to `sceneMinY`, but Raylib-go's `rlgl` matrix-stack helpers are awkward to use from the high-level binding. For v1, keep the grid at y=0 — meshes are normalized to fit a unit-ish bounding box around the origin via `normalizeMeshPreviewPositionsCopy` upstream, so y=0 is roughly the visual ground anyway. Drop the unused `sceneMinY` collection if it doesn't end up being used.)

- [ ] **Step 3: Remove the unused `sceneMinY` block from Step 1**

Since Step 2 doesn't end up using it, delete the bounding-box loop from Step 1. Leaves the function lean; can re-add if a future change needs it.

- [ ] **Step 4: Build + smoke test**

Run: `cd tools/mesh-renderer && go build -o joxblox-mesh-renderer.exe . && cd ../.. && go run ./cmd/joxblox`
Load a mesh. Expected: faint gray grid lines visible behind/under the model, helping read camera orientation.

- [ ] **Step 5: Commit**

```bash
git add tools/mesh-renderer/main.go tools/mesh-renderer/joxblox-mesh-renderer.exe
git commit -m "feat(mesh-renderer): draw a ground grid for spatial reference"
```

---

## Task 5: Go-side viewmode field + threading

**Files:**
- Modify: `internal/app/ui/mesh_preview.go`

- [ ] **Step 1: Add viewmode constants and field**

At the top of `internal/app/ui/mesh_preview.go`, near other exported constants, add:

```go
const (
	MeshViewmodeVertexColor = 0
	MeshViewmodeLitClay     = 1
	MeshViewmodeNormals     = 2
)
```

Add to `MeshPreviewWidget`:

```go
viewmode int
```

(Place alongside `wireframe` to keep render-state fields together.)

- [ ] **Step 2: Initialize viewmode in `NewMeshPreviewWidget`**

In the `MeshPreviewWidget{...}` literal in `NewMeshPreviewWidget`, add:

```go
viewmode: MeshViewmodeLitClay,
```

(Default for the no-data case; `applyData` overrides per data type in Step 4.)

- [ ] **Step 3: Add `SetViewmode` method**

Add near the other `Set*` methods (e.g. after `SetWireframe`):

```go
func (viewer *MeshPreviewWidget) SetViewmode(mode int) {
	if viewer == nil {
		return
	}
	switch mode {
	case MeshViewmodeVertexColor, MeshViewmodeLitClay, MeshViewmodeNormals:
		viewer.viewmode = mode
	default:
		viewer.viewmode = MeshViewmodeVertexColor
	}
	viewer.render()
}

func (viewer *MeshPreviewWidget) Viewmode() int {
	if viewer == nil {
		return MeshViewmodeVertexColor
	}
	return viewer.viewmode
}
```

- [ ] **Step 4: Choose default viewmode by data type in `applyData`**

In `applyData`, when `resetView` is true, set the viewmode default based on whether the data is a multi-batch scene:

```go
if resetView {
	if len(data.Batches) > 0 {
		viewer.viewmode = MeshViewmodeVertexColor
	} else {
		viewer.viewmode = MeshViewmodeLitClay
	}
}
```

(Place this block near the existing camera/view reset logic in `applyData`.)

- [ ] **Step 5: Pass viewmode through to the subprocess**

In `render()`, snapshot the viewmode alongside the other state values:

```go
viewmodeSnapshot := viewer.viewmode
```

And pass it to `proc.render`:

```go
rendered, renderErr := proc.render(width, height, cameraXSnapshot, cameraYSnapshot, cameraZSnapshot, selectedBatchSnapshot, yawSnapshot, pitchSnapshot, 1.0, opacitySnapshot, bgHex, wireframeSnapshot, viewmodeSnapshot)
```

- [ ] **Step 6: Update `meshRendererProcess.render` signature and protocol**

Find `func (p *meshRendererProcess) render(...)` and append `viewmode int` to the signature. Update the `RENDER` line format to include it as the 13th positional arg (after wireframe):

```go
func (p *meshRendererProcess) render(width int, height int, cameraX float64, cameraY float64, cameraZ float64, selectedBatch int, yaw float64, pitch float64, zoom float64, opacity float64, bgHex string, wireframe bool, viewmode int) (image.Image, error) {
	// ... existing prelude ...
	wireframeFlag := 0
	if wireframe {
		wireframeFlag = 1
	}
	cmd := fmt.Sprintf("RENDER %d %d %f %f %f %d %f %f %f %f %s %d %d\n",
		width, height, cameraX, cameraY, cameraZ, selectedBatch, yaw, pitch, zoom, opacity, bgHex, wireframeFlag, viewmode)
	// ... rest of existing function ...
}
```

(Keep the exact format of the existing call; only add the trailing `%d` and the new arg. Do not touch the response-decoding code.)

- [ ] **Step 7: Build + run existing tests**

Run: `go build ./... && go test ./internal/app/ui/...`
Expected: PASS. Any test that constructs a render call with the old signature must be updated to include `MeshViewmodeVertexColor` (or appropriate default) as the new last arg.

- [ ] **Step 8: Smoke test**

Run: `go run ./cmd/joxblox`. The Meshes sub-tab should still show the same shaded mesh as Task 4 (vertex-color default, since no UI to change viewmode yet — that's Task 6).

- [ ] **Step 9: Commit**

```bash
git add internal/app/ui/mesh_preview.go
git commit -m "feat(mesh-preview): viewmode field + threading through to subprocess"
```

---

## Task 6: NewMeshPreviewWithToolbar helper

**Files:**
- Modify: `internal/app/ui/mesh_preview.go`

**Why:** Avoids rewriting five call sites for the toolbar; consumers swap one constructor.

- [ ] **Step 1: Add the toolbar helper**

In `internal/app/ui/mesh_preview.go`, near the bottom (or wherever utility constructors live), add:

```go
// NewMeshPreviewWithToolbar wraps a fresh MeshPreviewWidget in a vertical
// container topped by a three-button viewmode segmented control
// (Lit · Color · Normals). Returns the container for layout and the inner
// widget so callers can still drive it (SetData, SetWireframe, etc).
func NewMeshPreviewWithToolbar() (fyne.CanvasObject, *MeshPreviewWidget) {
	viewer := NewMeshPreviewWidget()
	toolbar := newViewmodeToolbar(viewer)
	return container.NewBorder(toolbar, nil, nil, nil, viewer), viewer
}

// newViewmodeToolbar builds the segmented Lit · Color · Normals control.
// Reuses no internal state — entirely driven by viewer.Viewmode() and
// viewer.SetViewmode().
func newViewmodeToolbar(viewer *MeshPreviewWidget) fyne.CanvasObject {
	var buttons []*viewmodeButton
	setMode := func(mode int) {
		viewer.SetViewmode(mode)
		for _, btn := range buttons {
			btn.Refresh()
		}
	}
	isActive := func(mode int) func() bool {
		return func() bool { return viewer.Viewmode() == mode }
	}
	buttons = []*viewmodeButton{
		newViewmodeButton("Lit", isActive(MeshViewmodeLitClay), func() { setMode(MeshViewmodeLitClay) }),
		newViewmodeButton("Color", isActive(MeshViewmodeVertexColor), func() { setMode(MeshViewmodeVertexColor) }),
		newViewmodeButton("Normals", isActive(MeshViewmodeNormals), func() { setMode(MeshViewmodeNormals) }),
	}
	return container.NewHBox(widget.NewLabel("View:"), buttons[0], buttons[1], buttons[2])
}
```

- [ ] **Step 2: Add the `viewmodeButton` widget**

Below the helper, add a small custom button widget. Keep it simple — a `widget.Button` with text + a leading bullet that flips when active:

```go
type viewmodeButton struct {
	widget.Button
	label    string
	isActive func() bool
	onTap    func()
}

func newViewmodeButton(label string, isActive func() bool, onTap func()) *viewmodeButton {
	btn := &viewmodeButton{label: label, isActive: isActive, onTap: onTap}
	btn.OnTapped = onTap
	btn.SetText(label)
	btn.ExtendBaseWidget(btn)
	return btn
}

func (b *viewmodeButton) Refresh() {
	if b.isActive != nil && b.isActive() {
		b.Importance = widget.HighImportance
		b.SetText("● " + b.label)
	} else {
		b.Importance = widget.MediumImportance
		b.SetText(b.label)
	}
	b.Button.Refresh()
}
```

- [ ] **Step 3: Build**

Run: `go build ./...`
Expected: PASS. If `widget.HighImportance` isn't recognized, check the Fyne version; substitute with `widget.LowImportance`/`widget.DangerImportance` etc., or fall back to leaving the bullet prefix as the only active indicator.

- [ ] **Step 4: Smoke test by temporarily wiring one consumer**

Edit `internal/app/ui/tabs/renderdoc/meshes_view.go`. Find the line that constructs the preview widget (`previewWidget := ui.NewMeshPreviewWidget()`) and the line that adds it to the layout. Temporarily replace with:

```go
previewContainer, previewWidget := ui.NewMeshPreviewWithToolbar()
```

Then change wherever `previewWidget` is added to a layout to use `previewContainer` instead (the widget reference still works for `SetData`, `Clear`, etc.).

Run: `go run ./cmd/joxblox`
Open RenderDoc tab → Meshes sub-tab → load a capture → click a mesh row.
Expected: a small "View: Lit Color Normals" toolbar appears above the 3D preview. Clicking each button changes the rendering style.

Revert the temporary edit to `meshes_view.go` for now — the proper consumer updates land in Task 7.

- [ ] **Step 5: Commit**

```bash
git add internal/app/ui/mesh_preview.go
git commit -m "feat(mesh-preview): NewMeshPreviewWithToolbar helper + viewmode buttons"
```

---

## Task 7: Wire up all five consumers

**Files:**
- Modify: `internal/app/ui/asset_view.go`
- Modify: `internal/app/ui/asset_view_preview.go`
- Modify: `internal/app/ui/tabs/heatmap/model_heatmap_tab.go`
- Modify: `internal/app/ui/tabs/lodviewer/lod_viewer_tab.go`
- Modify: `internal/app/ui/tabs/renderdoc/meshes_view.go`

**Why:** Now that the helper exists, every consumer of `MeshPreviewWidget` gets the toolbar by switching one constructor call.

For each file, find the `NewMeshPreviewWidget()` call and replace with `NewMeshPreviewWithToolbar()`, then use the returned container in the layout.

- [ ] **Step 1: Update `internal/app/ui/asset_view.go`**

Find each `ui.NewMeshPreviewWidget()` call (likely just one). Change:

```go
mesh := ui.NewMeshPreviewWidget()
// ... uses mesh in layout
```

To:

```go
meshContainer, mesh := ui.NewMeshPreviewWithToolbar()
// ... uses meshContainer in layout, mesh for SetData/etc.
```

If the file is in package `ui`, drop the `ui.` qualifier.

- [ ] **Step 2: Update `internal/app/ui/asset_view_preview.go`**

Same change as Step 1.

- [ ] **Step 3: Update `internal/app/ui/tabs/heatmap/model_heatmap_tab.go`**

Same change. This file is in package `heatmap`, so `ui.NewMeshPreviewWithToolbar()` is the correct qualifier.

- [ ] **Step 4: Update `internal/app/ui/tabs/lodviewer/lod_viewer_tab.go`**

Same change.

- [ ] **Step 5: Update `internal/app/ui/tabs/renderdoc/meshes_view.go`**

Same change. (If the temporary edit from Task 6 is still in place, this is just confirming the final form.)

- [ ] **Step 6: Build + run all tests**

Run: `go build ./... && go test ./...`
Expected: PASS. The pre-existing `TestPrivateTriangleCounts` failure in `internal/app/loader` is unrelated and can be ignored.

- [ ] **Step 7: Smoke test all five locations**

Run: `go run ./cmd/joxblox`. Walk through:
1. Single Asset tab — load a `.mesh` asset by ID; preview should have the toolbar. Default = Lit Clay.
2. Heatmap tab — load a place that produces a 3D model in the heatmap detail; preview should have the toolbar.
3. LOD viewer tab — load any LOD set; preview should have the toolbar.
4. RenderDoc → Meshes sub-tab — load a `.rdc`, click a mesh; preview should have the toolbar. Default = Vertex Color (since meshes from a capture come in as scenes by default? confirm by inspection — if the data type is single-mesh, default is Lit Clay).

For each: click each viewmode button and confirm the rendering changes appropriately.

- [ ] **Step 8: Commit**

```bash
git add internal/app/ui/asset_view.go internal/app/ui/asset_view_preview.go internal/app/ui/tabs/heatmap/model_heatmap_tab.go internal/app/ui/tabs/lodviewer/lod_viewer_tab.go internal/app/ui/tabs/renderdoc/meshes_view.go
git commit -m "feat(mesh-preview): wire viewmode toolbar into all five consumers"
```

---

## Task 8: Final verification + CHANGELOG

- [ ] **Step 1: Run the full test suite**

Run: `go test ./...`
Expected: PASS (modulo the pre-existing `TestPrivateTriangleCounts` failure).

- [ ] **Step 2: Run vet**

Run: `go vet ./...`
Expected: no warnings.

- [ ] **Step 3: Verify the subprocess binary builds cleanly**

Run: `cd tools/mesh-renderer && go build -o joxblox-mesh-renderer.exe . && cd ../..`
Expected: PASS.

- [ ] **Step 4: Update CHANGELOG**

Edit `CHANGELOG.md`. Under the existing `## Unreleased` / `### Added` block (added in the materials feature), append:

```markdown
- Mesh previews now render with proper Phong lighting (key + camera-relative fill + rim) and contact shadows from a fixed key light, replacing the previous flat unlit silhouette
- Three viewmodes selectable from a small toolbar above every mesh preview: `Lit` (neutral clay shading), `Color` (per-vertex colors, today's default for scenes), `Normals` (RGB-mapped surface normals for triangulation/seam debugging)
- Subtle ground grid for spatial reference in 3D previews
```

- [ ] **Step 5: Commit**

```bash
git add CHANGELOG.md
git commit -m "docs(changelog): note mesh renderer lighting + viewmodes"
```
