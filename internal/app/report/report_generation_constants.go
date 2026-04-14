package report

import (
	"joxblox/internal/app/loader"
)

type GradeThresholds struct {
	MeshComplexity      [6]float64
	DuplicationWastePct [6]float64
	TotalSizeMB         [6]float64
	TextureSizeMB       [6]float64
	MeshSizeMB          [6]float64
	OversizedTextures   [6]float64
	DuplicateCount      [6]float64
	MeshPartCount       [6]float64
	DrawCalls           [6]float64
	PartCount           [6]float64
	AssetDiversity      [6]float64
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
	TotalSizeMB:         [6]float64{10, 12, 15, 18, 22, 28},
	TextureSizeMB:       [6]float64{5, 8, 10, 12, 15, 20},
	MeshSizeMB:          [6]float64{5, 8, 10, 12, 15, 18},
	OversizedTextures:   [6]float64{1, 3, 6, 10, 15, 25},
	DuplicateCount:      [6]float64{1, 5, 15, 40, 80, 150},
	MeshPartCount:       [6]float64{150, 175, 200, 225, 250, 275},
	DrawCalls:           [6]float64{100, 150, 200, 250, 2000, 4000},
	PartCount:           [6]float64{25, 50, 75, 100, 125, 150},
	AssetDiversity:      [6]float64{125, 175, 250, 300, 350, 400},
}

var AssetTypeConfigs = []AssetTypeConfig{
	{
		Label:                     "Map",
		OversizedTextureThreshold: loader.DefaultLargeTextureThreshold,
		Thresholds: GradeThresholds{
			MeshComplexity:      [6]float64{5_000, 15_000, 20_000, 35_000, 45_000, 60_000},
			DuplicationWastePct: [6]float64{2, 5, 15, 25, 40, 60},
			TotalSizeMB:         [6]float64{2, 5, 8, 12, 20, 30},
			TextureSizeMB:       [6]float64{2, 4, 6, 8, 12, 20},
			MeshSizeMB:          [6]float64{1, 2, 3, 5, 10, 15},
			OversizedTextures:   [6]float64{1, 3, 6, 10, 15, 25},
			DuplicateCount:      [6]float64{1, 5, 15, 40, 80, 150},
			MeshPartCount:       [6]float64{100, 250, 500, 1000, 2000, 4000},
			DrawCalls:           [6]float64{100, 250, 500, 1000, 2000, 4000},
			PartCount:           [6]float64{200, 500, 1000, 2500, 5000, 10000},
			AssetDiversity:      [6]float64{50, 100, 250, 500, 1000, 2000},
		},
	},
	{
		Label:                     "Vehicle: Basic",
		DisableSpatialMode:        true,
		OversizedTextureThreshold: VehicleOversizedTextureThreshold,
		Thresholds: VehicleThresholds,
	},
	{
		Label:                     "Vehicle: Super-car",
		DisableSpatialMode:        true,
		OversizedTextureThreshold: VehicleOversizedTextureThreshold,
		Thresholds: GradeThresholds{
			MeshComplexity: [6]float64{120_000, 140_000, 150_000, 160_000, 170_000, 180_000},
			DuplicationWastePct: VehicleThresholds.DuplicationWastePct,
			TotalSizeMB: VehicleThresholds.TotalSizeMB,
			TextureSizeMB: VehicleThresholds.TextureSizeMB,
			MeshSizeMB: VehicleThresholds.MeshSizeMB,
			OversizedTextures: VehicleThresholds.OversizedTextures,
			DuplicateCount: VehicleThresholds.DuplicateCount,
			MeshPartCount: VehicleThresholds.MeshPartCount,
			DrawCalls: VehicleThresholds.DrawCalls,
			PartCount: VehicleThresholds.PartCount,
			AssetDiversity: VehicleThresholds.AssetDiversity,
		},
	},


}

func DefaultAssetType() AssetTypeConfig {
	if len(AssetTypeConfigs) > 0 {
		return AssetTypeConfigs[0]
	}
	return AssetTypeConfig{}
}
