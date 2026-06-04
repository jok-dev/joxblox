//go:build release

package webapp

import _ "embed"

// bundledRustyAssetToolBinaryData is the Rust extractor binary, embedded in
// release builds so the shipped app can extract without a source tree or a
// sidecar binary on disk. build.sh populates release-assets/ before building
// with the `release` tag.
//
//go:embed release-assets/rbxl-id-extractor.bin
var bundledRustyAssetToolBinaryData []byte

func bundledRustyAssetToolBinary() []byte { return bundledRustyAssetToolBinaryData }
