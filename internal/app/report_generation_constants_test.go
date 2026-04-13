package app

import (
	"testing"

	"joxblox/internal/format"
)

func TestReportGenerationAssetTypeByID(t *testing.T) {
	mapAssetType, found := reportGenerationAssetTypeByID("map")
	if !found {
		t.Fatalf("expected to find map asset type")
	}
	if mapAssetType.Label != "Map" {
		t.Fatalf("expected map label, got %q", mapAssetType.Label)
	}

	vehicleAssetType, found := reportGenerationAssetTypeByID("vehicle")
	if !found {
		t.Fatalf("expected to find vehicle asset type")
	}
	if vehicleAssetType.Label != "Vehicle" {
		t.Fatalf("expected vehicle label, got %q", vehicleAssetType.Label)
	}
	if !vehicleAssetType.DisableSpatialMode {
		t.Fatalf("expected vehicle asset type to disable spatial mode")
	}
}

func TestComputePerformanceProfileForAssetTypeUsesCustomThresholds(t *testing.T) {
	customAssetType := reportGenerationAssetTypeConfig{
		ID:                        "custom",
		Label:                     "Custom",
		OversizedTextureThreshold: defaultLargeTextureThreshold,
		Thresholds: reportGenerationGradeThresholds{
			MeshComplexity:      [6]float64{10_000_000, 20_000_000, 30_000_000, 40_000_000, 50_000_000, 60_000_000},
			DuplicationWastePct: [6]float64{100, 200, 300, 400, 500, 600},
			TotalSizeMB:         [6]float64{1_000, 2_000, 3_000, 4_000, 5_000, 6_000},
			TextureSizeMB:       [6]float64{1_000, 2_000, 3_000, 4_000, 5_000, 6_000},
			MeshSizeMB:          [6]float64{1_000, 2_000, 3_000, 4_000, 5_000, 6_000},
			OversizedTextures:   [6]float64{100, 200, 300, 400, 500, 600},
			DuplicateCount:      [6]float64{100, 200, 300, 400, 500, 600},
			MeshPartCount:       [6]float64{10_000, 20_000, 30_000, 40_000, 50_000, 60_000},
			DrawCalls:           [6]float64{10_000, 20_000, 30_000, 40_000, 50_000, 60_000},
			PartCount:           [6]float64{20_000, 30_000, 40_000, 50_000, 60_000, 70_000},
			AssetDiversity:      [6]float64{10_000, 20_000, 30_000, 40_000, 50_000, 60_000},
		},
	}

	summary := reportGenerationSummary{
		TotalBytes:            800 * format.Megabyte,
		TextureBytes:          500 * format.Megabyte,
		MeshBytes:             500 * format.Megabyte,
		TriangleCount:         8_000_000,
		OversizedTextureCount: 0,
		DrawCallCount:         4000,
		UniqueAssetCount:      2000,
		MeshPartCount:         4000,
		PartCount:             10000,
	}

	grades := computePerformanceProfileForAssetType(customAssetType, reportCellPercentiles{}, summary)
	if len(grades) != 11 {
		t.Fatalf("expected 11 grades, got %d", len(grades))
	}
	for _, grade := range grades {
		if grade.Grade != gradeAPlus {
			t.Fatalf("expected all grades to use the custom generous thresholds, %q got %s", grade.Label, grade.Grade)
		}
	}
}

func TestComputeReportCellPercentilesDisableSpatialMode(t *testing.T) {
	assetType := reportGenerationAssetTypeConfig{
		ID:                 "vehicle",
		Label:              "Vehicle",
		DisableSpatialMode: true,
	}
	summary := reportGenerationSummary{
		TotalBytes:       123,
		TextureBytes:     45,
		MeshBytes:        67,
		TriangleCount:    89,
		DrawCallCount:    10,
		UniqueAssetCount: 11,
		MeshPartCount:    12,
		PartCount:        13,
	}

	percentiles := computeReportCellPercentiles(assetType, nil, summary)
	if !percentiles.WholeFileMode {
		t.Fatalf("expected whole-file mode percentiles")
	}
	if percentiles.CellCount != 1 {
		t.Fatalf("expected one synthetic cell, got %d", percentiles.CellCount)
	}
	if percentiles.P90TotalBytes != float64(summary.TotalBytes) {
		t.Fatalf("expected total bytes %.0f, got %.0f", float64(summary.TotalBytes), percentiles.P90TotalBytes)
	}
	if percentiles.P90Parts != float64(summary.PartCount) {
		t.Fatalf("expected part count %.0f, got %.0f", float64(summary.PartCount), percentiles.P90Parts)
	}
}
