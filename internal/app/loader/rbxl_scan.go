package loader

import (
	"errors"
	"os"
	"strings"

	"joxblox/internal/debug"
	"joxblox/internal/extractor"
)

// ScanRBXLFileForAssetIDs extracts every asset reference from an .rbxl/.rbxm
// file and returns deduped scan hits (one per asset+input key) with their use
// counts and scene-surface estimates. This is the Fyne-free scan engine shared
// by the legacy Fyne Scan tab and the web UI's ScanService.
func ScanRBXLFileForAssetIDs(filePath string, limit int, stopChannel <-chan struct{}) ([]ScanHit, error) {
	debug.Logf("RBXL scan started: %s (limit=%d)", filePath, limit)
	if _, readErr := os.Stat(filePath); readErr != nil {
		debug.Logf("RBXL scan read failed: %s", readErr.Error())
		return nil, readErr
	}

	extractResult, rustScanErr := extractor.ExtractAssetIDsWithCounts(filePath, 0, limit, stopChannel)
	if errors.Is(rustScanErr, extractor.ErrCancelled) {
		rustScanErr = ErrScanStopped
	}
	if errors.Is(rustScanErr, ErrScanStopped) {
		debug.Logf("RBXL scan stopped during Rust extraction")
		return nil, ErrScanStopped
	}
	if rustScanErr != nil {
		debug.Logf("RBXL scan Rust extraction failed: %s", rustScanErr.Error())
		return nil, rustScanErr
	}
	sceneSurfaceAreasByPath, surfaceErr := LoadRBXLSceneSurfaceAreas(filePath, nil, stopChannel)
	if errors.Is(surfaceErr, ErrScanStopped) {
		return nil, ErrScanStopped
	}

	hits := BuildScanHitsFromRustReferences(extractResult.References, filePath, sceneSurfaceAreasByPath, limit)
	debug.Logf("RBXL scan completed with %d unique asset IDs", len(hits))
	return hits, nil
}

// ScanRBXLFileForAssetIDsFiltered is ScanRBXLFileForAssetIDs limited to
// references whose instance path matches one of the supplied prefixes.
func ScanRBXLFileForAssetIDsFiltered(filePath string, pathPrefixes []string, limit int, stopChannel <-chan struct{}) ([]ScanHit, error) {
	debug.Logf("RBXL filtered scan started: %s (prefixes=%v, limit=%d)", filePath, pathPrefixes, limit)
	if _, readErr := os.Stat(filePath); readErr != nil {
		return nil, readErr
	}
	refs, err := extractor.ExtractFilteredRefs(filePath, pathPrefixes, stopChannel)
	if errors.Is(err, extractor.ErrCancelled) {
		err = ErrScanStopped
	}
	if errors.Is(err, ErrScanStopped) {
		return nil, ErrScanStopped
	}
	if err != nil {
		return nil, err
	}
	sceneSurfaceAreasByPath, surfaceErr := LoadRBXLSceneSurfaceAreas(filePath, pathPrefixes, stopChannel)
	if errors.Is(surfaceErr, ErrScanStopped) {
		return nil, ErrScanStopped
	}

	hits := BuildScanHitsFromRustReferences(refs, filePath, sceneSurfaceAreasByPath, limit)
	debug.Logf("RBXL filtered scan completed with %d unique asset IDs from %d references", len(hits), len(refs))
	return hits, nil
}

// BuildScanHitsFromRustReferences dedupes raw Rust extractor references into
// ScanHits keyed by asset reference, accumulating use counts and the largest
// scene surface area / path across every instance that references each asset.
func BuildScanHitsFromRustReferences(references []extractor.Result, filePath string, sceneSurfaceAreasByPath map[string]float64, limit int) []ScanHit {
	type hitBuilder struct {
		hit     ScanHit
		pathSet map[string]bool
	}

	builders := map[string]*hitBuilder{}
	order := make([]string, 0, len(references))
	for _, reference := range references {
		if reference.ID <= 0 {
			continue
		}

		assetInput := strings.TrimSpace(reference.RawContent)
		referenceKey := extractor.AssetReferenceKey(reference.ID, assetInput)
		builder, exists := builders[referenceKey]
		if !exists {
			builder = &hitBuilder{
				hit: ScanHit{
					AssetID:          reference.ID,
					AssetInput:       assetInput,
					FilePath:         filePath,
					InstanceType:     strings.TrimSpace(reference.InstanceType),
					InstanceName:     strings.TrimSpace(reference.InstanceName),
					InstancePath:     strings.TrimSpace(reference.InstancePath),
					PropertyName:     strings.TrimSpace(reference.PropertyName),
					SceneSurfaceArea: 0,
				},
				pathSet: map[string]bool{},
			}
			builder.hit.SceneSurfaceArea, builder.hit.LargestSurfacePath = EstimateSceneSurfaceAreaAndPathForPaths(
				reference.InstancePath,
				nil,
				sceneSurfaceAreasByPath,
			)
			builders[referenceKey] = builder
			order = append(order, referenceKey)
		}

		builder.hit.UseCount += rustReferenceUseCount(reference)
		addRustReferencePaths(&builder.hit, builder.pathSet, reference, sceneSurfaceAreasByPath)
	}

	hits := make([]ScanHit, 0, len(order))
	for _, referenceKey := range order {
		hits = append(hits, builders[referenceKey].hit)
		if limit > 0 && len(hits) >= limit {
			break
		}
	}
	return hits
}

func rustReferenceUseCount(reference extractor.Result) int {
	if reference.Used > 0 {
		return reference.Used
	}
	return 1
}

func addRustReferencePaths(hit *ScanHit, pathSet map[string]bool, reference extractor.Result, sceneSurfaceAreasByPath map[string]float64) {
	if hit == nil {
		return
	}

	allPaths := append([]string(nil), reference.AllInstancePaths...)
	if len(allPaths) == 0 {
		allPaths = append(allPaths, reference.InstancePath)
	}
	for _, instancePath := range allPaths {
		trimmedPath := strings.TrimSpace(instancePath)
		if trimmedPath == "" || pathSet[trimmedPath] {
			continue
		}
		pathSet[trimmedPath] = true
		hit.AllInstancePaths = append(hit.AllInstancePaths, trimmedPath)
		nextArea, nextPath := EstimateSceneSurfaceAreaAndPathForPaths(trimmedPath, nil, sceneSurfaceAreasByPath)
		if nextArea > hit.SceneSurfaceArea {
			hit.SceneSurfaceArea = nextArea
			hit.LargestSurfacePath = strings.TrimSpace(nextPath)
		} else if hit.SceneSurfaceArea <= 0 && strings.TrimSpace(hit.LargestSurfacePath) == "" {
			hit.LargestSurfacePath = strings.TrimSpace(nextPath)
		}
	}
}

// LoadRBXLSceneSurfaceAreas extracts per-instance-path scene surface areas from
// the file's render parts, used to weight large-texture scoring. A non-fatal
// extraction error yields an empty map so the scan can still proceed.
func LoadRBXLSceneSurfaceAreas(filePath string, pathPrefixes []string, stopChannel <-chan struct{}) (map[string]float64, error) {
	mapParts, err := extractor.ExtractMapRenderParts(filePath, pathPrefixes, stopChannel)
	if errors.Is(err, extractor.ErrCancelled) {
		err = ErrScanStopped
	}
	if errors.Is(err, ErrScanStopped) {
		return nil, ErrScanStopped
	}
	if err != nil {
		debug.Logf("RBXL map render extraction failed for large textures: %s", err.Error())
		return map[string]float64{}, nil
	}
	return BuildSceneSurfaceAreaIndex(mapParts), nil
}
