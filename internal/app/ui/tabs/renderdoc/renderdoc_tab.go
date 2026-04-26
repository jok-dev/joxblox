// Package renderdoctab builds the "RenderDoc" tab: load a .rdc capture, list
// every Texture2D with its dimensions, format, and GPU memory footprint so a
// user can skim what the GPU is actually storing without opening the
// RenderDoc GUI. Click any row to decode and preview the base-level image
// when the format is supported (BC1/BC3/R8G8B8A8/B8G8R8A8/R8).
package renderdoctab

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	"sort"
	"strconv"
	"strings"

	"joxblox/internal/format"
	"joxblox/internal/renderdoc"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	fyneDialog "fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

const rdcFileFilterLabel = "RenderDoc capture"

type renderdocTabState struct {
	allTextures     []renderdoc.TextureInfo
	displayTextures []renderdoc.TextureInfo
	report          *renderdoc.Report
	sortColumn      string
	sortDescending  bool
	filterText      string
	// hideNonAssets hides render targets, cubemaps, and small/utility
	// allocations from the table so the user only sees Roblox asset textures
	// by default. Toggled via the "Assets only" checkbox.
	hideNonAssets bool
	selectedRow   int // -1 when nothing selected
	bufferStore     *renderdoc.BufferStore
	xmlPath         string
	// channelMode controls which channel(s) the preview pane renders. The
	// five buttons above the preview are mutually exclusive: "all" shows the
	// full RGBA composite, the rest isolate one channel as grayscale.
	channelMode channelMode
	// sourceImage is the most recently decoded preview, kept around so the
	// channel toggles can re-mask it without re-decoding from the capture.
	sourceImage image.Image
}

var columnHeaders = []string{"ID", "W×H", "Mips", "Array", "Format", "Category", "VRAM"}

// NewRenderDocTab builds the RenderDoc tab. A launcher row sits above two
// sub-tabs: Textures (existing UI) and Meshes (new). The launcher's capture
// list dispatches loads to whichever sub-tab is currently selected and shows
// an indicator next to the capture currently loaded in that sub-tab.
func NewRenderDocTab(window fyne.Window) fyne.CanvasObject {
	const (
		texturesIndex  = 0
		meshesIndex    = 1
		materialsIndex = 2
	)

	var lc *launcher
	texturesView, loadTexturesFromPath := newTexturesSubTab(window, func(path string) {
		if lc != nil {
			lc.setLoaded(texturesIndex, path)
		}
	})
	meshesView, loadMeshesFromPath := newMeshesSubTab(window, func(path string) {
		if lc != nil {
			lc.setLoaded(meshesIndex, path)
		}
	})
	materialsView, loadMaterialsFromPath := newMaterialsSubTab(window, func(path string) {
		if lc != nil {
			lc.setLoaded(materialsIndex, path)
		}
	})

	tabs := container.NewAppTabs(
		container.NewTabItem("Textures", texturesView),
		container.NewTabItem("Meshes", meshesView),
		container.NewTabItem("Materials", materialsView),
	)
	tabs.SelectIndex(materialsIndex)

	// Loading dispatches to all three sub-tabs at once so a single click
	// populates Textures, Meshes, and Materials in parallel. Each sub-tab
	// owns its own background goroutine + buffer store; they don't share
	// parsed state today (3× convert+parse on load), but the wall-clock cost
	// is dominated by the single longest pipeline since they run concurrently.
	loadAllSubTabs := func(path string) {
		loadTexturesFromPath(path)
		loadMeshesFromPath(path)
		loadMaterialsFromPath(path)
	}

	lc = newLauncher(window, loadAllSubTabs)
	tabs.OnSelected = func(*container.TabItem) {
		lc.setActiveSubTab(tabs.SelectedIndex())
	}

	return container.NewBorder(lc.canvas, nil, nil, nil, tabs)
}

// newTexturesSubTab builds the Textures sub-tab. The window is used to parent
// dialogs shown from background goroutines.
func newTexturesSubTab(window fyne.Window, onLoaded func(path string)) (fyne.CanvasObject, func(path string)) {
	state := &renderdocTabState{
		sortColumn:     "VRAM",
		sortDescending: true,
		selectedRow:    -1,
		channelMode:    channelModeAll,
		hideNonAssets:  true,
	}

	pathLabel := widget.NewLabel("No capture loaded.")
	pathLabel.Wrapping = fyne.TextWrapWord
	summaryLabel := widget.NewLabel("")
	summaryLabel.Wrapping = fyne.TextWrapWord
	categoryLabel := widget.NewLabel("")
	categoryLabel.Wrapping = fyne.TextWrapWord
	countLabel := widget.NewLabel("")

	progressBar := widget.NewProgressBarInfinite()
	progressBar.Hide()

	filterEntry := widget.NewEntry()
	filterEntry.SetPlaceHolder("Filter by format or category")

	assetsOnlyCheck := widget.NewCheck("Assets only (hide render targets, cubemaps, utility)", nil)
	assetsOnlyCheck.SetChecked(state.hideNonAssets)

	previewCanvas := canvas.NewImageFromImage(nil)
	previewCanvas.FillMode = canvas.ImageFillContain
	previewCanvas.ScaleMode = canvas.ImageScaleFastest
	previewCanvas.SetMinSize(fyne.NewSize(320, 320))

	// Use a disabled multi-line Entry instead of a Label so the user can
	// select and copy substrings (resource ID, format, pixel hash). Disabled
	// Entries in Fyne 2.6 still allow text selection + Ctrl+C and look only
	// marginally different from a Label against our dark theme.
	previewInfoLabel := widget.NewMultiLineEntry()
	previewInfoLabel.Wrapping = fyne.TextWrapWord
	previewInfoLabel.SetText("Select a texture to preview.")
	previewInfoLabel.Disable()

	redrawPreview := func() {
		if state.sourceImage == nil {
			previewCanvas.Image = nil
		} else {
			previewCanvas.Image = applyChannelMode(state.sourceImage, state.channelMode)
		}
		previewCanvas.Refresh()
	}

	// Buttons are collected in a slice so selecting any one can refresh the
	// visual state of the others (they read state.channelMode and restyle
	// themselves on Refresh).
	var modeButtons []*channelModeButton
	setMode := func(mode channelMode) {
		state.channelMode = mode
		for _, btn := range modeButtons {
			btn.Refresh()
		}
		redrawPreview()
	}
	modeIsActive := func(mode channelMode) func() bool {
		return func() bool { return state.channelMode == mode }
	}
	modeButtons = []*channelModeButton{
		newChannelModeButton("All", channelColorAll, modeIsActive(channelModeAll), func() { setMode(channelModeAll) }),
		newChannelModeButton("R", channelColorRed, modeIsActive(channelModeR), func() { setMode(channelModeR) }),
		newChannelModeButton("G", channelColorGreen, modeIsActive(channelModeG), func() { setMode(channelModeG) }),
		newChannelModeButton("B", channelColorBlue, modeIsActive(channelModeB), func() { setMode(channelModeB) }),
		newChannelModeButton("A", channelColorAlpha, modeIsActive(channelModeA), func() { setMode(channelModeA) }),
	}
	alphaButton := modeButtons[4]
	// Hide the alpha toggle when the selected texture's format has no
	// meaningful alpha (BC1, single/double-channel raw, HDR color). Keeps
	// users from clicking "A" on a BC1 and seeing a flat white channel.
	updateAlphaButton := func(format string) {
		if renderdoc.FormatHasAlpha(format) {
			alphaButton.Show()
			return
		}
		alphaButton.Hide()
		if state.channelMode == channelModeA {
			setMode(channelModeAll)
		}
	}
	channelToggleRow := container.NewHBox(
		widget.NewLabel("Channels:"),
		modeButtons[0], modeButtons[1], modeButtons[2], modeButtons[3], modeButtons[4],
	)

	previewHeader := container.NewVBox(channelToggleRow, previewInfoLabel)
	previewPane := container.NewBorder(previewHeader, nil, nil, nil, previewCanvas)

	var table *widget.Table
	table = widget.NewTableWithHeaders(
		func() (int, int) {
			return len(state.displayTextures), len(columnHeaders)
		},
		func() fyne.CanvasObject {
			return widget.NewLabel("")
		},
		func(id widget.TableCellID, object fyne.CanvasObject) {
			label, ok := object.(*widget.Label)
			if !ok {
				return
			}
			if id.Row < 0 || id.Row >= len(state.displayTextures) || id.Col < 0 || id.Col >= len(columnHeaders) {
				label.SetText("")
				return
			}
			label.SetText(columnValue(state.displayTextures[id.Row], columnHeaders[id.Col]))
		},
	)
	table.CreateHeader = func() fyne.CanvasObject {
		return widget.NewButton("", nil)
	}
	table.UpdateHeader = func(id widget.TableCellID, object fyne.CanvasObject) {
		headerButton, ok := object.(*widget.Button)
		if !ok {
			return
		}
		if id.Row == -1 && id.Col >= 0 && id.Col < len(columnHeaders) {
			columnName := columnHeaders[id.Col]
			label := columnName
			if state.sortColumn == columnName {
				if state.sortDescending {
					label = columnName + " ▼"
				} else {
					label = columnName + " ▲"
				}
			}
			headerButton.SetText(label)
			headerButton.OnTapped = func() {
				if state.sortColumn == columnName {
					state.sortDescending = !state.sortDescending
				} else {
					state.sortColumn = columnName
					state.sortDescending = defaultSortDescendingFor(columnName)
				}
				applySortAndFilter(state)
				table.Refresh()
			}
			return
		}
		if id.Col == -1 && id.Row >= 0 {
			headerButton.SetText(strconv.Itoa(id.Row + 1))
		} else {
			headerButton.SetText("")
		}
		headerButton.OnTapped = nil
	}
	applyColumnWidths(table)
	table.OnSelected = func(id widget.TableCellID) {
		if id.Row < 0 || id.Row >= len(state.displayTextures) {
			return
		}
		state.selectedRow = id.Row
		selected := state.displayTextures[id.Row]
		updateAlphaButton(selected.Format)
		triggerPreview(state, selected, previewCanvas, previewInfoLabel)
	}
	// Fyne 2.6's widget.Table only moves a visual focus cursor on arrow keys
	// — it doesn't call Select, so OnSelected never fires and the preview
	// stays put. Wrap the table to translate Up/Down into Select on the
	// adjacent row. ExtendBaseWidget rewires Table's internal focus/Self()
	// so that when the Table focuses itself on a click, Fyne actually
	// routes TypedKey events to our wrapper instead of the base widget.
	interactiveTable := &arrowSelectTable{
		Table: table,
		rowCount: func() int {
			return len(state.displayTextures)
		},
		currentRow: func() int {
			return state.selectedRow
		},
	}
	interactiveTable.ExtendBaseWidget(interactiveTable)

	filterEntry.OnChanged = func(text string) {
		state.filterText = strings.TrimSpace(text)
		applySortAndFilter(state)
		table.Refresh()
		countLabel.SetText(fmt.Sprintf("Showing %d of %d textures", len(state.displayTextures), len(state.allTextures)))
	}
	assetsOnlyCheck.OnChanged = func(checked bool) {
		state.hideNonAssets = checked
		applySortAndFilter(state)
		table.Refresh()
		countLabel.SetText(fmt.Sprintf("Showing %d of %d textures", len(state.displayTextures), len(state.allTextures)))
	}

	onLoadFinished := func(report *renderdoc.Report, loadedPath string, xmlPath string, newStore *renderdoc.BufferStore, loadErr error) {
		progressBar.Hide()
		if loadErr != nil {
			pathLabel.SetText(fmt.Sprintf("Load failed: %s", loadedPath))
			fyneDialog.ShowError(loadErr, window)
			if newStore != nil {
				_ = newStore.Close()
				renderdoc.RemoveConvertedOutput(xmlPath)
			}
			return
		}
		// Release any previous capture's resources before swapping in the new.
		if state.bufferStore != nil {
			_ = state.bufferStore.Close()
		}
		if state.xmlPath != "" {
			renderdoc.RemoveConvertedOutput(state.xmlPath)
		}
		state.report = report
		state.allTextures = append([]renderdoc.TextureInfo(nil), report.Textures...)
		state.bufferStore = newStore
		state.xmlPath = xmlPath
		state.sortColumn = "VRAM"
		state.sortDescending = true
		state.filterText = strings.TrimSpace(filterEntry.Text)
		state.selectedRow = -1
		applySortAndFilter(state)
		pathLabel.SetText(fmt.Sprintf("Loaded: %s", loadedPath))
		assetsOnlyBytes, assetsOnlyCount := computeAssetsOnlyTotals(report.Textures)
		summaryLabel.SetText(fmt.Sprintf("GPU: %s · Total VRAM: %s across %d textures · Assets only: %s across %d",
			nonEmptyOr(report.GPUAdapter, "unknown"),
			format.FormatSizeAuto64(report.TotalBytes),
			len(report.Textures),
			format.FormatSizeAuto64(assetsOnlyBytes),
			assetsOnlyCount,
		))
		categoryLabel.SetText(buildCategorySummary(report))
		countLabel.SetText(fmt.Sprintf("Showing %d of %d textures", len(state.displayTextures), len(state.allTextures)))
		previewInfoLabel.SetText("Select a texture to preview.")
		state.sourceImage = nil
		previewCanvas.Image = nil
		previewCanvas.Refresh()
		table.Refresh()
		if onLoaded != nil {
			onLoaded(loadedPath)
		}
	}

	statusFn := func(message string) {
		pathLabel.SetText(message)
	}
	loadFromPath := func(path string) {
		go loadCaptureFromPath(window, progressBar, nil, path, statusFn, onLoadFinished)
	}

	header := container.NewVBox(
		pathLabel,
		summaryLabel,
		categoryLabel,
		progressBar,
		filterEntry,
		assetsOnlyCheck,
	)
	footer := countLabel
	// Place table on the left and preview pane on the right in a resizable
	// split. Default offset biases to the table since most inspection time is
	// scanning the list.
	split := container.NewHSplit(interactiveTable, previewPane)
	split.Offset = 0.65
	return container.NewBorder(header, footer, nil, nil, split), loadFromPath
}

// arrowSelectTable wraps widget.Table so Up/Down arrow keys actually change
// the selection (firing OnSelected), not just the internal focus cursor.
type arrowSelectTable struct {
	*widget.Table
	rowCount   func() int
	currentRow func() int
}

func (t *arrowSelectTable) TypedKey(event *fyne.KeyEvent) {
	switch event.Name {
	case fyne.KeyDown:
		next := t.currentRow() + 1
		if next < t.rowCount() {
			t.Select(widget.TableCellID{Row: next, Col: 0})
		}
		return
	case fyne.KeyUp:
		next := t.currentRow() - 1
		if next >= 0 {
			t.Select(widget.TableCellID{Row: next, Col: 0})
		}
		return
	case fyne.KeyHome:
		if t.rowCount() > 0 {
			t.Select(widget.TableCellID{Row: 0, Col: 0})
		}
		return
	case fyne.KeyEnd:
		if rows := t.rowCount(); rows > 0 {
			t.Select(widget.TableCellID{Row: rows - 1, Col: 0})
		}
		return
	}
	t.Table.TypedKey(event)
}

// triggerPreview runs DecodeTexturePreview on a background goroutine so the
// UI stays responsive even for 4K+ textures that take tens of milliseconds to
// decode. Updates the canvas + info label when decoding completes. The
// channel toggles reuse the stored sourceImage so masking is instant.
func triggerPreview(state *renderdocTabState, texture renderdoc.TextureInfo, previewCanvas *canvas.Image, infoLabel *widget.Entry) {
	infoLabel.SetText(fmt.Sprintf("Decoding %s %s (%d×%d)…", texture.ResourceID, texture.ShortFormat, texture.Width, texture.Height))
	state.sourceImage = nil
	previewCanvas.Image = nil
	previewCanvas.Refresh()

	if state.bufferStore == nil {
		infoLabel.SetText("No capture loaded.")
		return
	}
	store := state.bufferStore
	go func() {
		img, err := renderdoc.DecodeTexturePreview(texture, store)
		fyne.Do(func() {
			if err != nil {
				if errors.Is(err, renderdoc.ErrNoUploadData) {
					infoLabel.SetText(fmt.Sprintf("%s %s · no upload data (render target or GPU-generated)", texture.ResourceID, texture.ShortFormat))
				} else if errors.Is(err, renderdoc.ErrUnsupportedFormat) {
					infoLabel.SetText(fmt.Sprintf("%s %s · format not supported for preview yet", texture.ResourceID, texture.ShortFormat))
				} else {
					infoLabel.SetText(fmt.Sprintf("%s %s · decode failed: %s", texture.ResourceID, texture.ShortFormat, err.Error()))
				}
				state.sourceImage = nil
				previewCanvas.Image = nil
				previewCanvas.Refresh()
				return
			}
			infoLabel.SetText(previewInfoText(texture, img))
			state.sourceImage = img
			previewCanvas.Image = applyChannelMode(img, state.channelMode)
			previewCanvas.Refresh()
		})
	}()
}

// channelMode enumerates the preview render modes. Exactly one is active at
// a time. "all" renders the full RGBA composite; the rest isolate a single
// channel as grayscale so intensity is readable without the color tint.
type channelMode string

const (
	channelModeAll channelMode = "all"
	channelModeR   channelMode = "r"
	channelModeG   channelMode = "g"
	channelModeB   channelMode = "b"
	channelModeA   channelMode = "a"
)

// Colors used to tint each mode button. Chosen to read as unambiguously
// R/G/B at a glance while still working against the app's dark theme. The
// "All" button uses a neutral accent that doesn't favour any channel.
var (
	channelColorAll   = color.NRGBA{R: 120, G: 150, B: 200, A: 255}
	channelColorRed   = color.NRGBA{R: 220, G: 60, B: 60, A: 255}
	channelColorGreen = color.NRGBA{R: 60, G: 200, B: 80, A: 255}
	channelColorBlue  = color.NRGBA{R: 80, G: 130, B: 230, A: 255}
	channelColorAlpha = color.NRGBA{R: 210, G: 210, B: 210, A: 255}
)

// channelModeButton is a small tappable widget drawn as a colored rounded
// rect with a centered letter. Unlike a plain widget.Button it has an
// arbitrary fill color, and its visual state is read from an external
// predicate (isActive) — the preview pane shares one channelMode across all
// five buttons, so clicking one needs to implicitly deactivate the others.
type channelModeButton struct {
	widget.BaseWidget

	label    string
	onColor  color.NRGBA
	isActive func() bool
	onTap    func()

	background *canvas.Rectangle
	labelText  *canvas.Text
}

func newChannelModeButton(label string, onColor color.NRGBA, isActive func() bool, onTap func()) *channelModeButton {
	btn := &channelModeButton{
		label:    label,
		onColor:  onColor,
		isActive: isActive,
		onTap:    onTap,
	}
	btn.background = canvas.NewRectangle(color.Transparent)
	btn.background.CornerRadius = 4
	btn.labelText = canvas.NewText(label, color.White)
	btn.labelText.TextStyle = fyne.TextStyle{Bold: true}
	btn.labelText.TextSize = 14
	btn.labelText.Alignment = fyne.TextAlignCenter
	btn.applyStyle()
	btn.ExtendBaseWidget(btn)
	return btn
}

func (b *channelModeButton) applyStyle() {
	if b.isActive() {
		b.background.FillColor = b.onColor
		b.labelText.Color = color.White
	} else {
		// Darken the fill by ~70% so the button reads as "off" but still
		// identifiable as the same channel it represents.
		b.background.FillColor = color.NRGBA{
			R: b.onColor.R / 4,
			G: b.onColor.G / 4,
			B: b.onColor.B / 4,
			A: 255,
		}
		b.labelText.Color = color.NRGBA{R: 160, G: 160, B: 160, A: 255}
	}
	b.background.Refresh()
	b.labelText.Refresh()
}

func (b *channelModeButton) Tapped(_ *fyne.PointEvent) {
	if b.onTap != nil {
		b.onTap()
	}
}

func (b *channelModeButton) MinSize() fyne.Size {
	// "All" needs a little extra width for three letters; fixed 48 keeps the
	// row tidy without measuring individual text widths.
	if b.label == "All" {
		return fyne.NewSize(52, 32)
	}
	return fyne.NewSize(36, 32)
}

func (b *channelModeButton) Refresh() {
	b.applyStyle()
	b.BaseWidget.Refresh()
}

func (b *channelModeButton) CreateRenderer() fyne.WidgetRenderer {
	return &channelModeRenderer{
		btn:     b,
		objects: []fyne.CanvasObject{b.background, b.labelText},
	}
}

type channelModeRenderer struct {
	btn     *channelModeButton
	objects []fyne.CanvasObject
}

func (r *channelModeRenderer) Layout(size fyne.Size) {
	r.btn.background.Resize(size)
	r.btn.background.Move(fyne.NewPos(0, 0))
	textHeight := r.btn.labelText.MinSize().Height
	r.btn.labelText.Resize(fyne.NewSize(size.Width, textHeight))
	r.btn.labelText.Move(fyne.NewPos(0, (size.Height-textHeight)/2))
}

func (r *channelModeRenderer) MinSize() fyne.Size {
	return r.btn.MinSize()
}

func (r *channelModeRenderer) Refresh() {
	r.btn.applyStyle()
	canvas.Refresh(r.btn)
}

func (r *channelModeRenderer) Objects() []fyne.CanvasObject {
	return r.objects
}

func (r *channelModeRenderer) Destroy() {}

// applyChannelMode renders the source image according to the current exclusive
// channel mode: "all" returns the source untouched; R/G/B/A each return a
// grayscale image of that channel.
func applyChannelMode(source image.Image, mode channelMode) image.Image {
	switch mode {
	case channelModeAll:
		return source
	case channelModeR:
		return singleChannelGrayscale(source, 0)
	case channelModeG:
		return singleChannelGrayscale(source, 1)
	case channelModeB:
		return singleChannelGrayscale(source, 2)
	case channelModeA:
		return singleChannelGrayscale(source, 3)
	}
	return source
}

// singleChannelGrayscale returns a new image where R=G=B=source[channelIndex]
// for every pixel. channelIndex is 0 (R), 1 (G), 2 (B), or 3 (A). For the
// alpha case, output alpha is forced to 255 so the grayscale is always
// visible — otherwise low-alpha pixels would composite as transparent and
// hide the value we're trying to inspect.
func singleChannelGrayscale(source image.Image, channelIndex int) image.Image {
	bounds := source.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()
	out := image.NewNRGBA(image.Rect(0, 0, width, height))
	isAlphaChannel := channelIndex == 3

	if src, ok := source.(*image.NRGBA); ok {
		rowLen := width * 4
		for y := 0; y < height; y++ {
			srcRow := src.Pix[y*src.Stride : y*src.Stride+rowLen]
			dstRow := out.Pix[y*out.Stride : y*out.Stride+rowLen]
			for x := 0; x < rowLen; x += 4 {
				v := srcRow[x+channelIndex]
				dstRow[x+0] = v
				dstRow[x+1] = v
				dstRow[x+2] = v
				if isAlphaChannel {
					dstRow[x+3] = 0xFF
				} else {
					dstRow[x+3] = srcRow[x+3]
				}
			}
		}
		return out
	}

	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			cr, cg, cb, ca := source.At(bounds.Min.X+x, bounds.Min.Y+y).RGBA()
			var channel uint32
			switch channelIndex {
			case 0:
				channel = cr
			case 1:
				channel = cg
			case 2:
				channel = cb
			case 3:
				channel = ca
			}
			v := uint8(channel >> 8)
			offset := out.PixOffset(x, y)
			out.Pix[offset+0] = v
			out.Pix[offset+1] = v
			out.Pix[offset+2] = v
			if isAlphaChannel {
				out.Pix[offset+3] = 0xFF
			} else {
				out.Pix[offset+3] = uint8(ca >> 8)
			}
		}
	}
	return out
}

func previewInfoText(texture renderdoc.TextureInfo, img image.Image) string {
	bounds := img.Bounds()
	// Prefer the precomputed hash set at load time so identical textures
	// always read as identical strings. Fall back to live hashing for
	// captures hashed lazily (test fixtures).
	hash := texture.PixelHash
	if hash == "" {
		hash = renderdoc.HashImagePixels(img)
	}
	return fmt.Sprintf("%s · %s · %d×%d · base mip (full image: %s) · pixel hash %s",
		texture.ResourceID,
		texture.ShortFormat,
		bounds.Dx(),
		bounds.Dy(),
		format.FormatSizeAuto64(texture.Bytes),
		hash,
	)
}

func defaultSortDescendingFor(column string) bool {
	switch column {
	case "ID", "Format", "Category":
		return false
	}
	return true
}

func applyColumnWidths(table *widget.Table) {
	widths := map[int]float32{
		0: 80,  // ID
		1: 110, // W×H
		2: 60,  // Mips
		3: 60,  // Array
		4: 220, // Format
		5: 220, // Category
		6: 100, // VRAM
	}
	for col, width := range widths {
		table.SetColumnWidth(col, width)
	}
}

func columnValue(texture renderdoc.TextureInfo, column string) string {
	switch column {
	case "ID":
		return texture.ResourceID
	case "W×H":
		return fmt.Sprintf("%d × %d", texture.Width, texture.Height)
	case "Mips":
		if texture.MipLevels == 0 {
			return "full"
		}
		return strconv.Itoa(texture.MipLevels)
	case "Array":
		return strconv.Itoa(texture.ArraySize)
	case "Format":
		if texture.IsUnknownFmt {
			return texture.ShortFormat + " (?)"
		}
		return texture.ShortFormat
	case "Category":
		return string(texture.Category)
	case "VRAM":
		return format.FormatSizeAuto64(texture.Bytes)
	}
	return ""
}

func applySortAndFilter(state *renderdocTabState) {
	filtered := filterTextures(state.allTextures, state.filterText, state.hideNonAssets)
	sortTextures(filtered, state.sortColumn, state.sortDescending)
	state.displayTextures = filtered
}

// hiddenNonAssetCategories are the texture categories hidden when the
// "Assets only" checkbox is checked. These are engine scratch space or
// unclassifiable allocations, not texture data a Roblox developer
// uploaded or can optimize, so they tend to be noise when auditing a
// capture's asset texture footprint.
var hiddenNonAssetCategories = map[renderdoc.TextureCategory]struct{}{
	renderdoc.CategoryRenderTgt:        {},
	renderdoc.CategoryDepthTgt:         {},
	renderdoc.CategoryCubemap:          {},
	renderdoc.CategorySmallUtil:        {},
	renderdoc.CategoryBuiltin:        {},
	renderdoc.CategoryBuiltinBRDFLUT: {},
	renderdoc.CategoryUnknown:        {},
}

// defaultRobloxPixelHashes is sourced from the renderdoc package so both the
// UI and CLI texture-report tool classify built-ins the same way.
var defaultRobloxPixelHashes = renderdoc.DefaultRobloxBuiltinHashes

func filterTextures(textures []renderdoc.TextureInfo, filterText string, hideNonAssets bool) []renderdoc.TextureInfo {
	lower := strings.ToLower(filterText)
	out := make([]renderdoc.TextureInfo, 0, len(textures))
	for _, texture := range textures {
		if hideNonAssets {
			// Built-ins are already re-tagged as CategoryBuiltin after
			// hashing, so the category check alone is enough to hide them.
			if _, skip := hiddenNonAssetCategories[texture.Category]; skip {
				continue
			}
		}
		if lower != "" {
			haystack := strings.ToLower(texture.ShortFormat + " " + string(texture.Category))
			if !strings.Contains(haystack, lower) {
				continue
			}
		}
		out = append(out, texture)
	}
	return out
}

func sortTextures(textures []renderdoc.TextureInfo, column string, descending bool) {
	sort.SliceStable(textures, func(i, j int) bool {
		if descending {
			return compareTextures(textures[j], textures[i], column)
		}
		return compareTextures(textures[i], textures[j], column)
	})
}

func compareTextures(a, b renderdoc.TextureInfo, column string) bool {
	switch column {
	case "ID":
		return a.ResourceID < b.ResourceID
	case "W×H":
		if a.Width*a.Height != b.Width*b.Height {
			return a.Width*a.Height < b.Width*b.Height
		}
		return a.Width < b.Width
	case "Mips":
		return a.MipLevels < b.MipLevels
	case "Array":
		return a.ArraySize < b.ArraySize
	case "Format":
		return a.ShortFormat < b.ShortFormat
	case "Category":
		return string(a.Category) < string(b.Category)
	case "VRAM":
		return a.Bytes < b.Bytes
	}
	return false
}

// computeAssetsOnlyTotals returns the bytes and count of textures that pass
// the "Assets only" filter (category not in hiddenNonAssetCategories and
// pixel hash not in defaultRobloxPixelHashes). Used for the summary line so
// the user can compare the raw total to the developer-relevant subtotal.
func computeAssetsOnlyTotals(textures []renderdoc.TextureInfo) (int64, int) {
	var bytes int64
	count := 0
	for _, texture := range textures {
		if _, skip := hiddenNonAssetCategories[texture.Category]; skip {
			continue
		}
		bytes += texture.Bytes
		count++
	}
	return bytes, count
}

func buildCategorySummary(report *renderdoc.Report) string {
	type row struct {
		name  renderdoc.TextureCategory
		count int
		bytes int64
	}
	rows := make([]row, 0, len(report.ByCategory))
	for name, aggregate := range report.ByCategory {
		rows = append(rows, row{name, aggregate.Count, aggregate.Bytes})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].bytes > rows[j].bytes })

	parts := make([]string, 0, len(rows))
	for _, r := range rows {
		parts = append(parts, fmt.Sprintf("%s: %d (%s)", r.name, r.count, format.FormatSizeAuto64(r.bytes)))
	}
	return strings.Join(parts, " · ")
}

func nonEmptyOr(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

// loadCaptureFromPath runs the convert -> parse -> hash pipeline for a given
// capture path. Invoked by the launcher's load dispatch. Progress is
// reported via onStatusChanged; completion (success or
// failure) via onFinished, both invoked on the UI thread.
func loadCaptureFromPath(window fyne.Window, progressBar *widget.ProgressBarInfinite, loadButton *widget.Button, capturePath string, onStatusChanged func(string), onFinished func(*renderdoc.Report, string, string, *renderdoc.BufferStore, error)) {
	fyne.Do(func() {
		progressBar.Show()
		if loadButton != nil {
			loadButton.Disable()
		}
	})

	xmlPath, convertErr := renderdoc.ConvertToXML(capturePath)
	if convertErr != nil {
		fyne.Do(func() {
			onFinished(nil, capturePath, "", nil, convertErr)
		})
		return
	}

	report, parseErr := renderdoc.ParseCaptureXMLFile(xmlPath)
	if parseErr != nil {
		fyne.Do(func() {
			onFinished(nil, capturePath, xmlPath, nil, parseErr)
		})
		return
	}

	store, storeErr := renderdoc.OpenBufferStore(xmlPath)
	if storeErr != nil {
		// Parsing succeeded but we can't preview — still surface the report.
		fyne.Do(func() {
			onFinished(report, capturePath, xmlPath, nil, nil)
		})
		return
	}

	// Hash every asset texture so we can recognise known-default Roblox
	// textures by content. This is a 1-3 second step for typical maps
	// (parallelised across CPU cores). Progress is shown in the path label
	// so the tab doesn't just freeze.
	renderdoc.ComputeTextureHashes(report, store, func(done, total int) {
		if done != total && done%8 != 0 {
			// Throttle UI updates — hashing a Batcave-sized capture fires
			// ~55 progress callbacks and we don't need every one.
			return
		}
		fyne.Do(func() {
			if onStatusChanged != nil {
				onStatusChanged(fmt.Sprintf("Hashing textures %d/%d…", done, total))
			}
		})
	})
	// Reclassify any texture whose hash matches a known Roblox built-in
	// (BRDF LUT, default block face, etc.) as CategoryBuiltin so it shows
	// up distinctly from user-uploaded assets in the table and summary.
	renderdoc.ApplyBuiltinHashes(report, defaultRobloxPixelHashes)

	fyne.Do(func() {
		onFinished(report, capturePath, xmlPath, store, nil)
	})
}

