package loader

import (
	"regexp"
	"sort"
	"strings"
)

const maxExtractedVersions = 50

var versionPattern = regexp.MustCompile(`(?i)\bv?\d+(?:\.\d+)+\b`)

// ExtractVersionsFromResults scans every AssetInput for version-like tokens
// (v3.0, 2.1.0, V1.2.3) and returns the most common ones, capped at 50, sorted
// by frequency desc then lexically. Used to drive search autocomplete.
func ExtractVersionsFromResults(results []ScanResult) []string {
	counts := map[string]int{}
	for _, row := range results {
		input := row.AssetInput
		if input == "" {
			continue
		}
		matches := versionPattern.FindAllString(input, -1)
		for _, match := range matches {
			normalized := strings.ToLower(match)
			// Require at least one dot to avoid matching bare integers.
			if !strings.Contains(normalized, ".") {
				continue
			}
			counts[normalized]++
		}
	}
	versions := make([]string, 0, len(counts))
	for version := range counts {
		versions = append(versions, version)
	}
	sort.Slice(versions, func(i, j int) bool {
		if counts[versions[i]] != counts[versions[j]] {
			return counts[versions[i]] > counts[versions[j]]
		}
		return versions[i] < versions[j]
	})
	if len(versions) > maxExtractedVersions {
		versions = versions[:maxExtractedVersions]
	}
	return versions
}
