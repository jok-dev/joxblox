package renderdoc

import "sort"

// Material groups one (Color, Normal, MR) texture tuple bound together at the
// PS stage on at least one draw call. Built by BuildMaterials from the
// already-parsed texture and mesh reports.
type Material struct {
	ColorTextureID  string
	NormalTextureID string
	MRTextureID     string
	OtherTextureIDs []string // PS-bound textures we couldn't classify
	DrawCallCount   int
	TotalBytes      int64    // sum of unique map bytes
	MeshHashes      []string // unique mesh content hashes this material was used to draw
}

// sceneGlobalDrawCallFraction is the threshold above which a texture bound on
// a draw call is treated as scene-global (shadow map, env probe, etc.) rather
// than per-material. Empirical — Roblox's real per-material textures are
// bound on a small fraction of draws; globals are bound on essentially all.
const sceneGlobalDrawCallFraction = 0.8

// sceneGlobalMinDraws is the smallest draw-call count for which the
// frequency-based "scene global" detector is meaningful. Below this, every
// bound texture trivially hits 100% and the detector would falsely strip
// per-material textures. Category-based filtering (Builtin / RenderTgt /
// DepthTgt / Cubemap) still applies regardless.
const sceneGlobalMinDraws = 4

// BuildMaterials walks meshes.DrawCalls, classifies each PS-bound texture
// using the existing TextureInfo.Category, filters out scene-global textures,
// and dedupes materials by the (Color, Normal, MR) tuple.
func BuildMaterials(textures *Report, meshes *MeshReport) []Material {
	if textures == nil || meshes == nil || len(meshes.DrawCalls) == 0 {
		return nil
	}

	textureByID := map[string]TextureInfo{}
	for _, t := range textures.Textures {
		textureByID[t.ResourceID] = t
	}

	globals := computeSceneGlobalTextureIDs(textures, meshes, textureByID)

	type matKey struct{ color, normal, mr string }
	byKey := map[matKey]*Material{}
	var order []matKey

	for _, dc := range meshes.DrawCalls {
		var color, normal, mr string
		var others []string
		for _, texID := range dc.PSTextureIDs {
			if texID == "" {
				continue
			}
			if _, isGlobal := globals[texID]; isGlobal {
				continue
			}
			tex, known := textureByID[texID]
			if !known {
				others = append(others, texID)
				continue
			}
			switch tex.Category {
			case CategoryNormalDXT5nm:
				if normal == "" {
					normal = texID
				} else {
					others = append(others, texID)
				}
			case CategoryBlankMR, CategoryCustomMR:
				if mr == "" {
					mr = texID
				} else {
					others = append(others, texID)
				}
			case CategoryAssetOpaque, CategoryAssetAlpha, CategoryAssetRaw:
				if color == "" {
					color = texID
				} else {
					others = append(others, texID)
				}
			default:
				others = append(others, texID)
			}
		}
		if color == "" && normal == "" && mr == "" {
			// No PBR slots classified for this draw — typically a
			// depth-only, shadow, or outline pass that doesn't sample any
			// per-material textures. Whatever is in `others` here is
			// stale globals our filters didn't catch; lumping them into a
			// "material" produces a noisy catch-all row, so skip.
			continue
		}
		key := matKey{color, normal, mr}
		mat, exists := byKey[key]
		if !exists {
			mat = &Material{
				ColorTextureID:  color,
				NormalTextureID: normal,
				MRTextureID:     mr,
				TotalBytes:      sumUniqueBytes(textureByID, color, normal, mr),
			}
			byKey[key] = mat
			order = append(order, key)
		}
		mat.DrawCallCount++
		for _, o := range others {
			if !containsString(mat.OtherTextureIDs, o) {
				mat.OtherTextureIDs = append(mat.OtherTextureIDs, o)
			}
		}
	}

	out := make([]Material, 0, len(order))
	for _, k := range order {
		out = append(out, *byKey[k])
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].TotalBytes > out[j].TotalBytes
	})
	return out
}

func computeSceneGlobalTextureIDs(textures *Report, meshes *MeshReport, byID map[string]TextureInfo) map[string]struct{} {
	globals := map[string]struct{}{}
	for _, t := range textures.Textures {
		switch t.Category {
		case CategoryBuiltin, CategoryBuiltinBRDFLUT, CategoryRenderTgt, CategoryDepthTgt, CategoryCubemap:
			globals[t.ResourceID] = struct{}{}
		}
	}
	totalDraws := len(meshes.DrawCalls)
	if totalDraws < sceneGlobalMinDraws {
		return globals
	}
	bindCount := map[string]int{}
	for _, dc := range meshes.DrawCalls {
		seen := map[string]bool{}
		for _, texID := range dc.PSTextureIDs {
			if texID == "" || seen[texID] {
				continue
			}
			seen[texID] = true
			bindCount[texID]++
		}
	}
	threshold := int(float64(totalDraws) * sceneGlobalDrawCallFraction)
	if threshold < 1 {
		threshold = 1
	}
	for texID, n := range bindCount {
		if n >= threshold {
			globals[texID] = struct{}{}
		}
	}
	return globals
}

func sumUniqueBytes(byID map[string]TextureInfo, ids ...string) int64 {
	seen := map[string]bool{}
	var total int64
	for _, id := range ids {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		if t, ok := byID[id]; ok {
			total += t.Bytes
		}
	}
	return total
}

func containsString(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

// BuildMaterialsWithMeshHashes is BuildMaterials plus per-draw mesh hashing,
// so each Material's MeshHashes lists the unique meshes it draws. Reuses the
// same hash function as BuildMeshes for cross-tab consistency. Errors hashing
// an individual draw are silently dropped (the material still appears, just
// without that mesh hash) — same tolerance as BuildMeshes.
func BuildMaterialsWithMeshHashes(textures *Report, meshes *MeshReport, reader BufferReader) []Material {
	if textures == nil || meshes == nil || len(meshes.DrawCalls) == 0 {
		return nil
	}
	drawHashes := make([]string, len(meshes.DrawCalls))
	for i, dc := range meshes.DrawCalls {
		if reader == nil || len(dc.VertexBuffers) == 0 || dc.IndexBufferID == "" {
			continue
		}
		hash, _, _, err := hashMeshBuffers(dc, meshes.Buffers, reader)
		if err == nil {
			drawHashes[i] = hash
		}
	}
	out := BuildMaterials(textures, meshes)
	textureByID := map[string]TextureInfo{}
	for _, t := range textures.Textures {
		textureByID[t.ResourceID] = t
	}
	globals := computeSceneGlobalTextureIDs(textures, meshes, textureByID)
	hashesByKey := map[[3]string]map[string]struct{}{}
	for i, dc := range meshes.DrawCalls {
		hash := drawHashes[i]
		if hash == "" {
			continue
		}
		k := classifyDrawForKey(dc, textureByID, globals)
		if k == ([3]string{}) {
			continue
		}
		if hashesByKey[k] == nil {
			hashesByKey[k] = map[string]struct{}{}
		}
		hashesByKey[k][hash] = struct{}{}
	}
	for i := range out {
		k := [3]string{out[i].ColorTextureID, out[i].NormalTextureID, out[i].MRTextureID}
		set := hashesByKey[k]
		if len(set) == 0 {
			continue
		}
		hashes := make([]string, 0, len(set))
		for h := range set {
			hashes = append(hashes, h)
		}
		sort.Strings(hashes)
		out[i].MeshHashes = hashes
	}
	return out
}

// classifyDrawForKey returns the (color, normal, mr) key a single draw call
// produces — same logic as the inline classification in BuildMaterials but
// extracted so we can reuse it when attaching mesh hashes. Caller passes
// pre-built textureByID + globals so this stays O(slots) per call.
func classifyDrawForKey(dc DrawCall, textureByID map[string]TextureInfo, globals map[string]struct{}) [3]string {
	var color, normal, mr string
	for _, texID := range dc.PSTextureIDs {
		if texID == "" {
			continue
		}
		if _, isGlobal := globals[texID]; isGlobal {
			continue
		}
		tex, known := textureByID[texID]
		if !known {
			continue
		}
		switch tex.Category {
		case CategoryNormalDXT5nm:
			if normal == "" {
				normal = texID
			}
		case CategoryBlankMR, CategoryCustomMR:
			if mr == "" {
				mr = texID
			}
		case CategoryAssetOpaque, CategoryAssetAlpha, CategoryAssetRaw:
			if color == "" {
				color = texID
			}
		}
	}
	if color == "" && normal == "" && mr == "" {
		return [3]string{}
	}
	return [3]string{color, normal, mr}
}
