package renderdoctab

import (
	"errors"
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
	nativeDialog "github.com/sqweek/dialog"
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
	textureByID      map[string]renderdoc.TextureInfo
}

var materialColumnHeaders = []string{"Color", "Normal", "MR", "Color Hash", "Draws", "Meshes", "VRAM"}

func newMaterialsSubTab(window fyne.Window, onLoaded func(path string)) (fyne.CanvasObject, func(path string)) {
	state := &materialsTabState{
		sortColumn:     "VRAM",
		sortDescending: true,
		selectedRow:    -1,
		thumbnailCache: map[string]image.Image{},
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

	previewInfoLabel := widget.NewMultiLineEntry()
	previewInfoLabel.Wrapping = fyne.TextWrapWord
	previewInfoLabel.SetText("Select a material to preview.")
	previewInfoLabel.Disable()
	previewPane := container.NewMax(previewInfoLabel)

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
			renderMaterialCell(state, state.displayMaterials[id.Row], materialColumnHeaders[id.Col], label, img)
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
		updateMaterialPreviewPlaceholder(state, state.displayMaterials[id.Row], previewInfoLabel)
	}

	filterEntry.OnChanged = func(text string) {
		state.filterText = strings.TrimSpace(text)
		applyMaterialSortAndFilter(state)
		table.Refresh()
		countLabel.SetText(fmt.Sprintf("Showing %d of %d materials", len(state.displayMaterials), len(state.materials)))
	}

	var loadButton *widget.Button
	onLoadFinished := func(textureReport *renderdoc.Report, meshReport *renderdoc.MeshReport, materials []renderdoc.Material, loadedPath string, xmlPath string, store *renderdoc.BufferStore, loadErr error) {
		progressBar.Hide()
		if loadButton != nil {
			loadButton.Enable()
		}
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
		previewInfoLabel.SetText("Select a material to preview.")
		table.Refresh()
		if onLoaded != nil {
			onLoaded(loadedPath)
		}
	}

	loadFromPath := func(path string) {
		go loadMaterialsCaptureFromPath(window, progressBar, loadButton, path, onLoadFinished)
	}

	loadButton = widget.NewButton("Load .rdc…", func() {
		path, err := nativeDialog.File().Filter(rdcFileFilterLabel, "rdc").Title("Select RenderDoc capture (.rdc)").Load()
		if err != nil {
			if !errors.Is(err, nativeDialog.Cancelled) {
				fyneDialog.ShowError(err, window)
			}
			return
		}
		loadFromPath(path)
	})

	header := container.NewVBox(
		container.NewBorder(nil, nil, nil, loadButton, pathLabel),
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

func renderMaterialCell(state *materialsTabState, mat renderdoc.Material, column string, label *widget.Label, img *canvas.Image) {
	label.Hide()
	img.Hide()
	switch column {
	case "Color":
		setMaterialThumbnail(state, mat.ColorTextureID, label, img)
	case "Normal":
		setMaterialThumbnail(state, mat.NormalTextureID, label, img)
	case "MR":
		setMaterialThumbnail(state, mat.MRTextureID, label, img)
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

func setMaterialThumbnail(state *materialsTabState, texID string, label *widget.Label, img *canvas.Image) {
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
	decoded, err := renderdoc.DecodeTexturePreview(tex, state.bufferStore)
	if err != nil || decoded == nil {
		label.SetText("?")
		label.Show()
		return
	}
	state.thumbnailCache[texID] = decoded
	img.Image = decoded
	img.Refresh()
	img.Show()
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

func updateMaterialPreviewPlaceholder(state *materialsTabState, mat renderdoc.Material, label *widget.Entry) {
	var b strings.Builder
	fmt.Fprintf(&b, "Color: %s\nNormal: %s\nMR: %s\n",
		nonEmptyOrDash(mat.ColorTextureID),
		nonEmptyOrDash(mat.NormalTextureID),
		nonEmptyOrDash(mat.MRTextureID))
	if len(mat.OtherTextureIDs) > 0 {
		fmt.Fprintf(&b, "Other: %s\n", strings.Join(mat.OtherTextureIDs, ", "))
	}
	fmt.Fprintf(&b, "\nDraws: %d  Meshes: %d  VRAM: %s\n",
		mat.DrawCallCount, len(mat.MeshHashes), format.FormatSizeAuto64(mat.TotalBytes))
	if len(mat.MeshHashes) > 0 {
		b.WriteString("\nMesh hashes:\n")
		for _, h := range mat.MeshHashes {
			if len(h) > 16 {
				h = h[:16]
			}
			b.WriteString(h + "\n")
		}
	}
	label.SetText(b.String())
}

func nonEmptyOrDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
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
