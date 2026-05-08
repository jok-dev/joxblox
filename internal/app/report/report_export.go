package report

import (
	"encoding/json"
	"time"
)

// ReportExportFormatVersion identifies the schema version of the JSON
// produced by BuildReportExport. Bump it when fields move, change
// meaning, or stop being best-effort optional. The HTML viewer reads
// this to decide whether it can render the bundle.
const ReportExportFormatVersion = "1.0.0"

// ReportExport is the canonical JSON shape for a full Report Generation
// run. Designed so a viewer (separate static HTML page, future webapp,
// CI tooling) can re-render the report without re-running the analysis.
//
// Stability: fields with `,omitempty` may be absent on older or
// minimal exports. New fields will only be appended; existing field
// names + meanings won't shift without a FormatVersion bump.
type ReportExport struct {
	FormatVersion string             `json:"format_version"`
	GeneratedAt   time.Time          `json:"generated_at"`
	JoxbloxVersion string            `json:"joxblox_version,omitempty"`
	ReportID      string             `json:"report_id,omitempty"`
	Source        ReportExportSource `json:"source"`

	Overall          ReportExportOverall      `json:"overall"`
	Grades           []ReportExportGrade      `json:"grades"`
	Summary          ReportExportSummary      `json:"summary"`
	CellPercentiles  *ReportExportPercentiles `json:"cell_percentiles,omitempty"`
	Details          ReportExportDetails      `json:"details"`
}

// ReportExportSource captures what was analysed.
type ReportExportSource struct {
	FileName       string `json:"file_name,omitempty"`
	FileSizeBytes  int64  `json:"file_size_bytes,omitempty"`
	AssetType      string `json:"asset_type,omitempty"`
	WorkspaceOnly  bool   `json:"workspace_only,omitempty"`
}

// ReportExportOverall is the top-line headline grade.
type ReportExportOverall struct {
	Grade        string `json:"grade"`
	ScorePercent int    `json:"score_percent"`
	HasDuplicates bool  `json:"has_duplicates"`
}

// ReportExportGrade mirrors PerformanceGrade in JSON-friendly shape,
// with the spatial avg/p90/max readings split out so the viewer can
// render the three-column display without re-running the math.
type ReportExportGrade struct {
	Label             string                  `json:"label"`
	Grade             string                  `json:"grade"`
	Score             float64                 `json:"score"`
	Weight            float64                 `json:"weight,omitempty"`
	Value             string                  `json:"value,omitempty"`
	AvgCellValue      string                  `json:"avg_cell_value,omitempty"`
	P90CellValue      string                  `json:"p90_cell_value,omitempty"`
	MaxCellValue      string                  `json:"max_cell_value,omitempty"`
	Description       string                  `json:"description,omitempty"`
	MetricDescription string                  `json:"metric_description,omitempty"`
}

// ReportExportSummary mirrors report.Summary as a flat JSON object.
type ReportExportSummary struct {
	TotalBytes                 int64 `json:"total_bytes"`
	TextureBytes               int64 `json:"texture_bytes"`
	TexturePixelCount          int64 `json:"texture_pixel_count"`
	BC1PixelCount              int64 `json:"bc1_pixel_count"`
	BC3PixelCount              int64 `json:"bc3_pixel_count"`
	BC1BytesExact              int64 `json:"bc1_bytes_exact"`
	BC3BytesExact              int64 `json:"bc3_bytes_exact"`
	MismatchedPBRMaterialCount int   `json:"mismatched_pbr_material_count"`
	PBRMaterialCount           int   `json:"pbr_material_count"`
	MeshBytes                  int64 `json:"mesh_bytes"`
	TriangleCount              int64 `json:"triangle_count"`
	OversizedTextureCount      int   `json:"oversized_texture_count"`
	DrawCallCount              int64 `json:"draw_call_count"`
	DuplicateCount             int64 `json:"duplicate_count"`
	DuplicateSizeBytes         int64 `json:"duplicate_size_bytes"`
	ReferenceCount             int64 `json:"reference_count"`
	UniqueReferenceCount       int   `json:"unique_reference_count"`
	UniqueAssetCount           int   `json:"unique_asset_count"`
	ResolvedCount              int   `json:"resolved_count"`
	MeshPartCount              int   `json:"mesh_part_count"`
	PartCount                  int   `json:"part_count"`
	InstanceCount              int64 `json:"instance_count"`
}

// ReportExportPercentiles is a JSON-friendly view of CellPercentiles.
// Omitted in whole-file mode (where every metric collapses to a single
// scalar; the headline already covers it).
type ReportExportPercentiles struct {
	CellCount        int                                 `json:"cell_count"`
	CellSizeStuds    float64                             `json:"cell_size_studs"`
	WholeFileMode    bool                                `json:"whole_file_mode"`
	Metrics          map[string]ReportExportCellMetric   `json:"metrics"`
}

// ReportExportCellMetric is the avg/p90/max triple per metric.
type ReportExportCellMetric struct {
	Avg float64 `json:"avg"`
	P90 float64 `json:"p90"`
	Max float64 `json:"max"`
}

// ReportExportDetails carries the drill-down lists that back each
// row's "View" button.
type ReportExportDetails struct {
	MismatchedPBRMaterials []MismatchedPBRMaterialDetail   `json:"mismatched_pbr_materials"`
	OversizedTextures      []ReportExportOversizedTexture  `json:"oversized_textures"`
	DuplicateGroups        []ReportExportDuplicateGroup    `json:"duplicate_groups"`
}

// ReportExportOversizedTexture mirrors the tab-side detail struct in a
// version-stable JSON shape so the viewer doesn't depend on internals.
type ReportExportOversizedTexture struct {
	AssetID          int64   `json:"asset_id"`
	InstancePath     string  `json:"instance_path,omitempty"`
	Width            int     `json:"width,omitempty"`
	Height           int     `json:"height,omitempty"`
	TextureBytes     int64   `json:"texture_bytes"`
	SceneSurfaceArea float64 `json:"scene_surface_area,omitempty"`
	Score            float64 `json:"score"`
}

// ReportExportDuplicateGroup mirrors the tab-side duplicate group.
type ReportExportDuplicateGroup struct {
	FileSHA256         string  `json:"file_sha256"`
	AssetBytes         int64   `json:"asset_bytes"`
	AssetIDs           []int64 `json:"asset_ids"`
	SampleInstancePath string  `json:"sample_instance_path,omitempty"`
}

// MarshalReportExport produces canonical, indented JSON. Indented for
// human inspection; recipients fold it back to compact via tooling if
// they care about wire size.
func MarshalReportExport(report ReportExport) ([]byte, error) {
	return json.MarshalIndent(report, "", "  ")
}

// BuildReportExport packages the analysis state into a ReportExport.
// `reportID` should be a stable identifier (UUID, content hash, etc.)
// generated by the caller — kept caller-provided so the tag/share
// server can pick its own identifier scheme later. Empty is allowed.
func BuildReportExport(
	source ReportExportSource,
	reportID string,
	joxbloxVersion string,
	overall ReportExportOverall,
	grades []ReportExportGrade,
	summary Summary,
	percentiles *CellPercentiles,
	details ReportExportDetails,
) ReportExport {
	out := ReportExport{
		FormatVersion:  ReportExportFormatVersion,
		GeneratedAt:    time.Now().UTC(),
		JoxbloxVersion: joxbloxVersion,
		ReportID:       reportID,
		Source:         source,
		Overall:        overall,
		Grades:         grades,
		Summary:        SummaryToReportExportSummary(summary),
		Details:        details,
	}
	if percentiles != nil {
		out.CellPercentiles = percentilesToReportExport(*percentiles)
	}
	return out
}

// PerformanceGradeToReportExportGrade flattens a PerformanceGrade into
// the JSON-shaped grade entry. Renames TotalValue → P90CellValue so
// the JSON schema doesn't carry over the legacy field name.
func PerformanceGradeToReportExportGrade(grade PerformanceGrade) ReportExportGrade {
	return ReportExportGrade{
		Label:             grade.Label,
		Grade:             grade.Grade,
		Score:             grade.Score,
		Weight:            grade.Weight,
		Value:             grade.Value,
		AvgCellValue:      grade.AvgCellValue,
		P90CellValue:      grade.TotalValue,
		MaxCellValue:      grade.MaxCellValue,
		Description:       grade.Description,
		MetricDescription: grade.MetricDescription,
	}
}

// PerformanceGradesToReportExportGrades is the slice version.
func PerformanceGradesToReportExportGrades(grades []PerformanceGrade) []ReportExportGrade {
	out := make([]ReportExportGrade, len(grades))
	for i, grade := range grades {
		out[i] = PerformanceGradeToReportExportGrade(grade)
	}
	return out
}

// SummaryToReportExportSummary copies a Summary into the JSON shape.
func SummaryToReportExportSummary(summary Summary) ReportExportSummary {
	return ReportExportSummary{
		TotalBytes:                 summary.TotalBytes,
		TextureBytes:               summary.TextureBytes,
		TexturePixelCount:          summary.TexturePixelCount,
		BC1PixelCount:              summary.BC1PixelCount,
		BC3PixelCount:              summary.BC3PixelCount,
		BC1BytesExact:              summary.BC1BytesExact,
		BC3BytesExact:              summary.BC3BytesExact,
		MismatchedPBRMaterialCount: summary.MismatchedPBRMaterialCount,
		PBRMaterialCount:           summary.PBRMaterialCount,
		MeshBytes:                  summary.MeshBytes,
		TriangleCount:              summary.TriangleCount,
		OversizedTextureCount:      summary.OversizedTextureCount,
		DrawCallCount:              summary.DrawCallCount,
		DuplicateCount:             summary.DuplicateCount,
		DuplicateSizeBytes:         summary.DuplicateSizeBytes,
		ReferenceCount:             summary.ReferenceCount,
		UniqueReferenceCount:       summary.UniqueReferenceCount,
		UniqueAssetCount:           summary.UniqueAssetCount,
		ResolvedCount:              summary.ResolvedCount,
		MeshPartCount:              summary.MeshPartCount,
		PartCount:                  summary.PartCount,
		InstanceCount:              summary.InstanceCount,
	}
}

func percentilesToReportExport(percentiles CellPercentiles) *ReportExportPercentiles {
	return &ReportExportPercentiles{
		CellCount:     percentiles.CellCount,
		CellSizeStuds: percentiles.CellSizeStuds,
		WholeFileMode: percentiles.WholeFileMode,
		Metrics: map[string]ReportExportCellMetric{
			"total_bytes":       cellMetricToExport(percentiles.TotalBytes),
			"texture_bytes":     cellMetricToExport(percentiles.TextureBytes),
			"gpu_texture_bytes": cellMetricToExport(percentiles.GPUTextureBytes),
			"mesh_bytes":        cellMetricToExport(percentiles.MeshBytes),
			"triangle_count":    cellMetricToExport(percentiles.TriangleCount),
			"unique_assets":     cellMetricToExport(percentiles.UniqueAssets),
			"mesh_parts":        cellMetricToExport(percentiles.MeshParts),
			"parts":             cellMetricToExport(percentiles.Parts),
			"draw_calls":        cellMetricToExport(percentiles.DrawCalls),
			"instance_count":    cellMetricToExport(percentiles.InstanceCount),
		},
	}
}

func cellMetricToExport(metric CellMetric) ReportExportCellMetric {
	return ReportExportCellMetric{Avg: metric.Avg, P90: metric.P90, Max: metric.Max}
}
