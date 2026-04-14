package scan

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"joxblox/internal/app/loader"
	"joxblox/internal/format"

	"fyne.io/fyne/v2"
)

const (
	scanTableExportVersion     = 1
	scanWorkspaceExportVersion = 1
	progressIOChunkSize        = 256 * 1024
)

type scanJSONProgressReporter func(float64)

type ScanImportFormat int

const (
	ScanImportFormatUnknown ScanImportFormat = iota
	ScanImportFormatTable
	ScanImportFormatWorkspace
)

type scanTableExportPayload struct {
	Version    int                  `json:"version"`
	ExportedAt string               `json:"exportedAt"`
	Rows       []scanTableExportRow `json:"rows"`
}

type scanWorkspaceExportPayload struct {
	Version    int                             `json:"version"`
	ExportedAt string                          `json:"exportedAt"`
	Tables     map[string][]scanTableExportRow `json:"tables"`
}

type scanTableExportRow struct {
	AssetID              int64                   `json:"assetId"`
	AssetInput           string                  `json:"assetInput,omitempty"`
	UseCount             int                     `json:"useCount"`
	FilePath             string                  `json:"filePath"`
	FileSHA256           string                  `json:"fileSha256"`
	InstanceType         string                  `json:"instanceType,omitempty"`
	InstanceName         string                  `json:"instanceName,omitempty"`
	InstancePath         string                  `json:"instancePath,omitempty"`
	PropertyName         string                  `json:"propertyName,omitempty"`
	Source               string                  `json:"source"`
	State                string                  `json:"state"`
	Width                int                     `json:"width"`
	Height               int                     `json:"height"`
	DurationMillis       int64                   `json:"durationMillis,omitempty"`
	BytesSize            int                     `json:"bytesSize"`
	RecompressedPNGSize  int                     `json:"recompressedPngSize"`
	RecompressedJPEGSize int                     `json:"recompressedJpegSize"`
	Format               string                  `json:"format"`
	ContentType          string                  `json:"contentType"`
	AssetTypeID          int                     `json:"assetTypeId"`
	AssetTypeName        string                  `json:"assetTypeName"`
	Warning              bool                    `json:"warning"`
	WarningCause         string                  `json:"warningCause"`
	AssetDeliveryJSON    string                  `json:"assetDeliveryJson"`
	ThumbnailJSON        string                  `json:"thumbnailJson"`
	EconomyJSON          string                  `json:"economyJson"`
	RustyAssetToolJSON   string                  `json:"rustExtractorJson"`
	ReferencedAssetIDs   []int64                 `json:"referencedAssetIds"`
	ChildAssets          []loader.ChildAssetInfo `json:"childAssets"`
	TotalBytesSize       int                     `json:"totalBytesSize"`
	MeshNumFaces         uint32                  `json:"meshNumFaces,omitempty"`
	MeshNumVerts         uint32                  `json:"meshNumVerts,omitempty"`
	SceneSurfaceArea     float64                 `json:"sceneSurfaceArea,omitempty"`
	LargestSurfacePath   string                  `json:"largestSurfacePath,omitempty"`
	LargeTextureScore    float64                 `json:"largeTextureScore,omitempty"`
	ImageResourceName    string                  `json:"imageResourceName,omitempty"`
	ImageBytesBase64     string                  `json:"imageBytesBase64,omitempty"`
}

func DetectScanImportFormat(payloadBytes []byte) ScanImportFormat {
	probe := struct {
		Rows   json.RawMessage `json:"rows"`
		Tables json.RawMessage `json:"tables"`
	}{}
	if err := json.Unmarshal(payloadBytes, &probe); err != nil {
		return ScanImportFormatUnknown
	}
	if trimmedTables := strings.TrimSpace(string(probe.Tables)); trimmedTables != "" && trimmedTables != "null" {
		return ScanImportFormatWorkspace
	}
	if trimmedRows := strings.TrimSpace(string(probe.Rows)); trimmedRows != "" && trimmedRows != "null" {
		return ScanImportFormatTable
	}
	return ScanImportFormatUnknown
}

func marshalScanTable(rows []loader.ScanResult, report scanJSONProgressReporter) ([]byte, error) {
	exportRows := make([]scanTableExportRow, 0, len(rows))
	reportScanJSONProgress(report, 0.05)
	for index, row := range rows {
		exportRows = append(exportRows, mapScanResultToExportRow(row))
		reportLoopProgress(report, index+1, len(rows), 0.05, 0.8)
	}

	payload := scanTableExportPayload{
		Version:    scanTableExportVersion,
		ExportedAt: time.Now().UTC().Format(time.RFC3339),
		Rows:       exportRows,
	}
	reportScanJSONProgress(report, 0.9)
	payloadBytes, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return nil, err
	}
	reportScanJSONProgress(report, 1)
	return payloadBytes, nil
}

func unmarshalScanTable(payloadBytes []byte, report scanJSONProgressReporter) ([]loader.ScanResult, error) {
	payload := scanTableExportPayload{}
	reportScanJSONProgress(report, 0.05)
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return nil, err
	}
	reportScanJSONProgress(report, 0.35)

	if payload.Version != scanTableExportVersion {
		return nil, fmt.Errorf("unsupported scan table version: %d", payload.Version)
	}
	if payload.Rows == nil {
		reportScanJSONProgress(report, 1)
		return []loader.ScanResult{}, nil
	}

	importedRows := make([]loader.ScanResult, 0, len(payload.Rows))
	for index, row := range payload.Rows {
		mappedRow, mapErr := mapExportRowToScanResult(row)
		if mapErr != nil {
			return nil, mapErr
		}
		importedRows = append(importedRows, mappedRow)
		reportLoopProgress(report, index+1, len(payload.Rows), 0.35, 1)
	}
	reportScanJSONProgress(report, 1)
	return importedRows, nil
}

func MarshalScanWorkspace(tablesByContext map[string][]loader.ScanResult, report scanJSONProgressReporter) ([]byte, error) {
	exportTables := map[string][]scanTableExportRow{}
	totalRows := countScanWorkspaceRows(tablesByContext)
	processedRows := 0
	reportScanJSONProgress(report, 0.05)
	for contextKey, rows := range tablesByContext {
		trimmedContextKey := strings.TrimSpace(contextKey)
		if trimmedContextKey == "" {
			continue
		}
		exportRows := make([]scanTableExportRow, 0, len(rows))
		for _, row := range rows {
			exportRows = append(exportRows, mapScanResultToExportRow(row))
			processedRows++
			reportLoopProgress(report, processedRows, totalRows, 0.05, 0.8)
		}
		exportTables[trimmedContextKey] = exportRows
	}
	payload := scanWorkspaceExportPayload{
		Version:    scanWorkspaceExportVersion,
		ExportedAt: time.Now().UTC().Format(time.RFC3339),
		Tables:     exportTables,
	}
	reportScanJSONProgress(report, 0.9)
	payloadBytes, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return nil, err
	}
	reportScanJSONProgress(report, 1)
	return payloadBytes, nil
}

func UnmarshalScanWorkspace(payloadBytes []byte, report scanJSONProgressReporter) (map[string][]loader.ScanResult, error) {
	payload := scanWorkspaceExportPayload{}
	reportScanJSONProgress(report, 0.05)
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return nil, err
	}
	reportScanJSONProgress(report, 0.3)
	if payload.Version != scanWorkspaceExportVersion {
		return nil, fmt.Errorf("unsupported scan workspace version: %d", payload.Version)
	}
	resultTables := map[string][]loader.ScanResult{}
	totalRows := countExportWorkspaceRows(payload.Tables)
	processedRows := 0
	for contextKey, rows := range payload.Tables {
		trimmedContextKey := strings.TrimSpace(contextKey)
		if trimmedContextKey == "" {
			continue
		}
		mappedRows := make([]loader.ScanResult, 0, len(rows))
		for _, row := range rows {
			mappedRow, mapErr := mapExportRowToScanResult(row)
			if mapErr != nil {
				return nil, mapErr
			}
			mappedRows = append(mappedRows, mappedRow)
			processedRows++
			reportLoopProgress(report, processedRows, totalRows, 0.3, 1)
		}
		resultTables[trimmedContextKey] = mappedRows
	}
	reportScanJSONProgress(report, 1)
	return resultTables, nil
}

func ReadFileWithProgress(path string, report scanJSONProgressReporter) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	fileInfo, statErr := file.Stat()
	if statErr != nil {
		return nil, statErr
	}
	if fileInfo.Size() <= 0 {
		payloadBytes, readErr := io.ReadAll(file)
		if readErr != nil {
			return nil, readErr
		}
		reportScanJSONProgress(report, 1)
		return payloadBytes, nil
	}

	var payloadBuffer bytes.Buffer
	buffer := make([]byte, progressIOChunkSize)
	var bytesRead int64
	for {
		readCount, readErr := file.Read(buffer)
		if readCount > 0 {
			payloadBuffer.Write(buffer[:readCount])
			bytesRead += int64(readCount)
			reportScanJSONProgress(report, float64(bytesRead)/float64(fileInfo.Size()))
		}
		if readErr == nil {
			continue
		}
		if readErr == io.EOF {
			break
		}
		return nil, readErr
	}
	reportScanJSONProgress(report, 1)
	return payloadBuffer.Bytes(), nil
}

func WriteFileWithProgress(path string, payloadBytes []byte, report scanJSONProgressReporter) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}

	if len(payloadBytes) == 0 {
		reportScanJSONProgress(report, 1)
		return file.Close()
	}

	for offset := 0; offset < len(payloadBytes); {
		chunkEnd := offset + progressIOChunkSize
		if chunkEnd > len(payloadBytes) {
			chunkEnd = len(payloadBytes)
		}
		written, writeErr := file.Write(payloadBytes[offset:chunkEnd])
		if writeErr != nil {
			return writeErr
		}
		offset += written
		reportScanJSONProgress(report, float64(offset)/float64(len(payloadBytes)))
	}
	reportScanJSONProgress(report, 1)
	return file.Close()
}

func countScanWorkspaceRows(tablesByContext map[string][]loader.ScanResult) int {
	totalRows := 0
	for contextKey, rows := range tablesByContext {
		if strings.TrimSpace(contextKey) == "" {
			continue
		}
		totalRows += len(rows)
	}
	return totalRows
}

func countExportWorkspaceRows(tablesByContext map[string][]scanTableExportRow) int {
	totalRows := 0
	for contextKey, rows := range tablesByContext {
		if strings.TrimSpace(contextKey) == "" {
			continue
		}
		totalRows += len(rows)
	}
	return totalRows
}

func reportLoopProgress(report scanJSONProgressReporter, completed int, total int, start float64, end float64) {
	if total <= 0 {
		reportScanJSONProgress(report, end)
		return
	}
	progress := float64(completed) / float64(total)
	reportScanJSONProgress(report, start+((end-start)*progress))
}

func reportScanJSONProgress(report scanJSONProgressReporter, progress float64) {
	if report == nil {
		return
	}
	report(format.Clamp(progress, 0.0, 1.0))
}

func mapScanResultToExportRow(row loader.ScanResult) scanTableExportRow {
	imageResourceName := ""
	imageBytesBase64 := ""
	if row.Resource != nil {
		imageBytes := row.Resource.Content()
		if len(imageBytes) > 0 {
			imageResourceName = strings.TrimSpace(row.Resource.Name())
			imageBytesBase64 = base64.StdEncoding.EncodeToString(imageBytes)
		}
	}
	return scanTableExportRow{
		AssetID:              row.AssetID,
		AssetInput:           row.AssetInput,
		UseCount:             row.UseCount,
		FilePath:             row.FilePath,
		FileSHA256:           row.FileSHA256,
		InstanceType:         row.InstanceType,
		InstanceName:         row.InstanceName,
		InstancePath:         row.InstancePath,
		PropertyName:         row.PropertyName,
		Source:               row.Source,
		State:                row.State,
		Width:                row.Width,
		Height:               row.Height,
		DurationMillis:       row.Duration.Milliseconds(),
		BytesSize:            row.BytesSize,
		RecompressedPNGSize:  row.RecompressedPNGSize,
		RecompressedJPEGSize: row.RecompressedJPEGSize,
		Format:               row.Format,
		ContentType:          row.ContentType,
		AssetTypeID:          row.AssetTypeID,
		AssetTypeName:        row.AssetTypeName,
		Warning:              row.Warning,
		WarningCause:         row.WarningCause,
		AssetDeliveryJSON:    row.AssetDeliveryJSON,
		ThumbnailJSON:        row.ThumbnailJSON,
		EconomyJSON:          row.EconomyJSON,
		RustyAssetToolJSON:   row.RustyAssetToolJSON,
		ReferencedAssetIDs:   row.ReferencedAssetIDs,
		ChildAssets:          row.ChildAssets,
		TotalBytesSize:       row.TotalBytesSize,
		MeshNumFaces:         row.MeshNumFaces,
		MeshNumVerts:         row.MeshNumVerts,
		SceneSurfaceArea:     row.SceneSurfaceArea,
		LargestSurfacePath:   row.LargestSurfacePath,
		LargeTextureScore:    row.LargeTextureScore,
		ImageResourceName:    imageResourceName,
		ImageBytesBase64:     imageBytesBase64,
	}
}

func mapExportRowToScanResult(row scanTableExportRow) (loader.ScanResult, error) {
	importedResource := (*fyne.StaticResource)(nil)
	if strings.TrimSpace(row.ImageBytesBase64) != "" {
		imageBytes, decodeErr := base64.StdEncoding.DecodeString(row.ImageBytesBase64)
		if decodeErr != nil {
			return loader.ScanResult{}, fmt.Errorf("failed decoding image bytes for asset %d: %w", row.AssetID, decodeErr)
		}
		resourceName := strings.TrimSpace(row.ImageResourceName)
		if resourceName == "" {
			resourceName = fmt.Sprintf("asset_%d_imported", row.AssetID)
		}
		importedResource = fyne.NewStaticResource(resourceName, imageBytes)
	}
	return loader.ScanResult{
		AssetID:              row.AssetID,
		AssetInput:           strings.TrimSpace(row.AssetInput),
		UseCount:             row.UseCount,
		FilePath:             row.FilePath,
		FileSHA256:           strings.TrimSpace(row.FileSHA256),
		InstanceType:         strings.TrimSpace(row.InstanceType),
		InstanceName:         strings.TrimSpace(row.InstanceName),
		InstancePath:         strings.TrimSpace(row.InstancePath),
		PropertyName:         strings.TrimSpace(row.PropertyName),
		Source:               row.Source,
		State:                row.State,
		Width:                row.Width,
		Height:               row.Height,
		Duration:             time.Duration(row.DurationMillis) * time.Millisecond,
		BytesSize:            row.BytesSize,
		RecompressedPNGSize:  row.RecompressedPNGSize,
		RecompressedJPEGSize: row.RecompressedJPEGSize,
		Format:               row.Format,
		ContentType:          row.ContentType,
		AssetTypeID:          row.AssetTypeID,
		AssetTypeName:        row.AssetTypeName,
		Warning:              row.Warning,
		WarningCause:         row.WarningCause,
		AssetDeliveryJSON:    row.AssetDeliveryJSON,
		ThumbnailJSON:        row.ThumbnailJSON,
		EconomyJSON:          row.EconomyJSON,
		RustyAssetToolJSON:   row.RustyAssetToolJSON,
		ReferencedAssetIDs:   row.ReferencedAssetIDs,
		ChildAssets:          row.ChildAssets,
		TotalBytesSize:       row.TotalBytesSize,
		MeshNumFaces:         row.MeshNumFaces,
		MeshNumVerts:         row.MeshNumVerts,
		SceneSurfaceArea:     row.SceneSurfaceArea,
		LargestSurfacePath:   strings.TrimSpace(row.LargestSurfacePath),
		LargeTextureScore:    row.LargeTextureScore,
		Resource:             importedResource,
	}, nil
}
