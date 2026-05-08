package report

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// TestMarshalReportExportShape pins the JSON field names a viewer or
// downstream tool would key off. Update intentionally — bumping
// ReportExportFormatVersion when shapes change.
func TestMarshalReportExportShape(t *testing.T) {
	export := ReportExport{
		FormatVersion: ReportExportFormatVersion,
		GeneratedAt:   time.Date(2026, 4, 29, 12, 0, 0, 0, time.UTC),
		ReportID:      "rep_test",
		Source: ReportExportSource{
			FileName:      "place.rbxl",
			FileSizeBytes: 12345,
			AssetType:     "Map",
			WorkspaceOnly: true,
		},
		Overall: ReportExportOverall{Grade: "C", ScorePercent: 56},
		Grades: []ReportExportGrade{{
			Label:        "GPU Texture Memory",
			Grade:        "C",
			Score:        3.5,
			Weight:       0.25,
			Value:        "32 MB",
			AvgCellValue: "1 MB",
			P90CellValue: "8 MB",
			MaxCellValue: "32 MB",
		}},
		Summary:         ReportExportSummary{TotalBytes: 100, InstanceCount: 50},
		CellPercentiles: nil,
		Details: ReportExportDetails{
			OversizedTextures: []ReportExportOversizedTexture{{AssetID: 111, Width: 2048, Height: 2048, TextureBytes: 5_000_000, Score: 8192}},
			DuplicateGroups:   []ReportExportDuplicateGroup{{FileSHA256: "abc", AssetBytes: 2048, AssetIDs: []int64{1, 2, 3}}},
		},
	}

	data, err := MarshalReportExport(export)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	jsonText := string(data)

	mustContain := []string{
		`"format_version": "1.0.0"`,
		`"report_id": "rep_test"`,
		`"file_name": "place.rbxl"`,
		`"asset_type": "Map"`,
		`"workspace_only": true`,
		`"grade": "C"`,
		`"score_percent": 56`,
		`"label": "GPU Texture Memory"`,
		`"weight": 0.25`,
		`"avg_cell_value": "1 MB"`,
		`"p90_cell_value": "8 MB"`,
		`"max_cell_value": "32 MB"`,
		`"total_bytes": 100`,
		`"instance_count": 50`,
		`"asset_id": 111`,
		`"texture_bytes": 5000000`,
		`"file_sha256": "abc"`,
		`"asset_ids": [`,
	}
	for _, snippet := range mustContain {
		if !strings.Contains(jsonText, snippet) {
			t.Errorf("expected JSON to contain %q\n\nfull JSON:\n%s", snippet, jsonText)
		}
	}

	mustNotContain := []string{
		// Whole-file mode export should not surface percentiles at all.
		`"cell_percentiles":`,
	}
	for _, snippet := range mustNotContain {
		if strings.Contains(jsonText, snippet) {
			t.Errorf("expected JSON to NOT contain %q (whole-file export should omit cell percentiles)", snippet)
		}
	}

	// Round-trip parse to confirm the JSON is syntactically valid and
	// the field names match the exported types.
	var parsed ReportExport
	if unmarshalErr := json.Unmarshal(data, &parsed); unmarshalErr != nil {
		t.Fatalf("round-trip unmarshal: %v\n\nJSON:\n%s", unmarshalErr, jsonText)
	}
	if parsed.FormatVersion != ReportExportFormatVersion {
		t.Errorf("FormatVersion = %q, want %q", parsed.FormatVersion, ReportExportFormatVersion)
	}
	if parsed.Overall.Grade != "C" {
		t.Errorf("Overall.Grade = %q, want C", parsed.Overall.Grade)
	}
	if len(parsed.Grades) != 1 || parsed.Grades[0].Label != "GPU Texture Memory" {
		t.Errorf("Grades round-trip lost data: %+v", parsed.Grades)
	}
}

func TestBuildReportExportPopulatesPercentilesInSpatialMode(t *testing.T) {
	percentiles := CellPercentiles{
		CellCount:     50,
		CellSizeStuds: 1000,
		WholeFileMode: false,
		TriangleCount: CellMetric{Avg: 1000, P90: 5000, Max: 12000},
	}
	export := BuildReportExport(
		ReportExportSource{},
		"",
		"",
		ReportExportOverall{},
		nil,
		Summary{},
		&percentiles,
		ReportExportDetails{},
	)
	if export.CellPercentiles == nil {
		t.Fatalf("CellPercentiles missing in spatial mode")
	}
	tri, ok := export.CellPercentiles.Metrics["triangle_count"]
	if !ok {
		t.Fatalf("triangle_count metric missing")
	}
	if tri.P90 != 5000 || tri.Max != 12000 {
		t.Errorf("triangle_count metric = %+v, want avg=1000 p90=5000 max=12000", tri)
	}
}
