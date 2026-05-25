package report

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// BuildProfileExport turns a GenerateProfile result into the canonical
// ReportExport JSON document and a suggested file name. It mirrors the export
// the Fyne report tab produced: a stable-ish report ID, the source file's size,
// the overall grade, the per-metric grades, the flattened summary, and the
// detail lists. Spatial percentiles are only emitted when graded per-cell.
func BuildProfileExport(filePath string, scope string, result ProfileResult) ([]byte, string, error) {
	source := ReportExportSource{
		FileName:  filepath.Base(strings.TrimSpace(filePath)),
		AssetType: result.AssetType.Label,
		Scope:     strings.TrimSpace(scope),
	}
	if info, statErr := os.Stat(filePath); statErr == nil {
		source.FileSizeBytes = info.Size()
	}

	overall := ReportExportOverall{
		Grade:         result.OverallGrade,
		ScorePercent:  result.OverallScore,
		HasDuplicates: result.HasDuplicates,
	}

	details := ReportExportDetails{
		MismatchedPBRMaterials: append([]MismatchedPBRMaterialDetail(nil), result.MismatchedPBR...),
		OversizedTextures:      append([]ReportExportOversizedTexture(nil), result.Oversized...),
		DuplicateGroups:        append([]ReportExportDuplicateGroup(nil), result.DuplicateGroups...),
	}

	var percentilesPtr *CellPercentiles
	if !result.Percentiles.WholeFileMode && result.Percentiles.CellCount > 0 {
		percentiles := result.Percentiles
		percentilesPtr = &percentiles
	}

	reportExport := BuildReportExport(
		source,
		buildReportID(filePath, result.Summary),
		"",
		overall,
		PerformanceGradesToReportExportGrades(result.Grades),
		result.Summary,
		percentilesPtr,
		details,
	)

	jsonBytes, marshalErr := MarshalReportExport(reportExport)
	if marshalErr != nil {
		return nil, "", fmt.Errorf("encode report: %w", marshalErr)
	}
	return jsonBytes, defaultReportExportFileName(filePath, result.AssetType.Label), nil
}

// buildReportID produces a stable-ish per-export identifier, folding in the
// source path, asset count, and timestamp so two exports of the same file get
// distinct IDs.
func buildReportID(filePath string, summary Summary) string {
	hash := sha1.New()
	hash.Write([]byte(filePath))
	hash.Write([]byte(fmt.Sprintf(":%d:%d:%d:%d", summary.UniqueAssetCount, summary.TotalBytes, summary.InstanceCount, time.Now().UnixNano())))
	return "rep_" + hex.EncodeToString(hash.Sum(nil))[:16]
}

func defaultReportExportFileName(filePath string, assetTypeLabel string) string {
	base := strings.TrimSuffix(filepath.Base(strings.TrimSpace(filePath)), filepath.Ext(filePath))
	if base == "" {
		base = "report"
	}
	parts := []string{base}
	if trimmed := strings.TrimSpace(assetTypeLabel); trimmed != "" {
		parts = append(parts, strings.ReplaceAll(strings.ToLower(trimmed), " ", "-"))
	}
	parts = append(parts, time.Now().Format("20060102-150405"))
	return strings.Join(parts, "_") + ".json"
}
