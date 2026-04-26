# RenderDoc PBR Material Pairing Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a third "Materials" sub-tab to the RenderDoc tab that groups textures into deduplicated PBR materials (Color + Normal + MR) by walking PS-stage shader-resource bindings on each draw call.

**Architecture:** Extend the mesh-XML parser to also track `CreateShaderResourceView` and `PSSetShaderResources`, snapshotting the PS-bound texture IDs onto each `DrawCall`. A new `BuildMaterials` function walks those draw calls, filters out scene-global textures (built-ins, render targets, anything bound to ≥80% of draws), classifies the remainder using the existing texture `Category` (Normal=DXT5nm, MR=Blank/CustomMR, Color=AssetOpaque/Alpha/Raw), and dedupes by `(Color, Normal, MR)` tuple. The Materials sub-tab mirrors the Meshes sub-tab structure: sortable table on top with inline thumbnails, preview pane below.

**Tech Stack:** Go 1.23+, Fyne v2 widgets, existing `internal/renderdoc` and `internal/app/ui/tabs/renderdoc` packages.

**Spec:** [docs/superpowers/specs/2026-04-26-renderdoc-pbr-materials-design.md](../specs/2026-04-26-renderdoc-pbr-materials-design.md)

---

## File Structure

**Modify:**
- `internal/renderdoc/meshes.go` — add `SRVToTexture map[string]string` to `MeshReport`, add `PSTextureIDs []string` to `DrawCall`, parse `CreateShaderResourceView` and `PSSetShaderResources`, snapshot at draw time.
- `internal/renderdoc/meshes_test.go` — extend with PS-binding parse tests.
- `internal/app/ui/tabs/renderdoc/renderdoc_tab.go` — add `materialsIndex = 2`, append a third sub-tab, route launcher loads.

**Create:**
- `internal/renderdoc/materials.go` — `Material` type and `BuildMaterials` function.
- `internal/renderdoc/materials_test.go` — unit tests for `BuildMaterials`.
- `internal/app/ui/tabs/renderdoc/materials_view.go` — sub-tab UI with table, preview pane, and its own loader function.

---

## Task 1: Parse CreateShaderResourceView (SRV → Texture map)

**Files:**
- Modify: `internal/renderdoc/meshes.go`
- Test: `internal/renderdoc/meshes_test.go`

**Why:** D3D11 binds Shader Resource Views (SRVs), not textures directly. Without a map from SRV resource ID → underlying texture resource ID, PS bindings can't be resolved to textures.

- [ ] **Step 1: Write the failing test**

Add to `meshes_test.go`:

```go
func TestParseCreateShaderResourceViewBuildsMap(t *testing.T) {
	xmlData := `<rdc>
<chunk name="ID3D11Device::CreateShaderResourceView">
  <ResourceId name="pResource">12345</ResourceId>
  <ResourceId name="ppSRView">99001</ResourceId>
</chunk>
<chunk name="ID3D11Device::CreateShaderResourceView">
  <ResourceId name="pResource">12346</ResourceId>
  <ResourceId name="ppSRView">99002</ResourceId>
</chunk>
</rdc>`
	report, err := parseMeshXML(strings.NewReader(xmlData))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got := report.SRVToTexture["99001"]; got != "12345" {
		t.Errorf("SRV 99001 → %q, want 12345", got)
	}
	if got := report.SRVToTexture["99002"]; got != "12346" {
		t.Errorf("SRV 99002 → %q, want 12346", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/renderdoc/ -run TestParseCreateShaderResourceViewBuildsMap -v`
Expected: FAIL — `report.SRVToTexture` does not exist.

- [ ] **Step 3: Add `SRVToTexture` to `MeshReport` and the parse case**

In `meshes.go`, add the field to `MeshReport`:

```go
type MeshReport struct {
	Buffers       map[string]BufferInfo
	InputLayouts  map[string]InputLayoutInfo
	DrawCalls     []DrawCall
	SRVToTexture  map[string]string // SRV resource ID → underlying texture resource ID
}
```

Initialize it in `parseMeshXML` next to the other maps:

```go
report := &MeshReport{
	Buffers:      map[string]BufferInfo{},
	InputLayouts: map[string]InputLayoutInfo{},
	DrawCalls:    []DrawCall{},
	SRVToTexture: map[string]string{},
}
```

Add a new `case` inside the switch on `attr(start, "name")` (next to the other `ID3D11Device::*` cases):

```go
case "ID3D11Device::CreateShaderResourceView":
	srvID, texID, parseErr := parseCreateSRVChunk(decoder)
	if parseErr != nil {
		return nil, parseErr
	}
	if srvID != "" && texID != "" {
		report.SRVToTexture[srvID] = texID
	}
```

Add the helper near the other `parse*Chunk` functions (after `parseCreateInputLayoutChunk`):

```go
// parseCreateSRVChunk extracts (srvID, textureID) from a
// CreateShaderResourceView chunk. Returns ("","",nil) if either ID
// is missing.
func parseCreateSRVChunk(decoder *xml.Decoder) (string, string, error) {
	var srvID, texID string
	depth := 1
	for depth > 0 {
		token, err := decoder.Token()
		if err != nil {
			return "", "", fmt.Errorf("read createshaderresourceview: %w", err)
		}
		switch typed := token.(type) {
		case xml.StartElement:
			depth++
			if typed.Name.Local == "ResourceId" {
				name := attr(typed, "name")
				value, readErr := readTextElement(decoder)
				if readErr != nil {
					return "", "", readErr
				}
				depth--
				switch name {
				case "pResource":
					texID = strings.TrimSpace(value)
				case "ppSRView":
					srvID = strings.TrimSpace(value)
				}
			}
		case xml.EndElement:
			depth--
		}
	}
	return srvID, texID, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/renderdoc/ -run TestParseCreateShaderResourceViewBuildsMap -v`
Expected: PASS.

- [ ] **Step 5: Run the full renderdoc test suite to confirm no regressions**

Run: `go test ./internal/renderdoc/...`
Expected: PASS (all existing tests still green).

- [ ] **Step 6: Commit**

```bash
git add internal/renderdoc/meshes.go internal/renderdoc/meshes_test.go
git commit -m "feat(renderdoc): parse CreateShaderResourceView into SRV→texture map"
```

---

## Task 2: Parse PSSetShaderResources and snapshot onto DrawCall

**Files:**
- Modify: `internal/renderdoc/meshes.go`
- Test: `internal/renderdoc/meshes_test.go`

**Why:** With the SRV map built, we can now follow which SRVs are bound to PS at draw time and resolve them to texture IDs.

- [ ] **Step 1: Write the failing test**

Add to `meshes_test.go`:

```go
func TestParsePSSetShaderResourcesPopulatesDrawCall(t *testing.T) {
	xmlData := `<rdc>
<chunk name="ID3D11Device::CreateShaderResourceView">
  <ResourceId name="pResource">12345</ResourceId>
  <ResourceId name="ppSRView">99001</ResourceId>
</chunk>
<chunk name="ID3D11Device::CreateShaderResourceView">
  <ResourceId name="pResource">12346</ResourceId>
  <ResourceId name="ppSRView">99002</ResourceId>
</chunk>
<chunk name="ID3D11DeviceContext::PSSetShaderResources">
  <uint name="StartSlot">0</uint>
  <uint name="NumViews">2</uint>
  <array name="ppShaderResourceViews">
    <ResourceId>99001</ResourceId>
    <ResourceId>99002</ResourceId>
  </array>
</chunk>
<chunk name="ID3D11DeviceContext::DrawIndexed">
  <uint name="IndexCount">36</uint>
  <uint name="StartIndexLocation">0</uint>
  <int name="BaseVertexLocation">0</int>
</chunk>
</rdc>`
	report, err := parseMeshXML(strings.NewReader(xmlData))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(report.DrawCalls) != 1 {
		t.Fatalf("draw calls: got %d, want 1", len(report.DrawCalls))
	}
	dc := report.DrawCalls[0]
	want := []string{"12345", "12346"}
	if len(dc.PSTextureIDs) != len(want) {
		t.Fatalf("PSTextureIDs len: got %d, want %d", len(dc.PSTextureIDs), len(want))
	}
	for i, id := range want {
		if dc.PSTextureIDs[i] != id {
			t.Errorf("slot %d: got %q, want %q", i, dc.PSTextureIDs[i], id)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/renderdoc/ -run TestParsePSSetShaderResourcesPopulatesDrawCall -v`
Expected: FAIL — `DrawCall.PSTextureIDs` does not exist.

- [ ] **Step 3: Add `PSTextureIDs` to `DrawCall` and implement parsing**

Modify the `DrawCall` struct in `meshes.go`:

```go
type DrawCall struct {
	IndexCount         int
	StartIndexLocation int
	BaseVertexLocation int
	InstanceCount      int
	IndexBufferID      string
	IndexBufferFormat  string
	IndexBufferOffset  int
	VertexBuffers      []DrawCallVertexBuffer
	InputLayoutID      string
	PSTextureIDs       []string // texture IDs bound to PS stage at draw time, slot-indexed
}
```

In `parseMeshXML`, declare a current-PS-bindings tracker alongside the other "current" trackers:

```go
var currentPSSrvIDs []string
```

Add a new `case` inside the switch on `attr(start, "name")`:

```go
case "ID3D11DeviceContext::PSSetShaderResources":
	startSlot, srvIDs, parseErr := parsePSSetShaderResourcesChunk(decoder)
	if parseErr != nil {
		return nil, parseErr
	}
	currentPSSrvIDs = mergePSBindings(currentPSSrvIDs, startSlot, srvIDs)
```

In the existing `case "ID3D11DeviceContext::DrawIndexed"` ... block, after assigning the other `dc.*` fields, add the resolution step:

```go
dc.PSTextureIDs = resolvePSTextureIDs(currentPSSrvIDs, report.SRVToTexture)
```

Add the three helpers at the bottom of the file:

```go
// parsePSSetShaderResourcesChunk reads a PSSetShaderResources chunk and
// returns (startSlot, srvIDs). srvIDs is in slot order starting at startSlot.
// Empty SRV IDs (unbound slots) are preserved as empty strings.
func parsePSSetShaderResourcesChunk(decoder *xml.Decoder) (int, []string, error) {
	startSlot := 0
	var srvIDs []string
	depth := 1
	inArray := false
	for depth > 0 {
		token, err := decoder.Token()
		if err != nil {
			return 0, nil, fmt.Errorf("read pssetshaderresources: %w", err)
		}
		switch typed := token.(type) {
		case xml.StartElement:
			depth++
			switch typed.Name.Local {
			case "uint":
				if attr(typed, "name") == "StartSlot" {
					value, readErr := readIntElement(decoder)
					if readErr != nil {
						return 0, nil, readErr
					}
					depth--
					startSlot = value
				} else if skipErr := skipElement(decoder); skipErr != nil {
					return 0, nil, skipErr
				} else {
					depth--
				}
			case "array":
				if attr(typed, "name") == "ppShaderResourceViews" {
					inArray = true
				}
			case "ResourceId":
				if inArray {
					value, readErr := readTextElement(decoder)
					if readErr != nil {
						return 0, nil, readErr
					}
					depth--
					srvIDs = append(srvIDs, strings.TrimSpace(value))
				} else if skipErr := skipElement(decoder); skipErr != nil {
					return 0, nil, skipErr
				} else {
					depth--
				}
			}
		case xml.EndElement:
			depth--
			if typed.Name.Local == "array" {
				inArray = false
			}
		}
	}
	return startSlot, srvIDs, nil
}

// mergePSBindings overlays a PSSetShaderResources call onto the current
// bindings: writes srvIDs into slots [startSlot, startSlot+len(srvIDs)),
// preserving any earlier slots, and grows the slice if needed.
func mergePSBindings(current []string, startSlot int, srvIDs []string) []string {
	required := startSlot + len(srvIDs)
	if len(current) < required {
		grown := make([]string, required)
		copy(grown, current)
		current = grown
	}
	for i, id := range srvIDs {
		current[startSlot+i] = id
	}
	return current
}

// resolvePSTextureIDs maps a slice of bound SRV IDs to their underlying
// texture IDs. SRVs not in the map (or empty entries) become empty strings,
// preserving slot indices.
func resolvePSTextureIDs(srvIDs []string, srvToTexture map[string]string) []string {
	if len(srvIDs) == 0 {
		return nil
	}
	out := make([]string, len(srvIDs))
	for i, srvID := range srvIDs {
		if srvID == "" {
			continue
		}
		if texID, ok := srvToTexture[srvID]; ok {
			out[i] = texID
		}
	}
	return out
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/renderdoc/ -run TestParsePSSetShaderResourcesPopulatesDrawCall -v`
Expected: PASS.

- [ ] **Step 5: Add a test for the merge-bindings behavior**

Add to `meshes_test.go`:

```go
func TestMergePSBindingsOverlaysAndGrows(t *testing.T) {
	current := []string{"a", "b", "c"}
	got := mergePSBindings(current, 1, []string{"X", "Y", "Z"})
	want := []string{"a", "X", "Y", "Z"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("merge: got %v, want %v", got, want)
	}
}
```

(Add `"reflect"` to imports if not already there.)

- [ ] **Step 6: Run new test**

Run: `go test ./internal/renderdoc/ -run TestMergePSBindingsOverlaysAndGrows -v`
Expected: PASS.

- [ ] **Step 7: Run full renderdoc test suite**

Run: `go test ./internal/renderdoc/...`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/renderdoc/meshes.go internal/renderdoc/meshes_test.go
git commit -m "feat(renderdoc): snapshot PS-bound texture IDs onto each DrawCall"
```

---

## Task 3: Material type and BuildMaterials

**Files:**
- Create: `internal/renderdoc/materials.go`
- Test: `internal/renderdoc/materials_test.go`

**Why:** The core deduplication and classification logic. Pure function over already-parsed data — easy to test exhaustively before touching UI.

- [ ] **Step 1: Write the failing test (basic full-PBR triple case)**

Create `internal/renderdoc/materials_test.go`:

```go
package renderdoc

import (
	"reflect"
	"sort"
	"testing"
)

// makeTexture is a test helper that builds a minimal TextureInfo.
func makeTexture(id string, category TextureCategory, bytes int64) TextureInfo {
	return TextureInfo{ResourceID: id, Category: category, Bytes: bytes}
}

func TestBuildMaterialsGroupsFullPBRTriple(t *testing.T) {
	textures := &Report{
		Textures: []TextureInfo{
			makeTexture("100", CategoryAssetOpaque, 1024),
			makeTexture("200", CategoryNormalDXT5nm, 512),
			makeTexture("300", CategoryCustomMR, 256),
		},
	}
	meshes := &MeshReport{
		DrawCalls: []DrawCall{
			{PSTextureIDs: []string{"100", "200", "300"}},
		},
	}
	got := BuildMaterials(textures, meshes)
	if len(got) != 1 {
		t.Fatalf("materials: got %d, want 1", len(got))
	}
	mat := got[0]
	if mat.ColorTextureID != "100" || mat.NormalTextureID != "200" || mat.MRTextureID != "300" {
		t.Errorf("classification wrong: %+v", mat)
	}
	if mat.DrawCallCount != 1 {
		t.Errorf("DrawCallCount: got %d, want 1", mat.DrawCallCount)
	}
	if mat.TotalBytes != 1024+512+256 {
		t.Errorf("TotalBytes: got %d, want %d", mat.TotalBytes, 1024+512+256)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/renderdoc/ -run TestBuildMaterialsGroupsFullPBRTriple -v`
Expected: FAIL — `BuildMaterials` does not exist.

- [ ] **Step 3: Implement `BuildMaterials`**

Create `internal/renderdoc/materials.go`:

```go
package renderdoc

import "sort"

// Material groups one (Color, Normal, MR) texture tuple bound together at the
// PS stage on at least one draw call. Built by BuildMaterials from the
// already-parsed texture and mesh reports.
type Material struct {
	ColorTextureID  string
	NormalTextureID string
	MRTextureID     string
	OtherTextureIDs []string // PS-bound textures we couldn't classify
	DrawCallCount   int
	TotalBytes      int64    // sum of unique map bytes
	MeshHashes      []string // unique mesh content hashes this material was used to draw
}

// sceneGlobalDrawCallFraction is the threshold above which a texture bound on
// a draw call is treated as scene-global (shadow map, env probe, etc.) rather
// than per-material. Empirical — Roblox's real per-material textures are
// bound on a small fraction of draws; globals are bound on essentially all.
const sceneGlobalDrawCallFraction = 0.8

// BuildMaterials walks meshes.DrawCalls, classifies each PS-bound texture
// using the existing TextureInfo.Category, filters out scene-global textures,
// and dedupes materials by the (Color, Normal, MR) tuple.
func BuildMaterials(textures *Report, meshes *MeshReport) []Material {
	if textures == nil || meshes == nil || len(meshes.DrawCalls) == 0 {
		return nil
	}

	textureByID := map[string]TextureInfo{}
	for _, t := range textures.Textures {
		textureByID[t.ResourceID] = t
	}

	globals := computeSceneGlobalTextureIDs(textures, meshes, textureByID)

	type matKey struct{ color, normal, mr string }
	byKey := map[matKey]*Material{}
	var order []matKey

	// Per-draw mesh hash lookup. We avoid re-running BuildMeshes by hashing
	// the same way it does — but since BuildMaterials runs after BuildMeshes
	// in the load pipeline, the simpler thing is to ignore mesh linkage
	// entirely if we don't have a hash on the draw call. For v1 we leave
	// MeshHashes empty unless wired in by the loader; UI handles len==0.

	for _, dc := range meshes.DrawCalls {
		var color, normal, mr string
		var others []string
		for _, texID := range dc.PSTextureIDs {
			if texID == "" {
				continue
			}
			if _, isGlobal := globals[texID]; isGlobal {
				continue
			}
			tex, known := textureByID[texID]
			if !known {
				others = append(others, texID)
				continue
			}
			switch tex.Category {
			case CategoryNormalDXT5nm:
				if normal == "" {
					normal = texID
				} else {
					others = append(others, texID)
				}
			case CategoryBlankMR, CategoryCustomMR:
				if mr == "" {
					mr = texID
				} else {
					others = append(others, texID)
				}
			case CategoryAssetOpaque, CategoryAssetAlpha, CategoryAssetRaw:
				if color == "" {
					color = texID
				} else {
					others = append(others, texID)
				}
			default:
				others = append(others, texID)
			}
		}
		if color == "" && normal == "" && mr == "" && len(others) == 0 {
			continue
		}
		key := matKey{color, normal, mr}
		mat, exists := byKey[key]
		if !exists {
			mat = &Material{
				ColorTextureID:  color,
				NormalTextureID: normal,
				MRTextureID:     mr,
				TotalBytes:      sumUniqueBytes(textureByID, color, normal, mr),
			}
			byKey[key] = mat
			order = append(order, key)
		}
		mat.DrawCallCount++
		// Union "others" by appending unseen IDs only.
		for _, o := range others {
			if !containsString(mat.OtherTextureIDs, o) {
				mat.OtherTextureIDs = append(mat.OtherTextureIDs, o)
			}
		}
	}

	out := make([]Material, 0, len(order))
	for _, k := range order {
		out = append(out, *byKey[k])
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].TotalBytes > out[j].TotalBytes
	})
	return out
}

func computeSceneGlobalTextureIDs(textures *Report, meshes *MeshReport, byID map[string]TextureInfo) map[string]struct{} {
	globals := map[string]struct{}{}
	// Category-based globals.
	for _, t := range textures.Textures {
		switch t.Category {
		case CategoryBuiltin, CategoryBuiltinBRDFLUT, CategoryRenderTgt, CategoryDepthTgt, CategoryCubemap:
			globals[t.ResourceID] = struct{}{}
		}
	}
	// Frequency-based globals: any texture bound on >= 80% of draw calls.
	totalDraws := len(meshes.DrawCalls)
	if totalDraws == 0 {
		return globals
	}
	bindCount := map[string]int{}
	for _, dc := range meshes.DrawCalls {
		seen := map[string]bool{}
		for _, texID := range dc.PSTextureIDs {
			if texID == "" || seen[texID] {
				continue
			}
			seen[texID] = true
			bindCount[texID]++
		}
	}
	threshold := int(float64(totalDraws) * sceneGlobalDrawCallFraction)
	if threshold < 1 {
		threshold = 1
	}
	for texID, n := range bindCount {
		if n >= threshold {
			globals[texID] = struct{}{}
		}
	}
	return globals
}

func sumUniqueBytes(byID map[string]TextureInfo, ids ...string) int64 {
	seen := map[string]bool{}
	var total int64
	for _, id := range ids {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		if t, ok := byID[id]; ok {
			total += t.Bytes
		}
	}
	return total
}

func containsString(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/renderdoc/ -run TestBuildMaterialsGroupsFullPBRTriple -v`
Expected: PASS.

- [ ] **Step 5: Add tests for the other spec scenarios**

Append to `materials_test.go`:

```go
func TestBuildMaterialsDedupesByTuple(t *testing.T) {
	textures := &Report{
		Textures: []TextureInfo{
			makeTexture("100", CategoryAssetOpaque, 1024),
			makeTexture("200", CategoryNormalDXT5nm, 512),
		},
	}
	meshes := &MeshReport{
		DrawCalls: []DrawCall{
			{PSTextureIDs: []string{"100", "200"}},
			{PSTextureIDs: []string{"100", "200"}},
			{PSTextureIDs: []string{"100", "200"}},
		},
	}
	got := BuildMaterials(textures, meshes)
	if len(got) != 1 {
		t.Fatalf("materials: got %d, want 1", len(got))
	}
	if got[0].DrawCallCount != 3 {
		t.Errorf("DrawCallCount: got %d, want 3", got[0].DrawCallCount)
	}
}

func TestBuildMaterialsSplitsBySharedColorDifferentNormal(t *testing.T) {
	textures := &Report{
		Textures: []TextureInfo{
			makeTexture("100", CategoryAssetOpaque, 1024),
			makeTexture("200", CategoryNormalDXT5nm, 512),
			makeTexture("201", CategoryNormalDXT5nm, 512),
		},
	}
	meshes := &MeshReport{
		DrawCalls: []DrawCall{
			{PSTextureIDs: []string{"100", "200"}},
			{PSTextureIDs: []string{"100", "201"}},
		},
	}
	got := BuildMaterials(textures, meshes)
	if len(got) != 2 {
		t.Fatalf("materials: got %d, want 2", len(got))
	}
}

func TestBuildMaterialsExcludesGlobalByCategory(t *testing.T) {
	textures := &Report{
		Textures: []TextureInfo{
			makeTexture("100", CategoryAssetOpaque, 1024),
			makeTexture("999", CategoryBuiltinBRDFLUT, 64),
		},
	}
	meshes := &MeshReport{
		DrawCalls: []DrawCall{
			{PSTextureIDs: []string{"100", "999"}},
		},
	}
	got := BuildMaterials(textures, meshes)
	if len(got) != 1 || got[0].ColorTextureID != "100" {
		t.Fatalf("expected single color-only material, got %+v", got)
	}
	if len(got[0].OtherTextureIDs) != 0 {
		t.Errorf("global texture leaked into Others: %v", got[0].OtherTextureIDs)
	}
}

func TestBuildMaterialsExcludesGlobalByFrequency(t *testing.T) {
	textures := &Report{
		Textures: []TextureInfo{
			makeTexture("100", CategoryAssetOpaque, 1024),
			makeTexture("101", CategoryAssetOpaque, 1024),
			makeTexture("102", CategoryAssetOpaque, 1024),
			makeTexture("103", CategoryAssetOpaque, 1024),
			// "shadow" looks like an asset by category but is bound everywhere.
			makeTexture("shadow", CategoryAssetRaw, 64),
		},
	}
	meshes := &MeshReport{
		DrawCalls: []DrawCall{
			{PSTextureIDs: []string{"100", "shadow"}},
			{PSTextureIDs: []string{"101", "shadow"}},
			{PSTextureIDs: []string{"102", "shadow"}},
			{PSTextureIDs: []string{"103", "shadow"}},
		},
	}
	got := BuildMaterials(textures, meshes)
	if len(got) != 4 {
		t.Fatalf("materials: got %d, want 4", len(got))
	}
	for _, m := range got {
		if m.ColorTextureID == "shadow" {
			t.Errorf("shadow leaked as color: %+v", m)
		}
		if containsString(m.OtherTextureIDs, "shadow") {
			t.Errorf("shadow leaked into Others: %+v", m)
		}
	}
}

func TestBuildMaterialsMultipleColorsKeepsLowestSlotFirst(t *testing.T) {
	textures := &Report{
		Textures: []TextureInfo{
			makeTexture("100", CategoryAssetOpaque, 1024),
			makeTexture("101", CategoryAssetOpaque, 512),
		},
	}
	meshes := &MeshReport{
		DrawCalls: []DrawCall{
			{PSTextureIDs: []string{"100", "101"}},
		},
	}
	got := BuildMaterials(textures, meshes)
	if len(got) != 1 {
		t.Fatalf("materials: got %d, want 1", len(got))
	}
	if got[0].ColorTextureID != "100" {
		t.Errorf("ColorTextureID: got %q, want 100", got[0].ColorTextureID)
	}
	want := []string{"101"}
	if !reflect.DeepEqual(got[0].OtherTextureIDs, want) {
		t.Errorf("OtherTextureIDs: got %v, want %v", got[0].OtherTextureIDs, want)
	}
}

func TestBuildMaterialsSortsByTotalBytesDescending(t *testing.T) {
	textures := &Report{
		Textures: []TextureInfo{
			makeTexture("a", CategoryAssetOpaque, 100),
			makeTexture("b", CategoryAssetOpaque, 5000),
			makeTexture("c", CategoryAssetOpaque, 1000),
		},
	}
	meshes := &MeshReport{
		DrawCalls: []DrawCall{
			{PSTextureIDs: []string{"a"}},
			{PSTextureIDs: []string{"b"}},
			{PSTextureIDs: []string{"c"}},
		},
	}
	got := BuildMaterials(textures, meshes)
	var sizes []int64
	for _, m := range got {
		sizes = append(sizes, m.TotalBytes)
	}
	want := []int64{5000, 1000, 100}
	if !reflect.DeepEqual(sizes, want) {
		t.Errorf("sort order: got %v, want %v", sizes, want)
	}
}
```

(Add `"sort"` to imports if not already present — actually not needed in the test file; remove if unused.)

- [ ] **Step 6: Run all materials tests**

Run: `go test ./internal/renderdoc/ -run TestBuildMaterials -v`
Expected: PASS for all six.

- [ ] **Step 7: Run full renderdoc test suite**

Run: `go test ./internal/renderdoc/...`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/renderdoc/materials.go internal/renderdoc/materials_test.go
git commit -m "feat(renderdoc): BuildMaterials — dedupe (color,normal,mr) tuples"
```

---

## Task 4: MeshHashes wiring on materials

**Files:**
- Modify: `internal/renderdoc/materials.go`
- Modify: `internal/renderdoc/materials_test.go`

**Why:** `BuildMaterials` currently leaves `MeshHashes` empty. Wire it via the same per-draw hashing that `BuildMeshes` does, so the materials view can show "this material is on N meshes."

- [ ] **Step 1: Write the failing test**

Append to `materials_test.go`:

```go
// stubReader implements BufferReader for tests.
type stubReader map[string][]byte

func (s stubReader) ReadBuffer(id string) ([]byte, error) {
	return s[id], nil
}

func TestBuildMaterialsPopulatesMeshHashes(t *testing.T) {
	textures := &Report{
		Textures: []TextureInfo{
			makeTexture("100", CategoryAssetOpaque, 1024),
		},
	}
	// Two draws share PS textures (one material) but use two different VB+IB
	// pairs, so two distinct mesh hashes should land on the material.
	meshes := &MeshReport{
		Buffers: map[string]BufferInfo{
			"vbA":  {ResourceID: "vbA", InitialDataBufferID: "dataVbA"},
			"ibA":  {ResourceID: "ibA", InitialDataBufferID: "dataIbA"},
			"vbB":  {ResourceID: "vbB", InitialDataBufferID: "dataVbB"},
			"ibB":  {ResourceID: "ibB", InitialDataBufferID: "dataIbB"},
		},
		DrawCalls: []DrawCall{
			{
				PSTextureIDs:  []string{"100"},
				VertexBuffers: []DrawCallVertexBuffer{{BufferID: "vbA"}},
				IndexBufferID: "ibA",
			},
			{
				PSTextureIDs:  []string{"100"},
				VertexBuffers: []DrawCallVertexBuffer{{BufferID: "vbB"}},
				IndexBufferID: "ibB",
			},
		},
	}
	reader := stubReader{
		"dataVbA": []byte("vertexA"),
		"dataIbA": []byte("indexA"),
		"dataVbB": []byte("vertexB"),
		"dataIbB": []byte("indexB"),
	}
	got := BuildMaterialsWithMeshHashes(textures, meshes, reader)
	if len(got) != 1 {
		t.Fatalf("materials: got %d, want 1", len(got))
	}
	if len(got[0].MeshHashes) != 2 {
		t.Errorf("MeshHashes: got %d, want 2", len(got[0].MeshHashes))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/renderdoc/ -run TestBuildMaterialsPopulatesMeshHashes -v`
Expected: FAIL — `BuildMaterialsWithMeshHashes` does not exist.

- [ ] **Step 3: Add `BuildMaterialsWithMeshHashes`**

Append to `materials.go`:

```go
// BuildMaterialsWithMeshHashes is BuildMaterials plus per-draw mesh hashing,
// so each Material's MeshHashes lists the unique meshes it draws. Reuses the
// same hash function as BuildMeshes for cross-tab consistency. Errors hashing
// an individual draw are silently dropped (the material still appears, just
// without that mesh hash) — same tolerance as BuildMeshes.
func BuildMaterialsWithMeshHashes(textures *Report, meshes *MeshReport, reader BufferReader) []Material {
	if textures == nil || meshes == nil || len(meshes.DrawCalls) == 0 {
		return nil
	}
	// Pre-hash every draw once so we don't re-hash duplicates.
	drawHashes := make([]string, len(meshes.DrawCalls))
	for i, dc := range meshes.DrawCalls {
		if reader == nil || len(dc.VertexBuffers) == 0 || dc.IndexBufferID == "" {
			continue
		}
		hash, _, _, err := hashMeshBuffers(dc, meshes.Buffers, reader)
		if err == nil {
			drawHashes[i] = hash
		}
	}
	out := BuildMaterials(textures, meshes)
	// Re-walk to attach mesh hashes. Precompute the lookup tables once so
	// classifyDrawForKey is O(1) per draw call, not O(N) — a 5000-draw
	// capture would otherwise be 25M map-builds.
	textureByID := map[string]TextureInfo{}
	for _, t := range textures.Textures {
		textureByID[t.ResourceID] = t
	}
	globals := computeSceneGlobalTextureIDs(textures, meshes, textureByID)
	hashesByKey := map[[3]string]map[string]struct{}{}
	for i, dc := range meshes.DrawCalls {
		hash := drawHashes[i]
		if hash == "" {
			continue
		}
		k := classifyDrawForKey(dc, textureByID, globals)
		if k == ([3]string{}) {
			continue
		}
		if hashesByKey[k] == nil {
			hashesByKey[k] = map[string]struct{}{}
		}
		hashesByKey[k][hash] = struct{}{}
	}
	for i := range out {
		k := [3]string{out[i].ColorTextureID, out[i].NormalTextureID, out[i].MRTextureID}
		set := hashesByKey[k]
		if len(set) == 0 {
			continue
		}
		hashes := make([]string, 0, len(set))
		for h := range set {
			hashes = append(hashes, h)
		}
		sort.Strings(hashes)
		out[i].MeshHashes = hashes
	}
	return out
}

// classifyDrawForKey returns the (color, normal, mr) key a single draw call
// produces — same logic as the inline classification in BuildMaterials but
// extracted so we can reuse it when attaching mesh hashes. Caller passes
// pre-built textureByID + globals so this stays O(slots) per call.
func classifyDrawForKey(dc DrawCall, textureByID map[string]TextureInfo, globals map[string]struct{}) [3]string {
	var color, normal, mr string
	for _, texID := range dc.PSTextureIDs {
		if texID == "" {
			continue
		}
		if _, isGlobal := globals[texID]; isGlobal {
			continue
		}
		tex, known := textureByID[texID]
		if !known {
			continue
		}
		switch tex.Category {
		case CategoryNormalDXT5nm:
			if normal == "" {
				normal = texID
			}
		case CategoryBlankMR, CategoryCustomMR:
			if mr == "" {
				mr = texID
			}
		case CategoryAssetOpaque, CategoryAssetAlpha, CategoryAssetRaw:
			if color == "" {
				color = texID
			}
		}
	}
	if color == "" && normal == "" && mr == "" {
		return [3]string{}
	}
	return [3]string{color, normal, mr}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/renderdoc/ -run TestBuildMaterialsPopulatesMeshHashes -v`
Expected: PASS.

- [ ] **Step 5: Run full renderdoc suite**

Run: `go test ./internal/renderdoc/...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/renderdoc/materials.go internal/renderdoc/materials_test.go
git commit -m "feat(renderdoc): attach mesh hashes to materials via BufferReader"
```

---

## Task 5: Materials sub-tab UI scaffolding (table + state, no preview)

**Files:**
- Create: `internal/app/ui/tabs/renderdoc/materials_view.go`
- Modify: `internal/app/ui/tabs/renderdoc/renderdoc_tab.go`

**Why:** Build the visible scaffolding first so loads land somewhere. Preview pane comes in Task 6.

- [ ] **Step 1: Create `materials_view.go` with the table, loader, and load wiring**

Create `internal/app/ui/tabs/renderdoc/materials_view.go`:

```go
package renderdoctab

import (
	"errors"
	"fmt"
	"image"
	"strconv"
	"strings"

	"joxblox/internal/format"
	"joxblox/internal/renderdoc"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	fyneDialog "fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
	nativeDialog "github.com/sqweek/dialog"
)

type materialsTabState struct {
	materials        []renderdoc.Material
	displayMaterials []renderdoc.Material
	textureReport    *renderdoc.Report
	meshReport       *renderdoc.MeshReport
	bufferStore      *renderdoc.BufferStore
	xmlPath          string
	sortColumn       string
	sortDescending   bool
	filterText       string
	selectedRow      int
	thumbnailCache   map[string]image.Image // texID → decoded base mip
	textureByID      map[string]renderdoc.TextureInfo
}

var materialColumnHeaders = []string{"Color", "Normal", "MR", "Color Hash", "Draws", "Meshes", "VRAM"}

func newMaterialsSubTab(window fyne.Window, onLoaded func(path string)) (fyne.CanvasObject, func(path string)) {
	state := &materialsTabState{
		sortColumn:     "VRAM",
		sortDescending: true,
		selectedRow:    -1,
		thumbnailCache: map[string]image.Image{},
		textureByID:    map[string]renderdoc.TextureInfo{},
	}

	pathLabel := widget.NewLabel("No capture loaded.")
	pathLabel.Wrapping = fyne.TextWrapWord
	summaryLabel := widget.NewLabel("")
	summaryLabel.Wrapping = fyne.TextWrapWord
	countLabel := widget.NewLabel("")

	progressBar := widget.NewProgressBarInfinite()
	progressBar.Hide()

	filterEntry := widget.NewEntry()
	filterEntry.SetPlaceHolder("Filter by texture ID or hash")

	previewInfoLabel := widget.NewMultiLineEntry()
	previewInfoLabel.Wrapping = fyne.TextWrapWord
	previewInfoLabel.SetText("Select a material to preview.")
	previewInfoLabel.Disable()
	previewPane := container.NewMax(previewInfoLabel)

	var table *widget.Table
	table = widget.NewTableWithHeaders(
		func() (int, int) { return len(state.displayMaterials), len(materialColumnHeaders) },
		func() fyne.CanvasObject {
			img := canvas.NewImageFromImage(nil)
			img.FillMode = canvas.ImageFillContain
			img.SetMinSize(fyne.NewSize(32, 32))
			label := widget.NewLabel("")
			return container.NewMax(label, img)
		},
		func(id widget.TableCellID, object fyne.CanvasObject) {
			if id.Row < 0 || id.Row >= len(state.displayMaterials) || id.Col < 0 || id.Col >= len(materialColumnHeaders) {
				return
			}
			cont := object.(*fyne.Container)
			label := cont.Objects[0].(*widget.Label)
			img := cont.Objects[1].(*canvas.Image)
			renderMaterialCell(state, state.displayMaterials[id.Row], materialColumnHeaders[id.Col], label, img)
		},
	)
	table.CreateHeader = func() fyne.CanvasObject { return widget.NewButton("", nil) }
	table.UpdateHeader = func(id widget.TableCellID, object fyne.CanvasObject) {
		button := object.(*widget.Button)
		if id.Row == -1 && id.Col >= 0 && id.Col < len(materialColumnHeaders) {
			name := materialColumnHeaders[id.Col]
			label := name
			if state.sortColumn == name {
				if state.sortDescending {
					label = name + " ▼"
				} else {
					label = name + " ▲"
				}
			}
			button.SetText(label)
			button.OnTapped = func() {
				if state.sortColumn == name {
					state.sortDescending = !state.sortDescending
				} else {
					state.sortColumn = name
					state.sortDescending = true
				}
				applyMaterialSortAndFilter(state)
				table.Refresh()
			}
			return
		}
		if id.Col == -1 && id.Row >= 0 {
			button.SetText(strconv.Itoa(id.Row + 1))
		} else {
			button.SetText("")
		}
		button.OnTapped = nil
	}
	applyMaterialColumnWidths(table)
	table.OnSelected = func(id widget.TableCellID) {
		if id.Row < 0 || id.Row >= len(state.displayMaterials) {
			return
		}
		state.selectedRow = id.Row
		updateMaterialPreviewPlaceholder(state, state.displayMaterials[id.Row], previewInfoLabel)
	}

	filterEntry.OnChanged = func(text string) {
		state.filterText = strings.TrimSpace(text)
		applyMaterialSortAndFilter(state)
		table.Refresh()
		countLabel.SetText(fmt.Sprintf("Showing %d of %d materials", len(state.displayMaterials), len(state.materials)))
	}

	var loadButton *widget.Button
	onLoadFinished := func(textureReport *renderdoc.Report, meshReport *renderdoc.MeshReport, materials []renderdoc.Material, loadedPath string, xmlPath string, store *renderdoc.BufferStore, loadErr error) {
		progressBar.Hide()
		if loadButton != nil {
			loadButton.Enable()
		}
		if loadErr != nil {
			pathLabel.SetText(fmt.Sprintf("Load failed: %s", loadedPath))
			fyneDialog.ShowError(loadErr, window)
			if store != nil {
				_ = store.Close()
				renderdoc.RemoveConvertedOutput(xmlPath)
			}
			return
		}
		if state.bufferStore != nil {
			_ = state.bufferStore.Close()
		}
		if state.xmlPath != "" {
			renderdoc.RemoveConvertedOutput(state.xmlPath)
		}
		state.materials = materials
		state.textureReport = textureReport
		state.meshReport = meshReport
		state.bufferStore = store
		state.xmlPath = xmlPath
		state.thumbnailCache = map[string]image.Image{}
		state.textureByID = map[string]renderdoc.TextureInfo{}
		for _, t := range textureReport.Textures {
			state.textureByID[t.ResourceID] = t
		}
		state.filterText = strings.TrimSpace(filterEntry.Text)
		state.selectedRow = -1
		applyMaterialSortAndFilter(state)
		pathLabel.SetText(fmt.Sprintf("Loaded: %s", loadedPath))
		summaryLabel.SetText(fmt.Sprintf("%d materials across %d draw calls", len(materials), countTotalDraws(materials)))
		countLabel.SetText(fmt.Sprintf("Showing %d of %d materials", len(state.displayMaterials), len(state.materials)))
		previewInfoLabel.SetText("Select a material to preview.")
		table.Refresh()
		if onLoaded != nil {
			onLoaded(loadedPath)
		}
	}

	loadFromPath := func(path string) {
		go loadMaterialsCaptureFromPath(window, progressBar, loadButton, path, onLoadFinished)
	}

	loadButton = widget.NewButton("Load .rdc…", func() {
		path, err := nativeDialog.File().Filter(rdcFileFilterLabel, "rdc").Title("Select RenderDoc capture (.rdc)").Load()
		if err != nil {
			if !errors.Is(err, nativeDialog.Cancelled) {
				fyneDialog.ShowError(err, window)
			}
			return
		}
		loadFromPath(path)
	})

	header := container.NewVBox(
		container.NewBorder(nil, nil, nil, loadButton, pathLabel),
		summaryLabel,
		progressBar,
		filterEntry,
	)
	footer := countLabel
	split := container.NewHSplit(table, previewPane)
	split.Offset = 0.7
	return container.NewBorder(header, footer, nil, nil, split), loadFromPath
}

func loadMaterialsCaptureFromPath(window fyne.Window, progressBar *widget.ProgressBarInfinite, loadButton *widget.Button, capturePath string, onFinished func(*renderdoc.Report, *renderdoc.MeshReport, []renderdoc.Material, string, string, *renderdoc.BufferStore, error)) {
	fyne.Do(func() {
		progressBar.Show()
		if loadButton != nil {
			loadButton.Disable()
		}
	})
	xmlPath, convertErr := renderdoc.ConvertToXML(capturePath)
	if convertErr != nil {
		fyne.Do(func() { onFinished(nil, nil, nil, capturePath, "", nil, convertErr) })
		return
	}
	textureReport, parseErr := renderdoc.ParseCaptureXMLFile(xmlPath)
	if parseErr != nil {
		fyne.Do(func() { onFinished(nil, nil, nil, capturePath, xmlPath, nil, parseErr) })
		return
	}
	meshReport, meshErr := renderdoc.ParseMeshReportFromXMLFile(xmlPath)
	if meshErr != nil {
		fyne.Do(func() { onFinished(nil, nil, nil, capturePath, xmlPath, nil, meshErr) })
		return
	}
	store, storeErr := renderdoc.OpenBufferStore(xmlPath)
	if storeErr != nil {
		fyne.Do(func() { onFinished(nil, nil, nil, capturePath, xmlPath, nil, storeErr) })
		return
	}
	renderdoc.ComputeTextureHashes(textureReport, store, nil)
	renderdoc.ApplyBuiltinHashes(textureReport, defaultRobloxPixelHashes)
	materials := renderdoc.BuildMaterialsWithMeshHashes(textureReport, meshReport, store)
	fyne.Do(func() { onFinished(textureReport, meshReport, materials, capturePath, xmlPath, store, nil) })
}

func renderMaterialCell(state *materialsTabState, mat renderdoc.Material, column string, label *widget.Label, img *canvas.Image) {
	label.Hide()
	img.Hide()
	switch column {
	case "Color":
		setMaterialThumbnail(state, mat.ColorTextureID, label, img)
	case "Normal":
		setMaterialThumbnail(state, mat.NormalTextureID, label, img)
	case "MR":
		setMaterialThumbnail(state, mat.MRTextureID, label, img)
	case "Color Hash":
		label.SetText(materialColorHash(state, mat))
		label.Show()
	case "Draws":
		label.SetText(strconv.Itoa(mat.DrawCallCount))
		label.Show()
	case "Meshes":
		label.SetText(strconv.Itoa(len(mat.MeshHashes)))
		label.Show()
	case "VRAM":
		label.SetText(format.FormatSizeAuto64(mat.TotalBytes))
		label.Show()
	}
}

func setMaterialThumbnail(state *materialsTabState, texID string, label *widget.Label, img *canvas.Image) {
	if texID == "" {
		label.SetText("—")
		label.Show()
		return
	}
	if cached, ok := state.thumbnailCache[texID]; ok && cached != nil {
		img.Image = cached
		img.Refresh()
		img.Show()
		return
	}
	tex, ok := state.textureByID[texID]
	if !ok || state.bufferStore == nil {
		label.SetText("?")
		label.Show()
		return
	}
	// Decode synchronously the first time this row is rendered.  Decoded
	// thumbnails are tiny and the table only renders visible rows; this
	// keeps the cache logic simple. If decoding turns out to stall scroll
	// on large captures, move to a background goroutine + Refresh.
	decoded, err := renderdoc.DecodeTexturePreview(tex, state.bufferStore)
	if err != nil || decoded == nil {
		label.SetText("?")
		label.Show()
		return
	}
	state.thumbnailCache[texID] = decoded
	img.Image = decoded
	img.Refresh()
	img.Show()
}

func materialColorHash(state *materialsTabState, mat renderdoc.Material) string {
	if mat.ColorTextureID == "" {
		return "—"
	}
	if tex, ok := state.textureByID[mat.ColorTextureID]; ok && tex.PixelHash != "" {
		return tex.PixelHash
	}
	return "—"
}

func updateMaterialPreviewPlaceholder(state *materialsTabState, mat renderdoc.Material, label *widget.Entry) {
	var b strings.Builder
	fmt.Fprintf(&b, "Color: %s\nNormal: %s\nMR: %s\n",
		nonEmptyOrDash(mat.ColorTextureID),
		nonEmptyOrDash(mat.NormalTextureID),
		nonEmptyOrDash(mat.MRTextureID))
	if len(mat.OtherTextureIDs) > 0 {
		fmt.Fprintf(&b, "Other: %s\n", strings.Join(mat.OtherTextureIDs, ", "))
	}
	fmt.Fprintf(&b, "\nDraws: %d  Meshes: %d  VRAM: %s\n",
		mat.DrawCallCount, len(mat.MeshHashes), format.FormatSizeAuto64(mat.TotalBytes))
	if len(mat.MeshHashes) > 0 {
		b.WriteString("\nMesh hashes:\n")
		for _, h := range mat.MeshHashes {
			if len(h) > 16 {
				h = h[:16]
			}
			b.WriteString(h + "\n")
		}
	}
	label.SetText(b.String())
}

func nonEmptyOrDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func countTotalDraws(materials []renderdoc.Material) int {
	total := 0
	for _, m := range materials {
		total += m.DrawCallCount
	}
	return total
}

func applyMaterialColumnWidths(table *widget.Table) {
	table.SetColumnWidth(0, 48) // Color thumb
	table.SetColumnWidth(1, 48) // Normal thumb
	table.SetColumnWidth(2, 48) // MR thumb
	table.SetColumnWidth(3, 140) // Color Hash
	table.SetColumnWidth(4, 60)  // Draws
	table.SetColumnWidth(5, 60)  // Meshes
	table.SetColumnWidth(6, 90)  // VRAM
}

func applyMaterialSortAndFilter(state *materialsTabState) {
	filter := strings.ToLower(state.filterText)
	display := state.materials[:0:0]
	for _, m := range state.materials {
		if filter != "" && !materialMatchesFilter(state, m, filter) {
			continue
		}
		display = append(display, m)
	}
	sortMaterials(display, state.sortColumn, state.sortDescending)
	state.displayMaterials = display
}

func materialMatchesFilter(state *materialsTabState, m renderdoc.Material, filter string) bool {
	candidates := []string{m.ColorTextureID, m.NormalTextureID, m.MRTextureID, materialColorHash(state, m)}
	candidates = append(candidates, m.OtherTextureIDs...)
	for _, c := range candidates {
		if strings.Contains(strings.ToLower(c), filter) {
			return true
		}
	}
	return false
}

func sortMaterials(out []renderdoc.Material, column string, descending bool) {
	cmp := func(i, j int) bool { return out[i].TotalBytes > out[j].TotalBytes }
	switch column {
	case "Draws":
		cmp = func(i, j int) bool { return out[i].DrawCallCount > out[j].DrawCallCount }
	case "Meshes":
		cmp = func(i, j int) bool { return len(out[i].MeshHashes) > len(out[j].MeshHashes) }
	case "VRAM":
		cmp = func(i, j int) bool { return out[i].TotalBytes > out[j].TotalBytes }
	}
	if !descending {
		original := cmp
		cmp = func(i, j int) bool { return original(j, i) }
	}
	sort.SliceStable(out, cmp)
}
```

Add `"sort"` to the imports list at the top of the file.

- [ ] **Step 2: Wire the sub-tab into `renderdoc_tab.go`**

Modify `internal/app/ui/tabs/renderdoc/renderdoc_tab.go` `NewRenderDocTab`:

Change the index constants:

```go
const (
	texturesIndex  = 0
	meshesIndex    = 1
	materialsIndex = 2
)
```

Add the materials view setup after the meshes view:

```go
materialsView, loadMaterialsFromPath := newMaterialsSubTab(window, func(path string) {
	if lc != nil {
		lc.setLoaded(materialsIndex, path)
	}
})
```

Append to `container.NewAppTabs(...)`:

```go
container.NewTabItem("Materials", materialsView),
```

Extend `loadIntoActiveSubTab`:

```go
case materialsIndex:
	loadMaterialsFromPath(path)
```

- [ ] **Step 3: Build the project**

Run: `go build ./...`
Expected: PASS, no errors.

- [ ] **Step 4: Run all tests**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 5: Smoke test (manual)**

Run: `go run ./cmd/joxblox`
Open the RenderDoc tab, click the new "Materials" sub-tab. With no capture loaded the tab should show "No capture loaded." Use the launcher to load a `.rdc`. The Materials table should populate with rows; each row should show Color/Normal/MR thumbnails (or "—" placeholders), draws/meshes counts, and VRAM. Note any visual issues.

- [ ] **Step 6: Commit**

```bash
git add internal/app/ui/tabs/renderdoc/materials_view.go internal/app/ui/tabs/renderdoc/renderdoc_tab.go
git commit -m "feat(renderdoc-tab): Materials sub-tab with thumbnail table"
```

---

## Task 6: Materials preview pane (large maps + mesh hash list)

**Files:**
- Modify: `internal/app/ui/tabs/renderdoc/materials_view.go`

**Why:** Replace the placeholder text-only preview with the real preview pane: three 256×256 map images side-by-side with labels, and the mesh-hash list.

- [ ] **Step 1: Replace the preview placeholder with a real layout**

In `materials_view.go`, replace the `previewPane := container.NewMax(previewInfoLabel)` line and the `updateMaterialPreviewPlaceholder` function with a richer preview built from three labelled image canvases.

Add a `materialPreview` struct above `newMaterialsSubTab`:

```go
type materialPreview struct {
	colorImg, normalImg, mrImg       *canvas.Image
	colorLabel, normalLabel, mrLabel *widget.Label
	infoEntry                        *widget.Entry
	container                        *fyne.Container
}

func newMaterialPreview() *materialPreview {
	mk := func(title string) (*canvas.Image, *widget.Label, *fyne.Container) {
		img := canvas.NewImageFromImage(nil)
		img.FillMode = canvas.ImageFillContain
		img.SetMinSize(fyne.NewSize(256, 256))
		lbl := widget.NewLabel(title)
		return img, lbl, container.NewBorder(lbl, nil, nil, nil, img)
	}
	colorImg, colorLabel, colorBox := mk("Color: —")
	normalImg, normalLabel, normalBox := mk("Normal: —")
	mrImg, mrLabel, mrBox := mk("MR: —")
	row := container.NewGridWithColumns(3, colorBox, normalBox, mrBox)
	infoEntry := widget.NewMultiLineEntry()
	infoEntry.Wrapping = fyne.TextWrapWord
	infoEntry.Disable()
	infoEntry.SetText("Select a material to preview.")
	return &materialPreview{
		colorImg: colorImg, normalImg: normalImg, mrImg: mrImg,
		colorLabel: colorLabel, normalLabel: normalLabel, mrLabel: mrLabel,
		infoEntry: infoEntry,
		container: container.NewBorder(row, nil, nil, nil, infoEntry),
	}
}

func (p *materialPreview) reset() {
	p.colorImg.Image = nil
	p.normalImg.Image = nil
	p.mrImg.Image = nil
	p.colorImg.Refresh()
	p.normalImg.Refresh()
	p.mrImg.Refresh()
	p.colorLabel.SetText("Color: —")
	p.normalLabel.SetText("Normal: —")
	p.mrLabel.SetText("MR: —")
	p.infoEntry.SetText("Select a material to preview.")
}
```

In `newMaterialsSubTab`, replace these lines:

```go
previewInfoLabel := widget.NewMultiLineEntry()
previewInfoLabel.Wrapping = fyne.TextWrapWord
previewInfoLabel.SetText("Select a material to preview.")
previewInfoLabel.Disable()
previewPane := container.NewMax(previewInfoLabel)
```

with:

```go
preview := newMaterialPreview()
previewPane := preview.container
```

Replace `previewInfoLabel` references throughout the function with their `preview.*` equivalents:

- In `OnSelected`, replace `updateMaterialPreviewPlaceholder(state, ..., previewInfoLabel)` with `updateMaterialPreview(state, state.displayMaterials[id.Row], preview)`.
- In `onLoadFinished`, replace `previewInfoLabel.SetText("Select a material to preview.")` with `preview.reset()`.

Add the new update function (replacing `updateMaterialPreviewPlaceholder`):

```go
func updateMaterialPreview(state *materialsTabState, mat renderdoc.Material, preview *materialPreview) {
	setPreviewMap(state, mat.ColorTextureID, preview.colorImg, preview.colorLabel, "Color")
	setPreviewMap(state, mat.NormalTextureID, preview.normalImg, preview.normalLabel, "Normal")
	setPreviewMap(state, mat.MRTextureID, preview.mrImg, preview.mrLabel, "MR")

	var b strings.Builder
	fmt.Fprintf(&b, "Draws: %d   Meshes: %d   VRAM: %s\n",
		mat.DrawCallCount, len(mat.MeshHashes), format.FormatSizeAuto64(mat.TotalBytes))
	if len(mat.OtherTextureIDs) > 0 {
		fmt.Fprintf(&b, "Other PS textures: %s\n", strings.Join(mat.OtherTextureIDs, ", "))
	}
	if len(mat.MeshHashes) > 0 {
		b.WriteString("\nMesh hashes (first 16 chars):\n")
		for _, h := range mat.MeshHashes {
			if len(h) > 16 {
				h = h[:16]
			}
			b.WriteString(h + "\n")
		}
	}
	preview.infoEntry.SetText(b.String())
}

func setPreviewMap(state *materialsTabState, texID string, img *canvas.Image, label *widget.Label, kind string) {
	if texID == "" {
		img.Image = nil
		img.Refresh()
		label.SetText(kind + ": —")
		return
	}
	label.SetText(kind + ": " + texID)
	if cached, ok := state.thumbnailCache[texID]; ok && cached != nil {
		img.Image = cached
		img.Refresh()
		return
	}
	tex, ok := state.textureByID[texID]
	if !ok || state.bufferStore == nil {
		img.Image = nil
		img.Refresh()
		return
	}
	decoded, err := renderdoc.DecodeTexturePreview(tex, state.bufferStore)
	if err != nil || decoded == nil {
		img.Image = nil
		img.Refresh()
		return
	}
	state.thumbnailCache[texID] = decoded
	img.Image = decoded
	img.Refresh()
}
```

Delete the old `updateMaterialPreviewPlaceholder` and `nonEmptyOrDash` functions (no longer used).

- [ ] **Step 2: Build**

Run: `go build ./...`
Expected: PASS.

- [ ] **Step 3: Smoke test**

Run: `go run ./cmd/joxblox`
Load a capture in the Materials tab. Click a row. Confirm the preview pane shows three 256×256 thumbnails labelled with the texture IDs (or "—" for missing maps) plus the info block with draws/meshes/VRAM and the mesh-hash list.

- [ ] **Step 4: Commit**

```bash
git add internal/app/ui/tabs/renderdoc/materials_view.go
git commit -m "feat(renderdoc-tab): materials preview with side-by-side maps"
```

---

## Task 7: Final verification

- [ ] **Step 1: Run the entire test suite**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 2: Run go vet**

Run: `go vet ./...`
Expected: no warnings.

- [ ] **Step 3: Smoke-test all three sub-tabs**

Run: `go run ./cmd/joxblox` and verify Textures, Meshes, and Materials sub-tabs all load the same capture without crashing or hanging. Switch between sub-tabs rapidly to confirm no shared-state issues.

- [ ] **Step 4: Update CHANGELOG**

Add an "Unreleased" / "Added" section entry to `CHANGELOG.md`:

```markdown
- `Materials` sub-tab in the RenderDoc tab — groups PS-bound textures into deduplicated PBR materials (Color + Normal + MR), with per-material draw counts, mesh usage, and VRAM totals
```

- [ ] **Step 5: Commit**

```bash
git add CHANGELOG.md
git commit -m "docs(changelog): note the new RenderDoc Materials sub-tab"
```
