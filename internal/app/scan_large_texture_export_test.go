package app

import (
	"testing"

	"joxblox/internal/app/loader"
)

func TestMatchesActiveFiltersRespectsLargeTextureToggle(t *testing.T) {
	explorer := &scanResultsExplorer{
		showOnlyLargeTextures:   true,
		largeTextureThreshold:   4096,
		typeFilterValue:         loader.ScanFilterAllOption,
		instanceTypeFilterValue: loader.ScanFilterAllOption,
		propertyNameFilterValue: loader.ScanFilterAllOption,
	}
	largeRow := loader.RefreshLargeTextureMetrics(loader.ScanResult{
		BytesSize:        16384,
		Width:            256,
		Height:           256,
		SceneSurfaceArea: 2,
	})
	smallRow := loader.RefreshLargeTextureMetrics(loader.ScanResult{
		BytesSize:        2048,
		Width:            64,
		Height:           64,
		SceneSurfaceArea: 2,
	})
	noSceneDataRow := loader.RefreshLargeTextureMetrics(loader.ScanResult{
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
	original := loader.ScanResult{
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
