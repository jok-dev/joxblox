package loader

import "testing"

func TestEstimateSceneSurfaceAreaForPathsUsesAncestorAndLargestMatch(t *testing.T) {
	areaByPath := map[string]float64{
		"Workspace.Building.Wall":   24,
		"Workspace.Building.Window": 6,
	}

	got := EstimateSceneSurfaceAreaForPaths(
		"Workspace.Building.Wall.Decal",
		[]string{"Workspace.Building.Window.Texture"},
		areaByPath,
	)
	if got != 24 {
		t.Fatalf("expected largest matched area 24, got %v", got)
	}
}

func TestEstimateSceneSurfaceAreaAndPathForPathsReturnsLargestPath(t *testing.T) {
	areaByPath := map[string]float64{
		"Workspace.Building.Wall":   24,
		"Workspace.Building.Window": 6,
	}

	gotArea, gotPath := EstimateSceneSurfaceAreaAndPathForPaths(
		"Workspace.Building.Wall.Decal",
		[]string{"Workspace.Building.Window.Texture"},
		areaByPath,
	)
	if gotArea != 24 {
		t.Fatalf("expected largest matched area 24, got %v", gotArea)
	}
	if gotPath != "Workspace.Building.Wall.Decal" {
		t.Fatalf("expected largest path Workspace.Building.Wall.Decal, got %q", gotPath)
	}
}

func TestRefreshLargeTextureMetricsComputesScoreFromTextureBytes(t *testing.T) {
	row := RefreshLargeTextureMetrics(ScanResult{
		BytesSize:        8192,
		Width:            128,
		Height:           128,
		SceneSurfaceArea: 2,
	})

	if row.LargeTextureScore != 4096 {
		t.Fatalf("expected score 4096, got %v", row.LargeTextureScore)
	}
	if !IsLargeTexture(row, 4096) {
		t.Fatalf("expected row to be classified as large texture at threshold 4096")
	}
	if IsLargeTexture(row, 4097) {
		t.Fatalf("expected row to be excluded above threshold 4096")
	}
}
