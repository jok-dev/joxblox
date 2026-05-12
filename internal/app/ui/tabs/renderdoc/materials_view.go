package renderdoctab

import (
	"fmt"
	"image"
	"sort"
	"strconv"
	"strings"

	"joxblox/internal/app/loader"
	"joxblox/internal/app/ui"
	"joxblox/internal/app/ui/materialscommon"
	"joxblox/internal/assetmatch"
	"joxblox/internal/debug"
	"joxblox/internal/format"
	"joxblox/internal/renderdoc"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	fyneDialog "fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

// Preview slot indices for the 3-up Color/Normal/MR pane.
const (
	previewSlotColor  = 0
	previewSlotNormal = 1
	previewSlotMR     = 2
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
	thumbnails       *materialscommon.ThumbnailCache
	textureByID      map[string]renderdoc.TextureInfo
	// previewGeneration increments on every row click. Background decodes
	// capture the value at start and discard their result if a newer click
	// has happened, so a slow Color decode for an old row can't overwrite
	// a freshly-clicked row's preview pane.
	previewGeneration int
	// corpus + per-texture match overlay populated when a Scan tab scan
	// completes AND a capture is loaded. Materials surface the Color
	// slot's matched ID; nil corpus → "—" everywhere.
	corpus                  *assetmatch.TextureCorpus
	matchByTexID            map[string]int64
	matchAllByTexID         map[string][]int64
	openInSingleAssetButton *widget.Button
	corpusBuildInFlight     bool
	corpusRebuildPending    bool
}

var materialColumnHeaders = []string{"Color", "Normal", "MR", "Color Hash", "Draws", "Meshes", "VRAM", "Studio Asset"}

func newMaterialsSubTab(window fyne.Window, onLoaded func(path string)) (fyne.CanvasObject, func(path string)) {
	state := &materialsTabState{
		sortColumn:      "VRAM",
		sortDescending:  true,
		selectedRow:     -1,
		thumbnails:      materialscommon.NewThumbnailCache(),
		textureByID:     map[string]renderdoc.TextureInfo{},
		matchByTexID:    map[string]int64{},
		matchAllByTexID: map[string][]int64{},
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

	preview := materialscommon.NewPreviewPane([]materialscommon.PreviewSlot{
		{Title: "Color"},
		{Title: "Normal"},
		{Title: "MR"},
	}, "Select a material to preview.")

	openInSingleAssetButton := widget.NewButton("Open in Single Asset", func() {
		if state.selectedRow < 0 || state.selectedRow >= len(state.displayMaterials) {
			return
		}
		mat := state.displayMaterials[state.selectedRow]
		id, ok := state.matchByTexID[mat.ColorTextureID]
		if !ok || ui.OpenSingleAsset == nil {
			return
		}
		ui.OpenSingleAsset(id)
	})
	openInSingleAssetButton.Hide()
	state.openInSingleAssetButton = openInSingleAssetButton
	previewPane := container.NewBorder(nil, openInSingleAssetButton, nil, nil, preview.Container())

	var table *widget.Table
	table = widget.NewTableWithHeaders(
		func() (int, int) { return len(state.displayMaterials), len(materialColumnHeaders) },
		func() fyne.CanvasObject { return materialscommon.NewThumbnailCell() },
		func(id widget.TableCellID, object fyne.CanvasObject) {
			if id.Row < 0 || id.Row >= len(state.displayMaterials) || id.Col < 0 || id.Col >= len(materialColumnHeaders) {
				return
			}
			label := materialscommon.ThumbnailCellLabel(object)
			img := materialscommon.ThumbnailCellImage(object)
			if label == nil || img == nil {
				return
			}
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
		state.thumbnails = materialscommon.NewThumbnailCache()
		state.previewGeneration = 0
		state.textureByID = map[string]renderdoc.TextureInfo{}
		for _, t := range textureReport.Textures {
			state.textureByID[t.ResourceID] = t
		}
		recomputeMaterialMatches(state)
		state.filterText = strings.TrimSpace(filterEntry.Text)
		state.selectedRow = -1
		applyMaterialSortAndFilter(state)
		pathLabel.SetText(fmt.Sprintf("Loaded: %s", loadedPath))
		colorBytes, normalBytes, mrBytes := perCategoryAssetBytes(materials, state.textureByID)
		summaryLabel.SetText(fmt.Sprintf("%d materials across %d draw calls   ·   Color: %s   ·   Normal: %s   ·   MR: %s",
			len(materials), countTotalDraws(materials),
			format.FormatSizeAuto64(colorBytes),
			format.FormatSizeAuto64(normalBytes),
			format.FormatSizeAuto64(mrBytes),
		))
		countLabel.SetText(fmt.Sprintf("Showing %d of %d materials", len(state.displayMaterials), len(state.materials)))
		preview.Reset("Select a material to preview.")
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

	// Subscribe to scan-completion events so the Studio Asset column gets
	// populated once a place file is scanned. Unsubscribe is dropped — the
	// sub-tab lives for the whole app session.
	_ = loader.SubscribeScanCompleted(func() {
		requestMaterialCorpusRebuild(state, table)
	})
	if existing := loader.CurrentScan(); len(existing) > 0 {
		requestMaterialCorpusRebuild(state, table)
	}

	return container.NewBorder(header, footer, nil, nil, split), loadFromPath
}

// requestMaterialCorpusRebuild kicks off a corpus build for the current
// scan results. If a build is already in flight, sets "rebuild pending"
// instead of spawning another — keeps the publish bus from saturating
// CPU + I/O when scan events fire in quick succession. Must be called
// on the UI thread.
func requestMaterialCorpusRebuild(state *materialsTabState, table *widget.Table) {
	if state.corpusBuildInFlight {
		state.corpusRebuildPending = true
		return
	}
	state.corpusBuildInFlight = true
	scan := loader.CurrentScan()
	go func() {
		corpus, err := assetmatch.BuildTextureCorpus(scan, nil)
		fyne.Do(func() {
			state.corpusBuildInFlight = false
			if err != nil {
				debug.Logf("materials sub-tab: corpus build failed: %s", err.Error())
			} else {
				state.corpus = corpus
				recomputeMaterialMatches(state)
				table.Refresh()
			}
			if state.corpusRebuildPending {
				state.corpusRebuildPending = false
				requestMaterialCorpusRebuild(state, table)
			}
		})
	}()
}

func recomputeMaterialMatches(state *materialsTabState) {
	state.matchByTexID = map[string]int64{}
	state.matchAllByTexID = map[string][]int64{}
	if state.corpus == nil || state.textureReport == nil {
		return
	}
	for _, tex := range state.textureReport.Textures {
		if tex.DHash == 0 {
			continue
		}
		matches := state.corpus.Match(tex.DHash)
		if len(matches) == 0 {
			continue
		}
		state.matchByTexID[tex.ResourceID] = matches[0]
		state.matchAllByTexID[tex.ResourceID] = matches
	}
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
	case "Studio Asset":
		label.SetText(materialStudioAssetCell(state, mat))
		label.Show()
	}
}

func materialStudioAssetCell(state *materialsTabState, mat renderdoc.Material) string {
	if mat.ColorTextureID == "" {
		return "—"
	}
	id, ok := state.matchByTexID[mat.ColorTextureID]
	if !ok {
		return "—"
	}
	if extras := len(state.matchAllByTexID[mat.ColorTextureID]) - 1; extras > 0 {
		return fmt.Sprintf("%d (+%d)", id, extras)
	}
	return strconv.FormatInt(id, 10)
}

func setMaterialThumbnail(state *materialsTabState, texID string, label *widget.Label, img *canvas.Image, table *widget.Table) {
	if texID == "" {
		label.SetText("—")
		label.Show()
		return
	}
	if cached, ok := state.thumbnails.Get(texID); ok && cached != nil {
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
	store := state.bufferStore
	state.thumbnails.RequestDecode(texID, func() (image.Image, error) {
		return renderdoc.DecodeTexturePreview(tex, store)
	}, func() {
		if table != nil {
			table.Refresh()
		}
	})
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

func updateMaterialPreview(state *materialsTabState, mat renderdoc.Material, preview *materialscommon.PreviewPane, table *widget.Table) {
	gen := state.previewGeneration
	setPreviewSlot(state, previewSlotColor, mat.ColorTextureID, preview, gen, table)
	setPreviewSlot(state, previewSlotNormal, mat.NormalTextureID, preview, gen, table)
	setPreviewSlot(state, previewSlotMR, mat.MRTextureID, preview, gen, table)
	if state.openInSingleAssetButton != nil {
		if _, ok := state.matchByTexID[mat.ColorTextureID]; ok && mat.ColorTextureID != "" {
			state.openInSingleAssetButton.Show()
		} else {
			state.openInSingleAssetButton.Hide()
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Draws: %d   Meshes: %d   VRAM: %s\n",
		mat.DrawCallCount, len(mat.MeshHashes), format.FormatSizeAuto64(mat.TotalBytes))
	if len(mat.OtherTextureIDs) > 0 {
		fmt.Fprintf(&b, "Other PS textures: %s\n", strings.Join(mat.OtherTextureIDs, ", "))
	}
	if id, ok := state.matchByTexID[mat.ColorTextureID]; ok && mat.ColorTextureID != "" {
		fmt.Fprintf(&b, "Studio Asset (Color): %d\n", id)
		if all := state.matchAllByTexID[mat.ColorTextureID]; len(all) > 1 {
			extras := make([]string, 0, len(all)-1)
			for _, c := range all[1:] {
				extras = append(extras, strconv.FormatInt(c, 10))
			}
			fmt.Fprintf(&b, "Also: %s\n", strings.Join(extras, ", "))
		}
	} else if state.corpus != nil {
		b.WriteString("Studio Asset: not identified\n")
	} else {
		b.WriteString("Studio Asset: load a place file in the Scan tab to identify\n")
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
	preview.SetInfo(b.String())
}

func setPreviewSlot(state *materialsTabState, slotIndex int, texID string, preview *materialscommon.PreviewPane, gen int, table *widget.Table) {
	if texID == "" {
		preview.ClearSlot(slotIndex)
		return
	}
	if cached, ok := state.thumbnails.Get(texID); ok && cached != nil {
		preview.SetImage(slotIndex, texID, cached)
		return
	}
	preview.SetImage(slotIndex, texID, nil)
	tex, ok := state.textureByID[texID]
	if !ok || state.bufferStore == nil {
		return
	}
	store := state.bufferStore
	state.thumbnails.RequestDecode(texID, func() (image.Image, error) {
		return renderdoc.DecodeTexturePreview(tex, store)
	}, func() {
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
		if decoded, ok := state.thumbnails.Get(texID); ok {
			preview.SetImage(slotIndex, texID, decoded)
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

// perCategoryAssetBytes sums the unique-texture VRAM cost across all
// materials, split by slot. A texture shared across multiple materials is
// counted once. "Assets only" is implicit — BuildMaterials already filtered
// scene-globals (built-ins, render targets, cubemaps, anything bound to ≥80%
// of draws) out of the slot assignments, so only per-material textures
// contribute.
func perCategoryAssetBytes(materials []renderdoc.Material, byID map[string]renderdoc.TextureInfo) (color, normal, mr int64) {
	colorIDs := map[string]bool{}
	normalIDs := map[string]bool{}
	mrIDs := map[string]bool{}
	for _, m := range materials {
		if m.ColorTextureID != "" {
			colorIDs[m.ColorTextureID] = true
		}
		if m.NormalTextureID != "" {
			normalIDs[m.NormalTextureID] = true
		}
		if m.MRTextureID != "" {
			mrIDs[m.MRTextureID] = true
		}
	}
	sumIDs := func(ids map[string]bool) int64 {
		var total int64
		for id := range ids {
			if t, ok := byID[id]; ok {
				total += t.Bytes
			}
		}
		return total
	}
	return sumIDs(colorIDs), sumIDs(normalIDs), sumIDs(mrIDs)
}

func applyMaterialColumnWidths(table *widget.Table) {
	table.SetColumnWidth(0, 48)
	table.SetColumnWidth(1, 48)
	table.SetColumnWidth(2, 48)
	table.SetColumnWidth(3, 140)
	table.SetColumnWidth(4, 60)
	table.SetColumnWidth(5, 60)
	table.SetColumnWidth(6, 90)
	table.SetColumnWidth(7, 110) // Studio Asset
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
