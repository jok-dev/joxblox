package report

import (
	"joxblox/internal/app/loader"
)

// GradeThresholds bucket boundaries per metric.
//
// For metrics with a per-cell story, paired `*Max` / `*Typical` fields
// drive grading in spatial mode: `*Max` is graded against p90/cell (the
// "how bad is one cell allowed to get" ceiling), `*Typical` against the
// content-weighted average cell. In whole-file mode (no spatial data)
// the scalar total is graded against `*Max`. Asset types that don't
// care about spatial averaging (e.g. vehicles) leave `*Typical` zero —
// the grader falls back to `*Max` for both.
//
// Whole-scene metrics with no cell story (e.g. `DuplicationWastePct`,
// `OversizedTextures`) only have a single threshold field.
type GradeThresholds struct {
	MeshComplexityMax         [6]float64
	MeshComplexityTypical     [6]float64
	DuplicationWastePct       [6]float64
	GPUTextureMemoryMBMax     [6]float64
	GPUTextureMemoryMBTypical [6]float64
	MismatchedPBRMaps         [6]float64
	MeshSizeMBMax             [6]float64
	MeshSizeMBTypical         [6]float64
	OversizedTextures         [6]float64
	DuplicateCount            [6]float64
	MeshPartCountMax          [6]float64
	MeshPartCountTypical      [6]float64
	DrawCallsMax              [6]float64
	DrawCallsTypical          [6]float64
	PartCountMax              [6]float64
	PartCountTypical          [6]float64
	InstanceCountMax          [6]float64
	InstanceCountTypical      [6]float64
	AssetDiversityMax         [6]float64
	AssetDiversityTypical     [6]float64
}

type AssetTypeConfig struct {
	Label                     string
	DisableSpatialMode        bool
	OversizedTextureThreshold float64
	// BannedTextureSizeMB is a hard cap on individual texture GPU bytes;
	// any texture exceeding it forces the overall grade to F. Zero
	// disables the ban. Per-slot classification matters because normal
	// maps are always BC3 (2× a BC1 of the same dimensions).
	BannedTextureSizeMB float64
	Thresholds          GradeThresholds
}

const VehicleOversizedTextureThreshold = 100_000

var VehicleThresholds = GradeThresholds{
	MeshComplexityMax:     [6]float64{75_000, 100_000, 120_000, 140_000, 160_000, 180_000},
	DuplicationWastePct:   [6]float64{2, 5, 15, 25, 40, 60},
	GPUTextureMemoryMBMax: [6]float64{10, 16, 20, 24, 30, 40},
	MismatchedPBRMaps:     [6]float64{1, 2, 4, 6, 10, 15},
	MeshSizeMBMax:         [6]float64{5, 8, 10, 12, 15, 18},
	OversizedTextures:     [6]float64{1, 3, 6, 10, 15, 25},
	DuplicateCount:        [6]float64{1, 5, 15, 40, 80, 150},
	MeshPartCountMax:      [6]float64{150, 175, 200, 225, 250, 275},
	DrawCallsMax:          [6]float64{100, 150, 200, 250, 2000, 4000},
	PartCountMax:          [6]float64{25, 50, 75, 100, 125, 150},
	InstanceCountMax:      [6]float64{500, 1_000, 2_000, 4_000, 8_000, 15_000},
	AssetDiversityMax:     [6]float64{125, 175, 250, 300, 350, 400},
}

var AssetTypeConfigs = []AssetTypeConfig{
	{
		Label:                     "DE Map",
		OversizedTextureThreshold: loader.DefaultLargeTextureThreshold,
		BannedTextureSizeMB:       1.34,
		Thresholds: GradeThresholds{
			MeshComplexityMax:         [6]float64{5_000, 15_000, 20_000, 35_000, 45_000, 60_000},
			MeshComplexityTypical:     [6]float64{2_500, 7_500, 10_000, 17_500, 22_500, 30_000},
			DuplicationWastePct:       [6]float64{2, 5, 15, 25, 40, 60},
			GPUTextureMemoryMBMax:     [6]float64{4, 8, 12, 16, 24, 40},
			GPUTextureMemoryMBTypical: [6]float64{2, 4, 6, 8, 12, 20},
			MismatchedPBRMaps:         [6]float64{1, 2, 4, 6, 10, 15},
			MeshSizeMBMax:             [6]float64{1, 2, 3, 5, 10, 15},
			MeshSizeMBTypical:         [6]float64{0.5, 1, 1.5, 2.5, 5, 7.5},
			OversizedTextures:         [6]float64{1, 3, 6, 10, 15, 25},
			DuplicateCount:            [6]float64{1, 5, 15, 40, 80, 150},
			MeshPartCountMax:          [6]float64{100, 250, 500, 1000, 2000, 4000},
			MeshPartCountTypical:      [6]float64{50, 125, 250, 500, 1000, 2000},
			DrawCallsMax:              [6]float64{100, 250, 500, 1000, 2000, 4000},
			DrawCallsTypical:          [6]float64{50, 125, 250, 500, 1000, 2000},
			PartCountMax:              [6]float64{200, 500, 1000, 2500, 5000, 10000},
			PartCountTypical:          [6]float64{100, 250, 500, 1250, 2500, 5000},
			InstanceCountMax:          [6]float64{2_000, 5_000, 10_000, 25_000, 50_000, 100_000},
			InstanceCountTypical:      [6]float64{1_000, 2_500, 5_000, 12_500, 25_000, 50_000},
			AssetDiversityMax:         [6]float64{50, 100, 250, 500, 1000, 2000},
			AssetDiversityTypical:     [6]float64{25, 50, 125, 250, 500, 1000},
		},
	},
	{
		Label:                     "Vehicle: Basic",
		DisableSpatialMode:        true,
		OversizedTextureThreshold: VehicleOversizedTextureThreshold,
		Thresholds:                VehicleThresholds,
	},
	{
		Label:                     "Vehicle: Super-car",
		DisableSpatialMode:        true,
		OversizedTextureThreshold: VehicleOversizedTextureThreshold,
		Thresholds: GradeThresholds{
			MeshComplexityMax:     [6]float64{120_000, 140_000, 150_000, 160_000, 170_000, 180_000},
			DuplicationWastePct:   VehicleThresholds.DuplicationWastePct,
			GPUTextureMemoryMBMax: VehicleThresholds.GPUTextureMemoryMBMax,
			MismatchedPBRMaps:     VehicleThresholds.MismatchedPBRMaps,
			MeshSizeMBMax:         VehicleThresholds.MeshSizeMBMax,
			OversizedTextures:     VehicleThresholds.OversizedTextures,
			DuplicateCount:        VehicleThresholds.DuplicateCount,
			MeshPartCountMax:      VehicleThresholds.MeshPartCountMax,
			DrawCallsMax:          VehicleThresholds.DrawCallsMax,
			PartCountMax:          VehicleThresholds.PartCountMax,
			InstanceCountMax:      VehicleThresholds.InstanceCountMax,
			AssetDiversityMax:     VehicleThresholds.AssetDiversityMax,
		},
	},
}

func DefaultAssetType() AssetTypeConfig {
	if len(AssetTypeConfigs) > 0 {
		return AssetTypeConfigs[0]
	}
	return AssetTypeConfig{}
}
