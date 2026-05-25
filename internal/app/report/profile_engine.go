package report

import (
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"joxblox/internal/app/loader"
	"joxblox/internal/debug"
	"joxblox/internal/extractor"
	"joxblox/internal/format"
	"joxblox/internal/heatmap"
	"joxblox/internal/rbxlscene"
)

// reportGenerationCellSizeStuds is the fixed spatial cell edge length (in
// studs) the report engine grids the scene into for percentile grading.
const reportGenerationCellSizeStuds = 1000.0

// MaterialVariantWarning summarizes MaterialVariants referenced by the file but
// missing from MaterialService. An empty Summary means no warning.
type MaterialVariantWarning struct {
	Summary    string `json:"summary"`
	DetailText string `json:"detailText"`
}

// BannedTextureDetail is one texture whose worst per-slot GPU footprint exceeds
// the asset type's hard cap, forcing the overall grade to F.
type BannedTextureDetail struct {
	AssetID         int64 `json:"assetId"`
	InstancePath    string
	Width           int
	Height          int
	GPUTextureBytes int64
	IsBC3           bool
}

// ProfileResult is the full output of GenerateProfile: the graded performance
// profile plus the detail lists the UI shows and the JSON export consumes.
type ProfileResult struct {
	AssetType     AssetTypeConfig
	HasData       bool
	Summary       Summary
	Percentiles   CellPercentiles
	Grades        []PerformanceGrade
	HasDuplicates bool

	// OverallGrade / OverallScore treat any banned textures as active (the
	// default). The …IgnoringBan variants are the natural grade with the ban
	// suppressed, for the UI's "ignore ban" toggle.
	OverallGrade            string
	OverallScore            int
	OverallGradeIgnoringBan string
	OverallScoreIgnoringBan int

	MismatchedPBR   []MismatchedPBRMaterialDetail
	Oversized       []ReportExportOversizedTexture
	Banned          []BannedTextureDetail
	DuplicateGroups []ReportExportDuplicateGroup
	Warning         MaterialVariantWarning
}

type reportGenerationResolvedAsset struct {
	Stats      heatmap.AssetStats
	FileSHA256 string
}

type reportGenerationInstanceRenderInfo struct {
	InstanceType           string
	InstancePath           string
	MaterialKey            string
	MeshContentKey         string
	TextureContentKey      string
	SurfaceAppearanceProps map[string]string
	X                      float64
	Z                      float64
	HasPosition            bool
}

// GenerateProfile runs the full report pipeline on a place/model file:
// extract positioned refs + map parts + instance count, resolve every unique
// asset's stats, build the summary and spatial cells, then grade against the
// chosen asset type. progress (may be nil) is called during asset resolution.
func GenerateProfile(filePath string, assetTypeLabel string, pathPrefixes []string, progress func(done int, total int)) (ProfileResult, error) {
	trimmedPath := strings.TrimSpace(filePath)
	if trimmedPath == "" {
		return ProfileResult{}, fmt.Errorf("select an .rbxl or .rbxm file first")
	}

	assetType := AssetTypeByLabel(assetTypeLabel)

	// Scope the analysis to the chosen top-level DataModel services. Only
	// meaningful for place files; a model (.rbxm) has no service hierarchy.
	var prefixes []string
	if len(pathPrefixes) > 0 && strings.EqualFold(filepath.Ext(trimmedPath), ".rbxl") {
		prefixes = pathPrefixes
	}

	positionedRefs, extractErr := extractor.ExtractPositionedRefs(trimmedPath, prefixes, nil)
	if extractErr != nil {
		return ProfileResult{}, fmt.Errorf("extract assets: %w", extractErr)
	}

	mapPartsRaw, mapPartsErr := extractor.ExtractMapRenderParts(trimmedPath, prefixes, nil)
	if mapPartsErr != nil {
		debug.Logf("Report generation map extraction failed for %s: %s", trimmedPath, mapPartsErr.Error())
	}
	instanceCount, instanceCountErr := extractor.ExtractInstanceCount(trimmedPath, prefixes, nil)
	if instanceCountErr != nil {
		debug.Logf("Report generation instance-count extraction failed for %s: %s", trimmedPath, instanceCountErr.Error())
	}
	warning := buildMissingMaterialVariantWarning(trimmedPath, prefixes)

	mapParts := rbxlscene.ConvertRustMapParts(mapPartsRaw)

	if len(positionedRefs) == 0 {
		return ProfileResult{AssetType: assetType, Warning: warning}, nil
	}

	uniqueRefsByKey := map[string]heatmap.AssetReference{}
	for _, ref := range positionedRefs {
		if ref.ID <= 0 {
			continue
		}
		key := extractor.AssetReferenceKey(ref.ID, ref.RawContent)
		uniqueRefsByKey[key] = heatmap.AssetReference{
			AssetID:    ref.ID,
			AssetInput: strings.TrimSpace(ref.RawContent),
		}
	}
	refsToResolve := make([]heatmap.AssetReference, 0, len(uniqueRefsByKey))
	for _, ref := range uniqueRefsByKey {
		refsToResolve = append(refsToResolve, ref)
	}

	resolved := resolveReportGenerationAssets(refsToResolve, progress)

	summary, _, mismatchedPBRDetails, oversizedDetails, bannedDetails, duplicateGroups :=
		buildReportSummaryAndPoints(positionedRefs, resolved, mapParts, assetType.OversizedTextureThreshold, assetType.BannedTextureSizeMB)
	summary.InstanceCount = instanceCount.Count

	points := buildReportGenerationPoints(positionedRefs, resolved)
	cells := buildReportGenerationCells(points, mapParts, positionedRefs, resolved, instanceCount.Positions)

	percentiles := ComputeReportCellPercentiles(assetType, cells, summary)
	grades := ComputePerformanceProfileForAssetType(assetType, percentiles, summary)
	hasDuplicates := summary.DuplicateCount > 0
	hasBanned := len(bannedDetails) > 0

	result := ProfileResult{
		AssetType:               assetType,
		HasData:                 true,
		Summary:                 summary,
		Percentiles:             percentiles,
		Grades:                  grades,
		HasDuplicates:           hasDuplicates,
		OverallGrade:            OverallPerformanceGrade(grades, hasDuplicates, hasBanned),
		OverallScore:            OverallPerformanceScorePercent(grades, hasDuplicates, hasBanned),
		OverallGradeIgnoringBan: OverallPerformanceGrade(grades, hasDuplicates, false),
		OverallScoreIgnoringBan: OverallPerformanceScorePercent(grades, hasDuplicates, false),
		MismatchedPBR:           mismatchedPBRDetails,
		Oversized:               oversizedDetails,
		Banned:                  bannedDetails,
		DuplicateGroups:         duplicateGroups,
		Warning:                 warning,
	}
	return result, nil
}

// AssetTypeByLabel returns the configured asset type matching label (case
// sensitive on the canonical label), falling back to the default asset type.
func AssetTypeByLabel(label string) AssetTypeConfig {
	trimmed := strings.TrimSpace(label)
	for _, assetType := range AssetTypeConfigs {
		if assetType.Label == trimmed {
			return assetType
		}
	}
	return DefaultAssetType()
}

// AssetTypeLabels returns the selectable asset-type labels in order.
func AssetTypeLabels() []string {
	labels := make([]string, 0, len(AssetTypeConfigs))
	for _, assetType := range AssetTypeConfigs {
		labels = append(labels, assetType.Label)
	}
	return labels
}

func resolveReportGenerationAssets(references []heatmap.AssetReference, progress func(done int, total int)) map[string]reportGenerationResolvedAsset {
	if len(references) == 0 {
		return map[string]reportGenerationResolvedAsset{}
	}

	jobs := make(chan heatmap.AssetReference, len(references))
	for _, reference := range references {
		jobs <- reference
	}
	close(jobs)

	resolvedByReferenceKey := make(map[string]reportGenerationResolvedAsset, len(references))
	var resolvedMutex sync.Mutex
	var completed atomic.Int64
	workerCount := min(runtime.NumCPU()*2, len(references))
	if workerCount <= 0 {
		workerCount = 1
	}

	var waitGroup sync.WaitGroup
	for workerIndex := 0; workerIndex < workerCount; workerIndex++ {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			for reference := range jobs {
				referenceKey := extractor.AssetReferenceKey(reference.AssetID, reference.AssetInput)
				previewResult, previewErr := loader.LoadAssetStatsPreviewForReference(reference.AssetID, reference.AssetInput)
				resolvedAsset := reportGenerationResolvedAsset{
					Stats: heatmap.AssetStats{AssetID: reference.AssetID},
				}
				if previewErr == nil && previewResult != nil {
					resolvedAsset = reportGenerationResolvedAsset{
						Stats:      loader.BuildAssetStatsFromPreview(reference.AssetID, previewResult),
						FileSHA256: loader.NormalizeHash(loader.PreviewSHA256(previewResult)),
					}
				}
				resolvedMutex.Lock()
				resolvedByReferenceKey[referenceKey] = resolvedAsset
				resolvedMutex.Unlock()
				if progress != nil {
					progress(int(completed.Add(1)), len(references))
				}
			}
		}()
	}
	waitGroup.Wait()
	return resolvedByReferenceKey
}

func buildReportGenerationPoints(refs []extractor.PositionedResult, resolved map[string]reportGenerationResolvedAsset) []rbxlscene.RBXLHeatmapPoint {
	points := make([]rbxlscene.RBXLHeatmapPoint, 0, len(refs))
	for _, ref := range refs {
		if ref.ID <= 0 || ref.WorldX == nil || ref.WorldY == nil || ref.WorldZ == nil {
			continue
		}
		key := extractor.AssetReferenceKey(ref.ID, ref.RawContent)
		asset, found := resolved[key]
		if !found || asset.Stats.TotalBytes <= 0 {
			continue
		}
		points = append(points, rbxlscene.RBXLHeatmapPoint{
			AssetID:      ref.ID,
			AssetInput:   strings.TrimSpace(ref.RawContent),
			InstanceType: strings.TrimSpace(ref.InstanceType),
			InstanceName: strings.TrimSpace(ref.InstanceName),
			InstancePath: strings.TrimSpace(ref.InstancePath),
			PropertyName: strings.TrimSpace(ref.PropertyName),
			Stats:        asset.Stats,
			X:            *ref.WorldX,
			Y:            *ref.WorldY,
			Z:            *ref.WorldZ,
		})
	}
	return points
}

func buildReportGenerationCells(points []rbxlscene.RBXLHeatmapPoint, mapParts []rbxlscene.RBXLHeatmapMapPart, refs []extractor.PositionedResult, resolved map[string]reportGenerationResolvedAsset, instancePositions []extractor.InstancePosition) []heatmap.Cell {
	if len(points) == 0 && len(mapParts) == 0 {
		return nil
	}

	minX, maxX, minZ, maxZ := rbxlscene.HeatmapSceneBounds(points, mapParts)
	rangeX := maxX - minX
	rangeZ := maxZ - minZ
	longestRange := math.Max(rangeX, rangeZ)
	if longestRange <= 0 {
		longestRange = reportGenerationCellSizeStuds
	}

	gridDivisions := max(1, int(math.Ceil(longestRange/reportGenerationCellSizeStuds)))
	paddedLongestRange := float64(gridDivisions) * reportGenerationCellSizeStuds
	scene := &rbxlscene.RBXLHeatmapScene{
		Points:   points,
		MinimumX: minX,
		MaximumX: maxX,
		MinimumZ: minZ,
		MaximumZ: maxZ,
	}
	if rangeX >= rangeZ {
		scene.MaximumX = scene.MinimumX + paddedLongestRange
	} else {
		scene.MaximumZ = scene.MinimumZ + paddedLongestRange
	}

	cells, cellSizeWorld, columnCount, rowCount, _ := rbxlscene.BuildHeatmapCells(scene, gridDivisions)
	if cellSizeWorld <= 0 {
		cellSizeWorld = reportGenerationCellSizeStuds
	}

	renderInfos := buildReportGenerationRenderInfos(mapParts, refs)
	partCountsByCell := map[string]heatmap.Totals{}
	meshDrawKeysByCell := map[string]map[string]struct{}{}
	for _, renderInfo := range renderInfos {
		if !renderInfo.HasPosition {
			continue
		}
		column := format.Clamp(int(math.Floor((renderInfo.X-scene.MinimumX)/cellSizeWorld)), 0, columnCount-1)
		row := format.Clamp(int(math.Floor((renderInfo.Z-scene.MinimumZ)/cellSizeWorld)), 0, rowCount-1)
		cellKey := rbxlscene.HeatmapCellKey(row, column)
		counts := partCountsByCell[cellKey]
		switch NormalizeInstanceType(renderInfo.InstanceType) {
		case "meshpart":
			counts.MeshPartCount++
			drawCallKey := estimateMeshPartDrawCallKey(renderInfo)
			if drawCallKey == "" {
				counts.DrawCallCount++
			} else {
				drawKeys := meshDrawKeysByCell[cellKey]
				if drawKeys == nil {
					drawKeys = map[string]struct{}{}
					meshDrawKeysByCell[cellKey] = drawKeys
				}
				drawKeys[drawCallKey] = struct{}{}
			}
		case "part":
			counts.PartCount++
			counts.DrawCallCount++
		case "":
		default:
			counts.DrawCallCount++
		}
		partCountsByCell[cellKey] = counts
	}
	for cellKey, drawKeys := range meshDrawKeysByCell {
		counts := partCountsByCell[cellKey]
		counts.DrawCallCount += int64(len(drawKeys))
		partCountsByCell[cellKey] = counts
	}

	cellIndexByKey := make(map[string]int, len(cells))
	for index, cell := range cells {
		cellKey := rbxlscene.HeatmapCellKey(cell.Row, cell.Column)
		cellIndexByKey[cellKey] = index
	}
	for cellKey, counts := range partCountsByCell {
		index, found := cellIndexByKey[cellKey]
		if !found {
			var row, column int
			if _, scanErr := fmt.Sscanf(cellKey, "%d:%d", &row, &column); scanErr != nil {
				continue
			}
			cells = append(cells, heatmap.Cell{
				Row:      row,
				Column:   column,
				MinimumX: scene.MinimumX + float64(column)*cellSizeWorld,
				MaximumX: scene.MinimumX + float64(column+1)*cellSizeWorld,
				MinimumZ: scene.MinimumZ + float64(row)*cellSizeWorld,
				MaximumZ: scene.MinimumZ + float64(row+1)*cellSizeWorld,
			})
			index = len(cells) - 1
			cellIndexByKey[cellKey] = index
		}
		cells[index].Stats.MeshPartCount = counts.MeshPartCount
		cells[index].Stats.PartCount = counts.PartCount
		cells[index].Stats.DrawCallCount = counts.DrawCallCount
	}

	applyPerCellSurfaceAppearanceCorrection(cells, cellIndexByKey, refs, resolved, renderInfos, scene, cellSizeWorld, columnCount, rowCount)

	for _, pos := range instancePositions {
		x := float64(pos.X)
		z := float64(pos.Z)
		if x < scene.MinimumX || x >= scene.MinimumX+float64(columnCount)*cellSizeWorld {
			continue
		}
		if z < scene.MinimumZ || z >= scene.MinimumZ+float64(rowCount)*cellSizeWorld {
			continue
		}
		column := format.Clamp(int(math.Floor((x-scene.MinimumX)/cellSizeWorld)), 0, columnCount-1)
		row := format.Clamp(int(math.Floor((z-scene.MinimumZ)/cellSizeWorld)), 0, rowCount-1)
		cellKey := rbxlscene.HeatmapCellKey(row, column)
		index, found := cellIndexByKey[cellKey]
		if !found {
			cells = append(cells, heatmap.Cell{
				Row:      row,
				Column:   column,
				MinimumX: scene.MinimumX + float64(column)*cellSizeWorld,
				MaximumX: scene.MinimumX + float64(column+1)*cellSizeWorld,
				MinimumZ: scene.MinimumZ + float64(row)*cellSizeWorld,
				MaximumZ: scene.MinimumZ + float64(row+1)*cellSizeWorld,
			})
			index = len(cells) - 1
			cellIndexByKey[cellKey] = index
		}
		cells[index].Stats.InstanceCount++
	}

	sort.Slice(cells, func(left int, right int) bool {
		if cells[left].Row == cells[right].Row {
			return cells[left].Column < cells[right].Column
		}
		return cells[left].Row < cells[right].Row
	})
	return cells
}

// applyPerCellSurfaceAppearanceCorrection adjusts each cell's BC1/BC3 pixel
// count so per-cell GPU-memory grading sees engine-allocated MR-pack VRAM (one
// BC1 per unique normal map in the cell) instead of the raw per-asset tally.
func applyPerCellSurfaceAppearanceCorrection(
	cells []heatmap.Cell,
	cellIndexByKey map[string]int,
	refs []extractor.PositionedResult,
	resolved map[string]reportGenerationResolvedAsset,
	renderInfos map[string]reportGenerationInstanceRenderInfo,
	scene *rbxlscene.RBXLHeatmapScene,
	cellSizeWorld float64,
	columnCount, rowCount int,
) {
	if len(cells) == 0 || len(refs) == 0 || cellSizeWorld <= 0 {
		return
	}
	cellKeyForRef := func(ref extractor.PositionedResult) (string, bool) {
		x, z, ok := refWorldXZ(ref, renderInfos)
		if !ok {
			return "", false
		}
		column := format.Clamp(int(math.Floor((x-scene.MinimumX)/cellSizeWorld)), 0, columnCount-1)
		row := format.Clamp(int(math.Floor((z-scene.MinimumZ)/cellSizeWorld)), 0, rowCount-1)
		return rbxlscene.HeatmapCellKey(row, column), true
	}
	materialsByCell := buildSurfaceAppearanceMaterialsByOwner(refs, resolved, cellKeyForRef)
	for cellKey, materials := range materialsByCell {
		index, ok := cellIndexByKey[cellKey]
		if !ok {
			continue
		}
		delta := ComputeSurfaceAppearanceMemoryCorrection(materials)
		ApplyDeltaClamped(&cells[index].Stats.BC1PixelCount, delta.NetBC1Pixels())
		ApplyDeltaClamped(&cells[index].Stats.BC3PixelCount, delta.NetBC3Pixels())
	}
}

// refWorldXZ returns the (x, z) world position to attribute a ref to: its own
// world coords if present, otherwise the owning part's position via renderInfos.
func refWorldXZ(ref extractor.PositionedResult, renderInfos map[string]reportGenerationInstanceRenderInfo) (float64, float64, bool) {
	if ref.WorldX != nil && ref.WorldZ != nil {
		return *ref.WorldX, *ref.WorldZ, true
	}
	ownerPath, _ := PositionedRefTarget(ref)
	if info, ok := renderInfos[ownerPath]; ok && info.HasPosition {
		return info.X, info.Z, true
	}
	return 0, 0, false
}

func buildReportSummaryAndPoints(refs []extractor.PositionedResult, resolved map[string]reportGenerationResolvedAsset, mapParts []rbxlscene.RBXLHeatmapMapPart, oversizedTextureThreshold float64, bannedTextureSizeMB float64) (Summary, []rbxlscene.RBXLHeatmapPoint, []MismatchedPBRMaterialDetail, []ReportExportOversizedTexture, []BannedTextureDetail, []ReportExportDuplicateGroup) {
	summary := Summary{}
	uniqueAssetIDs := map[int64]struct{}{}
	uniqueReferenceKeys := map[string]struct{}{}
	hashCounts := map[string]int{}
	resolvedUniqueKeys := make([]string, 0, len(resolved))
	seenResolvedKeys := map[string]struct{}{}
	instancePathByKey := map[string]string{}
	assetIDByKey := map[string]int64{}
	for _, ref := range refs {
		if ref.ID <= 0 {
			continue
		}

		summary.ReferenceCount++
		uniqueAssetIDs[ref.ID] = struct{}{}
		key := extractor.AssetReferenceKey(ref.ID, ref.RawContent)
		if _, seen := uniqueReferenceKeys[key]; !seen {
			uniqueReferenceKeys[key] = struct{}{}
			summary.UniqueReferenceCount++
		}
		asset, found := resolved[key]
		if !found {
			continue
		}
		stats := asset.Stats
		if stats.TotalBytes <= 0 && stats.TextureBytes <= 0 && stats.MeshBytes <= 0 && stats.TriangleCount == 0 {
			continue
		}
		if reportGenerationMeshTriangleInstanceKey(ref) != "" && stats.TriangleCount > 0 {
			summary.TriangleCount += int64(stats.TriangleCount)
		}
		if _, seen := seenResolvedKeys[key]; seen {
			continue
		}
		seenResolvedKeys[key] = struct{}{}
		assetIDByKey[key] = ref.ID
		if instancePath := strings.TrimSpace(ref.InstancePath); instancePath != "" {
			instancePathByKey[key] = instancePath
		}
		if asset.FileSHA256 != "" {
			hashCounts[asset.FileSHA256]++
			resolvedUniqueKeys = append(resolvedUniqueKeys, key)
		}
		summary.TotalBytes += int64(stats.TotalBytes)
		summary.TextureBytes += int64(stats.TextureBytes)
		summary.TexturePixelCount += stats.PixelCount
		if stats.PixelCount > 0 {
			isBC3 := loader.ClassifyAsBC3(stats.HasAlphaChannel, stats.NonOpaqueAlphaPixels, ref.PropertyName)
			exactBytes := EstimateGPUTextureBytesExact(stats.Width, stats.Height, isBC3)
			if isBC3 {
				summary.BC3PixelCount += stats.PixelCount
				summary.BC3BytesExact += exactBytes
			} else {
				summary.BC1PixelCount += stats.PixelCount
				summary.BC1BytesExact += exactBytes
			}
		}
		summary.MeshBytes += int64(stats.MeshBytes)
		summary.ResolvedCount++
	}

	seenCounts := map[string]int{}
	duplicateGroupsByHash := map[string]*ReportExportDuplicateGroup{}
	for _, key := range resolvedUniqueKeys {
		asset := resolved[key]
		if asset.FileSHA256 == "" {
			continue
		}
		if hashCounts[asset.FileSHA256] < 2 {
			continue
		}
		if seenCounts[asset.FileSHA256] >= 1 && asset.Stats.TotalBytes >= 10*1024 {
			summary.DuplicateCount++
			summary.DuplicateSizeBytes += int64(asset.Stats.TotalBytes)
		}
		seenCounts[asset.FileSHA256]++

		if asset.Stats.TotalBytes < 10*1024 {
			continue
		}
		group, ok := duplicateGroupsByHash[asset.FileSHA256]
		if !ok {
			group = &ReportExportDuplicateGroup{FileSHA256: asset.FileSHA256, AssetBytes: int64(asset.Stats.TotalBytes)}
			duplicateGroupsByHash[asset.FileSHA256] = group
		}
		group.AssetIDs = append(group.AssetIDs, assetIDByKey[key])
		if group.SampleInstancePath == "" {
			group.SampleInstancePath = instancePathByKey[key]
		}
	}
	duplicateGroups := make([]ReportExportDuplicateGroup, 0, len(duplicateGroupsByHash))
	for _, group := range duplicateGroupsByHash {
		if len(group.AssetIDs) < 2 {
			continue
		}
		duplicateGroups = append(duplicateGroups, *group)
	}
	sort.Slice(duplicateGroups, func(i, j int) bool {
		wastedI := duplicateGroups[i].AssetBytes * int64(len(duplicateGroups[i].AssetIDs)-1)
		wastedJ := duplicateGroups[j].AssetBytes * int64(len(duplicateGroups[j].AssetIDs)-1)
		return wastedI > wastedJ
	})
	summary.UniqueAssetCount = len(uniqueAssetIDs)
	oversizedDetails := collectReportGenerationOversizedTextures(refs, resolved, mapParts, oversizedTextureThreshold)
	summary.OversizedTextureCount = len(oversizedDetails)
	bannedDetails := collectReportGenerationBannedTextures(refs, resolved, bannedTextureSizeMB)
	materials := collectSurfaceAppearanceMaterialSlots(refs, resolved)
	summary.MismatchedPBRMaterialCount, summary.PBRMaterialCount = CountMismatchedPBRMaterials(materials)
	summary.MismatchedPBRWastedBytes = ComputeMismatchedPBRWastedBytes(materials)
	mismatchedPBRDetails := CollectMismatchedPBRMaterials(materials)
	ApplySurfaceAppearanceMemoryCorrections(&summary, materials)

	points := buildReportGenerationPoints(refs, resolved)
	summary.MeshPartCount, summary.PartCount = countReportGenerationParts(mapParts, refs)
	summary.DrawCallCount = int64(countEstimatedDrawCalls(mapParts, refs))
	return summary, points, mismatchedPBRDetails, oversizedDetails, bannedDetails, duplicateGroups
}

func collectReportGenerationOversizedTextures(refs []extractor.PositionedResult, resolved map[string]reportGenerationResolvedAsset, mapParts []rbxlscene.RBXLHeatmapMapPart, threshold float64) []ReportExportOversizedTexture {
	if threshold <= 0 {
		threshold = loader.DefaultLargeTextureThreshold
	}
	if len(refs) == 0 || len(resolved) == 0 {
		return nil
	}

	areaByPath := loader.BuildSceneSurfaceAreaIndex(mapParts)
	maxAreaByReferenceKey := map[string]float64{}
	type refContext struct {
		assetID      int64
		instancePath string
		area         float64
	}
	bestRefByKey := map[string]refContext{}
	for _, ref := range refs {
		if ref.ID <= 0 {
			continue
		}
		referenceKey := extractor.AssetReferenceKey(ref.ID, ref.RawContent)
		instancePath := strings.TrimSpace(ref.InstancePath)
		area := loader.EstimateSceneSurfaceAreaForPaths(instancePath, nil, areaByPath)
		maxAreaByReferenceKey[referenceKey] = loader.MaxPositiveFloat64(maxAreaByReferenceKey[referenceKey], area)
		if existing, ok := bestRefByKey[referenceKey]; !ok || area > existing.area {
			bestRefByKey[referenceKey] = refContext{assetID: ref.ID, instancePath: instancePath, area: area}
		}
	}

	const minOversizedTextureBytes = 100 * 1024
	details := make([]ReportExportOversizedTexture, 0)
	for referenceKey, resolvedAsset := range resolved {
		textureBytes := resolvedAsset.Stats.TextureBytes
		if textureBytes < minOversizedTextureBytes {
			continue
		}
		score := loader.ComputeLargeTextureScore(textureBytes, maxAreaByReferenceKey[referenceKey])
		if score < threshold {
			continue
		}
		ctx := bestRefByKey[referenceKey]
		details = append(details, ReportExportOversizedTexture{
			AssetID:          ctx.assetID,
			InstancePath:     ctx.instancePath,
			Width:            resolvedAsset.Stats.Width,
			Height:           resolvedAsset.Stats.Height,
			TextureBytes:     int64(textureBytes),
			SceneSurfaceArea: maxAreaByReferenceKey[referenceKey],
			Score:            score,
		})
	}
	sort.Slice(details, func(i, j int) bool { return details[i].Score > details[j].Score })
	return details
}

// collectReportGenerationBannedTextures judges each texture by its worst
// per-slot GPU footprint — a texture used as a normal map in any slot is rated
// at its BC3 size (2× BC1).
func collectReportGenerationBannedTextures(refs []extractor.PositionedResult, resolved map[string]reportGenerationResolvedAsset, bannedSizeMB float64) []BannedTextureDetail {
	if bannedSizeMB <= 0 || len(resolved) == 0 {
		return nil
	}
	limitBytes := format.MegabytesToBytes(bannedSizeMB)
	if limitBytes <= 0 {
		return nil
	}

	type slotInfo struct {
		assetID      int64
		instancePath string
		gpuBytes     int64
		isBC3        bool
	}
	worstByKey := map[string]slotInfo{}
	for _, ref := range refs {
		if ref.ID <= 0 {
			continue
		}
		referenceKey := extractor.AssetReferenceKey(ref.ID, ref.RawContent)
		asset, found := resolved[referenceKey]
		if !found || asset.Stats.PixelCount <= 0 {
			continue
		}
		isBC3 := loader.ClassifyAsBC3(asset.Stats.HasAlphaChannel, asset.Stats.NonOpaqueAlphaPixels, ref.PropertyName)
		gpuBytes := EstimateGPUTextureBytesExact(asset.Stats.Width, asset.Stats.Height, isBC3)
		existing, ok := worstByKey[referenceKey]
		if !ok || gpuBytes > existing.gpuBytes {
			worstByKey[referenceKey] = slotInfo{
				assetID:      ref.ID,
				instancePath: strings.TrimSpace(ref.InstancePath),
				gpuBytes:     gpuBytes,
				isBC3:        isBC3,
			}
		}
	}

	details := make([]BannedTextureDetail, 0)
	for referenceKey, slot := range worstByKey {
		if slot.gpuBytes <= limitBytes {
			continue
		}
		asset := resolved[referenceKey]
		details = append(details, BannedTextureDetail{
			AssetID:         slot.assetID,
			InstancePath:    slot.instancePath,
			Width:           asset.Stats.Width,
			Height:          asset.Stats.Height,
			GPUTextureBytes: slot.gpuBytes,
			IsBC3:           slot.isBC3,
		})
	}
	sort.Slice(details, func(i, j int) bool { return details[i].GPUTextureBytes > details[j].GPUTextureBytes })
	return details
}

func countReportGenerationParts(mapParts []rbxlscene.RBXLHeatmapMapPart, refs []extractor.PositionedResult) (int, int) {
	if len(mapParts) > 0 {
		meshPartCount := 0
		partCount := 0
		for _, part := range mapParts {
			partType := strings.ToLower(strings.TrimSpace(part.InstanceType))
			switch partType {
			case "meshpart":
				meshPartCount++
			case "part":
				partCount++
			}
		}
		return meshPartCount, partCount
	}

	meshPartCount := 0
	partCount := 0
	seenInstancePaths := map[string]struct{}{}
	for _, ref := range refs {
		instancePath := strings.TrimSpace(ref.InstancePath)
		if instancePath == "" {
			continue
		}
		if _, seen := seenInstancePaths[instancePath]; seen {
			continue
		}
		seenInstancePaths[instancePath] = struct{}{}
		switch strings.ToLower(strings.TrimSpace(ref.InstanceType)) {
		case "meshpart":
			meshPartCount++
		case "part":
			partCount++
		}
	}
	return meshPartCount, partCount
}

func countEstimatedDrawCalls(mapParts []rbxlscene.RBXLHeatmapMapPart, refs []extractor.PositionedResult) int {
	renderInfos := buildReportGenerationRenderInfos(mapParts, refs)
	if len(renderInfos) == 0 {
		return 0
	}
	drawCalls := 0
	meshDrawKeys := map[string]struct{}{}
	for _, renderInfo := range renderInfos {
		switch NormalizeInstanceType(renderInfo.InstanceType) {
		case "meshpart":
			drawCallKey := estimateMeshPartDrawCallKey(renderInfo)
			if drawCallKey == "" {
				drawCalls++
				continue
			}
			meshDrawKeys[drawCallKey] = struct{}{}
		case "":
		default:
			drawCalls++
		}
	}
	return drawCalls + len(meshDrawKeys)
}

func collectSurfaceAppearanceMaterialSlots(refs []extractor.PositionedResult, resolved map[string]reportGenerationResolvedAsset) map[string]SurfaceAppearanceMaterialSlots {
	allInOneOwner := func(extractor.PositionedResult) (string, bool) { return "", true }
	return buildSurfaceAppearanceMaterialsByOwner(refs, resolved, allInOneOwner)[""]
}

// buildSurfaceAppearanceMaterialsByOwner walks SA-related refs once and groups
// them into per-owner material maps. ownerKeyFn returns the owner identifier
// for a ref (a constant for all-in-one-bucket, or a per-cell key for spatial
// bucketing) — return false to skip the ref.
//
// The rusty extractor emits each material's refs as a contiguous block
// (TexturePack, Normal, Color, then the next material's block); when a slot
// we already filled is about to be overwritten, that's the boundary into a new
// SurfaceAppearance on the same instance path — we suffix the material key with
// #1, #2, … to keep them distinct.
func buildSurfaceAppearanceMaterialsByOwner(
	refs []extractor.PositionedResult,
	resolved map[string]reportGenerationResolvedAsset,
	ownerKeyFn func(ref extractor.PositionedResult) (string, bool),
) map[string]map[string]SurfaceAppearanceMaterialSlots {
	materialsByOwner := map[string]map[string]SurfaceAppearanceMaterialSlots{}
	type pathState struct {
		currentKey string
		counter    int
	}
	stateByOwnerPath := map[string]map[string]*pathState{}
	for _, ref := range refs {
		if ref.ID <= 0 {
			continue
		}
		normalizedProperty := strings.ToLower(strings.TrimSpace(ref.PropertyName))
		if !IsSurfaceAppearanceProperty(normalizedProperty, ref.InstanceType) {
			continue
		}
		instancePath := strings.TrimSpace(ref.InstancePath)
		if instancePath == "" {
			continue
		}
		ownerKey, ok := ownerKeyFn(ref)
		if !ok {
			continue
		}
		refKey := extractor.AssetReferenceKey(ref.ID, ref.RawContent)
		asset, found := resolved[refKey]
		if !found || asset.Stats.PixelCount <= 0 {
			continue
		}
		slot := SurfaceAppearanceMaterialSlot{
			AssetKey:   refKey,
			Width:      asset.Stats.Width,
			Height:     asset.Stats.Height,
			PixelCount: asset.Stats.PixelCount,
		}

		materials := materialsByOwner[ownerKey]
		if materials == nil {
			materials = map[string]SurfaceAppearanceMaterialSlots{}
			materialsByOwner[ownerKey] = materials
		}
		pathStates := stateByOwnerPath[ownerKey]
		if pathStates == nil {
			pathStates = map[string]*pathState{}
			stateByOwnerPath[ownerKey] = pathStates
		}
		state, hasState := pathStates[instancePath]
		if !hasState {
			state = &pathState{currentKey: instancePath}
			pathStates[instancePath] = state
		}
		slots := materials[state.currentKey]
		if !slots.TryAssignByProperty(normalizedProperty, slot) {
			state.counter++
			state.currentKey = fmt.Sprintf("%s#%d", instancePath, state.counter)
			slots = materials[state.currentKey]
			slots.TryAssignByProperty(normalizedProperty, slot)
		}
		materials[state.currentKey] = slots
	}
	return materialsByOwner
}

func buildReportGenerationRenderInfos(mapParts []rbxlscene.RBXLHeatmapMapPart, refs []extractor.PositionedResult) map[string]reportGenerationInstanceRenderInfo {
	renderInfos := map[string]reportGenerationInstanceRenderInfo{}
	for _, part := range mapParts {
		instancePath := strings.TrimSpace(part.InstancePath)
		if instancePath == "" {
			continue
		}
		renderInfos[instancePath] = reportGenerationInstanceRenderInfo{
			InstanceType: strings.TrimSpace(part.InstanceType),
			InstancePath: instancePath,
			MaterialKey:  strings.TrimSpace(part.MaterialKey),
			X:            part.CenterX,
			Z:            part.CenterZ,
			HasPosition:  true,
		}
	}
	for _, ref := range refs {
		instancePath, instanceType := PositionedRefTarget(ref)
		if instancePath == "" {
			continue
		}
		if !isReportGenerationFallbackRenderableType(instanceType) {
			if len(mapParts) == 0 {
				continue
			}
			if _, found := renderInfos[instancePath]; !found {
				continue
			}
		}
		renderInfo := renderInfos[instancePath]
		if renderInfo.InstancePath == "" {
			renderInfo.InstancePath = instancePath
		}
		if renderInfo.InstanceType == "" {
			renderInfo.InstanceType = instanceType
		}
		if !renderInfo.HasPosition && ref.WorldX != nil && ref.WorldZ != nil {
			renderInfo.X = *ref.WorldX
			renderInfo.Z = *ref.WorldZ
			renderInfo.HasPosition = true
		}
		contentKey := extractor.AssetReferenceKey(ref.ID, ref.RawContent)
		normalizedPropertyName := strings.ToLower(strings.TrimSpace(ref.PropertyName))
		switch {
		case IsMeshContentProperty(normalizedPropertyName):
			renderInfo.MeshContentKey = contentKey
		case IsTextureContentProperty(normalizedPropertyName):
			renderInfo.TextureContentKey = contentKey
		case IsSurfaceAppearanceProperty(normalizedPropertyName, ref.InstanceType):
			if renderInfo.SurfaceAppearanceProps == nil {
				renderInfo.SurfaceAppearanceProps = map[string]string{}
			}
			renderInfo.SurfaceAppearanceProps[normalizedPropertyName] = contentKey
		}
		renderInfos[instancePath] = renderInfo
	}
	return renderInfos
}

func reportGenerationMeshTriangleInstanceKey(ref extractor.PositionedResult) string {
	if !IsMeshContentProperty(strings.ToLower(strings.TrimSpace(ref.PropertyName))) {
		return ""
	}
	instancePath, _ := PositionedRefTarget(ref)
	return strings.TrimSpace(instancePath)
}

func isReportGenerationFallbackRenderableType(instanceType string) bool {
	switch NormalizeInstanceType(instanceType) {
	case "meshpart", "part":
		return true
	default:
		return false
	}
}

func estimateMeshPartDrawCallKey(renderInfo reportGenerationInstanceRenderInfo) string {
	instancePath := strings.TrimSpace(renderInfo.InstancePath)
	meshContentKey := strings.TrimSpace(renderInfo.MeshContentKey)
	if meshContentKey == "" {
		if instancePath == "" {
			return ""
		}
		return "meshpart:" + instancePath
	}
	surfaceAppearanceKey := reportGenerationSurfaceAppearanceKey(renderInfo.SurfaceAppearanceProps)
	if surfaceAppearanceKey != "" {
		return "meshpart:" + meshContentKey + "|surface:" + surfaceAppearanceKey
	}
	textureContentKey := strings.TrimSpace(renderInfo.TextureContentKey)
	if textureContentKey != "" {
		return "meshpart:" + meshContentKey + "|texture:" + textureContentKey
	}
	materialKey := strings.TrimSpace(renderInfo.MaterialKey)
	if materialKey != "" {
		return "meshpart:" + meshContentKey + "|material:" + materialKey
	}
	if instancePath == "" {
		return ""
	}
	return "meshpart:" + instancePath
}

func reportGenerationSurfaceAppearanceKey(properties map[string]string) string {
	if len(properties) == 0 {
		return ""
	}
	propertyNames := make([]string, 0, len(properties))
	for propertyName := range properties {
		propertyNames = append(propertyNames, propertyName)
	}
	sort.Strings(propertyNames)
	parts := make([]string, 0, len(propertyNames))
	for _, propertyName := range propertyNames {
		parts = append(parts, propertyName+"="+properties[propertyName])
	}
	return strings.Join(parts, "|")
}

// buildMissingMaterialVariantWarning extracts MaterialVariants referenced by
// the file but missing from MaterialService and summarizes them. An empty
// result Summary means there was nothing to warn about.
func buildMissingMaterialVariantWarning(filePath string, pathPrefixes []string) MaterialVariantWarning {
	missing, extractErr := extractor.ExtractMissingMaterialVariants(filePath, materialVariantWarningPathPrefixes(pathPrefixes), nil)
	if extractErr != nil {
		if !errors.Is(extractErr, extractor.ErrCancelled) {
			debug.Logf("Report generation material warning extraction failed for %s: %s", filePath, extractErr.Error())
		}
		return MaterialVariantWarning{}
	}
	if len(missing) == 0 {
		return MaterialVariantWarning{}
	}

	uniqueNames := map[string]struct{}{}
	sampleNames := make([]string, 0, len(missing))
	for _, entry := range missing {
		name := strings.TrimSpace(entry.VariantName)
		if name == "" {
			continue
		}
		nameKey := strings.ToLower(name)
		if _, exists := uniqueNames[nameKey]; exists {
			continue
		}
		uniqueNames[nameKey] = struct{}{}
		sampleNames = append(sampleNames, name)
	}
	sort.Strings(sampleNames)

	variantCount := len(sampleNames)
	if variantCount == 0 {
		variantCount = len(missing)
	}
	if variantCount == 0 {
		return MaterialVariantWarning{}
	}

	label := strings.TrimSpace(filepath.Base(strings.TrimSpace(filePath)))
	if label == "" {
		label = "this file"
	}
	variantWord := "MaterialVariants"
	if variantCount == 1 {
		variantWord = "MaterialVariant"
	}
	detailLines := []string{
		"Referenced by " + label + ":",
		"",
		"Missing MaterialVariants in MaterialService:",
	}
	for _, name := range sampleNames {
		detailLines = append(detailLines, "- "+name)
	}
	return MaterialVariantWarning{
		Summary: "Warning: " + format.FormatIntCommas(int64(variantCount)) + " " + variantWord + " missing in MaterialService for " + label + ".",
		DetailText: strings.Join(detailLines, "\n") +
			"\n\nYou won't get a perfect look at all textures since some will be in missing materials.",
	}
}

func materialVariantWarningPathPrefixes(pathPrefixes []string) []string {
	if len(pathPrefixes) == 0 {
		return nil
	}
	filtered := make([]string, 0, len(pathPrefixes)+1)
	hasMaterialService := false
	for _, prefix := range pathPrefixes {
		trimmedPrefix := strings.TrimSpace(prefix)
		if trimmedPrefix == "" {
			continue
		}
		filtered = append(filtered, trimmedPrefix)
		if strings.EqualFold(trimmedPrefix, "MaterialService") {
			hasMaterialService = true
		}
	}
	if !hasMaterialService {
		filtered = append(filtered, "MaterialService")
	}
	return filtered
}
