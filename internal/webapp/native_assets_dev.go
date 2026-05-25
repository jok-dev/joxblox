//go:build !release

package webapp

// bundledRustyAssetToolBinary returns nil in non-release builds; the extractor
// then falls back to a sidecar binary or `cargo run` from the source tree.
func bundledRustyAssetToolBinary() []byte { return nil }
