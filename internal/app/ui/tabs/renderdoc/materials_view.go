package renderdoctab

import (
	"fmt"
	"image"
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

type materialsTabState struct {
	materials        []renderdoc.Material
	displayMaterials []renderdoc.Material
	textureReport    *renderdoc.Report
	meshReport       *renderdoc.MeshReport
	bufferStore      *renderdoc.BufferStore
	xmlPath          string
	sortColumn       string
	sortDescending   bool
	filterText       string
	selectedRow      int
	thumbnailCache   map[string]image.Image // texID → decoded base mip
	decodeInFlight   map[string]bool        // texID → background decode running
	textureByID      map[string]renderdoc.TextureInfo
	// previewGeneration increments on every row click. Background decodes
	// capture the value at start and discard their result if a newer click
	// has happened, so a slow Color decode for an old row can't overwrite
	// a freshly-clicked row's preview pane.
	previewGeneration int
}

var materialColumnHeaders = []string{"Color", "Normal", "MR", "Color Hash", "Draws", "Meshes", "VRAM"}

type materialPreview struct {
	colorImg, normalImg, mrImg       *canvas.Image
	colorLabel, normalLabel, mrLabel *widget.Label
	infoEntry                        *widget.Entry
	container                        *fyne.Container
}

func newMaterialPreview() *materialPreview {
	mk := func(title string) (*canvas.Image, *widget.Label, *fyne.Container) {
		img := canvas.NewImageFromImage(nil)
		img.FillMode = canvas.ImageFillContain
		img.SetMinSize(fyne.NewSize(256, 256))
		lbl := widget.NewLabel(title)
		return img, lbl, container.NewBorder(lbl, nil, nil, nil, img)
	}
	colorImg, colorLabel, colorBox := mk("Color: —")
	normalImg, normalLabel, normalBox := mk("Normal: —")
	mrImg, mrLabel, mrBox := mk("MR: —")
	row := container.NewGridWithColumns(3, colorBox, normalBox, mrBox)
	infoEntry := widget.NewMultiLineEntry()
	infoEntry.Wrapping = fyne.TextWrapWord
	infoEntry.Disable()
	infoEntry.SetText("Select a material to preview.")
	return &materialPreview{
		colorImg: colorImg, normalImg: normalImg, mrImg: mrImg,
		colorLabel: colorLabel, normalLabel: normalLabel, mrLabel: mrLabel,
		infoEntry:  infoEntry,
		container:  container.NewBorder(row, nil, nil, nil, infoEntry),
	}
}

func (p *materialPreview) reset() {
	p.colorImg.Image = nil
	p.normalImg.Image = nil
	p.mrImg.Image = nil
	p.colorImg.Refresh()
	p.normalImg.Refresh()
	p.mrImg.Refresh()
	p.colorLabel.SetText("Color: —")
	p.normalLabel.SetText("Normal: —")
	p.mrLabel.SetText("MR: —")
	p.infoEntry.SetText("Select a material to preview.")
}

func newMaterialsSubTab(window fyne.Window, onLoaded func(path string)) (fyne.CanvasObject, func(path string)) {
	state := &materialsTabState{
		sortColumn:     "VRAM",
		sortDescending: true,
		selectedRow:    -1,
		thumbnailCache: map[string]image.Image{},
		decodeInFlight: map[string]bool{},
		textureByID:    map[string]renderdoc.TextureInfo{},
	}

	pathLabel := widget.NewLabel("No capture loaded.")
	pathLabel.Wrapping = fyne.TextWrapWord
	summaryLabel := widget.NewLabel("")
	summaryLabel.Wrapping = fyne.TextWrapWord
	countLabel := widget.NewLabel("")

	progressBar := widget.NewProgressBarInfinite()
	progressBar.Hide()

	filterEntry := widget.NewEntry()
	filterEntry.SetPlaceHolder("Filter by texture ID or hash")

	preview := newMaterialPreview()
	previewPane := preview.container

	var table *widget.Table
	table = widget.NewTableWithHeaders(
		func() (int, int) { return len(state.displayMaterials), len(materialColumnHeaders) },
		func() fyne.CanvasObject {
			img := canvas.NewImageFromImage(nil)
			img.FillMode = canvas.ImageFillContain
			img.SetMinSize(fyne.NewSize(32, 32))
			label := widget.NewLabel("")
			return container.NewMax(label, img)
		},
		func(id widget.TableCellID, object fyne.CanvasObject) {
			if id.Row < 0 || id.Row >= len(state.displayMaterials) || id.Col < 0 || id.Col >= len(materialColumnHeaders) {
				return
			}
			cont := object.(*fyne.Container)
			label := cont.Objects[0].(*widget.Label)
			img := cont.Objects[1].(*canvas.Image)
			renderMaterialCell(state, state.displayMaterials[id.Row], materialColumnHeaders[id.Col], label, img, table)
		},
	)
	table.CreateHeader = func() fyne.CanvasObject { return widget.NewButton("", nil) }
	table.UpdateHeader = func(id widget.TableCellID, object fyne.CanvasObject) {
		button := object.(*widget.Button)
		if id.Row == -1 && id.Col >= 0 && id.Col < len(materialColumnHeaders) {
			name := materialColumnHeaders[id.Col]
			label := name
			if state.sortColumn == name {
				if state.sortDescending {
					label = name + " ▼"
				} else {
					label = name + " ▲"
				}
			}
			button.SetText(label)
			button.OnTapped = func() {
				if state.sortColumn == name {
					state.sortDescending = !state.sortDescending
				} else {
					state.sortColumn = name
					state.sortDescending = true
				}
				applyMaterialSortAndFilter(state)
				table.Refresh()
			}
			return
		}
		if id.Col == -1 && id.Row >= 0 {
			button.SetText(strconv.Itoa(id.Row + 1))
		} else {
			button.SetText("")
		}
		button.OnTapped = nil
	}
	applyMaterialColumnWidths(table)
	table.OnSelected = func(id widget.TableCellID) {
		if id.Row < 0 || id.Row >= len(state.displayMaterials) {
			return
		}
		state.selectedRow = id.Row
		state.previewGeneration++
		updateMaterialPreview(state, state.displayMaterials[id.Row], preview, table)
	}

	filterEntry.OnChanged = func(text string) {
		state.filterText = strings.TrimSpace(text)
		applyMaterialSortAndFilter(state)
		table.Refresh()
		countLabel.SetText(fmt.Sprintf("Showing %d of %d materials", len(state.displayMaterials), len(state.materials)))
	}

	onLoadFinished := func(textureReport *renderdoc.Report, meshReport *renderdoc.MeshReport, materials []renderdoc.Material, loadedPath string, xmlPath string, store *renderdoc.BufferStore, loadErr error) {
		progressBar.Hide()
		if loadErr != nil {
			pathLabel.SetText(fmt.Sprintf("Load failed: %s", loadedPath))
			fyneDialog.ShowError(loadErr, window)
			if store != nil {
				_ = store.Close()
				renderdoc.RemoveConvertedOutput(xmlPath)
			}
			return
		}
		if state.bufferStore != nil {
			_ = state.bufferStore.Close()
		}
		if state.xmlPath != "" {
			renderdoc.RemoveConvertedOutput(state.xmlPath)
		}
		state.materials = materials
		state.textureReport = textureReport
		state.meshReport = meshReport
		state.bufferStore = store
		state.xmlPath = xmlPath
		state.thumbnailCache = map[string]image.Image{}
		state.decodeInFlight = map[string]bool{}
		state.previewGeneration = 0
		state.textureByID = map[string]renderdoc.TextureInfo{}
		for _, t := range textureReport.Textures {
			state.textureByID[t.ResourceID] = t
		}
		state.filterText = strings.TrimSpace(filterEntry.Text)
		state.selectedRow = -1
		applyMaterialSortAndFilter(state)
		pathLabel.SetText(fmt.Sprintf("Loaded: %s", loadedPath))
		summaryLabel.SetText(fmt.Sprintf("%d materials across %d draw calls", len(materials), countTotalDraws(materials)))
		countLabel.SetText(fmt.Sprintf("Showing %d of %d materials", len(state.displayMaterials), len(state.materials)))
		preview.reset()
		table.Refresh()
		if onLoaded != nil {
			onLoaded(loadedPath)
		}
	}

	loadFromPath := func(path string) {
		go loadMaterialsCaptureFromPath(window, progressBar, nil, path, onLoadFinished)
	}

	header := container.NewVBox(
		pathLabel,
		summaryLabel,
		progressBar,
		filterEntry,
	)
	footer := countLabel
	split := container.NewHSplit(table, previewPane)
	split.Offset = 0.7
	return container.NewBorder(header, footer, nil, nil, split), loadFromPath
}

func loadMaterialsCaptureFromPath(window fyne.Window, progressBar *widget.ProgressBarInfinite, loadButton *widget.Button, capturePath string, onFinished func(*renderdoc.Report, *renderdoc.MeshReport, []renderdoc.Material, string, string, *renderdoc.BufferStore, error)) {
	fyne.Do(func() {
		progressBar.Show()
		if loadButton != nil {
			loadButton.Disable()
		}
	})
	xmlPath, convertErr := renderdoc.ConvertToXML(capturePath)
	if convertErr != nil {
		fyne.Do(func() { onFinished(nil, nil, nil, capturePath, "", nil, convertErr) })
		return
	}
	textureReport, parseErr := renderdoc.ParseCaptureXMLFile(xmlPath)
	if parseErr != nil {
		fyne.Do(func() { onFinished(nil, nil, nil, capturePath, xmlPath, nil, parseErr) })
		return
	}
	meshReport, meshErr := renderdoc.ParseMeshReportFromXMLFile(xmlPath)
	if meshErr != nil {
		fyne.Do(func() { onFinished(nil, nil, nil, capturePath, xmlPath, nil, meshErr) })
		return
	}
	store, storeErr := renderdoc.OpenBufferStore(xmlPath)
	if storeErr != nil {
		fyne.Do(func() { onFinished(nil, nil, nil, capturePath, xmlPath, nil, storeErr) })
		return
	}
	renderdoc.ComputeTextureHashes(textureReport, store, nil)
	renderdoc.ApplyBuiltinHashes(textureReport, defaultRobloxPixelHashes)
	materials := renderdoc.BuildMaterialsWithMeshHashes(textureReport, meshReport, store)
	fyne.Do(func() { onFinished(textureReport, meshReport, materials, capturePath, xmlPath, store, nil) })
}

func renderMaterialCell(state *materialsTabState, mat renderdoc.Material, column string, label *widget.Label, img *canvas.Image, table *widget.Table) {
	label.Hide()
	img.Hide()
	switch column {
	case "Color":
		setMaterialThumbnail(state, mat.ColorTextureID, label, img, table)
	case "Normal":
		setMaterialThumbnail(state, mat.NormalTextureID, label, img, table)
	case "MR":
		setMaterialThumbnail(state, mat.MRTextureID, label, img, table)
	case "Color Hash":
		label.SetText(materialColorHash(state, mat))
		label.Show()
	case "Draws":
		label.SetText(strconv.Itoa(mat.DrawCallCount))
		label.Show()
	case "Meshes":
		label.SetText(strconv.Itoa(len(mat.MeshHashes)))
		label.Show()
	case "VRAM":
		label.SetText(format.FormatSizeAuto64(mat.TotalBytes))
		label.Show()
	}
}

func setMaterialThumbnail(state *materialsTabState, texID string, label *widget.Label, img *canvas.Image, table *widget.Table) {
	if texID == "" {
		label.SetText("—")
		label.Show()
		return
	}
	if cached, ok := state.thumbnailCache[texID]; ok && cached != nil {
		img.Image = cached
		img.Refresh()
		img.Show()
		return
	}
	tex, ok := state.textureByID[texID]
	if !ok || state.bufferStore == nil {
		label.SetText("?")
		label.Show()
		return
	}
	label.SetText("…")
	label.Show()
	startTextureDecode(state, tex, func() {
		if table != nil {
			table.Refresh()
		}
	})
}

// startTextureDecode kicks off a background decode for the given texture and
// caches the result. onCached runs on the UI thread once the decode completes
// successfully. If a decode for this texID is already in flight, this is a
// no-op — the existing decode's onCached will fire a table refresh that
// re-renders any cells waiting on the cache.
func startTextureDecode(state *materialsTabState, tex renderdoc.TextureInfo, onCached func()) {
	if state.bufferStore == nil || state.decodeInFlight[tex.ResourceID] {
		return
	}
	state.decodeInFlight[tex.ResourceID] = true
	store := state.bufferStore
	go func() {
		decoded, err := renderdoc.DecodeTexturePreview(tex, store)
		fyne.Do(func() {
			delete(state.decodeInFlight, tex.ResourceID)
			if err != nil || decoded == nil {
				return
			}
			state.thumbnailCache[tex.ResourceID] = decoded
			if onCached != nil {
				onCached()
			}
		})
	}()
}

func materialColorHash(state *materialsTabState, mat renderdoc.Material) string {
	if mat.ColorTextureID == "" {
		return "—"
	}
	if tex, ok := state.textureByID[mat.ColorTextureID]; ok && tex.PixelHash != "" {
		return tex.PixelHash
	}
	return "—"
}

func updateMaterialPreview(state *materialsTabState, mat renderdoc.Material, preview *materialPreview, table *widget.Table) {
	gen := state.previewGeneration
	setPreviewMap(state, mat.ColorTextureID, preview.colorImg, preview.colorLabel, "Color", gen, table)
	setPreviewMap(state, mat.NormalTextureID, preview.normalImg, preview.normalLabel, "Normal", gen, table)
	setPreviewMap(state, mat.MRTextureID, preview.mrImg, preview.mrLabel, "MR", gen, table)

	var b strings.Builder
	fmt.Fprintf(&b, "Draws: %d   Meshes: %d   VRAM: %s\n",
		mat.DrawCallCount, len(mat.MeshHashes), format.FormatSizeAuto64(mat.TotalBytes))
	if len(mat.OtherTextureIDs) > 0 {
		fmt.Fprintf(&b, "Other PS textures: %s\n", strings.Join(mat.OtherTextureIDs, ", "))
	}
	if len(mat.MeshHashes) > 0 {
		b.WriteString("\nMesh hashes (first 16 chars):\n")
		for _, h := range mat.MeshHashes {
			if len(h) > 16 {
				h = h[:16]
			}
			b.WriteString(h + "\n")
		}
	}
	preview.infoEntry.SetText(b.String())
}

func setPreviewMap(state *materialsTabState, texID string, img *canvas.Image, label *widget.Label, kind string, gen int, table *widget.Table) {
	if texID == "" {
		img.Image = nil
		img.Refresh()
		label.SetText(kind + ": —")
		return
	}
	label.SetText(kind + ": " + texID)
	if cached, ok := state.thumbnailCache[texID]; ok && cached != nil {
		img.Image = cached
		img.Refresh()
		return
	}
	img.Image = nil
	img.Refresh()
	tex, ok := state.textureByID[texID]
	if !ok {
		return
	}
	startTextureDecode(state, tex, func() {
		// If a newer click happened, this preview slot belongs to a
		// different material now. Refresh the table (so its thumbnail
		// cell can pick up the cached image), but don't overwrite the
		// preview canvas with our stale result.
		if state.previewGeneration != gen {
			if table != nil {
				table.Refresh()
			}
			return
		}
		if decoded, ok := state.thumbnailCache[texID]; ok {
			img.Image = decoded
			img.Refresh()
		}
		if table != nil {
			table.Refresh()
		}
	})
}

func countTotalDraws(materials []renderdoc.Material) int {
	total := 0
	for _, m := range materials {
		total += m.DrawCallCount
	}
	return total
}

func applyMaterialColumnWidths(table *widget.Table) {
	table.SetColumnWidth(0, 48)
	table.SetColumnWidth(1, 48)
	table.SetColumnWidth(2, 48)
	table.SetColumnWidth(3, 140)
	table.SetColumnWidth(4, 60)
	table.SetColumnWidth(5, 60)
	table.SetColumnWidth(6, 90)
}

func applyMaterialSortAndFilter(state *materialsTabState) {
	filter := strings.ToLower(state.filterText)
	display := state.materials[:0:0]
	for _, m := range state.materials {
		if filter != "" && !materialMatchesFilter(state, m, filter) {
			continue
		}
		display = append(display, m)
	}
	sortMaterials(display, state.sortColumn, state.sortDescending)
	state.displayMaterials = display
}

func materialMatchesFilter(state *materialsTabState, m renderdoc.Material, filter string) bool {
	candidates := []string{m.ColorTextureID, m.NormalTextureID, m.MRTextureID, materialColorHash(state, m)}
	candidates = append(candidates, m.OtherTextureIDs...)
	for _, c := range candidates {
		if strings.Contains(strings.ToLower(c), filter) {
			return true
		}
	}
	return false
}

func sortMaterials(out []renderdoc.Material, column string, descending bool) {
	cmp := func(i, j int) bool { return out[i].TotalBytes > out[j].TotalBytes }
	switch column {
	case "Draws":
		cmp = func(i, j int) bool { return out[i].DrawCallCount > out[j].DrawCallCount }
	case "Meshes":
		cmp = func(i, j int) bool { return len(out[i].MeshHashes) > len(out[j].MeshHashes) }
	case "VRAM":
		cmp = func(i, j int) bool { return out[i].TotalBytes > out[j].TotalBytes }
	}
	if !descending {
		original := cmp
		cmp = func(i, j int) bool { return original(j, i) }
	}
	sort.SliceStable(out, cmp)
}
