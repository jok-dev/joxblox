# RenderDoc Recording Mode Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a Record/Stop toggle to the RenderDoc launcher that auto-fires F12 every Ns, processes each `.rdc` as it lands, deduplicates textures by dHash into an in-memory aggregate, deletes the source `.rdc`, and on Stop swaps the existing Textures sub-tab to render the aggregated set.

**Architecture:** A new `internal/renderdoc/recorder.go` owns the recording state machine (timer goroutine, bounded worker pool, aggregate map). The launcher gains Record/Stop + Interval controls and routes new `.rdc` events to the recorder when active. The Textures sub-tab gains a `dataSource` field — single-capture (today) or recording-aggregate — and renders the aggregate via the same table/preview code path as live captures.

**Tech Stack:** Go 1.23+, Fyne v2 widgets, existing `internal/renderdoc` parsing + hashing, existing `procutil`, `golang.org/x/image/draw` for thumbnail downscale.

**Spec:** [docs/superpowers/specs/2026-04-27-recording-mode-design.md](../specs/2026-04-27-recording-mode-design.md)

---

## File Structure

**Create:**
- `internal/renderdoc/recorder.go` — `Recorder`, `AggregateTexture`, `Snapshot`, state machine, processing pipeline.
- `internal/renderdoc/recorder_test.go` — state-machine + dedup + worker-pool tests.

**Modify:**
- `internal/app/ui/mesh_preview.go` — add `ShowRecordingResults` package-level hook (mirrors `OpenSingleAsset`).
- `internal/app/ui/tabs/renderdoc/launcher_view.go` — Record/Stop button, Interval entry, status polling, fsnotify intercept that routes to the active recorder.
- `internal/app/ui/tabs/renderdoc/renderdoc_tab.go` — `dataSource` field + recording-aggregate render path on the Textures sub-tab; wire `ui.ShowRecordingResults`.

---

## Task 1: `Recorder` skeleton + state-machine tests

**Files:**
- Create: `internal/renderdoc/recorder.go`
- Create: `internal/renderdoc/recorder_test.go`

**Why:** Get the state machine and synchronisation primitives in place under test before wiring real `.rdc` processing. The processing pipeline plugs in next.

- [ ] **Step 1: Write the failing tests**

Create `internal/renderdoc/recorder_test.go`:

```go
package renderdoc

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestRecorderStartIsActiveStopReturnsEmptyAggregate(t *testing.T) {
	sessionDir := t.TempDir()
	r := NewRecorder()
	if r.IsActive() {
		t.Fatal("IsActive before Start: got true, want false")
	}
	if err := r.Start(sessionDir, time.Hour); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !r.IsActive() {
		t.Fatal("IsActive after Start: got false, want true")
	}
	got := r.Stop()
	if r.IsActive() {
		t.Fatal("IsActive after Stop: got true, want false")
	}
	if len(got) != 0 {
		t.Errorf("aggregate after Stop with no captures: got %d, want 0", len(got))
	}
}

func TestRecorderDoubleStartErrors(t *testing.T) {
	sessionDir := t.TempDir()
	r := NewRecorder()
	if err := r.Start(sessionDir, time.Hour); err != nil {
		t.Fatalf("first Start: %v", err)
	}
	defer r.Stop()
	if err := r.Start(sessionDir, time.Hour); err == nil {
		t.Errorf("second Start: want error, got nil")
	}
}

func TestRecorderStopWhenInactiveIsNoop(t *testing.T) {
	r := NewRecorder()
	got := r.Stop()
	if got != nil {
		t.Errorf("Stop when inactive: got %v, want nil", got)
	}
}

func TestRecorderCreatesRecordingSubdirOnStart(t *testing.T) {
	sessionDir := t.TempDir()
	r := NewRecorder()
	if err := r.Start(sessionDir, time.Hour); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer r.Stop()
	entries, err := os.ReadDir(sessionDir)
	if err != nil {
		t.Fatalf("read sessionDir: %v", err)
	}
	var found bool
	for _, e := range entries {
		if e.IsDir() && len(e.Name()) > len("recording-") && e.Name()[:len("recording-")] == "recording-" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected a recording-<id>/ subdir, got entries: %v", entries)
	}
}

func TestRecorderErrorQueueCapsAtFive(t *testing.T) {
	sessionDir := t.TempDir()
	r := NewRecorder()
	_ = r.Start(sessionDir, time.Hour)
	defer r.Stop()
	for i := 0; i < 10; i++ {
		r.recordError(errors.New("boom"))
	}
	if got := r.Snapshot(); len(r.pendingErrs) != 5 {
		t.Errorf("error queue len: got %d, want 5 (last error: %v)", len(r.pendingErrs), got.LastError)
	}
}

func TestRecorderMergeAggregatesByDHash(t *testing.T) {
	sessionDir := t.TempDir()
	r := NewRecorder()
	if err := r.Start(sessionDir, time.Hour); err != nil {
		t.Fatalf("Start: %v", err)
	}
	r.merge(&AggregateTexture{DHash: 0xAAAA, Resource: "100"})
	r.merge(&AggregateTexture{DHash: 0xBBBB, Resource: "200"})
	r.merge(&AggregateTexture{DHash: 0xAAAA, Resource: "300"}) // dup, should be ignored
	got := r.Stop()
	if len(got) != 2 {
		t.Fatalf("aggregate len: got %d, want 2", len(got))
	}
	// First-seen wins for the duplicate.
	for _, tex := range got {
		if tex.DHash == 0xAAAA && tex.Resource != "100" {
			t.Errorf("dup should preserve first-seen Resource=100, got %q", tex.Resource)
		}
	}
}

// queueProbe lets a test wait until the recorder has accepted but not yet
// finished a synthetic ProcessCapture. Used by the worker-pool test.
func TestRecorderDropsCapturesWhenQueueSaturates(t *testing.T) {
	sessionDir := t.TempDir()
	r := NewRecorder()
	_ = r.Start(sessionDir, time.Hour)
	defer r.Stop()
	// Replace the real processor with a slow synthetic one so we can
	// fill the queue deterministically.
	var slowMu sync.Mutex
	slowMu.Lock() // workers block on this until we Unlock
	r.processFunc = func(rdcPath string) error {
		slowMu.Lock()
		slowMu.Unlock()
		return nil
	}
	// Submit many captures faster than they can be drained. Need to
	// produce real on-disk files because ProcessCapture uses the path.
	for i := 0; i < 30; i++ {
		path := filepath.Join(sessionDir, "fake.rdc")
		_ = os.WriteFile(path, []byte("synthetic"), 0o644)
		r.ProcessCapture(path)
	}
	snap := r.Snapshot()
	if snap.DroppedCount == 0 {
		t.Errorf("expected drops once queue saturated, got DroppedCount=0 (queueDepth=%d)", snap.QueueDepth)
	}
	slowMu.Unlock() // release the workers so Stop can drain
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/renderdoc/ -run TestRecorder -v`
Expected: FAIL — `Recorder`, `AggregateTexture`, `Snapshot`, `NewRecorder` undefined.

- [ ] **Step 3: Implement the recorder skeleton**

Create `internal/renderdoc/recorder.go`:

```go
package renderdoc

import (
	"errors"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

const (
	recorderMaxQueueDepth = 20
	recorderWorkerSlots   = 2
	recorderMaxErrorQueue = 5
)

// AggregateTexture is one deduplicated texture observed during a recording
// session. Built from a renderdoc TextureInfo plus a downsampled
// thumbnail. The dHash is the dedup key — first-seen wins.
type AggregateTexture struct {
	DHash     uint64
	PixelHash string
	Resource  string
	Format    string
	ShortFmt  string
	Width     int
	Height    int
	Bytes     int64
	Category  TextureCategory
	Thumbnail image.Image
	FirstSeen time.Time
}

// Snapshot is the live state surfaced to the launcher's status label.
type Snapshot struct {
	Active         bool
	CaptureCount   int
	UniqueTextures int
	DroppedCount   int
	QueueDepth     int
	LastError      error
}

// Recorder owns the recording state machine. One instance per app
// process; only one recording can be active at a time.
type Recorder struct {
	mu             sync.Mutex
	active         bool
	sessionDir     string
	recordingDir   string
	interval       time.Duration
	aggregate      map[uint64]*AggregateTexture
	captureCount   int
	droppedCount   int
	pendingErrs    []error
	processingWG   sync.WaitGroup
	triggerStop    chan struct{}
	workerSlots    chan struct{}
	queueDepth     atomic.Int32
	timerOnce      sync.Once

	// processFunc is the per-capture pipeline; swappable for tests.
	processFunc func(rdcPath string) error
}

// NewRecorder returns an idle recorder. Call Start to begin a session.
func NewRecorder() *Recorder {
	return &Recorder{
		aggregate: map[uint64]*AggregateTexture{},
	}
}

// IsActive reports whether a recording is currently running.
func (r *Recorder) IsActive() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.active
}

// Start initializes the recording subdir and worker pool. Returns an
// error if a recording is already active or the subdir can't be created.
// The timer that fires TriggerCapture is started by the caller (the
// launcher) — keeps Recorder pure-Go and testable without sending
// real keystrokes.
func (r *Recorder) Start(sessionDir string, interval time.Duration) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.active {
		return errors.New("recorder is already active")
	}
	stamp := time.Now().Format("20060102-150405")
	dir := filepath.Join(sessionDir, "recording-"+stamp)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create recording dir: %w", err)
	}
	r.sessionDir = sessionDir
	r.recordingDir = dir
	r.interval = interval
	r.aggregate = map[uint64]*AggregateTexture{}
	r.captureCount = 0
	r.droppedCount = 0
	r.pendingErrs = nil
	r.queueDepth.Store(0)
	r.triggerStop = make(chan struct{})
	r.workerSlots = make(chan struct{}, recorderWorkerSlots)
	for i := 0; i < recorderWorkerSlots; i++ {
		r.workerSlots <- struct{}{}
	}
	r.active = true
	return nil
}

// Stop halts the recorder, drains in-flight processing, removes the
// (now empty) recording subdir, and returns the aggregate sorted by
// FirstSeen. No-op + nil return when not active.
func (r *Recorder) Stop() []AggregateTexture {
	r.mu.Lock()
	if !r.active {
		r.mu.Unlock()
		return nil
	}
	r.active = false
	if r.triggerStop != nil {
		close(r.triggerStop)
	}
	dir := r.recordingDir
	r.mu.Unlock()

	r.processingWG.Wait()

	r.mu.Lock()
	out := make([]AggregateTexture, 0, len(r.aggregate))
	for _, tex := range r.aggregate {
		out = append(out, *tex)
	}
	r.mu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].FirstSeen.Before(out[j].FirstSeen) })

	if dir != "" {
		_ = os.Remove(dir) // best-effort; only succeeds if empty
	}
	return out
}

// TriggerStop returns a channel the launcher's timer goroutine should
// select on; closes when Stop is called so the timer exits cleanly.
func (r *Recorder) TriggerStop() <-chan struct{} {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.triggerStop
}

// Snapshot returns live counters + the last recorded error (if any).
func (r *Recorder) Snapshot() Snapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	var lastErr error
	if len(r.pendingErrs) > 0 {
		lastErr = r.pendingErrs[len(r.pendingErrs)-1]
	}
	return Snapshot{
		Active:         r.active,
		CaptureCount:   r.captureCount,
		UniqueTextures: len(r.aggregate),
		DroppedCount:   r.droppedCount,
		QueueDepth:     int(r.queueDepth.Load()),
		LastError:      lastErr,
	}
}

// ProcessCapture is the entry point invoked by the launcher's fsnotify
// hook for each new .rdc that arrives while recording is active.
// Bounded by workerSlots; surplus captures past maxQueueDepth are
// dropped with droppedCount++. Implementation stub — Task 2 wires the
// real convert/parse/hash pipeline into processFunc.
func (r *Recorder) ProcessCapture(rdcPath string) {
	r.mu.Lock()
	if !r.active {
		r.mu.Unlock()
		return
	}
	r.captureCount++
	r.mu.Unlock()

	if r.queueDepth.Load() >= recorderMaxQueueDepth {
		r.mu.Lock()
		r.droppedCount++
		r.mu.Unlock()
		return
	}
	r.queueDepth.Add(1)
	r.processingWG.Add(1)
	go func() {
		defer r.processingWG.Done()
		defer r.queueDepth.Add(-1)
		<-r.workerSlots
		defer func() { r.workerSlots <- struct{}{} }()
		processor := r.processFunc
		if processor == nil {
			return
		}
		if err := processor(rdcPath); err != nil {
			r.recordError(err)
		}
	}()
}

// merge inserts a texture into the aggregate; first-seen wins on dHash
// collisions. Safe to call from worker goroutines.
func (r *Recorder) merge(tex *AggregateTexture) {
	if tex == nil || tex.DHash == 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.aggregate[tex.DHash]; exists {
		return
	}
	if tex.FirstSeen.IsZero() {
		tex.FirstSeen = time.Now()
	}
	r.aggregate[tex.DHash] = tex
}

// recordError pushes onto pendingErrs, capped at recorderMaxErrorQueue.
func (r *Recorder) recordError(err error) {
	if err == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pendingErrs = append(r.pendingErrs, err)
	if len(r.pendingErrs) > recorderMaxErrorQueue {
		r.pendingErrs = r.pendingErrs[len(r.pendingErrs)-recorderMaxErrorQueue:]
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/renderdoc/ -run TestRecorder -v`
Expected: PASS for all 7 tests.

- [ ] **Step 5: Run with -race**

Run: `go test ./internal/renderdoc/ -run TestRecorder -race -v`
Expected: PASS, no race warnings.

- [ ] **Step 6: Commit**

```bash
git add internal/renderdoc/recorder.go internal/renderdoc/recorder_test.go
git commit -m "feat(recorder): state machine + bounded worker queue + aggregate dedup"
```

---

## Task 2: Wire the real processing pipeline

**Files:**
- Modify: `internal/renderdoc/recorder.go`

**Why:** Replace the test-only `processFunc` indirection with the real convert + parse + hash + downsample pipeline. The recorder becomes useful end-to-end after this task.

- [ ] **Step 1: Add the default processFunc setter and wire-up helpers**

In `internal/renderdoc/recorder.go`, append to the bottom of the file:

```go
// defaultProcessFunc is the production capture pipeline: wait for the
// file to settle, move it into the recording subdir, convert + parse +
// hash, decode + downsample new textures, then delete. Non-test code
// gets this via Start automatically.
func (r *Recorder) defaultProcessFunc(rdcPath string) error {
	if !waitFileStable(rdcPath, 250*time.Millisecond, 2, 30*time.Second) {
		return fmt.Errorf("file never stabilized: %s", rdcPath)
	}
	r.mu.Lock()
	dest := filepath.Join(r.recordingDir, filepath.Base(rdcPath))
	r.mu.Unlock()
	if err := os.Rename(rdcPath, dest); err != nil {
		return fmt.Errorf("move into recording dir: %w", err)
	}
	defer os.Remove(dest)

	xmlPath, convertErr := ConvertToXML(dest)
	if convertErr != nil {
		return fmt.Errorf("convert: %w", convertErr)
	}
	defer RemoveConvertedOutput(xmlPath)

	report, parseErr := ParseCaptureXMLFile(xmlPath)
	if parseErr != nil {
		return fmt.Errorf("parse: %w", parseErr)
	}
	store, storeErr := OpenBufferStore(xmlPath)
	if storeErr != nil {
		return fmt.Errorf("buffer store: %w", storeErr)
	}
	defer store.Close()
	ComputeTextureHashes(report, store, nil)

	for i := range report.Textures {
		tex := report.Textures[i]
		if tex.DHash == 0 || !isHashableCategory(tex.Category) {
			continue
		}
		// Skip if we've already aggregated this dHash — saves the
		// decode+downsample cost. merge() does the same check, but
		// avoiding the decode is the bigger win.
		r.mu.Lock()
		_, seen := r.aggregate[tex.DHash]
		r.mu.Unlock()
		if seen {
			continue
		}
		decoded, decErr := DecodeTexturePreview(tex, store)
		if decErr != nil || decoded == nil {
			continue
		}
		thumbnail := downsampleRecorderThumbnail(decoded)
		r.merge(&AggregateTexture{
			DHash:     tex.DHash,
			PixelHash: tex.PixelHash,
			Resource:  tex.ResourceID,
			Format:    tex.Format,
			ShortFmt:  tex.ShortFormat,
			Width:     tex.Width,
			Height:    tex.Height,
			Bytes:     tex.Bytes,
			Category:  tex.Category,
			Thumbnail: thumbnail,
			FirstSeen: time.Now(),
		})
	}
	return nil
}

// waitFileStable polls os.Stat until the file size has been the same
// for requiredStablePolls consecutive checks. Returns false on timeout.
// Mirrors the helper of the same purpose in the renderdoc tab launcher
// — duplicated here to avoid a UI-package import from a renderdoc
// internal.
func waitFileStable(path string, pollInterval time.Duration, requiredStablePolls int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	var prevSize int64 = -1
	stable := 0
	for time.Now().Before(deadline) {
		info, err := os.Stat(path)
		if err == nil {
			currentSize := info.Size()
			if currentSize > 0 && currentSize == prevSize {
				stable++
				if stable >= requiredStablePolls {
					return true
				}
			} else {
				stable = 0
				prevSize = currentSize
			}
		}
		time.Sleep(pollInterval)
	}
	return false
}
```

- [ ] **Step 2: Add the thumbnail downscaler (matches Materials sub-tab pattern)**

Append to `internal/renderdoc/recorder.go`:

```go
// recorderThumbnailMaxDim caps the cached preview size at 256 px on the
// longest edge. Matches the materials sub-tab's downsample policy so a
// single recording aggregate of 1000 unique textures fits in ~256 MB.
const recorderThumbnailMaxDim = 256

// downsampleRecorderThumbnail produces a small RGBA copy of img capped
// at recorderThumbnailMaxDim on its longest edge. Aspect ratio is
// preserved. Source images smaller than the cap are returned unchanged.
func downsampleRecorderThumbnail(src image.Image) image.Image {
	srcBounds := src.Bounds()
	w, h := srcBounds.Dx(), srcBounds.Dy()
	if w <= recorderThumbnailMaxDim && h <= recorderThumbnailMaxDim {
		return src
	}
	scale := float64(recorderThumbnailMaxDim) / float64(w)
	if h > w {
		scale = float64(recorderThumbnailMaxDim) / float64(h)
	}
	dstW := int(float64(w) * scale)
	dstH := int(float64(h) * scale)
	if dstW < 1 {
		dstW = 1
	}
	if dstH < 1 {
		dstH = 1
	}
	dst := image.NewNRGBA(image.Rect(0, 0, dstW, dstH))
	xdraw.BiLinear.Scale(dst, dst.Bounds(), src, srcBounds, xdraw.Src, nil)
	return dst
}
```

Add the import at the top of `recorder.go`:

```go
import (
	// ... existing imports ...
	xdraw "golang.org/x/image/draw"
)
```

- [ ] **Step 3: Use defaultProcessFunc when none was supplied**

In `Start`, after `r.active = true`, add:

```go
if r.processFunc == nil {
	r.processFunc = r.defaultProcessFunc
}
```

This way tests that set their own `processFunc` keep working (they set it after Start in the test, but Start is the first state-mutator the test calls — easier to leave the test override after Start). Adjust the test that uses a slow processor accordingly: it already sets `r.processFunc` after Start, which overrides the default.

- [ ] **Step 4: Build + run tests**

Run: `go build ./... && go test ./internal/renderdoc/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/renderdoc/recorder.go
git commit -m "feat(recorder): wire the real convert+parse+hash+downsample pipeline"
```

---

## Task 3: `ui.ShowRecordingResults` hook

**Files:**
- Modify: `internal/app/ui/mesh_preview.go`

**Why:** Same pattern as the existing `OpenSingleAsset` hook (set by app.go in production, called by the launcher). The Textures sub-tab will register a closure that swaps its `dataSource` and re-renders.

- [ ] **Step 1: Add the hook**

In `internal/app/ui/mesh_preview.go`, near the existing `var OpenSingleAsset func(assetID int64)` declaration, add:

```go
// ShowRecordingResults swaps the Textures sub-tab into recording-
// aggregate mode and renders the given textures. Set by the Textures
// sub-tab at construction; the launcher calls it on Stop.
var ShowRecordingResults func(textures []renderdoc.AggregateTexture)
```

Add the `renderdoc` import to the file if not present:

```go
import (
	// ...
	"joxblox/internal/renderdoc"
)
```

- [ ] **Step 2: Build**

Run: `go build ./...`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/app/ui/mesh_preview.go
git commit -m "feat(ui): ShowRecordingResults hook for launcher → Textures sub-tab"
```

---

## Task 4: Launcher Record/Stop button + interval entry + intercept

**Files:**
- Modify: `internal/app/ui/tabs/renderdoc/launcher_view.go`

**Why:** The user-visible entry point for the feature. Drives the recorder + routes incoming `.rdc` events while active.

- [ ] **Step 1: Add a recorder field to launcher**

In `launcher_view.go`, find the `launcher` struct and add:

```go
recorder        *renderdoc.Recorder
recordButton    *widget.Button
intervalEntry   *widget.Entry
recordingTicker *time.Ticker
recordingStop   chan struct{}
```

(Place near the existing `studioRunning` field.)

Initialize the recorder in `newLauncher` right after the launcher struct is created:

```go
l.recorder = renderdoc.NewRecorder()
```

- [ ] **Step 2: Build the Record button + interval entry**

In `newLauncher`, after the existing `captureButton := ...` block, add:

```go
intervalEntry := widget.NewEntry()
intervalEntry.SetText("1.0")
intervalEntry.SetPlaceHolder("Interval (s)")
intervalEntry.Resize(fyne.NewSize(60, intervalEntry.MinSize().Height))
l.intervalEntry = intervalEntry

var recordButton *widget.Button
recordButton = widget.NewButton("Record", func() {
	if l.recorder.IsActive() {
		l.stopRecording()
	} else {
		l.startRecording(window)
	}
})
recordButton.Disable()
l.recordButton = recordButton
```

Update the `topRow` layout to include them:

```go
topRow := container.NewBorder(nil, nil, studioLabel,
	container.NewHBox(openButton, launchButton, captureButton, recordButton, widget.NewLabel("Interval (s):"), container.NewGridWrap(fyne.NewSize(60, 36), intervalEntry)),
	statusLabel,
)
```

- [ ] **Step 3: Add the start/stop helpers**

Append to `launcher_view.go` (near `ensureSessionDir`, since they share state):

```go
// startRecording enables the recorder, kicks off the timer goroutine
// that fires F12 every interval, and updates UI. Caller must guarantee
// Studio is running (recordButton's enable state enforces this).
func (l *launcher) startRecording(window fyne.Window) {
	dir, err := l.ensureSessionDir()
	if err != nil {
		fyneDialog.ShowError(err, window)
		return
	}
	intervalSec, parseErr := strconv.ParseFloat(strings.TrimSpace(l.intervalEntry.Text), 64)
	if parseErr != nil || intervalSec < 0.25 || intervalSec > 10.0 {
		fyneDialog.ShowError(fmt.Errorf("invalid interval (use 0.25-10.0 seconds): %v", parseErr), window)
		return
	}
	interval := time.Duration(intervalSec * float64(time.Second))
	if err := l.recorder.Start(dir, interval); err != nil {
		fyneDialog.ShowError(err, window)
		return
	}
	l.recordButton.SetText("Stop Recording")

	// Start a ticker goroutine that fires TriggerCapture on each tick.
	stop := l.recorder.TriggerStop()
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				_ = renderdoc.TriggerCapture()
			}
		}
	}()

	// Start a status-polling goroutine that updates the launcher's
	// status label every 500ms while active.
	go l.pollRecordingStatus()
}

// stopRecording finalizes the recording, hands the aggregate to the
// Textures sub-tab via the ui.ShowRecordingResults hook, and resets UI.
func (l *launcher) stopRecording() {
	textures := l.recorder.Stop()
	l.recordButton.SetText("Record")
	if ui.ShowRecordingResults != nil {
		ui.ShowRecordingResults(textures)
	}
}

// pollRecordingStatus runs while a recording is active; updates the
// launcher's status label every 500ms.
func (l *launcher) pollRecordingStatus() {
	for {
		snap := l.recorder.Snapshot()
		if !snap.Active {
			return
		}
		fyne.Do(func() {
			text := fmt.Sprintf("Recording: %d captures, %d unique textures",
				snap.CaptureCount, snap.UniqueTextures)
			if snap.QueueDepth > 0 {
				text += fmt.Sprintf(" (queue: %d)", snap.QueueDepth)
			}
			if snap.DroppedCount > 0 {
				text += fmt.Sprintf(" (%d dropped — backlog)", snap.DroppedCount)
			}
			// We don't have a direct reference to statusLabel here; pass
			// it via a launcher field set during construction.
			if l.statusLabel != nil {
				l.statusLabel.SetText(text)
			}
		})
		time.Sleep(500 * time.Millisecond)
	}
}
```

You'll need to surface `statusLabel` on the launcher struct. In the struct, add:

```go
statusLabel *widget.Label
```

In `newLauncher`, after `statusLabel := widget.NewLabel("Ready")`, add:

```go
l.statusLabel = statusLabel
```

Add imports at the top of the file if missing:

```go
import (
	// ...
	"strconv"
	"joxblox/internal/app/ui"
)
```

- [ ] **Step 4: Enable Record alongside Capture when Studio launches**

Find the existing block in the launch button handler that enables `captureButton`:

```go
if captureButton != nil {
	captureButton.Enable()
}
```

Add right after:

```go
if recordButton != nil {
	recordButton.Enable()
}
```

Same for the disable-on-exit path:

```go
if captureButton != nil {
	captureButton.Disable()
}
```

Becomes:

```go
if captureButton != nil {
	captureButton.Disable()
}
if recordButton != nil {
	recordButton.Disable()
}
// Stop a recording if Studio went away mid-session.
if l.recorder != nil && l.recorder.IsActive() {
	l.stopRecording()
}
```

- [ ] **Step 5: Intercept fsnotify events while recording**

Find the `refreshCaptures` method (the auto-load path). At the top of the auto-load branch — currently:

```go
if autoLoad && !firstScan && loadCapture != nil {
	if newest, ok := newestFreshAddition(items, previousPaths); ok {
		// ... existing wait-for-stability + loadCapture
	}
}
```

Replace the whole `if autoLoad ...` block with:

```go
if l.recorder != nil && l.recorder.IsActive() {
	// While recording, every freshly-arrived .rdc in the session dir
	// (NOT files already inside recording-<id>/) routes to the
	// recorder. The auto-load path is bypassed entirely.
	for _, item := range items {
		if _, existed := previousPaths[item.Path]; existed {
			continue
		}
		// Skip files that are already inside a recording subdir.
		if filepath.Dir(item.Path) != l.sessionDir {
			continue
		}
		go l.recorder.ProcessCapture(item.Path)
	}
} else if autoLoad && !firstScan && loadCapture != nil {
	if newest, ok := newestFreshAddition(items, previousPaths); ok {
		pathToLoad := newest.Path
		go func() {
			if waitForFileStable(pathToLoad, 250*time.Millisecond, 2, 30*time.Second) {
				fyne.Do(func() {
					loadCapture(pathToLoad)
				})
			}
		}()
	}
}
```

- [ ] **Step 6: Build + smoke test compile only**

Run: `go build ./...`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/app/ui/tabs/renderdoc/launcher_view.go
git commit -m "feat(renderdoc-tab): Record/Stop button + status polling + fsnotify intercept"
```

---

## Task 5: Textures sub-tab recording-aggregate render path

**Files:**
- Modify: `internal/app/ui/tabs/renderdoc/renderdoc_tab.go`

**Why:** Wire the `ui.ShowRecordingResults` hook so the Textures sub-tab renders the aggregate. Same table + preview pane, different data source.

- [ ] **Step 1: Add `dataSource` and aggregate fields to state**

In `renderdocTabState`, add:

```go
dataSource          textureDataSource
recordingAggregate  []renderdoc.AggregateTexture
```

Above the struct, define:

```go
type textureDataSource int

const (
	dataSourceSingleCapture textureDataSource = iota
	dataSourceRecordingAggregate
)
```

Initialize in the state literal (default-zero is `dataSourceSingleCapture`, so no change needed).

- [ ] **Step 2: Map AggregateTexture → TextureInfo for display reuse**

Add at file scope, near the other helpers:

```go
// aggregateTexturesToInfos maps recording aggregates onto the same
// TextureInfo shape the Textures sub-tab renders today. Uploads is
// left empty — the recording aggregate uses cached thumbnails for
// preview, not the original buffer store.
func aggregateTexturesToInfos(aggregates []renderdoc.AggregateTexture) []renderdoc.TextureInfo {
	out := make([]renderdoc.TextureInfo, 0, len(aggregates))
	for _, a := range aggregates {
		out = append(out, renderdoc.TextureInfo{
			ResourceID:  a.Resource,
			Width:       a.Width,
			Height:      a.Height,
			Format:      a.Format,
			ShortFormat: a.ShortFmt,
			Bytes:       a.Bytes,
			Category:    a.Category,
			PixelHash:   a.PixelHash,
			DHash:       a.DHash,
		})
	}
	return out
}

// aggregateThumbnailFor returns the cached recording thumbnail for a
// given resource ID, if present in the current aggregate.
func aggregateThumbnailFor(state *renderdocTabState, resourceID string) image.Image {
	for i := range state.recordingAggregate {
		if state.recordingAggregate[i].Resource == resourceID {
			return state.recordingAggregate[i].Thumbnail
		}
	}
	return nil
}
```

- [ ] **Step 3: Wire the hook in `newTexturesSubTab`**

Inside `newTexturesSubTab`, after `state` is initialised, register:

```go
ui.ShowRecordingResults = func(textures []renderdoc.AggregateTexture) {
	fyne.Do(func() {
		state.dataSource = dataSourceRecordingAggregate
		state.recordingAggregate = textures
		state.allTextures = aggregateTexturesToInfos(textures)
		state.report = nil
		state.bufferStore = nil
		state.selectedRow = -1
		applySortAndFilter(state)
		recomputeTextureMatches(state)
		pathLabel.SetText(fmt.Sprintf("Recording aggregate · %d unique textures", len(textures)))
		summaryLabel.SetText("")
		categoryLabel.SetText("")
		countLabel.SetText(fmt.Sprintf("Showing %d of %d textures", len(state.displayTextures), len(state.allTextures)))
		previewInfoLabel.SetText("Select a texture to preview.")
		state.sourceImage = nil
		previewCanvas.Image = nil
		previewCanvas.Refresh()
		table.Refresh()
	})
}
```

- [ ] **Step 4: Update `triggerPreview` to use the cached thumbnail in recording mode**

Find the `triggerPreview` function in `renderdoc_tab.go`. At the top, after the current "no capture loaded" check, add a fast path for recording-aggregate mode:

```go
if state.dataSource == dataSourceRecordingAggregate {
	thumb := aggregateThumbnailFor(state, texture.ResourceID)
	if thumb == nil {
		infoLabel.SetText("Recording aggregate · thumbnail unavailable")
		state.sourceImage = nil
		previewCanvas.Image = nil
		previewCanvas.Refresh()
		return
	}
	state.sourceImage = thumb
	previewCanvas.Image = thumb
	previewCanvas.Refresh()
	infoLabel.SetText(fmt.Sprintf("Recording aggregate · %s · %s · %d×%d (thumbnail only)",
		texture.ResourceID, texture.ShortFormat, texture.Width, texture.Height))
	if state.openInSingleAssetButton != nil {
		if _, ok := state.matchByTexID[texture.ResourceID]; ok {
			state.openInSingleAssetButton.Show()
		} else {
			state.openInSingleAssetButton.Hide()
		}
	}
	return
}
```

**Important placement:** put this branch at the very top of `triggerPreview`, BEFORE the existing `if state.bufferStore == nil` early-return — recording mode intentionally has a nil buffer store, and this branch must intercept first.

- [ ] **Step 5: Recording mode resets when a normal capture loads**

In `onLoadFinished`, immediately after the `loadErr != nil` check passes (i.e. we're about to apply a new normal capture), add:

```go
state.dataSource = dataSourceSingleCapture
state.recordingAggregate = nil
```

(Place before `state.report = report`.)

- [ ] **Step 6: Build + run all tests**

Run: `go build ./... && go test ./...`
Expected: PASS modulo the pre-existing `TestPrivateTriangleCounts` unrelated failure.

- [ ] **Step 7: Commit**

```bash
git add internal/app/ui/tabs/renderdoc/renderdoc_tab.go
git commit -m "feat(renderdoc-tab): render recording aggregate via shared Textures table"
```

---

## Task 6: CHANGELOG + final verification

- [ ] **Step 1: Run full test suite + vet**

Run: `go test ./... && go vet ./...`
Expected: PASS (modulo pre-existing private-fixture failure in `internal/app/loader`).

- [ ] **Step 2: Manual smoke test**

Run: `go run ./cmd/joxblox`
1. Open RenderDoc tab. Click `Launch with RenderDoc`. Wait for Studio to load + open a place.
2. Click `Record`. Status label should cycle "Recording: N captures, M unique textures".
3. Walk around the place for ~30 seconds.
4. Click `Stop Recording`. Textures sub-tab should immediately swap to show all unique textures from the session.
5. Click a row → preview pane shows the cached thumbnail.
6. Sort + filter columns work; Studio Asset column populates if a place file is scanned.
7. Click a row in the captures list → Textures sub-tab swaps back to single-capture mode.

- [ ] **Step 3: Update CHANGELOG**

Edit `CHANGELOG.md`. Under the existing `## Unreleased` / `### Added` block, append:

```markdown
- RenderDoc Recording mode — Record/Stop toggle that auto-fires F12 every Ns, processes captures in the background, deduplicates textures by perceptual hash, and shows the unique-texture aggregate in the Textures sub-tab. Source `.rdc`s are deleted after extraction so disk usage stays bounded.
```

- [ ] **Step 4: Commit**

```bash
git add CHANGELOG.md
git commit -m "docs(changelog): note RenderDoc Recording mode"
```
