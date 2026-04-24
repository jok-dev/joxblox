// Command ktx2-probe exercises the KTX2 asset-delivery path end-to-end for
// a single asset ID: batch-endpoint lookup -> CDN fetch with zstd-friendly
// headers -> transport unwrap -> KTX2 parse. Prints real upload dimensions,
// format, mip chain, and supercompression scheme so we can validate the
// pipeline against known oversize assets before wiring it into the loader.
package main

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"joxblox/internal/roblox"
	"joxblox/internal/roblox/ktx2"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: ktx2-probe <assetId>")
		os.Exit(1)
	}
	assetID, err := strconv.ParseInt(os.Args[1], 10, 64)
	if err != nil || assetID <= 0 {
		fmt.Fprintln(os.Stderr, "assetId must be a positive integer")
		os.Exit(1)
	}

	cookie, err := roblox.LoadRoblosecurityCookieFromKeyring()
	if err != nil {
		fmt.Fprintf(os.Stderr, "load roblosecurity cookie: %v\n", err)
		os.Exit(1)
	}
	if cookie == "" {
		fmt.Fprintln(os.Stderr, "no roblosecurity cookie in keyring — sign in through the app first or set it via the auth settings")
		os.Exit(1)
	}
	roblox.SetRoblosecurityCookie(cookie)

	// Start with the direct /v1/asset?id= endpoint — it returns the
	// content bytes directly (no JSON indirection) and should honour
	// Accept-Encoding: zstd to serve the high-res KTX2 variant. If the
	// batch endpoint is needed later, we can add that path.
	directURL := fmt.Sprintf("https://assetdelivery.roblox.com/v1/asset/?id=%d", assetID)
	fmt.Printf("Asset %d: fetching %s with Accept-Encoding: gzip, deflate, zstd ...\n", assetID, directURL)
	raw, err := roblox.FetchAssetDeliveryLocation(directURL, 30*time.Second)
	if err != nil {
		fmt.Fprintf(os.Stderr, "CDN fetch failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("  transport payload: %d bytes (first bytes: %x)\n", len(raw), raw[:min(len(raw), 8)])

	unwrapped, err := ktx2.DecompressTransportWrapper(raw)
	if err != nil {
		fmt.Fprintf(os.Stderr, "transport unwrap failed: %v\n", err)
		os.Exit(1)
	}
	if len(unwrapped) != len(raw) {
		fmt.Printf("  unwrapped: %d bytes (transport compression applied)\n", len(unwrapped))
	}

	if !hasKTX2Magic(unwrapped) {
		fmt.Printf("\nResponse is NOT a KTX2 container. First 16 bytes: %x\n", unwrapped[:min(len(unwrapped), 16)])
		if len(unwrapped) >= 24 && unwrapped[0] == 0x89 && unwrapped[1] == 'P' && unwrapped[2] == 'N' && unwrapped[3] == 'G' {
			// PNG IHDR chunk lives at bytes 8-31; width is big-endian uint32
			// at offset 16, height at offset 20.
			w := uint32(unwrapped[16])<<24 | uint32(unwrapped[17])<<16 | uint32(unwrapped[18])<<8 | uint32(unwrapped[19])
			h := uint32(unwrapped[20])<<24 | uint32(unwrapped[21])<<16 | uint32(unwrapped[22])<<8 | uint32(unwrapped[23])
			fmt.Printf("  PNG dimensions: %dx%d\n", w, h)
			outPath := fmt.Sprintf("/tmp/asset_%d.png", assetID)
			if writeErr := os.WriteFile(outPath, unwrapped, 0644); writeErr == nil {
				fmt.Printf("  saved to %s for inspection\n", outPath)
			}
		}
		return
	}

	container, err := ktx2.Parse(unwrapped)
	if err != nil {
		fmt.Fprintf(os.Stderr, "KTX2 parse failed: %v\n", err)
		os.Exit(1)
	}

	header := container.Header
	fmt.Printf("\nKTX2 metadata:\n")
	fmt.Printf("  dimensions        : %dx%d\n", header.PixelWidth, header.PixelHeight)
	fmt.Printf("  vkFormat          : %d%s\n", header.VkFormat, formatHint(header.VkFormat))
	fmt.Printf("  supercompression  : %s\n", supercompressionName(header.SupercompressionScheme))
	fmt.Printf("  levels            : %d\n", header.LevelCount)
	fmt.Printf("  layerCount/faces  : %d / %d\n", header.LayerCount, header.FaceCount)
	fmt.Printf("  isBCFormat        : %v\n", header.IsBCFormat())
	for i := range container.Levels {
		w, h := container.MipDimensions(i)
		level := container.Levels[i]
		fmt.Printf("  mip %2d: %dx%d  stored=%d  uncompressed=%d\n",
			i, w, h, level.ByteLength, level.UncompressedByteLength)
	}

	fmt.Println("\nAttempting to decompress mip 0 ...")
	mip0, err := container.DecompressLevel(0)
	if err != nil {
		fmt.Printf("  skipped: %v\n", err)
		fmt.Println("  (BasisLZ/UASTC payloads need an external transcoder — not in tier 1-3 scope)")
		return
	}
	fmt.Printf("  mip 0 uncompressed: %d bytes (first bytes: %x)\n",
		len(mip0), mip0[:min(len(mip0), 16)])
	if header.IsBCFormat() {
		fmt.Println("  Raw BCn block data — can be decoded by internal/renderdoc BC helpers.")
	}
}

func hasKTX2Magic(data []byte) bool {
	if len(data) < len(ktx2.Magic) {
		return false
	}
	for i, b := range ktx2.Magic {
		if data[i] != b {
			return false
		}
	}
	return true
}

func formatHint(f ktx2.VkFormat) string {
	switch f {
	case ktx2.VkFormatUndefined:
		return " (UNDEFINED — likely BasisLZ/UASTC)"
	case ktx2.VkFormatBC1RGBUnorm, ktx2.VkFormatBC1RGBSrgb, ktx2.VkFormatBC1RGBAUnorm, ktx2.VkFormatBC1RGBASrgb:
		return " (BC1)"
	case ktx2.VkFormatBC3Unorm, ktx2.VkFormatBC3Srgb:
		return " (BC3 / DXT5)"
	case ktx2.VkFormatBC7Unorm, ktx2.VkFormatBC7Srgb:
		return " (BC7)"
	}
	return ""
}

func supercompressionName(s ktx2.SupercompressionScheme) string {
	switch s {
	case ktx2.SupercompressionNone:
		return "None"
	case ktx2.SupercompressionBasisLZ:
		return "BasisLZ"
	case ktx2.SupercompressionZstd:
		return "Zstd"
	case ktx2.SupercompressionZLIB:
		return "ZLIB"
	}
	return fmt.Sprintf("unknown(%d)", s)
}
