package report

// Normalized SurfaceAppearance property names. Use these instead of
// hand-typing the strings in switch statements.
const (
	SAPropertyColor     = "colormapcontent"
	SAPropertyNormal    = "normalmapcontent"
	SAPropertyMetalness = "metalnessmapcontent"
	SAPropertyRoughness = "roughnessmapcontent"
)

// SurfaceAppearanceMaterialSlots records per-slot source dimensions for
// one SurfaceAppearance instance. A slot with empty AssetKey or
// non-positive dimensions means it wasn't authored for that material.
// AssetKey is the dedup identifier (typically extractor.AssetReferenceKey
// output) — needed so the correction can recognise the same
// color/normal/M/R asset shared across many materials.
type SurfaceAppearanceMaterialSlot struct {
	AssetKey   string
	Width      int
	Height     int
	PixelCount int64
}

type SurfaceAppearanceMaterialSlots struct {
	Color     SurfaceAppearanceMaterialSlot
	Normal    SurfaceAppearanceMaterialSlot
	Metalness SurfaceAppearanceMaterialSlot
	Roughness SurfaceAppearanceMaterialSlot
}

// TryAssignByProperty stores slot in the field matching
// normalizedProperty if that slot is empty. Returns false (and no
// assignment) when the slot was already filled — the caller can treat
// that as a "new material on the same path" boundary. Unrecognised
// properties are silently ignored and return true (nothing to rotate).
func (s *SurfaceAppearanceMaterialSlots) TryAssignByProperty(normalizedProperty string, slot SurfaceAppearanceMaterialSlot) bool {
	target := s.slotPointer(normalizedProperty)
	if target == nil {
		return true
	}
	if target.AssetKey != "" {
		return false
	}
	*target = slot
	return true
}

// bumpDimsToLarger overwrites target's dimensions (Width, Height,
// PixelCount) with candidate's when candidate has more pixels. AssetKey
// is left untouched — useful when the target's identity is meaningful
// (e.g. an MR pack keyed to its normal asset whose dimensions need to
// stretch to fit a larger authored M or R).
func bumpDimsToLarger(target *SurfaceAppearanceMaterialSlot, candidate SurfaceAppearanceMaterialSlot) {
	if candidate.PixelCount > target.PixelCount {
		target.Width = candidate.Width
		target.Height = candidate.Height
		target.PixelCount = candidate.PixelCount
	}
}

// ApplyDeltaClamped adds delta to *target, clamping the result at zero.
func ApplyDeltaClamped(target *int64, delta int64) {
	*target += delta
	if *target < 0 {
		*target = 0
	}
}

func (s *SurfaceAppearanceMaterialSlots) slotPointer(normalizedProperty string) *SurfaceAppearanceMaterialSlot {
	switch normalizedProperty {
	case SAPropertyColor:
		return &s.Color
	case SAPropertyNormal:
		return &s.Normal
	case SAPropertyMetalness:
		return &s.Metalness
	case SAPropertyRoughness:
		return &s.Roughness
	}
	return nil
}

// SurfaceAppearanceMemoryCorrectionDelta captures the BC1/BC3 pixel/byte
// adjustment computed by ComputeSurfaceAppearanceMemoryCorrection — the
// caller applies it to whichever counters they hold (Summary or
// heatmap.Totals).
type SurfaceAppearanceMemoryCorrectionDelta struct {
	BlankMRGroupCount          int
	CustomMRGroupCount         int
	AddedMRPackPixels          int64
	AddedMRPackBytes           int64
	SubtractedStandalonePixels int64
	SubtractedStandaloneBytes  int64
	UpscaledNormalCount        int
	AddedNormalUpscalePixels   int64
	AddedNormalUpscaleBytes    int64
}

func (d SurfaceAppearanceMemoryCorrectionDelta) NetBC1Pixels() int64 {
	return d.AddedMRPackPixels - d.SubtractedStandalonePixels
}

func (d SurfaceAppearanceMemoryCorrectionDelta) NetBC1Bytes() int64 {
	return d.AddedMRPackBytes - d.SubtractedStandaloneBytes
}

func (d SurfaceAppearanceMemoryCorrectionDelta) NetBC3Pixels() int64 {
	return d.AddedNormalUpscalePixels
}

func (d SurfaceAppearanceMemoryCorrectionDelta) NetBC3Bytes() int64 {
	return d.AddedNormalUpscaleBytes
}

// ComputeSurfaceAppearanceMemoryCorrection works out how to reconcile a
// raw per-asset BC1 tally with what the Roblox engine actually allocates
// for SurfaceAppearance materials. Engine behaviour, learned from a
// RenderDoc capture cross-checked against the rbxl reference set:
//
//   - The engine packs Metalness + Roughness + (auto-derived roughness
//     fill when neither is authored) into a SINGLE BC1 tile per unique
//     NormalMap asset (or ColorMap asset, if the SurfaceAppearance has
//     no normal slot). MR pack size = max(base, any authored M source,
//     any authored R source) across materials sharing the normal — so
//     a 2048² roughness on a 512² normal produces a 2048² MR pack.
//     Color does NOT factor in. The fallback (when neither M nor R is
//     authored) is derived from the normal's surface variation — visible
//     as faint cavity/edge structure in the G channel of "Blank MR"
//     textures. Materials sharing a normal map share that pack — one
//     BC1 allocation, not one per material instance.
//   - Standalone Metalness or Roughness assets are NOT uploaded as their
//     own GPU textures; their pixels live inside the per-normal-map MR
//     pack. The raw tally still counts them (the rbxl references them
//     as separate asset IDs), so we subtract them out.
//   - Normal maps are upscaled at upload time to match their paired
//     color map when the normal source is smaller — i.e. effective
//     normal size = max(normal_source, paired_color). Color maps are
//     never resized to match their normal. The raw BC3 tally uses the
//     normal's source size, so we add the (upscaled − source) bytes
//     per unique normal asset (taking the largest paired color across
//     materials sharing the normal).
//
// Net effect on a typical map: +N MR-pack BC1s (one per unique normal
// map) − M unique-M-asset BC1s − R unique-R-asset BC1s + normal-upscale
// BC3 bytes for any normal smaller than its paired color.
//
// On Batcave (10 unique normal maps, 1 unique R, 0 unique M, all 1024²):
// adds ~6.7 MB MR packs, subtracts ~0.67 MB standalone R → +6.0 MB BC1
// net, landing the headline near RenderDoc's measured 26.89 MB.
func ComputeSurfaceAppearanceMemoryCorrection(materials map[string]SurfaceAppearanceMaterialSlots) SurfaceAppearanceMemoryCorrectionDelta {
	delta := SurfaceAppearanceMemoryCorrectionDelta{}
	type group struct {
		keySlot SurfaceAppearanceMaterialSlot
		hasMOrR bool
	}
	type normalUpscale struct {
		source         SurfaceAppearanceMaterialSlot
		maxPairedColor SurfaceAppearanceMaterialSlot
	}
	groups := map[string]*group{}
	uniqueMSlots := map[string]SurfaceAppearanceMaterialSlot{}
	uniqueRSlots := map[string]SurfaceAppearanceMaterialSlot{}
	normalUpscales := map[string]*normalUpscale{}
	for _, slots := range materials {
		// Fall back to the color map if the SurfaceAppearance has no
		// normal slot — the engine still allocates an MR pack at color
		// resolution in that configuration.
		keySlot := slots.Normal
		if keySlot.AssetKey == "" || keySlot.PixelCount <= 0 {
			keySlot = slots.Color
		}
		if keySlot.AssetKey == "" || keySlot.PixelCount <= 0 {
			continue
		}
		g, ok := groups[keySlot.AssetKey]
		if !ok {
			g = &group{keySlot: keySlot}
			groups[keySlot.AssetKey] = g
		}
		// MR pack is sized to fit the largest authored slot among the
		// materials sharing this normal: max(base, any M source, any R
		// source). The AssetKey stays the normal's — only dimensions
		// are bumped.
		bumpDimsToLarger(&g.keySlot, slots.Metalness)
		bumpDimsToLarger(&g.keySlot, slots.Roughness)
		if slots.Metalness.PixelCount > 0 || slots.Roughness.PixelCount > 0 {
			g.hasMOrR = true
		}
		if slots.Metalness.AssetKey != "" && slots.Metalness.PixelCount > 0 {
			uniqueMSlots[slots.Metalness.AssetKey] = slots.Metalness
		}
		if slots.Roughness.AssetKey != "" && slots.Roughness.PixelCount > 0 {
			uniqueRSlots[slots.Roughness.AssetKey] = slots.Roughness
		}
		if slots.Normal.AssetKey != "" && slots.Normal.PixelCount > 0 {
			nu, hadNU := normalUpscales[slots.Normal.AssetKey]
			if !hadNU {
				nu = &normalUpscale{source: slots.Normal}
				normalUpscales[slots.Normal.AssetKey] = nu
			}
			bumpDimsToLarger(&nu.maxPairedColor, slots.Color)
		}
	}
	for _, g := range groups {
		bytes := EstimateGPUTextureBytesExact(g.keySlot.Width, g.keySlot.Height, false)
		delta.AddedMRPackPixels += g.keySlot.PixelCount
		delta.AddedMRPackBytes += bytes
		if g.hasMOrR {
			delta.CustomMRGroupCount++
		} else {
			delta.BlankMRGroupCount++
		}
	}
	for _, standalone := range []map[string]SurfaceAppearanceMaterialSlot{uniqueMSlots, uniqueRSlots} {
		for _, s := range standalone {
			delta.SubtractedStandalonePixels += s.PixelCount
			delta.SubtractedStandaloneBytes += EstimateGPUTextureBytesExact(s.Width, s.Height, false)
		}
	}
	for _, nu := range normalUpscales {
		if nu.maxPairedColor.PixelCount <= nu.source.PixelCount {
			continue
		}
		sourceBytes := EstimateGPUTextureBytesExact(nu.source.Width, nu.source.Height, true)
		upscaledBytes := EstimateGPUTextureBytesExact(nu.maxPairedColor.Width, nu.maxPairedColor.Height, true)
		delta.AddedNormalUpscalePixels += nu.maxPairedColor.PixelCount - nu.source.PixelCount
		delta.AddedNormalUpscaleBytes += upscaledBytes - sourceBytes
		delta.UpscaledNormalCount++
	}
	return delta
}

type SurfaceAppearanceMemoryCorrectionSummary struct {
	SurfaceAppearanceMemoryCorrectionDelta
	PreCorrectionBC1Pixels  int64
	PostCorrectionBC1Pixels int64
	PreCorrectionBC1Bytes   int64
	PostCorrectionBC1Bytes  int64
	PreCorrectionBC3Pixels  int64
	PostCorrectionBC3Pixels int64
	PreCorrectionBC3Bytes   int64
	PostCorrectionBC3Bytes  int64
}

func ApplySurfaceAppearanceMemoryCorrections(summary *Summary, materials map[string]SurfaceAppearanceMaterialSlots) SurfaceAppearanceMemoryCorrectionSummary {
	out := SurfaceAppearanceMemoryCorrectionSummary{}
	if summary == nil {
		return out
	}
	out.PreCorrectionBC1Pixels = summary.BC1PixelCount
	out.PreCorrectionBC1Bytes = summary.BC1BytesExact
	out.PreCorrectionBC3Pixels = summary.BC3PixelCount
	out.PreCorrectionBC3Bytes = summary.BC3BytesExact
	delta := ComputeSurfaceAppearanceMemoryCorrection(materials)
	out.SurfaceAppearanceMemoryCorrectionDelta = delta
	ApplyDeltaClamped(&summary.BC1PixelCount, delta.NetBC1Pixels())
	ApplyDeltaClamped(&summary.BC1BytesExact, delta.NetBC1Bytes())
	ApplyDeltaClamped(&summary.BC3PixelCount, delta.NetBC3Pixels())
	ApplyDeltaClamped(&summary.BC3BytesExact, delta.NetBC3Bytes())
	out.PostCorrectionBC1Pixels = summary.BC1PixelCount
	out.PostCorrectionBC1Bytes = summary.BC1BytesExact
	out.PostCorrectionBC3Pixels = summary.BC3PixelCount
	out.PostCorrectionBC3Bytes = summary.BC3BytesExact
	return out
}
