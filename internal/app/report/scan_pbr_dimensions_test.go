package report

import (
	"testing"

	"joxblox/internal/app/loader"
)

func saTextureRow(propertyName string, assetID int64, instancePath string, widthHeight int) loader.ScanResult {
	return loader.ScanResult{
		AssetID:      assetID,
		InstanceType: "SurfaceAppearance",
		InstancePath: instancePath,
		PropertyName: propertyName,
		Width:        widthHeight,
		Height:       widthHeight,
		PixelCount:   int64(widthHeight) * int64(widthHeight),
	}
}

func TestCollectScanMaterialEntries_NormalUpscalesToPairedColor(t *testing.T) {
	rows := []loader.ScanResult{
		saTextureRow("ColorMapContent", 1001, "Workspace.A.SurfaceAppearance", 1024),
		saTextureRow("NormalMapContent", 2001, "Workspace.A.SurfaceAppearance", 256),
	}
	entries := CollectScanMaterialEntries(rows)
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	got := entries[0]
	if got.NormalWidth != 256 || got.NormalHeight != 256 {
		t.Errorf("authored normal dims = %dx%d, want 256x256", got.NormalWidth, got.NormalHeight)
	}
	if got.EffectiveNormalWidth != 1024 || got.EffectiveNormalHeight != 1024 {
		t.Errorf("effective normal = %dx%d, want 1024x1024 (upscaled to paired color)",
			got.EffectiveNormalWidth, got.EffectiveNormalHeight)
	}
	if !got.Mismatched {
		t.Errorf("expected mismatched=true for 1024 color + 256 normal")
	}
}

func TestCollectScanMaterialEntries_MRPackTakesMaxAcrossSlots(t *testing.T) {
	rows := []loader.ScanResult{
		saTextureRow("ColorMapContent", 1001, "Workspace.A.SurfaceAppearance", 512),
		saTextureRow("NormalMapContent", 2001, "Workspace.A.SurfaceAppearance", 512),
		saTextureRow("MetalnessMapContent", 3001, "Workspace.A.SurfaceAppearance", 256),
		saTextureRow("RoughnessMapContent", 4001, "Workspace.A.SurfaceAppearance", 2048),
	}
	entries := CollectScanMaterialEntries(rows)
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	got := entries[0]
	if got.MRPackWidth != 2048 || got.MRPackHeight != 2048 {
		t.Errorf("MR pack = %dx%d, want 2048x2048 (max of normal/M/R)", got.MRPackWidth, got.MRPackHeight)
	}
	if got.MRPackBytes <= 0 {
		t.Errorf("MR pack BC1 bytes should be positive, got %d", got.MRPackBytes)
	}
}

func TestCollectScanMaterialEntries_BlankMRGroupAddsPackBytes(t *testing.T) {
	// SA with normal but no M/R authored: engine still allocates a BC1 MR
	// pack at normal size.
	rows := []loader.ScanResult{
		saTextureRow("ColorMapContent", 1001, "Workspace.A.SurfaceAppearance", 1024),
		saTextureRow("NormalMapContent", 2001, "Workspace.A.SurfaceAppearance", 1024),
	}
	entries := CollectScanMaterialEntries(rows)
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	got := entries[0]
	if got.MRPackWidth != 1024 || got.MRPackHeight != 1024 {
		t.Errorf("blank-MR pack = %dx%d, want 1024x1024", got.MRPackWidth, got.MRPackHeight)
	}
	wantPackBytes := EstimateGPUTextureBytesExact(1024, 1024, false)
	if got.MRPackBytes != wantPackBytes {
		t.Errorf("MR pack bytes = %d, want %d", got.MRPackBytes, wantPackBytes)
	}
}

func TestCollectScanMaterialEntries_DedupesByAssetCombo(t *testing.T) {
	// Two SurfaceAppearances using the same color/normal asset bundle:
	// should collapse to one entry with InstanceCount=2.
	rows := []loader.ScanResult{
		saTextureRow("ColorMapContent", 1001, "Workspace.A.SurfaceAppearance", 1024),
		saTextureRow("NormalMapContent", 2001, "Workspace.A.SurfaceAppearance", 1024),
		saTextureRow("ColorMapContent", 1001, "Workspace.B.SurfaceAppearance", 1024),
		saTextureRow("NormalMapContent", 2001, "Workspace.B.SurfaceAppearance", 1024),
	}
	entries := CollectScanMaterialEntries(rows)
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1 (same bundle)", len(entries))
	}
	if entries[0].InstanceCount != 2 {
		t.Errorf("InstanceCount = %d, want 2", entries[0].InstanceCount)
	}
	if entries[0].InstancePath != "Workspace.A.SurfaceAppearance" {
		t.Errorf("InstancePath = %q, want lex-min Workspace.A.SurfaceAppearance", entries[0].InstancePath)
	}
}

func TestTotalScanMaterialGPUBytes_MatchesEngineModel(t *testing.T) {
	// One SA with 1024 color + 1024 normal + no M/R. Engine model:
	// 1× 1024 BC1 (color) + 1× 1024 BC3 (normal) + 1× 1024 BC1 (blank MR pack).
	rows := []loader.ScanResult{
		saTextureRow("ColorMapContent", 1001, "Workspace.A.SurfaceAppearance", 1024),
		saTextureRow("NormalMapContent", 2001, "Workspace.A.SurfaceAppearance", 1024),
	}
	got := TotalScanMaterialGPUBytes(rows)
	want := EstimateGPUTextureBytesExact(1024, 1024, false) + // color
		EstimateGPUTextureBytesExact(1024, 1024, true) + // normal BC3
		EstimateGPUTextureBytesExact(1024, 1024, false) // blank MR pack BC1
	if got != want {
		t.Errorf("TotalScanMaterialGPUBytes = %d, want %d", got, want)
	}
}

func TestTotalScanMaterialGPUBytes_DedupesSharedNormalAcrossMaterials(t *testing.T) {
	// Two materials sharing the same normal asset: only ONE upscaled
	// normal upload + ONE MR pack on the GPU.
	rows := []loader.ScanResult{
		saTextureRow("ColorMapContent", 1001, "Workspace.A.SurfaceAppearance", 1024),
		saTextureRow("NormalMapContent", 9999, "Workspace.A.SurfaceAppearance", 1024),
		saTextureRow("ColorMapContent", 1002, "Workspace.B.SurfaceAppearance", 1024),
		saTextureRow("NormalMapContent", 9999, "Workspace.B.SurfaceAppearance", 1024),
	}
	got := TotalScanMaterialGPUBytes(rows)
	want := 2*EstimateGPUTextureBytesExact(1024, 1024, false) + // 2 unique colors
		EstimateGPUTextureBytesExact(1024, 1024, true) + // 1 shared normal BC3
		EstimateGPUTextureBytesExact(1024, 1024, false) // 1 shared MR pack BC1
	if got != want {
		t.Errorf("TotalScanMaterialGPUBytes = %d, want %d (shared normal must dedup)", got, want)
	}
}
