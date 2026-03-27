package app

import "testing"

func TestBuildHeatmapCellsTracksUniqueTextureAndMeshCounts(t *testing.T) {
	scene := &rbxlHeatmapScene{
		Points: []rbxlHeatmapPoint{
			{
				AssetID: 101,
				Stats: rbxlHeatmapAssetStats{
					TextureBytes: 1200,
					TotalBytes:   1200,
					PixelCount:   4096,
				},
				X: 1,
				Z: 1,
			},
			{
				AssetID: 101,
				Stats: rbxlHeatmapAssetStats{
					TextureBytes: 1200,
					TotalBytes:   1200,
					PixelCount:   4096,
				},
				X: 1.5,
				Z: 1.5,
			},
			{
				AssetID: 202,
				Stats: rbxlHeatmapAssetStats{
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

func TestBuildHeatmapCellsTracksUniqueTextureAndMeshDiffCounts(t *testing.T) {
	scene := &rbxlHeatmapScene{
		Points: []rbxlHeatmapPoint{
			{
				AssetID: 101,
				Stats: rbxlHeatmapAssetStats{
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
				Stats: rbxlHeatmapAssetStats{
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
