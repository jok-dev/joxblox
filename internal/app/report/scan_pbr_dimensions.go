package report

import (
	"sort"
	"strings"

	"joxblox/internal/app/loader"
	"joxblox/internal/debug"
	"joxblox/internal/extractor"
	"joxblox/internal/format"
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

	// LooseImage is true when this entry isn't a SurfaceAppearance combo
	// — it's a single-asset Image referenced by a Decal, Texture,
	// ImageLabel, MeshPart.TextureID, etc. Only the Color slot is
	// populated (NormalAssetID / MetalnessAssetID / RoughnessAssetID
	// stay zero, NormalBytes / MRPackBytes stay zero). Surfaced in the
	// Materials sub-tab so users see every image-bearing asset in one
	// place; not used by the report-tab grades.
	LooseImage bool
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
	var colorBytes, colorBC3Bytes int64
	colorBC3Count := 0
	for key, color := range uniqueColors {
		meta := metaByKey[key]
		isBC3 := loader.ClassifyAsBC3(meta.hasAlphaChannel, meta.nonOpaqueAlphaPixels, meta.propertyName)
		bytes := EstimateGPUTextureBytesExact(color.Width, color.Height, isBC3)
		colorBytes += bytes
		if isBC3 {
			colorBC3Bytes += bytes
			colorBC3Count++
		}
		headlineTotal += bytes
		debug.Logf("[Materials-asset] kind=color key=%q id=%d isBC3=%t %dx%d → %s",
			key, meta.assetID, isBC3, color.Width, color.Height, format.FormatSizeAuto64(bytes))
	}
	var normalBytes int64
	upscaledNormalCount := 0
	for assetKey, normal := range normalsByKey {
		w, h := normal.source.width, normal.source.height
		upscaled := normal.maxPairedColor.pixels > normal.source.pixels
		if upscaled {
			w, h = normal.maxPairedColor.width, normal.maxPairedColor.height
			upscaledNormalCount++
		}
		bytes := EstimateGPUTextureBytesExact(w, h, true)
		normalBytes += bytes
		headlineTotal += bytes
		debug.Logf("[Materials-asset] kind=normal key=%q id=%d upscaled=%t %dx%d → %s",
			assetKey, metaByKey[assetKey].assetID, upscaled, w, h, format.FormatSizeAuto64(bytes))
	}
	var packBytes int64
	for groupKey, pack := range packsByKey {
		bytes := EstimateGPUTextureBytesExact(pack.width, pack.height, false)
		packBytes += bytes
		headlineTotal += bytes
		debug.Logf("[Materials-asset] kind=mr-pack groupKey=%q %dx%d → %s",
			groupKey, pack.width, pack.height, format.FormatSizeAuto64(bytes))
	}
	debug.Logf(
		"[Materials] PBR engine breakdown: %d SA bundles → %d unique colors (%s; %d BC3 = %s) + %d unique normals (%s, %d upscaled) + %d unique MR packs (%s) = %s headline",
		len(materials),
		len(uniqueColors), format.FormatSizeAuto64(colorBytes),
		colorBC3Count, format.FormatSizeAuto64(colorBC3Bytes),
		len(normalsByKey), format.FormatSizeAuto64(normalBytes), upscaledNormalCount,
		len(packsByKey), format.FormatSizeAuto64(packBytes),
		format.FormatSizeAuto64(headlineTotal),
	)
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

// CollectScanMaterialAndImageReport returns the PBR entries from
// CollectScanMaterialReport plus single-slot entries for every Image
// asset that isn't already represented in the PBR list — Decal.Texture,
// Texture.Texture, ImageLabel/ImageButton.Image, MeshPart.TextureID,
// Sky / ParticleEmitter / Beam / Trail textures, etc. The Materials
// sub-tab uses this so users see every image-bearing asset in one place,
// even ones that aren't strictly PBR materials.
//
// Loose entries fill the Color slot only; Normal/Metalness/Roughness
// asset IDs stay zero and NormalBytes / MRPackBytes stay zero. The
// returned headline includes both PBR engine bytes (deduped per the PBR
// model) AND each unique loose-image asset's color bytes — so the
// "Engine GPU memory" stat lines up with the sum of GPU Memory cells
// shown in the table.
func CollectScanMaterialAndImageReport(rows []loader.ScanResult) ([]ScanMaterialEntry, int64) {
	pbrEntries, pbrHeadline := CollectScanMaterialReport(rows)
	seen := map[int64]bool{}
	for _, e := range pbrEntries {
		if e.ColorAssetID > 0 {
			seen[e.ColorAssetID] = true
		}
		if e.NormalAssetID > 0 {
			seen[e.NormalAssetID] = true
		}
		if e.MetalnessAssetID > 0 {
			seen[e.MetalnessAssetID] = true
		}
		if e.RoughnessAssetID > 0 {
			seen[e.RoughnessAssetID] = true
		}
	}
	type looseAgg struct {
		entry   ScanMaterialEntry
		minPath string
		count   int
	}
	looseByID := map[int64]*looseAgg{}
	for _, row := range rows {
		if row.AssetID <= 0 {
			continue
		}
		// Match the Report tab's GPU Texture Memory accounting: any
		// loaded asset with positive pixel dimensions contributes,
		// regardless of `AssetTypeName`. Thumbnail-loaded assets
		// (`Thumbnail`), assets pending type resolution (`Unknown`),
		// or anything else with decoded image bytes still occupies
		// engine VRAM, so anchoring on `PixelCount > 0` keeps the
		// Materials headline aligned with the Report headline.
		if row.PixelCount <= 0 || row.Width <= 0 || row.Height <= 0 {
			continue
		}
		normalizedProperty := strings.ToLower(strings.TrimSpace(row.PropertyName))
		// Only skip rows whose property is one of the 4 routable PBR
		// slots — those are the assets buildScanResultMaterialsMap
		// actually placed into a Color/Normal/M/R slot and are
		// therefore already accounted for in `pbrEntries`. Anything
		// else with `mapcontent` in its name (`MaterialVariant.*`,
		// `EmissiveMaskContent`, etc.) passes IsSurfaceAppearanceProperty
		// but slotPointer can't route it into a slot, so the PBR
		// pipeline silently drops the asset — count those as loose
		// images so they still contribute to the Materials headline.
		if IsRoutableSAPBRSlot(normalizedProperty) {
			continue
		}
		if seen[row.AssetID] {
			continue
		}
		path := strings.TrimSpace(row.InstancePath)
		agg, ok := looseByID[row.AssetID]
		if !ok {
			isBC3 := loader.ClassifyAsBC3(row.HasAlphaChannel, row.NonOpaqueAlphaPixels, row.PropertyName)
			agg = &looseAgg{
				entry: ScanMaterialEntry{
					ColorAssetID: row.AssetID,
					ColorWidth:   row.Width,
					ColorHeight:  row.Height,
					ColorBytes:   EstimateGPUTextureBytesExact(row.Width, row.Height, isBC3),
					LooseImage:   true,
				},
				minPath: path,
			}
			looseByID[row.AssetID] = agg
		}
		agg.count++
		if path != "" && (agg.minPath == "" || path < agg.minPath) {
			agg.minPath = path
		}
	}
	out := make([]ScanMaterialEntry, 0, len(pbrEntries)+len(looseByID))
	out = append(out, pbrEntries...)
	var looseHeadline int64
	skippedNoDims := 0
	skippedRoutablePBR := 0
	skippedAlreadyInPBR := 0
	skippedZeroAssetID := 0
	candidateCount := 0
	for _, row := range rows {
		if row.AssetID <= 0 {
			skippedZeroAssetID++
			continue
		}
		if row.PixelCount <= 0 || row.Width <= 0 || row.Height <= 0 {
			skippedNoDims++
			continue
		}
		normalizedProperty := strings.ToLower(strings.TrimSpace(row.PropertyName))
		if IsRoutableSAPBRSlot(normalizedProperty) {
			skippedRoutablePBR++
			continue
		}
		if seen[row.AssetID] {
			skippedAlreadyInPBR++
			continue
		}
		candidateCount++
	}
	for assetID, agg := range looseByID {
		entry := agg.entry
		entry.InstanceCount = agg.count
		entry.InstancePath = agg.minPath
		out = append(out, entry)
		looseHeadline += entry.ColorBytes
		debug.Logf("[Materials-asset] kind=loose id=%d %dx%d refs=%d → %s",
			assetID, entry.ColorWidth, entry.ColorHeight, entry.InstanceCount, format.FormatSizeAuto64(entry.ColorBytes))
	}
	debug.Logf(
		"[Materials] Loose-image augmentation: %d unique loose assets totaling %s (skipped: %d zero-assetID, %d no-dims, %d routable-PBR-slot, %d already-in-PBR; %d candidates remain → %d unique after assetID dedup)",
		len(looseByID), format.FormatSizeAuto64(looseHeadline),
		skippedZeroAssetID, skippedNoDims, skippedRoutablePBR, skippedAlreadyInPBR,
		candidateCount, len(looseByID),
	)
	debug.Logf(
		"[Materials] Final headline: PBR %s + loose %s = %s (across %d total rows in)",
		format.FormatSizeAuto64(pbrHeadline),
		format.FormatSizeAuto64(looseHeadline),
		format.FormatSizeAuto64(pbrHeadline+looseHeadline),
		len(rows),
	)
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
	return out, pbrHeadline + looseHeadline
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
		normalizedProperty := strings.ToLower(strings.TrimSpace(row.PropertyName))
		if !IsSurfaceAppearanceProperty(normalizedProperty, row.InstanceType) {
			continue
		}
		// ScanResult dedupes per (AssetID, AssetInput) so a Color asset
		// referenced by 5 SurfaceAppearance instances collapses into a
		// single row whose primary InstancePath is one of the 5. Walk
		// AllInstancePaths (when set) so every owning SA gets the slot
		// assignment and the engine-model headline (MR packs, normal
		// upscaling) sees the full per-bundle picture instead of just
		// the primary path's bundle.
		paths := materialPathsForRow(row)
		if len(paths) == 0 {
			continue
		}
		slot := SurfaceAppearanceMaterialSlot{
			AssetKey:   extractor.AssetReferenceKey(row.AssetID, row.AssetInput),
			Width:      row.Width,
			Height:     row.Height,
			PixelCount: row.PixelCount,
		}
		for _, instancePath := range paths {
			slots := out[instancePath]
			slots.TryAssignByProperty(normalizedProperty, slot)
			out[instancePath] = slots
		}
	}
	return out
}

// materialPathsForRow returns the deduplicated set of instance paths
// the row's slot assignment should be applied to. Prefers
// AllInstancePaths when populated; falls back to the singular
// InstancePath. Empty / blank paths are dropped.
func materialPathsForRow(row loader.ScanResult) []string {
	if len(row.AllInstancePaths) == 0 {
		path := strings.TrimSpace(row.InstancePath)
		if path == "" {
			return nil
		}
		return []string{path}
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(row.AllInstancePaths))
	for _, raw := range row.AllInstancePaths {
		path := strings.TrimSpace(raw)
		if path == "" {
			continue
		}
		if _, dup := seen[path]; dup {
			continue
		}
		seen[path] = struct{}{}
		out = append(out, path)
	}
	if len(out) == 0 {
		path := strings.TrimSpace(row.InstancePath)
		if path == "" {
			return nil
		}
		return []string{path}
	}
	return out
}
