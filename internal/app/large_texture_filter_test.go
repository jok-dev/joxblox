package app

import "testing"

func TestEstimateSceneSurfaceAreaForPathsUsesAncestorAndLargestMatch(t *testing.T) {
	areaByPath := map[string]float64{
		"Workspace.Building.Wall":   24,
		"Workspace.Building.Window": 6,
	}

	got := estimateSceneSurfaceAreaForPaths(
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

	gotArea, gotPath := estimateSceneSurfaceAreaAndPathForPaths(
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
	row := refreshLargeTextureMetrics(scanResult{
		BytesSize:        8192,
		Width:            128,
		Height:           128,
		SceneSurfaceArea: 2,
	})

	if row.LargeTextureScore != 4096 {
		t.Fatalf("expected score 4096, got %v", row.LargeTextureScore)
	}
	if !isLargeTexture(row, 4096) {
		t.Fatalf("expected row to be classified as large texture at threshold 4096")
	}
	if isLargeTexture(row, 4097) {
		t.Fatalf("expected row to be excluded above threshold 4096")
	}
}

func TestMatchesActiveFiltersRespectsLargeTextureToggle(t *testing.T) {
	explorer := &scanResultsExplorer{
		showOnlyLargeTextures:   true,
		largeTextureThreshold:   4096,
		typeFilterValue:         scanFilterAllOption,
		instanceTypeFilterValue: scanFilterAllOption,
		propertyNameFilterValue: scanFilterAllOption,
	}
	largeRow := refreshLargeTextureMetrics(scanResult{
		BytesSize:        16384,
		Width:            256,
		Height:           256,
		SceneSurfaceArea: 2,
	})
	smallRow := refreshLargeTextureMetrics(scanResult{
		BytesSize:        2048,
		Width:            64,
		Height:           64,
		SceneSurfaceArea: 2,
	})
	noSceneDataRow := refreshLargeTextureMetrics(scanResult{
		BytesSize: 16384,
		Width:     256,
		Height:    256,
	})

	if !explorer.matchesActiveFilters(largeRow, map[string]int{}, false, false, false) {
		t.Fatalf("expected large-texture row to pass filter")
	}
	if explorer.matchesActiveFilters(smallRow, map[string]int{}, false, false, false) {
		t.Fatalf("expected small-texture row to be filtered out")
	}
	if explorer.matchesActiveFilters(noSceneDataRow, map[string]int{}, false, false, false) {
		t.Fatalf("expected row without scene data to be filtered out")
	}
}

func TestScanTableLargeTextureFieldsRoundTrip(t *testing.T) {
	original := scanResult{
		AssetID:           123,
		BytesSize:         8192,
		Width:             128,
		Height:            128,
		SceneSurfaceArea:  2,
		LargeTextureScore: 4096,
	}

	exported := mapScanResultToExportRow(original)
	imported, err := mapExportRowToScanResult(exported)
	if err != nil {
		t.Fatalf("mapExportRowToScanResult returned error: %v", err)
	}
	if imported.SceneSurfaceArea != original.SceneSurfaceArea {
		t.Fatalf("expected scene surface area %v, got %v", original.SceneSurfaceArea, imported.SceneSurfaceArea)
	}
	if imported.LargeTextureScore != original.LargeTextureScore {
		t.Fatalf("expected large texture score %v, got %v", original.LargeTextureScore, imported.LargeTextureScore)
	}
}
