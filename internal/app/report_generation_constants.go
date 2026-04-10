package app

import "strings"

const (
	reportGenerationAssetTypeMap     = "map"
	reportGenerationAssetTypeVehicle = "vehicle"
)

type reportGenerationGradeThresholds struct {
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

type reportGenerationAssetTypeConfig struct {
	ID                        string
	Label                     string
	DisableSpatialMode        bool
	OversizedTextureThreshold float64
	Thresholds                reportGenerationGradeThresholds
}

var reportGenerationAssetTypeConfigs = []reportGenerationAssetTypeConfig{
	{
		ID:                        reportGenerationAssetTypeMap,
		Label:                     "Map",
		OversizedTextureThreshold: defaultLargeTextureThreshold,
		Thresholds: reportGenerationGradeThresholds{
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
		ID:                        reportGenerationAssetTypeVehicle,
		Label:                     "Vehicle",
		DisableSpatialMode:        true,
		OversizedTextureThreshold: defaultLargeTextureThreshold,
		Thresholds: reportGenerationGradeThresholds{
			MeshComplexity:      [6]float64{75_000, 100_000, 120_000, 140_000, 160_000, 180_000},
			DuplicationWastePct: [6]float64{2, 5, 15, 25, 40, 60},
			TotalSizeMB:         [6]float64{5, 8, 10, 12, 18, 22},
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
}

func defaultReportGenerationAssetType() reportGenerationAssetTypeConfig {
	for _, config := range reportGenerationAssetTypeConfigs {
		if strings.EqualFold(config.ID, reportGenerationAssetTypeMap) {
			return config
		}
	}
	if len(reportGenerationAssetTypeConfigs) > 0 {
		return reportGenerationAssetTypeConfigs[0]
	}
	return reportGenerationAssetTypeConfig{}
}

func reportGenerationAssetTypeByID(assetTypeID string) (reportGenerationAssetTypeConfig, bool) {
	for _, config := range reportGenerationAssetTypeConfigs {
		if strings.EqualFold(config.ID, strings.TrimSpace(assetTypeID)) {
			return config, true
		}
	}
	return reportGenerationAssetTypeConfig{}, false
}
