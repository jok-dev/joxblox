package report

import (
	"sort"
	"strings"

	"joxblox/internal/app/loader"
	"joxblox/internal/extractor"
)

// ScanMaterialEntry summarises one engine-deduplicated PBR material — i.e.
// one unique (color, normal, metalness, roughness) asset combo — with the
// dimensions, asset IDs, and per-slot effective sizes that drive the
// engine's actual GPU footprint. Used to feed the Materials sub-tab in the
// scan view so users can see what the engine *uploads* (color + upscaled
// normal + MR pack), not just what each asset's authored size is.
type ScanMaterialEntry struct {
	// InstancePath is one example SurfaceAppearance path that uses this
	// combo — the lex-min when many instances share the bundle.
	InstancePath string
	// InstanceCount is how many SurfaceAppearance instances share this
	// asset bundle. Fixing one asset fixes every one of these usages.
	InstanceCount int

	ColorAssetID     int64
	NormalAssetID    int64
	MetalnessAssetID int64
	RoughnessAssetID int64

	// Authored slot sizes — zero when the slot wasn't authored.
	ColorWidth      int
	ColorHeight     int
	NormalWidth     int
	NormalHeight    int
	MetalnessWidth  int
	MetalnessHeight int
	RoughnessWidth  int
	RoughnessHeight int

	// EffectiveNormalWidth/Height = max(normal_source, max_paired_color).
	// The engine upscales a smaller normal at upload time, so this is the
	// size BC3 actually allocates for. Zero when there's no normal slot.
	EffectiveNormalWidth  int
	EffectiveNormalHeight int

	// MRPackWidth/Height = max(group key, any M source, any R source). The
	// engine packs Metalness + Roughness (+ derived fill when neither is
	// authored) into one BC1 tile per unique normal asset (or per color
	// when there's no normal). Zero when no MR pack is allocated.
	MRPackWidth  int
	MRPackHeight int

	// Per-slot GPU bytes the engine actually allocates for this material's
	// share — color at source size (BC1 / BC3 by alpha is approximated as
	// BC1 here since scan rows hold per-asset alpha info, not per-combo),
	// normal at the upscaled size (BC3), MR pack at pack size (BC1).
	// MR-pack and normal-upscale bytes only count once per unique asset
	// across the whole material list (see TotalGPUBytes for the headline).
	ColorBytes  int64
	NormalBytes int64
	MRPackBytes int64

	// Mismatched is true when the authored slots aren't all at the same
	// resolution — same definition as the report's Mismatched PBR Maps
	// grade.
	Mismatched bool
}

// CollectScanMaterialEntries groups SurfaceAppearance scan rows into one
// entry per unique (color, normal, M, R) asset combo and computes the
// engine-model effective sizes + per-slot GPU bytes for each. Sorted by
// total per-combo GPU footprint desc, then InstanceCount desc, then
// InstancePath asc — so the heaviest materials surface first.
func CollectScanMaterialEntries(rows []loader.ScanResult) []ScanMaterialEntry {
	entries, _ := CollectScanMaterialReport(rows)
	return entries
}

// CollectScanMaterialReport returns the per-combo entries together with
// the dedup-correct headline GPU total in a single pass over rows /
// materials. Hot-path callers (the Materials sub-tab) prefer this over
// CollectScanMaterialEntries + TotalScanMaterialGPUBytes because those
// each rebuild the materials map and the per-asset metadata index.
func CollectScanMaterialReport(rows []loader.ScanResult) ([]ScanMaterialEntry, int64) {
	if len(rows) == 0 {
		return nil, 0
	}
	materials := buildScanResultMaterialsMap(rows)
	if len(materials) == 0 {
		return nil, 0
	}

	// Per-asset alpha/property info so we can pick BC1 vs BC3 for the color
	// slot — same alpha rules as ScanResultGPUMemoryBytes.
	type assetMeta struct {
		assetID              int64
		hasAlphaChannel      bool
		nonOpaqueAlphaPixels int64
		propertyName         string
	}
	metaByKey := map[string]assetMeta{}
	for _, row := range rows {
		instancePath := strings.TrimSpace(row.InstancePath)
		if instancePath == "" {
			continue
		}
		normalizedProperty := strings.ToLower(strings.TrimSpace(row.PropertyName))
		if !IsSurfaceAppearanceProperty(normalizedProperty, row.InstanceType) {
			continue
		}
		key := extractor.AssetReferenceKey(row.AssetID, row.AssetInput)
		if _, exists := metaByKey[key]; !exists {
			metaByKey[key] = assetMeta{
				assetID:              row.AssetID,
				hasAlphaChannel:      row.HasAlphaChannel,
				nonOpaqueAlphaPixels: row.NonOpaqueAlphaPixels,
				propertyName:         row.PropertyName,
			}
		}
	}

	// Build per-pack-key MR pack effective dims (max of group key, any M,
	// any R across SAs sharing the key) — same engine model as
	// ComputeSurfaceAppearanceMemoryCorrection.
	type dims struct {
		width, height int
		pixels        int64
	}
	type normalEntry struct {
		source         dims
		maxPairedColor dims
	}
	normalsByKey := map[string]*normalEntry{}
	packsByKey := map[string]*dims{}
	for _, slots := range materials {
		if slots.Normal.AssetKey != "" && slots.Normal.PixelCount > 0 {
			entry, ok := normalsByKey[slots.Normal.AssetKey]
			if !ok {
				entry = &normalEntry{source: dims{slots.Normal.Width, slots.Normal.Height, slots.Normal.PixelCount}}
				normalsByKey[slots.Normal.AssetKey] = entry
			}
			if slots.Normal.PixelCount > entry.source.pixels {
				entry.source = dims{slots.Normal.Width, slots.Normal.Height, slots.Normal.PixelCount}
			}
			if slots.Color.AssetKey != "" && slots.Color.PixelCount > entry.maxPairedColor.pixels {
				entry.maxPairedColor = dims{slots.Color.Width, slots.Color.Height, slots.Color.PixelCount}
			}
		}
		groupKey := slots.Normal.AssetKey
		groupSlot := slots.Normal
		if groupKey == "" || groupSlot.PixelCount <= 0 {
			groupKey = slots.Color.AssetKey
			groupSlot = slots.Color
		}
		if groupKey == "" || groupSlot.PixelCount <= 0 {
			continue
		}
		entry, ok := packsByKey[groupKey]
		if !ok {
			entry = &dims{groupSlot.Width, groupSlot.Height, groupSlot.PixelCount}
			packsByKey[groupKey] = entry
		}
		bumpDims := func(candidate SurfaceAppearanceMaterialSlot) {
			if candidate.PixelCount > entry.pixels {
				*entry = dims{candidate.Width, candidate.Height, candidate.PixelCount}
			}
		}
		bumpDims(groupSlot)
		bumpDims(slots.Metalness)
		bumpDims(slots.Roughness)
	}

	// Group SurfaceAppearance instances into unique combos.
	type bucket struct {
		entry        ScanMaterialEntry
		slots        SurfaceAppearanceMaterialSlots
		minPath      string
		instanceList []string
	}
	buckets := map[[4]string]*bucket{}
	for path, slots := range materials {
		key := [4]string{slots.Color.AssetKey, slots.Normal.AssetKey, slots.Metalness.AssetKey, slots.Roughness.AssetKey}
		if key == ([4]string{}) {
			continue
		}
		b, ok := buckets[key]
		if !ok {
			b = &bucket{
				entry: ScanMaterialEntry{
					ColorWidth:       slots.Color.Width,
					ColorHeight:      slots.Color.Height,
					NormalWidth:      slots.Normal.Width,
					NormalHeight:     slots.Normal.Height,
					MetalnessWidth:   slots.Metalness.Width,
					MetalnessHeight:  slots.Metalness.Height,
					RoughnessWidth:   slots.Roughness.Width,
					RoughnessHeight:  slots.Roughness.Height,
					ColorAssetID:     metaByKey[slots.Color.AssetKey].assetID,
					NormalAssetID:    metaByKey[slots.Normal.AssetKey].assetID,
					MetalnessAssetID: metaByKey[slots.Metalness.AssetKey].assetID,
					RoughnessAssetID: metaByKey[slots.Roughness.AssetKey].assetID,
					Mismatched:       isMismatchedPBRMaterial(slots),
				},
				slots:   slots,
				minPath: path,
			}
			buckets[key] = b
		}
		b.instanceList = append(b.instanceList, path)
		if path < b.minPath {
			b.minPath = path
		}
	}

	out := make([]ScanMaterialEntry, 0, len(buckets))
	for _, b := range buckets {
		entry := b.entry
		entry.InstancePath = b.minPath
		entry.InstanceCount = len(b.instanceList)

		// Color bytes — use the per-asset alpha info we recorded, then
		// fall back to BC1 when we couldn't find any (rare; means the row
		// wasn't loaded yet).
		if b.slots.Color.AssetKey != "" && b.slots.Color.PixelCount > 0 {
			meta := metaByKey[b.slots.Color.AssetKey]
			isBC3 := loader.ClassifyAsBC3(meta.hasAlphaChannel, meta.nonOpaqueAlphaPixels, meta.propertyName)
			entry.ColorBytes = EstimateGPUTextureBytesExact(b.slots.Color.Width, b.slots.Color.Height, isBC3)
		}

		// Normal bytes at engine-effective size (max(normal, max_paired_color)).
		if b.slots.Normal.AssetKey != "" && b.slots.Normal.PixelCount > 0 {
			normalDims := dims{b.slots.Normal.Width, b.slots.Normal.Height, b.slots.Normal.PixelCount}
			if entry, ok := normalsByKey[b.slots.Normal.AssetKey]; ok {
				if entry.maxPairedColor.pixels > entry.source.pixels {
					normalDims = entry.maxPairedColor
				} else {
					normalDims = entry.source
				}
			}
			entry.EffectiveNormalWidth = normalDims.width
			entry.EffectiveNormalHeight = normalDims.height
			entry.NormalBytes = EstimateGPUTextureBytesExact(normalDims.width, normalDims.height, true)
		}

		// MR pack bytes for the group keyed to this combo's normal (or
		// color, when no normal) — sized to max(group key, any M, any R)
		// across all SAs sharing the key.
		groupKey := b.slots.Normal.AssetKey
		if groupKey == "" || b.slots.Normal.PixelCount <= 0 {
			groupKey = b.slots.Color.AssetKey
		}
		if pack, ok := packsByKey[groupKey]; ok {
			entry.MRPackWidth = pack.width
			entry.MRPackHeight = pack.height
			entry.MRPackBytes = EstimateGPUTextureBytesExact(pack.width, pack.height, false)
		}

		out = append(out, entry)
	}

	sort.Slice(out, func(i, j int) bool {
		iTotal := out[i].ColorBytes + out[i].NormalBytes + out[i].MRPackBytes
		jTotal := out[j].ColorBytes + out[j].NormalBytes + out[j].MRPackBytes
		if iTotal != jTotal {
			return iTotal > jTotal
		}
		if out[i].InstanceCount != out[j].InstanceCount {
			return out[i].InstanceCount > out[j].InstanceCount
		}
		return out[i].InstancePath < out[j].InstancePath
	})

	// Headline GPU bytes — dedup-correct: each unique color asset counts
	// once at its largest size; each unique normal counts once at its
	// upscaled BC3 size; each unique MR-pack key counts one BC1 pack.
	uniqueColors := map[string]SurfaceAppearanceMaterialSlot{}
	for _, slots := range materials {
		if slots.Color.AssetKey == "" || slots.Color.PixelCount <= 0 {
			continue
		}
		if existing, ok := uniqueColors[slots.Color.AssetKey]; !ok || slots.Color.PixelCount > existing.PixelCount {
			uniqueColors[slots.Color.AssetKey] = slots.Color
		}
	}
	var headlineTotal int64
	for key, color := range uniqueColors {
		meta := metaByKey[key]
		isBC3 := loader.ClassifyAsBC3(meta.hasAlphaChannel, meta.nonOpaqueAlphaPixels, meta.propertyName)
		headlineTotal += EstimateGPUTextureBytesExact(color.Width, color.Height, isBC3)
	}
	for assetKey, normal := range normalsByKey {
		w, h := normal.source.width, normal.source.height
		if normal.maxPairedColor.pixels > normal.source.pixels {
			w, h = normal.maxPairedColor.width, normal.maxPairedColor.height
		}
		headlineTotal += EstimateGPUTextureBytesExact(w, h, true)
		_ = assetKey
	}
	for _, pack := range packsByKey {
		headlineTotal += EstimateGPUTextureBytesExact(pack.width, pack.height, false)
	}
	return out, headlineTotal
}

// TotalScanMaterialGPUBytes is the engine-model GPU footprint summed over
// the materials map underlying entries — dedup-correct: a normal asset
// shared across many materials counts once. Thin wrapper around
// CollectScanMaterialReport for callers that only want the headline.
func TotalScanMaterialGPUBytes(rows []loader.ScanResult) int64 {
	_, total := CollectScanMaterialReport(rows)
	return total
}

// buildScanResultMaterialsMap groups SurfaceAppearance texture rows by the
// SA's InstancePath into the slot bundle the engine model expects. First
// authored slot wins on collision — multiple SurfaceAppearance instances at
// the exact same path is rare enough that we accept losing the second one's
// dimensions rather than reproduce the report tab's #1/#2 path-rotation
// bookkeeping (which depends on a walk order ScanResults don't preserve).
func buildScanResultMaterialsMap(rows []loader.ScanResult) map[string]SurfaceAppearanceMaterialSlots {
	out := map[string]SurfaceAppearanceMaterialSlots{}
	for _, row := range rows {
		if row.PixelCount <= 0 || row.Width <= 0 || row.Height <= 0 {
			continue
		}
		instancePath := strings.TrimSpace(row.InstancePath)
		if instancePath == "" {
			continue
		}
		normalizedProperty := strings.ToLower(strings.TrimSpace(row.PropertyName))
		if !IsSurfaceAppearanceProperty(normalizedProperty, row.InstanceType) {
			continue
		}
		slots := out[instancePath]
		slot := SurfaceAppearanceMaterialSlot{
			AssetKey:   extractor.AssetReferenceKey(row.AssetID, row.AssetInput),
			Width:      row.Width,
			Height:     row.Height,
			PixelCount: row.PixelCount,
		}
		slots.TryAssignByProperty(normalizedProperty, slot)
		out[instancePath] = slots
	}
	return out
}
