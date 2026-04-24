// Command rdc-texture-report prints a summary of a RenderDoc zip.xml capture.
// Useful for sanity-checking the internal/renderdoc parser against a real
// capture without launching the GUI.
package main

import (
	"fmt"
	"os"
	"sort"

	"joxblox/internal/renderdoc"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: rdc-texture-report <capture.zip.xml>")
		os.Exit(1)
	}
	report, err := renderdoc.ParseCaptureXMLFile(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if store, storeErr := renderdoc.OpenBufferStore(os.Args[1]); storeErr == nil {
		renderdoc.ComputeTextureHashes(report, store, nil)
		renderdoc.ApplyBuiltinHashes(report, renderdoc.DefaultRobloxBuiltinHashes)
		_ = store.Close()
	}
	fmt.Printf("GPU:    %s\n", report.GPUAdapter)
	fmt.Printf("Total:  %d textures, %.2f MB\n\n", len(report.Textures), float64(report.TotalBytes)/1024/1024)

	type catRow struct {
		name  renderdoc.TextureCategory
		agg   renderdoc.CategoryAggregate
	}
	var rows []catRow
	for name, agg := range report.ByCategory {
		rows = append(rows, catRow{name, agg})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].agg.Bytes > rows[j].agg.Bytes })
	fmt.Println("By category:")
	for _, row := range rows {
		fmt.Printf("  %-30s %4d textures  %8.2f MB\n", row.name, row.agg.Count, float64(row.agg.Bytes)/1024/1024)
	}
	fmt.Println()

	fmt.Println("Top 20 by VRAM:")
	sort.Slice(report.Textures, func(i, j int) bool { return report.Textures[i].Bytes > report.Textures[j].Bytes })
	limit := 20
	if limit > len(report.Textures) {
		limit = len(report.Textures)
	}
	for _, tex := range report.Textures[:limit] {
		hash := tex.PixelHash
		if hash == "" {
			hash = "-"
		}
		fmt.Printf("  %6s  %dx%d  mips=%d  %-38s  %8.2f MB  %-26s hash=%s  (uploads: %d)\n",
			tex.ResourceID, tex.Width, tex.Height, tex.MipLevels, tex.ShortFormat,
			float64(tex.Bytes)/1024/1024, tex.Category, hash, len(tex.Uploads),
		)
	}
}
