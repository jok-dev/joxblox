package app

import "testing"

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
		got := gradeFromThresholds(tt.value, thresholds)
		if got != tt.expected {
			t.Errorf("gradeFromThresholds(%.1f) = %s, want %s", tt.value, got, tt.expected)
		}
	}
}

func TestComputeMeshComplexityGrade(t *testing.T) {
	tests := []struct {
		triangles int64
		expected  string
	}{
		{0, gradeAPlus},
		{199_999, gradeAPlus},
		{200_000, gradeA},
		{499_999, gradeA},
		{500_000, gradeB},
		{999_999, gradeB},
		{1_000_000, gradeC},
		{1_999_999, gradeC},
		{2_000_000, gradeD},
		{3_999_999, gradeD},
		{4_000_000, gradeE},
		{7_999_999, gradeE},
		{8_000_000, gradeF},
	}
	for _, tt := range tests {
		got := computeMeshComplexityGrade(tt.triangles, 0, false)
		if got.Grade != tt.expected {
			t.Errorf("computeMeshComplexityGrade(%d) = %s, want %s", tt.triangles, got.Grade, tt.expected)
		}
	}
}

func TestComputeMeshComplexityGradeCellPercentile(t *testing.T) {
	got := computeMeshComplexityGrade(8_000_000, 100_000, true)
	if got.Grade != gradeAPlus {
		t.Errorf("expected A+ when cell p90 is 100k (100k/1000 = 100 < 200), got %s", got.Grade)
	}
	got = computeMeshComplexityGrade(100, 499_000, true)
	if got.Grade != gradeA {
		t.Errorf("expected A when cell p90 is 499k (499k/1000 = 499 < 500), got %s", got.Grade)
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
		got := computeDrawCallGrade(tt.drawCalls, 0, false)
		if got.Grade != tt.expected {
			t.Errorf("computeDrawCallGrade(%d) = %s, want %s", tt.drawCalls, got.Grade, tt.expected)
		}
	}
}

func TestComputeDuplicationWasteGrade(t *testing.T) {
	total := int64(100 * megabyte)
	tests := []struct {
		dupBytes int64
		expected string
	}{
		{0, gradeAPlus},
		{1 * megabyte, gradeAPlus},
		{2 * megabyte, gradeA},
		{4 * megabyte, gradeA},
		{5 * megabyte, gradeB},
		{14 * megabyte, gradeB},
		{15 * megabyte, gradeC},
		{24 * megabyte, gradeC},
		{25 * megabyte, gradeD},
		{39 * megabyte, gradeD},
		{40 * megabyte, gradeE},
		{59 * megabyte, gradeE},
		{60 * megabyte, gradeF},
	}
	for _, tt := range tests {
		got := computeDuplicationWasteGrade(tt.dupBytes, total)
		if got.Grade != tt.expected {
			t.Errorf("computeDuplicationWasteGrade(%d, %d) = %s, want %s", tt.dupBytes, total, got.Grade, tt.expected)
		}
	}
}

func TestComputeDuplicationWasteGradeZeroTotal(t *testing.T) {
	got := computeDuplicationWasteGrade(0, 0)
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
		{19 * megabyte, gradeAPlus},
		{20 * megabyte, gradeA},
		{49 * megabyte, gradeA},
		{50 * megabyte, gradeB},
		{99 * megabyte, gradeB},
		{100 * megabyte, gradeC},
		{199 * megabyte, gradeC},
		{200 * megabyte, gradeD},
		{399 * megabyte, gradeD},
		{400 * megabyte, gradeE},
		{799 * megabyte, gradeE},
		{800 * megabyte, gradeF},
	}
	for _, tt := range tests {
		got := computeDownloadSizeGrade(tt.totalBytes, 0, false)
		if got.Grade != tt.expected {
			t.Errorf("computeDownloadSizeGrade(%d) = %s, want %s", tt.totalBytes, got.Grade, tt.expected)
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
		got := computeAssetDiversityGrade(tt.count, 0, false)
		if got.Grade != tt.expected {
			t.Errorf("computeAssetDiversityGrade(%d) = %s, want %s", tt.count, got.Grade, tt.expected)
		}
	}
}

func TestOverallPerformanceGrade(t *testing.T) {
	tests := []struct {
		name     string
		grades   []performanceGrade
		expected string
	}{
		{
			name:     "all A+",
			grades:   []performanceGrade{{Grade: gradeAPlus}, {Grade: gradeAPlus}, {Grade: gradeAPlus}},
			expected: gradeAPlus,
		},
		{
			name:     "all F",
			grades:   []performanceGrade{{Grade: gradeF}, {Grade: gradeF}, {Grade: gradeF}},
			expected: gradeF,
		},
		{
			name:     "mixed A+ and A rounds to A",
			grades:   []performanceGrade{{Grade: gradeAPlus}, {Grade: gradeA}, {Grade: gradeA}},
			expected: gradeA,
		},
		{
			name:     "mixed rounds to D",
			grades:   []performanceGrade{{Grade: gradeA}, {Grade: gradeC}, {Grade: gradeD}, {Grade: gradeD}, {Grade: gradeF}},
			expected: gradeD,
		},
		{
			name:     "empty returns F",
			grades:   []performanceGrade{},
			expected: gradeF,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := overallPerformanceGrade(tt.grades, false)
			if got != tt.expected {
				t.Errorf("overallPerformanceGrade() = %s, want %s", got, tt.expected)
			}
		})
	}
}

func TestOverallPerformanceGradeCappedWithDuplicates(t *testing.T) {
	grades := []performanceGrade{{Grade: gradeAPlus}, {Grade: gradeAPlus}, {Grade: gradeAPlus}}
	got := overallPerformanceGrade(grades, true)
	if got != gradeB {
		t.Errorf("overallPerformanceGrade(all A+, hasDuplicates=true) = %s, want %s", got, gradeB)
	}

	lowGrades := []performanceGrade{{Grade: gradeD}, {Grade: gradeF}}
	got = overallPerformanceGrade(lowGrades, true)
	if got != gradeE {
		t.Errorf("overallPerformanceGrade(D+F, hasDuplicates=true) = %s, want %s (should not raise low grades)", got, gradeE)
	}
}

func TestOverallPerformanceScorePercent(t *testing.T) {
	perfectGrades := []performanceGrade{{Grade: gradeAPlus}, {Grade: gradeAPlus}, {Grade: gradeAPlus}}
	got := overallPerformanceScorePercent(perfectGrades, false)
	if got != 100 {
		t.Errorf("overallPerformanceScorePercent(all A+, false) = %d, want 100", got)
	}

	mixedGrades := []performanceGrade{{Grade: gradeA}, {Grade: gradeC}, {Grade: gradeD}, {Grade: gradeD}, {Grade: gradeF}}
	got = overallPerformanceScorePercent(mixedGrades, false)
	if got != 40 {
		t.Errorf("overallPerformanceScorePercent(mixed, false) = %d, want 40", got)
	}

	got = overallPerformanceScorePercent(perfectGrades, true)
	if got != 67 {
		t.Errorf("overallPerformanceScorePercent(all A+, true) = %d, want 67", got)
	}
}

func TestComputeTextureSizeGrade(t *testing.T) {
	tests := []struct {
		textureBytes int64
		expected     string
	}{
		{0, gradeAPlus},
		{9 * megabyte, gradeAPlus},
		{10 * megabyte, gradeA},
		{24 * megabyte, gradeA},
		{25 * megabyte, gradeB},
		{59 * megabyte, gradeB},
		{60 * megabyte, gradeC},
		{119 * megabyte, gradeC},
		{120 * megabyte, gradeD},
		{249 * megabyte, gradeD},
		{250 * megabyte, gradeE},
		{499 * megabyte, gradeE},
		{500 * megabyte, gradeF},
	}
	for _, tt := range tests {
		got := computeTextureSizeGrade(tt.textureBytes, 0, false)
		if got.Grade != tt.expected {
			t.Errorf("computeTextureSizeGrade(%d) = %s, want %s", tt.textureBytes, got.Grade, tt.expected)
		}
	}
}

func TestComputeMeshSizeGrade(t *testing.T) {
	tests := []struct {
		meshBytes int64
		expected  string
	}{
		{0, gradeAPlus},
		{9 * megabyte, gradeAPlus},
		{10 * megabyte, gradeA},
		{24 * megabyte, gradeA},
		{25 * megabyte, gradeB},
		{59 * megabyte, gradeB},
		{60 * megabyte, gradeC},
		{119 * megabyte, gradeC},
		{120 * megabyte, gradeD},
		{249 * megabyte, gradeD},
		{250 * megabyte, gradeE},
		{499 * megabyte, gradeE},
		{500 * megabyte, gradeF},
	}
	for _, tt := range tests {
		got := computeMeshSizeGrade(tt.meshBytes, 0, false)
		if got.Grade != tt.expected {
			t.Errorf("computeMeshSizeGrade(%d) = %s, want %s", tt.meshBytes, got.Grade, tt.expected)
		}
	}
}

func TestComputeDuplicateCountGrade(t *testing.T) {
	tests := []struct {
		count    int64
		expected string
	}{
		{0, gradeAPlus},
		{1, gradeAPlus},
		{2, gradeA},
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
		got := computeDuplicateCountGrade(tt.count)
		if got.Grade != tt.expected {
			t.Errorf("computeDuplicateCountGrade(%d) = %s, want %s", tt.count, got.Grade, tt.expected)
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
		got := computeOversizedTextureCountGrade(tt.count)
		if got.Grade != tt.expected {
			t.Errorf("computeOversizedTextureCountGrade(%d) = %s, want %s", tt.count, got.Grade, tt.expected)
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
		got := capGradeAtC(tt.grade)
		if got != tt.expected {
			t.Errorf("capGradeAtC(%s) = %s, want %s", tt.grade, got, tt.expected)
		}
	}
}

func TestDuplicatesCappedAtCInProfile(t *testing.T) {
	summary := reportGenerationSummary{
		TotalBytes:         5 * megabyte,
		TextureBytes:       3 * megabyte,
		MeshBytes:          2 * megabyte,
		TriangleCount:      50_000,
		DrawCallCount:      10,
		DuplicateCount:     1,
		DuplicateSizeBytes: 100,
		UniqueAssetCount:   20,
	}

	grades := computePerformanceProfile(reportCellPercentiles{}, summary)

	for _, g := range grades {
		if g.Label == "Duplicates" || g.Label == "Duplication Waste" {
			if gradeToNumeric(g.Grade) > gradeToNumeric(gradeC) {
				t.Errorf("grade %q = %s, expected C or worse when duplicates > 0", g.Label, g.Grade)
			}
		}
	}
}

func TestComputePerformanceProfileIntegration(t *testing.T) {
	summary := reportGenerationSummary{
		TotalBytes:            5 * megabyte,
		TextureBytes:          3 * megabyte,
		MeshBytes:             2 * megabyte,
		TriangleCount:         50_000,
		OversizedTextureCount: 0,
		DrawCallCount:         10,
		DuplicateCount:        0,
		DuplicateSizeBytes:    0,
		UniqueAssetCount:      20,
	}

	grades := computePerformanceProfile(reportCellPercentiles{}, summary)
	if len(grades) != 11 {
		t.Fatalf("expected 11 grades, got %d", len(grades))
	}

	for _, g := range grades {
		if g.Grade != gradeAPlus {
			t.Errorf("grade %q = %s, want %s", g.Label, g.Grade, gradeAPlus)
		}
	}

	overall := overallPerformanceGrade(grades, false)
	if overall != gradeAPlus {
		t.Errorf("overall = %s, want %s", overall, gradeAPlus)
	}
}

func TestComputePerformanceProfileWithCellPercentiles(t *testing.T) {
	summary := reportGenerationSummary{
		TotalBytes:            800 * megabyte,
		TextureBytes:          500 * megabyte,
		MeshBytes:             500 * megabyte,
		TriangleCount:         8_000_000,
		OversizedTextureCount: 0,
		DrawCallCount:         4000,
		UniqueAssetCount:      2000,
		MeshPartCount:         4000,
		PartCount:             10000,
	}
	percentiles := reportCellPercentiles{
		P90TotalBytes:    1 * float64(megabyte),
		P90TextureBytes:  1 * float64(megabyte),
		P90MeshBytes:     512 * 1024,
		P90TriangleCount: 4_000,
		P90UniqueAssets:  20,
		P90DrawCalls:     10,
		P90MeshParts:     10,
		P90Parts:         15,
		CellCount:        100,
		CellSizeStuds:    50,
	}

	grades := computePerformanceProfile(percentiles, summary)
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

func TestCountReportGenerationOversizedTextures(t *testing.T) {
	refs := []positionedRustyAssetToolResult{
		{
			ID:           101,
			RawContent:   "rbxassetid://101",
			InstancePath: "Workspace.BigTexture",
		},
		{
			ID:           202,
			RawContent:   "rbxassetid://202",
			InstancePath: "Workspace.SmallTexture",
		},
	}
	resolved := map[string]reportGenerationResolvedAsset{
		scanAssetReferenceKey(101, "rbxassetid://101"): {
			Stats: rbxlHeatmapAssetStats{TextureBytes: 200_000},
		},
		scanAssetReferenceKey(202, "rbxassetid://202"): {
			Stats: rbxlHeatmapAssetStats{TextureBytes: 10_000},
		},
	}
	mapParts := []rbxlHeatmapMapPart{
		{
			InstancePath: "Workspace.BigTexture",
			SizeX:        5,
			SizeY:        5,
			SizeZ:        5,
		},
		{
			InstancePath: "Workspace.SmallTexture",
			SizeX:        100,
			SizeY:        100,
			SizeZ:        100,
		},
	}

	count := countReportGenerationOversizedTextures(refs, resolved, mapParts, defaultLargeTextureThreshold)
	if count != 1 {
		t.Fatalf("expected 1 oversized texture, got %d", count)
	}
}

func TestComputeCellPercentiles(t *testing.T) {
	cells := []rbxlHeatmapCell{
		{Stats: rbxlHeatmapTotals{ReferenceCount: 5, TotalBytes: 100, TextureBytes: 40, MeshBytes: 60, TriangleCount: 1000, UniqueAssetCount: 3, MeshPartCount: 2, PartCount: 1, DrawCallCount: 2}, MinimumX: 0, MaximumX: 50},
		{Stats: rbxlHeatmapTotals{ReferenceCount: 3, TotalBytes: 200, TextureBytes: 80, MeshBytes: 120, TriangleCount: 2000, UniqueAssetCount: 5, MeshPartCount: 4, PartCount: 3, DrawCallCount: 4}, MinimumX: 50, MaximumX: 100},
		{Stats: rbxlHeatmapTotals{ReferenceCount: 0}},
	}
	percentiles := computeCellPercentiles(cells)
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
	percentiles := computeCellPercentiles(nil)
	if percentiles.CellCount != 0 {
		t.Errorf("expected 0 cells for nil input, got %d", percentiles.CellCount)
	}
}

func TestComputeCellPercentilesUsesMetricSpecificOccupiedCells(t *testing.T) {
	cells := []rbxlHeatmapCell{
		{
			Stats:    rbxlHeatmapTotals{ReferenceCount: 2, TotalBytes: 100, MeshPartCount: 4, DrawCallCount: 5},
			MinimumX: 0,
			MaximumX: 200,
		},
		{
			Stats:    rbxlHeatmapTotals{MeshPartCount: 2, DrawCallCount: 1},
			MinimumX: 200,
			MaximumX: 400,
		},
		{
			Stats:    rbxlHeatmapTotals{PartCount: 6},
			MinimumX: 400,
			MaximumX: 600,
		},
		{
			Stats:    rbxlHeatmapTotals{PartCount: 2},
			MinimumX: 600,
			MaximumX: 800,
		},
	}

	percentiles := computeCellPercentiles(cells)
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
	cells := []rbxlHeatmapCell{
		{Stats: rbxlHeatmapTotals{ReferenceCount: 1, TotalBytes: 100, TriangleCount: 1000}, MinimumX: 0, MaximumX: 50},
		{Stats: rbxlHeatmapTotals{ReferenceCount: 1, TotalBytes: 110, TriangleCount: 1100}, MinimumX: 50, MaximumX: 100},
		{Stats: rbxlHeatmapTotals{ReferenceCount: 1, TotalBytes: 10000, TriangleCount: 500000}, MinimumX: 100, MaximumX: 150},
	}

	percentiles := computeCellPercentiles(cells)
	if percentiles.P90TotalBytes != 10000 {
		t.Fatalf("expected P90TotalBytes 10000 to reflect the top 10%% cells, got %.0f", percentiles.P90TotalBytes)
	}
	if percentiles.P90TriangleCount != 500000 {
		t.Fatalf("expected P90TriangleCount 500000 to reflect the top 10%% cells, got %.0f", percentiles.P90TriangleCount)
	}
}
