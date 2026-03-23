package app

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
)

const (
	scanTableExportVersion     = 1
	scanWorkspaceExportVersion = 1
)

type scanImportFormat int

const (
	scanImportFormatUnknown scanImportFormat = iota
	scanImportFormatTable
	scanImportFormatWorkspace
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
	AssetID              int64            `json:"assetId"`
	UseCount             int              `json:"useCount"`
	FilePath             string           `json:"filePath"`
	FileSHA256           string           `json:"fileSha256"`
	Source               string           `json:"source"`
	State                string           `json:"state"`
	Width                int              `json:"width"`
	Height               int              `json:"height"`
	DurationMillis       int64            `json:"durationMillis,omitempty"`
	BytesSize            int              `json:"bytesSize"`
	RecompressedPNGSize  int              `json:"recompressedPngSize"`
	RecompressedJPEGSize int              `json:"recompressedJpegSize"`
	Format               string           `json:"format"`
	ContentType          string           `json:"contentType"`
	AssetTypeID          int              `json:"assetTypeId"`
	AssetTypeName        string           `json:"assetTypeName"`
	Warning              bool             `json:"warning"`
	WarningCause         string           `json:"warningCause"`
	AssetDeliveryJSON    string           `json:"assetDeliveryJson"`
	ThumbnailJSON        string           `json:"thumbnailJson"`
	EconomyJSON          string           `json:"economyJson"`
	RustExtractorJSON    string           `json:"rustExtractorJson"`
	ReferencedAssetIDs   []int64          `json:"referencedAssetIds"`
	ChildAssets          []childAssetInfo `json:"childAssets"`
	TotalBytesSize       int              `json:"totalBytesSize"`
	ImageResourceName    string           `json:"imageResourceName,omitempty"`
	ImageBytesBase64     string           `json:"imageBytesBase64,omitempty"`
}

func detectScanImportFormat(payloadBytes []byte) scanImportFormat {
	probe := struct {
		Rows   json.RawMessage `json:"rows"`
		Tables json.RawMessage `json:"tables"`
	}{}
	if err := json.Unmarshal(payloadBytes, &probe); err != nil {
		return scanImportFormatUnknown
	}
	if trimmedTables := strings.TrimSpace(string(probe.Tables)); trimmedTables != "" && trimmedTables != "null" {
		return scanImportFormatWorkspace
	}
	if trimmedRows := strings.TrimSpace(string(probe.Rows)); trimmedRows != "" && trimmedRows != "null" {
		return scanImportFormatTable
	}
	return scanImportFormatUnknown
}

func marshalScanTable(rows []scanResult) ([]byte, error) {
	exportRows := make([]scanTableExportRow, 0, len(rows))
	for _, row := range rows {
		exportRows = append(exportRows, mapScanResultToExportRow(row))
	}

	payload := scanTableExportPayload{
		Version:    scanTableExportVersion,
		ExportedAt: time.Now().UTC().Format(time.RFC3339),
		Rows:       exportRows,
	}
	return json.MarshalIndent(payload, "", "  ")
}

func unmarshalScanTable(payloadBytes []byte) ([]scanResult, error) {
	payload := scanTableExportPayload{}
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return nil, err
	}

	if payload.Version != scanTableExportVersion {
		return nil, fmt.Errorf("unsupported scan table version: %d", payload.Version)
	}
	if payload.Rows == nil {
		return []scanResult{}, nil
	}

	importedRows := make([]scanResult, 0, len(payload.Rows))
	for _, row := range payload.Rows {
		mappedRow, mapErr := mapExportRowToScanResult(row)
		if mapErr != nil {
			return nil, mapErr
		}
		importedRows = append(importedRows, mappedRow)
	}
	return importedRows, nil
}

func marshalScanWorkspace(tablesByContext map[string][]scanResult) ([]byte, error) {
	exportTables := map[string][]scanTableExportRow{}
	for contextKey, rows := range tablesByContext {
		trimmedContextKey := strings.TrimSpace(contextKey)
		if trimmedContextKey == "" {
			continue
		}
		exportRows := make([]scanTableExportRow, 0, len(rows))
		for _, row := range rows {
			exportRows = append(exportRows, mapScanResultToExportRow(row))
		}
		exportTables[trimmedContextKey] = exportRows
	}
	payload := scanWorkspaceExportPayload{
		Version:    scanWorkspaceExportVersion,
		ExportedAt: time.Now().UTC().Format(time.RFC3339),
		Tables:     exportTables,
	}
	return json.MarshalIndent(payload, "", "  ")
}

func unmarshalScanWorkspace(payloadBytes []byte) (map[string][]scanResult, error) {
	payload := scanWorkspaceExportPayload{}
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return nil, err
	}
	if payload.Version != scanWorkspaceExportVersion {
		return nil, fmt.Errorf("unsupported scan workspace version: %d", payload.Version)
	}
	resultTables := map[string][]scanResult{}
	for contextKey, rows := range payload.Tables {
		trimmedContextKey := strings.TrimSpace(contextKey)
		if trimmedContextKey == "" {
			continue
		}
		mappedRows := make([]scanResult, 0, len(rows))
		for _, row := range rows {
			mappedRow, mapErr := mapExportRowToScanResult(row)
			if mapErr != nil {
				return nil, mapErr
			}
			mappedRows = append(mappedRows, mappedRow)
		}
		resultTables[trimmedContextKey] = mappedRows
	}
	return resultTables, nil
}

func mapScanResultToExportRow(row scanResult) scanTableExportRow {
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
		UseCount:             row.UseCount,
		FilePath:             row.FilePath,
		FileSHA256:           row.FileSHA256,
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
		RustExtractorJSON:    row.RustExtractorJSON,
		ReferencedAssetIDs:   row.ReferencedAssetIDs,
		ChildAssets:          row.ChildAssets,
		TotalBytesSize:       row.TotalBytesSize,
		ImageResourceName:    imageResourceName,
		ImageBytesBase64:     imageBytesBase64,
	}
}

func mapExportRowToScanResult(row scanTableExportRow) (scanResult, error) {
	importedResource := (*fyne.StaticResource)(nil)
	if strings.TrimSpace(row.ImageBytesBase64) != "" {
		imageBytes, decodeErr := base64.StdEncoding.DecodeString(row.ImageBytesBase64)
		if decodeErr != nil {
			return scanResult{}, fmt.Errorf("failed decoding image bytes for asset %d: %w", row.AssetID, decodeErr)
		}
		resourceName := strings.TrimSpace(row.ImageResourceName)
		if resourceName == "" {
			resourceName = fmt.Sprintf("asset_%d_imported", row.AssetID)
		}
		importedResource = fyne.NewStaticResource(resourceName, imageBytes)
	}
	return scanResult{
		AssetID:              row.AssetID,
		UseCount:             row.UseCount,
		FilePath:             row.FilePath,
		FileSHA256:           strings.TrimSpace(row.FileSHA256),
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
		RustExtractorJSON:    row.RustExtractorJSON,
		ReferencedAssetIDs:   row.ReferencedAssetIDs,
		ChildAssets:          row.ChildAssets,
		TotalBytesSize:       row.TotalBytesSize,
		Resource:             importedResource,
	}, nil
}

func marshalScanTableMarkdown(rows []scanResult) ([]byte, error) {
	var builder strings.Builder
	builder.WriteString("# Scan Results\n\n")
	builder.WriteString(fmt.Sprintf("Generated: %s\n\n", time.Now().UTC().Format(time.RFC3339)))
	builder.WriteString(fmt.Sprintf("Total rows: %d\n\n", len(rows)))
	builder.WriteString("| Asset ID | Use Count | Type | Self Size | Dimensions | State | Source | SHA256 |\n")
	builder.WriteString("| --- | --- | --- | --- | --- | --- | --- | --- |\n")
	for _, row := range rows {
		typeText := row.AssetTypeName
		if row.AssetTypeID > 0 {
			typeText = fmt.Sprintf("%s (%d)", row.AssetTypeName, row.AssetTypeID)
		}
		dimensionText := "-"
		if row.Width > 0 && row.Height > 0 {
			dimensionText = fmt.Sprintf("%dx%d", row.Width, row.Height)
		}
		builder.WriteString(fmt.Sprintf(
			"| %s | %s | %s | %s | %s | %s | %s | %s |\n",
			escapeMarkdownTableCell(strconv.FormatInt(row.AssetID, 10)),
			escapeMarkdownTableCell(strconv.Itoa(row.UseCount)),
			escapeMarkdownTableCell(typeText),
			escapeMarkdownTableCell(formatSizeAuto(row.BytesSize)),
			escapeMarkdownTableCell(dimensionText),
			escapeMarkdownTableCell(row.State),
			escapeMarkdownTableCell(row.Source),
			escapeMarkdownTableCell(row.FileSHA256),
		))
	}
	return []byte(builder.String()), nil
}

func escapeMarkdownTableCell(rawValue string) string {
	cleanedValue := strings.TrimSpace(rawValue)
	if cleanedValue == "" {
		return "-"
	}
	cleanedValue = strings.ReplaceAll(cleanedValue, "|", "\\|")
	cleanedValue = strings.ReplaceAll(cleanedValue, "\n", " ")
	cleanedValue = strings.ReplaceAll(cleanedValue, "\r", " ")
	return cleanedValue
}
