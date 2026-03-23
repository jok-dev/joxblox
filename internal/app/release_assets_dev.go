//go:build !release

package app

func bundledRustExtractorBinary() []byte {
	return nil
}

func bundledChangelogMarkdown() string {
	return ""
}

func bundledLicenseText() string {
	return ""
}
