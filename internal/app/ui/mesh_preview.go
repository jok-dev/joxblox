package ui

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"

	"joxblox/internal/debug"
	"joxblox/internal/extractor"
	"joxblox/internal/format"
)

const MaxMeshPreviewTriangles = 20000
const MinMeshPreviewRenderDimension = 32

const (
	PreviewWidth  = 440
	PreviewHeight = 300
)

var GetPrimaryWindow func() fyne.Window
var LoadMouseLookSensitivity func() float64
var GetRepositoryRootPath func() (string, error)

// PrimaryWindow returns GetPrimaryWindow when it is set and non-nil, otherwise the first Fyne window.
func PrimaryWindow() fyne.Window {
	if GetPrimaryWindow != nil {
		if w := GetPrimaryWindow(); w != nil {
			return w
		}
	}
	currentApp := fyne.CurrentApp()
	if currentApp == nil {
		return nil
	}
	windows := currentApp.Driver().AllWindows()
	if len(windows) == 0 {
		return nil
	}
	return windows[0]
}

const (
	meshPreviewKeyboardTickInterval        = time.Second / 60
	meshPreviewKeyboardMovePerSecond       = 1.8
	meshPreviewKeyboardFastMultiplier      = 2.75
	meshPreviewKeyboardMaximumPitchRad     = 1.35
	MeshPreviewDefaultMouseLookSensitivity = 0.00625
	MeshPreviewMinimumMouseLookSensitivity = 0.001
	MeshPreviewMaximumMouseLookSensitivity = 0.03
	MeshPreviewMouseLookSensitivityStep    = 0.0005
	meshPreviewScrollMoveDistance          = 0.35
)

type MeshPreviewData struct {
	RawPositions         []float32
	RawIndices           []uint32
	RawColors            []uint8
	Batches              []MeshPreviewBatchData
	TriangleCount        uint32
	PreviewTriangleCount uint32
}

type MeshPreviewBatchData struct {
	RawPositions []float32
	RawIndices   []uint32
	RawColors    []uint8
}

type MeshPreviewWidget struct {
	widget.BaseWidget
	background     *canvas.Rectangle
	image          *canvas.Image
	data           MeshPreviewData
	selectedBatch  int
	opacity        float64
	cameraX        float64
	cameraY        float64
	cameraZ        float64
	yaw            float64
	pitch          float64
	zoom           float64
	rightMouseDown bool
	pickToken      atomic.Uint64
	renderToken    atomic.Uint64
	process        *meshRendererProcess
	OnBatchTapped  func(batchIndex int)
	focusCanvas    fyne.Canvas
	keyStateMutex  sync.Mutex
	keyState       meshPreviewKeyState
	movementStop   chan struct{}
	lastMousePos   fyne.Position
	hasLastMouse   bool
	pendingLookDX  float32
	pendingLookDY  float32
}

type meshPreviewKeyState struct {
	forward  bool
	backward bool
	left     bool
	right    bool
	fast     bool
}

func NewMeshPreviewWidget() *MeshPreviewWidget {
	initialYaw := -0.35
	initialPitch := 0.3
	initialZoom := 1.0
	initialCameraX, initialCameraY, initialCameraZ := meshPreviewInitialCameraPosition(initialYaw, initialPitch, initialZoom)
	viewer := &MeshPreviewWidget{
		background:    canvas.NewRectangle(color.NRGBA{R: 14, G: 17, B: 22, A: 255}),
		image:         canvas.NewImageFromImage(nil),
		selectedBatch: -1,
		opacity:       1.0,
		cameraX:       initialCameraX,
		cameraY:       initialCameraY,
		cameraZ:       initialCameraZ,
		yaw:           initialYaw,
		pitch:         initialPitch,
		zoom:          initialZoom,
	}
	viewer.image.FillMode = canvas.ImageFillStretch
	viewer.image.ScaleMode = canvas.ImageScaleFastest
	viewer.ExtendBaseWidget(viewer)
	return viewer
}

func MeshPreviewControlsText() string {
	return "Hold right click to look, WASD to move, scroll to move forward/back, Shift to go faster"
}

func (viewer *MeshPreviewWidget) CreateRenderer() fyne.WidgetRenderer {
	content := container.NewWithoutLayout(viewer.background, viewer.image)
	return widget.NewSimpleRenderer(content)
}

func (viewer *MeshPreviewWidget) MinSize() fyne.Size {
	return fyne.NewSize(PreviewWidth, PreviewHeight)
}

func (viewer *MeshPreviewWidget) Resize(size fyne.Size) {
	viewer.BaseWidget.Resize(size)
	viewer.background.Resize(size)
	viewer.background.Move(fyne.NewPos(0, 0))
	viewer.image.Resize(size)
	viewer.image.Move(fyne.NewPos(0, 0))
	viewer.render()
}

func (viewer *MeshPreviewWidget) Dragged(event *fyne.DragEvent) {
}

func (viewer *MeshPreviewWidget) DragEnd() {}

func (viewer *MeshPreviewWidget) applyMouseLookDelta(deltaX float32, deltaY float32) {
	if viewer == nil || !viewer.data.HasRenderableGeometry() {
		return
	}
	lookSensitivity := MeshPreviewDefaultMouseLookSensitivity
	if LoadMouseLookSensitivity != nil {
		lookSensitivity = LoadMouseLookSensitivity()
	}
	viewer.yaw -= float64(deltaX) * lookSensitivity
	viewer.pitch += float64(deltaY) * lookSensitivity
	viewer.pitch = format.Clamp(viewer.pitch, -meshPreviewKeyboardMaximumPitchRad, meshPreviewKeyboardMaximumPitchRad)
}

func (viewer *MeshPreviewWidget) Tapped(event *fyne.PointEvent) {
	if viewer == nil || event == nil || !viewer.data.HasRenderableGeometry() {
		return
	}
	viewer.requestFocus()
	size := viewer.Size()
	if size.Width < 1 || size.Height < 1 {
		return
	}
	proc := viewer.process
	if proc == nil || !proc.isRunning() {
		return
	}

	pickToken := viewer.pickToken.Add(1)
	cameraXSnapshot, cameraYSnapshot, cameraZSnapshot := viewer.cameraPosition()
	yawSnapshot := viewer.yaw
	pitchSnapshot := viewer.pitch
	clickX := int(math.Round(float64(event.Position.X)))
	clickY := int(math.Round(float64(event.Position.Y)))
	width := int(math.Max(1, float64(size.Width)))
	height := int(math.Max(1, float64(size.Height)))

	go func(expectedToken uint64, activeProcess *meshRendererProcess) {
		batchIndex, pickErr := activeProcess.pick(width, height, cameraXSnapshot, cameraYSnapshot, cameraZSnapshot, yawSnapshot, pitchSnapshot, 1.0, clickX, clickY)
		if pickErr != nil {
			debug.Logf("mesh renderer subprocess pick failed: %s", pickErr.Error())
			return
		}
		fyne.Do(func() {
			if viewer == nil || viewer.pickToken.Load() != expectedToken || viewer.process != activeProcess {
				return
			}
			viewer.SetSelectedBatch(batchIndex)
			if batchIndex >= 0 && viewer.OnBatchTapped != nil {
				viewer.OnBatchTapped(batchIndex)
			}
		})
	}(pickToken, proc)
}

func rayTriangleIntersect(ox, oy, oz, dx, dy, dz, v0x, v0y, v0z, v1x, v1y, v1z, v2x, v2y, v2z float64) float64 {
	e1x, e1y, e1z := v1x-v0x, v1y-v0y, v1z-v0z
	e2x, e2y, e2z := v2x-v0x, v2y-v0y, v2z-v0z
	px, py, pz := dy*e2z-dz*e2y, dz*e2x-dx*e2z, dx*e2y-dy*e2x
	det := e1x*px + e1y*py + e1z*pz
	if det > -1e-9 && det < 1e-9 {
		return -1
	}
	invDet := 1.0 / det
	tx, ty, tz := ox-v0x, oy-v0y, oz-v0z
	u := (tx*px + ty*py + tz*pz) * invDet
	if u < 0 || u > 1 {
		return -1
	}
	qx, qy, qz := ty*e1z-tz*e1y, tz*e1x-tx*e1z, tx*e1y-ty*e1x
	v := (dx*qx + dy*qy + dz*qz) * invDet
	if v < 0 || u+v > 1 {
		return -1
	}
	t := (e2x*qx + e2y*qy + e2z*qz) * invDet
	return t
}

func (viewer *MeshPreviewWidget) Scrolled(event *fyne.ScrollEvent) {
	if viewer == nil || event == nil || !viewer.data.HasRenderableGeometry() {
		return
	}
	viewer.requestFocus()
	if event.Scrolled.DY > 0 {
		viewer.moveAlongView(meshPreviewScrollMoveDistance)
		return
	}
	if event.Scrolled.DY < 0 {
		viewer.moveAlongView(-meshPreviewScrollMoveDistance)
	}
}

func (viewer *MeshPreviewWidget) Clear() {
	viewer.renderToken.Add(1)
	viewer.stopKeyboardMovement()
	viewer.data = MeshPreviewData{}
	viewer.selectedBatch = -1
	viewer.image.Image = nil
	viewer.image.Refresh()
	viewer.stopProcess()
}

func (viewer *MeshPreviewWidget) SetData(data MeshPreviewData) {
	viewer.applyData(data, true)
}

func (viewer *MeshPreviewWidget) SetDataPreserveView(data MeshPreviewData) {
	viewer.applyData(data, false)
}

func (viewer *MeshPreviewWidget) applyData(data MeshPreviewData, resetView bool) {
	viewer.stopProcess()
	viewer.data = data
	viewer.selectedBatch = -1
	if resetView {
		viewer.stopKeyboardMovement()
		viewer.yaw = -0.35
		viewer.pitch = 0.3
		viewer.zoom = 1.0
		viewer.cameraX, viewer.cameraY, viewer.cameraZ = meshPreviewInitialCameraPosition(viewer.yaw, viewer.pitch, viewer.zoom)
	}
	go func() {
		viewer.startProcessAndLoad()
		fyne.Do(func() {
			viewer.render()
		})
	}()
}

func (viewer *MeshPreviewWidget) UpdateSceneColors(data MeshPreviewData) {
	if viewer == nil {
		return
	}
	if !viewer.canReuseProcessForData(data) {
		viewer.SetDataPreserveView(data)
		return
	}
	batchColors, colorsErr := MeshPreviewBatchBaseColors(data.RenderableBatches())
	if colorsErr != nil {
		viewer.SetDataPreserveView(data)
		return
	}
	viewer.data = data
	proc := viewer.process
	if proc == nil || !proc.isRunning() {
		viewer.SetDataPreserveView(data)
		return
	}
	go func(activeProcess *meshRendererProcess, colors []color.NRGBA) {
		if recolorErr := activeProcess.recolorScene(colors); recolorErr != nil {
			debug.Logf("mesh renderer recolor failed: %s", recolorErr.Error())
			fyne.Do(func() {
				viewer.SetDataPreserveView(data)
			})
			return
		}
		fyne.Do(func() {
			if viewer == nil || viewer.process != activeProcess {
				return
			}
			viewer.render()
		})
	}(proc, batchColors)
}

func (viewer *MeshPreviewWidget) SetBackground(fill color.Color) {
	if viewer == nil || viewer.background == nil {
		return
	}
	viewer.background.FillColor = fill
	viewer.background.Refresh()
	viewer.render()
}

func (viewer *MeshPreviewWidget) SetZoom(nextZoom float64) {
	if viewer == nil {
		return
	}
	viewer.zoom = format.Clamp(nextZoom, 0.35, 5.0)
	viewer.render()
}

func (viewer *MeshPreviewWidget) SetOpacity(nextOpacity float64) {
	if viewer == nil {
		return
	}
	viewer.opacity = format.Clamp(nextOpacity, 0.1, 1.0)
	viewer.render()
}

func (viewer *MeshPreviewWidget) SetSelectedBatch(batchIndex int) {
	if viewer == nil {
		return
	}
	if batchIndex < 0 || batchIndex >= len(viewer.data.RenderableBatches()) {
		batchIndex = -1
	}
	if viewer.selectedBatch == batchIndex {
		return
	}
	viewer.selectedBatch = batchIndex
	viewer.render()
}

func (viewer *MeshPreviewWidget) SelectedBatch() int {
	if viewer == nil {
		return -1
	}
	return viewer.selectedBatch
}

func (viewer *MeshPreviewWidget) SetFocusCanvas(canvas fyne.Canvas) {
	if viewer == nil {
		return
	}
	viewer.focusCanvas = canvas
}

func (viewer *MeshPreviewWidget) FocusGained() {}

func (viewer *MeshPreviewWidget) FocusLost() {
	viewer.stopKeyboardMovement()
}

func (viewer *MeshPreviewWidget) TypedRune(_ rune) {}

func (viewer *MeshPreviewWidget) TypedKey(_ *fyne.KeyEvent) {}

func (viewer *MeshPreviewWidget) KeyDown(event *fyne.KeyEvent) {
	if viewer == nil || event == nil {
		return
	}
	viewer.updateKeyboardState(event.Name, true)
}

func (viewer *MeshPreviewWidget) KeyUp(event *fyne.KeyEvent) {
	if viewer == nil || event == nil {
		return
	}
	viewer.updateKeyboardState(event.Name, false)
}

func (viewer *MeshPreviewWidget) MouseIn(_ *desktop.MouseEvent) {
	viewer.requestFocus()
	viewer.hasLastMouse = false
}

func (viewer *MeshPreviewWidget) MouseMoved(event *desktop.MouseEvent) {
	if viewer == nil || event == nil {
		return
	}
	if viewer.rightMouseDown && viewer.hasLastMouse {
		viewer.keyStateMutex.Lock()
		viewer.pendingLookDX += event.Position.X - viewer.lastMousePos.X
		viewer.pendingLookDY += event.Position.Y - viewer.lastMousePos.Y
		viewer.ensureKeyboardMovementLocked()
		viewer.keyStateMutex.Unlock()
	}
	viewer.lastMousePos = event.Position
	viewer.hasLastMouse = true
}

func (viewer *MeshPreviewWidget) MouseOut() {
	viewer.hasLastMouse = false
}

func (viewer *MeshPreviewWidget) MouseDown(event *desktop.MouseEvent) {
	if viewer == nil || event == nil {
		return
	}
	viewer.requestFocus()
	if event.Button == desktop.MouseButtonSecondary {
		viewer.keyStateMutex.Lock()
		viewer.rightMouseDown = true
		viewer.lastMousePos = event.Position
		viewer.hasLastMouse = true
		viewer.ensureKeyboardMovementLocked()
		viewer.keyStateMutex.Unlock()
	}
}

func (viewer *MeshPreviewWidget) MouseUp(event *desktop.MouseEvent) {
	if viewer == nil || event == nil {
		return
	}
	if event.Button == desktop.MouseButtonSecondary {
		viewer.keyStateMutex.Lock()
		viewer.rightMouseDown = false
		viewer.hasLastMouse = false
		if !viewer.keyState.shouldMove() {
			viewer.stopKeyboardMovementLocked()
		}
		viewer.keyStateMutex.Unlock()
	}
}

func (viewer *MeshPreviewWidget) requestFocus() {
	if viewer == nil {
		return
	}
	canvas := viewer.focusCanvas
	if canvas == nil && GetPrimaryWindow != nil {
		if window := GetPrimaryWindow(); window != nil {
			canvas = window.Canvas()
		}
	}
	if canvas != nil {
		canvas.Focus(viewer)
	}
}

func (viewer *MeshPreviewWidget) updateKeyboardState(keyName fyne.KeyName, pressed bool) {
	if viewer == nil {
		return
	}
	viewer.keyStateMutex.Lock()
	changed := viewer.keyState.set(keyName, pressed)
	shouldMove := viewer.keyState.shouldUpdate(viewer.rightMouseDown)
	if !changed {
		viewer.keyStateMutex.Unlock()
		return
	}
	if shouldMove {
		viewer.ensureKeyboardMovementLocked()
	} else {
		viewer.stopKeyboardMovementLocked()
	}
	viewer.keyStateMutex.Unlock()
}

func (viewer *MeshPreviewWidget) ensureKeyboardMovementLocked() {
	if viewer.movementStop != nil {
		return
	}
	stopCh := make(chan struct{})
	viewer.movementStop = stopCh
	go viewer.runKeyboardMovement(stopCh)
}

func (viewer *MeshPreviewWidget) stopKeyboardMovement() {
	if viewer == nil {
		return
	}
	viewer.keyStateMutex.Lock()
	viewer.stopKeyboardMovementLocked()
	viewer.keyStateMutex.Unlock()
}

func (viewer *MeshPreviewWidget) stopKeyboardMovementLocked() {
	viewer.keyState = meshPreviewKeyState{}
	if viewer.movementStop == nil {
		return
	}
	close(viewer.movementStop)
	viewer.movementStop = nil
}

func (viewer *MeshPreviewWidget) runKeyboardMovement(stopCh chan struct{}) {
	ticker := time.NewTicker(meshPreviewKeyboardTickInterval)
	defer ticker.Stop()

	lastTick := time.Now()
	for {
		select {
		case <-stopCh:
			return
		case tickTime := <-ticker.C:
			deltaSeconds := tickTime.Sub(lastTick).Seconds()
			lastTick = tickTime

			viewer.keyStateMutex.Lock()
			lookDX := viewer.pendingLookDX
			lookDY := viewer.pendingLookDY
			viewer.pendingLookDX = 0
			viewer.pendingLookDY = 0
			moveX, moveY, moveZ := viewer.keyState.moveDelta(deltaSeconds, viewer.yaw, viewer.pitch)
			shouldContinue := viewer.keyState.shouldUpdate(viewer.rightMouseDown)
			viewer.keyStateMutex.Unlock()
			if lookDX == 0 && lookDY == 0 && moveX == 0 && moveY == 0 && moveZ == 0 {
				if !shouldContinue {
					viewer.keyStateMutex.Lock()
					viewer.stopKeyboardMovementLocked()
					viewer.keyStateMutex.Unlock()
					return
				}
				continue
			}

			fyne.Do(func() {
				if viewer == nil || !viewer.data.HasRenderableGeometry() {
					return
				}
				if lookDX != 0 || lookDY != 0 {
					viewer.applyMouseLookDelta(lookDX, lookDY)
				}
				viewer.cameraX += moveX
				viewer.cameraY += moveY
				viewer.cameraZ += moveZ
				viewer.render()
			})
		}
	}
}

func (state *meshPreviewKeyState) set(keyName fyne.KeyName, pressed bool) bool {
	if state == nil {
		return false
	}

	var target *bool
	switch keyName {
	case fyne.KeyW:
		target = &state.forward
	case fyne.KeyS:
		target = &state.backward
	case fyne.KeyA:
		target = &state.left
	case fyne.KeyD:
		target = &state.right
	case desktop.KeyShiftLeft, desktop.KeyShiftRight:
		target = &state.fast
	default:
		return false
	}

	if *target == pressed {
		return false
	}
	*target = pressed
	return true
}

func (state meshPreviewKeyState) shouldMove() bool {
	return state.forward || state.backward || state.left || state.right
}

func (state meshPreviewKeyState) shouldUpdate(rightMouseDown bool) bool {
	return rightMouseDown || state.shouldMove()
}

func (state meshPreviewKeyState) moveDelta(deltaSeconds float64, yaw float64, pitch float64) (float64, float64, float64) {
	if deltaSeconds <= 0 {
		return 0, 0, 0
	}

	speed := meshPreviewKeyboardMovePerSecond
	if state.fast {
		speed *= meshPreviewKeyboardFastMultiplier
	}
	step := speed * deltaSeconds

	forward, right, _ := meshPreviewCameraBasis(yaw, pitch)
	var moveX float64
	var moveY float64
	var moveZ float64
	if state.left {
		moveX -= right[0] * step
		moveY -= right[1] * step
		moveZ -= right[2] * step
	}
	if state.right {
		moveX += right[0] * step
		moveY += right[1] * step
		moveZ += right[2] * step
	}
	if state.forward {
		moveX += forward[0] * step
		moveY += forward[1] * step
		moveZ += forward[2] * step
	}
	if state.backward {
		moveX -= forward[0] * step
		moveY -= forward[1] * step
		moveZ -= forward[2] * step
	}
	return moveX, moveY, moveZ
}

func (viewer *MeshPreviewWidget) moveAlongView(distance float64) {
	if viewer == nil || distance == 0 {
		return
	}
	forward, _, _ := meshPreviewCameraBasis(viewer.yaw, viewer.pitch)
	viewer.cameraX += forward[0] * distance
	viewer.cameraY += forward[1] * distance
	viewer.cameraZ += forward[2] * distance
	viewer.render()
}

func (viewer *MeshPreviewWidget) render() {
	if viewer == nil {
		return
	}
	size := viewer.Size()
	if size.Width < MinMeshPreviewRenderDimension || size.Height < MinMeshPreviewRenderDimension {
		return
	}
	width := int(math.Max(1, float64(size.Width)))
	height := int(math.Max(1, float64(size.Height)))
	if !viewer.data.HasRenderableGeometry() {
		viewer.renderToken.Add(1)
		viewer.image.Image = nil
		viewer.image.Refresh()
		return
	}

	proc := viewer.process
	if proc == nil || !proc.isRunning() {
		return
	}

	cameraXSnapshot, cameraYSnapshot, cameraZSnapshot := viewer.cameraPosition()
	selectedBatchSnapshot := viewer.selectedBatch
	yawSnapshot := viewer.yaw
	pitchSnapshot := viewer.pitch
	opacitySnapshot := viewer.opacity
	bg := color.NRGBAModel.Convert(viewer.background.FillColor).(color.NRGBA)
	bgHex := fmt.Sprintf("%02x%02x%02x", bg.R, bg.G, bg.B)
	renderID := viewer.renderToken.Add(1)

	go func() {
		rendered, renderErr := proc.render(width, height, cameraXSnapshot, cameraYSnapshot, cameraZSnapshot, selectedBatchSnapshot, yawSnapshot, pitchSnapshot, 1.0, opacitySnapshot, bgHex)
		if renderErr != nil {
			debug.Logf("mesh renderer subprocess render failed: %s", renderErr.Error())
			return
		}
		fyne.Do(func() {
			if viewer == nil || viewer.renderToken.Load() != renderID {
				return
			}
			viewer.image.Image = rendered
			viewer.image.Refresh()
		})
	}()
}

// Render schedules a mesh redraw at the current widget size when geometry and the renderer process are ready.
func (viewer *MeshPreviewWidget) Render() {
	viewer.render()
}

func (viewer *MeshPreviewWidget) startProcessAndLoad() {
	if !viewer.data.HasRenderableGeometry() {
		debug.Logf("mesh renderer: no raw positions/indices, skipping")
		return
	}
	proc, startErr := startMeshRendererProcess()
	if startErr != nil {
		debug.Logf("mesh renderer process start failed: %s", startErr.Error())
		return
	}
	sceneBatches := viewer.data.RenderableBatches()
	var loadErr error
	switch {
	case len(viewer.data.Batches) > 0:
		loadErr = proc.loadScene(sceneBatches)
	case len(sceneBatches) == 1 && len(sceneBatches[0].RawColors) > 0:
		loadErr = proc.loadColored(normalizeMeshPreviewPositionsCopy(sceneBatches[0].RawPositions), sceneBatches[0].RawIndices, sceneBatches[0].RawColors)
	default:
		loadErr = proc.load(normalizeMeshPreviewPositionsCopy(viewer.data.RawPositions), viewer.data.RawIndices)
	}
	if loadErr != nil {
		debug.Logf("mesh renderer load failed: %s", loadErr.Error())
		proc.stop()
		return
	}
	totalVertices := 0
	totalIndices := 0
	for _, batch := range sceneBatches {
		totalVertices += len(batch.RawPositions) / 3
		totalIndices += len(batch.RawIndices)
	}
	debug.Logf("mesh renderer GPU subprocess started (%d batches, %d vertices, %d indices)", len(sceneBatches), totalVertices, totalIndices)
	viewer.process = proc
}

func (viewer *MeshPreviewWidget) stopProcess() {
	if viewer.process != nil {
		viewer.process.stop()
		viewer.process = nil
	}
}

func (viewer *MeshPreviewWidget) cameraPosition() (float64, float64, float64) {
	if viewer == nil {
		return 0, 0, 0
	}
	return viewer.cameraX, viewer.cameraY, viewer.cameraZ
}

func (viewer *MeshPreviewWidget) canReuseProcessForData(nextData MeshPreviewData) bool {
	currentBatches := viewer.data.RenderableBatches()
	nextBatches := nextData.RenderableBatches()
	if len(currentBatches) == 0 || len(currentBatches) != len(nextBatches) {
		return false
	}
	for i := range currentBatches {
		if len(currentBatches[i].RawPositions) != len(nextBatches[i].RawPositions) {
			return false
		}
		if len(currentBatches[i].RawIndices) != len(nextBatches[i].RawIndices) {
			return false
		}
	}
	return true
}

// meshRendererProcess manages the lifecycle of the mesh-renderer subprocess.
type meshRendererProcess struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	mu     sync.Mutex
	alive  bool
}

func startMeshRendererProcess() (*meshRendererProcess, error) {
	commandName, commandArgs, found := resolveMeshRendererCommand()
	if !found {
		return nil, fmt.Errorf("mesh-renderer binary not found")
	}

	cmd := exec.Command(commandName, commandArgs...)
	stderrFile, stderrErr := os.OpenFile(filepath.Join(os.TempDir(), "joxblox-mesh-renderer-stderr.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if stderrErr == nil {
		cmd.Stderr = stderrFile
	} else {
		cmd.Stderr = os.Stderr
	}

	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		stdinPipe.Close()
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}

	if startErr := cmd.Start(); startErr != nil {
		stdinPipe.Close()
		return nil, fmt.Errorf("start: %w", startErr)
	}

	return &meshRendererProcess{
		cmd:    cmd,
		stdin:  stdinPipe,
		stdout: bufio.NewReaderSize(stdoutPipe, 16*1024*1024),
		alive:  true,
	}, nil
}

func (p *meshRendererProcess) isRunning() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.alive
}

func (p *meshRendererProcess) load(positions []float32, indices []uint32) error {
	return p.loadColored(positions, indices, nil)
}

func (p *meshRendererProcess) loadColored(positions []float32, indices []uint32, colors []uint8) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.alive {
		return fmt.Errorf("process not alive")
	}

	vertexCount := len(positions) / 3
	indexCount := len(indices)
	if len(colors) == 0 {
		colors = meshPreviewDefaultColors(vertexCount)
	}
	if len(colors) != vertexCount*4 {
		return fmt.Errorf("vertex colors length %d does not match vertex count %d", len(colors), vertexCount)
	}

	header := fmt.Sprintf("LOADC %d %d\n", vertexCount, indexCount)
	if _, err := io.WriteString(p.stdin, header); err != nil {
		return fmt.Errorf("write header: %w", err)
	}

	posBytes := make([]byte, len(positions)*4)
	for i, v := range positions {
		binary.LittleEndian.PutUint32(posBytes[i*4:], math.Float32bits(v))
	}
	if _, err := p.stdin.Write(posBytes); err != nil {
		return fmt.Errorf("write positions: %w", err)
	}

	idxBytes := make([]byte, len(indices)*4)
	for i, v := range indices {
		binary.LittleEndian.PutUint32(idxBytes[i*4:], v)
	}
	if _, err := p.stdin.Write(idxBytes); err != nil {
		return fmt.Errorf("write indices: %w", err)
	}
	if _, err := p.stdin.Write(colors); err != nil {
		return fmt.Errorf("write colors: %w", err)
	}

	line, readErr := p.stdout.ReadString('\n')
	if readErr != nil {
		p.alive = false
		return fmt.Errorf("read response: %w", readErr)
	}
	line = strings.TrimSpace(line)
	if line != "OK" {
		return fmt.Errorf("load failed: %s", line)
	}
	return nil
}

func (p *meshRendererProcess) loadScene(batches []MeshPreviewBatchData) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.alive {
		return fmt.Errorf("process not alive")
	}
	if len(batches) == 0 {
		return fmt.Errorf("scene is empty")
	}

	header := fmt.Sprintf("LOADSCENE %d\n", len(batches))
	if _, err := io.WriteString(p.stdin, header); err != nil {
		return fmt.Errorf("write scene header: %w", err)
	}

	for _, batch := range batches {
		vertexCount := len(batch.RawPositions) / 3
		indexCount := len(batch.RawIndices)
		colors := batch.RawColors
		if len(colors) == 0 {
			colors = meshPreviewDefaultColors(vertexCount)
		}
		if len(colors) != vertexCount*4 {
			return fmt.Errorf("vertex colors length %d does not match vertex count %d", len(colors), vertexCount)
		}
		if _, err := io.WriteString(p.stdin, fmt.Sprintf("BATCH %d %d\n", vertexCount, indexCount)); err != nil {
			return fmt.Errorf("write batch header: %w", err)
		}

		posBytes := make([]byte, len(batch.RawPositions)*4)
		for i, v := range batch.RawPositions {
			binary.LittleEndian.PutUint32(posBytes[i*4:], math.Float32bits(v))
		}
		if _, err := p.stdin.Write(posBytes); err != nil {
			return fmt.Errorf("write batch positions: %w", err)
		}

		idxBytes := make([]byte, len(batch.RawIndices)*4)
		for i, v := range batch.RawIndices {
			binary.LittleEndian.PutUint32(idxBytes[i*4:], v)
		}
		if _, err := p.stdin.Write(idxBytes); err != nil {
			return fmt.Errorf("write batch indices: %w", err)
		}

		if _, err := p.stdin.Write(colors); err != nil {
			return fmt.Errorf("write batch colors: %w", err)
		}
	}

	line, readErr := p.stdout.ReadString('\n')
	if readErr != nil {
		p.alive = false
		return fmt.Errorf("read response: %w", readErr)
	}
	line = strings.TrimSpace(line)
	if line != "OK" {
		return fmt.Errorf("scene load failed: %s", line)
	}
	return nil
}

func (p *meshRendererProcess) recolorScene(batchColors []color.NRGBA) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.alive {
		return fmt.Errorf("process not alive")
	}
	if len(batchColors) == 0 {
		return fmt.Errorf("scene colors are empty")
	}

	header := fmt.Sprintf("RECOLOR %d\n", len(batchColors))
	if _, err := io.WriteString(p.stdin, header); err != nil {
		p.alive = false
		return fmt.Errorf("write recolor header: %w", err)
	}

	rawColors := make([]byte, 0, len(batchColors)*4)
	for _, batchColor := range batchColors {
		rawColors = append(rawColors, batchColor.R, batchColor.G, batchColor.B, batchColor.A)
	}
	if _, err := p.stdin.Write(rawColors); err != nil {
		p.alive = false
		return fmt.Errorf("write recolor payload: %w", err)
	}

	line, readErr := p.stdout.ReadString('\n')
	if readErr != nil {
		p.alive = false
		return fmt.Errorf("read response: %w", readErr)
	}
	line = strings.TrimSpace(line)
	if line != "OK" {
		return fmt.Errorf("scene recolor failed: %s", line)
	}
	return nil
}

func (p *meshRendererProcess) render(width int, height int, cameraX float64, cameraY float64, cameraZ float64, selectedBatch int, yaw float64, pitch float64, zoom float64, opacity float64, bgHex string) (image.Image, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.alive {
		return nil, fmt.Errorf("process not alive")
	}

	cmd := fmt.Sprintf("RENDER %d %d %.6f %.6f %.6f %d %.6f %.6f %.6f %.6f %s\n", width, height, cameraX, cameraY, cameraZ, selectedBatch, yaw, pitch, zoom, format.Clamp(opacity, 0.1, 1.0), bgHex)
	if _, err := io.WriteString(p.stdin, cmd); err != nil {
		p.alive = false
		return nil, fmt.Errorf("write render: %w", err)
	}

	line, readErr := p.stdout.ReadString('\n')
	if readErr != nil {
		p.alive = false
		return nil, fmt.Errorf("read header: %w", readErr)
	}
	line = strings.TrimSpace(line)

	if strings.HasPrefix(line, "ERR ") {
		return nil, fmt.Errorf("render error: %s", line[4:])
	}
	if !strings.HasPrefix(line, "FRAME ") {
		return nil, fmt.Errorf("unexpected response: %s", line)
	}

	frameParts := strings.Fields(line)
	if len(frameParts) < 4 {
		return nil, fmt.Errorf("invalid FRAME header: %s", line)
	}
	frameWidth, _ := strconv.Atoi(frameParts[1])
	frameHeight, _ := strconv.Atoi(frameParts[2])
	byteCount, _ := strconv.Atoi(frameParts[3])

	if byteCount <= 0 || byteCount > 64*1024*1024 {
		return nil, fmt.Errorf("invalid byte count: %d", byteCount)
	}

	pixels := make([]byte, byteCount)
	if _, err := io.ReadFull(p.stdout, pixels); err != nil {
		p.alive = false
		return nil, fmt.Errorf("read pixels: %w", err)
	}

	img := image.NewNRGBA(image.Rect(0, 0, frameWidth, frameHeight))
	copy(img.Pix, pixels)
	return img, nil
}

func (p *meshRendererProcess) renderOrtho(width int, height int, centerX float64, centerZ float64, orthoHalfW float64, orthoHalfH float64, cameraY float64, bgHex string) (image.Image, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.alive {
		return nil, fmt.Errorf("process not alive")
	}

	cmd := fmt.Sprintf("RENDERORTHO %d %d %.6f %.6f %.6f %.6f %.6f %s\n", width, height, centerX, centerZ, orthoHalfW, orthoHalfH, cameraY, bgHex)
	if _, err := io.WriteString(p.stdin, cmd); err != nil {
		p.alive = false
		return nil, fmt.Errorf("write renderOrtho: %w", err)
	}

	line, readErr := p.stdout.ReadString('\n')
	if readErr != nil {
		p.alive = false
		return nil, fmt.Errorf("read header: %w", readErr)
	}
	line = strings.TrimSpace(line)

	if strings.HasPrefix(line, "ERR ") {
		return nil, fmt.Errorf("renderOrtho error: %s", line[4:])
	}
	if !strings.HasPrefix(line, "FRAME ") {
		return nil, fmt.Errorf("unexpected response: %s", line)
	}

	frameParts := strings.Fields(line)
	if len(frameParts) < 4 {
		return nil, fmt.Errorf("invalid FRAME header: %s", line)
	}
	frameWidth, _ := strconv.Atoi(frameParts[1])
	frameHeight, _ := strconv.Atoi(frameParts[2])
	byteCount, _ := strconv.Atoi(frameParts[3])

	if byteCount <= 0 || byteCount > 256*1024*1024 {
		return nil, fmt.Errorf("invalid byte count: %d", byteCount)
	}

	pixels := make([]byte, byteCount)
	if _, err := io.ReadFull(p.stdout, pixels); err != nil {
		p.alive = false
		return nil, fmt.Errorf("read pixels: %w", err)
	}

	img := image.NewNRGBA(image.Rect(0, 0, frameWidth, frameHeight))
	copy(img.Pix, pixels)
	return img, nil
}

// RenderTopDownMapImage starts a temporary mesh-renderer process, loads the given
// scene batches, renders an orthographic top-down view covering the given world
// bounds, and returns the resulting image.
func RenderTopDownMapImage(batches []MeshPreviewBatchData, centerX float64, centerZ float64, halfWidth float64, halfHeight float64, cameraY float64, renderWidth int, renderHeight int, bgHex string) (image.Image, error) {
	if len(batches) == 0 {
		return nil, fmt.Errorf("no scene batches to render")
	}
	proc, startErr := startMeshRendererProcess()
	if startErr != nil {
		return nil, fmt.Errorf("start mesh renderer: %w", startErr)
	}
	defer proc.stop()

	if loadErr := proc.loadScene(batches); loadErr != nil {
		return nil, fmt.Errorf("load scene: %w", loadErr)
	}

	img, renderErr := proc.renderOrtho(renderWidth, renderHeight, centerX, centerZ, halfWidth, halfHeight, cameraY, bgHex)
	if renderErr != nil {
		return nil, fmt.Errorf("render ortho: %w", renderErr)
	}
	return img, nil
}

func (p *meshRendererProcess) pick(width int, height int, cameraX float64, cameraY float64, cameraZ float64, yaw float64, pitch float64, zoom float64, clickX int, clickY int) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.alive {
		return -1, fmt.Errorf("process not alive")
	}

	cmd := fmt.Sprintf("PICK %d %d %.6f %.6f %.6f %.6f %.6f %.6f %d %d\n", width, height, cameraX, cameraY, cameraZ, yaw, pitch, zoom, clickX, clickY)
	if _, err := io.WriteString(p.stdin, cmd); err != nil {
		p.alive = false
		return -1, fmt.Errorf("write pick: %w", err)
	}

	line, readErr := p.stdout.ReadString('\n')
	if readErr != nil {
		p.alive = false
		return -1, fmt.Errorf("read pick response: %w", readErr)
	}
	line = strings.TrimSpace(line)
	if strings.HasPrefix(line, "ERR ") {
		return -1, fmt.Errorf("pick error: %s", line[4:])
	}
	if !strings.HasPrefix(line, "PICKED ") {
		return -1, fmt.Errorf("unexpected pick response: %s", line)
	}
	pickedIndex, parseErr := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "PICKED ")))
	if parseErr != nil {
		return -1, fmt.Errorf("invalid pick response: %s", line)
	}
	return pickedIndex, nil
}

func (p *meshRendererProcess) stop() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.alive {
		return
	}
	p.alive = false
	io.WriteString(p.stdin, "QUIT\n")
	p.stdin.Close()
	go p.cmd.Wait()
}

func meshRendererBinaryName() string {
	if runtime.GOOS == "windows" {
		return "joxblox-mesh-renderer.exe"
	}
	return "joxblox-mesh-renderer"
}

func resolveMeshRendererCommand() (string, []string, bool) {
	if binaryPath, found := findMeshRendererBinaryPath(); found {
		return binaryPath, nil, true
	}
	if GetRepositoryRootPath != nil {
		if repoRoot, err := GetRepositoryRootPath(); err == nil && strings.TrimSpace(repoRoot) != "" {
			rendererSourcePath := filepath.Join(repoRoot, "tools", "mesh-renderer", "main.go")
			if _, statErr := os.Stat(rendererSourcePath); statErr == nil {
				return "go", []string{"run", rendererSourcePath}, true
			}
		}
	}
	return "", nil, false
}

func findMeshRendererBinaryPath() (string, bool) {
	name := meshRendererBinaryName()
	candidates := make([]string, 0, 6)

	if execPath, err := os.Executable(); err == nil && strings.TrimSpace(execPath) != "" {
		dir := filepath.Dir(execPath)
		candidates = append(candidates, filepath.Join(dir, name))
	}
	if GetRepositoryRootPath != nil {
		if repoRoot, err := GetRepositoryRootPath(); err == nil && strings.TrimSpace(repoRoot) != "" {
			candidates = append(candidates, filepath.Join(repoRoot, name))
			candidates = append(candidates, filepath.Join(repoRoot, "tools", "mesh-renderer", name))
		}
	}
	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates, filepath.Join(wd, name))
	}

	for _, path := range candidates {
		if _, err := os.Stat(path); err == nil {
			return path, true
		}
	}
	return "", false
}

func BuildMeshPreviewData(positions []float32, indices []uint32, triangleCount uint32, previewTriangleCount uint32) (MeshPreviewData, error) {
	return BuildMeshPreviewDataWithColors(positions, indices, nil, triangleCount, previewTriangleCount)
}

func BuildMeshPreviewDataWithColors(positions []float32, indices []uint32, colors []uint8, triangleCount uint32, previewTriangleCount uint32) (MeshPreviewData, error) {
	if err := validateMeshPreviewBatch(positions, indices, colors); err != nil {
		return MeshPreviewData{}, err
	}
	return MeshPreviewData{
		RawPositions:         append([]float32(nil), positions...),
		RawIndices:           append([]uint32(nil), indices...),
		RawColors:            append([]uint8(nil), colors...),
		TriangleCount:        triangleCount,
		PreviewTriangleCount: previewTriangleCount,
	}, nil
}

func BuildMeshPreviewSceneData(batches []MeshPreviewBatchData, triangleCount uint32, previewTriangleCount uint32) (MeshPreviewData, error) {
	if len(batches) == 0 {
		return MeshPreviewData{}, fmt.Errorf("mesh preview is empty")
	}
	validatedBatches := make([]MeshPreviewBatchData, 0, len(batches))
	for _, batch := range batches {
		if err := validateMeshPreviewBatch(batch.RawPositions, batch.RawIndices, batch.RawColors); err != nil {
			return MeshPreviewData{}, err
		}
		validatedBatches = append(validatedBatches, MeshPreviewBatchData{
			RawPositions: append([]float32(nil), batch.RawPositions...),
			RawIndices:   append([]uint32(nil), batch.RawIndices...),
			RawColors:    append([]uint8(nil), batch.RawColors...),
		})
	}
	return MeshPreviewData{
		Batches:              validatedBatches,
		TriangleCount:        triangleCount,
		PreviewTriangleCount: previewTriangleCount,
	}, nil
}

func validateMeshPreviewBatch(positions []float32, indices []uint32, colors []uint8) error {
	if len(positions) < 9 || len(indices) < 3 {
		return fmt.Errorf("mesh preview is empty")
	}
	if len(positions)%3 != 0 {
		return fmt.Errorf("mesh preview positions are not XYZ triplets")
	}
	if len(indices)%3 != 0 {
		return fmt.Errorf("mesh preview indices are not triangle triplets")
	}

	vertexCount := len(positions) / 3
	if len(colors) > 0 && len(colors) != vertexCount*4 {
		return fmt.Errorf("mesh preview colors length %d does not match vertex count %d", len(colors), vertexCount)
	}
	for i := 0; i < len(positions); i += 3 {
		if math.IsNaN(float64(positions[i])) || math.IsNaN(float64(positions[i+1])) || math.IsNaN(float64(positions[i+2])) {
			return fmt.Errorf("mesh preview contains invalid coordinates")
		}
	}
	for _, idx := range indices {
		if int(idx) >= vertexCount {
			return fmt.Errorf("mesh preview index %d out of range", idx)
		}
	}
	return nil
}

func meshPreviewDefaultColors(vertexCount int) []uint8 {
	if vertexCount <= 0 {
		return nil
	}
	colors := make([]uint8, vertexCount*4)
	for i := 0; i < vertexCount; i++ {
		base := i * 4
		colors[base+0] = 112
		colors[base+1] = 173
		colors[base+2] = 255
		colors[base+3] = 255
	}
	return colors
}

func MeshPreviewBatchBaseColors(batches []MeshPreviewBatchData) ([]color.NRGBA, error) {
	if len(batches) == 0 {
		return nil, fmt.Errorf("scene is empty")
	}
	result := make([]color.NRGBA, 0, len(batches))
	for _, batch := range batches {
		vertexCount := len(batch.RawPositions) / 3
		if vertexCount <= 0 {
			return nil, fmt.Errorf("batch has no vertices")
		}
		colors := batch.RawColors
		if len(colors) == 0 {
			colors = meshPreviewDefaultColors(vertexCount)
		}
		if len(colors) < 4 {
			return nil, fmt.Errorf("batch color payload is incomplete")
		}
		result = append(result, color.NRGBA{
			R: colors[0],
			G: colors[1],
			B: colors[2],
			A: colors[3],
		})
	}
	return result, nil
}

func CloneMeshPreviewData(data MeshPreviewData) MeshPreviewData {
	cloned := MeshPreviewData{
		RawPositions:         append([]float32(nil), data.RawPositions...),
		RawIndices:           append([]uint32(nil), data.RawIndices...),
		RawColors:            append([]uint8(nil), data.RawColors...),
		TriangleCount:        data.TriangleCount,
		PreviewTriangleCount: data.PreviewTriangleCount,
	}
	if len(data.Batches) > 0 {
		cloned.Batches = make([]MeshPreviewBatchData, 0, len(data.Batches))
		for _, batch := range data.Batches {
			cloned.Batches = append(cloned.Batches, MeshPreviewBatchData{
				RawPositions: append([]float32(nil), batch.RawPositions...),
				RawIndices:   append([]uint32(nil), batch.RawIndices...),
				RawColors:    append([]uint8(nil), batch.RawColors...),
			})
		}
	}
	return cloned
}

func CloneMeshPreviewDataWithSharedGeometry(data MeshPreviewData) MeshPreviewData {
	cloned := MeshPreviewData{
		RawPositions:         data.RawPositions,
		RawIndices:           data.RawIndices,
		RawColors:            append([]uint8(nil), data.RawColors...),
		TriangleCount:        data.TriangleCount,
		PreviewTriangleCount: data.PreviewTriangleCount,
	}
	if len(data.Batches) > 0 {
		cloned.Batches = make([]MeshPreviewBatchData, len(data.Batches))
		copy(cloned.Batches, data.Batches)
		for i := range cloned.Batches {
			cloned.Batches[i].RawColors = append([]uint8(nil), data.Batches[i].RawColors...)
		}
	}
	return cloned
}

func normalizeMeshPreviewPositionsCopy(positions []float32) []float32 {
	if len(positions) == 0 {
		return nil
	}
	normalized := append([]float32(nil), positions...)
	centerAndNormalizePositions(normalized)
	return normalized
}

func meshPreviewInitialCameraPosition(yaw float64, pitch float64, zoom float64) (float64, float64, float64) {
	distance := 3.0 / format.Clamp(zoom, 0.35, 5.0)
	sinYaw := math.Sin(yaw)
	cosYaw := math.Cos(yaw)
	sinPitch := math.Sin(pitch)
	cosPitch := math.Cos(pitch)
	return distance * cosPitch * sinYaw, distance * sinPitch, distance * cosYaw * cosPitch
}

func meshPreviewFOVYRadians(zoom float64) float64 {
	clampedZoom := format.Clamp(zoom, 0.35, 5.0)
	fovDegrees := format.Clamp(45.0/clampedZoom, 15.0, 90.0)
	return fovDegrees * math.Pi / 180.0
}

func meshPreviewCameraBasis(yaw float64, pitch float64) ([3]float64, [3]float64, [3]float64) {
	sinYaw := math.Sin(yaw)
	cosYaw := math.Cos(yaw)
	sinPitch := math.Sin(pitch)
	cosPitch := math.Cos(pitch)
	forward := [3]float64{-cosPitch * sinYaw, -sinPitch, -cosPitch * cosYaw}
	forward = normalizeMeshPreviewVector(forward)

	worldUp := [3]float64{0, 1, 0}
	right := [3]float64{
		forward[1]*worldUp[2] - forward[2]*worldUp[1],
		forward[2]*worldUp[0] - forward[0]*worldUp[2],
		forward[0]*worldUp[1] - forward[1]*worldUp[0],
	}
	if vectorLength := meshPreviewVectorLength(right); vectorLength <= 1e-9 {
		right = [3]float64{1, 0, 0}
	} else {
		right = normalizeMeshPreviewVector(right)
	}

	up := [3]float64{
		forward[1]*right[2] - forward[2]*right[1],
		forward[2]*right[0] - forward[0]*right[2],
		forward[0]*right[1] - forward[1]*right[0],
	}
	up = normalizeMeshPreviewVector(up)
	return forward, right, up
}

func normalizeMeshPreviewVector(vector [3]float64) [3]float64 {
	length := meshPreviewVectorLength(vector)
	if length <= 1e-9 {
		return [3]float64{}
	}
	return [3]float64{vector[0] / length, vector[1] / length, vector[2] / length}
}

func meshPreviewVectorLength(vector [3]float64) float64 {
	return math.Sqrt(vector[0]*vector[0] + vector[1]*vector[1] + vector[2]*vector[2])
}

func (data MeshPreviewData) HasRenderableGeometry() bool {
	for _, batch := range data.RenderableBatches() {
		if len(batch.RawPositions) >= 9 && len(batch.RawIndices) >= 3 {
			return true
		}
	}
	return false
}

func (data MeshPreviewData) SceneBatches() []MeshPreviewBatchData {
	if len(data.Batches) > 0 {
		return data.Batches
	}
	if len(data.RawPositions) == 0 || len(data.RawIndices) == 0 {
		return nil
	}
	return []MeshPreviewBatchData{{
		RawPositions: data.RawPositions,
		RawIndices:   data.RawIndices,
		RawColors:    data.RawColors,
	}}
}

func (data MeshPreviewData) RenderableBatches() []MeshPreviewBatchData {
	if len(data.Batches) > 0 {
		return data.Batches
	}
	if len(data.RawPositions) == 0 || len(data.RawIndices) == 0 {
		return nil
	}
	return []MeshPreviewBatchData{{
		RawPositions: normalizeMeshPreviewPositionsCopy(data.RawPositions),
		RawIndices:   append([]uint32(nil), data.RawIndices...),
		RawColors:    append([]uint8(nil), data.RawColors...),
	}}
}

func centerAndNormalizePositions(positions []float32) {
	if len(positions) < 3 {
		return
	}
	vertexCount := len(positions) / 3
	minX, minY, minZ := positions[0], positions[1], positions[2]
	maxX, maxY, maxZ := minX, minY, minZ
	for i := 1; i < vertexCount; i++ {
		x := positions[i*3]
		y := positions[i*3+1]
		z := positions[i*3+2]
		if x < minX {
			minX = x
		}
		if x > maxX {
			maxX = x
		}
		if y < minY {
			minY = y
		}
		if y > maxY {
			maxY = y
		}
		if z < minZ {
			minZ = z
		}
		if z > maxZ {
			maxZ = z
		}
	}
	centerX := (minX + maxX) * 0.5
	centerY := (minY + maxY) * 0.5
	centerZ := (minZ + maxZ) * 0.5
	radius := float32(0)
	for i := 0; i < vertexCount; i++ {
		positions[i*3] -= centerX
		positions[i*3+1] -= centerY
		positions[i*3+2] -= centerZ
		x := positions[i*3]
		y := positions[i*3+1]
		z := positions[i*3+2]
		distance := float32(math.Sqrt(float64(x*x + y*y + z*z)))
		if distance > radius {
			radius = distance
		}
	}
	if radius <= 0 {
		radius = 1
	}
	for i := range positions {
		positions[i] /= radius
	}
}

func ExtractMeshPreviewFromBytes(fileBytes []byte) (MeshPreviewData, error) {
	return ExtractMeshPreviewFromBytesWithLimit(fileBytes, MaxMeshPreviewTriangles)
}

func ExtractMeshPreviewFromBytesWithLimit(fileBytes []byte, maxTriangles int) (MeshPreviewData, error) {
	raw, err := extractor.ExtractMeshPreviewRawFromBytes(fileBytes, maxTriangles)
	if err != nil {
		return MeshPreviewData{}, err
	}
	return BuildMeshPreviewData(raw.Positions, raw.Indices, raw.TriangleCount, raw.PreviewTriangleCount)
}

func ExtractMeshPreviewFromFile(filePath string) (MeshPreviewData, error) {
	return ExtractMeshPreviewFromFileWithLimit(filePath, MaxMeshPreviewTriangles)
}

func ExtractMeshPreviewFromFileWithLimit(filePath string, maxTriangles int) (MeshPreviewData, error) {
	raw, err := extractor.ExtractMeshPreviewRawFromFile(filePath, maxTriangles)
	if err != nil {
		return MeshPreviewData{}, err
	}
	return BuildMeshPreviewData(raw.Positions, raw.Indices, raw.TriangleCount, raw.PreviewTriangleCount)
}
