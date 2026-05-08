package scan

import (
	"encoding/base64"
	"fmt"
	"html"
	"strconv"
	"strings"
	"time"

	"joxblox/internal/app/loader"
	"joxblox/internal/format"
	"joxblox/internal/roblox"
)

// TagHTMLReportOptions controls how BuildTagHTMLReport renders rows.
type TagHTMLReportOptions struct {
	// Title goes in <title> and the page heading.
	Title string
	// SourcePath is shown in the report header so the reader knows which
	// scan it came from. Optional.
	SourcePath string
	// EmbedImages, when true, base64-embeds image bytes from the loaded
	// preview so the report renders offline. Falls back to the Roblox
	// thumbnail URL when bytes aren't available.
	EmbedImages bool
}

// BuildTagHTMLReport produces a self-contained HTML document grouping the
// supplied results by tag. Each section lists every asset tagged with
// that label as a card showing the asset preview, ID, type, and size.
// Untagged rows are skipped.
//
// The output is meant to be saved to disk and opened in a browser; it
// embeds CSS inline and uses no external scripts so opening the file
// off-network still renders the layout.
func BuildTagHTMLReport(results []loader.ScanResult, store *ScanTagStore, options TagHTMLReportOptions) string {
	if store == nil {
		store = NewScanTagStore()
	}
	resultsByID := map[int64]loader.ScanResult{}
	for _, result := range results {
		if result.AssetID <= 0 {
			continue
		}
		if _, alreadySeen := resultsByID[result.AssetID]; alreadySeen {
			continue
		}
		resultsByID[result.AssetID] = result
	}
	idsByTag := store.AssetIDsByTag()

	title := strings.TrimSpace(options.Title)
	if title == "" {
		title = "Joxblox Scan Tag Report"
	}

	var builder strings.Builder
	builder.WriteString("<!DOCTYPE html>\n<html lang=\"en\">\n<head>\n<meta charset=\"utf-8\">\n")
	builder.WriteString("<title>")
	builder.WriteString(html.EscapeString(title))
	builder.WriteString("</title>\n<style>")
	builder.WriteString(tagReportCSS)
	builder.WriteString("</style>\n</head>\n<body>\n")

	builder.WriteString("<header><h1>")
	builder.WriteString(html.EscapeString(title))
	builder.WriteString("</h1>")
	builder.WriteString("<p class=\"meta\">Generated ")
	builder.WriteString(html.EscapeString(time.Now().Format("2006-01-02 15:04:05")))
	if sourcePath := strings.TrimSpace(options.SourcePath); sourcePath != "" {
		builder.WriteString(" &middot; Source: <code>")
		builder.WriteString(html.EscapeString(sourcePath))
		builder.WriteString("</code>")
	}
	builder.WriteString(fmt.Sprintf(" &middot; %d tagged assets", store.TaggedCount()))
	builder.WriteString("</p></header>\n")

	totalRendered := 0
	for _, tag := range AllScanTags() {
		assetIDs := idsByTag[tag]
		if len(assetIDs) == 0 {
			continue
		}
		builder.WriteString("<section class=\"tag-section\">\n<h2>")
		builder.WriteString(html.EscapeString(string(tag)))
		builder.WriteString(fmt.Sprintf(" <span class=\"count\">%d</span></h2>\n", len(assetIDs)))
		if tag == ScanTagDuplicated {
			totalRendered += writeDuplicatedSection(&builder, store, assetIDs, resultsByID, options.EmbedImages)
		} else {
			builder.WriteString("<div class=\"grid\">\n")
			for _, assetID := range assetIDs {
				row, found := resultsByID[assetID]
				if !found {
					row = loader.ScanResult{AssetID: assetID}
				}
				writeTagReportCard(&builder, row, options.EmbedImages)
				totalRendered++
			}
			builder.WriteString("</div>\n")
		}
		builder.WriteString("</section>\n")
	}

	if totalRendered == 0 {
		builder.WriteString("<section class=\"empty\"><p>No tagged assets yet. Right-click a row in the Scan tab and pick a tag.</p></section>\n")
	}

	builder.WriteString("</body>\n</html>\n")
	return builder.String()
}

// writeDuplicatedSection renders the Duplicated tag section. Assets the
// user manually grouped via the duplicate-picker dialog are rendered as
// "Group N" sub-sections — each member side-by-side under one heading
// so a reader can eyeball whether they really are the same content.
// Anything tagged Duplicated outside a group falls into a trailing
// "Ungrouped" sub-section. Returns the count of cards rendered.
func writeDuplicatedSection(
	builder *strings.Builder,
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
		builder.WriteString(fmt.Sprintf(
			"<div class=\"dup-group\">\n<h3>Group %d <span class=\"count\">%d copies</span></h3>\n<div class=\"grid\">\n",
			groupIndex+1,
			len(group),
		))
		for _, assetID := range group {
			groupedSet[assetID] = struct{}{}
			row, found := resultsByID[assetID]
			if !found {
				row = loader.ScanResult{AssetID: assetID}
			}
			writeTagReportCard(builder, row, embedImages)
			rendered++
		}
		builder.WriteString("</div>\n</div>\n")
	}
	// Anything tagged Duplicated but not in a curated group (legacy
	// togglers, or bookkeeping bugs) still gets shown so it isn't lost.
	ungrouped := make([]int64, 0)
	for _, id := range taggedAssetIDs {
		if _, in := groupedSet[id]; in {
			continue
		}
		ungrouped = append(ungrouped, id)
	}
	if len(ungrouped) > 0 {
		builder.WriteString("<div class=\"dup-group\">\n<h3>Ungrouped <span class=\"count\">")
		builder.WriteString(strconv.Itoa(len(ungrouped)))
		builder.WriteString("</span></h3>\n<div class=\"grid\">\n")
		for _, assetID := range ungrouped {
			row, found := resultsByID[assetID]
			if !found {
				row = loader.ScanResult{AssetID: assetID}
			}
			writeTagReportCard(builder, row, embedImages)
			rendered++
		}
		builder.WriteString("</div>\n</div>\n")
	}
	return rendered
}

func writeTagReportCard(builder *strings.Builder, row loader.ScanResult, embedImages bool) {
	assetIDText := strconv.FormatInt(row.AssetID, 10)
	builder.WriteString("<article class=\"card\">\n")

	imageSource := tagReportImageSource(row, embedImages)
	if imageSource != "" {
		builder.WriteString("<div class=\"thumb\"><img loading=\"lazy\" alt=\"asset ")
		builder.WriteString(html.EscapeString(assetIDText))
		builder.WriteString("\" src=\"")
		builder.WriteString(html.EscapeString(imageSource))
		builder.WriteString("\"></div>\n")
	} else {
		builder.WriteString("<div class=\"thumb placeholder\"><span>")
		builder.WriteString(html.EscapeString(loader.ScanResultTypeLabel(row)))
		builder.WriteString("</span></div>\n")
	}

	builder.WriteString("<div class=\"card-body\">\n")
	builder.WriteString("<div class=\"id\">ID ")
	builder.WriteString(html.EscapeString(assetIDText))
	builder.WriteString("</div>\n")

	typeLabel := strings.TrimSpace(loader.ScanResultTypeLabel(row))
	if typeLabel != "" && typeLabel != "-" {
		builder.WriteString("<div class=\"meta-line\">")
		builder.WriteString(html.EscapeString(typeLabel))
		builder.WriteString("</div>\n")
	}
	if row.Width > 0 && row.Height > 0 {
		builder.WriteString("<div class=\"meta-line\">")
		builder.WriteString(html.EscapeString(format.FormatDimensions(row.Width, row.Height)))
		builder.WriteString("</div>\n")
	}
	// Show the GPU texture footprint rather than the raw on-disk file size:
	// what users care about when triaging duplicates is "how much VRAM does
	// each copy chew", not the compressed file weight.
	if gpuLine := tagReportGPUMemoryLine(row); gpuLine != "" {
		builder.WriteString("<div class=\"meta-line\">")
		builder.WriteString(html.EscapeString(gpuLine))
		builder.WriteString("</div>\n")
	}
	if path := strings.TrimSpace(row.InstancePath); path != "" {
		builder.WriteString("<div class=\"meta-line path\" title=\"")
		builder.WriteString(html.EscapeString(path))
		builder.WriteString("\">")
		builder.WriteString(html.EscapeString(path))
		builder.WriteString("</div>\n")
	}
	builder.WriteString("</div>\n</article>\n")
}

// tagReportGPUMemoryLine returns "GPU 1.33 MB (BC3)" for rows whose
// pixel data is known, or empty for rows we can't bill (failed loads,
// non-textures, etc).
func tagReportGPUMemoryLine(row loader.ScanResult) string {
	formatted := loader.FormatScanResultGPUMemory(row)
	if formatted == "" || formatted == "-" {
		return ""
	}
	return "GPU " + formatted
}

func tagReportImageSource(row loader.ScanResult, embedImages bool) string {
	if embedImages && row.Resource != nil && len(row.Resource.StaticContent) > 0 {
		mimeType := strings.TrimSpace(row.ContentType)
		if mimeType == "" {
			mimeType = "image/png"
		}
		encoded := base64.StdEncoding.EncodeToString(row.Resource.StaticContent)
		return "data:" + mimeType + ";base64," + encoded
	}
	if row.AssetID > 0 && rowLikelyHasThumbnail(row) {
		return fmt.Sprintf("https://www.roblox.com/asset-thumbnail/image?assetId=%d&width=420&height=420&format=png", row.AssetID)
	}
	return ""
}

// rowLikelyHasThumbnail filters out rows that won't usefully render in
// HTML (e.g. failed loads). Mesh / image / model assets all have valid
// Roblox thumbnail endpoints, so we don't block by type — just skip
// rows that explicitly failed.
func rowLikelyHasThumbnail(row loader.ScanResult) bool {
	if row.State == loader.FailedScanRowState {
		return false
	}
	if strings.EqualFold(row.Source, roblox.SourceNoThumbnail) {
		return false
	}
	return true
}

const tagReportCSS = `
:root { color-scheme: dark light; }
* { box-sizing: border-box; }
body {
	margin: 0;
	font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif;
	background: #111418;
	color: #e8eaed;
	padding: 24px;
}
header { margin-bottom: 24px; }
header h1 { margin: 0 0 4px 0; font-size: 22px; }
header .meta { margin: 0; font-size: 13px; color: #9aa0a6; }
header .meta code { background: #1f2329; padding: 2px 6px; border-radius: 3px; font-size: 12px; }
.tag-section { margin-bottom: 32px; }
.tag-section h2 {
	font-size: 18px;
	margin: 0 0 12px 0;
	padding-bottom: 6px;
	border-bottom: 1px solid #2a2f36;
}
.tag-section h2 .count {
	display: inline-block;
	margin-left: 6px;
	padding: 1px 8px;
	border-radius: 10px;
	background: #2a2f36;
	color: #9aa0a6;
	font-size: 12px;
	font-weight: normal;
	vertical-align: middle;
}
.dup-group {
	margin: 0 0 18px 0;
	padding: 12px;
	border: 1px solid #2a2f36;
	border-radius: 6px;
	background: #15181d;
}
.dup-group h3 {
	margin: 0 0 10px 0;
	font-size: 14px;
	color: #c8cdd2;
	font-weight: 600;
}
.dup-group h3 .count {
	display: inline-block;
	margin-left: 6px;
	padding: 1px 8px;
	border-radius: 10px;
	background: #2a2f36;
	color: #9aa0a6;
	font-size: 11px;
	font-weight: normal;
	vertical-align: middle;
}
.grid {
	display: grid;
	grid-template-columns: repeat(auto-fill, minmax(220px, 1fr));
	gap: 12px;
}
.card {
	background: #1a1d22;
	border: 1px solid #2a2f36;
	border-radius: 6px;
	overflow: hidden;
	display: flex;
	flex-direction: column;
}
.thumb {
	width: 100%;
	aspect-ratio: 1 / 1;
	background: #0e1115;
	display: flex;
	align-items: center;
	justify-content: center;
	overflow: hidden;
}
.thumb img { width: 100%; height: 100%; object-fit: contain; display: block; }
.thumb.placeholder span {
	color: #6b7280;
	font-size: 13px;
	text-transform: uppercase;
	letter-spacing: 0.05em;
}
.card-body { padding: 8px 10px; font-size: 12px; line-height: 1.4; }
.card-body .id { font-weight: 600; font-size: 13px; margin-bottom: 4px; }
.card-body .meta-line { color: #9aa0a6; }
.card-body .path { white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.empty { text-align: center; color: #9aa0a6; padding: 40px 0; }
@media (prefers-color-scheme: light) {
	body { background: #fafafa; color: #1f2329; }
	header .meta { color: #5f6368; }
	header .meta code { background: #ececec; }
	.tag-section h2 { border-bottom-color: #d8d8d8; }
	.tag-section h2 .count { background: #ececec; color: #5f6368; }
	.card { background: #fff; border-color: #d8d8d8; }
	.thumb { background: #f0f0f0; }
	.thumb.placeholder span { color: #9aa0a6; }
	.card-body .meta-line { color: #5f6368; }
}
`
