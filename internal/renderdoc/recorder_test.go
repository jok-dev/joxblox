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

func TestRecorderMergeAggregatesByDHashLargestWins(t *testing.T) {
	sessionDir := t.TempDir()
	r := NewRecorder()
	if err := r.Start(sessionDir, time.Hour); err != nil {
		t.Fatalf("Start: %v", err)
	}
	firstSeen := time.Now().Add(-time.Hour)
	r.merge(&AggregateTexture{DHash: 0xAAAA, Resource: "small", Bytes: 1024, FirstSeen: firstSeen})
	r.merge(&AggregateTexture{DHash: 0xBBBB, Resource: "other", Bytes: 4096})
	r.merge(&AggregateTexture{DHash: 0xAAAA, Resource: "large", Bytes: 4 * 1024 * 1024}) // bigger — should replace
	r.merge(&AggregateTexture{DHash: 0xAAAA, Resource: "tiny", Bytes: 16})                // smaller — should be ignored
	got := r.Stop()
	if len(got) != 2 {
		t.Fatalf("aggregate len: got %d, want 2", len(got))
	}
	for _, tex := range got {
		if tex.DHash != 0xAAAA {
			continue
		}
		if tex.Resource != "large" {
			t.Errorf("largest-wins on dHash collision: got Resource=%q (Bytes=%d), want Resource=large", tex.Resource, tex.Bytes)
		}
		if !tex.FirstSeen.Equal(firstSeen) {
			t.Errorf("FirstSeen should carry over from original entry: got %v, want %v", tex.FirstSeen, firstSeen)
		}
	}
}

func TestRecorderDropsCapturesWhenQueueSaturates(t *testing.T) {
	sessionDir := t.TempDir()
	r := NewRecorder()
	_ = r.Start(sessionDir, time.Hour)
	defer r.Stop()
	var slowMu sync.Mutex
	slowMu.Lock()
	r.processFunc = func(rdcPath string) error {
		slowMu.Lock()
		slowMu.Unlock()
		return nil
	}
	for i := 0; i < 30; i++ {
		path := filepath.Join(sessionDir, "fake.rdc")
		_ = os.WriteFile(path, []byte("synthetic"), 0o644)
		r.ProcessCapture(path)
	}
	snap := r.Snapshot()
	if snap.DroppedCount == 0 {
		t.Errorf("expected drops once queue saturated, got DroppedCount=0 (queueDepth=%d)", snap.QueueDepth)
	}
	slowMu.Unlock()
}
