//go:build release

package app

import _ "embed"

var (
	//go:embed release-assets/rbxl-id-extractor.bin
	bundledRustyAssetToolBinaryData []byte

	//go:embed release-assets/joxblox-mesh-renderer.bin
	bundledMeshRendererBinaryData []byte

	//go:embed release-assets/CHANGELOG.md
	bundledChangelogMarkdownData string

	//go:embed release-assets/LICENSE.md
	bundledLicenseTextData string
)

func bundledRustyAssetToolBinary() []byte {
	return bundledRustyAssetToolBinaryData
}

func bundledMeshRendererBinary() []byte {
	return bundledMeshRendererBinaryData
}

func bundledChangelogMarkdown() string {
	return bundledChangelogMarkdownData
}

func bundledLicenseText() string {
	return bundledLicenseTextData
}
