package scan

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"time"

	"joxblox/internal/app/loader"
	"joxblox/internal/format"

	"github.com/jung-kurt/gofpdf"
)

// TagPDFReportOptions controls how BuildTagPDFReport renders the layout.
// Embedding image thumbnails balloons the file size; a Studio-class scan
// with hundreds of tagged 1024² PNGs stuffed into a PDF can hit
// hundreds of MB. The toggle lets callers ship a slim text-only PDF
// when bytes matter more than visual confirmation.
type TagPDFReportOptions struct {
	Title       string
	SourcePath  string
	EmbedImages bool
}

// PDF page geometry. A4 portrait gives a ~210×297mm sheet; a 4-card
// row at ~45mm wide leaves comfortable inter-card padding without
// shrinking the thumbnail too far. Cards are ~45×60mm so two rows of
// four fit per page with a header band reserved at the top.
const (
	pdfPageMarginMM    = 12.0
	pdfCardWidthMM     = 45.0
	pdfCardHeightMM    = 62.0
	pdfCardGapMM       = 4.0
	pdfThumbHeightMM   = 36.0
	pdfTagSectionGapMM = 6.0
	pdfBodyFontSize    = 9.0
	pdfMetaFontSize    = 8.0
	pdfHeaderFontSize  = 14.0
	pdfTagFontSize     = 12.0
	pdfGroupFontSize   = 10.0
)

// BuildTagPDFReport produces the same logical document as
// BuildTagHTMLReport but as a PDF byte stream — tag sections, card
// grids, image thumbnails, and the duplicate-group sub-blocks. Returns
// the encoded PDF or an error if gofpdf bails (which it does on a
// rendering bug, not on user-input issues).
func BuildTagPDFReport(results []loader.ScanResult, store *ScanTagStore, options TagPDFReportOptions) ([]byte, error) {
	if store == nil {
		store = NewScanTagStore()
	}
	resultsByID := buildPDFResultsByID(results)
	idsByTag := store.AssetIDsByTag()

	pdf := gofpdf.New("P", "mm", "A4", "")
	pdf.SetMargins(pdfPageMarginMM, pdfPageMarginMM, pdfPageMarginMM)
	pdf.SetAutoPageBreak(true, pdfPageMarginMM)
	pdf.AddPage()

	writePDFHeader(pdf, options, store.TaggedCount())

	totalRendered := 0
	for _, tag := range AllScanTags() {
		assetIDs := idsByTag[tag]
		if len(assetIDs) == 0 {
			continue
		}
		writePDFTagHeading(pdf, string(tag), len(assetIDs))
		if tag == ScanTagDuplicated {
			totalRendered += writePDFDuplicatedSection(pdf, store, assetIDs, resultsByID, options.EmbedImages)
		} else {
			totalRendered += writePDFCardGrid(pdf, assetIDs, resultsByID, options.EmbedImages)
		}
		pdf.Ln(pdfTagSectionGapMM)
	}

	if totalRendered == 0 {
		pdf.SetFont("Helvetica", "I", pdfBodyFontSize)
		pdf.MultiCell(0, 6, "No tagged assets yet. Right-click a row in the Scan tab and pick a tag.", "", "C", false)
	}

	if pdf.Err() {
		return nil, pdf.Error()
	}
	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func buildPDFResultsByID(results []loader.ScanResult) map[int64]loader.ScanResult {
	out := map[int64]loader.ScanResult{}
	for _, result := range results {
		if result.AssetID <= 0 {
			continue
		}
		if _, alreadySeen := out[result.AssetID]; alreadySeen {
			continue
		}
		out[result.AssetID] = result
	}
	return out
}

func writePDFHeader(pdf *gofpdf.Fpdf, options TagPDFReportOptions, taggedCount int) {
	title := strings.TrimSpace(options.Title)
	if title == "" {
		title = "Joxblox Scan Tag Report"
	}
	pdf.SetFont("Helvetica", "B", pdfHeaderFontSize)
	pdf.MultiCell(0, 7, title, "", "L", false)

	pdf.SetFont("Helvetica", "", pdfMetaFontSize)
	metaLine := "Generated " + time.Now().Format("2006-01-02 15:04:05")
	if source := strings.TrimSpace(options.SourcePath); source != "" {
		metaLine += "  ·  Source: " + source
	}
	metaLine += fmt.Sprintf("  ·  %d tagged assets", taggedCount)
	pdf.MultiCell(0, 5, metaLine, "", "L", false)
	pdf.Ln(2)
}

func writePDFTagHeading(pdf *gofpdf.Fpdf, tagName string, count int) {
	pdf.SetFont("Helvetica", "B", pdfTagFontSize)
	pdf.MultiCell(0, 6, fmt.Sprintf("%s  (%d)", tagName, count), "B", "L", false)
	pdf.Ln(1)
}

// writePDFDuplicatedSection mirrors writeDuplicatedSection from the HTML
// builder — curated groups render as their own sub-sections with a
// "Group N — N copies" heading; anything tagged Duplicated outside a
// curated group falls into a trailing "Ungrouped" sub-section.
func writePDFDuplicatedSection(
	pdf *gofpdf.Fpdf,
	store *ScanTagStore,
	taggedAssetIDs []int64,
	resultsByID map[int64]loader.ScanResult,
	embedImages bool,
) int {
	groups := store.DuplicateGroups()
	rendered := 0
	groupedSet := map[int64]struct{}{}
	for groupIndex, group := range groups {
		if len(group) < 2 {
			continue
		}
		pdf.SetFont("Helvetica", "B", pdfGroupFontSize)
		pdf.MultiCell(0, 5, fmt.Sprintf("Group %d  (%d copies)", groupIndex+1, len(group)), "", "L", false)
		pdf.Ln(1)
		for _, assetID := range group {
			groupedSet[assetID] = struct{}{}
		}
		rendered += writePDFCardGrid(pdf, group, resultsByID, embedImages)
		pdf.Ln(2)
	}
	ungrouped := make([]int64, 0)
	for _, id := range taggedAssetIDs {
		if _, in := groupedSet[id]; in {
			continue
		}
		ungrouped = append(ungrouped, id)
	}
	if len(ungrouped) > 0 {
		pdf.SetFont("Helvetica", "B", pdfGroupFontSize)
		pdf.MultiCell(0, 5, fmt.Sprintf("Ungrouped  (%d)", len(ungrouped)), "", "L", false)
		pdf.Ln(1)
		rendered += writePDFCardGrid(pdf, ungrouped, resultsByID, embedImages)
	}
	return rendered
}

func writePDFCardGrid(pdf *gofpdf.Fpdf, assetIDs []int64, resultsByID map[int64]loader.ScanResult, embedImages bool) int {
	if len(assetIDs) == 0 {
		return 0
	}
	pageWidth, _ := pdf.GetPageSize()
	leftMargin, _, rightMargin, _ := pdf.GetMargins()
	usableWidth := pageWidth - leftMargin - rightMargin
	cardsPerRow := int((usableWidth + pdfCardGapMM) / (pdfCardWidthMM + pdfCardGapMM))
	if cardsPerRow < 1 {
		cardsPerRow = 1
	}

	rendered := 0
	column := 0
	rowStartY := pdf.GetY()
	for _, assetID := range assetIDs {
		row, found := resultsByID[assetID]
		if !found {
			row = loader.ScanResult{AssetID: assetID}
		}
		if column == 0 {
			rowStartY = pdf.GetY()
			// AddPage if a full card row won't fit; gofpdf's
			// AutoPageBreak handles overflow during a single cell, but
			// we want every card in a row to land on the same page so
			// the grid reads cleanly.
			_, pageHeight := pdf.GetPageSize()
			_, _, _, bottomMargin := pdf.GetMargins()
			if rowStartY+pdfCardHeightMM > pageHeight-bottomMargin {
				pdf.AddPage()
				rowStartY = pdf.GetY()
			}
		}
		x := leftMargin + float64(column)*(pdfCardWidthMM+pdfCardGapMM)
		writePDFCard(pdf, row, x, rowStartY, embedImages)
		rendered++
		column++
		if column >= cardsPerRow {
			column = 0
			pdf.SetY(rowStartY + pdfCardHeightMM + pdfCardGapMM)
			pdf.SetX(leftMargin)
		}
	}
	if column != 0 {
		// Partial trailing row — bump cursor below it so the next
		// section doesn't draw on top.
		pdf.SetY(rowStartY + pdfCardHeightMM + pdfCardGapMM)
		pdf.SetX(leftMargin)
	}
	return rendered
}

func writePDFCard(pdf *gofpdf.Fpdf, row loader.ScanResult, x, y float64, embedImages bool) {
	pdf.SetDrawColor(180, 180, 180)
	pdf.Rect(x, y, pdfCardWidthMM, pdfCardHeightMM, "D")

	innerPadding := 1.5
	thumbX := x + innerPadding
	thumbY := y + innerPadding
	thumbWidth := pdfCardWidthMM - 2*innerPadding

	thumbnailDrawn := false
	if embedImages {
		thumbnailDrawn = drawPDFThumbnail(pdf, row, thumbX, thumbY, thumbWidth, pdfThumbHeightMM)
	}
	if !thumbnailDrawn {
		// Placeholder block matching the HTML report's "no preview"
		// state — keeps the grid alignment consistent.
		pdf.SetFillColor(240, 240, 240)
		pdf.Rect(thumbX, thumbY, thumbWidth, pdfThumbHeightMM, "F")
		pdf.SetTextColor(120, 120, 120)
		pdf.SetFont("Helvetica", "I", pdfMetaFontSize)
		label := strings.TrimSpace(loader.ScanResultTypeLabel(row))
		if label == "" || label == "-" {
			label = "(no preview)"
		}
		pdf.SetXY(thumbX, thumbY+pdfThumbHeightMM/2-2)
		pdf.CellFormat(thumbWidth, 4, label, "", 0, "C", false, 0, "")
		pdf.SetTextColor(0, 0, 0)
	}

	textY := thumbY + pdfThumbHeightMM + 1.5
	textX := thumbX
	textWidth := thumbWidth

	pdf.SetFont("Helvetica", "B", pdfBodyFontSize)
	pdf.SetXY(textX, textY)
	pdf.CellFormat(textWidth, 4, "ID "+strconv.FormatInt(row.AssetID, 10), "", 0, "L", false, 0, "")

	pdf.SetFont("Helvetica", "", pdfMetaFontSize)
	cursorY := textY + 4
	for _, line := range collectPDFCardMetaLines(row) {
		if cursorY+3.2 > y+pdfCardHeightMM-innerPadding {
			break
		}
		pdf.SetXY(textX, cursorY)
		pdf.CellFormat(textWidth, 3.2, truncateForPDFCell(pdf, line, textWidth), "", 0, "L", false, 0, "")
		cursorY += 3.4
	}
}

// drawPDFThumbnail registers the row's preview bytes with gofpdf's
// in-memory image cache and draws them centred in the thumbnail box.
// Returns false when the row has no embeddable preview (gofpdf only
// supports PNG / JPEG / GIF; everything else falls through to the
// placeholder).
func drawPDFThumbnail(pdf *gofpdf.Fpdf, row loader.ScanResult, x, y, width, height float64) bool {
	if row.Resource == nil || len(row.Resource.StaticContent) == 0 {
		return false
	}
	imageType := pdfImageTypeFor(row.ContentType, row.Resource.StaticContent)
	if imageType == "" {
		return false
	}
	imageName := fmt.Sprintf("asset-%d", row.AssetID)
	if pdf.GetImageInfo(imageName) == nil {
		options := gofpdf.ImageOptions{ImageType: imageType, ReadDpi: false}
		pdf.RegisterImageOptionsReader(imageName, options, bytes.NewReader(row.Resource.StaticContent))
		if pdf.Err() {
			// Drop the error on the floor — drawing the placeholder
			// is preferable to abandoning the whole report. Clear so
			// the next image attempt isn't poisoned.
			pdf.ClearError()
			return false
		}
	}
	pdf.ImageOptions(imageName, x, y, width, height, false, gofpdf.ImageOptions{ImageType: imageType}, 0, "")
	if pdf.Err() {
		pdf.ClearError()
		return false
	}
	return true
}

// pdfImageTypeFor picks the type label gofpdf wants. Honors the
// scanner-reported MIME type first; falls back to magic-byte sniffing
// because some scan rows have empty or generic ContentType.
func pdfImageTypeFor(mimeType string, content []byte) string {
	switch strings.ToLower(strings.TrimSpace(mimeType)) {
	case "image/png":
		return "PNG"
	case "image/jpeg", "image/jpg":
		return "JPG"
	case "image/gif":
		return "GIF"
	}
	if len(content) >= 8 {
		switch {
		case bytes.HasPrefix(content, []byte{0x89, 0x50, 0x4E, 0x47}):
			return "PNG"
		case bytes.HasPrefix(content, []byte{0xFF, 0xD8, 0xFF}):
			return "JPG"
		case bytes.HasPrefix(content, []byte{0x47, 0x49, 0x46, 0x38}):
			return "GIF"
		}
	}
	return ""
}

func collectPDFCardMetaLines(row loader.ScanResult) []string {
	lines := make([]string, 0, 4)
	if typeLabel := strings.TrimSpace(loader.ScanResultTypeLabel(row)); typeLabel != "" && typeLabel != "-" {
		lines = append(lines, typeLabel)
	}
	if row.Width > 0 && row.Height > 0 {
		lines = append(lines, format.FormatDimensions(row.Width, row.Height))
	}
	if gpuLine := tagReportGPUMemoryLine(row); gpuLine != "" {
		lines = append(lines, gpuLine)
	}
	if path := strings.TrimSpace(row.InstancePath); path != "" {
		lines = append(lines, path)
	}
	return lines
}

// truncateForPDFCell shortens a string with an ellipsis so it fits
// within the supplied width at the current font. gofpdf has no native
// "ellipsize on overflow" — long instance paths would otherwise spill
// across the card boundary.
func truncateForPDFCell(pdf *gofpdf.Fpdf, text string, maxWidth float64) string {
	if pdf.GetStringWidth(text) <= maxWidth {
		return text
	}
	const ellipsis = "…"
	for length := len(text) - 1; length > 0; length-- {
		candidate := text[:length] + ellipsis
		if pdf.GetStringWidth(candidate) <= maxWidth {
			return candidate
		}
	}
	return ellipsis
}
