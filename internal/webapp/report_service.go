package webapp

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"joxblox/internal/app/report"

	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// ReportGrade is one serializable performance-profile row for the web report
// view. Display strings are pre-formatted Go-side so byte sizes stay consistent
// with the rest of the app.
type ReportGrade struct {
	Grade             string `json:"grade"`
	Label             string `json:"label"`
	Value             string `json:"value"`
	AvgCellValue      string `json:"avgCellValue"`
	MaxCellValue      string `json:"maxCellValue"`
	Description       string `json:"description"`
	MetricDescription string `json:"metricDescription"`
	// HasDetail marks rows whose "View" button opens a detail list (Mismatched
	// PBR Maps, Oversized Textures, Duplicates).
	HasDetail bool `json:"hasDetail"`
}

// ReportDetailRow is one entry in a "View" detail list: a bold header line plus
// optional sub-lines, all pre-formatted.
type ReportDetailRow struct {
	Header string   `json:"header"`
	Lines  []string `json:"lines"`
}

// ReportResult is the full serializable output of a report generation run.
type ReportResult struct {
	HasData        bool   `json:"hasData"`
	AssetTypeLabel string `json:"assetTypeLabel"`

	OverallGrade            string `json:"overallGrade"`
	OverallScore            int    `json:"overallScore"`
	OverallGradeIgnoringBan string `json:"overallGradeIgnoringBan"`
	OverallScoreIgnoringBan int    `json:"overallScoreIgnoringBan"`

	RangeNote     string `json:"rangeNote"`
	CellCount     int    `json:"cellCount"`
	WholeFileMode bool   `json:"wholeFileMode"`

	Grades []ReportGrade `json:"grades"`

	HasDuplicates bool    `json:"hasDuplicates"`
	BannedCount   int     `json:"bannedCount"`
	BannedLimitMB float64 `json:"bannedLimitMb"`

	Warning       string `json:"warning"`
	WarningDetail string `json:"warningDetail"`

	MismatchedPBR []ReportDetailRow `json:"mismatchedPbr"`
	Oversized     []ReportDetailRow `json:"oversized"`
	Banned        []ReportDetailRow `json:"banned"`
	Duplicates    []ReportDetailRow `json:"duplicates"`
}

// ReportProgress is emitted on the "report:progress" event while resolving
// assets during report generation.
type ReportProgress struct {
	Done  int `json:"done"`
	Total int `json:"total"`
}

// ReportService generates performance-profile reports for the web UI, reusing
// the shared Fyne-free report engine. The most recent run's profile is cached
// so the Save…JSON/HTML/PDF exports can be rebuilt without re-running analysis.
type ReportService struct {
	ctx context.Context

	mu           sync.Mutex
	lastProfile  report.ProfileResult
	lastHasData  bool
	lastFilePath string
	lastScope    string
}

func NewReportService() *ReportService { return &ReportService{} }

func (s *ReportService) startup(ctx context.Context) { s.ctx = ctx }

// ListAssetTypes returns the selectable asset-type labels for the report
// generation dropdown.
func (s *ReportService) ListAssetTypes() []string {
	return report.AssetTypeLabels()
}

// GenerateReport runs the report engine on the file and returns the graded
// performance profile. It emits "report:progress" events during resolution and
// caches the profile for a later Save…JSON/HTML/PDF call.
func (s *ReportService) GenerateReport(path string, assetTypeLabel string, pathFilter string) (ReportResult, error) {
	trimmedPath := strings.TrimSpace(path)
	if trimmedPath == "" {
		return ReportResult{}, fmt.Errorf("Select an .rbxl or .rbxm file first.")
	}

	prefixes := parsePathPrefixes(pathFilter)
	profile, err := report.GenerateProfile(trimmedPath, assetTypeLabel, prefixes, func(done, total int) {
		// Throttle so a many-asset resolve doesn't flood the webview.
		if done == total || done%5 == 0 {
			wailsRuntime.EventsEmit(s.ctx, "report:progress", ReportProgress{Done: done, Total: total})
		}
	})
	if err != nil {
		return ReportResult{}, err
	}

	s.mu.Lock()
	s.lastProfile = profile
	s.lastHasData = profile.HasData
	s.lastFilePath = trimmedPath
	s.lastScope = strings.Join(prefixes, ",")
	s.mu.Unlock()

	return buildReportResult(profile), nil
}

// SaveReportJSON writes the most recent report's JSON export via a native save
// dialog. Returns false (without error) if the user cancels.
func (s *ReportService) SaveReportJSON() (bool, error) {
	filePath, scope, profile, ok := s.cachedProfile()
	if !ok {
		return false, fmt.Errorf("Generate a report before exporting.")
	}
	jsonBytes, defaultName, err := report.BuildProfileExport(filePath, scope, profile)
	if err != nil {
		return false, err
	}
	return s.saveBytes("Export report as JSON", defaultName, "JSON report", "*.json", jsonBytes)
}

// SaveReportHTML writes the most recent report as a self-contained HTML file.
func (s *ReportService) SaveReportHTML() (bool, error) {
	filePath, _, profile, ok := s.cachedProfile()
	if !ok {
		return false, fmt.Errorf("Generate a report before exporting.")
	}
	htmlBytes := []byte(report.BuildProfileHTML(filePath, profile))
	defaultName := report.DefaultReportExportBaseName(filePath, profile.AssetType.Label) + ".html"
	return s.saveBytes("Export report as HTML", defaultName, "HTML report", "*.html", htmlBytes)
}

// SaveReportPDF writes the most recent report as a PDF file.
func (s *ReportService) SaveReportPDF() (bool, error) {
	filePath, _, profile, ok := s.cachedProfile()
	if !ok {
		return false, fmt.Errorf("Generate a report before exporting.")
	}
	pdfBytes, err := report.BuildProfilePDF(filePath, profile)
	if err != nil {
		return false, err
	}
	defaultName := report.DefaultReportExportBaseName(filePath, profile.AssetType.Label) + ".pdf"
	return s.saveBytes("Export report as PDF", defaultName, "PDF report", "*.pdf", pdfBytes)
}

// cachedProfile returns the last generated profile and its source context. The
// bool is false when no report with data has been generated yet.
func (s *ReportService) cachedProfile() (string, string, report.ProfileResult, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.lastHasData {
		return "", "", report.ProfileResult{}, false
	}
	return s.lastFilePath, s.lastScope, s.lastProfile, true
}

func (s *ReportService) saveBytes(title, defaultName, filterName, filterPattern string, data []byte) (bool, error) {
	return saveBytesViaDialog(s.ctx, title, defaultName, filterName, filterPattern, data)
}

func buildReportResult(profile report.ProfileResult) ReportResult {
	result := ReportResult{
		HasData:        profile.HasData,
		AssetTypeLabel: profile.AssetType.Label,
		Warning:        profile.Warning.Summary,
		WarningDetail:  profile.Warning.DetailText,
	}
	if !profile.HasData {
		return result
	}

	result.OverallGrade = profile.OverallGrade
	result.OverallScore = profile.OverallScore
	result.OverallGradeIgnoringBan = profile.OverallGradeIgnoringBan
	result.OverallScoreIgnoringBan = profile.OverallScoreIgnoringBan
	result.HasDuplicates = profile.HasDuplicates
	result.BannedCount = len(profile.Banned)
	result.BannedLimitMB = profile.AssetType.BannedTextureSizeMB
	result.CellCount = profile.Percentiles.CellCount
	result.WholeFileMode = profile.Percentiles.WholeFileMode
	result.RangeNote = report.RangeNote(profile.Percentiles)

	result.Grades = make([]ReportGrade, 0, len(profile.Grades))
	for _, g := range profile.Grades {
		result.Grades = append(result.Grades, ReportGrade{
			Grade:             g.Grade,
			Label:             g.Label,
			Value:             g.Value,
			AvgCellValue:      g.AvgCellValue,
			MaxCellValue:      g.MaxCellValue,
			Description:       g.Description,
			MetricDescription: g.MetricDescription,
			HasDetail:         gradeHasDetail(g.Label, profile),
		})
	}

	result.MismatchedPBR = toReportDetailRows(report.MismatchedPBRDetailRows(profile.MismatchedPBR))
	result.Oversized = toReportDetailRows(report.OversizedDetailRows(profile.Oversized))
	result.Banned = toReportDetailRows(report.BannedDetailRows(profile.Banned))
	result.Duplicates = toReportDetailRows(report.DuplicateDetailRows(profile.DuplicateGroups))
	return result
}

func toReportDetailRows(rows []report.ProfileDetailRow) []ReportDetailRow {
	out := make([]ReportDetailRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, ReportDetailRow{Header: row.Header, Lines: row.Lines})
	}
	return out
}

func gradeHasDetail(label string, profile report.ProfileResult) bool {
	switch label {
	case "Mismatched PBR Maps":
		return len(profile.MismatchedPBR) > 0
	case "Oversized Textures":
		return len(profile.Oversized) > 0
	case "Duplicates":
		return len(profile.DuplicateGroups) > 0
	default:
		return false
	}
}
