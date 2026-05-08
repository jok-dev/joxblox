package scan

import (
	"joxblox/internal/app/loader"
	"joxblox/internal/extractor"
)

// PreviewLookup returns a cached preview for the given asset reference
// key, or nil when no preview is cached.
type PreviewLookup func(referenceKey string) *loader.AssetPreviewResult

// BuildResultsForRBXL extracts scan hits from the given .rbxl/.rbxm and
// assembles `[]ScanResult` rows by consulting `lookup` for already-loaded
// previews. Hits with a cached preview reuse that data (no fetch); hits
// without one yield a base row stamped as a load failure so the table
// still shows them. Pass an empty `pathPrefixes` slice to scan the whole
// file. The Rust extraction is the only I/O the caller pays for.
func BuildResultsForRBXL(filePath string, pathPrefixes []string, lookup PreviewLookup, stopChannel <-chan struct{}) ([]loader.ScanResult, error) {
	var hits []loader.ScanHit
	var err error
	if len(pathPrefixes) > 0 {
		hits, err = scanRBXLFileForAssetIDsFiltered(filePath, pathPrefixes, 0, stopChannel)
	} else {
		hits, err = scanRBXLFileForAssetIDs(filePath, 0, stopChannel)
	}
	if err != nil {
		return nil, err
	}
	results := make([]loader.ScanResult, 0, len(hits))
	for _, hit := range hits {
		var preview *loader.AssetPreviewResult
		if lookup != nil {
			preview = lookup(extractor.AssetReferenceKey(hit.AssetID, hit.AssetInput))
		}
		if preview != nil {
			results = append(results, loader.ApplyPreviewToScanResult(loader.BuildBaseScanResultFromHit(hit), preview))
			continue
		}
		results = append(results, loader.BuildFailedScanResultFromHit(hit, errPreviewNotCached))
	}
	return results, nil
}

type previewNotCachedError struct{}

func (previewNotCachedError) Error() string { return "preview not cached from report" }

var errPreviewNotCached previewNotCachedError
