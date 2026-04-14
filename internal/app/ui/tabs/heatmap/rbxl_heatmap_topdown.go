package heatmaptab

import (
	"bytes"
	"fmt"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"

	"joxblox/internal/app/loader"
	"joxblox/internal/app/report"
	"joxblox/internal/app/ui"
	"joxblox/internal/debug"
	"joxblox/internal/extractor"
	"joxblox/internal/format"
	"joxblox/internal/heatmap"
	"joxblox/internal/roblox/mesh"
)

const topDownRenderBgHex = "1c2228"

type topDownInstance struct {
	InstancePath string
	InstanceType string
	CenterX      float64
	CenterY      float64
	CenterZ      float64
	SizeX        float64
	SizeY        float64
	SizeZ        float64
	BasisSizeX   float64
	BasisSizeY   float64
	BasisSizeZ   float64
	YawDegrees   float64
	Rotation     [9]float64
	Color        color.NRGBA
	Transparency float64
	MeshRef      *heatmap.AssetReference
}

type topDownResolvedMesh struct {
	Preview       ui.MeshPreviewData
	Bounds        modelHeatmapMeshBounds
	TriangleCount uint32
}

func buildTopDownInstances(mapParts []extractor.MapRenderPartResult, refs []extractor.PositionedResult) []topDownInstance {
	meshRefsByPath := map[string][]heatmap.AssetReference{}
	for _, ref := range refs {
		propertyName := strings.ToLower(strings.TrimSpace(ref.PropertyName))
		instancePath, instanceType := report.PositionedRefTarget(ref)
		instancePath = strings.TrimSpace(instancePath)
		if instancePath == "" {
			continue
		}
		if report.NormalizeInstanceType(instanceType) != "meshpart" {
			continue
		}
		if !report.IsMeshContentProperty(propertyName) {
			continue
		}
		meshRefsByPath[instancePath] = append(meshRefsByPath[instancePath], heatmap.AssetReference{
			AssetID:    ref.ID,
			AssetInput: strings.TrimSpace(ref.RawContent),
		})
	}

	meshPartOccurrence := map[string]int{}
	instances := make([]topDownInstance, 0, len(mapParts))
	for _, part := range mapParts {
		if part.CenterX == nil || part.CenterY == nil || part.CenterZ == nil {
			continue
		}
		if part.SizeX == nil || part.SizeY == nil || part.SizeZ == nil {
			continue
		}
		sizeX := math.Abs(*part.SizeX)
		sizeY := math.Abs(*part.SizeY)
		sizeZ := math.Abs(*part.SizeZ)
		if sizeX <= 0 || sizeZ <= 0 {
			continue
		}

		red, green, blue := 163, 162, 165
		if part.ColorR != nil {
			red = format.Clamp(*part.ColorR, 0, 255)
		}
		if part.ColorG != nil {
			green = format.Clamp(*part.ColorG, 0, 255)
		}
		if part.ColorB != nil {
			blue = format.Clamp(*part.ColorB, 0, 255)
		}
		transparency := 0.0
		if part.Transparency != nil {
			transparency = format.Clamp(*part.Transparency, 0, 1)
		}

		inst := topDownInstance{
			InstancePath: strings.TrimSpace(part.InstancePath),
			InstanceType: strings.TrimSpace(part.InstanceType),
			CenterX:      *part.CenterX,
			CenterY:      *part.CenterY,
			CenterZ:      *part.CenterZ,
			SizeX:        sizeX,
			SizeY:        sizeY,
			SizeZ:        sizeZ,
			BasisSizeX:   positiveModelHeatmapBasisSize(part.BasisSizeX, sizeX),
			BasisSizeY:   positiveModelHeatmapBasisSize(part.BasisSizeY, sizeY),
			BasisSizeZ:   positiveModelHeatmapBasisSize(part.BasisSizeZ, sizeZ),
			YawDegrees:   dereferenceModelHeatmapFloat(part.YawDegrees),
			Rotation:     modelHeatmapRotationFromPart(part),
			Color:        color.NRGBA{R: uint8(red), G: uint8(green), B: uint8(blue), A: 255},
			Transparency: transparency,
		}

		if report.NormalizeInstanceType(part.InstanceType) == "meshpart" {
			refsForPath := meshRefsByPath[inst.InstancePath]
			if len(refsForPath) > 0 {
				occurrenceIndex := meshPartOccurrence[inst.InstancePath]
				meshPartOccurrence[inst.InstancePath] = occurrenceIndex + 1
				ref := refsForPath[0]
				if occurrenceIndex < len(refsForPath) {
					ref = refsForPath[occurrenceIndex]
				}
				inst.MeshRef = &ref
			}
		}

		instances = append(instances, inst)
	}
	return instances
}

func resolveTopDownMeshes(instances []topDownInstance, onProgress func(done int, total int)) map[string]topDownResolvedMesh {
	uniqueRefs := map[string]heatmap.AssetReference{}
	for _, inst := range instances {
		if inst.MeshRef == nil {
			continue
		}
		key := extractor.AssetReferenceKey(inst.MeshRef.AssetID, inst.MeshRef.AssetInput)
		uniqueRefs[key] = *inst.MeshRef
	}
	if len(uniqueRefs) == 0 {
		return nil
	}
	refs := make([]heatmap.AssetReference, 0, len(uniqueRefs))
	for _, ref := range uniqueRefs {
		refs = append(refs, ref)
	}

	return loader.RunResolveWorkers(
		refs,
		func(ref heatmap.AssetReference) string {
			return extractor.AssetReferenceKey(ref.AssetID, ref.AssetInput)
		},
		func(reference heatmap.AssetReference) topDownResolvedMesh {
			resolved := topDownResolvedMesh{}
			previewResult, previewErr := loader.LoadAssetStatsPreviewForReference(reference.AssetID, reference.AssetInput)
			if previewErr != nil || previewResult == nil || len(previewResult.DownloadBytes) == 0 {
				return resolved
			}
			if meshInfo, meshErr := mesh.ParseHeader(previewResult.DownloadBytes); meshErr == nil {
				resolved.TriangleCount = meshInfo.NumFaces
			}
			if previewData, meshPreviewErr := ui.ExtractMeshPreviewFromBytesWithLimit(previewResult.DownloadBytes, ui.MaxMeshPreviewTriangles); meshPreviewErr == nil {
				resolved.Preview = previewData
				resolved.Bounds = computeModelHeatmapMeshBounds(previewData.RawPositions)
				if resolved.TriangleCount == 0 {
					resolved.TriangleCount = previewData.TriangleCount
				}
			}
			return resolved
		},
		1,
		onProgress,
		func() bool { return false },
	)
}

const topDownMaxVerticesPerBatch = 60000

func topDownInstanceBounds(instances []topDownInstance) (minX, maxX, minZ, maxZ, maxY float64) {
	if len(instances) == 0 {
		return -1, 1, -1, 1, 0
	}
	first := instances[0]
	halfX := first.SizeX / 2
	halfZ := first.SizeZ / 2
	halfY := first.SizeY / 2
	minX = first.CenterX - halfX
	maxX = first.CenterX + halfX
	minZ = first.CenterZ - halfZ
	maxZ = first.CenterZ + halfZ
	maxY = first.CenterY + halfY
	for _, inst := range instances[1:] {
		hx := inst.SizeX / 2
		hz := inst.SizeZ / 2
		hy := inst.SizeY / 2
		minX = math.Min(minX, inst.CenterX-hx)
		maxX = math.Max(maxX, inst.CenterX+hx)
		minZ = math.Min(minZ, inst.CenterZ-hz)
		maxZ = math.Max(maxZ, inst.CenterZ+hz)
		maxY = math.Max(maxY, inst.CenterY+hy)
	}
	return
}

func buildTopDownSceneBatches(instances []topDownInstance, resolved map[string]topDownResolvedMesh) []ui.MeshPreviewBatchData {
	var batches []ui.MeshPreviewBatchData
	current := ui.MeshPreviewBatchData{}
	currentVertices := 0

	flushBatch := func() {
		if len(current.RawPositions) > 0 {
			batches = append(batches, current)
		}
		current = ui.MeshPreviewBatchData{}
		currentVertices = 0
	}

	for _, inst := range instances {
		if inst.Transparency >= 0.95 {
			continue
		}

		instVertexCount := 0
		if inst.MeshRef != nil {
			key := extractor.AssetReferenceKey(inst.MeshRef.AssetID, inst.MeshRef.AssetInput)
			if meshData, found := resolved[key]; found && meshData.Preview.HasRenderableGeometry() {
				instVertexCount = len(meshData.Preview.RawPositions) / 3
			}
		}
		if instVertexCount == 0 {
			instVertexCount = 8
		}

		if currentVertices > 0 && currentVertices+instVertexCount > topDownMaxVerticesPerBatch {
			flushBatch()
		}

		prevVertexCount := len(current.RawPositions) / 3
		hasMesh := false
		if inst.MeshRef != nil {
			key := extractor.AssetReferenceKey(inst.MeshRef.AssetID, inst.MeshRef.AssetInput)
			if meshData, found := resolved[key]; found && meshData.Preview.HasRenderableGeometry() {
				appendTopDownMeshGeometry(&current, inst, meshData)
				hasMesh = true
			}
		}
		if !hasMesh {
			appendTopDownBoxGeometry(&current, inst)
		}

		newVertexCount := len(current.RawPositions)/3 - prevVertexCount
		if newVertexCount > 0 {
			appendVertexColors(&current, inst.Color, newVertexCount)
			currentVertices += newVertexCount
		}
	}
	flushBatch()
	return batches
}

func appendTopDownMeshGeometry(batch *ui.MeshPreviewBatchData, inst topDownInstance, meshData topDownResolvedMesh) {
	baseVertexIndex := uint32(len(batch.RawPositions) / 3)
	bounds := meshData.Bounds
	rotation := inst.Rotation
	if rotation == ([9]float64{}) {
		rotation = modelHeatmapYawRotation(inst.YawDegrees)
	}

	for i := 0; i < len(meshData.Preview.RawPositions); i += 3 {
		localX := modelHeatmapAxisTransform(float64(meshData.Preview.RawPositions[i]), bounds.CenterX, bounds.SizeX, inst.SizeX)
		localY := modelHeatmapAxisTransform(float64(meshData.Preview.RawPositions[i+1]), bounds.CenterY, bounds.SizeY, inst.SizeY)
		localZ := modelHeatmapAxisTransform(float64(meshData.Preview.RawPositions[i+2]), bounds.CenterZ, bounds.SizeZ, inst.SizeZ)
		rotatedX, rotatedY, rotatedZ := applyModelHeatmapRotation(rotation, localX, localY, localZ)
		batch.RawPositions = append(batch.RawPositions,
			float32(rotatedX+inst.CenterX),
			float32(rotatedY+inst.CenterY),
			float32(rotatedZ+inst.CenterZ),
		)
	}
	for _, index := range meshData.Preview.RawIndices {
		batch.RawIndices = append(batch.RawIndices, baseVertexIndex+index)
	}
}

func appendTopDownBoxGeometry(batch *ui.MeshPreviewBatchData, inst topDownInstance) {
	halfX := inst.SizeX / 2
	halfY := inst.SizeY / 2
	halfZ := inst.SizeZ / 2

	rotation := inst.Rotation
	if rotation == ([9]float64{}) {
		rotation = modelHeatmapYawRotation(inst.YawDegrees)
	}

	localCorners := [8][3]float64{
		{-halfX, -halfY, -halfZ},
		{halfX, -halfY, -halfZ},
		{halfX, halfY, -halfZ},
		{-halfX, halfY, -halfZ},
		{-halfX, -halfY, halfZ},
		{halfX, -halfY, halfZ},
		{halfX, halfY, halfZ},
		{-halfX, halfY, halfZ},
	}

	baseVertex := uint32(len(batch.RawPositions) / 3)
	for _, corner := range localCorners {
		rx, ry, rz := applyModelHeatmapRotation(rotation, corner[0], corner[1], corner[2])
		batch.RawPositions = append(batch.RawPositions,
			float32(rx+inst.CenterX),
			float32(ry+inst.CenterY),
			float32(rz+inst.CenterZ),
		)
	}

	boxIndices := [36]uint32{
		0, 1, 2, 0, 2, 3, // front
		5, 4, 7, 5, 7, 6, // back
		4, 0, 3, 4, 3, 7, // left
		1, 5, 6, 1, 6, 2, // right
		3, 2, 6, 3, 6, 7, // top
		4, 5, 1, 4, 1, 0, // bottom
	}
	for _, idx := range boxIndices {
		batch.RawIndices = append(batch.RawIndices, baseVertex+idx)
	}
}

func appendVertexColors(batch *ui.MeshPreviewBatchData, c color.NRGBA, count int) {
	for range count {
		batch.RawColors = append(batch.RawColors, c.R, c.G, c.B, 255)
	}
}

func RenderTopDownMapImageFromParts(
	mapParts []extractor.MapRenderPartResult,
	refs []extractor.PositionedResult,
	width int,
	height int,
	onProgress func(done int, total int),
) ([]byte, error) {
	instances := buildTopDownInstances(mapParts, refs)
	if len(instances) == 0 {
		return nil, fmt.Errorf("no renderable parts for top-down view")
	}

	resolved := resolveTopDownMeshes(instances, onProgress)
	batches := buildTopDownSceneBatches(instances, resolved)
	if len(batches) == 0 {
		return nil, fmt.Errorf("no scene batches were built")
	}

	meshPartCount := 0
	boxPartCount := 0
	for _, inst := range instances {
		if inst.MeshRef != nil {
			meshPartCount++
		} else {
			boxPartCount++
		}
	}
	totalVerts := 0
	for _, b := range batches {
		totalVerts += len(b.RawPositions) / 3
	}
	debug.Logf("top-down render: %d batches, %d mesh instances, %d box instances, %d total vertices from %d map parts",
		len(batches), meshPartCount, boxPartCount, totalVerts, len(mapParts))

	minX, maxX, minZ, maxZ, maxY := topDownInstanceBounds(instances)
	centerX := (minX + maxX) / 2
	centerZ := (minZ + maxZ) / 2
	worldWidth := maxX - minX
	worldHeight := maxZ - minZ
	if worldWidth <= 0 {
		worldWidth = 2
	}
	if worldHeight <= 0 {
		worldHeight = 2
	}

	cameraY := maxY + 500

	contentW := float64(width - rbxlHeatmapPadding*2)
	contentH := float64(height - rbxlHeatmapPadding*2)
	orthoHalfW := (worldWidth / 2) * (float64(width) / contentW)
	orthoHalfH := (worldHeight / 2) * (float64(height) / contentH)

	renderW := width
	renderH := height
	if orthoHalfW > orthoHalfH {
		renderH = int(math.Round(float64(renderW) * orthoHalfH / orthoHalfW))
	} else {
		renderW = int(math.Round(float64(renderH) * orthoHalfW / orthoHalfH))
	}
	if renderW < 1 {
		renderW = 1
	}
	if renderH < 1 {
		renderH = 1
	}

	debug.Logf("top-down camera: center=(%.1f, %.1f) world=%.1fx%.1f orthoHalf=(%.1f, %.1f) render=%dx%d cameraY=%.1f",
		centerX, centerZ, worldWidth, worldHeight, orthoHalfW, orthoHalfH, renderW, renderH, cameraY)

	img, renderErr := ui.RenderTopDownMapImage(batches, centerX, centerZ, orthoHalfW, orthoHalfH, cameraY, renderW, renderH, topDownRenderBgHex)
	if renderErr != nil {
		return nil, fmt.Errorf("3D top-down render failed: %w", renderErr)
	}

	debugPath := filepath.Join(os.TempDir(), "joxblox-heatmap-topdown-debug.png")
	if f, err := os.Create(debugPath); err == nil {
		png.Encode(f, img)
		f.Close()
		debug.Logf("top-down debug image saved to %s", debugPath)
	}

	var buffer bytes.Buffer
	encoder := png.Encoder{CompressionLevel: png.BestSpeed}
	if encodeErr := encoder.Encode(&buffer, img); encodeErr != nil {
		return nil, fmt.Errorf("PNG encode failed: %w", encodeErr)
	}
	return buffer.Bytes(), nil
}
