package loader

import (
	"fmt"
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

// ScanResultMatchesQuery is a convenience wrapper that compiles the query on
// every call. For hot paths (per-row filtering) prefer compiling once and
// reusing via ScanResultMatchesCompiledQuery.
func ScanResultMatchesQuery(result ScanResult, query string) bool {
	if strings.TrimSpace(query) == "" {
		return true
	}
	compiled := CompileScanQuery(query)
	return compiled.Matches(result, ScanQueryContext{})
}

// ScanResultMatchesCompiledQuery applies a pre-compiled query to a single row.
// Use this in the filter loop to avoid re-parsing per row.
func ScanResultMatchesCompiledQuery(result ScanResult, compiled ScanQuery, ctx ScanQueryContext) bool {
	if compiled.IsEmpty() {
		return true
	}
	return compiled.Matches(result, ctx)
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
