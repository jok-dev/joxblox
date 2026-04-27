# RenderDoc Recording Mode — Design

**Date:** 2026-04-27
**Status:** Approved, ready for plan

## Goal

Add a "Record" mode to the RenderDoc launcher that auto-fires the F12 capture trigger every Ns while running, processes each `.rdc` as it lands, accumulates only previously-unseen textures into an in-memory aggregate (deduplicated by dHash), and deletes the `.rdc` after extraction. Stop → the existing Textures sub-tab swaps to show all unique textures collected during the session.

**Primary use:** walk a place in Studio while joxblox auto-captures every second; at the end you have a deduplicated visual catalog of every texture the engine actually uploaded during that play session, without 100s of MB of redundant `.rdc` files on disk.

## Non-goals

- Recording aggregation in Materials or Meshes sub-tabs (textures only).
- Persisting the recording aggregate to disk between app launches.
- Multi-recording management (only one recording at a time for v1).
- Full-size preview / channel toggles in recording results — thumbnails only.
- Re-opening a closed recording aggregate after loading a normal `.rdc`.
- Auto-trigger via anything other than F12 (e.g. dummy mouse moves to keep the engine rendering).

## Architecture

A new `internal/renderdoc/recorder.go` owns the recording state machine: timer, capture counter, aggregate set, processing queue. The launcher gains a Record/Stop toggle that drives it. The Textures sub-tab gains a new "data source" mode (single-capture vs recording-aggregate) so the same UI renders both.

```
Launcher                                 Recorder                       Textures sub-tab
  │                                        │                              │
  ├─ click Record ──────────────────────►  │                              │
  │   (creates recording-<unix>/ folder)   │                              │
  │                                        │                              │
  │   ┌── timer fires every interval ──┐   │                              │
  │   │  TriggerCapture() (F12)        │   │                              │
  │   └────────────────────────────────┘   │                              │
  │                                        │                              │
  │   ┌── fsnotify .rdc detected ─┐        │                              │
  │   │  move to recording-<id>/  ├───────►│ process: convert + parse +   │
  │   │  enqueue for processing   │        │  decode + dHash; for each    │
  │   └───────────────────────────┘        │  unseen texture, downsample  │
  │                                        │  thumbnail and add to        │
  │                                        │  aggregate; delete .rdc      │
  │                                        │                              │
  ├─ click Stop ───────────────────────►   │                              │
  │   (stop timer, drain queue,            ├──── publish aggregate ──────►│ render aggregate
  │    show recording aggregate)           │                              │ in existing table
```

## Recorder package (`internal/renderdoc/recorder.go`)

```go
type Recorder struct {
    sessionDir string
    interval   time.Duration

    mu             sync.Mutex
    active         bool
    recordingDir   string                       // recording-<unix> subdir under sessionDir
    aggregate      map[uint64]*AggregateTexture // dHash → entry, first-seen wins
    captureCount   int
    droppedCount   int
    pendingErrs    []error                      // last 5 errors for status display
    processingWG   sync.WaitGroup
    triggerStop    chan struct{}                // close to stop the timer goroutine
    workerSlots    chan struct{}                // bounded semaphore: 2 concurrent processors
    queueDepth     atomic.Int32                 // current backlog count
}

type AggregateTexture struct {
    DHash     uint64
    PixelHash string
    Resource  string         // first-seen resource ID
    Format    string         // long DXGI format name
    ShortFmt  string
    Width     int
    Height    int
    Bytes     int64          // existing TextureInfo.Bytes (per-mip GPU footprint)
    Category  TextureCategory
    Thumbnail image.Image    // ~256px (longest edge), kept for the Textures preview pane
    FirstSeen time.Time
}

// Start initializes recordingDir, the timer goroutine, and the worker
// pool. Returns an error if a recording is already active or the
// directory can't be created.
func (r *Recorder) Start(sessionDir string, interval time.Duration) error

// Stop halts the timer, drains in-flight processing, removes the
// recording subdir, and returns the aggregate (sorted by FirstSeen).
// Safe to call when not active — returns nil + no-op.
func (r *Recorder) Stop() []AggregateTexture

// Snapshot returns live counters for status-label polling. Safe from any goroutine.
func (r *Recorder) Snapshot() Snapshot

type Snapshot struct {
    Active          bool
    CaptureCount    int
    UniqueTextures  int
    DroppedCount    int
    QueueDepth      int
    LastError       error
}

// ProcessCapture is the entry point invoked by the launcher's fsnotify
// hook for each new .rdc that lands while recording is active. Bounded
// by workerSlots — surplus arrivals queue up to maxQueueDepth and are
// then dropped with droppedCount++.
func (r *Recorder) ProcessCapture(rdcPath string)

// IsActive reports whether a recording is currently running.
func (r *Recorder) IsActive() bool
```

### State machine

- **Idle.** No timer, aggregate empty.
- **Start clicked** → create `recording-<unix>/` subdir, start a `time.Ticker` firing `TriggerCapture` every interval, transition to **Recording**.
- **Recording.** Each tick fires F12. Each new `.rdc` in the session dir is moved to `recording-<id>/`, processed in a worker goroutine, deleted on success.
- **Stop clicked** → stop the ticker via `close(triggerStop)`, drain in-flight processing (`processingWG.Wait()`), hand the aggregate to the Textures sub-tab via the `ui.ShowRecordingResults` hook. Transition back to **Idle**. The empty `recording-<id>/` is removed.

### Processing pipeline (per `.rdc`)

1. **Wait for file stability** (reuse `waitForFileStable` already in `launcher_view.go`; promote it to a small shared helper if needed).
2. **Move** the `.rdc` into `recording-<id>/` so concurrent loads from other sub-tabs ignore it.
3. **Convert** via `ConvertToXML` (cmd window already hidden in the recent fix).
4. **Parse** via `ParseCaptureXMLFile` and **build buffer store**.
5. **Hash + decode** via `ComputeTextureHashes` (which already populates `DHash` alongside `PixelHash`).
6. **Merge.** For each `TextureInfo` whose `Category` is one of the asset categories and `DHash != 0`, check the aggregate map. If absent: call `DecodeTexturePreview` + `downsampleForCache` to ~256px, build an `AggregateTexture`, write it to the aggregate via the merge channel.
7. **Cleanup.** Close the buffer store, `RemoveConvertedOutput(xmlPath)`, `os.Remove(rdcInRecordingDir)`.

Errors at any step: log via `debug.Logf`, push onto `pendingErrs` (cap at last 5), continue — one bad capture must not kill the recording.

### Concurrency

- Captures arrive at ~1 Hz; processing one capture takes ~500 ms – 2 s depending on size.
- **Worker pool:** bounded to 2 concurrent processors via the `workerSlots` semaphore (`chan struct{}` with capacity 2).
- **Queue cap:** `maxQueueDepth = 20`. If a new capture arrives and `queueDepth >= 20`, drop it and increment `droppedCount`. The status label surfaces this so the user knows.
- **Aggregate writes:** workers send `*AggregateTexture` messages over a single buffered channel; one merge goroutine reads and writes the map. Avoids per-write map locking and gives `Stop` a clean shutdown signal.

## Launcher UI

Top row gains a new button `Record` / `Stop Recording` to the right of `Capture (F12)`. Same enable rules as Capture (Studio must be running). When recording, the status label cycles every 500 ms via a polling goroutine:

```
Recording: 12 captures, 47 unique textures (queue: 1)
```

Or with drops:
```
Recording: 30 captures, 84 unique textures (3 dropped — backlog)
```

A small numeric `Interval (s)` entry next to the Record button lets the user tune cadence. Default 1.0 s, range 0.25–10.0.

After Stop:
```
Recording stopped: 50 captures, 84 unique textures (3 errors)
```

The launcher's existing fsnotify watcher gets a small intercept: when the recorder is active, `.rdc` files appearing in `sessionDir` (excluding files already inside `recording-<id>/`) are routed to `recorder.ProcessCapture` instead of the normal "auto-load latest" path. When inactive, behaviour is unchanged.

## Textures sub-tab integration

The Textures sub-tab's `renderdocTabState` gains a `dataSource` field that's either `singleCapture` (today) or `recordingAggregate`. New header line in recording mode:

```
Recording aggregate · 84 unique textures from 50 captures · [×] Close recording view
```

When `dataSource == recordingAggregate`:
- Table renders the same columns; row data comes from the aggregate slice mapped to a synthetic `[]TextureInfo` (so the existing `applySortAndFilter` and `columnValue` functions just work).
- Filter / sort identical.
- Preview pane shows the cached thumbnail; channel toggles disabled; info label shows: "Recording aggregate · thumbnail only".
- The Studio Asset column still works (matches against the loaded scan corpus the same way).
- "Open in Single Asset" button still works for matched textures.
- Loading any normal `.rdc` via the launcher's row-Load button switches `dataSource` back to `singleCapture` and discards the recording view (per the YAGNI decision — no recovery affordance for v1).
- Clicking the `[×] Close recording view` clears the aggregate and shows "No capture loaded."

Bridge between launcher and Textures sub-tab: `internal/app/ui` gains a hook (mirrors the existing `OpenSingleAsset` pattern):

```go
var ShowRecordingResults func(textures []renderdoc.AggregateTexture)
```

The Textures sub-tab sets this hook at construction; the launcher calls it on Stop.

## Edge cases

- **Studio exits mid-recording.** The recorder doesn't observe Studio's lifetime directly; the existing watcher signals `studioRunning = false` via `cmd.Wait()`. The recorder keeps running until the user clicks Stop. Captures will fail (no .rdc files arrive); status will plateau. Acceptable.
- **Recording started with Studio not running.** Record button is disabled in this state — same gating as Capture.
- **Manual Capture (F12) clicked during recording.** Resulting `.rdc` flows through the same `ProcessCapture` path. Counted as a capture.
- **Concurrent recordings.** Disallowed for v1 — `Recorder.Start` returns an error if `active`.
- **Fail to move file.** Skip + log. Aggregate not affected.
- **Fail to convert / parse / decode.** Skip the capture, log, push onto `pendingErrs`. Aggregate not affected.
- **Empty aggregate at Stop.** `ShowRecordingResults([]AggregateTexture{})` — Textures sub-tab shows empty table with placeholder.
- **App exit while recording.** Cleanup is best-effort; the recording subdir + any partially-processed files may remain in temp. Acceptable; users can sweep via the existing Open folder button.

## Memory bound

100 unique textures × ~256 KB thumbnail = ~25 MB. 1000 × 256 KB = ~256 MB. For very long sessions this could grow. Acceptable for v1; if it becomes a problem we can swap thumbnails to disk or cap the aggregate size with an LRU.

## Testing

- `recorder_test.go`:
  - State machine transitions: Start → Stop with no captures returns empty aggregate; Stop while not active is a no-op; Start when already active errors.
  - Aggregate dedup: feeding two synthetic `TextureInfo` slices with overlapping dHashes results in the union by first-seen.
  - Error queue cap: pushing 10 errors leaves only the last 5.
  - Worker pool bounding: feeding 30 ProcessCapture calls in parallel with mocked-slow processors; assert `droppedCount == 10` after queue saturates.
  - `-race` clean.
- No UI tests (consistent with existing pattern).

## Build sequence (rough)

1. `internal/renderdoc/recorder.go`: type + state-machine skeleton with synthetic `TextureInfo` injection (no real `.rdc` parsing). Tests for state machine + dedup.
2. Wire the real processing pipeline (convert + parse + hash) into `ProcessCapture`.
3. `internal/app/ui` `ShowRecordingResults` hook.
4. Launcher: Record/Stop button, Interval entry, status polling, fsnotify intercept.
5. Textures sub-tab: `dataSource` field, recording-aggregate render path, header strip, Close button.
6. Manual smoke test: walk a Roblox place for 30 s, verify aggregate.
