package app

import (
	"testing"

	"joxblox/internal/heatmap"
)

func TestBuildHeatmapCellsTracksUniqueTextureAndMeshCounts(t *testing.T) {
	scene := &rbxlHeatmapScene{
		Points: []rbxlHeatmapPoint{
			{
				AssetID: 101,
				Stats: heatmap.AssetStats{
					TextureBytes: 1200,
					TotalBytes:   1200,
					PixelCount:   4096,
				},
				X: 1,
				Z: 1,
			},
			{
				AssetID: 101,
				Stats: heatmap.AssetStats{
					TextureBytes: 1200,
					TotalBytes:   1200,
					PixelCount:   4096,
				},
				X: 1.5,
				Z: 1.5,
			},
			{
				AssetID: 202,
				Stats: heatmap.AssetStats{
					MeshBytes:     2400,
					TotalBytes:    2400,
					TriangleCount: 300,
				},
				X: 2,
				Z: 2,
			},
		},
		MinimumX: 0,
		MaximumX: 10,
		MinimumZ: 0,
		MaximumZ: 10,
	}

	cells, _, _, _, _ := buildHeatmapCells(scene, 1)
	if len(cells) != 1 {
		t.Fatalf("expected 1 cell, got %d", len(cells))
	}

	cell := cells[0]
	if cell.Stats.ReferenceCount != 3 {
		t.Fatalf("expected 3 references, got %d", cell.Stats.ReferenceCount)
	}
	if cell.Stats.UniqueAssetCount != 2 {
		t.Fatalf("expected 2 unique assets, got %d", cell.Stats.UniqueAssetCount)
	}
	if cell.Stats.UniqueTextureCount != 1 {
		t.Fatalf("expected 1 unique texture, got %d", cell.Stats.UniqueTextureCount)
	}
	if cell.Stats.UniqueMeshCount != 1 {
		t.Fatalf("expected 1 unique mesh, got %d", cell.Stats.UniqueMeshCount)
	}
}

func TestBuildHeatmapCellsTracksPartAndMeshPartCounts(t *testing.T) {
	scene := &rbxlHeatmapScene{
		Points: []rbxlHeatmapPoint{
			{
				AssetID:      101,
				InstanceType: "MeshPart",
				InstancePath: "Workspace.MeshPart1",
				Stats:        heatmap.AssetStats{TotalBytes: 1000},
				X:            1,
				Z:            1,
			},
			{
				AssetID:      102,
				InstanceType: "MeshPart",
				InstancePath: "Workspace.MeshPart1",
				Stats:        heatmap.AssetStats{TotalBytes: 2000},
				X:            1.5,
				Z:            1.5,
			},
			{
				AssetID:      103,
				InstanceType: "MeshPart",
				InstancePath: "Workspace.MeshPart2",
				Stats:        heatmap.AssetStats{TotalBytes: 500},
				X:            2,
				Z:            2,
			},
			{
				AssetID:      201,
				InstanceType: "Part",
				InstancePath: "Workspace.Part1",
				Stats:        heatmap.AssetStats{TotalBytes: 300},
				X:            3,
				Z:            3,
			},
		},
		MinimumX: 0,
		MaximumX: 10,
		MinimumZ: 0,
		MaximumZ: 10,
	}

	cells, _, _, _, _ := buildHeatmapCells(scene, 1)
	if len(cells) != 1 {
		t.Fatalf("expected 1 cell, got %d", len(cells))
	}

	cell := cells[0]
	if cell.Stats.MeshPartCount != 2 {
		t.Errorf("expected 2 MeshParts (2 unique instance paths), got %d", cell.Stats.MeshPartCount)
	}
	if cell.Stats.PartCount != 1 {
		t.Errorf("expected 1 Part, got %d", cell.Stats.PartCount)
	}
}

func TestBuildHeatmapCellsCountsTrianglesPerMeshInstance(t *testing.T) {
	scene := &rbxlHeatmapScene{
		Points: []rbxlHeatmapPoint{
			{
				AssetID:      101,
				InstanceType: "MeshPart",
				InstancePath: "Workspace.MeshPart1",
				PropertyName: "MeshContent",
				Stats: heatmap.AssetStats{
					MeshBytes:     2400,
					TotalBytes:    2400,
					TriangleCount: 300,
				},
				X: 1,
				Z: 1,
			},
			{
				AssetID:      101,
				InstanceType: "MeshPart",
				InstancePath: "Workspace.MeshPart2",
				PropertyName: "MeshContent",
				Stats: heatmap.AssetStats{
					MeshBytes:     2400,
					TotalBytes:    2400,
					TriangleCount: 300,
				},
				X: 1.5,
				Z: 1.5,
			},
		},
		MinimumX: 0,
		MaximumX: 10,
		MinimumZ: 0,
		MaximumZ: 10,
	}

	cells, _, _, _, _ := buildHeatmapCells(scene, 1)
	if len(cells) != 1 {
		t.Fatalf("expected 1 cell, got %d", len(cells))
	}

	cell := cells[0]
	if cell.Stats.UniqueAssetCount != 1 {
		t.Fatalf("expected unique asset count 1, got %d", cell.Stats.UniqueAssetCount)
	}
	if cell.Stats.TotalBytes != 2400 {
		t.Fatalf("expected total bytes 2400 after asset dedupe, got %d", cell.Stats.TotalBytes)
	}
	if cell.Stats.TriangleCount != 600 {
		t.Fatalf("expected triangle count 600 across two mesh instances, got %d", cell.Stats.TriangleCount)
	}
}

func TestHeatMetricValueNewMetrics(t *testing.T) {
	cell := heatmap.Cell{
		Stats: heatmap.Totals{
			UniqueAssetCount: 42,
			MeshPartCount:    15,
			PartCount:        7,
		},
	}
	maximums := heatMetricMaximums{}

	if v := heatMetricValue(cell, heatMetricUniqueAssetCount, maximums); v != 42 {
		t.Errorf("heatMetricValue(UniqueAssets) = %.0f, want 42", v)
	}
	if v := heatMetricValue(cell, heatMetricMeshPartCount, maximums); v != 15 {
		t.Errorf("heatMetricValue(MeshParts) = %.0f, want 15", v)
	}
	if v := heatMetricValue(cell, heatMetricPartCount, maximums); v != 7 {
		t.Errorf("heatMetricValue(Parts) = %.0f, want 7", v)
	}
}

func TestBuildHeatmapCellsTracksUniqueTextureAndMeshDiffCounts(t *testing.T) {
	scene := &rbxlHeatmapScene{
		Points: []rbxlHeatmapPoint{
			{
				AssetID: 101,
				Stats: heatmap.AssetStats{
					TextureBytes: 1000,
					TotalBytes:   1000,
					PixelCount:   1024,
				},
				X: 1,
				Z: 1,
			},
		},
		ComparePoints: []rbxlHeatmapPoint{
			{
				AssetID: 202,
				Stats: heatmap.AssetStats{
					MeshBytes:     2000,
					TotalBytes:    2000,
					TriangleCount: 250,
				},
				X: 1,
				Z: 1,
			},
		},
		MinimumX: 0,
		MaximumX: 10,
		MinimumZ: 0,
		MaximumZ: 10,
		DiffMode: true,
	}

	cells, _, _, _, _ := buildHeatmapCells(scene, 1)
	if len(cells) != 1 {
		t.Fatalf("expected 1 cell, got %d", len(cells))
	}

	cell := cells[0]
	if cell.BaseStats.UniqueTextureCount != 1 {
		t.Fatalf("expected base unique texture count 1, got %d", cell.BaseStats.UniqueTextureCount)
	}
	if cell.BaseStats.UniqueMeshCount != 0 {
		t.Fatalf("expected base unique mesh count 0, got %d", cell.BaseStats.UniqueMeshCount)
	}
	if cell.Stats.UniqueTextureCount != -1 {
		t.Fatalf("expected diff unique texture count -1, got %d", cell.Stats.UniqueTextureCount)
	}
	if cell.Stats.UniqueMeshCount != 1 {
		t.Fatalf("expected diff unique mesh count 1, got %d", cell.Stats.UniqueMeshCount)
	}
}
