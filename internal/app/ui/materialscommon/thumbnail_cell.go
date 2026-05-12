package materialscommon

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// ThumbnailCellMinDim is the minimum on-screen size for a thumbnail
// cell's image. Sized to fit alongside text columns at the table's
// default row height without forcing the row to grow.
const ThumbnailCellMinDim = 32

// NewThumbnailCell returns a fyne CanvasObject suitable as a Table
// CreateCell return value: a stacked container with a label on top of a
// canvas.Image. Use ThumbnailCellLabel / ThumbnailCellImage during
// UpdateCell to access the inner widgets.
func NewThumbnailCell() fyne.CanvasObject {
	img := canvas.NewImageFromImage(nil)
	img.FillMode = canvas.ImageFillContain
	img.ScaleMode = canvas.ImageScaleFastest
	img.SetMinSize(fyne.NewSize(ThumbnailCellMinDim, ThumbnailCellMinDim))
	label := widget.NewLabel("")
	return container.NewStack(label, img)
}

// ThumbnailCellLabel returns the label widget inside a cell built by
// NewThumbnailCell, or nil if the object isn't one.
func ThumbnailCellLabel(cell fyne.CanvasObject) *widget.Label {
	c, ok := cell.(*fyne.Container)
	if !ok || len(c.Objects) < 2 {
		return nil
	}
	lbl, _ := c.Objects[0].(*widget.Label)
	return lbl
}

// ThumbnailCellImage returns the canvas.Image inside a cell built by
// NewThumbnailCell, or nil if the object isn't one.
func ThumbnailCellImage(cell fyne.CanvasObject) *canvas.Image {
	c, ok := cell.(*fyne.Container)
	if !ok || len(c.Objects) < 2 {
		return nil
	}
	img, _ := c.Objects[1].(*canvas.Image)
	return img
}
