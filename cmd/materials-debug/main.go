// Debug helper for reconciling the Materials sub-tab's "Engine GPU
// memory" headline against the Report tab's "GPU Texture Memory".
// Loads an rbxl, runs the same positioned-ref extraction the report
// uses, and prints how many SurfaceAppearance refs hit each instance
// path, how many distinct paths each unique (asset, propertyName) is
// referenced from, and how many SA bundles the materials map should
// reconstruct vs the deduped-row materials map. Doesn't load asset
// previews — diagnostic only, no Roblox network calls.
package main

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"joxblox/internal/extractor"
)

func main() {
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: materials-debug <rbxl path>")
		os.Exit(2)
	}
	path := flag.Arg(0)

	prefixes := []string{"Workspace.", "MaterialService."}

	// Use the same scan-flow extraction the asset scan tab uses, so we
	// see exactly the AllInstancePaths the materials view will receive.
	scanRefs, scanErr := extractor.ExtractFilteredRefs(path, prefixes, nil)
	if scanErr != nil {
		fmt.Fprintf(os.Stderr, "scan extract failed: %v\n", scanErr)
		os.Exit(1)
	}
	scanResultCount := len(scanRefs)
	saScanRefs := 0
	totalAllPaths := 0
	maxAllPaths := 0
	multiPathScanRefs := 0
	for _, r := range scanRefs {
		propLower := strings.ToLower(strings.TrimSpace(r.PropertyName))
		if !isSAProperty(propLower, r.InstanceType) {
			continue
		}
		saScanRefs++
		n := len(r.AllInstancePaths)
		totalAllPaths += n
		if n > 1 {
			multiPathScanRefs++
		}
		if n > maxAllPaths {
			maxAllPaths = n
		}
	}
	fmt.Printf("Scan-flow refs: %d total, %d SA-related; %d SA refs carry >1 AllInstancePaths (max %d, total %d)\n",
		scanResultCount, saScanRefs, multiPathScanRefs, maxAllPaths, totalAllPaths)
	fmt.Printf("Note: a SA ref with empty AllInstancePaths means the rust extractor only emitted a single InstancePath for it.\n\n")

	// Replicate buildScanHitsFromRustReferences's dedup-by-(assetID,assetInput)
	// to verify hits end up with multi-path AllInstancePaths.
	type hitBuilder struct {
		assetID    int64
		assetInput string
		property   string
		isSA       bool
		paths      map[string]struct{}
	}
	hitBuilders := map[string]*hitBuilder{}
	for _, r := range scanRefs {
		if r.ID <= 0 {
			continue
		}
		key := extractor.AssetReferenceKey(r.ID, r.RawContent)
		hb, ok := hitBuilders[key]
		if !ok {
			propLower := strings.ToLower(strings.TrimSpace(r.PropertyName))
			hb = &hitBuilder{
				assetID:    r.ID,
				assetInput: r.RawContent,
				property:   propLower,
				isSA:       isSAProperty(propLower, r.InstanceType),
				paths:      map[string]struct{}{},
			}
			hitBuilders[key] = hb
		}
		// Mirror addRustReferencePaths: prefer reference.AllInstancePaths,
		// fall back to InstancePath.
		paths := r.AllInstancePaths
		if len(paths) == 0 {
			paths = []string{r.InstancePath}
		}
		for _, p := range paths {
			trimmed := strings.TrimSpace(p)
			if trimmed == "" {
				continue
			}
			hb.paths[trimmed] = struct{}{}
		}
	}
	saHits, multiPathSAHits, totalSAHitPaths := 0, 0, 0
	for _, hb := range hitBuilders {
		if !hb.isSA {
			continue
		}
		saHits++
		totalSAHitPaths += len(hb.paths)
		if len(hb.paths) > 1 {
			multiPathSAHits++
		}
	}
	fmt.Printf("After Go dedup: %d hits total, %d SA hits, %d SA hits carry >1 AllInstancePaths, total paths %d\n",
		len(hitBuilders), saHits, multiPathSAHits, totalSAHitPaths)
	fmt.Println()

	refs, err := extractor.ExtractPositionedRefs(path, prefixes, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "positioned extract failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Loaded %d positioned refs from %s\n", len(refs), path)

	type assetKey struct {
		assetID    int64
		assetInput string
		property   string
	}
	pathsByAsset := map[assetKey]map[string]struct{}{}
	saByPath := map[string][]assetKey{}
	saRefCount := 0
	for _, ref := range refs {
		if ref.ID <= 0 {
			continue
		}
		propLower := strings.ToLower(strings.TrimSpace(ref.PropertyName))
		if !isSAProperty(propLower, ref.InstanceType) {
			continue
		}
		saRefCount++
		key := assetKey{assetID: ref.ID, assetInput: ref.RawContent, property: propLower}
		path := strings.TrimSpace(ref.InstancePath)
		if pathsByAsset[key] == nil {
			pathsByAsset[key] = map[string]struct{}{}
		}
		pathsByAsset[key][path] = struct{}{}
		saByPath[path] = append(saByPath[path], key)
	}
	fmt.Printf("SurfaceAppearance refs: %d\n", saRefCount)
	fmt.Printf("Unique (asset, property) pairs: %d\n", len(pathsByAsset))
	fmt.Printf("Distinct SurfaceAppearance instance paths: %d\n", len(saByPath))

	multiPathAssets := 0
	totalExtraPaths := 0
	for _, paths := range pathsByAsset {
		if len(paths) > 1 {
			multiPathAssets++
			totalExtraPaths += len(paths) - 1
		}
	}
	fmt.Printf("Assets referenced from >1 SA instance path: %d (carries %d 'extra' paths beyond the primary)\n",
		multiPathAssets, totalExtraPaths)

	type colorEntry struct {
		assetID int64
		paths   int
	}
	var colors []colorEntry
	for key, paths := range pathsByAsset {
		if !strings.Contains(key.property, "color") {
			continue
		}
		colors = append(colors, colorEntry{assetID: key.assetID, paths: len(paths)})
	}
	sort.Slice(colors, func(i, j int) bool { return colors[i].paths > colors[j].paths })
	fmt.Println("\nTop 10 Color assets by SA path count (these are the bundles the deduped scan loses):")
	for i, c := range colors {
		if i >= 10 {
			break
		}
		fmt.Printf("  asset %d → %d paths\n", c.assetID, c.paths)
	}
}

func isSAProperty(propertyLower, instanceType string) bool {
	if strings.EqualFold(strings.TrimSpace(instanceType), "surfaceappearance") {
		return true
	}
	return strings.Contains(propertyLower, "mapcontent")
}
