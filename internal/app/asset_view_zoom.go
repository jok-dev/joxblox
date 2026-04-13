package app

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"

	"joxblox/internal/format"
)

const (
	expandedBackgroundBlack = "Black"
	expandedBackgroundWhite = "White"
)

func newZoomPanImage(option previewDownloadOption) *zoomPanImage {
	viewer := &zoomPanImage{
		background: canvas.NewRectangle(color.Black),
		image:      canvas.NewImageFromResource(previewResourceForOption(option)),
		option:     option,
		zoom:       1.0,
		offsetX:    0,
		offsetY:    0,
	}
	viewer.image.FillMode = canvas.ImageFillStretch
	viewer.image.ScaleMode = canvas.ImageScaleFastest
	viewer.ExtendBaseWidget(viewer)
	return viewer
}

func zoomPanBackgroundColor(mode string) color.Color {
	switch mode {
	case expandedBackgroundWhite:
		return color.White
	default:
		return color.Black
	}
}

func (viewer *zoomPanImage) CreateRenderer() fyne.WidgetRenderer {
	content := container.NewWithoutLayout(viewer.background, viewer.image)
	return widget.NewSimpleRenderer(content)
}

func (viewer *zoomPanImage) MinSize() fyne.Size {
	return fyne.NewSize(240, 180)
}

func (viewer *zoomPanImage) Resize(size fyne.Size) {
	viewer.BaseWidget.Resize(size)
	viewer.updateLayout()
}

func (viewer *zoomPanImage) SetOption(option previewDownloadOption) {
	centerX, centerY := viewer.normalizedCenter()
	currentWidth, currentHeight := viewer.optionDimensions()
	nextWidth, nextHeight := previewOptionDimensions(option)
	viewer.option = option
	viewer.image.Resource = previewResourceForOption(option)
	if currentWidth > 0 && nextWidth > 0 {
		widthScale := float64(currentWidth) / float64(nextWidth)
		heightScale := 1.0
		if currentHeight > 0 && nextHeight > 0 {
			heightScale = float64(currentHeight) / float64(nextHeight)
		}
		viewer.zoom *= (widthScale + heightScale) / 2.0
		if viewer.zoom < 0.25 {
			viewer.zoom = 0.25
		}
		if viewer.zoom > 8.0 {
			viewer.zoom = 8.0
		}
	}
	viewer.setNormalizedCenter(centerX, centerY)
	viewer.updateLayout()
	viewer.image.Refresh()
}

func (viewer *zoomPanImage) SetZoom(nextZoom float64) {
	centerX, centerY := viewer.normalizedCenter()
	if nextZoom < 0.25 {
		nextZoom = 0.25
	}
	if nextZoom > 8.0 {
		nextZoom = 8.0
	}
	viewer.zoom = nextZoom
	viewer.setNormalizedCenter(centerX, centerY)
	viewer.updateLayout()
}

func (viewer *zoomPanImage) SetBackground(mode string) {
	if viewer == nil || viewer.background == nil {
		return
	}
	viewer.background.FillColor = zoomPanBackgroundColor(mode)
	viewer.background.Refresh()
}

func (viewer *zoomPanImage) SetHoverCallback(callback func(imageX float64, imageY float64, pointer fyne.Position, inside bool)) {
	viewer.hoverCallback = callback
}

func (viewer *zoomPanImage) SetTapCallback(callback func(imageX float64, imageY float64, pointer fyne.Position, inside bool)) {
	viewer.tapCallback = callback
}

func (viewer *zoomPanImage) Dragged(event *fyne.DragEvent) {
	viewer.offsetX += event.Dragged.DX
	viewer.offsetY += event.Dragged.DY
	viewer.updateLayout()
}

func (viewer *zoomPanImage) DragEnd() {}

func (viewer *zoomPanImage) Scrolled(event *fyne.ScrollEvent) {
	if event == nil {
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

func (viewer *zoomPanImage) MouseIn(event *desktop.MouseEvent) {
	viewer.handleHoverEvent(event)
}

func (viewer *zoomPanImage) MouseMoved(event *desktop.MouseEvent) {
	viewer.handleHoverEvent(event)
}

func (viewer *zoomPanImage) MouseOut() {
	if viewer.hoverCallback != nil {
		viewer.hoverCallback(0, 0, fyne.NewPos(0, 0), false)
	}
}

func (viewer *zoomPanImage) handleHoverEvent(event *desktop.MouseEvent) {
	if viewer == nil || viewer.hoverCallback == nil || event == nil {
		return
	}
	imageX, imageY, inside := viewer.imagePointForPointer(event.Position)
	viewer.hoverCallback(imageX, imageY, event.Position, inside)
}

func (viewer *zoomPanImage) Tapped(event *fyne.PointEvent) {
	if viewer == nil || viewer.tapCallback == nil || event == nil {
		return
	}
	imageX, imageY, inside := viewer.imagePointForPointer(event.Position)
	viewer.tapCallback(imageX, imageY, event.Position, inside)
}

func (viewer *zoomPanImage) TappedSecondary(_ *fyne.PointEvent) {}

func (viewer *zoomPanImage) imagePointForPointer(position fyne.Position) (float64, float64, bool) {
	if viewer == nil {
		return 0, 0, false
	}
	positionX, positionY, scaledWidth, scaledHeight := viewer.layoutMetrics()
	if scaledWidth <= 0 || scaledHeight <= 0 {
		return 0, 0, false
	}
	localX := float64(position.X - positionX)
	localY := float64(position.Y - positionY)
	if localX < 0 || localY < 0 || localX > float64(scaledWidth) || localY > float64(scaledHeight) {
		return 0, 0, false
	}
	imageWidth, imageHeight := viewer.optionDimensions()
	if imageWidth <= 0 || imageHeight <= 0 {
		return 0, 0, false
	}
	imageX := (localX / float64(scaledWidth)) * float64(imageWidth)
	imageY := (localY / float64(scaledHeight)) * float64(imageHeight)
	return imageX, imageY, true
}

func (viewer *zoomPanImage) updateLayout() {
	size := viewer.Size()
	viewer.background.Resize(size)
	viewer.background.Move(fyne.NewPos(0, 0))

	imageWidth := float32(viewer.option.width)
	imageHeight := float32(viewer.option.height)
	if imageWidth <= 0 {
		imageWidth = float32(previewWidth)
	}
	if imageHeight <= 0 {
		imageHeight = float32(previewHeight)
	}
	scaledWidth := imageWidth * float32(viewer.zoom)
	scaledHeight := imageHeight * float32(viewer.zoom)
	baseX := (size.Width - scaledWidth) / 2
	baseY := (size.Height - scaledHeight) / 2
	positionX := format.Clamp(baseX+viewer.offsetX, min(size.Width-scaledWidth, baseX), max(0, baseX))
	positionY := format.Clamp(baseY+viewer.offsetY, min(size.Height-scaledHeight, baseY), max(0, baseY))

	if scaledWidth <= size.Width {
		positionX = baseX
		viewer.offsetX = 0
	} else {
		viewer.offsetX = positionX - baseX
	}
	if scaledHeight <= size.Height {
		positionY = baseY
		viewer.offsetY = 0
	} else {
		viewer.offsetY = positionY - baseY
	}

	viewer.image.Resize(fyne.NewSize(scaledWidth, scaledHeight))
	viewer.image.Move(fyne.NewPos(positionX, positionY))
	canvas.Refresh(viewer)
}

func (viewer *zoomPanImage) normalizedCenter() (float32, float32) {
	size := viewer.Size()
	scaledWidth, scaledHeight := viewer.scaledDimensions()
	if scaledWidth <= 0 || scaledHeight <= 0 {
		return 0.5, 0.5
	}
	positionX, positionY, _, _ := viewer.layoutMetrics()
	centerX := (size.Width/2 - positionX) / scaledWidth
	centerY := (size.Height/2 - positionY) / scaledHeight
	return format.Clamp(centerX, 0, 1), format.Clamp(centerY, 0, 1)
}

func (viewer *zoomPanImage) setNormalizedCenter(centerX float32, centerY float32) {
	size := viewer.Size()
	scaledWidth, scaledHeight := viewer.scaledDimensions()
	baseX := (size.Width - scaledWidth) / 2
	baseY := (size.Height - scaledHeight) / 2
	desiredPositionX := size.Width/2 - format.Clamp(centerX, 0, 1)*scaledWidth
	desiredPositionY := size.Height/2 - format.Clamp(centerY, 0, 1)*scaledHeight
	viewer.offsetX = desiredPositionX - baseX
	viewer.offsetY = desiredPositionY - baseY
}

func (viewer *zoomPanImage) scaledDimensions() (float32, float32) {
	imageWidth, imageHeight := viewer.optionDimensions()
	return imageWidth * float32(viewer.zoom), imageHeight * float32(viewer.zoom)
}

func (viewer *zoomPanImage) layoutMetrics() (float32, float32, float32, float32) {
	size := viewer.Size()
	scaledWidth, scaledHeight := viewer.scaledDimensions()
	baseX := (size.Width - scaledWidth) / 2
	baseY := (size.Height - scaledHeight) / 2
	positionX := format.Clamp(baseX+viewer.offsetX, min(size.Width-scaledWidth, baseX), max(0, baseX))
	positionY := format.Clamp(baseY+viewer.offsetY, min(size.Height-scaledHeight, baseY), max(0, baseY))
	return positionX, positionY, scaledWidth, scaledHeight
}

func (viewer *zoomPanImage) optionDimensions() (float32, float32) {
	return previewOptionDimensions(viewer.option)
}

func previewOptionDimensions(option previewDownloadOption) (float32, float32) {
	imageWidth := float32(option.width)
	imageHeight := float32(option.height)
	if imageWidth <= 0 {
		imageWidth = float32(previewWidth)
	}
	if imageHeight <= 0 {
		imageHeight = float32(previewHeight)
	}
	return imageWidth, imageHeight
}
