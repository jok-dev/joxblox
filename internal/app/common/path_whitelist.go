package common

import (
	"strings"

	"joxblox/internal/app/loader"
)

func MatchesAnyPathWhitelist(instancePath string, patternsText string) bool {
	for _, line := range strings.Split(patternsText, "\n") {
		pattern := strings.TrimSpace(line)
		if pattern == "" {
			continue
		}
		if matchesPathWhitelist(instancePath, pattern) {
			return true
		}
	}
	return false
}

func WhitelistPatternsToPathPrefixes(patternsText string) []string {
	var prefixes []string
	for _, line := range strings.Split(patternsText, "\n") {
		pattern := strings.TrimSpace(line)
		if pattern == "" {
			continue
		}
		lower := strings.ToLower(pattern)
		if strings.HasSuffix(lower, ".*") {
			prefixes = append(prefixes, strings.TrimSuffix(lower, "*"))
		} else if strings.HasSuffix(lower, "*") {
			prefixes = append(prefixes, strings.TrimSuffix(lower, "*"))
		} else {
			prefixes = append(prefixes, lower)
		}
	}
	return prefixes
}

func ScanHitMatchesPathWhitelist(hit loader.ScanHit, patternsText string) bool {
	if hit.InstancePath != "" && MatchesAnyPathWhitelist(hit.InstancePath, patternsText) {
		return true
	}
	for _, path := range hit.AllInstancePaths {
		if path != "" && MatchesAnyPathWhitelist(path, patternsText) {
			return true
		}
	}
	return false
}

func matchesPathWhitelist(instancePath string, pattern string) bool {
	normalizedPath := strings.ToLower(strings.TrimSpace(instancePath))
	normalizedPattern := strings.ToLower(strings.TrimSpace(pattern))
	if normalizedPattern == "" {
		return true
	}
	if strings.HasSuffix(normalizedPattern, ".*") {
		prefix := strings.TrimSuffix(normalizedPattern, ".*")
		return strings.HasPrefix(normalizedPath, prefix+".")
	}
	if strings.HasSuffix(normalizedPattern, "*") {
		prefix := strings.TrimSuffix(normalizedPattern, "*")
		return strings.HasPrefix(normalizedPath, prefix)
	}
	return normalizedPath == normalizedPattern
}
