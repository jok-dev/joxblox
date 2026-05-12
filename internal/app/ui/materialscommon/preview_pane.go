// Package materialscommon hosts the shared shell pieces used by both the
// Scan tab's `Materials` sub-tab and the RenderDoc tab's `Materials`
// sub-tab — N-slot preview pane, thumbnail-cell layout, and a keyed
// thumbnail cache with background decode + downsample. The data feeding
// each tab differs (rbxl asset IDs vs RenderDoc texture IDs) so neither
// the table model nor the column set is shared; only the visual shell.
package materialscommon

import (
	"image"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// PreviewSlot describes one labeled image area in a PreviewPane.
type PreviewSlot struct {
	Title string
}

// PreviewSlotMinDim is the minimum on-screen size for a preview slot's
// image. Kept small so the host HSplit can be dragged narrow without the
// 3-/4-slot grid pinning it open — the image's `FillMode = Contain`
// scales gracefully up when there's room. Per the project's CLAUDE.md
// guidance: keep mins on widgets that bubble up to a top-level tab
// (~80px or less) so the window stays resizable.
const PreviewSlotMinDim = 80

// PreviewPane is the right-hand side of a materials sub-tab — N labeled
// image previews in a single row plus a multi-line info entry below.
// Both the renderdoc and scan materials views share this shell.
type PreviewPane struct {
	container *fyne.Container
	slots     []*previewSlotWidgets
	info      *widget.Entry
}

type previewSlotWidgets struct {
	title string
	img   *canvas.Image
	label *widget.Label
}

// NewPreviewPane builds a PreviewPane with one image slot per entry in
// slots. placeholder is the initial info-entry text (e.g. "Select a
// material to preview.").
func NewPreviewPane(slots []PreviewSlot, placeholder string) *PreviewPane {
	pane := &PreviewPane{
		slots: make([]*previewSlotWidgets, len(slots)),
	}
	boxes := make([]fyne.CanvasObject, len(slots))
	for i, slot := range slots {
		img := canvas.NewImageFromImage(nil)
		img.FillMode = canvas.ImageFillContain
		img.ScaleMode = canvas.ImageScaleFastest
		img.SetMinSize(fyne.NewSize(PreviewSlotMinDim, PreviewSlotMinDim))
		img.Hide()
		label := widget.NewLabel(slot.Title + ": —")
		// Truncate rather than expand: a 10-digit asset ID caption like
		// "Color: 1234567890" rendered at default font would otherwise pin
		// each slot's min width to ~140px, which prevents the host HSplit
		// from being dragged narrow.
		label.Wrapping = fyne.TextTruncate
		boxes[i] = container.NewBorder(label, nil, nil, nil, img)
		pane.slots[i] = &previewSlotWidgets{title: slot.Title, img: img, label: label}
	}
	columns := len(slots)
	if columns < 1 {
		columns = 1
	}
	row := container.NewGridWithColumns(columns, boxes...)
	info := widget.NewMultiLineEntry()
	info.Wrapping = fyne.TextWrapWord
	info.Disable()
	if placeholder == "" {
		placeholder = "Select a row to preview."
	}
	info.SetText(placeholder)
	pane.info = info
	pane.container = container.NewBorder(row, nil, nil, nil, info)
	return pane
}

// Container returns the root canvas object — drop this into your tab.
func (p *PreviewPane) Container() fyne.CanvasObject { return p.container }

// Reset returns every slot to "Title: —" with no image, and resets the
// info entry to placeholder.
func (p *PreviewPane) Reset(placeholder string) {
	for _, slot := range p.slots {
		clearPreviewImage(slot.img)
		slot.label.SetText(slot.title + ": —")
	}
	if placeholder != "" {
		p.info.SetText(placeholder)
	}
}

// SetImage paints img into the slot at index. caption replaces the label
// suffix ("Color: 1234"). Pass img=nil to keep the caption while the
// image is missing/decoding.
func (p *PreviewPane) SetImage(index int, caption string, img image.Image) {
	if index < 0 || index >= len(p.slots) {
		return
	}
	slot := p.slots[index]
	if caption == "" {
		slot.label.SetText(slot.title + ": —")
	} else {
		slot.label.SetText(slot.title + ": " + caption)
	}
	if img == nil {
		clearPreviewImage(slot.img)
		return
	}
	slot.img.Image = img
	slot.img.Refresh()
	slot.img.Show()
}

// ClearSlot resets slot at index to no image + "Title: —" label.
func (p *PreviewPane) ClearSlot(index int) {
	if index < 0 || index >= len(p.slots) {
		return
	}
	slot := p.slots[index]
	clearPreviewImage(slot.img)
	slot.label.SetText(slot.title + ": —")
}

// SetInfo replaces the multi-line info entry contents.
func (p *PreviewPane) SetInfo(text string) {
	p.info.SetText(text)
}

// clearPreviewImage visually blanks a canvas.Image. Setting Image=nil
// and Refresh() doesn't reliably wipe the last-painted frame in Fyne
// 2.6, so we hide the canvas as well — the labelled placeholder
// ("Title: —") makes the empty slot obvious.
func clearPreviewImage(img *canvas.Image) {
	img.Image = nil
	img.Refresh()
	img.Hide()
}
