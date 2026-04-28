package report

import "testing"

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
