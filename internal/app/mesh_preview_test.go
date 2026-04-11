package app

import (
	"math"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
)

func TestBuildMeshPreviewDataValidatesInput(t *testing.T) {
	_, err := buildMeshPreviewData([]float32{0, 0, 0}, []uint32{0, 1, 2}, 1, 1)
	if err == nil {
		t.Fatal("expected error for too few positions")
	}

	_, err = buildMeshPreviewData(
		[]float32{-1, -1, 0, 1, -1, 0, 0, 1, 0},
		[]uint32{0, 1, 5},
		1, 1,
	)
	if err == nil {
		t.Fatal("expected error for out-of-range index")
	}

	_, err = buildMeshPreviewData(
		[]float32{float32(math.NaN()), 0, 0, 1, -1, 0, 0, 1, 0},
		[]uint32{0, 1, 2},
		1, 1,
	)
	if err == nil {
		t.Fatal("expected error for NaN coordinate")
	}
}

func TestBuildMeshPreviewDataPreservesRawData(t *testing.T) {
	rawPos := []float32{-1, -1, 0, 1, -1, 0, 0, 1, 0}
	rawIdx := []uint32{0, 1, 2}
	data, err := buildMeshPreviewData(rawPos, rawIdx, 1, 1)
	if err != nil {
		t.Fatalf("buildMeshPreviewData returned error: %v", err)
	}
	if len(data.RawPositions) != len(rawPos) {
		t.Fatalf("expected %d raw positions, got %d", len(rawPos), len(data.RawPositions))
	}
	if len(data.RawIndices) != len(rawIdx) {
		t.Fatalf("expected %d raw indices, got %d", len(rawIdx), len(data.RawIndices))
	}
	for i, v := range rawPos {
		if data.RawPositions[i] != v {
			t.Fatalf("raw position mismatch at %d: expected %f, got %f", i, v, data.RawPositions[i])
		}
	}
}

func TestBuildMeshPreviewDataStoresTriangleCounts(t *testing.T) {
	data, err := buildMeshPreviewData(
		[]float32{-1, -1, 0, 1, -1, 0, 0, 1, 0},
		[]uint32{0, 1, 2},
		100, 1,
	)
	if err != nil {
		t.Fatalf("buildMeshPreviewData returned error: %v", err)
	}
	if data.TriangleCount != 100 {
		t.Fatalf("expected TriangleCount 100, got %d", data.TriangleCount)
	}
	if data.PreviewTriangleCount != 1 {
		t.Fatalf("expected PreviewTriangleCount 1, got %d", data.PreviewTriangleCount)
	}
}

func TestBuildMeshPreviewDataWithColorsValidatesColorCount(t *testing.T) {
	_, err := buildMeshPreviewDataWithColors(
		[]float32{-1, -1, 0, 1, -1, 0, 0, 1, 0},
		[]uint32{0, 1, 2},
		[]uint8{255, 0, 0, 255},
		1,
		1,
	)
	if err == nil {
		t.Fatal("expected error for invalid color count")
	}
}

func TestBuildMeshPreviewSceneDataPreservesBatches(t *testing.T) {
	scene, err := buildMeshPreviewSceneData([]meshPreviewBatchData{
		{
			RawPositions: []float32{-1, -1, 0, 1, -1, 0, 0, 1, 0},
			RawIndices:   []uint32{0, 1, 2},
			RawColors: []uint8{
				255, 0, 0, 255,
				0, 255, 0, 255,
				0, 0, 255, 255,
			},
		},
	}, 10, 1)
	if err != nil {
		t.Fatalf("buildMeshPreviewSceneData returned error: %v", err)
	}
	if len(scene.Batches) != 1 {
		t.Fatalf("expected 1 batch, got %d", len(scene.Batches))
	}
	if len(scene.Batches[0].RawColors) != 12 {
		t.Fatalf("expected 12 color bytes, got %d", len(scene.Batches[0].RawColors))
	}
}

func TestMeshPreviewKeyStateSetTracksMovementKeys(t *testing.T) {
	var state meshPreviewKeyState
	if !state.set(fyne.KeyW, true) {
		t.Fatal("expected W key press to update state")
	}
	if !state.forward {
		t.Fatal("expected W key press to set forward state")
	}
	if state.set(fyne.KeyW, true) {
		t.Fatal("expected duplicate W key press to be ignored")
	}
	if !state.set(desktop.KeyShiftLeft, true) {
		t.Fatal("expected Shift key press to update state")
	}
	if !state.fast {
		t.Fatal("expected Shift key press to enable fast movement")
	}
	if state.set(fyne.KeyEscape, true) {
		t.Fatal("expected unrelated keys to be ignored")
	}
}

func TestMeshPreviewKeyStateMoveDeltaHonorsShiftSpeed(t *testing.T) {
	slow := meshPreviewKeyState{forward: true}
	slowX, slowY, slowZ := slow.moveDelta(0.5, 0, 0)
	expectedSlow := meshPreviewKeyboardMovePerSecond * 0.5
	if math.Abs(slowX) > 1e-9 || math.Abs(slowY) > 1e-9 {
		t.Fatalf("expected forward movement only on Z axis, got (%f,%f,%f)", slowX, slowY, slowZ)
	}
	if math.Abs(slowZ+expectedSlow) > 1e-9 {
		t.Fatalf("expected Z delta %f, got %f", -expectedSlow, slowZ)
	}

	fast := meshPreviewKeyState{forward: true, fast: true}
	fastX, fastY, fastZ := fast.moveDelta(0.5, 0, 0)
	expectedFast := expectedSlow * meshPreviewKeyboardFastMultiplier
	if math.Abs(fastX) > 1e-9 || math.Abs(fastY) > 1e-9 {
		t.Fatalf("expected fast forward movement only on Z axis, got (%f,%f,%f)", fastX, fastY, fastZ)
	}
	if math.Abs(fastZ+expectedFast) > 1e-9 {
		t.Fatalf("expected fast Z delta %f, got %f", -expectedFast, fastZ)
	}
}

func TestMeshPreviewKeyStateMoveDeltaStrafesRelativeToYaw(t *testing.T) {
	state := meshPreviewKeyState{right: true}
	moveX, moveY, moveZ := state.moveDelta(1.0, math.Pi/2, 0)
	if math.Abs(moveX) > 1e-9 || math.Abs(moveY) > 1e-9 {
		t.Fatalf("expected strafe movement on Z axis when facing -X, got (%f,%f,%f)", moveX, moveY, moveZ)
	}
	if math.Abs(moveZ+meshPreviewKeyboardMovePerSecond) > 1e-9 {
		t.Fatalf("expected negative Z strafe delta %f, got %f", -meshPreviewKeyboardMovePerSecond, moveZ)
	}
}

func TestMeshPreviewKeyStateShouldUpdateWhileLooking(t *testing.T) {
	var idle meshPreviewKeyState
	if idle.shouldUpdate(false) {
		t.Fatal("expected idle state without right click to stop updates")
	}
	if !idle.shouldUpdate(true) {
		t.Fatal("expected right click look to keep updates running")
	}

	moving := meshPreviewKeyState{forward: true}
	if !moving.shouldUpdate(false) {
		t.Fatal("expected movement keys to keep updates running")
	}
}

func TestMeshPreviewInitialCameraPositionMatchesDefaultOrbitStart(t *testing.T) {
	cameraX, cameraY, cameraZ := meshPreviewInitialCameraPosition(-0.35, 0.3, 1.0)
	distance := math.Sqrt(cameraX*cameraX + cameraY*cameraY + cameraZ*cameraZ)
	if math.Abs(distance-3.0) > 0.01 {
		t.Fatalf("expected default camera distance near 3, got %f", distance)
	}
	if cameraX >= 0 || cameraY <= 0 || cameraZ <= 0 {
		t.Fatalf("unexpected default camera quadrant (%f,%f,%f)", cameraX, cameraY, cameraZ)
	}
}

func TestMeshPreviewMoveAlongViewUsesForwardVector(t *testing.T) {
	viewer := newMeshPreviewWidget()
	viewer.cameraX = 10
	viewer.cameraY = 5
	viewer.cameraZ = -2
	viewer.yaw = 0
	viewer.pitch = 0

	viewer.moveAlongView(2)

	if math.Abs(viewer.cameraX-10) > 1e-9 || math.Abs(viewer.cameraY-5) > 1e-9 || math.Abs(viewer.cameraZ+4) > 1e-9 {
		t.Fatalf("expected camera to move forward to (10,5,-4), got (%f,%f,%f)", viewer.cameraX, viewer.cameraY, viewer.cameraZ)
	}
}

func TestMeshPreviewSetSelectedBatchClampsInvalidSelection(t *testing.T) {
	viewer := newMeshPreviewWidget()
	viewer.data = meshPreviewData{
		Batches: []meshPreviewBatchData{
			{RawPositions: []float32{-1, -1, 0, 1, -1, 0, 0, 1, 0}, RawIndices: []uint32{0, 1, 2}},
			{RawPositions: []float32{-1, -1, 1, 1, -1, 1, 0, 1, 1}, RawIndices: []uint32{0, 1, 2}},
		},
	}

	viewer.SetSelectedBatch(1)
	if viewer.selectedBatch != 1 {
		t.Fatalf("expected selected batch 1, got %d", viewer.selectedBatch)
	}

	viewer.SetSelectedBatch(99)
	if viewer.selectedBatch != -1 {
		t.Fatalf("expected invalid selection to clear batch, got %d", viewer.selectedBatch)
	}
}

func TestMeshPreviewRenderableBatchesNormalizesSingleMesh(t *testing.T) {
	data := meshPreviewData{
		RawPositions: []float32{10, 0, 0, 12, 0, 0, 10, 2, 0},
		RawIndices:   []uint32{0, 1, 2},
	}
	batches := data.renderableBatches()
	if len(batches) != 1 {
		t.Fatalf("expected 1 renderable batch, got %d", len(batches))
	}
	if len(batches[0].RawPositions) != len(data.RawPositions) {
		t.Fatalf("expected normalized positions to preserve vertex count")
	}
	if batches[0].RawPositions[0] == data.RawPositions[0] {
		t.Fatal("expected single-mesh renderable batch positions to be normalized")
	}
	if data.RawPositions[0] != 10 {
		t.Fatal("expected renderable batch normalization to avoid mutating original data")
	}
}

func TestMeshPreviewSetOpacityClampsValues(t *testing.T) {
	viewer := newMeshPreviewWidget()

	viewer.SetOpacity(0.01)
	if math.Abs(viewer.opacity-0.1) > 1e-9 {
		t.Fatalf("expected minimum opacity clamp of 0.1, got %f", viewer.opacity)
	}

	viewer.SetOpacity(2.0)
	if math.Abs(viewer.opacity-1.0) > 1e-9 {
		t.Fatalf("expected maximum opacity clamp of 1.0, got %f", viewer.opacity)
	}
}

func TestCloneMeshPreviewDataDeepCopiesSlices(t *testing.T) {
	original := meshPreviewData{
		RawPositions:         []float32{1, 2, 3},
		RawIndices:           []uint32{0, 1, 2},
		RawColors:            []uint8{1, 2, 3, 4},
		TriangleCount:        3,
		PreviewTriangleCount: 1,
		Batches: []meshPreviewBatchData{{
			RawPositions: []float32{4, 5, 6},
			RawIndices:   []uint32{0, 1, 2},
			RawColors:    []uint8{5, 6, 7, 8},
		}},
	}

	cloned := cloneMeshPreviewData(original)
	cloned.RawPositions[0] = 99
	cloned.RawIndices[0] = 99
	cloned.RawColors[0] = 99
	cloned.Batches[0].RawPositions[0] = 99
	cloned.Batches[0].RawIndices[0] = 99
	cloned.Batches[0].RawColors[0] = 99

	if original.RawPositions[0] == 99 || original.RawIndices[0] == 99 || original.RawColors[0] == 99 {
		t.Fatal("expected top-level mesh preview slices to be deep copied")
	}
	if original.Batches[0].RawPositions[0] == 99 || original.Batches[0].RawIndices[0] == 99 || original.Batches[0].RawColors[0] == 99 {
		t.Fatal("expected batch slices to be deep copied")
	}
}

func TestMeshPreviewBatchBaseColorsUsesFirstVertexColor(t *testing.T) {
	colors, err := meshPreviewBatchBaseColors([]meshPreviewBatchData{
		{
			RawPositions: []float32{-1, -1, 0, 1, -1, 0, 0, 1, 0},
			RawIndices:   []uint32{0, 1, 2},
			RawColors: []uint8{
				10, 20, 30, 255,
				40, 50, 60, 255,
				70, 80, 90, 255,
			},
		},
	})
	if err != nil {
		t.Fatalf("meshPreviewBatchBaseColors returned error: %v", err)
	}
	if len(colors) != 1 {
		t.Fatalf("expected 1 batch color, got %d", len(colors))
	}
	if colors[0].R != 10 || colors[0].G != 20 || colors[0].B != 30 || colors[0].A != 255 {
		t.Fatalf("unexpected batch base color: %+v", colors[0])
	}
}
