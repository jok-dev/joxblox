package webapp

import (
	"context"

	"joxblox/internal/app/loader"
	"joxblox/internal/extractor"

	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// resolveScanResults resolves each scan hit's asset stats concurrently, keyed
// by the asset-reference key so duplicate references collapse to a single
// load. When progressEvent is non-empty it emits throttled ScanProgress events
// under that event name so the webview can render a progress bar. Shared by the
// Asset Breakdown scan and the Tag Report so both use the identical resolve
// path (and therefore the same preview/size/triangle behavior).
func resolveScanResults(ctx context.Context, hits []loader.ScanHit, progressEvent string) map[string]loader.ScanResult {
	return loader.RunResolveWorkers(
		hits,
		func(hit loader.ScanHit) string {
			return extractor.AssetReferenceKey(hit.AssetID, hit.AssetInput)
		},
		func(hit loader.ScanHit) loader.ScanResult {
			result, err := loader.LoadScanResult(hit)
			if err != nil {
				return loader.BuildFailedScanResultFromHit(hit, err)
			}
			return result
		},
		2,
		func(done, total int) {
			// Throttle so a many-asset resolve doesn't flood the webview.
			if progressEvent != "" && (done == total || done%5 == 0) {
				wailsRuntime.EventsEmit(ctx, progressEvent, ScanProgress{Done: done, Total: total})
			}
		},
		nil,
	)
}
