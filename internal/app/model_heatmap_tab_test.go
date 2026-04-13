package app

import (
	"image/color"
	"math"
	"testing"

	"joxblox/internal/app/ui"
	"joxblox/internal/extractor"
	"joxblox/internal/heatmap"
)

func TestBuildModelHeatmapInstancesCollectsTextureRefs(t *testing.T) {
	centerX := 0.0
	centerY := 0.0
	centerZ := 0.0
	sizeX := 2.0
	sizeY := 2.0
	sizeZ := 2.0
	mapParts := []extractor.MapRenderPartResult{
		{
			InstanceType: "MeshPart",
			InstancePath: "Workspace.Crate",
			CenterX:      &centerX,
			CenterY:      &centerY,
			CenterZ:      &centerZ,
			SizeX:        &sizeX,
			SizeY:        &sizeY,
			SizeZ:        &sizeZ,
		},
	}
	refs := []extractor.PositionedResult{
		{
			ID:           111,
			RawContent:   "rbxassetid://111",
			InstanceType: "MeshPart",
			InstancePath: "Workspace.Crate",
			PropertyName: "MeshContent",
		},
		{
			ID:           222,
			RawContent:   "rbxassetid://222",
			InstanceType: "MeshPart",
			InstancePath: "Workspace.Crate",
			PropertyName: "TextureID",
		},
	}

	instances := buildModelHeatmapInstances(mapParts, refs)
	if len(instances) != 1 {
		t.Fatalf("expected 1 instance, got %d", len(instances))
	}
	if instances[0].MeshRef.AssetID != 111 {
		t.Fatalf("expected mesh asset 111, got %d", instances[0].MeshRef.AssetID)
	}
	if len(instances[0].TextureRefs) != 1 || instances[0].TextureRefs[0].AssetID != 222 {
		t.Fatalf("expected texture ref 222, got %+v", instances[0].TextureRefs)
	}
}

func TestBuildModelHeatmapInstancesCollectsSurfaceAppearanceTextures(t *testing.T) {
	centerX := 0.0
	centerY := 0.0
	centerZ := 0.0
	sizeX := 2.0
	sizeY := 2.0
	sizeZ := 2.0
	mapParts := []extractor.MapRenderPartResult{
		{
			InstanceType: "MeshPart",
			InstancePath: "Workspace.Floor",
			CenterX:      &centerX,
			CenterY:      &centerY,
			CenterZ:      &centerZ,
			SizeX:        &sizeX,
			SizeY:        &sizeY,
			SizeZ:        &sizeZ,
		},
	}
	refs := []extractor.PositionedResult{
		{
			ID:           111,
			RawContent:   "rbxassetid://111",
			InstanceType: "MeshPart",
			InstancePath: "Workspace.Floor",
			PropertyName: "MeshContent",
		},
		{
			ID:           555,
			RawContent:   "rbxassetid://555",
			InstanceType: "SurfaceAppearance",
			InstancePath: "Workspace.Floor.SurfaceAppearance",
			PropertyName: "ColorMapContent",
		},
		{
			ID:           556,
			RawContent:   "rbxassetid://556",
			InstanceType: "SurfaceAppearance",
			InstancePath: "Workspace.Floor.SurfaceAppearance",
			PropertyName: "NormalMapContent",
		},
	}

	instances := buildModelHeatmapInstances(mapParts, refs)
	if len(instances) != 1 {
		t.Fatalf("expected 1 instance, got %d", len(instances))
	}
	if len(instances[0].TextureRefs) != 2 {
		t.Fatalf("expected 2 texture refs from SurfaceAppearance, got %d: %+v", len(instances[0].TextureRefs), instances[0].TextureRefs)
	}
	ids := map[int64]bool{}
	for _, ref := range instances[0].TextureRefs {
		ids[ref.AssetID] = true
	}
	if !ids[555] || !ids[556] {
		t.Fatalf("expected texture refs 555 and 556, got %+v", instances[0].TextureRefs)
	}
}

func TestBuildModelHeatmapPreviewDataTextureModeColorsByTextureBytes(t *testing.T) {
	meshPreview, err := ui.BuildMeshPreviewData(
		[]float32{
			-1, -1, 0,
			1, -1, 0,
			0, 1, 0,
		},
		[]uint32{0, 1, 2},
		24,
		1,
	)
	if err != nil {
		t.Fatalf("buildMeshPreviewData returned error: %v", err)
	}
	instances := []modelHeatmapMeshInstance{
		{
			InstancePath: "Workspace.Small",
			MeshRef:      heatmap.AssetReference{AssetID: 1, AssetInput: "rbxassetid://1"},
			TextureRefs:  []heatmap.AssetReference{{AssetID: 10, AssetInput: "rbxassetid://10"}},
			SizeX:        2,
			SizeY:        2,
			SizeZ:        2,
		},
		{
			InstancePath: "Workspace.Big",
			MeshRef:      heatmap.AssetReference{AssetID: 2, AssetInput: "rbxassetid://2"},
			TextureRefs:  []heatmap.AssetReference{{AssetID: 20, AssetInput: "rbxassetid://20"}},
			SizeX:        2,
			SizeY:        2,
			SizeZ:        2,
		},
	}
	resolvedMeshes := map[string]modelHeatmapResolvedMesh{
		"rbxassetid://1": {
			Reference:     heatmap.AssetReference{AssetID: 1, AssetInput: "rbxassetid://1"},
			Preview:       meshPreview,
			Bounds:        computeModelHeatmapMeshBounds(meshPreview.RawPositions),
			TriangleCount: 24,
		},
		"rbxassetid://2": {
			Reference:     heatmap.AssetReference{AssetID: 2, AssetInput: "rbxassetid://2"},
			Preview:       meshPreview,
			Bounds:        computeModelHeatmapMeshBounds(meshPreview.RawPositions),
			TriangleCount: 24,
		},
	}
	resolvedTextures := map[string]modelHeatmapResolvedTexture{
		"rbxassetid://10": {
			Reference: heatmap.AssetReference{AssetID: 10, AssetInput: "rbxassetid://10"},
			Width:     16,
			Height:    16,
			BytesSize: 512,
		},
		"rbxassetid://20": {
			Reference: heatmap.AssetReference{AssetID: 20, AssetInput: "rbxassetid://20"},
			Width:     512,
			Height:    512,
			BytesSize: 65536,
		},
	}

	previewData, infos, summary, buildErr := buildModelHeatmapPreviewDataWithMode(instances, resolvedMeshes, resolvedTextures, 1.0, modelHeatmapModeTexture)
	if buildErr != nil {
		t.Fatalf("texture mode build failed: %v", buildErr)
	}
	if len(previewData.Batches) != 2 || len(infos) != 2 {
		t.Fatalf("expected 2 batches, got %d infos %d", len(previewData.Batches), len(infos))
	}
	if summary.HeatMode != modelHeatmapModeTexture {
		t.Fatalf("expected heat mode %q, got %q", modelHeatmapModeTexture, summary.HeatMode)
	}
	if summary.UniqueTextureCount != 2 {
		t.Fatalf("expected 2 unique textures, got %d", summary.UniqueTextureCount)
	}
	if summary.TextureBytes != 512+65536 {
		t.Fatalf("expected texture bytes %d, got %d", 512+65536, summary.TextureBytes)
	}
	if infos[0].TextureBytes != 512 || infos[1].TextureBytes != 65536 {
		t.Fatalf("unexpected per-batch texture bytes: %d, %d", infos[0].TextureBytes, infos[1].TextureBytes)
	}
	firstColor := [3]uint8{previewData.Batches[0].RawColors[0], previewData.Batches[0].RawColors[1], previewData.Batches[0].RawColors[2]}
	secondColor := [3]uint8{previewData.Batches[1].RawColors[0], previewData.Batches[1].RawColors[1], previewData.Batches[1].RawColors[2]}
	if firstColor == secondColor {
		t.Fatal("expected texture mode to color textures differently")
	}
}

func TestModelHeatmapValueFromInfoHandlesTextureModes(t *testing.T) {
	info := modelHeatmapBatchInfo{
		TriangleCount:  50,
		Density:        1.5,
		TextureBytes:   2048,
		TextureDensity: 7.5,
	}
	if got := modelHeatmapValueFromInfo(modelHeatmapModeTexture, info); got != 2048 {
		t.Fatalf("expected texture bytes 2048, got %f", got)
	}
	if got := modelHeatmapValueFromInfo(modelHeatmapModeSizeScaledTexture, info); got != 7.5 {
		t.Fatalf("expected texture density 7.5, got %f", got)
	}
	if got := modelHeatmapValueFromInfo(modelHeatmapModeTriangles, info); got != 50 {
		t.Fatalf("expected triangles 50, got %f", got)
	}
	if got := modelHeatmapValueFromInfo(modelHeatmapModeSizeScaledTriangles, info); got != 1.5 {
		t.Fatalf("expected density 1.5, got %f", got)
	}
}

func TestBuildModelHeatmapInstancesMatchesMeshContentRefs(t *testing.T) {
	centerX := 10.0
	centerY := 5.0
	centerZ := -4.0
	sizeX := 2.0
	sizeY := 3.0
	sizeZ := 4.0
	yawDegrees := 45.0
	mapParts := []extractor.MapRenderPartResult{
		{
			InstanceType: "MeshPart",
			InstancePath: "Workspace.Building.Statue",
			CenterX:      &centerX,
			CenterY:      &centerY,
			CenterZ:      &centerZ,
			SizeX:        &sizeX,
			SizeY:        &sizeY,
			SizeZ:        &sizeZ,
			YawDegrees:   &yawDegrees,
		},
		{
			InstanceType: "Part",
			InstancePath: "Workspace.Building.Base",
			CenterX:      &centerX,
			CenterY:      &centerY,
			CenterZ:      &centerZ,
			SizeX:        &sizeX,
			SizeY:        &sizeY,
			SizeZ:        &sizeZ,
		},
	}
	refs := []extractor.PositionedResult{
		{
			ID:           123,
			RawContent:   "rbxassetid://123",
			InstanceType: "MeshPart",
			InstancePath: "Workspace.Building.Statue",
			PropertyName: "MeshContent",
		},
		{
			ID:           999,
			RawContent:   "rbxassetid://999",
			InstanceType: "Part",
			InstancePath: "Workspace.Building.Base",
			PropertyName: "TextureID",
		},
	}

	instances := buildModelHeatmapInstances(mapParts, refs)
	if len(instances) != 1 {
		t.Fatalf("expected 1 meshpart instance, got %d", len(instances))
	}
	if instances[0].MeshRef.AssetID != 123 {
		t.Fatalf("expected mesh asset 123, got %d", instances[0].MeshRef.AssetID)
	}
	if instances[0].InstancePath != "Workspace.Building.Statue" {
		t.Fatalf("unexpected instance path %q", instances[0].InstancePath)
	}
}

func TestBuildModelHeatmapPreviewDataBuildsColoredScene(t *testing.T) {
	meshPreview, err := ui.BuildMeshPreviewData(
		[]float32{
			-1, -1, 0,
			1, -1, 0,
			0, 1, 0,
		},
		[]uint32{0, 1, 2},
		120,
		1,
	)
	if err != nil {
		t.Fatalf("buildMeshPreviewData returned error: %v", err)
	}

	instances := []modelHeatmapMeshInstance{{
		InstancePath: "Workspace.Mesh",
		MeshRef: heatmap.AssetReference{
			AssetID:    123,
			AssetInput: "rbxassetid://123",
		},
		CenterX:    25,
		CenterY:    10,
		CenterZ:    -15,
		SizeX:      2,
		SizeY:      4,
		SizeZ:      6,
		YawDegrees: 30,
	}}
	resolved := map[string]modelHeatmapResolvedMesh{
		"rbxassetid://123": {
			Reference:     heatmap.AssetReference{AssetID: 123, AssetInput: "rbxassetid://123"},
			Preview:       meshPreview,
			Bounds:        computeModelHeatmapMeshBounds(meshPreview.RawPositions),
			TriangleCount: 120,
		},
	}

	previewData, _, summary, buildErr := buildModelHeatmapPreviewData(instances, resolved)
	if buildErr != nil {
		t.Fatalf("buildModelHeatmapPreviewData returned error: %v", buildErr)
	}
	if len(previewData.Batches) != 1 {
		t.Fatalf("expected 1 scene batch, got %d", len(previewData.Batches))
	}
	if len(previewData.Batches[0].RawColors) != 12 {
		t.Fatalf("expected 12 color bytes, got %d", len(previewData.Batches[0].RawColors))
	}
	if summary.RenderedMeshPartCount != 1 {
		t.Fatalf("expected 1 rendered meshpart, got %d", summary.RenderedMeshPartCount)
	}
	if summary.TriangleCount != 120 {
		t.Fatalf("expected triangle count 120, got %d", summary.TriangleCount)
	}
	for _, value := range previewData.Batches[0].RawPositions {
		if value < -1.01 || value > 1.01 {
			t.Fatalf("expected normalized scene positions, got %f", value)
		}
	}
}

func TestModelHeatmapTriangleDensityUsesSurfaceArea(t *testing.T) {
	density := modelHeatmapTriangleDensity(2, 3, 4, 120)
	if density <= 0 {
		t.Fatalf("expected positive density, got %f", density)
	}
	if density != 120.0/52.0 {
		t.Fatalf("expected density %f, got %f", 120.0/52.0, density)
	}
}

func TestApplyModelHeatmapRotationUsesFullMatrix(t *testing.T) {
	rotation := [9]float64{
		1, 0, 0,
		0, 0, 1,
		0, -1, 0,
	}
	x, y, z := applyModelHeatmapRotation(rotation, 0, 2, 0)
	if x != 0 || y != 0 || z != -2 {
		t.Fatalf("expected rotated vector (0,0,-2), got (%f,%f,%f)", x, y, z)
	}
}

func TestApplyModelHeatmapRotationMatchesYawFallbackConvention(t *testing.T) {
	rotation := modelHeatmapYawRotation(90)
	x, y, z := applyModelHeatmapRotation(rotation, 1, 0, 0)
	if math.Abs(x) > 1e-9 || y != 0 || math.Abs(z+1) > 1e-9 {
		t.Fatalf("expected yaw rotation to map +X to -Z, got (%f,%f,%f)", x, y, z)
	}
}

func TestAppendModelHeatmapMeshInstancePreservesMeshOrigin(t *testing.T) {
	meshPreview, err := ui.BuildMeshPreviewData(
		[]float32{
			-1, -1, 0,
			1, -1, 0,
			0, 1, 0,
		},
		[]uint32{0, 1, 2},
		1,
		1,
	)
	if err != nil {
		t.Fatalf("buildMeshPreviewData returned error: %v", err)
	}

	batch := ui.MeshPreviewBatchData{}
	appendModelHeatmapMeshInstance(&batch, modelHeatmapMeshInstance{
		CenterX:    5,
		CenterY:    0,
		CenterZ:    0,
		SizeX:      2,
		SizeY:      2,
		SizeZ:      2,
		BasisSizeX: 2,
		BasisSizeY: 2,
		BasisSizeZ: 2,
		Rotation: [9]float64{
			1, 0, 0,
			0, 1, 0,
			0, 0, 1,
		},
	}, modelHeatmapResolvedMesh{
		Preview: meshPreview,
		Bounds:  computeModelHeatmapMeshBounds(meshPreview.RawPositions),
	}, color.NRGBA{R: 255, G: 0, B: 0, A: 255})

	if len(batch.RawPositions) < 9 {
		t.Fatalf("expected 3 vertices in batch")
	}
	expectedFirstX := float32(4.0)
	if math.Abs(float64(batch.RawPositions[0]-expectedFirstX)) > 0.01 {
		t.Fatalf("expected first vertex x near %f (mesh origin preserved + world offset), got %f", expectedFirstX, batch.RawPositions[0])
	}
}

func TestModelHeatmapColorAppliesHeatSpread(t *testing.T) {
	density := 10.0
	maxDensity := 100.0

	narrow := modelHeatmapColor(density, maxDensity, 0.5)
	wide := modelHeatmapColor(density, maxDensity, 2.0)

	if narrow == wide {
		t.Fatal("expected heat spread to change the resulting color")
	}
	if wide.R < narrow.R {
		t.Fatalf("expected wider spread to push low densities warmer, got narrow=%v wide=%v", narrow, wide)
	}
}

func TestModelHeatmapValueUsesSelectedMode(t *testing.T) {
	densityValue := modelHeatmapValue(modelHeatmapModeSizeScaledTriangles, 2.5, 120)
	if math.Abs(densityValue-2.5) > 1e-9 {
		t.Fatalf("expected size-scaled mode to use density, got %f", densityValue)
	}

	triangleValue := modelHeatmapValue(modelHeatmapModeTriangles, 2.5, 120)
	if triangleValue != 120 {
		t.Fatalf("expected triangle mode to use triangle count, got %f", triangleValue)
	}
}

func TestBuildModelHeatmapPreviewDataTriangleModeColorsEqualTriangleCounts(t *testing.T) {
	meshPreview, err := ui.BuildMeshPreviewData(
		[]float32{
			-1, -1, 0,
			1, -1, 0,
			0, 1, 0,
		},
		[]uint32{0, 1, 2},
		120,
		1,
	)
	if err != nil {
		t.Fatalf("buildMeshPreviewData returned error: %v", err)
	}

	instances := []modelHeatmapMeshInstance{
		{
			InstancePath: "Workspace.Small",
			MeshRef:      heatmap.AssetReference{AssetID: 123, AssetInput: "rbxassetid://123"},
			SizeX:        2,
			SizeY:        2,
			SizeZ:        2,
		},
		{
			InstancePath: "Workspace.Large",
			MeshRef:      heatmap.AssetReference{AssetID: 456, AssetInput: "rbxassetid://456"},
			SizeX:        20,
			SizeY:        20,
			SizeZ:        20,
		},
	}
	resolved := map[string]modelHeatmapResolvedMesh{
		"rbxassetid://123": {
			Reference:     heatmap.AssetReference{AssetID: 123, AssetInput: "rbxassetid://123"},
			Preview:       meshPreview,
			Bounds:        computeModelHeatmapMeshBounds(meshPreview.RawPositions),
			TriangleCount: 120,
		},
		"rbxassetid://456": {
			Reference:     heatmap.AssetReference{AssetID: 456, AssetInput: "rbxassetid://456"},
			Preview:       meshPreview,
			Bounds:        computeModelHeatmapMeshBounds(meshPreview.RawPositions),
			TriangleCount: 120,
		},
	}

	densityPreview, _, _, err := buildModelHeatmapPreviewDataWithMode(instances, resolved, nil, 1.0, modelHeatmapModeSizeScaledTriangles)
	if err != nil {
		t.Fatalf("density mode build failed: %v", err)
	}
	trianglePreview, _, summary, err := buildModelHeatmapPreviewDataWithMode(instances, resolved, nil, 1.0, modelHeatmapModeTriangles)
	if err != nil {
		t.Fatalf("triangle mode build failed: %v", err)
	}

	if summary.HeatMode != modelHeatmapModeTriangles {
		t.Fatalf("expected summary heat mode to be triangle mode, got %q", summary.HeatMode)
	}
	if len(densityPreview.Batches) != 2 || len(trianglePreview.Batches) != 2 {
		t.Fatal("expected two batches in both preview modes")
	}
	if densityPreview.Batches[0].RawColors[0] == densityPreview.Batches[1].RawColors[0] &&
		densityPreview.Batches[0].RawColors[1] == densityPreview.Batches[1].RawColors[1] &&
		densityPreview.Batches[0].RawColors[2] == densityPreview.Batches[1].RawColors[2] {
		t.Fatal("expected density mode to color differently sized meshes differently")
	}
	if trianglePreview.Batches[0].RawColors[0] != trianglePreview.Batches[1].RawColors[0] ||
		trianglePreview.Batches[0].RawColors[1] != trianglePreview.Batches[1].RawColors[1] ||
		trianglePreview.Batches[0].RawColors[2] != trianglePreview.Batches[1].RawColors[2] {
		t.Fatal("expected triangle mode to color equal triangle counts the same")
	}
}
