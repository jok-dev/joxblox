//go:build release

package app

import _ "embed"

var (
	//go:embed release-assets/rbxl-id-extractor.bin
	bundledRustExtractorBinaryData []byte

	//go:embed release-assets/CHANGELOG.md
	bundledChangelogMarkdownData string

	//go:embed release-assets/LICENSE.md
	bundledLicenseTextData string
)

func bundledRustExtractorBinary() []byte {
	return bundledRustExtractorBinaryData
}

func bundledChangelogMarkdown() string {
	return bundledChangelogMarkdownData
}

func bundledLicenseText() string {
	return bundledLicenseTextData
}
