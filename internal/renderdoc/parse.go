package renderdoc

import (
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"os"
	"strings"
)

// TextureInfo is a flattened view of one ID3D11Device::CreateTexture2D call
// found in a RenderDoc zip.xml capture.
type TextureInfo struct {
	ResourceID   string
	Width        int
	Height       int
	MipLevels    int
	ArraySize    int
	Format       string
	ShortFormat  string
	Usage        string
	BindFlags    string
	MiscFlags    string
	IsBCFormat   bool
	IsUnknownFmt bool
	Bytes        int64
	Category     TextureCategory
	// Uploads lists every UpdateSubresource targeting this texture, in the
	// order they appear in the capture. Each entry points to a buffer stored
	// in the companion .zip archive and corresponds to one mip level (or one
	// array slice × mip level for texture arrays/cubemaps).
	Uploads []TextureUpload
	// PixelHash is a truncated SHA-256 hex (16 chars) of the decoded
	// base-mip pixel buffer. Populated by ComputeTextureHashes when a
	// BufferStore is available. Lets the UI spot identical textures and
	// filter out well-known defaults by content.
	PixelHash string
	// DHash is the 64-bit perceptual hash (dHash) of the decoded base
	// mip, computed in the same pass as PixelHash. Used for cross-
	// referencing captured textures against a Roblox place's scan
	// results — exact pixel hashes don't survive BC compression
	// roundtripping but dHash with a small Hamming threshold does.
	DHash uint64
}

// TextureUpload captures one ID3D11DeviceContext::UpdateSubresource chunk.
// For a simple 2D texture with N mips, there will be N uploads with
// Subresource=0..N-1 corresponding to mip levels 0..N-1 in decreasing size.
type TextureUpload struct {
	Subresource int
	BufferID    string
	ByteLength  int
	SrcRowPitch int
}

// TextureCategory groups the textures the way a reader cares about them when
// skimming a capture: asset textures vs engine scratch space.
type TextureCategory string

const (
	// BC-compressed assets split by whether the format carries alpha. This
	// matters for optimisation audits: opaque assets can often be re-encoded
	// to BC1 (0.5 B/px), while alpha-carrying ones need BC3/BC7 (1.0 B/px),
	// so knowing the split lets a reader see where alpha is actually costing
	// VRAM vs. where it was a default encoding choice.
	CategoryAssetOpaque TextureCategory = "Asset w/o Alpha"
	CategoryAssetAlpha  TextureCategory = "Asset w/ Alpha"
	CategoryAssetRaw    TextureCategory = "Asset (uncompressed)"
	CategoryRenderTgt TextureCategory = "Render target"
	CategoryDepthTgt  TextureCategory = "Depth/stencil target"
	CategoryCubemap   TextureCategory = "Cubemap (skybox/probe)"
	CategorySmallUtil TextureCategory = "Small / utility"
	CategoryBuiltin   TextureCategory = "Roblox built-in"
	// More specific built-in categories. When we know what a default
	// texture actually is (via its pixel hash), we prefer a named category
	// over the generic "Roblox built-in" so the table is informative.
	CategoryBuiltinBRDFLUT TextureCategory = "Specular BRDF LUT"
	// Metalness/roughness packed texture. Roblox's SurfaceAppearance packs
	// MetalnessMap into R and RoughnessMap into G of a single BC1 tile
	// (B is always strictly 0 on these, which is how we detect them).
	// Split into two sub-categories so a reader can tell the difference
	// between a material that has authored MR and one that's shipping the
	// engine's blank fallback — both cost the same VRAM but only one
	// reflects a developer intent.
	CategoryBlankMR  TextureCategory = "Blank MR"
	CategoryCustomMR TextureCategory = "Custom MR"
	// Normal maps stored in the DXT5nm swizzle (R=255, B=0, X in alpha,
	// Y in green). Detected from decoded pixel pattern rather than a
	// specific hash because each user's normal map has a unique hash.
	CategoryNormalDXT5nm TextureCategory = "Normal (DXT5nm)"
	CategoryUnknown   TextureCategory = "Unknown"
)

// Report is the whole-capture summary: the per-texture rows plus simple
// aggregates used by the UI summary bar.
type Report struct {
	Textures    []TextureInfo
	TotalBytes  int64
	ByCategory  map[TextureCategory]CategoryAggregate
	GPUAdapter  string
	Driver      string
}

// CategoryAggregate is the per-category roll-up shown at the top of the UI.
type CategoryAggregate struct {
	Count int
	Bytes int64
}

// ParseCaptureXMLFile parses the XML file emitted by
// `renderdoccmd convert -c zip.xml` and returns a Report.
func ParseCaptureXMLFile(path string) (*Report, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open capture xml: %w", err)
	}
	defer file.Close()
	return parseCaptureXML(file)
}

// parseCaptureXML streams through the XML pulling only the chunks we care
// about, so we don't have to build the full DOM of a multi-MB document.
func parseCaptureXML(reader io.Reader) (*Report, error) {
	decoder := xml.NewDecoder(reader)
	report := &Report{ByCategory: map[TextureCategory]CategoryAggregate{}}
	textureIndexByID := map[string]int{}

	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("xml parse: %w", err)
		}
		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		if start.Name.Local != "chunk" {
			continue
		}
		name := attr(start, "name")
		switch name {
		case "ID3D11Device::CreateTexture2D":
			texture, parseErr := parseCreateTexture2DChunk(decoder, start)
			if parseErr != nil {
				return nil, parseErr
			}
			if texture == nil {
				continue
			}
			report.Textures = append(report.Textures, *texture)
			textureIndexByID[texture.ResourceID] = len(report.Textures) - 1
			report.TotalBytes += texture.Bytes
			agg := report.ByCategory[texture.Category]
			agg.Count++
			agg.Bytes += texture.Bytes
			report.ByCategory[texture.Category] = agg
		case "ID3D11DeviceContext::UpdateSubresource":
			resourceID, upload, parseErr := parseUpdateSubresourceChunk(decoder, start)
			if parseErr != nil {
				return nil, parseErr
			}
			if resourceID == "" {
				continue
			}
			if idx, ok := textureIndexByID[resourceID]; ok {
				report.Textures[idx].Uploads = append(report.Textures[idx].Uploads, upload)
			}
		case "Internal::Driver Initialisation Parameters":
			// Capture the GPU adapter name for the summary bar. We only peek at
			// a small window of tokens since driver params are always early in
			// the capture.
			adapterName, driverName := parseDriverInit(decoder, start)
			if adapterName != "" {
				report.GPUAdapter = adapterName
			}
			if driverName != "" {
				report.Driver = driverName
			}
		}
	}

	return report, nil
}

// parseUpdateSubresourceChunk extracts the target resource ID, subresource
// index, and buffer reference from one UpdateSubresource chunk.
func parseUpdateSubresourceChunk(decoder *xml.Decoder, start xml.StartElement) (string, TextureUpload, error) {
	var upload TextureUpload
	var resourceID string
	haveDstResource := false
	depth := 1

	for depth > 0 {
		token, err := decoder.Token()
		if err != nil {
			return "", upload, fmt.Errorf("read updatesubresource: %w", err)
		}
		switch typed := token.(type) {
		case xml.StartElement:
			depth++
			switch typed.Name.Local {
			case "ResourceId":
				value, readErr := readTextElement(decoder)
				if readErr != nil {
					return "", upload, readErr
				}
				depth--
				if attr(typed, "name") == "pDstResource" && !haveDstResource {
					resourceID = strings.TrimSpace(value)
					haveDstResource = true
				}
			case "uint":
				name := attr(typed, "name")
				value, readErr := readIntElement(decoder)
				if readErr != nil {
					return "", upload, readErr
				}
				depth--
				switch name {
				case "DstSubresource":
					upload.Subresource = value
				case "SrcRowPitch":
					upload.SrcRowPitch = value
				}
			case "buffer":
				if attr(typed, "name") == "Contents" {
					if byteLen := attr(typed, "byteLength"); byteLen != "" {
						if parsed, err := strconvAtoi(byteLen); err == nil {
							upload.ByteLength = parsed
						}
					}
					value, readErr := readTextElement(decoder)
					if readErr != nil {
						return "", upload, readErr
					}
					depth--
					upload.BufferID = strings.TrimSpace(value)
				} else if skipErr := skipElement(decoder); skipErr != nil {
					return "", upload, skipErr
				} else {
					depth--
				}
			}
		case xml.EndElement:
			depth--
		}
	}
	return resourceID, upload, nil
}

// parseCreateTexture2DChunk walks the nested <struct>/<uint>/<enum> elements
// inside one CreateTexture2D chunk and returns a populated TextureInfo.
func parseCreateTexture2DChunk(decoder *xml.Decoder, start xml.StartElement) (*TextureInfo, error) {
	info := &TextureInfo{}
	depth := 1 // we're already inside <chunk>

	for depth > 0 {
		token, err := decoder.Token()
		if err != nil {
			return nil, fmt.Errorf("read texture chunk: %w", err)
		}
		switch typed := token.(type) {
		case xml.StartElement:
			depth++
			switch typed.Name.Local {
			case "uint":
				name := attr(typed, "name")
				value, readErr := readIntElement(decoder)
				if readErr != nil {
					return nil, readErr
				}
				switch name {
				case "Width":
					info.Width = value
				case "Height":
					info.Height = value
				case "MipLevels":
					info.MipLevels = value
				case "ArraySize":
					info.ArraySize = value
				}
				depth--
			case "enum":
				name := attr(typed, "name")
				str := attr(typed, "string")
				// Consume inner value + end token.
				if skipErr := skipElement(decoder); skipErr != nil {
					return nil, skipErr
				}
				depth--
				switch name {
				case "Format":
					info.Format = str
				case "Usage":
					info.Usage = str
				case "BindFlags":
					info.BindFlags = str
				case "MiscFlags":
					info.MiscFlags = str
				}
			case "ResourceId":
				if attr(typed, "name") == "pTexture" {
					value, readErr := readTextElement(decoder)
					if readErr != nil {
						return nil, readErr
					}
					info.ResourceID = strings.TrimSpace(value)
				} else if skipErr := skipElement(decoder); skipErr != nil {
					return nil, skipErr
				}
				depth--
			}
		case xml.EndElement:
			depth--
			if depth == 0 && typed.Name.Local == start.Name.Local {
				// Finished this chunk.
			}
		}
	}

	if info.Width == 0 || info.Height == 0 || info.Format == "" {
		return nil, nil
	}
	if info.ArraySize == 0 {
		info.ArraySize = 1
	}
	info.ShortFormat = shortFormatName(info.Format)
	info.IsBCFormat = isBlockCompressed(info.Format)
	bpp, known := formatBytesPerPixel(info.Format)
	info.IsUnknownFmt = !known
	if known {
		info.Bytes = computeTextureBytes(info.Width, info.Height, info.MipLevels, info.ArraySize, info.Format, bpp)
	}
	info.Category = categorize(info)
	return info, nil
}

// computeTextureBytes sums the byte cost of every mip level of every array
// slice. BC formats pad to 4x4 blocks, which matters for very small mips.
func computeTextureBytes(width, height, mipLevels, arraySize int, format string, bytesPerPixel float64) int64 {
	if mipLevels <= 0 {
		// MipLevels=0 in D3D11 means "full chain". Compute it.
		count := 1
		w, h := width, height
		for w > 1 || h > 1 {
			w = maxInt(1, w/2)
			h = maxInt(1, h/2)
			count++
		}
		mipLevels = count
	}

	var total float64
	w, h := width, height
	bc := isBlockCompressed(format)
	for i := 0; i < mipLevels; i++ {
		if bc {
			bw := maxInt(1, (w+3)/4)
			bh := maxInt(1, (h+3)/4)
			blockBytes := bytesPerPixel * 16 // 4x4 block
			total += float64(bw*bh) * blockBytes
		} else {
			total += float64(w*h) * bytesPerPixel
		}
		w = maxInt(1, w/2)
		h = maxInt(1, h/2)
	}
	return int64(math.Round(total * float64(arraySize)))
}

func categorize(info *TextureInfo) TextureCategory {
	if info.IsUnknownFmt {
		return CategoryUnknown
	}
	misc := info.MiscFlags
	bind := info.BindFlags

	if strings.Contains(misc, "TEXTURECUBE") {
		return CategoryCubemap
	}
	if strings.Contains(bind, "DEPTH_STENCIL") {
		return CategoryDepthTgt
	}
	if strings.Contains(bind, "RENDER_TARGET") && !info.IsBCFormat {
		return CategoryRenderTgt
	}
	if info.IsBCFormat && strings.Contains(bind, "SHADER_RESOURCE") {
		if bcFormatHasAlpha(info.Format) {
			return CategoryAssetAlpha
		}
		return CategoryAssetOpaque
	}
	if !info.IsBCFormat && strings.Contains(bind, "SHADER_RESOURCE") && info.Width >= 64 && info.Height >= 64 {
		return CategoryAssetRaw
	}
	return CategorySmallUtil
}

// parseDriverInit extracts the adapter name (e.g. "AMD Radeon RX 7900 XT") and
// the driver string (e.g. "D3D11") from the first driver-init chunk.
func parseDriverInit(decoder *xml.Decoder, start xml.StartElement) (string, string) {
	depth := 1
	adapter := ""
	for depth > 0 {
		token, err := decoder.Token()
		if err != nil {
			return adapter, ""
		}
		switch typed := token.(type) {
		case xml.StartElement:
			depth++
			if typed.Name.Local == "string" && attr(typed, "name") == "Description" {
				value, readErr := readTextElement(decoder)
				if readErr == nil {
					adapter = strings.TrimSpace(value)
				}
				depth--
			}
		case xml.EndElement:
			depth--
			if depth == 0 && typed.Name.Local == start.Name.Local {
				return adapter, ""
			}
		}
	}
	return adapter, ""
}

func attr(start xml.StartElement, name string) string {
	for _, a := range start.Attr {
		if a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}

func readIntElement(decoder *xml.Decoder) (int, error) {
	text, err := readTextElement(decoder)
	if err != nil {
		return 0, err
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return 0, nil
	}
	var value int
	if _, err := fmt.Sscanf(text, "%d", &value); err != nil {
		return 0, fmt.Errorf("parse int %q: %w", text, err)
	}
	return value, nil
}

func readTextElement(decoder *xml.Decoder) (string, error) {
	var builder strings.Builder
	depth := 1
	for depth > 0 {
		token, err := decoder.Token()
		if err != nil {
			return "", err
		}
		switch typed := token.(type) {
		case xml.CharData:
			builder.Write(typed)
		case xml.StartElement:
			depth++
		case xml.EndElement:
			depth--
		}
	}
	return builder.String(), nil
}

func skipElement(decoder *xml.Decoder) error {
	depth := 1
	for depth > 0 {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		switch token.(type) {
		case xml.StartElement:
			depth++
		case xml.EndElement:
			depth--
		}
	}
	return nil
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// strconvAtoi is a tiny wrapper so parse.go stays import-light. We use it in
// hot-path attribute parsing where performance matters more than fmt.Sscanf's
// flexibility.
func strconvAtoi(s string) (int, error) {
	value := 0
	negative := false
	if len(s) == 0 {
		return 0, fmt.Errorf("empty string")
	}
	start := 0
	if s[0] == '-' {
		negative = true
		start = 1
	}
	for i := start; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("non-digit %q", c)
		}
		value = value*10 + int(c-'0')
	}
	if negative {
		value = -value
	}
	return value, nil
}
