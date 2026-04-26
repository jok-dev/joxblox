package renderdoc

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	"runtime"
	"sync"

	xdraw "golang.org/x/image/draw"
)

// pixelHashHexChars is the truncated SHA-256 hex length used for texture
// content hashes — 16 hex chars = 64 bits of entropy, plenty to eyeball
// identity across a capture that typically has < 1000 textures.
const pixelHashHexChars = 16

// HashImagePixels returns a short hex hash of the decoded pixel data so
// duplicate textures (same content, different resource IDs) can be spotted
// by eye and filtered by content.
func HashImagePixels(img image.Image) string {
	hasher := sha256.New()
	if nrgba, ok := img.(*image.NRGBA); ok {
		hasher.Write(nrgba.Pix)
	} else {
		bounds := img.Bounds()
		rowBytes := make([]byte, bounds.Dx()*4)
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				cr, cg, cb, ca := img.At(x, y).RGBA()
				offset := (x - bounds.Min.X) * 4
				rowBytes[offset+0] = uint8(cr >> 8)
				rowBytes[offset+1] = uint8(cg >> 8)
				rowBytes[offset+2] = uint8(cb >> 8)
				rowBytes[offset+3] = uint8(ca >> 8)
			}
			hasher.Write(rowBytes)
		}
	}
	return hex.EncodeToString(hasher.Sum(nil))[:pixelHashHexChars]
}

// ComputeTextureHashes decodes every asset-category texture in the report in
// parallel and populates PixelHash on each. Other categories (render
// targets, cubemaps, small/utility, depth/stencil) are skipped since they
// never need a content hash in practice. Safe to call once after load; no-op
// if store is nil. onProgress is invoked from worker goroutines with the
// count of textures hashed so far — use fyne.Do from the caller's side if
// updating UI.
func ComputeTextureHashes(report *Report, store *BufferStore, onProgress func(done, total int)) {
	if report == nil || store == nil {
		return
	}

	// Collect indexes of textures worth hashing; everything else is cheap to
	// leave with an empty PixelHash.
	workIndexes := make([]int, 0, len(report.Textures))
	for i, texture := range report.Textures {
		if !isHashableCategory(texture.Category) {
			continue
		}
		if len(texture.Uploads) == 0 {
			continue
		}
		workIndexes = append(workIndexes, i)
	}
	total := len(workIndexes)
	if total == 0 {
		return
	}

	workerCount := runtime.NumCPU() / 2
	if workerCount < 1 {
		workerCount = 1
	}
	if workerCount > 8 {
		workerCount = 8
	}
	if workerCount > total {
		workerCount = total
	}

	jobs := make(chan int, total)
	var wg sync.WaitGroup
	var doneCounter int
	var doneMu sync.Mutex

	for w := 0; w < workerCount; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range jobs {
				texture := report.Textures[idx]
				img, err := DecodeTexturePreview(texture, store)
				if err == nil && img != nil {
					report.Textures[idx].PixelHash = HashImagePixels(img)
				if dHash, dHashErr := computeImageDHash(img); dHashErr == nil {
					report.Textures[idx].DHash = dHash
				}
					// Content-based re-tagging. Patterns we recognise:
					//   - DXT5nm-swizzled normals (BC3 with R=255 and B=0
					//     uniform across the image).
					//   - MR-packed textures (BC1 with B strictly 0 — the
					//     Roblox SurfaceAppearance layout: R=metalness,
					//     G=roughness, B unused). Further split into:
					//       · Blank MR — solid R=0, G=~128 (engine fallback
					//         when no MR was uploaded)
					//       · Custom MR — anything else with B=0 (developer
					//         authored at least one of M or R)
					// ApplyBuiltinHashes runs later and can still override
					// with a hash-specific category if needed.
					if isBC3Format(texture.Format) && detectDXT5nm(img) {
						report.Textures[idx].Category = CategoryNormalDXT5nm
					} else if detectBPackedMR(img) {
						if isBlankMRFill(img) {
							report.Textures[idx].Category = CategoryBlankMR
						} else {
							report.Textures[idx].Category = CategoryCustomMR
						}
					}
				}
				if onProgress != nil {
					doneMu.Lock()
					doneCounter++
					current := doneCounter
					doneMu.Unlock()
					onProgress(current, total)
				}
			}
		}()
	}

	for _, idx := range workIndexes {
		jobs <- idx
	}
	close(jobs)
	wg.Wait()
}

func isHashableCategory(category TextureCategory) bool {
	switch category {
	case CategoryAssetOpaque, CategoryAssetAlpha, CategoryAssetRaw:
		return true
	}
	return false
}

func isBC3Format(format string) bool {
	switch format {
	case "DXGI_FORMAT_BC3_UNORM", "DXGI_FORMAT_BC3_UNORM_SRGB", "DXGI_FORMAT_BC3_TYPELESS":
		return true
	}
	return false
}

// detectDXT5nm reports whether the decoded image matches the DXT5nm normal
// encoding signature: R channel uniformly high (forced to 255 at encode time)
// and B channel uniformly low (forced to 0). Only valid for NRGBA images,
// which is what the BC3 decoder always produces.
func detectDXT5nm(img image.Image) bool {
	nrgba, ok := img.(*image.NRGBA)
	if !ok {
		return false
	}
	for i := 0; i < len(nrgba.Pix); i += 4 {
		if nrgba.Pix[i+0] < dxt5nmRedMinByte {
			return false
		}
		if nrgba.Pix[i+2] > dxt5nmBlueMaxByte {
			return false
		}
	}
	return true
}

// DefaultRobloxBuiltinHashes maps the 16-char SHA-256 prefix of known
// built-in Roblox textures (ones that appear in every capture regardless of
// scene content) to the specific category they should be re-tagged as.
// This is curated empirically by capturing different scenes and comparing
// hashes; extend as more defaults are positively identified.
var DefaultRobloxBuiltinHashes = map[string]TextureCategory{
	// 256x256 R16G16_FLOAT specular BRDF integration LUT (split-sum
	// approximation, Karis 2013). Roblox's PBR shader loads this once at
	// startup regardless of scene content.
	"1891c45789f65637": CategoryBuiltinBRDFLUT,
	// Known defaults we haven't positively identified yet — likely default
	// face textures, placeholder atlases, or other system assets. Bucketed
	// generically until we know what each is.
	"e985d8ef07ff53e6": CategoryBuiltin,
	"d05eadbee115f553": CategoryBuiltin,
	"4ef9f217986a42f1": CategoryBuiltin,
	"d1ca9cf08f9948e2": CategoryBuiltin,
	"9f6f8ad2ce17c02d": CategoryBuiltin,
	"c012b01594693422": CategoryBuiltin,
	"0eb5a0647bf660ba": CategoryBuiltin,
	"82db2db8b5428749": CategoryBuiltin,
	"57b7080aebff8f8e": CategoryBuiltin,
	"ea1dea91141b50e8": CategoryBuiltin,
	// Reported by user; likely a default skybox face.
	"cc2d1541a7f1f115": CategoryBuiltin,
}

// Thresholds for detecting DXT5nm-swizzled normals from a decoded base mip.
// The encoding forces R=255 and B=0 on every pixel; we allow a small
// tolerance to absorb BC block quantisation noise at mip boundaries.
const (
	dxt5nmRedMinByte  = 250 // R must be at least this high on every pixel
	dxt5nmBlueMaxByte = 8   // B must be at most this low on every pixel
)

// Thresholds for detecting the Roblox MR-packed texture layout. A BC1
// texture with B strictly near-zero on every pixel is the signature of
// the SurfaceAppearance MR pack (R=metalness, G=roughness, B unused).
// Authored color maps almost always have non-zero blue on some pixels,
// so this is a reliable discriminator.
const (
	mrPackBlueMaxByte = 4 // strict per-pixel bound on B
	// Blank-fill sub-check: when the MR pack is the engine fallback
	// rather than authored, R is strictly 0 and G is uniformly ~128.
	// BC1 quantisation of a G=128 midpoint produces isolated speckles
	// up to ~230 at block boundaries, so we bound G by mean only.
	blankMRRedMaxByte      = 4
	blankMRMeanGreenMinInt = 120
	blankMRMeanGreenMaxInt = 136
)

// detectBPackedMR reports whether the decoded image has B strictly near
// zero on every pixel — the signature of a Roblox MR-packed BC1 tile
// (R=metalness, G=roughness, B=0). Both blank and authored MR maps match.
// Only valid for NRGBA images.
func detectBPackedMR(img image.Image) bool {
	nrgba, ok := img.(*image.NRGBA)
	if !ok {
		return false
	}
	if len(nrgba.Pix) == 0 {
		return false
	}
	for i := 0; i < len(nrgba.Pix); i += 4 {
		if nrgba.Pix[i+2] > mrPackBlueMaxByte {
			return false
		}
	}
	return true
}

// isBlankMRFill reports whether the decoded image matches the engine's
// blank MR fallback: R strictly 0 and G uniformly ~128. Call only after
// detectBPackedMR has confirmed B=0; this exists to split blank fills
// from authored MR maps. Only valid for NRGBA images.
func isBlankMRFill(img image.Image) bool {
	nrgba, ok := img.(*image.NRGBA)
	if !ok {
		return false
	}
	if len(nrgba.Pix) == 0 {
		return false
	}
	var greenSum int64
	var pixelCount int64
	for i := 0; i < len(nrgba.Pix); i += 4 {
		if nrgba.Pix[i+0] > blankMRRedMaxByte {
			return false
		}
		greenSum += int64(nrgba.Pix[i+1])
		pixelCount++
	}
	meanGreen := greenSum / pixelCount
	return meanGreen >= blankMRMeanGreenMinInt && meanGreen <= blankMRMeanGreenMaxInt
}

// ApplyBuiltinHashes re-tags every texture whose PixelHash appears in the
// given map with the specific category mapped to that hash, and rebuilds
// report.ByCategory so the summary totals stay accurate. Call this after
// ComputeTextureHashes. Textures lacking a PixelHash (unhashed categories,
// decode failures) are left alone.
//
// Using a per-hash category lets the table show what each built-in is
// ("Specular BRDF LUT" etc.) rather than a generic "built-in" bucket.
func ApplyBuiltinHashes(report *Report, hashCategories map[string]TextureCategory) {
	if report == nil {
		return
	}
	for i := range report.Textures {
		hash := report.Textures[i].PixelHash
		if hash == "" {
			continue
		}
		if newCategory, ok := hashCategories[hash]; ok {
			report.Textures[i].Category = newCategory
		}
	}
	// Always rebuild ByCategory so categories set by earlier passes
	// (ComputeTextureHashes's DXT5nm detection, any future classifiers)
	// are correctly reflected in the summary counts and bytes.
	report.ByCategory = map[TextureCategory]CategoryAggregate{}
	for _, texture := range report.Textures {
		agg := report.ByCategory[texture.Category]
		agg.Count++
		agg.Bytes += texture.Bytes
		report.ByCategory[texture.Category] = agg
	}
}

// computeImageDHash returns a 64-bit perceptual hash of img using the same
// 9×8 grayscale-difference algorithm as loader.ComputeImageDHash, but
// taking an already-decoded image so we don't waste a second decode pass.
// Returns 0 + error when the image is too small to hash meaningfully.
func computeImageDHash(img image.Image) (uint64, error) {
	const dHashWidth = 9
	const dHashHeight = 8
	bounds := img.Bounds()
	if bounds.Dx() < dHashWidth || bounds.Dy() < dHashHeight {
		return 0, fmt.Errorf("image too small for dHash (%dx%d)", bounds.Dx(), bounds.Dy())
	}
	resized := image.NewGray(image.Rect(0, 0, dHashWidth, dHashHeight))
	xdraw.BiLinear.Scale(resized, resized.Bounds(), img, bounds, xdraw.Over, nil)

	var hash uint64
	bitIndex := 0
	for y := 0; y < dHashHeight; y++ {
		for x := 0; x < dHashWidth-1; x++ {
			left := resized.GrayAt(x, y).Y
			right := resized.GrayAt(x+1, y).Y
			if left < right {
				hash |= 1 << bitIndex
			}
			bitIndex++
		}
	}
	return hash, nil
}
