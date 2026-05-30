package webapp

import (
	"context"
	"fmt"
	"strings"

	"joxblox/internal/app/loader"
	"joxblox/internal/extractor"
	"joxblox/internal/format"

	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// ScanRow is one serializable result row for the web Scan table. It mirrors the
// columns the Fyne ScanResultsExplorer rendered, with sizes pre-formatted.
type ScanRow struct {
	AssetID       int64  `json:"assetId"`
	AssetInput    string `json:"assetInput"`
	UseCount      int    `json:"useCount"`
	InstanceType  string `json:"instanceType"`
	InstancePath  string `json:"instancePath"`
	PropertyName  string `json:"propertyName"`
	State         string `json:"state"`
	Source        string `json:"source"`
	AssetTypeName string `json:"assetTypeName"`
	AssetTypeID   int    `json:"assetTypeId"`

	Width             int    `json:"width"`
	Height            int    `json:"height"`
	DimensionsDisplay string `json:"dimensionsDisplay"`
	Format            string `json:"format"`

	BytesSize        int    `json:"bytesSize"`
	TotalBytesSize   int    `json:"totalBytesSize"`
	SizeDisplay      string `json:"sizeDisplay"`
	GPUMemoryBytes   int64  `json:"gpuMemoryBytes"`
	GPUMemoryDisplay string `json:"gpuMemoryDisplay"`
	GPUFormatLabel   string `json:"gpuFormatLabel"`

	MeshTriangles uint32 `json:"meshTriangles"`

	Warning      bool   `json:"warning"`
	WarningCause string `json:"warningCause"`
	IsLarge      bool   `json:"isLarge"`
}

// ScanSummary is the dashboard-card data shown above the results table.
type ScanSummary struct {
	TotalAssets       int    `json:"totalAssets"`
	FailedCount       int    `json:"failedCount"`
	TotalBytes        int    `json:"totalBytes"`
	TotalBytesDisplay string `json:"totalBytesDisplay"`
	TotalGPUBytes     int64  `json:"totalGpuBytes"`
	TotalGPUDisplay   string `json:"totalGpuDisplay"`
	TotalTriangles    int64  `json:"totalTriangles"`
	LargeTextureCount int    `json:"largeTextureCount"`
}

// ScanReport is the full result of a scan.
type ScanReport struct {
	FilePath string      `json:"filePath"`
	Rows     []ScanRow   `json:"rows"`
	Summary  ScanSummary `json:"summary"`
}

// ScanOptions are the knobs the web Scan view exposes.
type ScanOptions struct {
	// PathFilter is an optional newline- or comma-separated list of instance
	// path prefixes; non-empty switches to the filtered extractor path.
	PathFilter string `json:"pathFilter"`
	// LargeTextureThresholdMB flags rows whose estimated GPU footprint exceeds
	// this many megabytes. Zero disables the flag (no rows counted as large).
	LargeTextureThresholdMB float64 `json:"largeTextureThresholdMb"`
}

// ScanProgress is emitted on the "scan:progress" event during resolution.
type ScanProgress struct {
	Done  int `json:"done"`
	Total int `json:"total"`
}

// DataModelService is one top-level service of a place file, shown as a
// checkbox in the Asset Breakdown "Datamodel" selector.
type DataModelService struct {
	Name          string `json:"name"`
	ClassName     string `json:"className"`
	InstanceCount int64  `json:"instanceCount"`
}

// CachedScanReport is the result of a cache lookup: Found reports whether a
// previously-processed report exists for the file (unchanged) and options.
type CachedScanReport struct {
	Found  bool       `json:"found"`
	Report ScanReport `json:"report"`
}

// ScanService runs .rbxl/.rbxm asset scans for the web UI, reusing the shared
// Fyne-free scan engine in the loader package.
type ScanService struct {
	ctx context.Context
}

func NewScanService() *ScanService { return &ScanService{} }

func (s *ScanService) startup(ctx context.Context) { s.ctx = ctx }

// PickRBXLFile opens a native file picker for a place/model file and returns
// the chosen path ("" if the user cancelled).
func (s *ScanService) PickRBXLFile() (string, error) {
	return wailsRuntime.OpenFileDialog(s.ctx, wailsRuntime.OpenDialogOptions{
		Title: "Select a Roblox place or model file",
		Filters: []wailsRuntime.FileFilter{
			{DisplayName: "Roblox place/model files", Pattern: "*.rbxl;*.rbxm"},
		},
	})
}

// GetDataModelTree returns the place file's top-level services so the web UI
// can offer a "which parts of the DataModel to analyze" selector. Only
// meaningful for .rbxl place files; callers gate on the extension.
func (s *ScanService) GetDataModelTree(path string) ([]DataModelService, error) {
	trimmedPath := strings.TrimSpace(path)
	if trimmedPath == "" {
		return []DataModelService{}, nil
	}
	nodes, err := extractor.ExtractDataModelTree(trimmedPath, nil)
	if err != nil {
		return nil, err
	}
	services := make([]DataModelService, 0, len(nodes))
	for _, node := range nodes {
		services = append(services, DataModelService{
			Name:          node.Name,
			ClassName:     node.ClassName,
			InstanceCount: node.InstanceCount,
		})
	}
	return services, nil
}

// GetCachedScan returns the previously-processed report for this file and
// options if one exists and the file is unchanged. Found is false when the
// file has never been processed (with these options) or has changed since,
// in which case the UI shows the "Process" button.
func (s *ScanService) GetCachedScan(path string, options ScanOptions) (CachedScanReport, error) {
	trimmedPath := strings.TrimSpace(path)
	if trimmedPath == "" {
		return CachedScanReport{}, nil
	}
	report, found, err := readScanCache(trimmedPath, options)
	if err != nil {
		// A cache read error is non-fatal — fall back to "not processed".
		return CachedScanReport{}, nil
	}
	return CachedScanReport{Found: found, Report: report}, nil
}

// ScanFile extracts asset references from the file and resolves each asset's
// stats concurrently, emitting "scan:progress" events as it goes. The returned
// report carries the per-asset rows plus a summary for the dashboard cards.
func (s *ScanService) ScanFile(path string, options ScanOptions) (ScanReport, error) {
	trimmedPath := strings.TrimSpace(path)
	if trimmedPath == "" {
		return ScanReport{}, fmt.Errorf("Select an .rbxl or .rbxm file first.")
	}

	prefixes := parsePathPrefixes(options.PathFilter)
	var hits []loader.ScanHit
	var hitErr error
	if len(prefixes) > 0 {
		hits, hitErr = loader.ScanRBXLFileForAssetIDsFiltered(trimmedPath, prefixes, 0, nil)
	} else {
		hits, hitErr = loader.ScanRBXLFileForAssetIDs(trimmedPath, 0, nil)
	}
	if hitErr != nil {
		return ScanReport{}, hitErr
	}
	if len(hits) == 0 {
		return ScanReport{FilePath: trimmedPath, Rows: []ScanRow{}}, nil
	}

	resolved := resolveScanResults(s.ctx, hits, "scan:progress")

	thresholdBytes := format.MegabytesToBytes(options.LargeTextureThresholdMB)
	rows := make([]ScanRow, 0, len(hits))
	summary := ScanSummary{TotalAssets: len(hits)}
	for _, hit := range hits {
		result, ok := resolved[extractor.AssetReferenceKey(hit.AssetID, hit.AssetInput)]
		if !ok {
			continue
		}
		row := buildScanRow(result, thresholdBytes)
		rows = append(rows, row)

		if result.State == loader.FailedScanRowState {
			summary.FailedCount++
		}
		summary.TotalBytes += result.BytesSize
		summary.TotalGPUBytes += row.GPUMemoryBytes
		if result.MeshNumFaces > 0 {
			summary.TotalTriangles += int64(result.MeshNumFaces) * int64(max(result.UseCount, 1))
		}
		if row.IsLarge {
			summary.LargeTextureCount++
		}
	}
	summary.TotalBytesDisplay = format.FormatSizeAuto(summary.TotalBytes)
	summary.TotalGPUDisplay = format.FormatSizeAuto64(summary.TotalGPUBytes)

	report := ScanReport{FilePath: trimmedPath, Rows: rows, Summary: summary}
	writeScanCache(trimmedPath, options, report)
	return report, nil
}

func buildScanRow(result loader.ScanResult, thresholdBytes int64) ScanRow {
	gpuBytes := loader.ScanResultGPUMemoryBytes(result)
	dimensions := ""
	if result.Width > 0 && result.Height > 0 {
		dimensions = format.FormatDimensions(result.Width, result.Height)
	}
	sizeBytes := result.BytesSize
	if result.TotalBytesSize > sizeBytes {
		sizeBytes = result.TotalBytesSize
	}
	isLarge := thresholdBytes > 0 && gpuBytes > thresholdBytes
	return ScanRow{
		AssetID:           result.AssetID,
		AssetInput:        result.AssetInput,
		UseCount:          result.UseCount,
		InstanceType:      result.InstanceType,
		InstancePath:      result.InstancePath,
		PropertyName:      result.PropertyName,
		State:             result.State,
		Source:            result.Source,
		AssetTypeName:     result.AssetTypeName,
		AssetTypeID:       result.AssetTypeID,
		Width:             result.Width,
		Height:            result.Height,
		DimensionsDisplay: dimensions,
		Format:            result.Format,
		BytesSize:         result.BytesSize,
		TotalBytesSize:    result.TotalBytesSize,
		SizeDisplay:       format.FormatSizeAuto(sizeBytes),
		GPUMemoryBytes:    gpuBytes,
		GPUMemoryDisplay:  format.FormatSizeAuto64(gpuBytes),
		GPUFormatLabel:    loader.ScanResultGPUMemoryFormatLabel(result),
		MeshTriangles:     result.MeshNumFaces,
		Warning:           result.Warning,
		WarningCause:      result.WarningCause,
		IsLarge:           isLarge,
	}
}

// parsePathPrefixes splits a user-entered filter on newlines and commas into
// trimmed, non-empty prefixes.
func parsePathPrefixes(raw string) []string {
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == '\n' || r == ',' || r == ';'
	})
	prefixes := make([]string, 0, len(fields))
	for _, field := range fields {
		if trimmed := strings.TrimSpace(field); trimmed != "" {
			prefixes = append(prefixes, trimmed)
		}
	}
	return prefixes
}
