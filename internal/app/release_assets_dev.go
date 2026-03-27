//go:build !release

package app

func bundledRustyAssetToolBinary() []byte {
	return nil
}

func bundledChangelogMarkdown() string {
	return ""
}

func bundledLicenseText() string {
	return ""
}
