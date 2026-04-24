package renderdoctab

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"joxblox/internal/app/ui"
	"joxblox/internal/format"
	"joxblox/internal/renderdoc"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	fyneDialog "fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
	nativeDialog "github.com/sqweek/dialog"
)

type meshesTabState struct {
	meshes         []renderdoc.MeshInfo
	displayMeshes  []renderdoc.MeshInfo
	report         *renderdoc.MeshReport
	bufferStore    *renderdoc.BufferStore
	xmlPath        string
	sortColumn     string
	sortDescending bool
	filterText     string
	selectedRow    int
}

var meshColumnHeaders = []string{"ID", "Verts", "Tris", "VB bytes", "IB bytes", "Draws", "Layout", "Hash"}

func newMeshesSubTab(window fyne.Window) fyne.CanvasObject {
	state := &meshesTabState{
		sortColumn:     "VB bytes",
		sortDescending: true,
		selectedRow:    -1,
	}

	pathLabel := widget.NewLabel("No capture loaded.")
	pathLabel.Wrapping = fyne.TextWrapWord
	summaryLabel := widget.NewLabel("")
	summaryLabel.Wrapping = fyne.TextWrapWord
	countLabel := widget.NewLabel("")

	progressBar := widget.NewProgressBarInfinite()
	progressBar.Hide()

	filterEntry := widget.NewEntry()
	filterEntry.SetPlaceHolder("Filter by hash or layout")

	previewWidget := ui.NewMeshPreviewWidget()
	previewInfoLabel := widget.NewMultiLineEntry()
	previewInfoLabel.Wrapping = fyne.TextWrapWord
	previewInfoLabel.SetText("Select a mesh to preview.")
	previewInfoLabel.Disable()

	previewPane := container.NewBorder(previewInfoLabel, nil, nil, nil, previewWidget)

	var table *widget.Table
	table = widget.NewTableWithHeaders(
		func() (int, int) { return len(state.displayMeshes), len(meshColumnHeaders) },
		func() fyne.CanvasObject { return widget.NewLabel("") },
		func(id widget.TableCellID, object fyne.CanvasObject) {
			label := object.(*widget.Label)
			if id.Row < 0 || id.Row >= len(state.displayMeshes) {
				label.SetText("")
				return
			}
			label.SetText(meshColumnValue(state.displayMeshes[id.Row], meshColumnHeaders[id.Col]))
		},
	)
	table.CreateHeader = func() fyne.CanvasObject { return widget.NewButton("", nil) }
	table.UpdateHeader = func(id widget.TableCellID, object fyne.CanvasObject) {
		button := object.(*widget.Button)
		if id.Row == -1 && id.Col >= 0 && id.Col < len(meshColumnHeaders) {
			name := meshColumnHeaders[id.Col]
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
				applyMeshSortAndFilter(state)
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
	applyMeshColumnWidths(table)
	table.OnSelected = func(id widget.TableCellID) {
		if id.Row < 0 || id.Row >= len(state.displayMeshes) {
			return
		}
		state.selectedRow = id.Row
		mesh := state.displayMeshes[id.Row]
		loadMeshPreview(state, mesh, previewWidget, previewInfoLabel)
	}

	filterEntry.OnChanged = func(text string) {
		state.filterText = strings.TrimSpace(text)
		applyMeshSortAndFilter(state)
		table.Refresh()
		countLabel.SetText(fmt.Sprintf("Showing %d of %d meshes", len(state.displayMeshes), len(state.meshes)))
	}

	var loadButton *widget.Button
	onLoadFinished := func(report *renderdoc.MeshReport, meshes []renderdoc.MeshInfo, loadedPath, xmlPath string, newStore *renderdoc.BufferStore, loadErr error) {
		progressBar.Hide()
		if loadButton != nil {
			loadButton.Enable()
		}
		if loadErr != nil {
			pathLabel.SetText(fmt.Sprintf("Load failed: %s", loadedPath))
			fyneDialog.ShowError(loadErr, window)
			if newStore != nil {
				_ = newStore.Close()
			}
			if xmlPath != "" {
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
		state.report = report
		state.meshes = meshes
		state.bufferStore = newStore
		state.xmlPath = xmlPath
		state.selectedRow = -1
		applyMeshSortAndFilter(state)
		pathLabel.SetText(fmt.Sprintf("Loaded: %s", loadedPath))
		summaryLabel.SetText(buildMeshSummary(meshes))
		countLabel.SetText(fmt.Sprintf("Showing %d of %d meshes", len(state.displayMeshes), len(state.meshes)))
		previewWidget.Clear()
		previewInfoLabel.SetText("Select a mesh to preview.")
		table.Refresh()
	}

	loadButton = widget.NewButton("Load .rdc…", func() {
		go pickAndLoadMeshCapture(window, progressBar, loadButton, onLoadFinished)
	})

	header := container.NewVBox(
		container.NewBorder(nil, nil, nil, loadButton, pathLabel),
		summaryLabel,
		progressBar,
		filterEntry,
	)
	split := container.NewHSplit(table, previewPane)
	split.Offset = 0.55
	return container.NewBorder(header, countLabel, nil, nil, split)
}

func meshColumnValue(mesh renderdoc.MeshInfo, column string) string {
	switch column {
	case "ID":
		return mesh.FirstResourceID
	case "Verts":
		stride := 0
		if len(mesh.VertexBuffers) > 0 {
			stride = mesh.VertexBuffers[0].Stride
		}
		if stride <= 0 {
			return "-"
		}
		return strconv.Itoa(mesh.VertexBufferBytes / stride)
	case "Tris":
		return strconv.Itoa(mesh.IndexCount / 3)
	case "VB bytes":
		return format.FormatSizeAuto64(int64(mesh.VertexBufferBytes))
	case "IB bytes":
		return format.FormatSizeAuto64(int64(mesh.IndexBufferBytes))
	case "Draws":
		return strconv.Itoa(mesh.DrawCallCount)
	case "Layout":
		return mesh.InputLayoutID
	case "Hash":
		return truncateHash(mesh.Hash)
	}
	return ""
}

func truncateHash(hash string) string {
	if len(hash) > 16 {
		return hash[:16]
	}
	return hash
}

func applyMeshColumnWidths(table *widget.Table) {
	widths := map[int]float32{0: 80, 1: 80, 2: 80, 3: 100, 4: 100, 5: 70, 6: 90, 7: 150}
	for col, w := range widths {
		table.SetColumnWidth(col, w)
	}
}

func applyMeshSortAndFilter(state *meshesTabState) {
	lower := strings.ToLower(state.filterText)
	filtered := make([]renderdoc.MeshInfo, 0, len(state.meshes))
	for _, mesh := range state.meshes {
		if lower != "" {
			haystack := strings.ToLower(mesh.Hash + " " + mesh.InputLayoutID)
			if !strings.Contains(haystack, lower) {
				continue
			}
		}
		filtered = append(filtered, mesh)
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		less := compareMeshes(filtered[i], filtered[j], state.sortColumn)
		if state.sortDescending {
			return !less
		}
		return less
	})
	state.displayMeshes = filtered
}

func compareMeshes(a, b renderdoc.MeshInfo, column string) bool {
	switch column {
	case "ID":
		return a.FirstResourceID < b.FirstResourceID
	case "Verts", "Tris", "VB bytes":
		return a.VertexBufferBytes < b.VertexBufferBytes
	case "IB bytes":
		return a.IndexBufferBytes < b.IndexBufferBytes
	case "Draws":
		return a.DrawCallCount < b.DrawCallCount
	case "Layout":
		return a.InputLayoutID < b.InputLayoutID
	case "Hash":
		return a.Hash < b.Hash
	}
	return false
}

func buildMeshSummary(meshes []renderdoc.MeshInfo) string {
	totalVB, totalIB, draws := int64(0), int64(0), 0
	for _, m := range meshes {
		totalVB += int64(m.VertexBufferBytes)
		totalIB += int64(m.IndexBufferBytes)
		draws += m.DrawCallCount
	}
	return fmt.Sprintf("%d unique meshes · VB %s · IB %s · %d draw calls total",
		len(meshes),
		format.FormatSizeAuto64(totalVB),
		format.FormatSizeAuto64(totalIB),
		draws,
	)
}

func loadMeshPreview(state *meshesTabState, mesh renderdoc.MeshInfo, viewer *ui.MeshPreviewWidget, infoLabel *widget.Entry) {
	infoLabel.SetText(fmt.Sprintf("Decoding %s · %d verts · %d tris…",
		mesh.FirstResourceID,
		vertexCountOf(mesh),
		mesh.IndexCount/3,
	))
	if state.bufferStore == nil {
		infoLabel.SetText("No capture loaded.")
		return
	}
	go func() {
		data, err := buildMeshPreviewData(state.report, mesh, state.bufferStore)
		fyne.Do(func() {
			if err != nil {
				if errors.Is(err, renderdoc.ErrUnsupportedPositionFormat) {
					infoLabel.SetText(fmt.Sprintf("%s · preview format not supported yet", mesh.FirstResourceID))
				} else {
					infoLabel.SetText(fmt.Sprintf("%s · decode failed: %s", mesh.FirstResourceID, err.Error()))
				}
				viewer.Clear()
				return
			}
			infoLabel.SetText(fmt.Sprintf("%s · %d verts · %d tris · %s VB · hash %s",
				mesh.FirstResourceID,
				vertexCountOf(mesh),
				mesh.IndexCount/3,
				format.FormatSizeAuto64(int64(mesh.VertexBufferBytes)),
				truncateHash(mesh.Hash),
			))
			viewer.SetData(data)
		})
	}()
}

func vertexCountOf(mesh renderdoc.MeshInfo) int {
	if len(mesh.VertexBuffers) == 0 || mesh.VertexBuffers[0].Stride <= 0 {
		return 0
	}
	return mesh.VertexBufferBytes / mesh.VertexBuffers[0].Stride
}

func buildMeshPreviewData(report *renderdoc.MeshReport, mesh renderdoc.MeshInfo, store *renderdoc.BufferStore) (ui.MeshPreviewData, error) {
	if report == nil || len(mesh.VertexBuffers) == 0 {
		return ui.MeshPreviewData{}, fmt.Errorf("no geometry for mesh %s", mesh.FirstResourceID)
	}
	layout, ok := report.InputLayouts[mesh.InputLayoutID]
	if !ok {
		return ui.MeshPreviewData{}, fmt.Errorf("input layout %s not found", mesh.InputLayoutID)
	}
	var positionElement *renderdoc.InputLayoutElement
	for i := range layout.Elements {
		if strings.EqualFold(layout.Elements[i].SemanticName, "POSITION") && layout.Elements[i].SemanticIndex == 0 {
			positionElement = &layout.Elements[i]
			break
		}
	}
	if positionElement == nil {
		return ui.MeshPreviewData{}, fmt.Errorf("no POSITION semantic in layout %s", mesh.InputLayoutID)
	}

	var positionVB renderdoc.DrawCallVertexBuffer
	found := false
	for _, vb := range mesh.VertexBuffers {
		if vb.Slot == positionElement.InputSlot {
			positionVB = vb
			found = true
			break
		}
	}
	if !found {
		return ui.MeshPreviewData{}, fmt.Errorf("no VB bound to position slot %d", positionElement.InputSlot)
	}

	vbBufInfo, ok := report.Buffers[positionVB.BufferID]
	if !ok || vbBufInfo.InitialDataBufferID == "" {
		return ui.MeshPreviewData{}, fmt.Errorf("VB %s has no InitialData", positionVB.BufferID)
	}
	vbBytes, err := store.ReadBuffer(vbBufInfo.InitialDataBufferID)
	if err != nil {
		return ui.MeshPreviewData{}, fmt.Errorf("read VB: %w", err)
	}
	positions, err := renderdoc.DecodePositions(vbBytes, positionElement.Format, positionElement.AlignedByteOffset, positionVB.Stride)
	if err != nil {
		return ui.MeshPreviewData{}, err
	}

	ibBufInfo, ok := report.Buffers[mesh.IndexBufferID]
	if !ok || ibBufInfo.InitialDataBufferID == "" {
		return ui.MeshPreviewData{}, fmt.Errorf("IB %s has no InitialData", mesh.IndexBufferID)
	}
	ibBytes, err := store.ReadBuffer(ibBufInfo.InitialDataBufferID)
	if err != nil {
		return ui.MeshPreviewData{}, fmt.Errorf("read IB: %w", err)
	}
	indices, err := renderdoc.DecodeIndices(ibBytes, mesh.IndexBufferFormat)
	if err != nil {
		return ui.MeshPreviewData{}, err
	}
	triangleCount := uint32(len(indices) / 3)
	return ui.MeshPreviewData{
		RawPositions:         positions,
		RawIndices:           indices,
		TriangleCount:        triangleCount,
		PreviewTriangleCount: triangleCount,
	}, nil
}

func pickAndLoadMeshCapture(window fyne.Window, progressBar *widget.ProgressBarInfinite, loadButton *widget.Button, onFinished func(*renderdoc.MeshReport, []renderdoc.MeshInfo, string, string, *renderdoc.BufferStore, error)) {
	capturePath, err := nativeDialog.File().
		Filter("RenderDoc capture", "rdc").
		Title("Select RenderDoc capture (.rdc)").
		Load()
	if err != nil {
		if errors.Is(err, nativeDialog.Cancelled) {
			return
		}
		fyne.Do(func() { fyneDialog.ShowError(err, window) })
		return
	}
	fyne.Do(func() {
		progressBar.Show()
		if loadButton != nil {
			loadButton.Disable()
		}
	})

	xmlPath, convertErr := renderdoc.ConvertToXML(capturePath)
	if convertErr != nil {
		fyne.Do(func() { onFinished(nil, nil, capturePath, "", nil, convertErr) })
		return
	}
	report, parseErr := renderdoc.ParseMeshReportFromXMLFile(xmlPath)
	if parseErr != nil {
		fyne.Do(func() { onFinished(nil, nil, capturePath, xmlPath, nil, parseErr) })
		return
	}
	store, storeErr := renderdoc.OpenBufferStore(xmlPath)
	if storeErr != nil {
		fyne.Do(func() { onFinished(report, nil, capturePath, xmlPath, nil, storeErr) })
		return
	}
	meshes, buildErr := renderdoc.BuildMeshes(report, store)
	if buildErr != nil {
		_ = store.Close()
		fyne.Do(func() { onFinished(report, nil, capturePath, xmlPath, nil, buildErr) })
		return
	}
	fyne.Do(func() { onFinished(report, meshes, capturePath, xmlPath, store, nil) })
}
