package report

import (
	"bytes"
	"fmt"
	"html"
	"path/filepath"
	"strings"
	"time"

	"github.com/jung-kurt/gofpdf"
)

// gradeRGB maps a letter grade to its display colour, shared by the HTML and
// PDF exports (and mirroring the web view's palette).
func gradeRGB(grade string) (int, int, int) {
	switch grade {
	case "A+":
		return 0, 200, 83
	case "A":
		return 76, 175, 80
	case "B":
		return 139, 195, 74
	case "C":
		return 255, 193, 7
	case "D":
		return 255, 152, 0
	case "E":
		return 255, 87, 34
	default:
		return 244, 67, 54
	}
}

func gradeHex(grade string) string {
	r, g, b := gradeRGB(grade)
	return fmt.Sprintf("#%02x%02x%02x", r, g, b)
}

// profileExportTitle builds the document heading for a report export.
func profileExportTitle(filePath string, result ProfileResult) string {
	base := filepath.Base(strings.TrimSpace(filePath))
	if base == "" {
		base = "Report"
	}
	if label := strings.TrimSpace(result.AssetType.Label); label != "" {
		return fmt.Sprintf("%s — %s Performance Profile", base, label)
	}
	return base + " — Performance Profile"
}

// spatialColumns reports whether the profile was graded over spatial cells (so
// the per-cell columns are meaningful) rather than the whole file.
func spatialColumns(result ProfileResult) bool {
	return !result.Percentiles.WholeFileMode && result.Percentiles.CellCount > 0
}

const profileReportCSS = `
:root { color-scheme: dark; }
* { box-sizing: border-box; }
body { margin: 0; padding: 32px; background: #18181b; color: #e4e4e7; font: 14px/1.5 -apple-system, Segoe UI, Roboto, sans-serif; }
header h1 { margin: 0 0 4px; font-size: 22px; }
header .meta { color: #71717a; font-size: 12px; margin: 0 0 20px; }
header .meta code { color: #a1a1aa; }
.overall { font-size: 30px; font-weight: 700; margin: 8px 0 4px; }
.range-note { color: #71717a; font-size: 12px; margin-bottom: 20px; }
.banner { padding: 10px 14px; border-radius: 6px; margin: 12px 0; font-weight: 600; }
.banner.warn { background: #3a2e00; color: #fbbf24; }
.banner.banned { background: #5a1414; color: #fca5a5; }
table.grades { width: 100%; border-collapse: collapse; margin-bottom: 28px; }
table.grades th { text-align: left; color: #71717a; font-weight: 500; font-style: italic; padding: 6px 12px; border-bottom: 1px solid #2e2e33; }
table.grades td { padding: 8px 12px; border-bottom: 1px solid #2e2e33; vertical-align: middle; }
.grade { font-size: 18px; font-weight: 700; text-align: center; width: 48px; }
.metric { font-weight: 600; }
section.detail { margin-bottom: 24px; }
section.detail h2 { font-size: 16px; border-bottom: 1px solid #2e2e33; padding-bottom: 6px; }
.detail-row { padding: 6px 0; border-bottom: 1px solid #232327; }
.detail-row .header { font-weight: 600; }
.detail-row .line { color: #a1a1aa; padding-left: 16px; font-size: 12px; }
`

// BuildProfileHTML renders a GenerateProfile result as a self-contained HTML
// document: the overall grade, the per-metric grade table, and the drill-down
// detail lists. It embeds CSS inline and uses no external scripts, so the saved
// file renders offline.
func BuildProfileHTML(filePath string, result ProfileResult) string {
	title := profileExportTitle(filePath, result)
	spatial := spatialColumns(result)

	var b strings.Builder
	b.WriteString("<!DOCTYPE html>\n<html lang=\"en\">\n<head>\n<meta charset=\"utf-8\">\n<title>")
	b.WriteString(html.EscapeString(title))
	b.WriteString("</title>\n<style>")
	b.WriteString(profileReportCSS)
	b.WriteString("</style>\n</head>\n<body>\n<header><h1>")
	b.WriteString(html.EscapeString(title))
	b.WriteString("</h1><p class=\"meta\">Generated ")
	b.WriteString(html.EscapeString(time.Now().Format("2006-01-02 15:04:05")))
	if source := strings.TrimSpace(filePath); source != "" {
		b.WriteString(" &middot; Source: <code>")
		b.WriteString(html.EscapeString(source))
		b.WriteString("</code>")
	}
	b.WriteString("</p></header>\n")

	if !result.HasData {
		b.WriteString("<p>No assets found in the selected file.</p>\n</body>\n</html>\n")
		return b.String()
	}

	if summary := strings.TrimSpace(result.Warning.Summary); summary != "" {
		b.WriteString("<div class=\"banner warn\">")
		b.WriteString(html.EscapeString(summary))
		b.WriteString("</div>\n")
	}

	b.WriteString(fmt.Sprintf("<div class=\"overall\" style=\"color:%s\">Overall: %s (%d%%)</div>\n",
		gradeHex(result.OverallGrade), html.EscapeString(result.OverallGrade), result.OverallScore))
	b.WriteString("<div class=\"range-note\">")
	b.WriteString(html.EscapeString(RangeNote(result.Percentiles)))
	b.WriteString("</div>\n")

	if len(result.Banned) > 0 {
		limit := ""
		if result.AssetType.BannedTextureSizeMB > 0 {
			limit = fmt.Sprintf("%.2f MB ", result.AssetType.BannedTextureSizeMB)
		}
		b.WriteString(fmt.Sprintf("<div class=\"banner banned\">BANNED: %d image(s) over the %sGPU-memory limit — overall grade forced to F</div>\n",
			len(result.Banned), limit))
	}

	b.WriteString("<table class=\"grades\">\n<thead><tr><th></th><th>Metric</th><th>value</th>")
	if spatial {
		b.WriteString("<th>typical</th><th>max/cell</th>")
	}
	b.WriteString("</tr></thead>\n<tbody>\n")
	for _, g := range result.Grades {
		b.WriteString("<tr><td class=\"grade\" style=\"color:")
		b.WriteString(gradeHex(g.Grade))
		b.WriteString("\">")
		b.WriteString(html.EscapeString(g.Grade))
		b.WriteString("</td><td class=\"metric\">")
		b.WriteString(html.EscapeString(g.Label))
		b.WriteString("</td><td>")
		b.WriteString(html.EscapeString(g.Value))
		b.WriteString("</td>")
		if spatial {
			b.WriteString("<td>")
			b.WriteString(html.EscapeString(g.AvgCellValue))
			b.WriteString("</td><td>")
			b.WriteString(html.EscapeString(g.MaxCellValue))
			b.WriteString("</td>")
		}
		b.WriteString("</tr>\n")
	}
	b.WriteString("</tbody>\n</table>\n")

	writeHTMLDetailSection(&b, "Mismatched PBR Maps", MismatchedPBRDetailRows(result.MismatchedPBR))
	writeHTMLDetailSection(&b, "Oversized Textures", OversizedDetailRows(result.Oversized))
	writeHTMLDetailSection(&b, "Banned Images", BannedDetailRows(result.Banned))
	writeHTMLDetailSection(&b, "Duplicates", DuplicateDetailRows(result.DuplicateGroups))

	b.WriteString("</body>\n</html>\n")
	return b.String()
}

func writeHTMLDetailSection(b *strings.Builder, title string, rows []ProfileDetailRow) {
	if len(rows) == 0 {
		return
	}
	b.WriteString(fmt.Sprintf("<section class=\"detail\">\n<h2>%s (%d)</h2>\n", html.EscapeString(title), len(rows)))
	for _, row := range rows {
		b.WriteString("<div class=\"detail-row\"><div class=\"header\">")
		b.WriteString(html.EscapeString(row.Header))
		b.WriteString("</div>")
		for _, line := range row.Lines {
			b.WriteString("<div class=\"line\">")
			b.WriteString(html.EscapeString(line))
			b.WriteString("</div>")
		}
		b.WriteString("</div>\n")
	}
	b.WriteString("</section>\n")
}

// PDF page geometry for the report export (A4 portrait).
const (
	profilePDFMarginMM    = 14.0
	profilePDFHeaderFont  = 15.0
	profilePDFMetaFont    = 8.5
	profilePDFOverallFont = 18.0
	profilePDFBodyFont    = 9.5
	profilePDFSectionFont = 12.0
)

// BuildProfilePDF renders a GenerateProfile result to a PDF byte stream: the
// overall grade, the per-metric grade table, and the drill-down detail lists.
// Built with gofpdf so it needs no external renderer.
func BuildProfilePDF(filePath string, result ProfileResult) ([]byte, error) {
	pdf := gofpdf.New("P", "mm", "A4", "")
	pdf.SetMargins(profilePDFMarginMM, profilePDFMarginMM, profilePDFMarginMM)
	pdf.SetAutoPageBreak(true, profilePDFMarginMM)
	pdf.AddPage()

	pageWidth, _ := pdf.GetPageSize()
	contentWidth := pageWidth - 2*profilePDFMarginMM

	pdf.SetFont("Helvetica", "B", profilePDFHeaderFont)
	pdf.MultiCell(0, 7, profileExportTitle(filePath, result), "", "L", false)
	pdf.SetFont("Helvetica", "", profilePDFMetaFont)
	meta := "Generated " + time.Now().Format("2006-01-02 15:04:05")
	if source := strings.TrimSpace(filePath); source != "" {
		meta += "  ·  Source: " + source
	}
	pdf.MultiCell(0, 5, meta, "", "L", false)
	pdf.Ln(3)

	if !result.HasData {
		pdf.SetFont("Helvetica", "I", profilePDFBodyFont)
		pdf.MultiCell(0, 6, "No assets found in the selected file.", "", "L", false)
		return outputPDF(pdf)
	}

	if summary := strings.TrimSpace(result.Warning.Summary); summary != "" {
		pdf.SetFont("Helvetica", "I", profilePDFBodyFont)
		pdf.SetTextColor(0xb4, 0x8a, 0x00)
		pdf.MultiCell(0, 5, summary, "", "L", false)
		pdf.SetTextColor(0, 0, 0)
		pdf.Ln(2)
	}

	r, g, bl := gradeRGB(result.OverallGrade)
	pdf.SetFont("Helvetica", "B", profilePDFOverallFont)
	pdf.SetTextColor(r, g, bl)
	pdf.MultiCell(0, 9, fmt.Sprintf("Overall: %s (%d%%)", result.OverallGrade, result.OverallScore), "", "L", false)
	pdf.SetTextColor(0, 0, 0)
	pdf.SetFont("Helvetica", "", profilePDFMetaFont)
	pdf.MultiCell(0, 5, RangeNote(result.Percentiles), "", "L", false)
	pdf.Ln(2)

	if len(result.Banned) > 0 {
		limit := ""
		if result.AssetType.BannedTextureSizeMB > 0 {
			limit = fmt.Sprintf("%.2f MB ", result.AssetType.BannedTextureSizeMB)
		}
		pdf.SetFont("Helvetica", "B", profilePDFBodyFont)
		pdf.SetTextColor(0xc0, 0x1a, 0x1a)
		pdf.MultiCell(0, 5, fmt.Sprintf("BANNED: %d image(s) over the %sGPU-memory limit — overall grade forced to F", len(result.Banned), limit), "", "L", false)
		pdf.SetTextColor(0, 0, 0)
		pdf.Ln(2)
	}

	writePDFGradeTable(pdf, result, contentWidth)

	writePDFProfileDetailSection(pdf, "Mismatched PBR Maps", MismatchedPBRDetailRows(result.MismatchedPBR))
	writePDFProfileDetailSection(pdf, "Oversized Textures", OversizedDetailRows(result.Oversized))
	writePDFProfileDetailSection(pdf, "Banned Images", BannedDetailRows(result.Banned))
	writePDFProfileDetailSection(pdf, "Duplicates", DuplicateDetailRows(result.DuplicateGroups))

	return outputPDF(pdf)
}

func writePDFGradeTable(pdf *gofpdf.Fpdf, result ProfileResult, contentWidth float64) {
	spatial := spatialColumns(result)
	gradeW := 14.0
	var metricW, valueW, typicalW, maxW float64
	if spatial {
		metricW = contentWidth * 0.34
		valueW = contentWidth * 0.24
		typicalW = (contentWidth - gradeW - metricW - valueW) / 2
		maxW = typicalW
	} else {
		metricW = contentWidth * 0.45
		valueW = contentWidth - gradeW - metricW
	}

	pdf.SetFont("Helvetica", "I", profilePDFMetaFont)
	pdf.SetTextColor(0x71, 0x71, 0x7a)
	pdf.CellFormat(gradeW, 6, "", "B", 0, "C", false, 0, "")
	pdf.CellFormat(metricW, 6, "Metric", "B", 0, "L", false, 0, "")
	pdf.CellFormat(valueW, 6, "value", "B", 0, "L", false, 0, "")
	if spatial {
		pdf.CellFormat(typicalW, 6, "typical", "B", 0, "L", false, 0, "")
		pdf.CellFormat(maxW, 6, "max/cell", "B", 1, "L", false, 0, "")
	} else {
		pdf.Ln(-1)
	}
	pdf.SetTextColor(0, 0, 0)

	for _, grade := range result.Grades {
		r, g, b := gradeRGB(grade.Grade)
		pdf.SetFont("Helvetica", "B", profilePDFBodyFont+1)
		pdf.SetTextColor(r, g, b)
		pdf.CellFormat(gradeW, 7, grade.Grade, "B", 0, "C", false, 0, "")
		pdf.SetTextColor(0, 0, 0)
		pdf.SetFont("Helvetica", "B", profilePDFBodyFont)
		pdf.CellFormat(metricW, 7, grade.Label, "B", 0, "L", false, 0, "")
		pdf.SetFont("Helvetica", "", profilePDFBodyFont)
		pdf.CellFormat(valueW, 7, grade.Value, "B", 0, "L", false, 0, "")
		if spatial {
			pdf.CellFormat(typicalW, 7, grade.AvgCellValue, "B", 0, "L", false, 0, "")
			pdf.CellFormat(maxW, 7, grade.MaxCellValue, "B", 1, "L", false, 0, "")
		} else {
			pdf.Ln(-1)
		}
	}
	pdf.Ln(5)
}

func writePDFProfileDetailSection(pdf *gofpdf.Fpdf, title string, rows []ProfileDetailRow) {
	if len(rows) == 0 {
		return
	}
	pdf.SetFont("Helvetica", "B", profilePDFSectionFont)
	pdf.MultiCell(0, 6, fmt.Sprintf("%s (%d)", title, len(rows)), "B", "L", false)
	pdf.Ln(1)
	for _, row := range rows {
		pdf.SetFont("Helvetica", "B", profilePDFBodyFont)
		pdf.MultiCell(0, 5, row.Header, "", "L", false)
		pdf.SetFont("Helvetica", "", profilePDFBodyFont-0.5)
		pdf.SetTextColor(0x55, 0x55, 0x5a)
		for _, line := range row.Lines {
			pdf.MultiCell(0, 4.5, "    "+line, "", "L", false)
		}
		pdf.SetTextColor(0, 0, 0)
	}
	pdf.Ln(4)
}

func outputPDF(pdf *gofpdf.Fpdf) ([]byte, error) {
	if pdf.Err() {
		return nil, pdf.Error()
	}
	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// DefaultReportExportBaseName returns the timestamped base file name (without
// extension) for a report export of filePath / assetTypeLabel.
func DefaultReportExportBaseName(filePath string, assetTypeLabel string) string {
	name := defaultReportExportFileName(filePath, assetTypeLabel)
	return strings.TrimSuffix(name, filepath.Ext(name))
}
