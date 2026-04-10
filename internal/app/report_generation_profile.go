package app

import (
	"fmt"
	"math"
	"sort"
)

type performanceGrade struct {
	Grade            string
	Label            string
	Value            string
	TotalValue       string
	Description      string
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

type reportCellPercentiles struct {
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

func computeCellPercentiles(cells []rbxlHeatmapCell) reportCellPercentiles {
	if len(cells) == 0 {
		return reportCellPercentiles{}
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
		return reportCellPercentiles{}
	}

	return reportCellPercentiles{
		P90TotalBytes:    percentileFloat64(totalBytesValues, reportGenerationPercentile),
		P90TextureBytes:  percentileFloat64(textureBytesValues, reportGenerationPercentile),
		P90MeshBytes:     percentileFloat64(meshBytesValues, reportGenerationPercentile),
		P90TriangleCount: percentileFloat64(triangleCountValues, reportGenerationPercentile),
		P90UniqueAssets:  percentileFloat64(uniqueAssetValues, reportGenerationPercentile),
		P90MeshParts:     percentileFloat64(meshPartValues, reportGenerationPercentile),
		P90Parts:         percentileFloat64(partValues, reportGenerationPercentile),
		P90DrawCalls:     percentileFloat64(drawCallValues, reportGenerationPercentile),
		CellCount:        occupied,
		CellSizeStuds:    cellSize,
	}
}

func computeReportCellPercentiles(assetType reportGenerationAssetTypeConfig, cells []rbxlHeatmapCell, summary reportGenerationSummary) reportCellPercentiles {
	if assetType.DisableSpatialMode {
		return reportCellPercentiles{
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
	return computeCellPercentiles(cells)
}

func percentileFloat64(values []float64, percentile float64) float64 {
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

func computePerformanceProfile(percentiles reportCellPercentiles, summary reportGenerationSummary) []performanceGrade {
	return computePerformanceProfileForAssetType(defaultReportGenerationAssetType(), percentiles, summary)
}

func computePerformanceProfileForAssetType(assetType reportGenerationAssetTypeConfig, percentiles reportCellPercentiles, summary reportGenerationSummary) []performanceGrade {
	useCellPercentiles := percentiles.CellCount > 0
	thresholds := assetType.Thresholds

	grades := []performanceGrade{
		computeDownloadSizeGradeWithThresholds(summary.TotalBytes, percentiles.P90TotalBytes, useCellPercentiles, thresholds.TotalSizeMB),
		computeTextureSizeGradeWithThresholds(summary.TextureBytes, percentiles.P90TextureBytes, useCellPercentiles, thresholds.TextureSizeMB),
		computeMeshSizeGradeWithThresholds(summary.MeshBytes, percentiles.P90MeshBytes, useCellPercentiles, thresholds.MeshSizeMB),
		computeOversizedTextureCountGradeWithThresholds(summary.OversizedTextureCount, thresholds.OversizedTextures),
		computeMeshComplexityGradeWithThresholds(summary.TriangleCount, percentiles.P90TriangleCount, useCellPercentiles, thresholds.MeshComplexity),
		computeDrawCallGradeWithThresholds(summary.DrawCallCount, percentiles.P90DrawCalls, useCellPercentiles, thresholds.DrawCalls),
		computeMeshPartCountGradeWithThresholds(summary.MeshPartCount, percentiles.P90MeshParts, useCellPercentiles, thresholds.MeshPartCount),
		computePartCountGradeWithThresholds(summary.PartCount, percentiles.P90Parts, useCellPercentiles, thresholds.PartCount),
		computeAssetDiversityGradeWithThresholds(summary.UniqueAssetCount, percentiles.P90UniqueAssets, useCellPercentiles, thresholds.AssetDiversity),
	}

	dupCount := computeDuplicateCountGradeWithThresholds(summary.DuplicateCount, thresholds.DuplicateCount)
	dupWaste := computeDuplicationWasteGradeWithThresholds(summary.DuplicateSizeBytes, summary.TotalBytes, thresholds.DuplicationWastePct)
	if summary.DuplicateCount > 0 {
		dupCount.Grade = capGradeAtC(dupCount.Grade)
		dupWaste.Grade = capGradeAtC(dupWaste.Grade)
	}
	grades = append(grades, dupCount, dupWaste)
	return grades
}

func overallPerformanceGrade(grades []performanceGrade, hasDuplicates bool) string {
	avg := overallPerformanceNumericAverage(grades)
	grade := numericToGrade(int(math.Round(avg)))
	if hasDuplicates && gradeToNumeric(grade) > gradeToNumeric(gradeB) {
		return gradeB
	}
	return grade
}

func overallPerformanceScorePercent(grades []performanceGrade, hasDuplicates bool) int {
	avg := overallPerformanceNumericAverage(grades)
	if hasDuplicates && avg > float64(gradeToNumeric(gradeB)) {
		avg = float64(gradeToNumeric(gradeB))
	}
	return int(math.Round((avg / float64(gradeToNumeric(gradeAPlus))) * 100))
}

func overallPerformanceNumericAverage(grades []performanceGrade) float64 {
	if len(grades) == 0 {
		return 0
	}

	total := 0
	for _, g := range grades {
		total += gradeToNumeric(g.Grade)
	}
	return float64(total) / float64(len(grades))
}

func computeMeshComplexityGrade(triangleCount int64, percentilePerCell float64, useCellPercentile bool) performanceGrade {
	return computeMeshComplexityGradeWithThresholds(triangleCount, percentilePerCell, useCellPercentile, defaultReportGenerationAssetType().Thresholds.MeshComplexity)
}

func computeMeshComplexityGradeWithThresholds(triangleCount int64, percentilePerCell float64, useCellPercentile bool, thresholds [6]float64) performanceGrade {
	gradeValue := float64(triangleCount)
	totalLabel := ""
	if useCellPercentile {
		gradeValue = percentilePerCell
		totalLabel = formatIntCommas(int64(percentilePerCell)) + " p90/cell"
	}
	grade := gradeFromThresholds(gradeValue, thresholds)
	return performanceGrade{
		Grade:            grade,
		Label:            "Mesh Complexity",
		Value:            formatIntCommas(triangleCount) + " tris",
		TotalValue:       totalLabel,
		Description:      meshGradeDescription(grade),
		MetricDescription: "Total triangle count across all MeshParts in the scene",
	}
}

func computeDuplicationWasteGrade(duplicateSizeBytes int64, totalBytes int64) performanceGrade {
	return computeDuplicationWasteGradeWithThresholds(duplicateSizeBytes, totalBytes, defaultReportGenerationAssetType().Thresholds.DuplicationWastePct)
}

func computeDuplicationWasteGradeWithThresholds(duplicateSizeBytes int64, totalBytes int64, thresholds [6]float64) performanceGrade {
	percentage := 0.0
	if totalBytes > 0 {
		percentage = float64(duplicateSizeBytes) / float64(totalBytes) * 100.0
	}
	grade := gradeFromThresholds(percentage, thresholds)
	return performanceGrade{
		Grade:            grade,
		Label:            "Duplication Waste",
		Value:            fmt.Sprintf("%.1f%%", percentage),
		Description:      duplicationGradeDescription(grade),
		MetricDescription: "Percentage of total size wasted by duplicate assets",
	}
}

func computeDownloadSizeGrade(totalBytes int64, percentilePerCell float64, useCellPercentile bool) performanceGrade {
	return computeDownloadSizeGradeWithThresholds(totalBytes, percentilePerCell, useCellPercentile, defaultReportGenerationAssetType().Thresholds.TotalSizeMB)
}

func computeDownloadSizeGradeWithThresholds(totalBytes int64, percentilePerCell float64, useCellPercentile bool, thresholds [6]float64) performanceGrade {
	gradeValue := float64(totalBytes) / float64(megabyte)
	totalLabel := ""
	if useCellPercentile {
		gradeValue = percentilePerCell / float64(megabyte)
		totalLabel = formatSizeAuto64(int64(percentilePerCell)) + " p90/cell"
	}
	grade := gradeFromThresholds(gradeValue, thresholds)
	return performanceGrade{
		Grade:            grade,
		Label:            "Total Size",
		Value:            formatSizeAuto64(totalBytes),
		TotalValue:       totalLabel,
		Description:      downloadSizeGradeDescription(grade),
		MetricDescription: "Sum of all asset data (meshes + textures) that must be downloaded",
	}
}

func computeTextureSizeGrade(textureBytes int64, percentilePerCell float64, useCellPercentile bool) performanceGrade {
	return computeTextureSizeGradeWithThresholds(textureBytes, percentilePerCell, useCellPercentile, defaultReportGenerationAssetType().Thresholds.TextureSizeMB)
}

func computeTextureSizeGradeWithThresholds(textureBytes int64, percentilePerCell float64, useCellPercentile bool, thresholds [6]float64) performanceGrade {
	gradeValue := float64(textureBytes) / float64(megabyte)
	totalLabel := ""
	if useCellPercentile {
		gradeValue = percentilePerCell / float64(megabyte)
		totalLabel = formatSizeAuto64(int64(percentilePerCell)) + " p90/cell"
	}
	grade := gradeFromThresholds(gradeValue, thresholds)
	return performanceGrade{
		Grade:            grade,
		Label:            "Texture Size",
		Value:            formatSizeAuto64(textureBytes),
		TotalValue:       totalLabel,
		Description:      textureSizeGradeDescription(grade),
		MetricDescription: "Total size of all image/texture assets in the scene",
	}
}

func computeMeshSizeGrade(meshBytes int64, percentilePerCell float64, useCellPercentile bool) performanceGrade {
	return computeMeshSizeGradeWithThresholds(meshBytes, percentilePerCell, useCellPercentile, defaultReportGenerationAssetType().Thresholds.MeshSizeMB)
}

func computeMeshSizeGradeWithThresholds(meshBytes int64, percentilePerCell float64, useCellPercentile bool, thresholds [6]float64) performanceGrade {
	gradeValue := float64(meshBytes) / float64(megabyte)
	totalLabel := ""
	if useCellPercentile {
		gradeValue = percentilePerCell / float64(megabyte)
		totalLabel = formatSizeAuto64(int64(percentilePerCell)) + " p90/cell"
	}
	grade := gradeFromThresholds(gradeValue, thresholds)
	return performanceGrade{
		Grade:            grade,
		Label:            "Mesh Size",
		Value:            formatSizeAuto64(meshBytes),
		TotalValue:       totalLabel,
		Description:      meshSizeGradeDescription(grade),
		MetricDescription: "Total size of all mesh geometry data in the scene",
	}
}

func computeOversizedTextureCountGrade(oversizedTextureCount int) performanceGrade {
	return computeOversizedTextureCountGradeWithThresholds(oversizedTextureCount, defaultReportGenerationAssetType().Thresholds.OversizedTextures)
}

func computeOversizedTextureCountGradeWithThresholds(oversizedTextureCount int, thresholds [6]float64) performanceGrade {
	grade := gradeFromThresholds(float64(oversizedTextureCount), thresholds)
	return performanceGrade{
		Grade:            grade,
		Label:            "Oversized Textures",
		Value:            fmt.Sprintf("%d textures", oversizedTextureCount),
		Description:      oversizedTextureCountGradeDescription(grade),
		MetricDescription: "Textures larger than optimal for their on-screen surface area",
	}
}

func computeDuplicateCountGrade(duplicateCount int64) performanceGrade {
	return computeDuplicateCountGradeWithThresholds(duplicateCount, defaultReportGenerationAssetType().Thresholds.DuplicateCount)
}

func computeDuplicateCountGradeWithThresholds(duplicateCount int64, thresholds [6]float64) performanceGrade {
	grade := gradeFromThresholds(float64(duplicateCount), thresholds)
	return performanceGrade{
		Grade:            grade,
		Label:            "Duplicates",
		Value:            formatIntCommas(duplicateCount) + " duplicates",
		Description:      duplicateCountGradeDescription(grade),
		MetricDescription: "Assets uploaded multiple times with identical content",
	}
}

func computeMeshPartCountGrade(meshPartCount int, percentilePerCell float64, useCellPercentile bool) performanceGrade {
	return computeMeshPartCountGradeWithThresholds(meshPartCount, percentilePerCell, useCellPercentile, defaultReportGenerationAssetType().Thresholds.MeshPartCount)
}

func computeMeshPartCountGradeWithThresholds(meshPartCount int, percentilePerCell float64, useCellPercentile bool, thresholds [6]float64) performanceGrade {
	gradeValue := float64(meshPartCount)
	totalLabel := ""
	if useCellPercentile {
		gradeValue = percentilePerCell
		totalLabel = fmt.Sprintf("%.0f p90/cell", percentilePerCell)
	}
	grade := gradeFromThresholds(gradeValue, thresholds)
	return performanceGrade{
		Grade:            grade,
		Label:            "MeshParts",
		Value:            fmt.Sprintf("%d", meshPartCount),
		TotalValue:       totalLabel,
		Description:      meshPartCountGradeDescription(grade),
		MetricDescription: "Count of MeshPart instances in the scene",
	}
}

func computeDrawCallGrade(drawCallCount int64, percentilePerCell float64, useCellPercentile bool) performanceGrade {
	return computeDrawCallGradeWithThresholds(drawCallCount, percentilePerCell, useCellPercentile, defaultReportGenerationAssetType().Thresholds.DrawCalls)
}

func computeDrawCallGradeWithThresholds(drawCallCount int64, percentilePerCell float64, useCellPercentile bool, thresholds [6]float64) performanceGrade {
	gradeValue := float64(drawCallCount)
	totalLabel := ""
	if useCellPercentile {
		gradeValue = percentilePerCell
		totalLabel = fmt.Sprintf("%.0f p90/cell", percentilePerCell)
	}
	grade := gradeFromThresholds(gradeValue, thresholds)
	return performanceGrade{
		Grade:            grade,
		Label:            "Draw Calls",
		Value:            fmt.Sprintf("%d est.", drawCallCount),
		TotalValue:       totalLabel,
		Description:      drawCallGradeDescription(grade),
		MetricDescription: "Estimated GPU draw calls based on unique mesh/texture/material combos",
	}
}

func computePartCountGrade(partCount int, percentilePerCell float64, useCellPercentile bool) performanceGrade {
	return computePartCountGradeWithThresholds(partCount, percentilePerCell, useCellPercentile, defaultReportGenerationAssetType().Thresholds.PartCount)
}

func computePartCountGradeWithThresholds(partCount int, percentilePerCell float64, useCellPercentile bool, thresholds [6]float64) performanceGrade {
	gradeValue := float64(partCount)
	totalLabel := ""
	if useCellPercentile {
		gradeValue = percentilePerCell
		totalLabel = fmt.Sprintf("%.0f p90/cell", percentilePerCell)
	}
	grade := gradeFromThresholds(gradeValue, thresholds)
	return performanceGrade{
		Grade:            grade,
		Label:            "Parts",
		Value:            fmt.Sprintf("%d", partCount),
		TotalValue:       totalLabel,
		Description:      partCountGradeDescription(grade),
		MetricDescription: "Count of Part instances (legacy bricks) in the scene",
	}
}

func computeAssetDiversityGrade(uniqueAssetCount int, percentilePerCell float64, useCellPercentile bool) performanceGrade {
	return computeAssetDiversityGradeWithThresholds(uniqueAssetCount, percentilePerCell, useCellPercentile, defaultReportGenerationAssetType().Thresholds.AssetDiversity)
}

func computeAssetDiversityGradeWithThresholds(uniqueAssetCount int, percentilePerCell float64, useCellPercentile bool, thresholds [6]float64) performanceGrade {
	gradeValue := float64(uniqueAssetCount)
	totalLabel := ""
	if useCellPercentile {
		gradeValue = percentilePerCell
		totalLabel = fmt.Sprintf("%.0f p90/cell", percentilePerCell)
	}
	grade := gradeFromThresholds(gradeValue, thresholds)
	return performanceGrade{
		Grade:            grade,
		Label:            "Asset Diversity",
		Value:            fmt.Sprintf("%d unique", uniqueAssetCount),
		TotalValue:       totalLabel,
		Description:      assetDiversityGradeDescription(grade),
		MetricDescription: "Number of unique assets that must be fetched from CDN",
	}
}

// thresholds define 7 grade buckets: A+, A, B, C, D, E, F.
// Values below thresholds[0] get A+; values >= thresholds[5] get F.
func gradeFromThresholds(value float64, thresholds [6]float64) string {
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

func capGradeAtC(grade string) string {
	if gradeToNumeric(grade) > gradeToNumeric(gradeC) {
		return gradeC
	}
	return grade
}

func gradeToNumeric(grade string) int {
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

func numericToGrade(value int) string {
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

func meshGradeDescription(grade string) string {
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

func duplicationGradeDescription(grade string) string {
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

func downloadSizeGradeDescription(grade string) string {
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

func textureSizeGradeDescription(grade string) string {
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

func meshSizeGradeDescription(grade string) string {
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

func duplicateCountGradeDescription(grade string) string {
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

func oversizedTextureCountGradeDescription(grade string) string {
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

func meshPartCountGradeDescription(grade string) string {
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

func drawCallGradeDescription(grade string) string {
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

func partCountGradeDescription(grade string) string {
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

func assetDiversityGradeDescription(grade string) string {
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
