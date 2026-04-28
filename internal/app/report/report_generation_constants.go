package report

import (
	"joxblox/internal/app/loader"
)

// GradeThresholds bucket boundaries per metric.
//
// For metrics that have a per-cell story (everything graded as "p90/cell"
// in spatial mode), the main field is the *Headline* threshold — applied
// to whole-file totals when spatial mode is disabled and to BOTH p90/cell
// and max/cell in spatial mode. The companion `*Avg` field is the
// avg/cell threshold (typically lower than headline because the average
// cell load is expected to be smaller than the worst-10% cell). Asset
// types that don't care about spatial averaging (e.g. vehicles) leave
// the `*Avg` fields zero — the grader falls back to the headline.
type GradeThresholds struct {
	MeshComplexity        [6]float64
	MeshComplexityAvg     [6]float64
	DuplicationWastePct   [6]float64
	GPUTextureMemoryMB    [6]float64
	GPUTextureMemoryMBAvg [6]float64
	MismatchedPBRMaps     [6]float64
	MeshSizeMB            [6]float64
	MeshSizeMBAvg         [6]float64
	OversizedTextures     [6]float64
	DuplicateCount        [6]float64
	MeshPartCount         [6]float64
	MeshPartCountAvg      [6]float64
	DrawCalls             [6]float64
	DrawCallsAvg          [6]float64
	PartCount             [6]float64
	PartCountAvg          [6]float64
	InstanceCount         [6]float64
	InstanceCountAvg      [6]float64
	AssetDiversity        [6]float64
	AssetDiversityAvg     [6]float64
}

type AssetTypeConfig struct {
	Label                     string
	DisableSpatialMode        bool
	OversizedTextureThreshold float64
	Thresholds                GradeThresholds
}

const VehicleOversizedTextureThreshold = 100_000

var VehicleThresholds = GradeThresholds{
	MeshComplexity:      [6]float64{75_000, 100_000, 120_000, 140_000, 160_000, 180_000},
	DuplicationWastePct: [6]float64{2, 5, 15, 25, 40, 60},
	GPUTextureMemoryMB:  [6]float64{10, 16, 20, 24, 30, 40},
	MismatchedPBRMaps:   [6]float64{1, 2, 4, 6, 10, 15},
	MeshSizeMB:          [6]float64{5, 8, 10, 12, 15, 18},
	OversizedTextures:   [6]float64{1, 3, 6, 10, 15, 25},
	DuplicateCount:      [6]float64{1, 5, 15, 40, 80, 150},
	MeshPartCount:       [6]float64{150, 175, 200, 225, 250, 275},
	DrawCalls:           [6]float64{100, 150, 200, 250, 2000, 4000},
	PartCount:           [6]float64{25, 50, 75, 100, 125, 150},
	InstanceCount:       [6]float64{500, 1_000, 2_000, 4_000, 8_000, 15_000},
	AssetDiversity:      [6]float64{125, 175, 250, 300, 350, 400},
}

var AssetTypeConfigs = []AssetTypeConfig{
	{
		Label:                     "Map",
		OversizedTextureThreshold: loader.DefaultLargeTextureThreshold,
		Thresholds: GradeThresholds{
			MeshComplexity:        [6]float64{5_000, 15_000, 20_000, 35_000, 45_000, 60_000},
			MeshComplexityAvg:     [6]float64{2_500, 7_500, 10_000, 17_500, 22_500, 30_000},
			DuplicationWastePct:   [6]float64{2, 5, 15, 25, 40, 60},
			GPUTextureMemoryMB:    [6]float64{4, 8, 12, 16, 24, 40},
			GPUTextureMemoryMBAvg: [6]float64{2, 4, 6, 8, 12, 20},
			MismatchedPBRMaps:     [6]float64{1, 2, 4, 6, 10, 15},
			MeshSizeMB:            [6]float64{1, 2, 3, 5, 10, 15},
			MeshSizeMBAvg:         [6]float64{0.5, 1, 1.5, 2.5, 5, 7.5},
			OversizedTextures:     [6]float64{1, 3, 6, 10, 15, 25},
			DuplicateCount:        [6]float64{1, 5, 15, 40, 80, 150},
			MeshPartCount:         [6]float64{100, 250, 500, 1000, 2000, 4000},
			MeshPartCountAvg:      [6]float64{50, 125, 250, 500, 1000, 2000},
			DrawCalls:             [6]float64{100, 250, 500, 1000, 2000, 4000},
			DrawCallsAvg:          [6]float64{50, 125, 250, 500, 1000, 2000},
			PartCount:             [6]float64{200, 500, 1000, 2500, 5000, 10000},
			PartCountAvg:          [6]float64{100, 250, 500, 1250, 2500, 5000},
			InstanceCount:         [6]float64{2_000, 5_000, 10_000, 25_000, 50_000, 100_000},
			InstanceCountAvg:      [6]float64{1_000, 2_500, 5_000, 12_500, 25_000, 50_000},
			AssetDiversity:        [6]float64{50, 100, 250, 500, 1000, 2000},
			AssetDiversityAvg:     [6]float64{25, 50, 125, 250, 500, 1000},
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
			MeshComplexity:      [6]float64{120_000, 140_000, 150_000, 160_000, 170_000, 180_000},
			DuplicationWastePct: VehicleThresholds.DuplicationWastePct,
			GPUTextureMemoryMB:  VehicleThresholds.GPUTextureMemoryMB,
			MismatchedPBRMaps:   VehicleThresholds.MismatchedPBRMaps,
			MeshSizeMB:          VehicleThresholds.MeshSizeMB,
			OversizedTextures:   VehicleThresholds.OversizedTextures,
			DuplicateCount:      VehicleThresholds.DuplicateCount,
			MeshPartCount:       VehicleThresholds.MeshPartCount,
			DrawCalls:           VehicleThresholds.DrawCalls,
			PartCount:           VehicleThresholds.PartCount,
			InstanceCount:       VehicleThresholds.InstanceCount,
			AssetDiversity:      VehicleThresholds.AssetDiversity,
		},
	},
}

func DefaultAssetType() AssetTypeConfig {
	if len(AssetTypeConfigs) > 0 {
		return AssetTypeConfigs[0]
	}
	return AssetTypeConfig{}
}
