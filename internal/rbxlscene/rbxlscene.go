// Package rbxlscene holds the Fyne-free scene model and spatial cell-gridding
// helpers shared between the heatmap UI and the report-generation engine. The
// types and functions here used to live inside the Fyne heatmap tab; they were
// extracted so the web (Wails) backend can reuse the exact same grid math
// without pulling in Fyne.
package rbxlscene

import (
	"fmt"
	"image/color"
	"math"
	"sort"
	"strings"

	"joxblox/internal/app/loader"
	"joxblox/internal/extractor"
	"joxblox/internal/format"
	"joxblox/internal/heatmap"
)

// defaultGridDivisions matches the heatmap tab's default when a caller passes a
// non-positive division count to BuildHeatmapCells.
const defaultGridDivisions = 36

// RBXLHeatmapPoint is one positioned asset reference in scene space.
type RBXLHeatmapPoint struct {
	AssetID      int64
	AssetInput   string
	InstanceType string
	InstanceName string
	InstancePath string
	PropertyName string
	Stats        heatmap.AssetStats
	X            float64
	Y            float64
	Z            float64
}

// RBXLHeatmapMapPart is one rendered Part/MeshPart with a world position and
// footprint, used for spatial gridding and surface-area estimates.
type RBXLHeatmapMapPart struct {
	InstanceType string
	InstanceName string
	InstancePath string
	MaterialKey  string
	CenterX      float64
	CenterY      float64
	CenterZ      float64
	SizeX        float64
	SizeY        float64
	SizeZ        float64
	YawDegrees   float64
	Color        color.NRGBA
	Transparency float64
}

func (p RBXLHeatmapMapPart) GetInstancePath() string { return p.InstancePath }

func (p RBXLHeatmapMapPart) GetDimensions() (float64, float64, float64) {
	return p.SizeX, p.SizeY, p.SizeZ
}

// RBXLHeatmapScene bundles the points/parts plus the computed grid for a file
// (or a diff of two files).
type RBXLHeatmapScene struct {
	Points         []RBXLHeatmapPoint
	ComparePoints  []RBXLHeatmapPoint
	MapParts       []RBXLHeatmapMapPart
	Cells          []heatmap.Cell
	CellIndexByKey map[string]int
	CellSizeWorld  float64
	ColumnCount    int
	RowCount       int
	GridDivisions  int
	MinimumX       float64
	MaximumX       float64
	MinimumZ       float64
	MaximumZ       float64
	MinimumY       float64
	MaximumY       float64
	MaxCellBytes   int64
	DiffMode       bool
	DiffLabel      string
}

// HeatmapCellKey is the canonical "row:column" key for a cell.
func HeatmapCellKey(row int, column int) string {
	return fmt.Sprintf("%d:%d", row, column)
}

// BuildHeatmapCells grids the scene's points into square cells, accumulating
// per-cell stats. It returns the cells plus the world cell size, column/row
// counts, and the largest absolute cell byte total. The scene's CellIndexByKey
// is populated as a side effect.
func BuildHeatmapCells(scene *RBXLHeatmapScene, gridDivisions int) ([]heatmap.Cell, float64, int, int, int64) {
	if scene == nil || (len(scene.Points) == 0 && len(scene.ComparePoints) == 0) {
		return nil, 0, 0, 0, 0
	}
	if gridDivisions <= 0 {
		gridDivisions = defaultGridDivisions
	}
	rangeX := scene.MaximumX - scene.MinimumX
	rangeZ := scene.MaximumZ - scene.MinimumZ
	maxRange := math.Max(rangeX, rangeZ)
	if maxRange <= 0 {
		maxRange = 1
	}
	cellSizeWorld := maxRange / float64(gridDivisions)
	if cellSizeWorld <= 0 {
		cellSizeWorld = 1
	}
	columnCount := max(1, int(math.Ceil(rangeX/cellSizeWorld)))
	rowCount := max(1, int(math.Ceil(rangeZ/cellSizeWorld)))
	type heatmapCellAccumulator struct {
		cell                 *heatmap.Cell
		baseSeenAssets       map[int64]struct{}
		compareSeenAssets    map[int64]struct{}
		baseSeenInstances    map[string]struct{}
		compareSeenInstances map[string]struct{}
	}
	cellByKey := map[string]*heatmapCellAccumulator{}
	maxCellBytes := int64(0)
	accumulatePoints := func(points []RBXLHeatmapPoint, apply func(accumulator *heatmapCellAccumulator, point RBXLHeatmapPoint)) {
		for _, point := range points {
			column := format.Clamp(int(math.Floor((point.X-scene.MinimumX)/cellSizeWorld)), 0, columnCount-1)
			row := format.Clamp(int(math.Floor((point.Z-scene.MinimumZ)/cellSizeWorld)), 0, rowCount-1)
			cellKey := fmt.Sprintf("%d:%d", row, column)
			accumulator, found := cellByKey[cellKey]
			if !found {
				accumulator = &heatmapCellAccumulator{
					cell: &heatmap.Cell{
						Row:      row,
						Column:   column,
						MinimumX: scene.MinimumX + float64(column)*cellSizeWorld,
						MaximumX: scene.MinimumX + float64(column+1)*cellSizeWorld,
						MinimumZ: scene.MinimumZ + float64(row)*cellSizeWorld,
						MaximumZ: scene.MinimumZ + float64(row+1)*cellSizeWorld,
					},
					baseSeenAssets:       map[int64]struct{}{},
					compareSeenAssets:    map[int64]struct{}{},
					baseSeenInstances:    map[string]struct{}{},
					compareSeenInstances: map[string]struct{}{},
				}
				cellByKey[cellKey] = accumulator
			}
			apply(accumulator, point)
		}
	}
	accumulateTotals := func(totals *heatmap.Totals, point RBXLHeatmapPoint, delta int64, firstAssetInSquare bool, firstInstanceInSquare bool) {
		totals.ReferenceCount += delta
		if firstInstanceInSquare {
			switch point.InstanceType {
			case "MeshPart":
				totals.MeshPartCount += delta
			case "Part":
				totals.PartCount += delta
			}
		}
		totals.TriangleCount += int64(point.Stats.TriangleCount) * delta
		if !firstAssetInSquare {
			return
		}
		totals.UniqueAssetCount += delta
		if heatmapPointHasTextureContent(point) {
			totals.UniqueTextureCount += delta
		}
		if heatmapPointHasMeshContent(point) {
			totals.UniqueMeshCount += delta
		}
		totals.TextureBytes += int64(point.Stats.TextureBytes) * delta
		totals.MeshBytes += int64(point.Stats.MeshBytes) * delta
		totals.TotalBytes += int64(point.Stats.TotalBytes) * delta
		totals.PixelCount += point.Stats.PixelCount * delta
		if point.Stats.PixelCount > 0 {
			if loader.ClassifyAsBC3(point.Stats.HasAlphaChannel, point.Stats.NonOpaqueAlphaPixels, point.PropertyName) {
				totals.BC3PixelCount += point.Stats.PixelCount * delta
			} else {
				totals.BC1PixelCount += point.Stats.PixelCount * delta
			}
		}
	}
	accumulatePoints(scene.Points, func(accumulator *heatmapCellAccumulator, point RBXLHeatmapPoint) {
		_, seenAssetInBase := accumulator.baseSeenAssets[point.AssetID]
		if !seenAssetInBase {
			accumulator.baseSeenAssets[point.AssetID] = struct{}{}
		}
		instancePath := point.InstancePath
		_, seenInstanceInBase := accumulator.baseSeenInstances[instancePath]
		if !seenInstanceInBase && instancePath != "" {
			accumulator.baseSeenInstances[instancePath] = struct{}{}
		}
		firstInst := !seenInstanceInBase && instancePath != ""
		if scene.DiffMode {
			accumulateTotals(&accumulator.cell.BaseStats, point, 1, !seenAssetInBase, firstInst)
			accumulateTotals(&accumulator.cell.DeltaStats, point, -1, !seenAssetInBase, firstInst)
			accumulator.cell.Stats = accumulator.cell.DeltaStats
		} else {
			accumulateTotals(&accumulator.cell.Stats, point, 1, !seenAssetInBase, firstInst)
		}
	})
	accumulatePoints(scene.ComparePoints, func(accumulator *heatmapCellAccumulator, point RBXLHeatmapPoint) {
		_, seenAssetInCompare := accumulator.compareSeenAssets[point.AssetID]
		if !seenAssetInCompare {
			accumulator.compareSeenAssets[point.AssetID] = struct{}{}
		}
		instancePath := point.InstancePath
		_, seenInstanceInCompare := accumulator.compareSeenInstances[instancePath]
		if !seenInstanceInCompare && instancePath != "" {
			accumulator.compareSeenInstances[instancePath] = struct{}{}
		}
		firstInst := !seenInstanceInCompare && instancePath != ""
		accumulateTotals(&accumulator.cell.Stats, point, 1, !seenAssetInCompare, firstInst)
		accumulateTotals(&accumulator.cell.DeltaStats, point, 1, !seenAssetInCompare, firstInst)
	})

	cells := make([]heatmap.Cell, 0, len(cellByKey))
	for _, accumulator := range cellByKey {
		cell := accumulator.cell
		if scene.DiffMode {
			cell.Stats = cell.DeltaStats
		}
		if format.AbsInt64(cell.Stats.TotalBytes) > maxCellBytes {
			maxCellBytes = format.AbsInt64(cell.Stats.TotalBytes)
		}
		cells = append(cells, *cell)
	}
	sort.Slice(cells, func(left int, right int) bool {
		if cells[left].Row == cells[right].Row {
			return cells[left].Column < cells[right].Column
		}
		return cells[left].Row < cells[right].Row
	})
	scene.CellIndexByKey = make(map[string]int, len(cells))
	for index, cell := range cells {
		scene.CellIndexByKey[HeatmapCellKey(cell.Row, cell.Column)] = index
	}
	return cells, cellSizeWorld, columnCount, rowCount, maxCellBytes
}

func heatmapPointHasTextureContent(point RBXLHeatmapPoint) bool {
	return point.Stats.TextureBytes > 0 || point.Stats.PixelCount > 0
}

func heatmapPointHasMeshContent(point RBXLHeatmapPoint) bool {
	return point.Stats.MeshBytes > 0 || point.Stats.TriangleCount > 0
}

// ConvertRustMapParts maps the rust extractor's positioned-part results into
// scene-space map parts, dropping any part missing a center or footprint.
func ConvertRustMapParts(parts []extractor.MapRenderPartResult) []RBXLHeatmapMapPart {
	converted := make([]RBXLHeatmapMapPart, 0, len(parts))
	for _, part := range parts {
		if part.CenterX == nil || part.CenterY == nil || part.CenterZ == nil {
			continue
		}
		if part.SizeX == nil || part.SizeY == nil || part.SizeZ == nil {
			continue
		}
		sizeX := *part.SizeX
		sizeY := *part.SizeY
		sizeZ := *part.SizeZ
		if sizeX <= 0 || sizeZ <= 0 {
			continue
		}
		yawDegrees := 0.0
		if part.YawDegrees != nil {
			yawDegrees = *part.YawDegrees
		}
		transparency := 0.0
		if part.Transparency != nil {
			transparency = format.Clamp(*part.Transparency, 0, 1)
		}
		red := 163
		green := 162
		blue := 165
		if part.ColorR != nil {
			red = format.Clamp(*part.ColorR, 0, 255)
		}
		if part.ColorG != nil {
			green = format.Clamp(*part.ColorG, 0, 255)
		}
		if part.ColorB != nil {
			blue = format.Clamp(*part.ColorB, 0, 255)
		}
		converted = append(converted, RBXLHeatmapMapPart{
			InstanceType: strings.TrimSpace(part.InstanceType),
			InstanceName: strings.TrimSpace(part.InstanceName),
			InstancePath: strings.TrimSpace(part.InstancePath),
			MaterialKey:  strings.TrimSpace(part.MaterialKey),
			CenterX:      *part.CenterX,
			CenterY:      *part.CenterY,
			CenterZ:      *part.CenterZ,
			SizeX:        sizeX,
			SizeY:        sizeY,
			SizeZ:        sizeZ,
			YawDegrees:   yawDegrees,
			Color: color.NRGBA{
				R: uint8(red),
				G: uint8(green),
				B: uint8(blue),
				A: 255,
			},
			Transparency: transparency,
		})
	}
	return converted
}

// HeatmapSceneBounds returns the (minX, maxX, minZ, maxZ) world bounds covering
// every point and the rotated footprint of every map part.
func HeatmapSceneBounds(points []RBXLHeatmapPoint, mapParts []RBXLHeatmapMapPart) (float64, float64, float64, float64) {
	hasBounds := false
	minX := 0.0
	maxX := 0.0
	minZ := 0.0
	maxZ := 0.0
	updateBounds := func(x float64, z float64) {
		if !hasBounds {
			minX, maxX = x, x
			minZ, maxZ = z, z
			hasBounds = true
			return
		}
		minX = math.Min(minX, x)
		maxX = math.Max(maxX, x)
		minZ = math.Min(minZ, z)
		maxZ = math.Max(maxZ, z)
	}
	for _, point := range points {
		updateBounds(point.X, point.Z)
	}
	for _, part := range mapParts {
		for _, corner := range mapPartWorldCorners(part) {
			updateBounds(corner.x, corner.z)
		}
	}
	if !hasBounds {
		return -1, 1, -1, 1
	}
	if minX == maxX {
		minX -= 1
		maxX += 1
	}
	if minZ == maxZ {
		minZ -= 1
		maxZ += 1
	}
	return minX, maxX, minZ, maxZ
}

type worldCorner struct {
	x float64
	z float64
}

func mapPartWorldCorners(part RBXLHeatmapMapPart) []worldCorner {
	halfX := part.SizeX / 2
	halfZ := part.SizeZ / 2
	angle := part.YawDegrees * (math.Pi / 180.0)
	cosAngle := math.Cos(angle)
	sinAngle := math.Sin(angle)
	localCorners := []worldCorner{
		{x: -halfX, z: -halfZ},
		{x: halfX, z: -halfZ},
		{x: halfX, z: halfZ},
		{x: -halfX, z: halfZ},
	}
	worldCorners := make([]worldCorner, 0, len(localCorners))
	for _, corner := range localCorners {
		worldCorners = append(worldCorners, worldCorner{
			x: part.CenterX + corner.x*cosAngle - corner.z*sinAngle,
			z: part.CenterZ + corner.x*sinAngle + corner.z*cosAngle,
		})
	}
	return worldCorners
}
