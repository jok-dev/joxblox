package loader

import (
	"sync"
	"testing"
)

func TestPublishScanCompletedNotifiesAllSubscribers(t *testing.T) {
	resetScanEventState()
	var mu sync.Mutex
	var calls int
	unsub1 := SubscribeScanCompleted(func() { mu.Lock(); calls++; mu.Unlock() })
	unsub2 := SubscribeScanCompleted(func() { mu.Lock(); calls++; mu.Unlock() })
	defer unsub1()
	defer unsub2()

	PublishScanCompleted([]ScanResult{{AssetID: 1}, {AssetID: 2}})

	if calls != 2 {
		t.Errorf("subscribers called %d times, want 2", calls)
	}
	current := CurrentScan()
	if len(current) != 2 || current[0].AssetID != 1 {
		t.Errorf("CurrentScan returned %+v, want 2 results starting with AssetID=1", current)
	}
}

func TestUnsubscribeStopsFurtherCalls(t *testing.T) {
	resetScanEventState()
	var calls int
	unsub := SubscribeScanCompleted(func() { calls++ })
	unsub()
	PublishScanCompleted(nil)
	if calls != 0 {
		t.Errorf("calls after unsubscribe: got %d, want 0", calls)
	}
}

func TestCurrentScanReturnsNilWhenNothingPublished(t *testing.T) {
	resetScanEventState()
	if got := CurrentScan(); got != nil {
		t.Errorf("CurrentScan before any publish: got %v, want nil", got)
	}
}
