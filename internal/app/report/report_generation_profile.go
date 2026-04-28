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
	// AvgCellValue / TotalValue (= p90) / MaxCellValue are the per-cell
	// summaries shown alongside the value column when the report is in
	// spatial mode. TotalValue keeps its name (existing field) and means
	// "p90/cell". Empty for grades that don't have a per-cell story.
	AvgCellValue      string
	TotalValue        string
	MaxCellValue      string
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

// CellMetric carries the three across-cell summaries we report for a
// per-cell metric: arithmetic mean, p90, and max. Avg uses the same
// occupancy filter as p90 (only cells where the metric is meaningful).
type CellMetric struct {
	Avg float64
	P90 float64
	Max float64
}

func metricFromValues(values []float64) CellMetric {
	if len(values) == 0 {
		return CellMetric{}
	}
	sum := 0.0
	maxValue := values[0]
	for _, v := range values {
		sum += v
		if v > maxValue {
			maxValue = v
		}
	}
	return CellMetric{
		Avg: sum / float64(len(values)),
		P90: PercentileFloat64(values, reportGenerationPercentile),
		Max: maxValue,
	}
}

func wholeFileMetric(value float64) CellMetric {
	return CellMetric{Avg: value, P90: value, Max: value}
}

type CellPercentiles struct {
	TotalBytes      CellMetric
	TextureBytes    CellMetric
	GPUTextureBytes CellMetric
	MeshBytes       CellMetric
	TriangleCount   CellMetric
	UniqueAssets    CellMetric
	MeshParts       CellMetric
	Parts           CellMetric
	DrawCalls       CellMetric
	InstanceCount   CellMetric
	CellCount       int
	CellSizeStuds   float64
	WholeFileMode   bool
}

func ComputeCellPercentiles(cells []heatmap.Cell) CellPercentiles {
	if len(cells) == 0 {
		return CellPercentiles{}
	}

	occupied := 0
	totalBytesValues := make([]float64, 0, len(cells))
	textureBytesValues := make([]float64, 0, len(cells))
	gpuTextureBytesValues := make([]float64, 0, len(cells))
	meshBytesValues := make([]float64, 0, len(cells))
	triangleCountValues := make([]float64, 0, len(cells))
	uniqueAssetValues := make([]float64, 0, len(cells))
	meshPartValues := make([]float64, 0, len(cells))
	partValues := make([]float64, 0, len(cells))
	drawCallValues := make([]float64, 0, len(cells))
	instanceCountValues := make([]float64, 0, len(cells))
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
		if cell.Stats.InstanceCount > 0 {
			instanceCountValues = append(instanceCountValues, float64(cell.Stats.InstanceCount))
		}
		if cell.Stats.ReferenceCount <= 0 {
			if cellSize == 0 && (cell.Stats.MeshPartCount > 0 || cell.Stats.PartCount > 0 || cell.Stats.DrawCallCount > 0 || cell.Stats.InstanceCount > 0) {
				cellSize = cell.MaximumX - cell.MinimumX
			}
			continue
		}
		occupied++
		totalBytesValues = append(totalBytesValues, float64(cell.Stats.TotalBytes))
		textureBytesValues = append(textureBytesValues, float64(cell.Stats.TextureBytes))
		gpuTextureBytesValues = append(gpuTextureBytesValues, float64(EstimateGPUTextureBytes(cell.Stats.BC1PixelCount, cell.Stats.BC3PixelCount)))
		meshBytesValues = append(meshBytesValues, float64(cell.Stats.MeshBytes))
		triangleCountValues = append(triangleCountValues, float64(cell.Stats.TriangleCount))
		uniqueAssetValues = append(uniqueAssetValues, float64(cell.Stats.UniqueAssetCount))
		if cellSize == 0 {
			cellSize = cell.MaximumX - cell.MinimumX
		}
	}

	if occupied == 0 && len(meshPartValues) == 0 && len(partValues) == 0 && len(drawCallValues) == 0 && len(instanceCountValues) == 0 {
		return CellPercentiles{}
	}

	return CellPercentiles{
		TotalBytes:      metricFromValues(totalBytesValues),
		TextureBytes:    metricFromValues(textureBytesValues),
		GPUTextureBytes: metricFromValues(gpuTextureBytesValues),
		MeshBytes:       metricFromValues(meshBytesValues),
		TriangleCount:   metricFromValues(triangleCountValues),
		UniqueAssets:    metricFromValues(uniqueAssetValues),
		MeshParts:       metricFromValues(meshPartValues),
		Parts:           metricFromValues(partValues),
		DrawCalls:       metricFromValues(drawCallValues),
		InstanceCount:   metricFromValues(instanceCountValues),
		CellCount:       occupied,
		CellSizeStuds:   cellSize,
	}
}

func ComputeReportCellPercentiles(assetType AssetTypeConfig, cells []heatmap.Cell, summary Summary) CellPercentiles {
	if assetType.DisableSpatialMode {
		return CellPercentiles{
			TotalBytes:      wholeFileMetric(float64(summary.TotalBytes)),
			TextureBytes:    wholeFileMetric(float64(summary.TextureBytes)),
			GPUTextureBytes: wholeFileMetric(float64(EstimateGPUTextureBytes(summary.BC1PixelCount, summary.BC3PixelCount))),
			MeshBytes:       wholeFileMetric(float64(summary.MeshBytes)),
			TriangleCount:   wholeFileMetric(float64(summary.TriangleCount)),
			UniqueAssets:    wholeFileMetric(float64(summary.UniqueAssetCount)),
			MeshParts:       wholeFileMetric(float64(summary.MeshPartCount)),
			Parts:           wholeFileMetric(float64(summary.PartCount)),
			DrawCalls:       wholeFileMetric(float64(summary.DrawCallCount)),
			InstanceCount:   wholeFileMetric(float64(summary.InstanceCount)),
			CellCount:       1,
			WholeFileMode:   true,
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
	// Whole-file asset types (e.g. vehicle categories with DisableSpatialMode)
	// fold all metrics into a synthetic single cell. The avg/p90/max columns
	// would just repeat the headline value, so suppress per-cell display
	// entirely for them.
	useCellPercentiles := percentiles.CellCount > 0 && !percentiles.WholeFileMode
	thresholds := assetType.Thresholds

	grades := []PerformanceGrade{
		ComputeGPUTextureMemoryGradeWithThresholds(summary.BC1PixelCount, summary.BC3PixelCount, summary.BC1BytesExact, summary.BC3BytesExact, percentiles.GPUTextureBytes, useCellPercentiles, thresholds.GPUTextureMemoryMB),
		ComputeMeshSizeGradeWithThresholds(summary.MeshBytes, percentiles.MeshBytes, useCellPercentiles, thresholds.MeshSizeMB),
		ComputeMeshComplexityGradeWithThresholds(summary.TriangleCount, percentiles.TriangleCount, useCellPercentiles, thresholds.MeshComplexity),
		ComputeDrawCallGradeWithThresholds(summary.DrawCallCount, percentiles.DrawCalls, useCellPercentiles, thresholds.DrawCalls),
		ComputeMeshPartCountGradeWithThresholds(summary.MeshPartCount, percentiles.MeshParts, useCellPercentiles, thresholds.MeshPartCount),
		ComputePartCountGradeWithThresholds(summary.PartCount, percentiles.Parts, useCellPercentiles, thresholds.PartCount),
		ComputeInstanceCountGradeWithThresholds(summary.InstanceCount, percentiles.InstanceCount, useCellPercentiles, thresholds.InstanceCount),
		ComputeAssetDiversityGradeWithThresholds(summary.UniqueAssetCount, percentiles.UniqueAssets, useCellPercentiles, thresholds.AssetDiversity),
		ComputeMismatchedPBRMapsGradeWithThresholds(summary.MismatchedPBRMaterialCount, summary.PBRMaterialCount, thresholds.MismatchedPBRMaps),
		ComputeOversizedTextureCountGradeWithThresholds(summary.OversizedTextureCount, thresholds.OversizedTextures),
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

// formatCellMetricBytes / formatCellMetricInts produce the three per-cell
// labels (avg, p90, max) shown next to the grade value in spatial mode.
// Returns empty strings when useCellPercentile is false so the UI omits
// the cells entirely.
func formatCellMetricBytes(metric CellMetric, useCellPercentile bool) (string, string, string) {
	if !useCellPercentile {
		return "", "", ""
	}
	return format.FormatSizeAuto64(int64(metric.Avg)), format.FormatSizeAuto64(int64(metric.P90)), format.FormatSizeAuto64(int64(metric.Max))
}

func formatCellMetricInts(metric CellMetric, useCellPercentile bool) (string, string, string) {
	if !useCellPercentile {
		return "", "", ""
	}
	return format.FormatIntCommas(int64(metric.Avg)), format.FormatIntCommas(int64(metric.P90)), format.FormatIntCommas(int64(metric.Max))
}

func ComputeMeshComplexityGrade(triangleCount int64, cellMetric CellMetric, useCellPercentile bool) PerformanceGrade {
	return ComputeMeshComplexityGradeWithThresholds(triangleCount, cellMetric, useCellPercentile, DefaultAssetType().Thresholds.MeshComplexity)
}

func ComputeMeshComplexityGradeWithThresholds(triangleCount int64, cellMetric CellMetric, useCellPercentile bool, thresholds [6]float64) PerformanceGrade {
	gradeValue := float64(triangleCount)
	if useCellPercentile {
		gradeValue = cellMetric.P90
	}
	avgLabel, p90Label, maxLabel := formatCellMetricInts(cellMetric, useCellPercentile)
	grade := GradeFromThresholds(gradeValue, thresholds)
	return PerformanceGrade{
		Grade:             grade,
		Score:             ContinuousScoreFromThresholds(gradeValue, thresholds),
		Label:             "Mesh Complexity",
		Value:             format.FormatIntCommas(triangleCount) + " tris",
		AvgCellValue:      avgLabel,
		TotalValue:        p90Label,
		MaxCellValue:      maxLabel,
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

func ComputeInstanceCountGrade(instanceCount int64, cellMetric CellMetric, useCellPercentile bool) PerformanceGrade {
	return ComputeInstanceCountGradeWithThresholds(instanceCount, cellMetric, useCellPercentile, DefaultAssetType().Thresholds.InstanceCount)
}

// ComputeInstanceCountGradeWithThresholds grades scene instance count.
// In spatial mode it grades on the p90/cell of positionable instances
// (instances without world coordinates can't be bucketed and are dropped
// from the cell roll-up). The whole-file count drives the headline value
// so users still see the true descendant total.
func ComputeInstanceCountGradeWithThresholds(instanceCount int64, cellMetric CellMetric, useCellPercentile bool, thresholds [6]float64) PerformanceGrade {
	gradeValue := float64(instanceCount)
	if useCellPercentile {
		gradeValue = cellMetric.P90
	}
	avgLabel, p90Label, maxLabel := formatCellMetricInts(cellMetric, useCellPercentile)
	grade := GradeFromThresholds(gradeValue, thresholds)
	return PerformanceGrade{
		Grade:             grade,
		Score:             ContinuousScoreFromThresholds(gradeValue, thresholds),
		Label:             "Instances",
		Value:             format.FormatIntCommas(instanceCount),
		AvgCellValue:      avgLabel,
		TotalValue:        p90Label,
		MaxCellValue:      maxLabel,
		Description:       InstanceCountGradeDescription(grade),
		MetricDescription: "Total descendants in the file (every instance of every class) — graded on the p90/cell of positionable instances when spatial data is available",
	}
}

func InstanceCountGradeDescription(grade string) string {
	switch grade {
	case gradeAPlus:
		return "Tiny instance tree, negligible streaming or replication overhead"
	case gradeA:
		return "Small instance tree, fast to stream and replicate"
	case gradeB:
		return "Moderate instance count, healthy for most experiences"
	case gradeC:
		return "Elevated instance count, consider grouping or simplifying"
	case gradeD:
		return "High instance count, noticeable streaming and serialization cost"
	case gradeE:
		return "Very high instance count, significant overhead"
	default:
		return "Extreme instance count, severe streaming and replication cost"
	}
}

// GPU-format byte rates (bytes per source pixel) for the formats Roblox picks
// at upload time: BC1 for alpha-less sources, BC3 for sources that carry any
// alpha channel. The mip-chain multiplier (~1.33x) covers the full pyramid.
const (
	BC1BytesPerPixel = 0.5
	BC3BytesPerPixel = 1.0
	MipChainFactor   = 4.0 / 3.0
)

// EstimateGPUTextureBytes sums the BC1 and BC3 GPU footprints (with mipmaps)
// for the given pixel-count buckets. bc1Pixels are pixels from textures Roblox
// would store as BC1; bc3Pixels are pixels from textures stored as BC3.
//
// Uses the 4/3 geometric mip-sum approximation; accurate to within ~0.01%
// for typical asset resolutions but undercounts by ~10-30 B per texture
// because BC formats require a full 4x4 block per mip regardless of how
// small the mip is. For byte-for-byte RDC parity use
// EstimateGPUTextureBytesExact instead.
func EstimateGPUTextureBytes(bc1Pixels, bc3Pixels int64) int64 {
	if bc1Pixels < 0 {
		bc1Pixels = 0
	}
	if bc3Pixels < 0 {
		bc3Pixels = 0
	}
	total := float64(bc1Pixels)*BC1BytesPerPixel*MipChainFactor +
		float64(bc3Pixels)*BC3BytesPerPixel*MipChainFactor
	return int64(math.Round(total))
}

// EstimateGPUTextureBytesExact returns the byte-accurate on-GPU footprint
// of a single texture, summing each mip explicitly and rounding each mip's
// block count up to the BC 4x4-block minimum. Matches what RDC reports.
// Bytes-per-block: 8 for BC1, 16 for BC3. Full mip chain down to 1x1.
// Returns 0 for non-positive dimensions.
func EstimateGPUTextureBytesExact(width, height int, isBC3 bool) int64 {
	if width <= 0 || height <= 0 {
		return 0
	}
	bytesPerBlock := int64(8)
	if isBC3 {
		bytesPerBlock = 16
	}
	var total int64
	w, h := width, height
	for {
		// BC formats store 4x4 blocks; any mip smaller than 4x4 still
		// allocates one block per dimension-axis (minimum 1 block).
		blocksX := (w + 3) / 4
		if blocksX < 1 {
			blocksX = 1
		}
		blocksY := (h + 3) / 4
		if blocksY < 1 {
			blocksY = 1
		}
		total += int64(blocksX) * int64(blocksY) * bytesPerBlock
		if w == 1 && h == 1 {
			break
		}
		if w > 1 {
			w /= 2
		}
		if h > 1 {
			h /= 2
		}
	}
	return total
}

func ComputeGPUTextureMemoryGrade(bc1Pixels, bc3Pixels, bc1BytesExact, bc3BytesExact int64, cellMetric CellMetric, useCellPercentile bool) PerformanceGrade {
	return ComputeGPUTextureMemoryGradeWithThresholds(bc1Pixels, bc3Pixels, bc1BytesExact, bc3BytesExact, cellMetric, useCellPercentile, DefaultAssetType().Thresholds.GPUTextureMemoryMB)
}

// ComputeGPUTextureMemoryGradeWithThresholds grades combined BC1+BC3 GPU
// texture memory. Prefers bc1BytesExact+bc3BytesExact (byte-for-byte match
// with RenderDoc) and falls back to the pixel approximation when exact
// bytes aren't populated (older call sites or tests). cellMetric carries
// the per-cell avg/p90/max of GPU bytes when in spatial mode.
func ComputeGPUTextureMemoryGradeWithThresholds(bc1Pixels, bc3Pixels, bc1BytesExact, bc3BytesExact int64, cellMetric CellMetric, useCellPercentile bool, thresholds [6]float64) PerformanceGrade {
	totalBytes := bc1BytesExact + bc3BytesExact
	if totalBytes <= 0 {
		totalBytes = EstimateGPUTextureBytes(bc1Pixels, bc3Pixels)
	}
	gradeValue := float64(totalBytes) / float64(format.Megabyte)
	if useCellPercentile {
		gradeValue = cellMetric.P90 / float64(format.Megabyte)
	}
	avgLabel, p90Label, maxLabel := formatCellMetricBytes(cellMetric, useCellPercentile)
	grade := GradeFromThresholds(gradeValue, thresholds)
	return PerformanceGrade{
		Grade:             grade,
		Score:             ContinuousScoreFromThresholds(gradeValue, thresholds),
		Label:             "GPU Texture Memory",
		Value:             format.FormatSizeAuto64(totalBytes),
		AvgCellValue:      avgLabel,
		TotalValue:        p90Label,
		MaxCellValue:      maxLabel,
		Description:       GPUTextureMemoryGradeDescription(grade),
		MetricDescription: "Estimated GPU VRAM: BC1 (0.5 B/px) for no-alpha sources + BC3 (1.0 B/px) for alpha sources, both with mipmaps",
	}
}

func ComputeMismatchedPBRMapsGrade(mismatchedCount, totalCount int) PerformanceGrade {
	return ComputeMismatchedPBRMapsGradeWithThresholds(mismatchedCount, totalCount, DefaultAssetType().Thresholds.MismatchedPBRMaps)
}

// ComputeMismatchedPBRMapsGradeWithThresholds grades how many SurfaceAppearance
// materials have authored color/normal/metalness/roughness slots that aren't
// all the same source resolution. Mixing a 2K color with a 512 normal forces
// the engine to upscale the smaller map, wasting VRAM and signalling
// inconsistent authoring.
func ComputeMismatchedPBRMapsGradeWithThresholds(mismatchedCount, totalCount int, thresholds [6]float64) PerformanceGrade {
	_ = totalCount
	gradeValue := float64(mismatchedCount)
	grade := GradeFromThresholds(gradeValue, thresholds)
	return PerformanceGrade{
		Grade:             grade,
		Score:             ContinuousScoreFromThresholds(gradeValue, thresholds),
		Label:             "Mismatched PBR Maps",
		Value:             fmt.Sprintf("%d materials", mismatchedCount),
		Description:       MismatchedPBRMapsGradeDescription(grade),
		MetricDescription: "SurfaceAppearance materials whose color/normal/metalness/roughness textures aren't all at the same source resolution — the engine upscales the smaller maps, wasting VRAM",
	}
}

func MismatchedPBRMapsGradeDescription(grade string) string {
	switch grade {
	case gradeAPlus:
		return "All PBR materials use matching map resolutions"
	case gradeA:
		return "A handful of materials mix map resolutions across their PBR slots"
	case gradeB:
		return "Some materials mix resolutions across color/normal/MR — pick one size per material"
	case gradeC:
		return "Notable number of materials with mismatched PBR map sizes"
	case gradeD:
		return "Many materials have unbalanced PBR maps; resize smaller maps to match"
	case gradeE:
		return "Heavy PBR map mismatch; significant authoring cleanup needed"
	default:
		return "Most materials have unmatched PBR map resolutions"
	}
}

func GPUTextureMemoryGradeDescription(grade string) string {
	switch grade {
	case gradeAPlus:
		return "Minimal GPU texture memory, negligible VRAM impact"
	case gradeA:
		return "Low GPU texture memory, plenty of headroom on all platforms"
	case gradeB:
		return "Moderate GPU texture memory, acceptable for most devices"
	case gradeC:
		return "Heavy GPU texture memory, may strain low-end devices"
	case gradeD:
		return "Very heavy GPU texture memory, risk of texture streaming hitches"
	case gradeE:
		return "Extreme GPU texture memory, likely to cause VRAM pressure"
	default:
		return "Massive GPU texture memory, will exceed VRAM budget on most devices"
	}
}

func ComputeMeshSizeGrade(meshBytes int64, cellMetric CellMetric, useCellPercentile bool) PerformanceGrade {
	return ComputeMeshSizeGradeWithThresholds(meshBytes, cellMetric, useCellPercentile, DefaultAssetType().Thresholds.MeshSizeMB)
}

func ComputeMeshSizeGradeWithThresholds(meshBytes int64, cellMetric CellMetric, useCellPercentile bool, thresholds [6]float64) PerformanceGrade {
	gradeValue := float64(meshBytes) / float64(format.Megabyte)
	if useCellPercentile {
		gradeValue = cellMetric.P90 / float64(format.Megabyte)
	}
	avgLabel, p90Label, maxLabel := formatCellMetricBytes(cellMetric, useCellPercentile)
	grade := GradeFromThresholds(gradeValue, thresholds)
	return PerformanceGrade{
		Grade:             grade,
		Score:             ContinuousScoreFromThresholds(gradeValue, thresholds),
		Label:             "Mesh Size",
		Value:             format.FormatSizeAuto64(meshBytes),
		AvgCellValue:      avgLabel,
		TotalValue:        p90Label,
		MaxCellValue:      maxLabel,
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

func ComputeMeshPartCountGrade(meshPartCount int, cellMetric CellMetric, useCellPercentile bool) PerformanceGrade {
	return ComputeMeshPartCountGradeWithThresholds(meshPartCount, cellMetric, useCellPercentile, DefaultAssetType().Thresholds.MeshPartCount)
}

func ComputeMeshPartCountGradeWithThresholds(meshPartCount int, cellMetric CellMetric, useCellPercentile bool, thresholds [6]float64) PerformanceGrade {
	gradeValue := float64(meshPartCount)
	if useCellPercentile {
		gradeValue = cellMetric.P90
	}
	avgLabel, p90Label, maxLabel := formatCellMetricInts(cellMetric, useCellPercentile)
	grade := GradeFromThresholds(gradeValue, thresholds)
	return PerformanceGrade{
		Grade:             grade,
		Score:             ContinuousScoreFromThresholds(gradeValue, thresholds),
		Label:             "MeshParts",
		Value:             fmt.Sprintf("%d", meshPartCount),
		AvgCellValue:      avgLabel,
		TotalValue:        p90Label,
		MaxCellValue:      maxLabel,
		Description:       MeshPartCountGradeDescription(grade),
		MetricDescription: "Count of MeshPart instances in the scene",
	}
}

func ComputeDrawCallGrade(drawCallCount int64, cellMetric CellMetric, useCellPercentile bool) PerformanceGrade {
	return ComputeDrawCallGradeWithThresholds(drawCallCount, cellMetric, useCellPercentile, DefaultAssetType().Thresholds.DrawCalls)
}

func ComputeDrawCallGradeWithThresholds(drawCallCount int64, cellMetric CellMetric, useCellPercentile bool, thresholds [6]float64) PerformanceGrade {
	gradeValue := float64(drawCallCount)
	if useCellPercentile {
		gradeValue = cellMetric.P90
	}
	avgLabel, p90Label, maxLabel := formatCellMetricInts(cellMetric, useCellPercentile)
	grade := GradeFromThresholds(gradeValue, thresholds)
	return PerformanceGrade{
		Grade:             grade,
		Score:             ContinuousScoreFromThresholds(gradeValue, thresholds),
		Label:             "Draw Calls",
		Value:             fmt.Sprintf("%d est.", drawCallCount),
		AvgCellValue:      avgLabel,
		TotalValue:        p90Label,
		MaxCellValue:      maxLabel,
		Description:       DrawCallGradeDescription(grade),
		MetricDescription: "Estimated GPU draw calls based on unique mesh/texture/material combos",
	}
}

func ComputePartCountGrade(partCount int, cellMetric CellMetric, useCellPercentile bool) PerformanceGrade {
	return ComputePartCountGradeWithThresholds(partCount, cellMetric, useCellPercentile, DefaultAssetType().Thresholds.PartCount)
}

func ComputePartCountGradeWithThresholds(partCount int, cellMetric CellMetric, useCellPercentile bool, thresholds [6]float64) PerformanceGrade {
	gradeValue := float64(partCount)
	if useCellPercentile {
		gradeValue = cellMetric.P90
	}
	avgLabel, p90Label, maxLabel := formatCellMetricInts(cellMetric, useCellPercentile)
	grade := GradeFromThresholds(gradeValue, thresholds)
	return PerformanceGrade{
		Grade:             grade,
		Score:             ContinuousScoreFromThresholds(gradeValue, thresholds),
		Label:             "Parts",
		Value:             fmt.Sprintf("%d", partCount),
		AvgCellValue:      avgLabel,
		TotalValue:        p90Label,
		MaxCellValue:      maxLabel,
		Description:       PartCountGradeDescription(grade),
		MetricDescription: "Count of Part instances (legacy bricks) in the scene",
	}
}

func ComputeAssetDiversityGrade(uniqueAssetCount int, cellMetric CellMetric, useCellPercentile bool) PerformanceGrade {
	return ComputeAssetDiversityGradeWithThresholds(uniqueAssetCount, cellMetric, useCellPercentile, DefaultAssetType().Thresholds.AssetDiversity)
}

func ComputeAssetDiversityGradeWithThresholds(uniqueAssetCount int, cellMetric CellMetric, useCellPercentile bool, thresholds [6]float64) PerformanceGrade {
	gradeValue := float64(uniqueAssetCount)
	if useCellPercentile {
		gradeValue = cellMetric.P90
	}
	avgLabel, p90Label, maxLabel := formatCellMetricInts(cellMetric, useCellPercentile)
	grade := GradeFromThresholds(gradeValue, thresholds)
	return PerformanceGrade{
		Grade:             grade,
		Score:             ContinuousScoreFromThresholds(gradeValue, thresholds),
		Label:             "Asset Diversity",
		Value:             fmt.Sprintf("%d unique", uniqueAssetCount),
		AvgCellValue:      avgLabel,
		TotalValue:        p90Label,
		MaxCellValue:      maxLabel,
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
