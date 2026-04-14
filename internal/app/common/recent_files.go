package common

import (
	"encoding/json"
	"strings"

	"fyne.io/fyne/v2"
)

const maxRecentFiles = 10

func LoadRecentFilesFromPreferences(preferenceKey string) []string {
	trimmedKey := strings.TrimSpace(preferenceKey)
	if trimmedKey == "" {
		return []string{}
	}
	currentApp := fyne.CurrentApp()
	if currentApp == nil {
		return []string{}
	}
	rawValue := strings.TrimSpace(currentApp.Preferences().String(trimmedKey))
	if rawValue == "" {
		return []string{}
	}

	recentFiles := []string{}
	if err := json.Unmarshal([]byte(rawValue), &recentFiles); err != nil {
		return []string{}
	}
	normalizedRecentFiles := make([]string, 0, len(recentFiles))
	seenPaths := map[string]bool{}
	for _, recentPath := range recentFiles {
		trimmedPath := strings.TrimSpace(recentPath)
		if trimmedPath == "" {
			continue
		}
		normalizedPathKey := strings.ToLower(trimmedPath)
		if seenPaths[normalizedPathKey] {
			continue
		}
		seenPaths[normalizedPathKey] = true
		normalizedRecentFiles = append(normalizedRecentFiles, trimmedPath)
		if len(normalizedRecentFiles) >= maxRecentFiles {
			break
		}
	}
	return normalizedRecentFiles
}

func SaveRecentFilesToPreferences(preferenceKey string, recentFiles []string) {
	trimmedKey := strings.TrimSpace(preferenceKey)
	if trimmedKey == "" {
		return
	}
	currentApp := fyne.CurrentApp()
	if currentApp == nil {
		return
	}
	normalizedRecentFiles := make([]string, 0, len(recentFiles))
	seenPaths := map[string]bool{}
	for _, recentPath := range recentFiles {
		trimmedPath := strings.TrimSpace(recentPath)
		if trimmedPath == "" {
			continue
		}
		normalizedPathKey := strings.ToLower(trimmedPath)
		if seenPaths[normalizedPathKey] {
			continue
		}
		seenPaths[normalizedPathKey] = true
		normalizedRecentFiles = append(normalizedRecentFiles, trimmedPath)
		if len(normalizedRecentFiles) >= maxRecentFiles {
			break
		}
	}
	jsonBytes, err := json.Marshal(normalizedRecentFiles)
	if err != nil {
		return
	}
	currentApp.Preferences().SetString(trimmedKey, string(jsonBytes))
}
