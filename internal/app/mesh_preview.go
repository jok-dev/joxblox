package app

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"sync/atomic"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

const maxMeshPreviewTriangles = 20000
const minMeshPreviewRenderDimension = 32

type meshPreviewVector struct {
	X float64
	Y float64
	Z float64
}

type meshPreviewData struct {
	Positions            []meshPreviewVector
	Indices              []uint32
	TriangleCount        uint32
	PreviewTriangleCount uint32
}

type meshPreviewWidget struct {
	widget.BaseWidget
	background  *canvas.Rectangle
	image       *canvas.Image
	data        meshPreviewData
	yaw         float64
	pitch       float64
	zoom        float64
	renderToken atomic.Uint64
}

type meshPreviewProjectedVertex struct {
	X float64
	Y float64
	Z float64
}

func newMeshPreviewWidget() *meshPreviewWidget {
	viewer := &meshPreviewWidget{
		background: canvas.NewRectangle(color.NRGBA{R: 14, G: 17, B: 22, A: 255}),
		image:      canvas.NewImageFromImage(nil),
		yaw:        -0.35,
		pitch:      0.3,
		zoom:       1.0,
	}
	viewer.image.FillMode = canvas.ImageFillStretch
	viewer.image.ScaleMode = canvas.ImageScaleFastest
	viewer.ExtendBaseWidget(viewer)
	return viewer
}

func (viewer *meshPreviewWidget) CreateRenderer() fyne.WidgetRenderer {
	content := container.NewWithoutLayout(viewer.background, viewer.image)
	return widget.NewSimpleRenderer(content)
}

func (viewer *meshPreviewWidget) MinSize() fyne.Size {
	return fyne.NewSize(previewWidth, previewHeight)
}

func (viewer *meshPreviewWidget) Resize(size fyne.Size) {
	viewer.BaseWidget.Resize(size)
	viewer.background.Resize(size)
	viewer.background.Move(fyne.NewPos(0, 0))
	viewer.image.Resize(size)
	viewer.image.Move(fyne.NewPos(0, 0))
	viewer.render()
}

func (viewer *meshPreviewWidget) Dragged(event *fyne.DragEvent) {
	if viewer == nil || event == nil || len(viewer.data.Positions) == 0 || len(viewer.data.Indices) == 0 {
		return
	}
	viewer.yaw += float64(event.Dragged.DX) * 0.0125
	viewer.pitch -= float64(event.Dragged.DY) * 0.0125
	viewer.pitch = clampFloat64(viewer.pitch, -1.35, 1.35)
	viewer.render()
}

func (viewer *meshPreviewWidget) DragEnd() {}

func (viewer *meshPreviewWidget) Scrolled(event *fyne.ScrollEvent) {
	if viewer == nil || event == nil || len(viewer.data.Positions) == 0 || len(viewer.data.Indices) == 0 {
		return
	}
	if event.Scrolled.DY > 0 {
		viewer.SetZoom(viewer.zoom * 1.1)
		return
	}
	if event.Scrolled.DY < 0 {
		viewer.SetZoom(viewer.zoom / 1.1)
	}
}

func (viewer *meshPreviewWidget) Clear() {
	viewer.renderToken.Add(1)
	viewer.data = meshPreviewData{}
	viewer.image.Image = nil
	viewer.image.Refresh()
}

func (viewer *meshPreviewWidget) SetData(data meshPreviewData) {
	viewer.data = data
	viewer.yaw = -0.35
	viewer.pitch = 0.3
	viewer.zoom = 1.0
	viewer.render()
}

func (viewer *meshPreviewWidget) SetBackground(fill color.Color) {
	if viewer == nil || viewer.background == nil {
		return
	}
	viewer.background.FillColor = fill
	viewer.background.Refresh()
	viewer.render()
}

func (viewer *meshPreviewWidget) SetZoom(nextZoom float64) {
	if viewer == nil {
		return
	}
	viewer.zoom = clampFloat64(nextZoom, 0.35, 5.0)
	viewer.render()
}

func (viewer *meshPreviewWidget) render() {
	if viewer == nil {
		return
	}
	size := viewer.Size()
	if size.Width < minMeshPreviewRenderDimension || size.Height < minMeshPreviewRenderDimension {
		return
	}
	width := int(math.Max(1, float64(size.Width)))
	height := int(math.Max(1, float64(size.Height)))
	if len(viewer.data.Positions) == 0 || len(viewer.data.Indices) == 0 {
		viewer.renderToken.Add(1)
		viewer.image.Image = nil
		viewer.image.Refresh()
		return
	}
	dataSnapshot := viewer.data
	yawSnapshot := viewer.yaw
	pitchSnapshot := viewer.pitch
	zoomSnapshot := viewer.zoom
	backgroundSnapshot := color.NRGBAModel.Convert(viewer.background.FillColor).(color.NRGBA)
	renderID := viewer.renderToken.Add(1)
	go func() {
		renderedImage := renderMeshPreviewImage(
			dataSnapshot,
			width,
			height,
			yawSnapshot,
			pitchSnapshot,
			zoomSnapshot,
			backgroundSnapshot,
		)
		fyne.Do(func() {
			if viewer == nil || viewer.renderToken.Load() != renderID {
				return
			}
			viewer.image.Image = renderedImage
			viewer.image.Refresh()
		})
	}()
}

func buildMeshPreviewData(positions []float32, indices []uint32, triangleCount uint32, previewTriangleCount uint32) (meshPreviewData, error) {
	if len(positions) < 9 || len(indices) < 3 {
		return meshPreviewData{}, fmt.Errorf("mesh preview is empty")
	}
	if len(positions)%3 != 0 {
		return meshPreviewData{}, fmt.Errorf("mesh preview positions are not XYZ triplets")
	}
	if len(indices)%3 != 0 {
		return meshPreviewData{}, fmt.Errorf("mesh preview indices are not triangle triplets")
	}

	positionCount := len(positions) / 3
	normalized := make([]meshPreviewVector, 0, positionCount)
	minimum := meshPreviewVector{X: math.MaxFloat64, Y: math.MaxFloat64, Z: math.MaxFloat64}
	maximum := meshPreviewVector{X: -math.MaxFloat64, Y: -math.MaxFloat64, Z: -math.MaxFloat64}
	for index := 0; index < len(positions); index += 3 {
		vertex := meshPreviewVector{
			X: float64(positions[index]),
			Y: float64(positions[index+1]),
			Z: float64(positions[index+2]),
		}
		if math.IsNaN(vertex.X) || math.IsNaN(vertex.Y) || math.IsNaN(vertex.Z) {
			return meshPreviewData{}, fmt.Errorf("mesh preview contains invalid coordinates")
		}
		minimum.X = math.Min(minimum.X, vertex.X)
		minimum.Y = math.Min(minimum.Y, vertex.Y)
		minimum.Z = math.Min(minimum.Z, vertex.Z)
		maximum.X = math.Max(maximum.X, vertex.X)
		maximum.Y = math.Max(maximum.Y, vertex.Y)
		maximum.Z = math.Max(maximum.Z, vertex.Z)
		normalized = append(normalized, vertex)
	}

	center := meshPreviewVector{
		X: (minimum.X + maximum.X) / 2,
		Y: (minimum.Y + maximum.Y) / 2,
		Z: (minimum.Z + maximum.Z) / 2,
	}
	radius := 0.0
	for index := range normalized {
		normalized[index].X -= center.X
		normalized[index].Y -= center.Y
		normalized[index].Z -= center.Z
		radius = math.Max(radius, vectorLength(normalized[index]))
	}
	if radius <= 0 {
		radius = 1
	}
	for index := range normalized {
		normalized[index].X /= radius
		normalized[index].Y /= radius
		normalized[index].Z /= radius
	}

	for _, vertexIndex := range indices {
		if int(vertexIndex) >= len(normalized) {
			return meshPreviewData{}, fmt.Errorf("mesh preview index %d out of range", vertexIndex)
		}
	}

	return meshPreviewData{
		Positions:            normalized,
		Indices:              append([]uint32(nil), indices...),
		TriangleCount:        triangleCount,
		PreviewTriangleCount: previewTriangleCount,
	}, nil
}

func renderMeshPreviewImage(data meshPreviewData, width int, height int, yaw float64, pitch float64, zoom float64, background color.Color) image.Image {
	frame := image.NewRGBA(image.Rect(0, 0, width, height))
	fill := color.NRGBAModel.Convert(background).(color.NRGBA)
	fillImage(frame, fill)

	projected, rotated := projectMeshPreviewVertices(data.Positions, width, height, yaw, pitch, zoom)
	if len(projected) == 0 {
		return frame
	}

	zBuffer := make([]float64, width*height)
	for index := range zBuffer {
		zBuffer[index] = math.Inf(1)
	}

	lightDirection := normalizeMeshVector(meshPreviewVector{X: -0.4, Y: 0.7, Z: -1.0})
	baseShade := color.NRGBA{R: 112, G: 173, B: 255, A: 255}

	for triangle := 0; triangle+2 < len(data.Indices); triangle += 3 {
		firstIndex := int(data.Indices[triangle])
		secondIndex := int(data.Indices[triangle+1])
		thirdIndex := int(data.Indices[triangle+2])
		if firstIndex < 0 || secondIndex < 0 || thirdIndex < 0 {
			continue
		}
		if firstIndex >= len(projected) || secondIndex >= len(projected) || thirdIndex >= len(projected) {
			continue
		}

		rotatedFirst := rotated[firstIndex]
		rotatedSecond := rotated[secondIndex]
		rotatedThird := rotated[thirdIndex]
		normal := normalizeMeshVector(crossMeshVector(
			subtractMeshVector(rotatedSecond, rotatedFirst),
			subtractMeshVector(rotatedThird, rotatedFirst),
		))
		if normal.Z > 0 {
			normal = meshPreviewVector{
				X: -normal.X,
				Y: -normal.Y,
				Z: -normal.Z,
			}
		}

		lightAmount := math.Max(0, dotMeshVector(normal, lightDirection))
		intensity := 0.2 + 0.8*lightAmount
		shaded := shadeMeshColor(baseShade, intensity)

		rasterizeMeshTriangle(frame, zBuffer, width, height,
			projected[firstIndex],
			projected[secondIndex],
			projected[thirdIndex],
			shaded,
		)
	}

	return frame
}

func projectMeshPreviewVertices(vertices []meshPreviewVector, width int, height int, yaw float64, pitch float64, zoom float64) ([]meshPreviewProjectedVertex, []meshPreviewVector) {
	if len(vertices) == 0 {
		return nil, nil
	}
	sinYaw, cosYaw := math.Sincos(yaw)
	sinPitch, cosPitch := math.Sincos(pitch)
	scale := math.Min(float64(width), float64(height)) * 0.78 * clampFloat64(zoom, 0.35, 5.0)
	cameraDistance := 3.0
	projected := make([]meshPreviewProjectedVertex, 0, len(vertices))
	rotated := make([]meshPreviewVector, 0, len(vertices))
	for _, vertex := range vertices {
		yawRotated := meshPreviewVector{
			X: vertex.X*cosYaw + vertex.Z*sinYaw,
			Y: vertex.Y,
			Z: -vertex.X*sinYaw + vertex.Z*cosYaw,
		}
		pitchRotated := meshPreviewVector{
			X: yawRotated.X,
			Y: yawRotated.Y*cosPitch - yawRotated.Z*sinPitch,
			Z: yawRotated.Y*sinPitch + yawRotated.Z*cosPitch,
		}
		depth := pitchRotated.Z + cameraDistance
		if depth <= 0.001 {
			depth = 0.001
		}
		projected = append(projected, meshPreviewProjectedVertex{
			X: float64(width)/2 + (pitchRotated.X/depth)*scale,
			Y: float64(height)/2 - (pitchRotated.Y/depth)*scale,
			Z: depth,
		})
		rotated = append(rotated, pitchRotated)
	}
	return projected, rotated
}

func rasterizeMeshTriangle(frame *image.RGBA, zBuffer []float64, width int, height int, first meshPreviewProjectedVertex, second meshPreviewProjectedVertex, third meshPreviewProjectedVertex, fill color.NRGBA) {
	minX := int(math.Max(0, math.Floor(math.Min(first.X, math.Min(second.X, third.X)))))
	maxX := int(math.Min(float64(width-1), math.Ceil(math.Max(first.X, math.Max(second.X, third.X)))))
	minY := int(math.Max(0, math.Floor(math.Min(first.Y, math.Min(second.Y, third.Y)))))
	maxY := int(math.Min(float64(height-1), math.Ceil(math.Max(first.Y, math.Max(second.Y, third.Y)))))
	if minX > maxX || minY > maxY {
		return
	}

	triangleArea := edgeFunction(first.X, first.Y, second.X, second.Y, third.X, third.Y)
	if math.Abs(triangleArea) < 1e-6 {
		return
	}
	sign := 1.0
	if triangleArea < 0 {
		sign = -1
		triangleArea = -triangleArea
	}
	edgeTolerance := math.Max(1e-6, triangleArea*1e-6)

	for y := minY; y <= maxY; y++ {
		for x := minX; x <= maxX; x++ {
			sampleX := float64(x) + 0.5
			sampleY := float64(y) + 0.5
			weightFirst := edgeFunction(second.X, second.Y, third.X, third.Y, sampleX, sampleY) * sign
			weightSecond := edgeFunction(third.X, third.Y, first.X, first.Y, sampleX, sampleY) * sign
			weightThird := edgeFunction(first.X, first.Y, second.X, second.Y, sampleX, sampleY) * sign
			if weightFirst < -edgeTolerance || weightSecond < -edgeTolerance || weightThird < -edgeTolerance {
				continue
			}

			baryFirst := weightFirst / triangleArea
			barySecond := weightSecond / triangleArea
			baryThird := weightThird / triangleArea
			depth := baryFirst*first.Z + barySecond*second.Z + baryThird*third.Z
			bufferIndex := y*width + x
			if depth >= zBuffer[bufferIndex] {
				continue
			}
			zBuffer[bufferIndex] = depth
			frame.Set(x, y, fill)
		}
	}
}

func shadeMeshColor(base color.NRGBA, intensity float64) color.NRGBA {
	intensity = clampFloat64(intensity, 0, 1)
	return color.NRGBA{
		R: uint8(clampFloat64(float64(base.R)*intensity, 0, 255)),
		G: uint8(clampFloat64(float64(base.G)*intensity, 0, 255)),
		B: uint8(clampFloat64(float64(base.B)*intensity, 0, 255)),
		A: 255,
	}
}

func fillImage(target *image.RGBA, fill color.NRGBA) {
	if target == nil {
		return
	}
	for y := target.Rect.Min.Y; y < target.Rect.Max.Y; y++ {
		for x := target.Rect.Min.X; x < target.Rect.Max.X; x++ {
			target.Set(x, y, fill)
		}
	}
}

func subtractMeshVector(left meshPreviewVector, right meshPreviewVector) meshPreviewVector {
	return meshPreviewVector{
		X: left.X - right.X,
		Y: left.Y - right.Y,
		Z: left.Z - right.Z,
	}
}

func crossMeshVector(left meshPreviewVector, right meshPreviewVector) meshPreviewVector {
	return meshPreviewVector{
		X: left.Y*right.Z - left.Z*right.Y,
		Y: left.Z*right.X - left.X*right.Z,
		Z: left.X*right.Y - left.Y*right.X,
	}
}

func dotMeshVector(left meshPreviewVector, right meshPreviewVector) float64 {
	return left.X*right.X + left.Y*right.Y + left.Z*right.Z
}

func vectorLength(value meshPreviewVector) float64 {
	return math.Sqrt(dotMeshVector(value, value))
}

func normalizeMeshVector(value meshPreviewVector) meshPreviewVector {
	length := vectorLength(value)
	if length <= 0 {
		return meshPreviewVector{}
	}
	return meshPreviewVector{
		X: value.X / length,
		Y: value.Y / length,
		Z: value.Z / length,
	}
}

func edgeFunction(ax float64, ay float64, bx float64, by float64, px float64, py float64) float64 {
	return (px-ax)*(by-ay) - (py-ay)*(bx-ax)
}

func clampFloat64(value float64, minimum float64, maximum float64) float64 {
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}
	return value
}
