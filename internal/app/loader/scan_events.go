package loader

import "sync"

// scanEvents is a tiny package-level publish/subscribe channel for "the
// most recently completed scan changed." Used by the RenderDoc tab to
// rebuild its asset-ID match overlay without coupling to the Scan tab's
// internals. Only one publisher (the Scan tab); fan-out to N subscribers.
type scanEvents struct {
	mu          sync.RWMutex
	current     []ScanResult
	subscribers map[int]func()
	nextID      int
}

var scanEventState = &scanEvents{subscribers: map[int]func(){}}

// CurrentScan returns the most recently published scan results, or nil if
// nothing has been published this session. The slice is shared — callers
// must not mutate it.
func CurrentScan() []ScanResult {
	scanEventState.mu.RLock()
	defer scanEventState.mu.RUnlock()
	return scanEventState.current
}

// SubscribeScanCompleted registers a callback that fires every time a new
// scan completes. Returns a function to unregister. Safe to call from any
// goroutine; callbacks fire on the publishing goroutine.
func SubscribeScanCompleted(callback func()) (unsubscribe func()) {
	scanEventState.mu.Lock()
	id := scanEventState.nextID
	scanEventState.nextID++
	scanEventState.subscribers[id] = callback
	scanEventState.mu.Unlock()
	return func() {
		scanEventState.mu.Lock()
		delete(scanEventState.subscribers, id)
		scanEventState.mu.Unlock()
	}
}

// PublishScanCompleted records the new results as the current scan and
// notifies every active subscriber. Pass nil to indicate "results were
// cleared" (CurrentScan will then return nil).
func PublishScanCompleted(results []ScanResult) {
	scanEventState.mu.Lock()
	scanEventState.current = results
	subscribers := make([]func(), 0, len(scanEventState.subscribers))
	for _, cb := range scanEventState.subscribers {
		subscribers = append(subscribers, cb)
	}
	scanEventState.mu.Unlock()
	for _, cb := range subscribers {
		cb()
	}
}

// resetScanEventState is for tests only — restores the bus to its
// initial empty state so tests don't see each other's residue.
func resetScanEventState() {
	scanEventState.mu.Lock()
	scanEventState.current = nil
	scanEventState.subscribers = map[int]func(){}
	scanEventState.nextID = 0
	scanEventState.mu.Unlock()
}
