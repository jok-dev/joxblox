package app

import (
	"math"
	"testing"

	"joxblox/internal/app/heatmaptab"
	"joxblox/internal/app/loader"
	"joxblox/internal/extractor"
	"joxblox/internal/format"
	"joxblox/internal/heatmap"
)

func TestBuildReportSummaryAndPoints(t *testing.T) {
	x1, y1, z1 := 10.0, 5.0, 20.0
	x2, y2, z2 := 30.0, 5.0, 40.0
	x3, y3, z3 := 50.0, 5.0, 60.0

	refs := []extractor.PositionedResult{
		{
			ID:           111,
			RawContent:   "rbxassetid://111",
			InstanceType: "MeshPart",
			InstancePath: "Workspace.MeshPart1",
			WorldX:       &x1,
			WorldY:       &y1,
			WorldZ:       &z1,
		},
		{
			ID:           222,
			RawContent:   "rbxthumb://type=Asset&id=111&w=420&h=420",
			InstanceType: "MeshPart",
			InstancePath: "Workspace.MeshPart1",
			WorldX:       &x2,
			WorldY:       &y2,
			WorldZ:       &z2,
		},
		{
			ID:           333,
			RawContent:   "rbxassetid://333",
			InstanceType: "Part",
			InstancePath: "Workspace.Part1",
			WorldX:       &x3,
			WorldY:       &y3,
			WorldZ:       &z3,
		},
		{
			ID:           444,
			RawContent:   "rbxassetid://444",
			InstanceType: "MeshPart",
			InstancePath: "Workspace.MeshPart2",
			PropertyName: "MeshContent",
			WorldX:       &x1,
			WorldY:       &y1,
			WorldZ:       &z1,
		},
	}

	resolved := map[string]reportGenerationResolvedAsset{
		extractor.AssetReferenceKey(111, "rbxassetid://111"): {
			Stats: heatmap.AssetStats{
				TotalBytes:    5 * format.Megabyte,
				TextureBytes:  3 * format.Megabyte,
				PixelCount:    512 * 512,
				TriangleCount: 0,
			},
			FileSHA256: "same-hash",
		},
		extractor.AssetReferenceKey(222, "rbxthumb://type=Asset&id=111&w=420&h=420"): {
			Stats: heatmap.AssetStats{
				TotalBytes:   5 * format.Megabyte,
				TextureBytes: 3 * format.Megabyte,
				PixelCount:   512 * 512,
			},
			FileSHA256: "same-hash",
		},
		extractor.AssetReferenceKey(333, "rbxassetid://333"): {
			Stats: heatmap.AssetStats{
				TotalBytes:   5 * format.Megabyte,
				TextureBytes: 3 * format.Megabyte,
				PixelCount:   1024 * 1024,
			},
			FileSHA256: "same-hash",
		},
		extractor.AssetReferenceKey(444, "rbxassetid://444"): {
			Stats: heatmap.AssetStats{
				TotalBytes:    7 * format.Megabyte,
				MeshBytes:     7 * format.Megabyte,
				TriangleCount: 4321,
			},
			FileSHA256: "different-hash",
		},
	}

	summary, points := buildReportSummaryAndPoints(refs, resolved, nil, loader.DefaultLargeTextureThreshold)

	if summary.TotalBytes != 22*format.Megabyte {
		t.Fatalf("expected total bytes %d, got %d", 22*format.Megabyte, summary.TotalBytes)
	}
	if summary.TextureBytes != 9*format.Megabyte {
		t.Fatalf("expected texture bytes %d, got %d", 9*format.Megabyte, summary.TextureBytes)
	}
	if summary.MeshBytes != 7*format.Megabyte {
		t.Fatalf("expected mesh bytes %d, got %d", 7*format.Megabyte, summary.MeshBytes)
	}
	if summary.TriangleCount != 4321 {
		t.Fatalf("expected triangle count 4321, got %d", summary.TriangleCount)
	}
	if summary.DuplicateCount != 2 {
		t.Fatalf("expected duplicate count 2, got %d", summary.DuplicateCount)
	}
	if summary.DuplicateSizeBytes != 10*format.Megabyte {
		t.Fatalf("expected duplicate size bytes %d, got %d", 10*format.Megabyte, summary.DuplicateSizeBytes)
	}
	if summary.ReferenceCount != 4 {
		t.Fatalf("expected reference count 4, got %d", summary.ReferenceCount)
	}
	if summary.UniqueReferenceCount != 4 {
		t.Fatalf("expected unique reference count 4, got %d", summary.UniqueReferenceCount)
	}
	if summary.UniqueAssetCount != 4 {
		t.Fatalf("expected unique asset count 4, got %d", summary.UniqueAssetCount)
	}
	if summary.ResolvedCount != 4 {
		t.Fatalf("expected resolved count 4, got %d", summary.ResolvedCount)
	}
	if summary.MeshPartCount != 2 {
		t.Fatalf("expected MeshPart count 2, got %d", summary.MeshPartCount)
	}
	if summary.PartCount != 1 {
		t.Fatalf("expected Part count 1, got %d", summary.PartCount)
	}
	if summary.DrawCallCount != 3 {
		t.Fatalf("expected draw call count 3, got %d", summary.DrawCallCount)
	}

	if len(points) != 4 {
		t.Fatalf("expected 4 heatmap points, got %d", len(points))
	}
	for _, p := range points {
		if p.AssetID <= 0 {
			t.Errorf("point has invalid AssetID: %d", p.AssetID)
		}
	}
}

func TestBuildReportSummaryAndPointsNoPositions(t *testing.T) {
	refs := []extractor.PositionedResult{
		{
			ID:           111,
			RawContent:   "rbxassetid://111",
			InstanceType: "MeshPart",
			InstancePath: "Root.MeshPart1",
		},
	}

	resolved := map[string]reportGenerationResolvedAsset{
		extractor.AssetReferenceKey(111, "rbxassetid://111"): {
			Stats: heatmap.AssetStats{
				TotalBytes:   5 * format.Megabyte,
				TextureBytes: 3 * format.Megabyte,
			},
			FileSHA256: "hash1",
		},
	}

	summary, points := buildReportSummaryAndPoints(refs, resolved, nil, loader.DefaultLargeTextureThreshold)

	if summary.TotalBytes != 5*format.Megabyte {
		t.Errorf("expected total bytes %d, got %d", 5*format.Megabyte, summary.TotalBytes)
	}
	if summary.MeshPartCount != 1 {
		t.Errorf("expected MeshPartCount 1, got %d", summary.MeshPartCount)
	}
	if summary.DrawCallCount != 1 {
		t.Errorf("expected DrawCallCount 1, got %d", summary.DrawCallCount)
	}
	if len(points) != 0 {
		t.Errorf("expected 0 points (no positions), got %d", len(points))
	}
}

func TestBuildReportSummaryAndPointsCountsDuplicatesByUniqueResolvedReference(t *testing.T) {
	x1, y1, z1 := 10.0, 5.0, 20.0
	x2, y2, z2 := 20.0, 5.0, 30.0
	refs := []extractor.PositionedResult{
		{
			ID:           111,
			RawContent:   "rbxassetid://111",
			InstanceType: "MeshPart",
			InstancePath: "Workspace.MeshPart1",
			WorldX:       &x1,
			WorldY:       &y1,
			WorldZ:       &z1,
		},
		{
			ID:           111,
			RawContent:   "rbxassetid://111",
			InstanceType: "MeshPart",
			InstancePath: "Workspace.MeshPart2",
			WorldX:       &x2,
			WorldY:       &y2,
			WorldZ:       &z2,
		},
		{
			ID:           222,
			RawContent:   "rbxassetid://222",
			InstanceType: "MeshPart",
			InstancePath: "Workspace.MeshPart3",
			WorldX:       &x2,
			WorldY:       &y2,
			WorldZ:       &z2,
		},
	}
	resolved := map[string]reportGenerationResolvedAsset{
		extractor.AssetReferenceKey(111, "rbxassetid://111"): {
			Stats:      heatmap.AssetStats{TotalBytes: 5 * format.Megabyte},
			FileSHA256: "same-hash",
		},
		extractor.AssetReferenceKey(222, "rbxassetid://222"): {
			Stats:      heatmap.AssetStats{TotalBytes: 5 * format.Megabyte},
			FileSHA256: "same-hash",
		},
	}

	summary, _ := buildReportSummaryAndPoints(refs, resolved, nil, loader.DefaultLargeTextureThreshold)

	if summary.ReferenceCount != 3 {
		t.Fatalf("expected reference count 3, got %d", summary.ReferenceCount)
	}
	if summary.UniqueReferenceCount != 2 {
		t.Fatalf("expected unique reference count 2, got %d", summary.UniqueReferenceCount)
	}
	if summary.TotalBytes != 10*format.Megabyte {
		t.Fatalf("expected total bytes %d after deduping repeated refs, got %d", 10*format.Megabyte, summary.TotalBytes)
	}
	if summary.ResolvedCount != 2 {
		t.Fatalf("expected resolved count 2 after deduping repeated refs, got %d", summary.ResolvedCount)
	}
	if summary.DuplicateCount != 1 {
		t.Fatalf("expected duplicate count 1 by unique resolved reference, got %d", summary.DuplicateCount)
	}
	if summary.DuplicateSizeBytes != 5*format.Megabyte {
		t.Fatalf("expected duplicate size bytes %d, got %d", 5*format.Megabyte, summary.DuplicateSizeBytes)
	}
	if summary.DrawCallCount != 3 {
		t.Fatalf("expected draw call count 3, got %d", summary.DrawCallCount)
	}
}

func TestBuildReportSummaryAndPointsCountsTrianglesPerMeshInstance(t *testing.T) {
	x1, y1, z1 := 10.0, 5.0, 20.0
	x2, y2, z2 := 20.0, 5.0, 30.0
	refs := []extractor.PositionedResult{
		{
			ID:           111,
			RawContent:   "rbxassetid://111",
			InstanceType: "MeshPart",
			InstancePath: "Workspace.MeshPart1",
			PropertyName: "MeshContent",
			WorldX:       &x1,
			WorldY:       &y1,
			WorldZ:       &z1,
		},
		{
			ID:           111,
			RawContent:   "rbxassetid://111",
			InstanceType: "MeshPart",
			InstancePath: "Workspace.MeshPart2",
			PropertyName: "MeshContent",
			WorldX:       &x2,
			WorldY:       &y2,
			WorldZ:       &z2,
		},
	}
	resolved := map[string]reportGenerationResolvedAsset{
		extractor.AssetReferenceKey(111, "rbxassetid://111"): {
			Stats: heatmap.AssetStats{
				TotalBytes:    5 * format.Megabyte,
				MeshBytes:     5 * format.Megabyte,
				TriangleCount: 123,
			},
			FileSHA256: "same-hash",
		},
	}

	summary, _ := buildReportSummaryAndPoints(refs, resolved, nil, loader.DefaultLargeTextureThreshold)

	if summary.TriangleCount != 246 {
		t.Fatalf("expected triangle count 246 across two mesh instances, got %d", summary.TriangleCount)
	}
	if summary.TotalBytes != 5*format.Megabyte {
		t.Fatalf("expected total bytes to remain deduped at %d, got %d", 5*format.Megabyte, summary.TotalBytes)
	}
	if summary.ResolvedCount != 1 {
		t.Fatalf("expected resolved count 1 for one unique resolved asset, got %d", summary.ResolvedCount)
	}
}

func TestBuildReportSummaryAndPointsUsesMapPartsForCounts(t *testing.T) {
	x1, y1, z1 := 10.0, 5.0, 20.0
	refs := []extractor.PositionedResult{
		{
			ID:           111,
			RawContent:   "rbxassetid://111",
			InstanceType: "MeshPart",
			InstancePath: "Workspace.MeshPart1",
			WorldX:       &x1,
			WorldY:       &y1,
			WorldZ:       &z1,
		},
	}
	resolved := map[string]reportGenerationResolvedAsset{
		extractor.AssetReferenceKey(111, "rbxassetid://111"): {
			Stats: heatmap.AssetStats{
				TotalBytes: 5 * format.Megabyte,
				MeshBytes:  5 * format.Megabyte,
			},
			FileSHA256: "hash1",
		},
	}
	mapParts := []heatmaptab.RBXLHeatmapMapPart{
		{InstanceType: "MeshPart", InstancePath: "Workspace.MeshPart1"},
		{InstanceType: "MeshPart", InstancePath: "Workspace.MeshPart2"},
		{InstanceType: "MeshPart", InstancePath: "Workspace.MeshPart3"},
		{InstanceType: "Part", InstancePath: "Workspace.Part1"},
		{InstanceType: "Part", InstancePath: "Workspace.Part2"},
	}

	summary, _ := buildReportSummaryAndPoints(refs, resolved, mapParts, loader.DefaultLargeTextureThreshold)

	if summary.MeshPartCount != 3 {
		t.Fatalf("expected MeshPartCount 3 from raw map parts, got %d", summary.MeshPartCount)
	}
	if summary.PartCount != 2 {
		t.Fatalf("expected PartCount 2 from map parts, got %d", summary.PartCount)
	}
	if summary.DrawCallCount != 5 {
		t.Fatalf("expected DrawCallCount 5 from map parts, got %d", summary.DrawCallCount)
	}
}

func TestBuildReportGenerationCellsUsesFixedCellSize(t *testing.T) {
	points := []heatmaptab.RBXLHeatmapPoint{
		{
			AssetID: 1,
			Stats:   heatmap.AssetStats{TotalBytes: 100},
			X:       10,
			Z:       10,
		},
		{
			AssetID: 2,
			Stats:   heatmap.AssetStats{TotalBytes: 100},
			X:       1350,
			Z:       10,
		},
	}
	mapParts := []heatmaptab.RBXLHeatmapMapPart{
		{
			InstanceType: "Part",
			InstancePath: "Workspace.Part1",
			CenterX:      1390,
			CenterZ:      10,
			SizeX:        20,
			SizeZ:        20,
		},
	}

	cells := buildReportGenerationCells(points, mapParts, nil)
	if len(cells) != 2 {
		t.Fatalf("expected 2 occupied cells, got %d", len(cells))
	}
	for _, cell := range cells {
		if math.Abs((cell.MaximumX-cell.MinimumX)-reportGenerationCellSizeStuds) > 0.001 {
			t.Fatalf("expected cell width %.0f, got %.3f", reportGenerationCellSizeStuds, cell.MaximumX-cell.MinimumX)
		}
	}
}

func TestBuildReportGenerationCellsUsesMapPartsForPartCounts(t *testing.T) {
	points := []heatmaptab.RBXLHeatmapPoint{
		{
			AssetID: 1,
			Stats:   heatmap.AssetStats{TotalBytes: 100},
			X:       10,
			Z:       10,
		},
	}
	mapParts := []heatmaptab.RBXLHeatmapMapPart{
		{
			InstanceType: "MeshPart",
			InstancePath: "Workspace.MeshPart1",
			CenterX:      15,
			CenterZ:      15,
			SizeX:        4,
			SizeZ:        4,
		},
		{
			InstanceType: "Part",
			InstancePath: "Workspace.Part1",
			CenterX:      1260,
			CenterZ:      15,
			SizeX:        4,
			SizeZ:        4,
		},
	}

	cells := buildReportGenerationCells(points, mapParts, nil)
	if len(cells) != 2 {
		t.Fatalf("expected 2 cells after adding map-part-only cell, got %d", len(cells))
	}

	totalMeshParts := int64(0)
	totalParts := int64(0)
	totalDrawCalls := int64(0)
	for _, cell := range cells {
		totalMeshParts += cell.Stats.MeshPartCount
		totalParts += cell.Stats.PartCount
		totalDrawCalls += cell.Stats.DrawCallCount
	}
	if totalMeshParts != 1 {
		t.Fatalf("expected total mesh parts 1 in cells, got %d", totalMeshParts)
	}
	if totalParts != 1 {
		t.Fatalf("expected total parts 1 in cells, got %d", totalParts)
	}
	if totalDrawCalls != 2 {
		t.Fatalf("expected total draw calls 2 in cells, got %d", totalDrawCalls)
	}
}

func TestBuildReportGenerationCellsDeduplicatesAssetSizeWithinCell(t *testing.T) {
	points := []heatmaptab.RBXLHeatmapPoint{
		{
			AssetID:      1,
			InstancePath: "Workspace.MeshPart1",
			Stats: heatmap.AssetStats{
				TotalBytes:   100,
				TextureBytes: 60,
			},
			X: 10,
			Z: 10,
		},
		{
			AssetID:      1,
			InstancePath: "Workspace.MeshPart2",
			Stats: heatmap.AssetStats{
				TotalBytes:   100,
				TextureBytes: 60,
			},
			X: 20,
			Z: 20,
		},
	}

	cells := buildReportGenerationCells(points, nil, nil)
	if len(cells) != 1 {
		t.Fatalf("expected 1 cell, got %d", len(cells))
	}
	if cells[0].Stats.ReferenceCount != 2 {
		t.Fatalf("expected reference count 2, got %d", cells[0].Stats.ReferenceCount)
	}
	if cells[0].Stats.UniqueAssetCount != 1 {
		t.Fatalf("expected unique asset count 1, got %d", cells[0].Stats.UniqueAssetCount)
	}
	if cells[0].Stats.TotalBytes != 100 {
		t.Fatalf("expected total bytes 100 after cell dedupe, got %d", cells[0].Stats.TotalBytes)
	}
	if cells[0].Stats.TextureBytes != 60 {
		t.Fatalf("expected texture bytes 60 after cell dedupe, got %d", cells[0].Stats.TextureBytes)
	}
}

func TestCountEstimatedDrawCallsGroupsMeshPartsByInstancingKey(t *testing.T) {
	mapParts := []heatmaptab.RBXLHeatmapMapPart{
		{InstanceType: "MeshPart", InstancePath: "Workspace.MeshPart1", MaterialKey: "metal"},
		{InstanceType: "MeshPart", InstancePath: "Workspace.MeshPart2", MaterialKey: "metal"},
		{InstanceType: "MeshPart", InstancePath: "Workspace.MeshPart3", MaterialKey: "wood"},
		{InstanceType: "Part", InstancePath: "Workspace.Part1"},
	}
	refs := []extractor.PositionedResult{
		{ID: 100, RawContent: "rbxassetid://100", InstanceType: "MeshPart", InstancePath: "Workspace.MeshPart1", PropertyName: "MeshContent"},
		{ID: 200, RawContent: "rbxassetid://200", InstanceType: "MeshPart", InstancePath: "Workspace.MeshPart1", PropertyName: "TextureContent"},
		{ID: 100, RawContent: "rbxassetid://100", InstanceType: "MeshPart", InstancePath: "Workspace.MeshPart2", PropertyName: "MeshContent"},
		{ID: 200, RawContent: "rbxassetid://200", InstanceType: "MeshPart", InstancePath: "Workspace.MeshPart2", PropertyName: "TextureContent"},
		{ID: 100, RawContent: "rbxassetid://100", InstanceType: "MeshPart", InstancePath: "Workspace.MeshPart3", PropertyName: "MeshContent"},
	}

	drawCalls := countEstimatedDrawCalls(mapParts, refs)
	if drawCalls != 3 {
		t.Fatalf("expected 3 estimated draw calls, got %d", drawCalls)
	}
}

func TestBuildReportGenerationCellsEstimatesDrawCallsFromRefs(t *testing.T) {
	x1, y1, z1 := 10.0, 5.0, 10.0
	x2, y2, z2 := 20.0, 5.0, 20.0
	points := []heatmaptab.RBXLHeatmapPoint{
		{
			AssetID:      1,
			InstanceType: "MeshPart",
			InstancePath: "Workspace.MeshPart1",
			Stats:        heatmap.AssetStats{TotalBytes: 100},
			X:            x1,
			Y:            y1,
			Z:            z1,
		},
		{
			AssetID:      2,
			InstanceType: "MeshPart",
			InstancePath: "Workspace.MeshPart2",
			Stats:        heatmap.AssetStats{TotalBytes: 100},
			X:            x2,
			Y:            y2,
			Z:            z2,
		},
	}
	refs := []extractor.PositionedResult{
		{ID: 100, RawContent: "rbxassetid://100", InstanceType: "MeshPart", InstancePath: "Workspace.MeshPart1", PropertyName: "MeshContent", WorldX: &x1, WorldY: &y1, WorldZ: &z1},
		{ID: 200, RawContent: "rbxassetid://200", InstanceType: "MeshPart", InstancePath: "Workspace.MeshPart1", PropertyName: "TextureContent", WorldX: &x1, WorldY: &y1, WorldZ: &z1},
		{ID: 100, RawContent: "rbxassetid://100", InstanceType: "MeshPart", InstancePath: "Workspace.MeshPart2", PropertyName: "MeshContent", WorldX: &x2, WorldY: &y2, WorldZ: &z2},
		{ID: 200, RawContent: "rbxassetid://200", InstanceType: "MeshPart", InstancePath: "Workspace.MeshPart2", PropertyName: "TextureContent", WorldX: &x2, WorldY: &y2, WorldZ: &z2},
	}

	cells := buildReportGenerationCells(points, nil, refs)
	if len(cells) != 1 {
		t.Fatalf("expected 1 cell, got %d", len(cells))
	}
	if cells[0].Stats.DrawCallCount != 1 {
		t.Fatalf("expected 1 draw call after instancing identical meshparts, got %d", cells[0].Stats.DrawCallCount)
	}
}

func TestCountReportGenerationOversizedTextures(t *testing.T) {
	refs := []extractor.PositionedResult{
		{
			ID:           101,
			RawContent:   "rbxassetid://101",
			InstancePath: "Workspace.BigTexture",
		},
		{
			ID:           202,
			RawContent:   "rbxassetid://202",
			InstancePath: "Workspace.SmallTexture",
		},
	}
	resolved := map[string]reportGenerationResolvedAsset{
		extractor.AssetReferenceKey(101, "rbxassetid://101"): {
			Stats: heatmap.AssetStats{TextureBytes: 200_000},
		},
		extractor.AssetReferenceKey(202, "rbxassetid://202"): {
			Stats: heatmap.AssetStats{TextureBytes: 10_000},
		},
	}
	mapParts := []heatmaptab.RBXLHeatmapMapPart{
		{
			InstancePath: "Workspace.BigTexture",
			SizeX:        5,
			SizeY:        5,
			SizeZ:        5,
		},
		{
			InstancePath: "Workspace.SmallTexture",
			SizeX:        100,
			SizeY:        100,
			SizeZ:        100,
		},
	}

	count := countReportGenerationOversizedTextures(refs, resolved, mapParts, loader.DefaultLargeTextureThreshold)
	if count != 1 {
		t.Fatalf("expected 1 oversized texture, got %d", count)
	}
}
