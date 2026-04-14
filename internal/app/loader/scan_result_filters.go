package loader

import (
	"fmt"
	"strconv"
	"strings"
)

func BuildHashCounts(results []ScanResult) map[string]int {
	hashCounts := map[string]int{}
	for _, result := range results {
		normalizedHash := NormalizeHash(result.FileSHA256)
		if normalizedHash == "" {
			continue
		}
		hashCounts[normalizedHash]++
	}
	return hashCounts
}

func IsDuplicateByHash(result ScanResult, hashCounts map[string]int) bool {
	normalizedHash := NormalizeHash(result.FileSHA256)
	if normalizedHash == "" {
		return false
	}
	return hashCounts[normalizedHash] >= 2
}

func NormalizeHash(rawHash string) string {
	return strings.ToLower(strings.TrimSpace(rawHash))
}

func ContainsString(values []string, candidate string) bool {
	for _, value := range values {
		if value == candidate {
			return true
		}
	}
	return false
}

func ScanResultMatchesQuery(result ScanResult, query string) bool {
	trimmedQuery := strings.ToLower(strings.TrimSpace(query))
	if trimmedQuery == "" {
		return true
	}
	searchFields := []string{
		result.Side,
		strconv.FormatInt(result.AssetID, 10),
		result.AssetInput,
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
		fmt.Sprintf("%.1f %.1f %.1f", result.WorldX, result.WorldY, result.WorldZ),
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

func ScanResultTypeLabel(result ScanResult) string {
	trimmedTypeName := ScanResultTypeFilterLabel(result)
	if result.AssetTypeID > 0 {
		return fmt.Sprintf("%s (%d)", trimmedTypeName, result.AssetTypeID)
	}
	return trimmedTypeName
}

func ScanResultTypeFilterLabel(result ScanResult) string {
	trimmedTypeName := strings.TrimSpace(result.AssetTypeName)
	if trimmedTypeName == "" {
		trimmedTypeName = "Unknown"
	}
	return trimmedTypeName
}

func ScanResultInstanceTypeLabel(result ScanResult) string {
	trimmedInstanceType := strings.TrimSpace(result.InstanceType)
	if trimmedInstanceType == "" {
		return "Unknown"
	}
	return trimmedInstanceType
}

func ScanResultPropertyNameLabel(result ScanResult) string {
	trimmedPropertyName := strings.TrimSpace(result.PropertyName)
	if trimmedPropertyName == "" {
		return "Unknown"
	}
	return trimmedPropertyName
}
