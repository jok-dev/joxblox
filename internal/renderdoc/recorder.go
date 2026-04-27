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
	mu           sync.Mutex
	active       bool
	sessionDir   string
	recordingDir string
	interval     time.Duration
	aggregate    map[uint64]*AggregateTexture
	captureCount int
	droppedCount int
	pendingErrs  []error
	processingWG sync.WaitGroup
	triggerStop  chan struct{}
	workerSlots  chan struct{}
	queueDepth   atomic.Int32

	// processFunc is the per-capture pipeline; swappable for tests.
	// Nil at construction; defaultProcessFunc is wired in by Start
	// once Task 2 lands. Tests override after Start.
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
// launcher) — keeps Recorder pure-Go and testable without sending real
// keystrokes.
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
// dropped with droppedCount++.
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
