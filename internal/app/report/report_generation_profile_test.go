package report

import (
	"math"
	"testing"

	"joxblox/internal/format"
	"joxblox/internal/heatmap"
)

func TestGradeFromThresholds(t *testing.T) {
	thresholds := [6]float64{50, 100, 200, 300, 400, 500}

	tests := []struct {
		value    float64
		expected string
	}{
		{0, gradeAPlus},
		{49.9, gradeAPlus},
		{50, gradeA},
		{99.9, gradeA},
		{100, gradeB},
		{199.9, gradeB},
		{200, gradeC},
		{299.9, gradeC},
		{300, gradeD},
		{399.9, gradeD},
		{400, gradeE},
		{499.9, gradeE},
		{500, gradeF},
		{9999, gradeF},
	}
	for _, tt := range tests {
		got := GradeFromThresholds(tt.value, thresholds)
		if got != tt.expected {
			t.Errorf("GradeFromThresholds(%.1f) = %s, want %s", tt.value, got, tt.expected)
		}
	}
}

func TestContinuousScoreFromThresholds(t *testing.T) {
	thresholds := [6]float64{50, 100, 200, 300, 400, 500}

	tests := []struct {
		value    float64
		expected float64
	}{
		{0, 6.0},
		{25, 5.5},
		{50, 5.0},
		{75, 4.5},
		{100, 4.0},
		{150, 3.5},
		{200, 3.0},
		{250, 2.5},
		{300, 2.0},
		{350, 1.5},
		{400, 1.0},
		{450, 0.5},
		{500, 0.0},
		{9999, 0.0},
	}
	for _, tt := range tests {
		got := ContinuousScoreFromThresholds(tt.value, thresholds)
		if math.Abs(got-tt.expected) > 0.001 {
			t.Errorf("ContinuousScoreFromThresholds(%.1f) = %.3f, want %.3f", tt.value, got, tt.expected)
		}
	}
}

func TestContinuousScoreMatchesGradeBoundaries(t *testing.T) {
	thresholds := [6]float64{50, 100, 200, 300, 400, 500}

	boundaryTests := []struct {
		value         float64
		expectedGrade string
		expectedScore float64
	}{
		{0, gradeAPlus, 6.0},
		{50, gradeA, 5.0},
		{100, gradeB, 4.0},
		{200, gradeC, 3.0},
		{300, gradeD, 2.0},
		{400, gradeE, 1.0},
		{500, gradeF, 0.0},
	}
	for _, tt := range boundaryTests {
		grade := GradeFromThresholds(tt.value, thresholds)
		score := ContinuousScoreFromThresholds(tt.value, thresholds)
		if grade != tt.expectedGrade {
			t.Errorf("value=%.0f: grade=%s, want %s", tt.value, grade, tt.expectedGrade)
		}
		if math.Abs(score-tt.expectedScore) > 0.001 {
			t.Errorf("value=%.0f: score=%.3f, want %.3f", tt.value, score, tt.expectedScore)
		}
	}
}

func TestOverallPerformanceScorePercentContinuous(t *testing.T) {
	thresholds := DefaultAssetType().Thresholds

	bareMid := ComputeDownloadSizeGradeWithThresholds(3*format.Megabyte, 0, false, thresholds.TotalSizeMB)
	bareHigh := ComputeDownloadSizeGradeWithThresholds(4*format.Megabyte, 0, false, thresholds.TotalSizeMB)
	if bareMid.Grade != bareHigh.Grade {
		t.Fatal("test expects both values in the same grade bucket")
	}
	if bareMid.Score == bareHigh.Score {
		t.Errorf("continuous scores should differ for different values within same grade: mid=%.3f, high=%.3f", bareMid.Score, bareHigh.Score)
	}
	if bareMid.Score <= bareHigh.Score {
		t.Errorf("lower value should have higher score: mid=%.3f, high=%.3f", bareMid.Score, bareHigh.Score)
	}
}

func TestComputeMeshComplexityGrade(t *testing.T) {
	tests := []struct {
		triangles int64
		expected  string
	}{
		{0, gradeAPlus},
		{4_999, gradeAPlus},
		{5_000, gradeA},
		{14_999, gradeA},
		{15_000, gradeB},
		{19_999, gradeB},
		{20_000, gradeC},
		{34_999, gradeC},
		{35_000, gradeD},
		{44_999, gradeD},
		{45_000, gradeE},
		{59_999, gradeE},
		{60_000, gradeF},
	}
	for _, tt := range tests {
		got := ComputeMeshComplexityGrade(tt.triangles, 0, false)
		if got.Grade != tt.expected {
			t.Errorf("ComputeMeshComplexityGrade(%d) = %s, want %s", tt.triangles, got.Grade, tt.expected)
		}
	}
}

func TestComputeMeshComplexityGradeCellPercentile(t *testing.T) {
	got := ComputeMeshComplexityGrade(8_000_000, 4_000, true)
	if got.Grade != gradeAPlus {
		t.Errorf("expected A+ when cell p90 is 4000 (< 5000), got %s", got.Grade)
	}
	got = ComputeMeshComplexityGrade(100, 10_000, true)
	if got.Grade != gradeA {
		t.Errorf("expected A when cell p90 is 10000 (>= 5000, < 15000), got %s", got.Grade)
	}
}

func TestComputeDrawCallGrade(t *testing.T) {
	tests := []struct {
		drawCalls int64
		expected  string
	}{
		{0, gradeAPlus},
		{99, gradeAPlus},
		{100, gradeA},
		{249, gradeA},
		{250, gradeB},
		{499, gradeB},
		{500, gradeC},
		{999, gradeC},
		{1000, gradeD},
		{1999, gradeD},
		{2000, gradeE},
		{3999, gradeE},
		{4000, gradeF},
	}
	for _, tt := range tests {
		got := ComputeDrawCallGrade(tt.drawCalls, 0, false)
		if got.Grade != tt.expected {
			t.Errorf("ComputeDrawCallGrade(%d) = %s, want %s", tt.drawCalls, got.Grade, tt.expected)
		}
	}
}

func TestComputeDuplicationWasteGrade(t *testing.T) {
	total := int64(100 * format.Megabyte)
	tests := []struct {
		dupBytes int64
		expected string
	}{
		{0, gradeAPlus},
		{1 * format.Megabyte, gradeAPlus},
		{2 * format.Megabyte, gradeA},
		{4 * format.Megabyte, gradeA},
		{5 * format.Megabyte, gradeB},
		{14 * format.Megabyte, gradeB},
		{15 * format.Megabyte, gradeC},
		{24 * format.Megabyte, gradeC},
		{25 * format.Megabyte, gradeD},
		{39 * format.Megabyte, gradeD},
		{40 * format.Megabyte, gradeE},
		{59 * format.Megabyte, gradeE},
		{60 * format.Megabyte, gradeF},
	}
	for _, tt := range tests {
		got := ComputeDuplicationWasteGrade(tt.dupBytes, total)
		if got.Grade != tt.expected {
			t.Errorf("ComputeDuplicationWasteGrade(%d, %d) = %s, want %s", tt.dupBytes, total, got.Grade, tt.expected)
		}
	}
}

func TestComputeDuplicationWasteGradeZeroTotal(t *testing.T) {
	got := ComputeDuplicationWasteGrade(0, 0)
	if got.Grade != gradeAPlus {
		t.Errorf("expected A+ for zero total, got %s", got.Grade)
	}
}

func TestComputeDownloadSizeGrade(t *testing.T) {
	tests := []struct {
		totalBytes int64
		expected   string
	}{
		{0, gradeAPlus},
		{1 * format.Megabyte, gradeAPlus},
		{2 * format.Megabyte, gradeA},
		{4 * format.Megabyte, gradeA},
		{5 * format.Megabyte, gradeB},
		{7 * format.Megabyte, gradeB},
		{8 * format.Megabyte, gradeC},
		{11 * format.Megabyte, gradeC},
		{12 * format.Megabyte, gradeD},
		{19 * format.Megabyte, gradeD},
		{20 * format.Megabyte, gradeE},
		{29 * format.Megabyte, gradeE},
		{30 * format.Megabyte, gradeF},
	}
	for _, tt := range tests {
		got := ComputeDownloadSizeGrade(tt.totalBytes, 0, false)
		if got.Grade != tt.expected {
			t.Errorf("ComputeDownloadSizeGrade(%d) = %s, want %s", tt.totalBytes, got.Grade, tt.expected)
		}
	}
}

func TestComputeAssetDiversityGrade(t *testing.T) {
	tests := []struct {
		count    int
		expected string
	}{
		{0, gradeAPlus},
		{49, gradeAPlus},
		{50, gradeA},
		{99, gradeA},
		{100, gradeB},
		{249, gradeB},
		{250, gradeC},
		{499, gradeC},
		{500, gradeD},
		{999, gradeD},
		{1000, gradeE},
		{1999, gradeE},
		{2000, gradeF},
	}
	for _, tt := range tests {
		got := ComputeAssetDiversityGrade(tt.count, 0, false)
		if got.Grade != tt.expected {
			t.Errorf("ComputeAssetDiversityGrade(%d) = %s, want %s", tt.count, got.Grade, tt.expected)
		}
	}
}

func TestOverallPerformanceGrade(t *testing.T) {
	tests := []struct {
		name     string
		grades   []PerformanceGrade
		expected string
	}{
		{
			name:     "all A+",
			grades:   []PerformanceGrade{{Grade: gradeAPlus}, {Grade: gradeAPlus}, {Grade: gradeAPlus}},
			expected: gradeAPlus,
		},
		{
			name:     "all F",
			grades:   []PerformanceGrade{{Grade: gradeF}, {Grade: gradeF}, {Grade: gradeF}},
			expected: gradeF,
		},
		{
			name:     "mixed A+ and A rounds to A",
			grades:   []PerformanceGrade{{Grade: gradeAPlus}, {Grade: gradeA}, {Grade: gradeA}},
			expected: gradeA,
		},
		{
			name:     "mixed rounds to D",
			grades:   []PerformanceGrade{{Grade: gradeA}, {Grade: gradeC}, {Grade: gradeD}, {Grade: gradeD}, {Grade: gradeF}},
			expected: gradeD,
		},
		{
			name:     "empty returns F",
			grades:   []PerformanceGrade{},
			expected: gradeF,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := OverallPerformanceGrade(tt.grades, false)
			if got != tt.expected {
				t.Errorf("OverallPerformanceGrade() = %s, want %s", got, tt.expected)
			}
		})
	}
}

func TestOverallPerformanceGradeCappedWithDuplicates(t *testing.T) {
	grades := []PerformanceGrade{{Grade: gradeAPlus}, {Grade: gradeAPlus}, {Grade: gradeAPlus}}
	got := OverallPerformanceGrade(grades, true)
	if got != gradeB {
		t.Errorf("OverallPerformanceGrade(all A+, hasDuplicates=true) = %s, want %s", got, gradeB)
	}

	lowGrades := []PerformanceGrade{{Grade: gradeD}, {Grade: gradeF}}
	got = OverallPerformanceGrade(lowGrades, true)
	if got != gradeE {
		t.Errorf("OverallPerformanceGrade(D+F, hasDuplicates=true) = %s, want %s (should not raise low grades)", got, gradeE)
	}
}

func TestOverallPerformanceScorePercent(t *testing.T) {
	perfectGrades := []PerformanceGrade{{Grade: gradeAPlus}, {Grade: gradeAPlus}, {Grade: gradeAPlus}}
	got := OverallPerformanceScorePercent(perfectGrades, false)
	if got != 100 {
		t.Errorf("OverallPerformanceScorePercent(all A+, false) = %d, want 100", got)
	}

	mixedGrades := []PerformanceGrade{{Grade: gradeA}, {Grade: gradeC}, {Grade: gradeD}, {Grade: gradeD}, {Grade: gradeF}}
	got = OverallPerformanceScorePercent(mixedGrades, false)
	if got != 40 {
		t.Errorf("OverallPerformanceScorePercent(mixed, false) = %d, want 40", got)
	}

	got = OverallPerformanceScorePercent(perfectGrades, true)
	if got != 67 {
		t.Errorf("OverallPerformanceScorePercent(all A+, true) = %d, want 67", got)
	}
}

func TestComputeTextureSizeGrade(t *testing.T) {
	tests := []struct {
		textureBytes int64
		expected     string
	}{
		{0, gradeAPlus},
		{1 * format.Megabyte, gradeAPlus},
		{2 * format.Megabyte, gradeA},
		{3 * format.Megabyte, gradeA},
		{4 * format.Megabyte, gradeB},
		{5 * format.Megabyte, gradeB},
		{6 * format.Megabyte, gradeC},
		{7 * format.Megabyte, gradeC},
		{8 * format.Megabyte, gradeD},
		{11 * format.Megabyte, gradeD},
		{12 * format.Megabyte, gradeE},
		{19 * format.Megabyte, gradeE},
		{20 * format.Megabyte, gradeF},
	}
	for _, tt := range tests {
		got := ComputeTextureSizeGrade(tt.textureBytes, 0, false)
		if got.Grade != tt.expected {
			t.Errorf("ComputeTextureSizeGrade(%d) = %s, want %s", tt.textureBytes, got.Grade, tt.expected)
		}
	}
}

func TestComputeMeshSizeGrade(t *testing.T) {
	tests := []struct {
		meshBytes int64
		expected  string
	}{
		{0, gradeAPlus},
		{format.Megabyte / 2, gradeAPlus},
		{1 * format.Megabyte, gradeA},
		{format.Megabyte + format.Megabyte/2, gradeA},
		{2 * format.Megabyte, gradeB},
		{format.Megabyte*2 + format.Megabyte/2, gradeB},
		{3 * format.Megabyte, gradeC},
		{4 * format.Megabyte, gradeC},
		{5 * format.Megabyte, gradeD},
		{9 * format.Megabyte, gradeD},
		{10 * format.Megabyte, gradeE},
		{14 * format.Megabyte, gradeE},
		{15 * format.Megabyte, gradeF},
	}
	for _, tt := range tests {
		got := ComputeMeshSizeGrade(tt.meshBytes, 0, false)
		if got.Grade != tt.expected {
			t.Errorf("ComputeMeshSizeGrade(%d) = %s, want %s", tt.meshBytes, got.Grade, tt.expected)
		}
	}
}

func TestComputeDuplicateCountGrade(t *testing.T) {
	tests := []struct {
		count    int64
		expected string
	}{
		{0, gradeAPlus},
		{1, gradeA},
		{4, gradeA},
		{5, gradeB},
		{14, gradeB},
		{15, gradeC},
		{39, gradeC},
		{40, gradeD},
		{79, gradeD},
		{80, gradeE},
		{149, gradeE},
		{150, gradeF},
	}
	for _, tt := range tests {
		got := ComputeDuplicateCountGrade(tt.count)
		if got.Grade != tt.expected {
			t.Errorf("ComputeDuplicateCountGrade(%d) = %s, want %s", tt.count, got.Grade, tt.expected)
		}
	}
}

func TestComputeOversizedTextureCountGrade(t *testing.T) {
	tests := []struct {
		count    int
		expected string
	}{
		{0, gradeAPlus},
		{1, gradeA},
		{2, gradeA},
		{3, gradeB},
		{5, gradeB},
		{6, gradeC},
		{9, gradeC},
		{10, gradeD},
		{14, gradeD},
		{15, gradeE},
		{24, gradeE},
		{25, gradeF},
	}
	for _, tt := range tests {
		got := ComputeOversizedTextureCountGrade(tt.count)
		if got.Grade != tt.expected {
			t.Errorf("ComputeOversizedTextureCountGrade(%d) = %s, want %s", tt.count, got.Grade, tt.expected)
		}
	}
}

func TestCapGradeAtC(t *testing.T) {
	tests := []struct {
		grade    string
		expected string
	}{
		{gradeAPlus, gradeC},
		{gradeA, gradeC},
		{gradeB, gradeC},
		{gradeC, gradeC},
		{gradeD, gradeD},
		{gradeE, gradeE},
		{gradeF, gradeF},
	}
	for _, tt := range tests {
		got := CapGradeAtC(tt.grade)
		if got != tt.expected {
			t.Errorf("CapGradeAtC(%s) = %s, want %s", tt.grade, got, tt.expected)
		}
	}
}

func TestDuplicatesCappedAtCInProfile(t *testing.T) {
	summary := Summary{
		TotalBytes:         5 * format.Megabyte,
		TextureBytes:       3 * format.Megabyte,
		MeshBytes:          2 * format.Megabyte,
		TriangleCount:      50_000,
		DrawCallCount:      10,
		DuplicateCount:     1,
		DuplicateSizeBytes: 100,
		UniqueAssetCount:   20,
	}

	grades := ComputePerformanceProfile(CellPercentiles{}, summary)

	for _, g := range grades {
		if g.Label == "Duplicates" || g.Label == "Duplication Waste" {
			if GradeToNumeric(g.Grade) > GradeToNumeric(gradeC) {
				t.Errorf("grade %q = %s, expected C or worse when duplicates > 0", g.Label, g.Grade)
			}
		}
	}
}

func TestComputePerformanceProfileIntegration(t *testing.T) {
	summary := Summary{
		TotalBytes:            1 * format.Megabyte,
		TextureBytes:          format.Megabyte / 2,
		MeshBytes:             format.Megabyte / 2,
		TriangleCount:         4_000,
		OversizedTextureCount: 0,
		DrawCallCount:         10,
		DuplicateCount:        0,
		DuplicateSizeBytes:    0,
		UniqueAssetCount:      20,
	}

	grades := ComputePerformanceProfile(CellPercentiles{}, summary)
	if len(grades) != 11 {
		t.Fatalf("expected 11 grades, got %d", len(grades))
	}

	for _, g := range grades {
		if g.Grade != gradeAPlus {
			t.Errorf("grade %q = %s, want %s", g.Label, g.Grade, gradeAPlus)
		}
	}

	overall := OverallPerformanceGrade(grades, false)
	if overall != gradeAPlus {
		t.Errorf("overall = %s, want %s", overall, gradeAPlus)
	}
}

func TestComputePerformanceProfileWithCellPercentiles(t *testing.T) {
	summary := Summary{
		TotalBytes:            800 * format.Megabyte,
		TextureBytes:          500 * format.Megabyte,
		MeshBytes:             500 * format.Megabyte,
		TriangleCount:         8_000_000,
		OversizedTextureCount: 0,
		DrawCallCount:         4000,
		UniqueAssetCount:      2000,
		MeshPartCount:         4000,
		PartCount:             10000,
	}
	percentiles := CellPercentiles{
		P90TotalBytes:    1 * float64(format.Megabyte),
		P90TextureBytes:  1 * float64(format.Megabyte),
		P90MeshBytes:     512 * 1024,
		P90TriangleCount: 4_000,
		P90UniqueAssets:  20,
		P90DrawCalls:     10,
		P90MeshParts:     10,
		P90Parts:         15,
		CellCount:        100,
		CellSizeStuds:    50,
	}

	grades := ComputePerformanceProfile(percentiles, summary)
	for _, g := range grades {
		if g.Label == "Duplicates" || g.Label == "Duplication Waste" {
			continue
		}
		if g.Grade != gradeAPlus {
			t.Errorf("grade %q = %s, want A+ (cell p90 values are small despite large totals)", g.Label, g.Grade)
		}
		if g.Label == "Oversized Textures" {
			if g.TotalValue != "" {
				t.Errorf("grade %q should not report p90/cell, got %q", g.Label, g.TotalValue)
			}
			continue
		}
		if g.TotalValue == "" {
			t.Errorf("grade %q should have p90/cell TotalValue when cell percentiles are used", g.Label)
		}
		if g.TotalValue != "" && g.Label != "Duplicates" && g.Label != "Duplication Waste" && g.TotalValue[len(g.TotalValue)-8:] != "p90/cell" {
			t.Errorf("grade %q should report p90/cell, got %q", g.Label, g.TotalValue)
		}
	}
}

func TestComputeCellPercentiles(t *testing.T) {
	cells := []heatmap.Cell{
		{Stats: heatmap.Totals{ReferenceCount: 5, TotalBytes: 100, TextureBytes: 40, MeshBytes: 60, TriangleCount: 1000, UniqueAssetCount: 3, MeshPartCount: 2, PartCount: 1, DrawCallCount: 2}, MinimumX: 0, MaximumX: 50},
		{Stats: heatmap.Totals{ReferenceCount: 3, TotalBytes: 200, TextureBytes: 80, MeshBytes: 120, TriangleCount: 2000, UniqueAssetCount: 5, MeshPartCount: 4, PartCount: 3, DrawCallCount: 4}, MinimumX: 50, MaximumX: 100},
		{Stats: heatmap.Totals{ReferenceCount: 0}},
	}
	percentiles := ComputeCellPercentiles(cells)
	if percentiles.CellCount != 2 {
		t.Fatalf("expected 2 occupied cells, got %d", percentiles.CellCount)
	}
	if percentiles.P90TotalBytes != 200 {
		t.Errorf("expected P90TotalBytes=200, got %.0f", percentiles.P90TotalBytes)
	}
	if percentiles.P90TriangleCount != 2000 {
		t.Errorf("expected P90TriangleCount=2000, got %.0f", percentiles.P90TriangleCount)
	}
	if percentiles.P90MeshParts != 4 {
		t.Errorf("expected P90MeshParts=4, got %.0f", percentiles.P90MeshParts)
	}
	if percentiles.P90Parts != 3 {
		t.Errorf("expected P90Parts=3, got %.0f", percentiles.P90Parts)
	}
	if percentiles.P90DrawCalls != 4 {
		t.Errorf("expected P90DrawCalls=4, got %.0f", percentiles.P90DrawCalls)
	}
	if percentiles.CellSizeStuds != 50 {
		t.Errorf("expected CellSizeStuds=50, got %.0f", percentiles.CellSizeStuds)
	}
}

func TestComputeCellPercentilesEmpty(t *testing.T) {
	percentiles := ComputeCellPercentiles(nil)
	if percentiles.CellCount != 0 {
		t.Errorf("expected 0 cells for nil input, got %d", percentiles.CellCount)
	}
}

func TestComputeCellPercentilesUsesMetricSpecificOccupiedCells(t *testing.T) {
	cells := []heatmap.Cell{
		{
			Stats:    heatmap.Totals{ReferenceCount: 2, TotalBytes: 100, MeshPartCount: 4, DrawCallCount: 5},
			MinimumX: 0,
			MaximumX: 200,
		},
		{
			Stats:    heatmap.Totals{MeshPartCount: 2, DrawCallCount: 1},
			MinimumX: 200,
			MaximumX: 400,
		},
		{
			Stats:    heatmap.Totals{PartCount: 6},
			MinimumX: 400,
			MaximumX: 600,
		},
		{
			Stats:    heatmap.Totals{PartCount: 2},
			MinimumX: 600,
			MaximumX: 800,
		},
	}

	percentiles := ComputeCellPercentiles(cells)
	if percentiles.CellCount != 1 {
		t.Fatalf("expected 1 reference-occupied cell, got %d", percentiles.CellCount)
	}
	if percentiles.P90TotalBytes != 100 {
		t.Fatalf("expected P90TotalBytes 100, got %.0f", percentiles.P90TotalBytes)
	}
	if percentiles.P90MeshParts != 4 {
		t.Fatalf("expected P90MeshParts 4 across 2 meshpart cells, got %.0f", percentiles.P90MeshParts)
	}
	if percentiles.P90Parts != 6 {
		t.Fatalf("expected P90Parts 6 across 2 part cells, got %.0f", percentiles.P90Parts)
	}
	if percentiles.P90DrawCalls != 5 {
		t.Fatalf("expected P90DrawCalls 5 across 2 draw-call cells, got %.0f", percentiles.P90DrawCalls)
	}
}

func TestComputeCellPercentilesCaptureTopTenPercent(t *testing.T) {
	cells := []heatmap.Cell{
		{Stats: heatmap.Totals{ReferenceCount: 1, TotalBytes: 100, TriangleCount: 1000}, MinimumX: 0, MaximumX: 50},
		{Stats: heatmap.Totals{ReferenceCount: 1, TotalBytes: 110, TriangleCount: 1100}, MinimumX: 50, MaximumX: 100},
		{Stats: heatmap.Totals{ReferenceCount: 1, TotalBytes: 10000, TriangleCount: 500000}, MinimumX: 100, MaximumX: 150},
	}

	percentiles := ComputeCellPercentiles(cells)
	if percentiles.P90TotalBytes != 10000 {
		t.Fatalf("expected P90TotalBytes 10000 to reflect the top 10%% cells, got %.0f", percentiles.P90TotalBytes)
	}
	if percentiles.P90TriangleCount != 500000 {
		t.Fatalf("expected P90TriangleCount 500000 to reflect the top 10%% cells, got %.0f", percentiles.P90TriangleCount)
	}
}
