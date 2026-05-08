package report

import (
	"fmt"
	"testing"
)

func slot(assetKey string, widthHeight int) SurfaceAppearanceMaterialSlot {
	return SurfaceAppearanceMaterialSlot{
		AssetKey:   assetKey,
		Width:      widthHeight,
		Height:     widthHeight,
		PixelCount: int64(widthHeight) * int64(widthHeight),
	}
}

func bc1Bytes(widthHeight int) int64 {
	return EstimateGPUTextureBytesExact(widthHeight, widthHeight, false)
}

func bc3Bytes(widthHeight int) int64 {
	return EstimateGPUTextureBytesExact(widthHeight, widthHeight, true)
}

func TestApplySurfaceAppearanceMemoryCorrections_SingleMaterial_NoMR_AddsOneBlankPack(t *testing.T) {
	summary := &Summary{}
	materials := map[string]SurfaceAppearanceMaterialSlots{
		"Workspace.Wall.SurfaceAppearance": {Color: slot("color-A", 512), Normal: slot("normal-A", 512)},
	}
	correction := ApplySurfaceAppearanceMemoryCorrections(summary, materials)
	if correction.BlankMRGroupCount != 1 || correction.CustomMRGroupCount != 0 {
		t.Errorf("groups = (blank=%d custom=%d), want (1, 0)", correction.BlankMRGroupCount, correction.CustomMRGroupCount)
	}
	if summary.BC1PixelCount != int64(512*512) {
		t.Errorf("BC1PixelCount = %d, want %d", summary.BC1PixelCount, 512*512)
	}
	if summary.BC1BytesExact != bc1Bytes(512) {
		t.Errorf("BC1BytesExact = %d, want %d", summary.BC1BytesExact, bc1Bytes(512))
	}
}

func TestApplySurfaceAppearanceMemoryCorrections_DedupesByNormalNotColor(t *testing.T) {
	// Two materials sharing one normal map but with distinct color maps.
	// Engine allocates one MR pack (keyed by normal), not two.
	summary := &Summary{}
	sharedNormal := slot("normal-shared", 1024)
	materials := map[string]SurfaceAppearanceMaterialSlots{
		"Workspace.A.SurfaceAppearance": {Color: slot("color-A", 1024), Normal: sharedNormal},
		"Workspace.B.SurfaceAppearance": {Color: slot("color-B", 1024), Normal: sharedNormal},
	}
	correction := ApplySurfaceAppearanceMemoryCorrections(summary, materials)
	if correction.BlankMRGroupCount != 1 {
		t.Errorf("BlankMRGroupCount = %d, want 1 (shared normal → one MR pack)", correction.BlankMRGroupCount)
	}
	if summary.BC1PixelCount != int64(1024*1024) {
		t.Errorf("BC1PixelCount = %d, want %d (one pack at normal's 1024²)", summary.BC1PixelCount, 1024*1024)
	}
}

func TestApplySurfaceAppearanceMemoryCorrections_DistinctNormalsSameColor_AllocatesPackPerNormal(t *testing.T) {
	// Two materials sharing one color map but with distinct normal maps.
	// Engine allocates two MR packs (one per unique normal).
	summary := &Summary{}
	sharedColor := slot("color-shared", 1024)
	materials := map[string]SurfaceAppearanceMaterialSlots{
		"Workspace.A.SurfaceAppearance": {Color: sharedColor, Normal: slot("normal-A", 1024)},
		"Workspace.B.SurfaceAppearance": {Color: sharedColor, Normal: slot("normal-B", 1024)},
	}
	correction := ApplySurfaceAppearanceMemoryCorrections(summary, materials)
	if correction.BlankMRGroupCount != 2 {
		t.Errorf("BlankMRGroupCount = %d, want 2 (distinct normals → two MR packs)", correction.BlankMRGroupCount)
	}
	if summary.BC1PixelCount != int64(2*1024*1024) {
		t.Errorf("BC1PixelCount = %d, want %d (two packs at 1024²)", summary.BC1PixelCount, 2*1024*1024)
	}
}

func TestApplySurfaceAppearanceMemoryCorrections_NoNormalFallsBackToColorAndDedupes(t *testing.T) {
	// SurfaceAppearance entries with no normal slot fall back to color
	// keying — three materials sharing one color map and no normal still
	// dedupe to one MR pack at color resolution.
	summary := &Summary{}
	colorA := slot("color-shared", 1024)
	materials := map[string]SurfaceAppearanceMaterialSlots{
		"Workspace.A.SurfaceAppearance": {Color: colorA},
		"Workspace.B.SurfaceAppearance": {Color: colorA},
		"Workspace.C.SurfaceAppearance": {Color: colorA},
	}
	correction := ApplySurfaceAppearanceMemoryCorrections(summary, materials)
	if correction.BlankMRGroupCount != 1 {
		t.Errorf("BlankMRGroupCount = %d, want 1 (shared color → one MR pack)", correction.BlankMRGroupCount)
	}
	if summary.BC1PixelCount != int64(1024*1024) {
		t.Errorf("BC1PixelCount = %d, want %d (one pack at 1024²)", summary.BC1PixelCount, 1024*1024)
	}
}

func TestApplySurfaceAppearanceMemoryCorrections_AnyMOrR_MarksGroupAsCustomAndSubtractsStandalone(t *testing.T) {
	// Pre-correction tally has the R asset already counted as a standalone BC1.
	summary := &Summary{
		BC1PixelCount: int64(1024 * 1024),
		BC1BytesExact: bc1Bytes(1024),
	}
	materials := map[string]SurfaceAppearanceMaterialSlots{
		"Workspace.Wall.SurfaceAppearance": {
			Color:     slot("color-A", 1024),
			Roughness: slot("rough-shared", 1024),
		},
	}
	correction := ApplySurfaceAppearanceMemoryCorrections(summary, materials)
	if correction.CustomMRGroupCount != 1 || correction.BlankMRGroupCount != 0 {
		t.Errorf("groups = (custom=%d blank=%d), want (1, 0)", correction.CustomMRGroupCount, correction.BlankMRGroupCount)
	}
	// +1 MR pack at 1024², -1 standalone R BC1 at 1024². Net 0.
	if summary.BC1PixelCount != int64(1024*1024) {
		t.Errorf("BC1PixelCount = %d, want %d (+pack − standaloneR)", summary.BC1PixelCount, 1024*1024)
	}
	if correction.AddedMRPackBytes != bc1Bytes(1024) || correction.SubtractedStandaloneBytes != bc1Bytes(1024) {
		t.Errorf("added=%d subtracted=%d, want both = %d", correction.AddedMRPackBytes, correction.SubtractedStandaloneBytes, bc1Bytes(1024))
	}
}

func TestApplySurfaceAppearanceMemoryCorrections_StandaloneRSharedAcrossMaterials_SubtractsOnce(t *testing.T) {
	// One R asset shared across two materials, two distinct color maps.
	summary := &Summary{
		BC1PixelCount: int64(1024 * 1024),
		BC1BytesExact: bc1Bytes(1024),
	}
	rough := slot("rough-shared", 1024)
	materials := map[string]SurfaceAppearanceMaterialSlots{
		"Workspace.A.SurfaceAppearance": {Color: slot("color-A", 1024), Roughness: rough},
		"Workspace.B.SurfaceAppearance": {Color: slot("color-B", 1024), Roughness: rough},
	}
	correction := ApplySurfaceAppearanceMemoryCorrections(summary, materials)
	// Two unique color maps → 2 packs added (each at 1024²).
	// One unique R asset → 1 standalone subtracted.
	wantPixels := int64(1024*1024) + 2*int64(1024*1024) - 1*int64(1024*1024)
	if summary.BC1PixelCount != wantPixels {
		t.Errorf("BC1PixelCount = %d, want %d", summary.BC1PixelCount, wantPixels)
	}
	if correction.CustomMRGroupCount != 2 {
		t.Errorf("CustomMRGroupCount = %d, want 2", correction.CustomMRGroupCount)
	}
}

func TestApplySurfaceAppearanceMemoryCorrections_NormalOnlyKeyedAtNormalResolution(t *testing.T) {
	summary := &Summary{}
	materials := map[string]SurfaceAppearanceMaterialSlots{
		"Workspace.Wall.SurfaceAppearance": {Normal: slot("normal-A", 256)},
	}
	correction := ApplySurfaceAppearanceMemoryCorrections(summary, materials)
	if correction.BlankMRGroupCount != 1 {
		t.Errorf("BlankMRGroupCount = %d, want 1", correction.BlankMRGroupCount)
	}
	if summary.BC1PixelCount != int64(256*256) {
		t.Errorf("BC1PixelCount = %d, want %d", summary.BC1PixelCount, 256*256)
	}
}

func TestApplySurfaceAppearanceMemoryCorrections_MaterialWithNoColorOrNormal_Skipped(t *testing.T) {
	// A material with neither Color nor Normal authored has no MR pack (the
	// engine needs a key map to size the pack against). Its M/R assets are
	// then real standalone BC1 uploads, so we leave the tally unchanged.
	summary := &Summary{BC1PixelCount: 4242}
	materials := map[string]SurfaceAppearanceMaterialSlots{
		"Workspace.Wall.SurfaceAppearance": {Metalness: slot("m-A", 512)},
	}
	ApplySurfaceAppearanceMemoryCorrections(summary, materials)
	if summary.BC1PixelCount != 4242 {
		t.Errorf("BC1PixelCount = %d, want 4242 (material with no color/normal is skipped)", summary.BC1PixelCount)
	}
}

func TestApplySurfaceAppearanceMemoryCorrections_UnderflowClampsToZero(t *testing.T) {
	summary := &Summary{BC1PixelCount: 100}
	materials := map[string]SurfaceAppearanceMaterialSlots{
		"Workspace.Wall.SurfaceAppearance": {
			Color:     slot("color-A", 256),
			Metalness: slot("m-A", 1024),
			Roughness: slot("r-A", 1024),
		},
	}
	ApplySurfaceAppearanceMemoryCorrections(summary, materials)
	if summary.BC1PixelCount != 0 {
		t.Errorf("BC1PixelCount = %d, want 0 (clamped from negative)", summary.BC1PixelCount)
	}
}

func TestApplySurfaceAppearanceMemoryCorrections_NilSummarySafe(t *testing.T) {
	materials := map[string]SurfaceAppearanceMaterialSlots{
		"x": {Color: slot("color-A", 100)},
	}
	ApplySurfaceAppearanceMemoryCorrections(nil, materials)
}

func TestApplySurfaceAppearanceMemoryCorrections_EmptyMaterialsNoOp(t *testing.T) {
	summary := &Summary{BC1PixelCount: 4242}
	ApplySurfaceAppearanceMemoryCorrections(summary, nil)
	if summary.BC1PixelCount != 4242 {
		t.Errorf("BC1PixelCount = %d, want 4242 (no materials → no change)", summary.BC1PixelCount)
	}
}

func TestApplySurfaceAppearanceMemoryCorrections_RoughnessLargerThanBase_MRPackSizesToRoughness(t *testing.T) {
	// Color=512, Normal=512, Roughness=2048 → engine sizes the MR pack
	// to fit the 2048² roughness.
	summary := &Summary{
		BC1PixelCount: int64(2048 * 2048),
		BC1BytesExact: bc1Bytes(2048),
	}
	materials := map[string]SurfaceAppearanceMaterialSlots{
		"Workspace.Wall.SurfaceAppearance": {
			Color:     slot("color-A", 512),
			Normal:    slot("normal-A", 512),
			Roughness: slot("r-A", 2048),
		},
	}
	correction := ApplySurfaceAppearanceMemoryCorrections(summary, materials)
	wantPackBytes := bc1Bytes(2048)
	if correction.AddedMRPackBytes != wantPackBytes {
		t.Errorf("AddedMRPackBytes = %d, want %d (sized to roughness)", correction.AddedMRPackBytes, wantPackBytes)
	}
	// MR pack = +2048², standalone R = -2048² → BC1 unchanged.
	if summary.BC1BytesExact != bc1Bytes(2048) {
		t.Errorf("BC1BytesExact = %d, want %d (pack added, R subtracted, net 0)", summary.BC1BytesExact, bc1Bytes(2048))
	}
}

func TestApplySurfaceAppearanceMemoryCorrections_LargestMOrRAcrossSharedNormal_DriversPackSize(t *testing.T) {
	// Two materials sharing a normal; one has a 1024² M, the other has
	// a 2048² R. The MR pack covers the largest, so 2048².
	summary := &Summary{}
	sharedNormal := slot("normal-shared", 512)
	materials := map[string]SurfaceAppearanceMaterialSlots{
		"Workspace.A.SurfaceAppearance": {Normal: sharedNormal, Metalness: slot("m-A", 1024)},
		"Workspace.B.SurfaceAppearance": {Normal: sharedNormal, Roughness: slot("r-B", 2048)},
	}
	correction := ApplySurfaceAppearanceMemoryCorrections(summary, materials)
	wantPackBytes := bc1Bytes(2048)
	if correction.AddedMRPackBytes != wantPackBytes {
		t.Errorf("AddedMRPackBytes = %d, want %d (sized to largest authored across group)", correction.AddedMRPackBytes, wantPackBytes)
	}
}

func TestApplySurfaceAppearanceMemoryCorrections_NormalSmallerThanColor_AddsBC3UpscaleDelta(t *testing.T) {
	// Engine upscales the 512² normal to match its 1024² color, so BC3
	// bytes should reflect the 1024² cost, not the 512² source.
	rawBC3At512 := bc3Bytes(512)
	summary := &Summary{
		BC3PixelCount: 512 * 512,
		BC3BytesExact: rawBC3At512,
	}
	materials := map[string]SurfaceAppearanceMaterialSlots{
		"Workspace.Wall.SurfaceAppearance": {
			Color:  slot("color-A", 1024),
			Normal: slot("normal-A", 512),
		},
	}
	correction := ApplySurfaceAppearanceMemoryCorrections(summary, materials)
	if correction.UpscaledNormalCount != 1 {
		t.Errorf("UpscaledNormalCount = %d, want 1", correction.UpscaledNormalCount)
	}
	wantBC3Bytes := bc3Bytes(1024)
	if summary.BC3BytesExact != wantBC3Bytes {
		t.Errorf("BC3BytesExact = %d, want %d (upscaled to color size)", summary.BC3BytesExact, wantBC3Bytes)
	}
	if summary.BC3PixelCount != int64(1024*1024) {
		t.Errorf("BC3PixelCount = %d, want %d", summary.BC3PixelCount, 1024*1024)
	}
}

func TestApplySurfaceAppearanceMemoryCorrections_NormalLargerThanColor_NoUpscale(t *testing.T) {
	// Color is smaller than the normal; engine does NOT downscale the
	// normal, and color stays at its source size — no correction.
	rawBC3At1024 := bc3Bytes(1024)
	summary := &Summary{
		BC3PixelCount: 1024 * 1024,
		BC3BytesExact: rawBC3At1024,
	}
	materials := map[string]SurfaceAppearanceMaterialSlots{
		"Workspace.Wall.SurfaceAppearance": {
			Color:  slot("color-A", 512),
			Normal: slot("normal-A", 1024),
		},
	}
	correction := ApplySurfaceAppearanceMemoryCorrections(summary, materials)
	if correction.UpscaledNormalCount != 0 {
		t.Errorf("UpscaledNormalCount = %d, want 0", correction.UpscaledNormalCount)
	}
	if summary.BC3BytesExact != rawBC3At1024 {
		t.Errorf("BC3BytesExact = %d, want %d (unchanged)", summary.BC3BytesExact, rawBC3At1024)
	}
}

func TestApplySurfaceAppearanceMemoryCorrections_SharedNormalUpscalesToLargestPairedColor(t *testing.T) {
	// One normal asset shared across two materials with different color
	// sizes; engine upscales the normal to the LARGEST paired color.
	rawBC3At512 := bc3Bytes(512)
	summary := &Summary{
		BC3PixelCount: 512 * 512,
		BC3BytesExact: rawBC3At512,
	}
	sharedNormal := slot("normal-shared", 512)
	materials := map[string]SurfaceAppearanceMaterialSlots{
		"Workspace.A.SurfaceAppearance": {Color: slot("color-A", 1024), Normal: sharedNormal},
		"Workspace.B.SurfaceAppearance": {Color: slot("color-B", 2048), Normal: sharedNormal},
	}
	correction := ApplySurfaceAppearanceMemoryCorrections(summary, materials)
	if correction.UpscaledNormalCount != 1 {
		t.Errorf("UpscaledNormalCount = %d, want 1", correction.UpscaledNormalCount)
	}
	wantBC3Bytes := bc3Bytes(2048)
	if summary.BC3BytesExact != wantBC3Bytes {
		t.Errorf("BC3BytesExact = %d, want %d (upscaled to largest color 2048²)", summary.BC3BytesExact, wantBC3Bytes)
	}
}

func TestCountMismatchedPBRMaterials(t *testing.T) {
	tests := []struct {
		name             string
		materials        map[string]SurfaceAppearanceMaterialSlots
		wantMismatched   int
		wantTotal        int
	}{
		{
			name:           "all matching sizes",
			materials:      map[string]SurfaceAppearanceMaterialSlots{"A": {Color: slot("c", 1024), Normal: slot("n", 1024), Roughness: slot("r", 1024)}},
			wantMismatched: 0,
			wantTotal:      1,
		},
		{
			name:           "color and normal differ",
			materials:      map[string]SurfaceAppearanceMaterialSlots{"A": {Color: slot("c", 2048), Normal: slot("n", 512)}},
			wantMismatched: 1,
			wantTotal:      1,
		},
		{
			name:           "single slot can't mismatch",
			materials:      map[string]SurfaceAppearanceMaterialSlots{"A": {Color: slot("c", 1024)}},
			wantMismatched: 0,
			wantTotal:      1,
		},
		{
			name:           "empty slots ignored, two-slot mismatch counts",
			materials:      map[string]SurfaceAppearanceMaterialSlots{"A": {Color: slot("c", 1024), Roughness: slot("r", 512)}},
			wantMismatched: 1,
			wantTotal:      1,
		},
		{
			name: "mixed population",
			materials: map[string]SurfaceAppearanceMaterialSlots{
				"A": {Color: slot("c1", 1024), Normal: slot("n1", 1024)},
				"B": {Color: slot("c2", 2048), Normal: slot("n2", 512)},
				"C": {Color: slot("c3", 512)},
				"D": {},
			},
			wantMismatched: 1,
			wantTotal:      3,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotMismatched, gotTotal := CountMismatchedPBRMaterials(tt.materials)
			if gotMismatched != tt.wantMismatched || gotTotal != tt.wantTotal {
				t.Errorf("CountMismatchedPBRMaterials() = (%d, %d), want (%d, %d)", gotMismatched, gotTotal, tt.wantMismatched, tt.wantTotal)
			}
		})
	}
}

func TestCountMismatchedPBRMaterials_DedupesByAssetCombo(t *testing.T) {
	// 50 SurfaceAppearance instances all referencing the same (color, normal)
	// asset bundle should count as 1 unique mismatched combo + 1 unique
	// authored combo, not 50 of each. A different bundle on a separate
	// instance adds 1 more to each.
	materials := map[string]SurfaceAppearanceMaterialSlots{}
	for i := 0; i < 50; i++ {
		materials[fmt.Sprintf("Workspace.Wall%02d", i)] = SurfaceAppearanceMaterialSlots{
			Color:  slot("color-A", 256),
			Normal: slot("normal-A", 512),
		}
	}
	materials["Workspace.Roof"] = SurfaceAppearanceMaterialSlots{
		Color:  slot("color-B", 256),
		Normal: slot("normal-B", 1024),
	}
	mismatched, total := CountMismatchedPBRMaterials(materials)
	if mismatched != 2 || total != 2 {
		t.Errorf("CountMismatchedPBRMaterials() = (%d, %d), want (2, 2) — deduped by asset combo", mismatched, total)
	}
}

// rectSlot mirrors `slot` but allows distinct width/height to verify the
// comparison treats (W,H) as a pair, not just total pixel count.
func TestCountMismatchedPBRMaterials_NonSquareDimensions(t *testing.T) {
	rect := func(key string, w, h int) SurfaceAppearanceMaterialSlot {
		return SurfaceAppearanceMaterialSlot{AssetKey: key, Width: w, Height: h, PixelCount: int64(w) * int64(h)}
	}
	materials := map[string]SurfaceAppearanceMaterialSlots{
		"A": {Color: rect("c", 1024, 512), Normal: rect("n", 512, 1024)},
	}
	mismatched, total := CountMismatchedPBRMaterials(materials)
	if mismatched != 1 || total != 1 {
		t.Errorf("got (%d, %d), want (1, 1) — same pixel count but different (W,H) should mismatch", mismatched, total)
	}
}

func TestComputeMismatchedPBRWastedBytes(t *testing.T) {
	// Engine model: each material allocates a BC3 normal (upscaled to its
	// paired color when smaller) plus a BC1 MR pack sized to max(normal,
	// metalness, roughness). Savings = engineBytes(orig) − engineBytes(clamped).
	tests := []struct {
		name      string
		materials map[string]SurfaceAppearanceMaterialSlots
		wantBytes int64
	}{
		{
			name:      "all matching: no waste",
			materials: map[string]SurfaceAppearanceMaterialSlots{"A": {Color: slot("c", 1024), Normal: slot("n", 1024)}},
			wantBytes: 0,
		},
		{
			name: "color is biggest: no waste",
			// orig: normal upscaled 1024→2048 BC3 + MR@1024 BC1
			// clamped: nothing changes (nothing larger than color)
			materials: map[string]SurfaceAppearanceMaterialSlots{"A": {Color: slot("c", 2048), Normal: slot("n", 1024)}},
			wantBytes: 0,
		},
		{
			name: "normal larger than color: BC3 normal + BC1 MR pack saved",
			// orig: normal@1024 BC3 + MR@1024 BC1
			// clamped: normal@512 BC3 + MR@512 BC1
			materials: map[string]SurfaceAppearanceMaterialSlots{"A": {Color: slot("c", 512), Normal: slot("n", 1024)}},
			wantBytes: (bc3Bytes(1024) - bc3Bytes(512)) + (bc1Bytes(1024) - bc1Bytes(512)),
		},
		{
			name: "metalness larger than color (no normal): BC1 MR pack saved",
			// orig: no normal → MR keyed off color@512, sized to max(512, M=1024) = 1024 BC1
			// clamped: MR@max(512, 512) = 512 BC1
			materials: map[string]SurfaceAppearanceMaterialSlots{"A": {Color: slot("c", 512), Metalness: slot("m", 1024)}},
			wantBytes: bc1Bytes(1024) - bc1Bytes(512),
		},
		{
			name: "256 color, 512 normal, 4x4 roughness — 4x4 doesn't undo savings",
			// orig: normal@512 BC3 + MR@max(512, 4) = 512 BC1
			// clamped: normal@256 BC3 + MR@max(256, 4) = 256 BC1
			materials: map[string]SurfaceAppearanceMaterialSlots{"A": {Color: slot("c", 256), Normal: slot("n", 512), Roughness: slot("r", 4)}},
			wantBytes: (bc3Bytes(512) - bc3Bytes(256)) + (bc1Bytes(512) - bc1Bytes(256)),
		},
		{
			name: "256 color + 4x4 metalness: mismatched, but engine model gives 0 waste",
			// 4x4 M sits inside the MR pack at color size either way:
			// orig MR @ max(256, 4) = 256, clamped MR @ max(256, 4) = 256.
			materials: map[string]SurfaceAppearanceMaterialSlots{"A": {Color: slot("c", 256), Metalness: slot("m", 4)}},
			wantBytes: 0,
		},
		{
			name:      "no color authored: skipped",
			materials: map[string]SurfaceAppearanceMaterialSlots{"A": {Normal: slot("n", 512), Metalness: slot("m", 1024)}},
			wantBytes: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ComputeMismatchedPBRWastedBytes(tt.materials)
			if got != tt.wantBytes {
				t.Errorf("ComputeMismatchedPBRWastedBytes() = %d, want %d", got, tt.wantBytes)
			}
		})
	}
}

func TestTotalEngineSurfaceAppearanceVariableBytes(t *testing.T) {
	// Sanity-check the engine model: BC3 normal per unique normal asset
	// (upscaled to largest paired color if smaller) + BC1 MR pack per
	// unique normal-or-color group.
	tests := []struct {
		name      string
		materials map[string]SurfaceAppearanceMaterialSlots
		want      int64
	}{
		{
			name:      "single material, color + normal same size",
			materials: map[string]SurfaceAppearanceMaterialSlots{"A": {Color: slot("c", 512), Normal: slot("n", 512)}},
			want:      bc3Bytes(512) + bc1Bytes(512),
		},
		{
			name:      "normal smaller than color: normal upscales to color, MR sized to normal source",
			materials: map[string]SurfaceAppearanceMaterialSlots{"A": {Color: slot("c", 1024), Normal: slot("n", 512)}},
			want:      bc3Bytes(1024) + bc1Bytes(512),
		},
		{
			name:      "MR pack sized to largest authored slot",
			materials: map[string]SurfaceAppearanceMaterialSlots{"A": {Color: slot("c", 512), Normal: slot("n", 512), Roughness: slot("r", 2048)}},
			want:      bc3Bytes(512) + bc1Bytes(2048),
		},
		{
			name:      "no normal: MR keyed off color",
			materials: map[string]SurfaceAppearanceMaterialSlots{"A": {Color: slot("c", 256), Metalness: slot("m", 1024)}},
			want:      bc1Bytes(1024),
		},
		{
			name: "shared normal across two materials counts once",
			materials: map[string]SurfaceAppearanceMaterialSlots{
				"A": {Color: slot("c1", 256), Normal: slot("n-shared", 512)},
				"B": {Color: slot("c2", 256), Normal: slot("n-shared", 512)},
			},
			// One BC3 normal at max(512, max paired color=256)=512, one BC1 MR pack at 512.
			want: bc3Bytes(512) + bc1Bytes(512),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TotalEngineSurfaceAppearanceVariableBytes(tt.materials)
			if got != tt.want {
				t.Errorf("TotalEngineSurfaceAppearanceVariableBytes() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestCollectMismatchedPBRMaterials_DedupesByAssetCombo(t *testing.T) {
	// 50 mismatched SurfaceAppearance instances all reference the same
	// 512² normal bundle. One extra instance has a 512² normal too (so
	// equal per-combo waste) — it should sort BELOW the 50-instance combo
	// since count is the secondary sort key after waste.
	materials := map[string]SurfaceAppearanceMaterialSlots{}
	for i := 0; i < 50; i++ {
		materials[fmt.Sprintf("Workspace.Wall%02d.SurfaceAppearance", i)] = SurfaceAppearanceMaterialSlots{
			Color:  slot("color-A", 256),
			Normal: slot("normal-A", 512),
		}
	}
	materials["Workspace.Roof.SurfaceAppearance"] = SurfaceAppearanceMaterialSlots{
		Color:  slot("color-B", 256),
		Normal: slot("normal-B", 512),
	}
	got := CollectMismatchedPBRMaterials(materials)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2 (deduped by asset combo)", len(got))
	}
	if got[0].InstanceCount != 50 {
		t.Errorf("got[0].InstanceCount = %d, want 50 (same waste, higher count first)", got[0].InstanceCount)
	}
	if got[1].InstanceCount != 1 {
		t.Errorf("got[1].InstanceCount = %d, want 1", got[1].InstanceCount)
	}
	// Lex-min representative path:
	wantPath := "Workspace.Wall00.SurfaceAppearance"
	if got[0].InstancePath != wantPath {
		t.Errorf("got[0].InstancePath = %q, want %q", got[0].InstancePath, wantPath)
	}
}

func TestCollectMismatchedPBRMaterials_DistinctSizesSameAssets_ShouldNotHappenButGroupSafe(t *testing.T) {
	// Two materials referencing the same color asset key, both mismatched.
	// They should collapse since the asset bundle is identical.
	materials := map[string]SurfaceAppearanceMaterialSlots{
		"A": {Color: slot("c", 256), Normal: slot("n", 512)},
		"B": {Color: slot("c", 256), Normal: slot("n", 512)},
	}
	got := CollectMismatchedPBRMaterials(materials)
	if len(got) != 1 || got[0].InstanceCount != 2 {
		t.Errorf("got %d entries (count=%d), want 1 entry with count=2", len(got), got[0].InstanceCount)
	}
}

func TestCollectMismatchedPBRMaterials_PerComboWasteAndSort(t *testing.T) {
	// Three combos with progressively bigger waste. Highest waste should
	// sort first regardless of instance count or path.
	materials := map[string]SurfaceAppearanceMaterialSlots{
		"Workspace.Small.SurfaceAppearance":  {Color: slot("c-s", 256), Normal: slot("n-s", 512)},
		"Workspace.Medium.SurfaceAppearance": {Color: slot("c-m", 256), Normal: slot("n-m", 1024)},
		"Workspace.Large.SurfaceAppearance":  {Color: slot("c-l", 256), Normal: slot("n-l", 2048)},
	}
	got := CollectMismatchedPBRMaterials(materials)
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	// Engine bytes: BC3 normal + BC1 MR pack, both at the larger of normal-source
	// vs paired-color. For each combo, clamping drops to color=256.
	wantSmall := (bc3Bytes(512) - bc3Bytes(256)) + (bc1Bytes(512) - bc1Bytes(256))
	wantMedium := (bc3Bytes(1024) - bc3Bytes(256)) + (bc1Bytes(1024) - bc1Bytes(256))
	wantLarge := (bc3Bytes(2048) - bc3Bytes(256)) + (bc1Bytes(2048) - bc1Bytes(256))
	if got[0].WastedBytes != wantLarge {
		t.Errorf("got[0].WastedBytes = %d, want %d (largest combo first)", got[0].WastedBytes, wantLarge)
	}
	if got[1].WastedBytes != wantMedium {
		t.Errorf("got[1].WastedBytes = %d, want %d", got[1].WastedBytes, wantMedium)
	}
	if got[2].WastedBytes != wantSmall {
		t.Errorf("got[2].WastedBytes = %d, want %d", got[2].WastedBytes, wantSmall)
	}
}

func TestComputeMismatchedPBRWastedBytes_DedupesSharedAssets(t *testing.T) {
	// One 512² normal asset shared across 50 mismatched materials (color=256
	// each). The "downscale to color" saving should count once, not 50x.
	materials := map[string]SurfaceAppearanceMaterialSlots{}
	for i := 0; i < 50; i++ {
		materials[fmt.Sprintf("Workspace.Mat%02d", i)] = SurfaceAppearanceMaterialSlots{
			Color:  slot(fmt.Sprintf("color-%02d", i), 256),
			Normal: slot("normal-shared", 512),
		}
	}
	got := ComputeMismatchedPBRWastedBytes(materials)
	// Engine bytes:
	//   current   = 1 × BC3@512 (shared normal) + 1 × BC1@512 (shared MR pack)
	//   clamped   = 1 × BC3@256 + 1 × BC1@256 (every material clamps the shared normal to 256)
	want := (bc3Bytes(512) - bc3Bytes(256)) + (bc1Bytes(512) - bc1Bytes(256))
	if got != want {
		t.Errorf("ComputeMismatchedPBRWastedBytes() = %d, want %d (dedup'd, not multiplied by 50)", got, want)
	}
}

func TestComputeMismatchedPBRWastedBytes_SharedAssetCantDownscaleIfAnyMaterialNeedsItBig(t *testing.T) {
	// Normal "n-shared" at 512² is paired with both a 256² color (mismatched,
	// could downscale) and a 512² color (matched, must stay at 512). The
	// engine can only allocate the asset at one size, so in practice we
	// can't downscale → expect 0 savings.
	materials := map[string]SurfaceAppearanceMaterialSlots{
		"A-mismatched": {Color: slot("c1", 256), Normal: slot("n-shared", 512)},
		"B-matched":    {Color: slot("c2", 512), Normal: slot("n-shared", 512)},
	}
	got := ComputeMismatchedPBRWastedBytes(materials)
	if got != 0 {
		t.Errorf("ComputeMismatchedPBRWastedBytes() = %d, want 0 (shared asset blocks downscale)", got)
	}
}

func TestComputeSurfaceAppearanceMemoryCorrection_NetDeltas(t *testing.T) {
	materials := map[string]SurfaceAppearanceMaterialSlots{
		"A": {Color: slot("color-A", 1024), Roughness: slot("rough-shared", 512)},
		"B": {Color: slot("color-A", 1024), Roughness: slot("rough-shared", 512)},
	}
	delta := ComputeSurfaceAppearanceMemoryCorrection(materials)
	// 1 unique color map → 1 pack at 1024², 1 unique R → 1 subtract at 512².
	if delta.NetBC1Pixels() != int64(1024*1024)-int64(512*512) {
		t.Errorf("NetBC1Pixels = %d, want %d", delta.NetBC1Pixels(), int64(1024*1024)-int64(512*512))
	}
	if delta.NetBC1Bytes() != bc1Bytes(1024)-bc1Bytes(512) {
		t.Errorf("NetBC1Bytes = %d, want %d", delta.NetBC1Bytes(), bc1Bytes(1024)-bc1Bytes(512))
	}
}
