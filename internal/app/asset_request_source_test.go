package app

import "testing"

func TestAssetRequestTraceClassifiesMemoryByDefault(t *testing.T) {
	trace := &assetRequestTrace{}

	if got := trace.classifyRequestSource(); got != heatmapAssetRequestSourceMemory {
		t.Fatalf("expected default trace source to be memory, got %v", got)
	}
}

func TestAssetRequestTraceClassifiesDiskWhenCacheUsed(t *testing.T) {
	trace := &assetRequestTrace{}
	trace.markDisk()

	if got := trace.classifyRequestSource(); got != heatmapAssetRequestSourceDisk {
		t.Fatalf("expected disk source after disk mark, got %v", got)
	}
}

func TestAssetRequestTraceClassifiesNetworkWhenAnyNetworkUsed(t *testing.T) {
	trace := &assetRequestTrace{}
	trace.markDisk()
	trace.markNetwork()

	if got := trace.classifyRequestSource(); got != heatmapAssetRequestSourceNetwork {
		t.Fatalf("expected network source to override disk, got %v", got)
	}
}

func TestFormatSingleRequestSourceBreakdown(t *testing.T) {
	if got := formatSingleRequestSourceBreakdown(heatmapAssetRequestSourceMemory); got != "fetched from: mem 1, disk 0, net 0" {
		t.Fatalf("unexpected memory breakdown string: %q", got)
	}
	if got := formatSingleRequestSourceBreakdown(heatmapAssetRequestSourceDisk); got != "fetched from: mem 0, disk 1, net 0" {
		t.Fatalf("unexpected disk breakdown string: %q", got)
	}
	if got := formatSingleRequestSourceBreakdown(heatmapAssetRequestSourceNetwork); got != "fetched from: mem 0, disk 0, net 1" {
		t.Fatalf("unexpected network breakdown string: %q", got)
	}
}
