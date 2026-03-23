package app

import (
	"fmt"
	"strconv"
	"strings"
)

func buildHashCounts(results []scanResult) map[string]int {
	hashCounts := map[string]int{}
	for _, result := range results {
		normalizedHash := normalizeHash(result.FileSHA256)
		if normalizedHash == "" {
			continue
		}
		hashCounts[normalizedHash]++
	}
	return hashCounts
}

func isDuplicateByHash(result scanResult, hashCounts map[string]int) bool {
	normalizedHash := normalizeHash(result.FileSHA256)
	if normalizedHash == "" {
		return false
	}
	return hashCounts[normalizedHash] >= 2
}

func normalizeHash(rawHash string) string {
	return strings.ToLower(strings.TrimSpace(rawHash))
}

func containsString(values []string, candidate string) bool {
	for _, value := range values {
		if value == candidate {
			return true
		}
	}
	return false
}

func scanResultMatchesQuery(result scanResult, query string) bool {
	trimmedQuery := strings.ToLower(strings.TrimSpace(query))
	if trimmedQuery == "" {
		return true
	}
	searchFields := []string{
		strconv.FormatInt(result.AssetID, 10),
		strconv.Itoa(result.UseCount),
		result.FilePath,
		result.FileSHA256,
		result.Source,
		result.State,
		result.AssetTypeName,
		result.InstanceType,
		result.PropertyName,
		result.InstanceName,
		result.InstancePath,
		strconv.Itoa(result.AssetTypeID),
		result.Format,
		result.ContentType,
	}
	for _, fieldValue := range searchFields {
		if strings.Contains(strings.ToLower(fieldValue), trimmedQuery) {
			return true
		}
	}
	return false
}

func scanResultTypeLabel(result scanResult) string {
	trimmedTypeName := scanResultTypeFilterLabel(result)
	if result.AssetTypeID > 0 {
		return fmt.Sprintf("%s (%d)", trimmedTypeName, result.AssetTypeID)
	}
	return trimmedTypeName
}

func scanResultTypeFilterLabel(result scanResult) string {
	trimmedTypeName := strings.TrimSpace(result.AssetTypeName)
	if trimmedTypeName == "" {
		trimmedTypeName = "Unknown"
	}
	return trimmedTypeName
}

func scanResultInstanceTypeLabel(result scanResult) string {
	trimmedInstanceType := strings.TrimSpace(result.InstanceType)
	if trimmedInstanceType == "" {
		return "Unknown"
	}
	return trimmedInstanceType
}

func scanResultPropertyNameLabel(result scanResult) string {
	trimmedPropertyName := strings.TrimSpace(result.PropertyName)
	if trimmedPropertyName == "" {
		return "Unknown"
	}
	return trimmedPropertyName
}
