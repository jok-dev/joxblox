package report

import (
	"fmt"
	"math"
	"sort"

	"joxblox/internal/format"
	"joxblox/internal/heatmap"
)

type PerformanceGrade struct {
	Grade             string
	Score             float64
	Label             string
	Value             string
	TotalValue        string
	Description       string
	MetricDescription string
}

const (
	gradeAPlus                 = "A+"
	gradeA                     = "A"
	gradeB                     = "B"
	gradeC                     = "C"
	gradeD                     = "D"
	gradeE                     = "E"
	gradeF                     = "F"
	reportGenerationPercentile = 0.9
)

type CellPercentiles struct {
	P90TotalBytes    float64
	P90TextureBytes  float64
	P90MeshBytes     float64
	P90TriangleCount float64
	P90UniqueAssets  float64
	P90MeshParts     float64
	P90Parts         float64
	P90DrawCalls     float64
	CellCount        int
	CellSizeStuds    float64
	WholeFileMode    bool
}

func ComputeCellPercentiles(cells []heatmap.Cell) CellPercentiles {
	if len(cells) == 0 {
		return CellPercentiles{}
	}

	occupied := 0
	totalBytesValues := make([]float64, 0, len(cells))
	textureBytesValues := make([]float64, 0, len(cells))
	meshBytesValues := make([]float64, 0, len(cells))
	triangleCountValues := make([]float64, 0, len(cells))
	uniqueAssetValues := make([]float64, 0, len(cells))
	meshPartValues := make([]float64, 0, len(cells))
	partValues := make([]float64, 0, len(cells))
	drawCallValues := make([]float64, 0, len(cells))
	cellSize := 0.0

	for _, cell := range cells {
		if cell.Stats.MeshPartCount > 0 {
			meshPartValues = append(meshPartValues, float64(cell.Stats.MeshPartCount))
		}
		if cell.Stats.PartCount > 0 {
			partValues = append(partValues, float64(cell.Stats.PartCount))
		}
		if cell.Stats.DrawCallCount > 0 {
			drawCallValues = append(drawCallValues, float64(cell.Stats.DrawCallCount))
		}
		if cell.Stats.ReferenceCount <= 0 {
			if cellSize == 0 && (cell.Stats.MeshPartCount > 0 || cell.Stats.PartCount > 0 || cell.Stats.DrawCallCount > 0) {
				cellSize = cell.MaximumX - cell.MinimumX
			}
			continue
		}
		occupied++
		totalBytesValues = append(totalBytesValues, float64(cell.Stats.TotalBytes))
		textureBytesValues = append(textureBytesValues, float64(cell.Stats.TextureBytes))
		meshBytesValues = append(meshBytesValues, float64(cell.Stats.MeshBytes))
		triangleCountValues = append(triangleCountValues, float64(cell.Stats.TriangleCount))
		uniqueAssetValues = append(uniqueAssetValues, float64(cell.Stats.UniqueAssetCount))
		if cellSize == 0 {
			cellSize = cell.MaximumX - cell.MinimumX
		}
	}

	if occupied == 0 && len(meshPartValues) == 0 && len(partValues) == 0 && len(drawCallValues) == 0 {
		return CellPercentiles{}
	}

	return CellPercentiles{
		P90TotalBytes:    PercentileFloat64(totalBytesValues, reportGenerationPercentile),
		P90TextureBytes:  PercentileFloat64(textureBytesValues, reportGenerationPercentile),
		P90MeshBytes:     PercentileFloat64(meshBytesValues, reportGenerationPercentile),
		P90TriangleCount: PercentileFloat64(triangleCountValues, reportGenerationPercentile),
		P90UniqueAssets:  PercentileFloat64(uniqueAssetValues, reportGenerationPercentile),
		P90MeshParts:     PercentileFloat64(meshPartValues, reportGenerationPercentile),
		P90Parts:         PercentileFloat64(partValues, reportGenerationPercentile),
		P90DrawCalls:     PercentileFloat64(drawCallValues, reportGenerationPercentile),
		CellCount:        occupied,
		CellSizeStuds:    cellSize,
	}
}

func ComputeReportCellPercentiles(assetType AssetTypeConfig, cells []heatmap.Cell, summary Summary) CellPercentiles {
	if assetType.DisableSpatialMode {
		return CellPercentiles{
			P90TotalBytes:    float64(summary.TotalBytes),
			P90TextureBytes:  float64(summary.TextureBytes),
			P90MeshBytes:     float64(summary.MeshBytes),
			P90TriangleCount: float64(summary.TriangleCount),
			P90UniqueAssets:  float64(summary.UniqueAssetCount),
			P90MeshParts:     float64(summary.MeshPartCount),
			P90Parts:         float64(summary.PartCount),
			P90DrawCalls:     float64(summary.DrawCallCount),
			CellCount:        1,
			WholeFileMode:    true,
		}
	}
	return ComputeCellPercentiles(cells)
}

func PercentileFloat64(values []float64, percentile float64) float64 {
	if len(values) == 0 {
		return 0
	}

	sortedValues := append([]float64(nil), values...)
	sort.Float64s(sortedValues)

	switch {
	case percentile <= 0:
		return sortedValues[0]
	case percentile >= 1:
		return sortedValues[len(sortedValues)-1]
	}

	index := int(math.Ceil(percentile*float64(len(sortedValues)))) - 1
	if index < 0 {
		index = 0
	}
	if index >= len(sortedValues) {
		index = len(sortedValues) - 1
	}

	return sortedValues[index]
}

func ComputePerformanceProfile(percentiles CellPercentiles, summary Summary) []PerformanceGrade {
	return ComputePerformanceProfileForAssetType(DefaultAssetType(), percentiles, summary)
}

func ComputePerformanceProfileForAssetType(assetType AssetTypeConfig, percentiles CellPercentiles, summary Summary) []PerformanceGrade {
	useCellPercentiles := percentiles.CellCount > 0
	thresholds := assetType.Thresholds

	grades := []PerformanceGrade{
		ComputeDownloadSizeGradeWithThresholds(summary.TotalBytes, percentiles.P90TotalBytes, useCellPercentiles, thresholds.TotalSizeMB),
		ComputeTextureSizeGradeWithThresholds(summary.TextureBytes, percentiles.P90TextureBytes, useCellPercentiles, thresholds.TextureSizeMB),
		ComputeMeshSizeGradeWithThresholds(summary.MeshBytes, percentiles.P90MeshBytes, useCellPercentiles, thresholds.MeshSizeMB),
		ComputeOversizedTextureCountGradeWithThresholds(summary.OversizedTextureCount, thresholds.OversizedTextures),
		ComputeMeshComplexityGradeWithThresholds(summary.TriangleCount, percentiles.P90TriangleCount, useCellPercentiles, thresholds.MeshComplexity),
		ComputeDrawCallGradeWithThresholds(summary.DrawCallCount, percentiles.P90DrawCalls, useCellPercentiles, thresholds.DrawCalls),
		ComputeMeshPartCountGradeWithThresholds(summary.MeshPartCount, percentiles.P90MeshParts, useCellPercentiles, thresholds.MeshPartCount),
		ComputePartCountGradeWithThresholds(summary.PartCount, percentiles.P90Parts, useCellPercentiles, thresholds.PartCount),
		ComputeAssetDiversityGradeWithThresholds(summary.UniqueAssetCount, percentiles.P90UniqueAssets, useCellPercentiles, thresholds.AssetDiversity),
	}

	dupCount := ComputeDuplicateCountGradeWithThresholds(summary.DuplicateCount, thresholds.DuplicateCount)
	dupWaste := ComputeDuplicationWasteGradeWithThresholds(summary.DuplicateSizeBytes, summary.TotalBytes, thresholds.DuplicationWastePct)
	if summary.DuplicateCount > 0 {
		dupCount.Grade = CapGradeAtC(dupCount.Grade)
		dupWaste.Grade = CapGradeAtC(dupWaste.Grade)
	}
	grades = append(grades, dupCount, dupWaste)
	return grades
}

func OverallPerformanceGrade(grades []PerformanceGrade, hasDuplicates bool) string {
	avg := OverallPerformanceNumericAverage(grades)
	grade := NumericToGrade(int(math.Round(avg)))
	if hasDuplicates && GradeToNumeric(grade) > GradeToNumeric(gradeB) {
		return gradeB
	}
	return grade
}

func OverallPerformanceScorePercent(grades []PerformanceGrade, hasDuplicates bool) int {
	avg := overallContinuousAverage(grades)
	if hasDuplicates && avg > float64(GradeToNumeric(gradeB)) {
		avg = float64(GradeToNumeric(gradeB))
	}
	return int(math.Round((avg / float64(GradeToNumeric(gradeAPlus))) * 100))
}

func OverallPerformanceNumericAverage(grades []PerformanceGrade) float64 {
	if len(grades) == 0 {
		return 0
	}

	total := 0
	for _, g := range grades {
		total += GradeToNumeric(g.Grade)
	}
	return float64(total) / float64(len(grades))
}

func overallContinuousAverage(grades []PerformanceGrade) float64 {
	if len(grades) == 0 {
		return 0
	}
	total := 0.0
	for _, g := range grades {
		if g.Score > 0 || g.Grade == gradeF {
			total += g.Score
		} else {
			total += float64(GradeToNumeric(g.Grade))
		}
	}
	return total / float64(len(grades))
}

func ComputeMeshComplexityGrade(triangleCount int64, percentilePerCell float64, useCellPercentile bool) PerformanceGrade {
	return ComputeMeshComplexityGradeWithThresholds(triangleCount, percentilePerCell, useCellPercentile, DefaultAssetType().Thresholds.MeshComplexity)
}

func ComputeMeshComplexityGradeWithThresholds(triangleCount int64, percentilePerCell float64, useCellPercentile bool, thresholds [6]float64) PerformanceGrade {
	gradeValue := float64(triangleCount)
	totalLabel := ""
	if useCellPercentile {
		gradeValue = percentilePerCell
		totalLabel = format.FormatIntCommas(int64(percentilePerCell)) + " p90/cell"
	}
	grade := GradeFromThresholds(gradeValue, thresholds)
	return PerformanceGrade{
		Grade:             grade,
		Score:             ContinuousScoreFromThresholds(gradeValue, thresholds),
		Label:             "Mesh Complexity",
		Value:             format.FormatIntCommas(triangleCount) + " tris",
		TotalValue:        totalLabel,
		Description:       MeshGradeDescription(grade),
		MetricDescription: "Total triangle count across all MeshParts in the scene",
	}
}

func ComputeDuplicationWasteGrade(duplicateSizeBytes int64, totalBytes int64) PerformanceGrade {
	return ComputeDuplicationWasteGradeWithThresholds(duplicateSizeBytes, totalBytes, DefaultAssetType().Thresholds.DuplicationWastePct)
}

func ComputeDuplicationWasteGradeWithThresholds(duplicateSizeBytes int64, totalBytes int64, thresholds [6]float64) PerformanceGrade {
	percentage := 0.0
	if totalBytes > 0 {
		percentage = float64(duplicateSizeBytes) / float64(totalBytes) * 100.0
	}
	grade := GradeFromThresholds(percentage, thresholds)
	return PerformanceGrade{
		Grade:             grade,
		Score:             ContinuousScoreFromThresholds(percentage, thresholds),
		Label:             "Duplication Waste",
		Value:             fmt.Sprintf("%.1f%%", percentage),
		Description:       DuplicationGradeDescription(grade),
		MetricDescription: "Percentage of total size wasted by duplicate assets",
	}
}

func ComputeDownloadSizeGrade(totalBytes int64, percentilePerCell float64, useCellPercentile bool) PerformanceGrade {
	return ComputeDownloadSizeGradeWithThresholds(totalBytes, percentilePerCell, useCellPercentile, DefaultAssetType().Thresholds.TotalSizeMB)
}

func ComputeDownloadSizeGradeWithThresholds(totalBytes int64, percentilePerCell float64, useCellPercentile bool, thresholds [6]float64) PerformanceGrade {
	gradeValue := float64(totalBytes) / float64(format.Megabyte)
	totalLabel := ""
	if useCellPercentile {
		gradeValue = percentilePerCell / float64(format.Megabyte)
		totalLabel = format.FormatSizeAuto64(int64(percentilePerCell)) + " p90/cell"
	}
	grade := GradeFromThresholds(gradeValue, thresholds)
	return PerformanceGrade{
		Grade:             grade,
		Score:             ContinuousScoreFromThresholds(gradeValue, thresholds),
		Label:             "Total Size",
		Value:             format.FormatSizeAuto64(totalBytes),
		TotalValue:        totalLabel,
		Description:       DownloadSizeGradeDescription(grade),
		MetricDescription: "Sum of all asset data (meshes + textures) that must be downloaded",
	}
}

func ComputeTextureSizeGrade(textureBytes int64, percentilePerCell float64, useCellPercentile bool) PerformanceGrade {
	return ComputeTextureSizeGradeWithThresholds(textureBytes, percentilePerCell, useCellPercentile, DefaultAssetType().Thresholds.TextureSizeMB)
}

func ComputeTextureSizeGradeWithThresholds(textureBytes int64, percentilePerCell float64, useCellPercentile bool, thresholds [6]float64) PerformanceGrade {
	gradeValue := float64(textureBytes) / float64(format.Megabyte)
	totalLabel := ""
	if useCellPercentile {
		gradeValue = percentilePerCell / float64(format.Megabyte)
		totalLabel = format.FormatSizeAuto64(int64(percentilePerCell)) + " p90/cell"
	}
	grade := GradeFromThresholds(gradeValue, thresholds)
	return PerformanceGrade{
		Grade:             grade,
		Score:             ContinuousScoreFromThresholds(gradeValue, thresholds),
		Label:             "Texture Size",
		Value:             format.FormatSizeAuto64(textureBytes),
		TotalValue:        totalLabel,
		Description:       TextureSizeGradeDescription(grade),
		MetricDescription: "Total size of all image/texture assets in the scene",
	}
}

func ComputeMeshSizeGrade(meshBytes int64, percentilePerCell float64, useCellPercentile bool) PerformanceGrade {
	return ComputeMeshSizeGradeWithThresholds(meshBytes, percentilePerCell, useCellPercentile, DefaultAssetType().Thresholds.MeshSizeMB)
}

func ComputeMeshSizeGradeWithThresholds(meshBytes int64, percentilePerCell float64, useCellPercentile bool, thresholds [6]float64) PerformanceGrade {
	gradeValue := float64(meshBytes) / float64(format.Megabyte)
	totalLabel := ""
	if useCellPercentile {
		gradeValue = percentilePerCell / float64(format.Megabyte)
		totalLabel = format.FormatSizeAuto64(int64(percentilePerCell)) + " p90/cell"
	}
	grade := GradeFromThresholds(gradeValue, thresholds)
	return PerformanceGrade{
		Grade:             grade,
		Score:             ContinuousScoreFromThresholds(gradeValue, thresholds),
		Label:             "Mesh Size",
		Value:             format.FormatSizeAuto64(meshBytes),
		TotalValue:        totalLabel,
		Description:       MeshSizeGradeDescription(grade),
		MetricDescription: "Total size of all mesh geometry data in the scene",
	}
}

func ComputeOversizedTextureCountGrade(oversizedTextureCount int) PerformanceGrade {
	return ComputeOversizedTextureCountGradeWithThresholds(oversizedTextureCount, DefaultAssetType().Thresholds.OversizedTextures)
}

func ComputeOversizedTextureCountGradeWithThresholds(oversizedTextureCount int, thresholds [6]float64) PerformanceGrade {
	gradeValue := float64(oversizedTextureCount)
	grade := GradeFromThresholds(gradeValue, thresholds)
	return PerformanceGrade{
		Grade:             grade,
		Score:             ContinuousScoreFromThresholds(gradeValue, thresholds),
		Label:             "Oversized Textures",
		Value:             fmt.Sprintf("%d textures", oversizedTextureCount),
		Description:       OversizedTextureCountGradeDescription(grade),
		MetricDescription: "Textures larger than optimal for their on-screen surface area",
	}
}

func ComputeDuplicateCountGrade(duplicateCount int64) PerformanceGrade {
	return ComputeDuplicateCountGradeWithThresholds(duplicateCount, DefaultAssetType().Thresholds.DuplicateCount)
}

func ComputeDuplicateCountGradeWithThresholds(duplicateCount int64, thresholds [6]float64) PerformanceGrade {
	gradeValue := float64(duplicateCount)
	grade := GradeFromThresholds(gradeValue, thresholds)
	return PerformanceGrade{
		Grade:             grade,
		Score:             ContinuousScoreFromThresholds(gradeValue, thresholds),
		Label:             "Duplicates",
		Value:             format.FormatIntCommas(duplicateCount) + " duplicates",
		Description:       DuplicateCountGradeDescription(grade),
		MetricDescription: "Assets uploaded multiple times with identical content",
	}
}

func ComputeMeshPartCountGrade(meshPartCount int, percentilePerCell float64, useCellPercentile bool) PerformanceGrade {
	return ComputeMeshPartCountGradeWithThresholds(meshPartCount, percentilePerCell, useCellPercentile, DefaultAssetType().Thresholds.MeshPartCount)
}

func ComputeMeshPartCountGradeWithThresholds(meshPartCount int, percentilePerCell float64, useCellPercentile bool, thresholds [6]float64) PerformanceGrade {
	gradeValue := float64(meshPartCount)
	totalLabel := ""
	if useCellPercentile {
		gradeValue = percentilePerCell
		totalLabel = fmt.Sprintf("%.0f p90/cell", percentilePerCell)
	}
	grade := GradeFromThresholds(gradeValue, thresholds)
	return PerformanceGrade{
		Grade:             grade,
		Score:             ContinuousScoreFromThresholds(gradeValue, thresholds),
		Label:             "MeshParts",
		Value:             fmt.Sprintf("%d", meshPartCount),
		TotalValue:        totalLabel,
		Description:       MeshPartCountGradeDescription(grade),
		MetricDescription: "Count of MeshPart instances in the scene",
	}
}

func ComputeDrawCallGrade(drawCallCount int64, percentilePerCell float64, useCellPercentile bool) PerformanceGrade {
	return ComputeDrawCallGradeWithThresholds(drawCallCount, percentilePerCell, useCellPercentile, DefaultAssetType().Thresholds.DrawCalls)
}

func ComputeDrawCallGradeWithThresholds(drawCallCount int64, percentilePerCell float64, useCellPercentile bool, thresholds [6]float64) PerformanceGrade {
	gradeValue := float64(drawCallCount)
	totalLabel := ""
	if useCellPercentile {
		gradeValue = percentilePerCell
		totalLabel = fmt.Sprintf("%.0f p90/cell", percentilePerCell)
	}
	grade := GradeFromThresholds(gradeValue, thresholds)
	return PerformanceGrade{
		Grade:             grade,
		Score:             ContinuousScoreFromThresholds(gradeValue, thresholds),
		Label:             "Draw Calls",
		Value:             fmt.Sprintf("%d est.", drawCallCount),
		TotalValue:        totalLabel,
		Description:       DrawCallGradeDescription(grade),
		MetricDescription: "Estimated GPU draw calls based on unique mesh/texture/material combos",
	}
}

func ComputePartCountGrade(partCount int, percentilePerCell float64, useCellPercentile bool) PerformanceGrade {
	return ComputePartCountGradeWithThresholds(partCount, percentilePerCell, useCellPercentile, DefaultAssetType().Thresholds.PartCount)
}

func ComputePartCountGradeWithThresholds(partCount int, percentilePerCell float64, useCellPercentile bool, thresholds [6]float64) PerformanceGrade {
	gradeValue := float64(partCount)
	totalLabel := ""
	if useCellPercentile {
		gradeValue = percentilePerCell
		totalLabel = fmt.Sprintf("%.0f p90/cell", percentilePerCell)
	}
	grade := GradeFromThresholds(gradeValue, thresholds)
	return PerformanceGrade{
		Grade:             grade,
		Score:             ContinuousScoreFromThresholds(gradeValue, thresholds),
		Label:             "Parts",
		Value:             fmt.Sprintf("%d", partCount),
		TotalValue:        totalLabel,
		Description:       PartCountGradeDescription(grade),
		MetricDescription: "Count of Part instances (legacy bricks) in the scene",
	}
}

func ComputeAssetDiversityGrade(uniqueAssetCount int, percentilePerCell float64, useCellPercentile bool) PerformanceGrade {
	return ComputeAssetDiversityGradeWithThresholds(uniqueAssetCount, percentilePerCell, useCellPercentile, DefaultAssetType().Thresholds.AssetDiversity)
}

func ComputeAssetDiversityGradeWithThresholds(uniqueAssetCount int, percentilePerCell float64, useCellPercentile bool, thresholds [6]float64) PerformanceGrade {
	gradeValue := float64(uniqueAssetCount)
	totalLabel := ""
	if useCellPercentile {
		gradeValue = percentilePerCell
		totalLabel = fmt.Sprintf("%.0f p90/cell", percentilePerCell)
	}
	grade := GradeFromThresholds(gradeValue, thresholds)
	return PerformanceGrade{
		Grade:             grade,
		Score:             ContinuousScoreFromThresholds(gradeValue, thresholds),
		Label:             "Asset Diversity",
		Value:             fmt.Sprintf("%d unique", uniqueAssetCount),
		TotalValue:        totalLabel,
		Description:       AssetDiversityGradeDescription(grade),
		MetricDescription: "Number of unique assets that must be fetched from CDN",
	}
}

// GradeFromThresholds defines 7 grade buckets: A+, A, B, C, D, E, F.
// Values below thresholds[0] get A+; values >= thresholds[5] get F.
func GradeFromThresholds(value float64, thresholds [6]float64) string {
	switch {
	case value < thresholds[0]:
		return gradeAPlus
	case value < thresholds[1]:
		return gradeA
	case value < thresholds[2]:
		return gradeB
	case value < thresholds[3]:
		return gradeC
	case value < thresholds[4]:
		return gradeD
	case value < thresholds[5]:
		return gradeE
	default:
		return gradeF
	}
}

// ContinuousScoreFromThresholds returns a float in [0, 6] that linearly
// interpolates within each grade bucket, giving a granular score instead
// of snapping to a discrete integer.
func ContinuousScoreFromThresholds(value float64, thresholds [6]float64) float64 {
	if value <= 0 {
		return 6.0
	}
	boundaries := [7]float64{0, thresholds[0], thresholds[1], thresholds[2], thresholds[3], thresholds[4], thresholds[5]}
	for i := 1; i < len(boundaries); i++ {
		if value < boundaries[i] {
			t := (value - boundaries[i-1]) / (boundaries[i] - boundaries[i-1])
			return float64(7-i) - t
		}
	}
	return 0.0
}

func CapGradeAtC(grade string) string {
	if GradeToNumeric(grade) > GradeToNumeric(gradeC) {
		return gradeC
	}
	return grade
}

func GradeToNumeric(grade string) int {
	switch grade {
	case gradeAPlus:
		return 6
	case gradeA:
		return 5
	case gradeB:
		return 4
	case gradeC:
		return 3
	case gradeD:
		return 2
	case gradeE:
		return 1
	default:
		return 0
	}
}

func NumericToGrade(value int) string {
	switch {
	case value >= 6:
		return gradeAPlus
	case value >= 5:
		return gradeA
	case value >= 4:
		return gradeB
	case value >= 3:
		return gradeC
	case value >= 2:
		return gradeD
	case value >= 1:
		return gradeE
	default:
		return gradeF
	}
}

func MeshGradeDescription(grade string) string {
	switch grade {
	case gradeAPlus:
		return "Minimal polygon count, excellent rendering performance"
	case gradeA:
		return "Low polygon count, great rendering performance"
	case gradeB:
		return "Moderate polygon count, good performance"
	case gradeC:
		return "Elevated polygon count, may impact frame rate"
	case gradeD:
		return "High polygon count, noticeable frame rate impact"
	case gradeE:
		return "Very high polygon count, significant frame rate impact"
	default:
		return "Extreme polygon count, severe frame rate impact"
	}
}

func DuplicationGradeDescription(grade string) string {
	switch grade {
	case gradeAPlus:
		return "No meaningful duplication, assets are perfectly consolidated"
	case gradeA:
		return "Minimal duplication, assets are well-consolidated"
	case gradeB:
		return "Some duplicated assets, minor waste"
	case gradeC:
		return "Notable duplication, consolidation recommended"
	case gradeD:
		return "Significant duplication, wasting memory and bandwidth"
	case gradeE:
		return "Severe duplication, major optimization opportunity"
	default:
		return "Extreme duplication, most assets are redundant"
	}
}

func DownloadSizeGradeDescription(grade string) string {
	switch grade {
	case gradeAPlus:
		return "Tiny download footprint, near-instant load"
	case gradeA:
		return "Small download footprint, fast initial load"
	case gradeB:
		return "Moderate download size, acceptable load times"
	case gradeC:
		return "Large download size, slower initial experience"
	case gradeD:
		return "Very large download, long wait for players"
	case gradeE:
		return "Extremely large download, players may abandon loading"
	default:
		return "Massive download, likely to drive players away"
	}
}

func TextureSizeGradeDescription(grade string) string {
	switch grade {
	case gradeAPlus:
		return "Minimal texture payload, negligible bandwidth impact"
	case gradeA:
		return "Lightweight textures, minimal bandwidth impact"
	case gradeB:
		return "Moderate texture payload, acceptable for most experiences"
	case gradeC:
		return "Heavy texture payload, consider compressing or downscaling"
	case gradeD:
		return "Very heavy textures, significant download and memory cost"
	case gradeE:
		return "Extreme texture weight, major optimization needed"
	default:
		return "Massive texture payload, severely impacts all platforms"
	}
}

func MeshSizeGradeDescription(grade string) string {
	switch grade {
	case gradeAPlus:
		return "Minimal mesh data, negligible download cost"
	case gradeA:
		return "Lightweight mesh data, fast to download"
	case gradeB:
		return "Moderate mesh payload, reasonable for most experiences"
	case gradeC:
		return "Heavy mesh payload, consider simplifying geometry"
	case gradeD:
		return "Very heavy mesh data, impacting download and streaming"
	case gradeE:
		return "Extreme mesh weight, major optimization needed"
	default:
		return "Massive mesh payload, severely impacts all platforms"
	}
}

func DuplicateCountGradeDescription(grade string) string {
	switch grade {
	case gradeAPlus:
		return "No duplicates, assets are perfectly managed"
	case gradeA:
		return "Very few duplicates, assets are well-managed"
	case gradeB:
		return "Some duplicate assets, minor consolidation opportunity"
	case gradeC:
		return "Many duplicate assets, consolidation recommended"
	case gradeD:
		return "High duplication, significant instancing opportunities missed"
	case gradeE:
		return "Severe duplication, many assets uploaded multiple times"
	default:
		return "Extreme duplication, asset management overhaul needed"
	}
}

func OversizedTextureCountGradeDescription(grade string) string {
	switch grade {
	case gradeAPlus:
		return "No oversized textures detected"
	case gradeA:
		return "Very few oversized textures, low texture waste"
	case gradeB:
		return "Some oversized textures, minor optimization opportunity"
	case gradeC:
		return "Several oversized textures, review texture density"
	case gradeD:
		return "Many oversized textures, significant memory waste"
	case gradeE:
		return "Very high oversized texture count, optimization strongly recommended"
	default:
		return "Extreme oversized texture count, major texture optimization needed"
	}
}

func MeshPartCountGradeDescription(grade string) string {
	switch grade {
	case gradeAPlus:
		return "Very few MeshParts, minimal rendering overhead"
	case gradeA:
		return "Low MeshPart count, efficient scene"
	case gradeB:
		return "Moderate MeshParts, reasonable for most experiences"
	case gradeC:
		return "Many MeshParts, consider merging where possible"
	case gradeD:
		return "High MeshPart count, noticeable draw call overhead"
	case gradeE:
		return "Very high MeshPart count, significant performance cost"
	default:
		return "Extreme MeshPart count, major draw call bottleneck"
	}
}

func DrawCallGradeDescription(grade string) string {
	switch grade {
	case gradeAPlus:
		return "Very low estimated draw call overhead, excellent batching"
	case gradeA:
		return "Low estimated draw calls, efficient instancing and batching"
	case gradeB:
		return "Moderate estimated draw calls, healthy for most scenes"
	case gradeC:
		return "Elevated estimated draw calls, batching opportunities likely"
	case gradeD:
		return "High estimated draw calls, rendering overhead may be noticeable"
	case gradeE:
		return "Very high estimated draw calls, GPU submission cost is significant"
	default:
		return "Extreme estimated draw calls, rendering overhead is likely a bottleneck"
	}
}

func PartCountGradeDescription(grade string) string {
	switch grade {
	case gradeAPlus:
		return "Very few Parts, minimal physics and rendering cost"
	case gradeA:
		return "Low Part count, efficient scene"
	case gradeB:
		return "Moderate Parts, acceptable for most experiences"
	case gradeC:
		return "Many Parts, consider unions or MeshParts"
	case gradeD:
		return "High Part count, impacting physics and rendering"
	case gradeE:
		return "Very high Part count, significant performance cost"
	default:
		return "Extreme Part count, severe physics and rendering impact"
	}
}

func AssetDiversityGradeDescription(grade string) string {
	switch grade {
	case gradeAPlus:
		return "Very few unique assets, near-instant CDN resolution"
	case gradeA:
		return "Few unique assets, fast CDN resolution"
	case gradeB:
		return "Moderate asset count, reasonable CDN load"
	case gradeC:
		return "Many unique assets, increased CDN fetch time"
	case gradeD:
		return "High asset count, slow cold-start loading"
	case gradeE:
		return "Very high asset count, significant cold-start penalty"
	default:
		return "Extreme asset count, severely impacts initial load"
	}
}
