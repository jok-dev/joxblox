package report

import (
	"fmt"
	"strings"

	"joxblox/internal/app/loader"
	"joxblox/internal/format"
)

// ProfileDetailRow is one entry in a drill-down list (Mismatched PBR Maps,
// Oversized Textures, Banned Images, Duplicates): a header line plus optional
// sub-lines, all pre-formatted so every surface — the web view, the HTML
// export, and the PDF export — renders identical text and byte sizes.
type ProfileDetailRow struct {
	Header string   `json:"header"`
	Lines  []string `json:"lines"`
}

// RangeNote describes how the profile was graded (spatial cells vs whole file).
func RangeNote(percentiles CellPercentiles) string {
	if percentiles.WholeFileMode {
		return "Graded over the whole file as one cell"
	}
	if percentiles.CellCount > 0 {
		return fmt.Sprintf("Graded over %d cells (%.0fm x %.0fm)", percentiles.CellCount, percentiles.CellSizeStuds, percentiles.CellSizeStuds)
	}
	return "Graded from raw totals (no spatial data)"
}

// MismatchedPBRDetailRows formats the mismatched-PBR drill-down.
func MismatchedPBRDetailRows(details []MismatchedPBRMaterialDetail) []ProfileDetailRow {
	rows := make([]ProfileDetailRow, 0, len(details))
	for _, detail := range details {
		path := strings.TrimSpace(detail.InstancePath)
		if path == "" {
			path = "(unknown path)"
		}
		var suffixes []string
		if detail.InstanceCount > 1 {
			suffixes = append(suffixes, fmt.Sprintf("×%d instances", detail.InstanceCount))
		}
		if detail.WastedBytes > 0 {
			suffixes = append(suffixes, fmt.Sprintf("save %s", format.FormatSizeAuto64(detail.WastedBytes)))
		}
		if len(suffixes) > 0 {
			path = fmt.Sprintf("%s  (%s)", path, strings.Join(suffixes, " / "))
		}
		rows = append(rows, ProfileDetailRow{Header: path, Lines: []string{formatMismatchedPBRSlots(detail)}})
	}
	return rows
}

func formatMismatchedPBRSlots(detail MismatchedPBRMaterialDetail) string {
	parts := make([]string, 0, 4)
	appendSlot := func(name string, w, h int) {
		if w <= 0 || h <= 0 {
			return
		}
		parts = append(parts, fmt.Sprintf("%s %d×%d", name, w, h))
	}
	appendSlot("Color", detail.ColorWidth, detail.ColorHeight)
	appendSlot("Normal", detail.NormalWidth, detail.NormalHeight)
	appendSlot("Metalness", detail.MetalnessWidth, detail.MetalnessHeight)
	appendSlot("Roughness", detail.RoughnessWidth, detail.RoughnessHeight)
	return strings.Join(parts, "   ")
}

// OversizedDetailRows formats the oversized-texture drill-down.
func OversizedDetailRows(details []ReportExportOversizedTexture) []ProfileDetailRow {
	rows := make([]ProfileDetailRow, 0, len(details))
	for _, d := range details {
		parts := []string{}
		if d.AssetID > 0 {
			parts = append(parts, fmt.Sprintf("rbxassetid://%d", d.AssetID))
		}
		if d.Width > 0 && d.Height > 0 {
			parts = append(parts, fmt.Sprintf("%d×%d", d.Width, d.Height))
		}
		if d.TextureBytes > 0 {
			parts = append(parts, format.FormatSizeAuto64(d.TextureBytes))
		}
		parts = append(parts, fmt.Sprintf("score %s", loader.FormatLargeTextureScore(d.Score)))
		if d.SceneSurfaceArea > 0 {
			parts = append(parts, fmt.Sprintf("over %s", loader.FormatSceneSurfaceArea(d.SceneSurfaceArea)))
		}
		path := strings.TrimSpace(d.InstancePath)
		if path == "" {
			path = "(no instance path)"
		}
		rows = append(rows, ProfileDetailRow{Header: strings.Join(parts, "   "), Lines: []string{path}})
	}
	return rows
}

// BannedDetailRows formats the banned-image drill-down.
func BannedDetailRows(details []BannedTextureDetail) []ProfileDetailRow {
	rows := make([]ProfileDetailRow, 0, len(details))
	for _, d := range details {
		parts := []string{}
		if d.AssetID > 0 {
			parts = append(parts, fmt.Sprintf("rbxassetid://%d", d.AssetID))
		}
		if d.Width > 0 && d.Height > 0 {
			parts = append(parts, fmt.Sprintf("%d×%d", d.Width, d.Height))
		}
		if d.GPUTextureBytes > 0 {
			compression := "BC1"
			if d.IsBC3 {
				compression = "BC3"
			}
			parts = append(parts, fmt.Sprintf("%s GPU (%s)", format.FormatSizeAuto64(d.GPUTextureBytes), compression))
		}
		path := strings.TrimSpace(d.InstancePath)
		if path == "" {
			path = "(no instance path)"
		}
		rows = append(rows, ProfileDetailRow{Header: strings.Join(parts, "   "), Lines: []string{path}})
	}
	return rows
}

// DuplicateDetailRows formats the duplicate-group drill-down.
func DuplicateDetailRows(groups []ReportExportDuplicateGroup) []ProfileDetailRow {
	rows := make([]ProfileDetailRow, 0, len(groups))
	for _, g := range groups {
		copies := len(g.AssetIDs)
		wasted := g.AssetBytes * int64(copies-1)
		header := fmt.Sprintf("%d× copies   %s each   %s wasted", copies, format.FormatSizeAuto64(g.AssetBytes), format.FormatSizeAuto64(wasted))
		lines := make([]string, 0, 2)
		idStrings := make([]string, 0, len(g.AssetIDs))
		seen := map[int64]struct{}{}
		for _, id := range g.AssetIDs {
			if id <= 0 {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			idStrings = append(idStrings, fmt.Sprintf("rbxassetid://%d", id))
		}
		if len(idStrings) > 0 {
			lines = append(lines, strings.Join(idStrings, "  "))
		}
		if path := strings.TrimSpace(g.SampleInstancePath); path != "" {
			lines = append(lines, "sample: "+path)
		}
		rows = append(rows, ProfileDetailRow{Header: header, Lines: lines})
	}
	return rows
}
