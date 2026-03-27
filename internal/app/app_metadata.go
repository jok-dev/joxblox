package app

import (
	"fmt"
	"os"
	"strings"
)

const (
	appDisplayName = "Joxblox"
	appAuthorName  = "Jok"
	appSourceURL   = "https://github.com/jok-dev/joxblox"
	appLicenseName = "GNU General Public License v3.0"
)

var appVersion = "v1.1.0"

func appAboutSummary() string {
	return fmt.Sprintf(
		"%s %s\n\nAuthor: %s\nSource: %s\nLicense: %s\n\nThis program is free software and comes with ABSOLUTELY NO WARRANTY.",
		appDisplayName,
		appVersion,
		appAuthorName,
		appSourceURL,
		appLicenseName,
	)
}

func loadChangelogText() string {
	if bundledText := strings.TrimSpace(bundledChangelogMarkdown()); bundledText != "" {
		return bundledText
	}
	return loadRepositoryDocument("CHANGELOG.md", "# Changelog\n\nChangelog unavailable.")
}

func loadLicenseText() string {
	if bundledText := strings.TrimSpace(bundledLicenseText()); bundledText != "" {
		return bundledText
	}
	return loadRepositoryDocument("LICENSE.md", "License text unavailable.")
}

func loadRepositoryDocument(fileName string, fallback string) string {
	repositoryRootPath, rootErr := getRepositoryRootPath()
	if rootErr != nil {
		return fallback
	}
	documentBytes, readErr := os.ReadFile(repositoryRootPath + string(os.PathSeparator) + fileName)
	if readErr != nil {
		return fallback
	}
	documentText := strings.TrimSpace(string(documentBytes))
	if documentText == "" {
		return fallback
	}
	return documentText
}
