package scan

import (
	"strings"
	"testing"

	"joxblox/internal/app/loader"
)

func TestScanTagStoreToggle(t *testing.T) {
	store := NewScanTagStore()
	if store.Has(42, ScanTagDownscale) {
		t.Fatalf("expected fresh store to have no tags")
	}
	if added := store.Toggle(42, ScanTagDownscale); !added {
		t.Fatalf("first toggle should add the tag")
	}
	if !store.Has(42, ScanTagDownscale) {
		t.Fatalf("expected tag to be present after add")
	}
	if added := store.Toggle(42, ScanTagDownscale); added {
		t.Fatalf("second toggle should remove the tag")
	}
	if store.Has(42, ScanTagDownscale) {
		t.Fatalf("expected tag to be gone after remove")
	}
	if store.TaggedCount() != 0 {
		t.Fatalf("removing the last tag should drop the asset entry, got count=%d", store.TaggedCount())
	}
}

func TestScanTagStoreTagsCanonicalOrder(t *testing.T) {
	store := NewScanTagStore()
	// Add in random order; result should still come back in AllScanTags order.
	store.Toggle(7, ScanTagRemove)
	store.Toggle(7, ScanTagDownscale)
	store.Toggle(7, ScanTagAtlas)
	tags := store.Tags(7)
	want := []ScanTag{ScanTagDownscale, ScanTagAtlas, ScanTagRemove}
	if len(tags) != len(want) {
		t.Fatalf("got %v, want %v", tags, want)
	}
	for index, tag := range tags {
		if tag != want[index] {
			t.Fatalf("tag[%d] = %s, want %s (full result %v)", index, tag, want[index], tags)
		}
	}
}

func TestScanTagStoreAssetIDsByTagSorted(t *testing.T) {
	store := NewScanTagStore()
	store.Toggle(20, ScanTagDecimate)
	store.Toggle(5, ScanTagDecimate)
	store.Toggle(13, ScanTagDecimate)
	groups := store.AssetIDsByTag()
	got := groups[ScanTagDecimate]
	want := []int64{5, 13, 20}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for index, id := range got {
		if id != want[index] {
			t.Fatalf("got[%d] = %d, want %d", index, id, want[index])
		}
	}
}

func TestBuildTagHTMLReportGroupsByTag(t *testing.T) {
	store := NewScanTagStore()
	store.Toggle(101, ScanTagDownscale)
	store.Toggle(202, ScanTagDuplicated)
	store.Toggle(202, ScanTagAtlas) // 202 lives in two sections
	results := []loader.ScanResult{
		{AssetID: 101, AssetTypeName: "Texture", Width: 1024, Height: 1024, BytesSize: 2_500_000},
		{AssetID: 202, AssetTypeName: "Texture", Width: 512, Height: 512, BytesSize: 600_000},
		{AssetID: 999, AssetTypeName: "Untagged"},
	}
	htmlReport := BuildTagHTMLReport(results, store, TagHTMLReportOptions{
		Title:      "Test Report",
		SourcePath: "C:/scan/place.rbxl",
	})
	if !strings.Contains(htmlReport, ">Downscale ") {
		t.Fatalf("expected Downscale heading, got:\n%s", htmlReport)
	}
	if !strings.Contains(htmlReport, ">Duplicated ") {
		t.Fatalf("expected Duplicated heading")
	}
	if !strings.Contains(htmlReport, ">Atlas ") {
		t.Fatalf("expected Atlas heading")
	}
	if strings.Contains(htmlReport, ">Decimate ") {
		t.Fatalf("Decimate heading should be omitted (no tagged assets)")
	}
	if strings.Contains(htmlReport, "ID 999") {
		t.Fatalf("untagged asset 999 must not appear")
	}
	if !strings.Contains(htmlReport, "ID 101") {
		t.Fatalf("expected tagged asset 101 in report")
	}
	if strings.Count(htmlReport, "ID 202") != 2 {
		t.Fatalf("asset 202 should appear in both Duplicated and Atlas sections")
	}
	if !strings.Contains(htmlReport, "C:/scan/place.rbxl") {
		t.Fatalf("expected source path to appear in header")
	}
}

func TestBuildTagHTMLReportEmptyStore(t *testing.T) {
	htmlReport := BuildTagHTMLReport(nil, NewScanTagStore(), TagHTMLReportOptions{})
	if !strings.Contains(htmlReport, "No tagged assets yet") {
		t.Fatalf("empty store should render the empty-state message, got:\n%s", htmlReport)
	}
}

func TestSetDuplicateGroupTagsAllMembers(t *testing.T) {
	store := NewScanTagStore()
	store.SetDuplicateGroup([]int64{10, 20, 30})
	for _, id := range []int64{10, 20, 30} {
		if !store.Has(id, ScanTagDuplicated) {
			t.Errorf("asset %d expected Duplicated tag after SetDuplicateGroup", id)
		}
	}
	groups := store.DuplicateGroups()
	if len(groups) != 1 {
		t.Fatalf("got %d groups, want 1", len(groups))
	}
	if !sameAssetSet(groups[0], []int64{10, 20, 30}) {
		t.Errorf("group members = %v, want {10,20,30}", groups[0])
	}
}

func TestSetDuplicateGroupPreservesPrimaryFirst(t *testing.T) {
	// The user picks asset 50 first (the right-clicked row), then adds
	// 10 and 30. Order in the input should survive into DuplicateGroupOf
	// so the HTML report can render the primary at the top of the group.
	store := NewScanTagStore()
	store.SetDuplicateGroup([]int64{50, 10, 30})
	got := store.DuplicateGroupOf(50)
	want := []int64{50, 10, 30}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i, id := range got {
		if id != want[i] {
			t.Fatalf("got %v, want %v (order matters)", got, want)
		}
	}
}

func TestSetDuplicateGroupMovingAssetDissolvesOrphanGroup(t *testing.T) {
	// First group [10,20,30]; user opens dialog from asset 50 and picks
	// {50, 20}. Asset 20 should leave the first group; group 1 must
	// dissolve since [10,30] is still ≥ 2 — wait, that's still a group.
	// Better test: pull TWO members so the original drops to 1.
	store := NewScanTagStore()
	store.SetDuplicateGroup([]int64{10, 20, 30})
	store.SetDuplicateGroup([]int64{50, 20, 30})
	if !store.Has(50, ScanTagDuplicated) || !store.Has(20, ScanTagDuplicated) || !store.Has(30, ScanTagDuplicated) {
		t.Fatalf("members of new group should all carry Duplicated tag")
	}
	if store.Has(10, ScanTagDuplicated) {
		t.Errorf("asset 10 was the only one left in the first group — should have lost Duplicated tag when the group dissolved")
	}
	groups := store.DuplicateGroups()
	if len(groups) != 1 {
		t.Fatalf("expected 1 surviving group, got %d", len(groups))
	}
}

func TestSetDuplicateGroupSingleMemberClearsTag(t *testing.T) {
	// Calling SetDuplicateGroup with just one id pulls that id out of any
	// prior group and clears its Duplicated tag — the remaining members
	// of the prior group stay a valid group as long as ≥2 of them remain.
	store := NewScanTagStore()
	store.SetDuplicateGroup([]int64{10, 20, 30})
	store.SetDuplicateGroup([]int64{10})
	if store.Has(10, ScanTagDuplicated) {
		t.Errorf("asset 10 alone is not a duplicate group — Duplicated tag should be cleared")
	}
	if got := store.DuplicateGroupOf(10); got != nil {
		t.Errorf("asset 10 should be ungrouped, got %v", got)
	}
	groups := store.DuplicateGroups()
	if len(groups) != 1 || !sameAssetSet(groups[0], []int64{20, 30}) {
		t.Errorf("surviving group should be {20,30}, got %v", groups)
	}
}

func TestBuildTagHTMLReportRendersDuplicateGroups(t *testing.T) {
	store := NewScanTagStore()
	store.SetDuplicateGroup([]int64{101, 102, 103})
	results := []loader.ScanResult{
		{AssetID: 101, AssetTypeName: "Texture", Width: 1024, Height: 1024, BytesSize: 2_500_000, PixelCount: 1024 * 1024, PropertyName: "ColorMapContent"},
		{AssetID: 102, AssetTypeName: "Texture", Width: 1024, Height: 1024, BytesSize: 2_400_000, PixelCount: 1024 * 1024, PropertyName: "ColorMapContent"},
		{AssetID: 103, AssetTypeName: "Texture", Width: 1024, Height: 1024, BytesSize: 2_300_000, PixelCount: 1024 * 1024, PropertyName: "ColorMapContent"},
	}
	htmlReport := BuildTagHTMLReport(results, store, TagHTMLReportOptions{Title: "Test"})
	if !strings.Contains(htmlReport, "Group 1") {
		t.Fatalf("expected the curated group to render as 'Group 1', got:\n%s", htmlReport)
	}
	if !strings.Contains(htmlReport, "3 copies") {
		t.Fatalf("expected the group header to show '3 copies'")
	}
	if !strings.Contains(htmlReport, "ID 101") || !strings.Contains(htmlReport, "ID 102") || !strings.Contains(htmlReport, "ID 103") {
		t.Fatalf("expected all three group members to appear in the report")
	}
}

func TestBuildTagHTMLReportShowsGPUMemoryNotFileSize(t *testing.T) {
	store := NewScanTagStore()
	store.Toggle(101, ScanTagDownscale)
	results := []loader.ScanResult{
		{AssetID: 101, AssetTypeName: "Texture", Width: 1024, Height: 1024, BytesSize: 2_500_000, PixelCount: 1024 * 1024, PropertyName: "ColorMapContent"},
	}
	htmlReport := BuildTagHTMLReport(results, store, TagHTMLReportOptions{})
	if !strings.Contains(htmlReport, "GPU ") {
		t.Fatalf("expected GPU memory line in card body, got:\n%s", htmlReport)
	}
	// File-size line was the previous behavior; make sure we no longer
	// emit a raw 2.38 MB line for the BytesSize value.
	if strings.Contains(htmlReport, "2.38 MB") && !strings.Contains(htmlReport, "GPU 2.38 MB") {
		t.Errorf("file-size line should no longer appear without the 'GPU' prefix")
	}
}

func TestAutoTagDetectedDuplicatesGroupsBySHA(t *testing.T) {
	store := NewScanTagStore()
	rows := []loader.ScanResult{
		{AssetID: 1, FileSHA256: "sha-aaa"},
		{AssetID: 2, FileSHA256: "sha-aaa"},
		{AssetID: 3, FileSHA256: "sha-aaa"},
		{AssetID: 4, FileSHA256: "sha-bbb"},
		{AssetID: 5, FileSHA256: "sha-bbb"},
		{AssetID: 6, FileSHA256: "sha-unique"},
		{AssetID: 7, FileSHA256: ""},
	}
	created := store.AutoTagDetectedDuplicates(rows)
	if created != 2 {
		t.Errorf("created = %d, want 2 (sha-aaa, sha-bbb)", created)
	}
	if got := store.DuplicateGroupOf(1); len(got) != 3 {
		t.Errorf("group for sha-aaa = %v, want 3 members", got)
	}
	if got := store.DuplicateGroupOf(4); len(got) != 2 {
		t.Errorf("group for sha-bbb = %v, want 2 members", got)
	}
	if store.Has(6, ScanTagDuplicated) {
		t.Errorf("asset 6 has a unique sha — should not be tagged")
	}
	if store.Has(7, ScanTagDuplicated) {
		t.Errorf("asset 7 has empty sha — should not be tagged")
	}
}

func TestAutoTagDetectedDuplicatesSkipsClustersWithExistingGroupMembers(t *testing.T) {
	store := NewScanTagStore()
	store.SetDuplicateGroup([]int64{1, 99}) // user manually grouped 1 with 99
	rows := []loader.ScanResult{
		{AssetID: 1, FileSHA256: "sha-aaa"},
		{AssetID: 2, FileSHA256: "sha-aaa"},
		{AssetID: 3, FileSHA256: "sha-aaa"},
	}
	created := store.AutoTagDetectedDuplicates(rows)
	if created != 0 {
		t.Errorf("created = %d, want 0 — manual group on member 1 should block auto-tagging this cluster", created)
	}
	// User's group with 99 should still be intact.
	if got := store.DuplicateGroupOf(1); len(got) != 2 {
		t.Errorf("manual group for asset 1 = %v, want [1, 99]", got)
	}
	// Other members of the SHA cluster should not be tagged.
	if store.Has(2, ScanTagDuplicated) || store.Has(3, ScanTagDuplicated) {
		t.Errorf("assets 2/3 should not be tagged when their cluster overlaps a manual group")
	}
}

func TestAutoTagDetectedDuplicatesIsIdempotent(t *testing.T) {
	store := NewScanTagStore()
	rows := []loader.ScanResult{
		{AssetID: 1, FileSHA256: "sha-aaa"},
		{AssetID: 2, FileSHA256: "sha-aaa"},
	}
	if got := store.AutoTagDetectedDuplicates(rows); got != 1 {
		t.Fatalf("first call: created %d, want 1", got)
	}
	if got := store.AutoTagDetectedDuplicates(rows); got != 0 {
		t.Errorf("second call should be a no-op (cluster already grouped), got %d new groups", got)
	}
	if len(store.DuplicateGroups()) != 1 {
		t.Errorf("expected exactly 1 group after idempotent re-run, got %d", len(store.DuplicateGroups()))
	}
}

func TestSetDuplicateGroupAllSingleDissolves(t *testing.T) {
	// Pulling enough members out that the prior group drops below 2 must
	// dissolve it and clear Duplicated from the lone survivor.
	store := NewScanTagStore()
	store.SetDuplicateGroup([]int64{10, 20})
	store.SetDuplicateGroup([]int64{10})
	if store.Has(20, ScanTagDuplicated) {
		t.Errorf("asset 20 was alone in the prior group — Duplicated should have been cleared")
	}
	if len(store.DuplicateGroups()) != 0 {
		t.Errorf("expected 0 groups, got %v", store.DuplicateGroups())
	}
}

func TestToggleDuplicatedDropsAssetFromGroup(t *testing.T) {
	store := NewScanTagStore()
	store.SetDuplicateGroup([]int64{10, 20, 30})
	// Toggle removes Duplicated from 10 — group must drop it. Group still
	// has [20,30] so it survives.
	store.Toggle(10, ScanTagDuplicated)
	if got := store.DuplicateGroupOf(10); got != nil {
		t.Errorf("asset 10 should be ungrouped after Toggle, got %v", got)
	}
	if got := store.DuplicateGroupOf(20); len(got) != 2 {
		t.Errorf("surviving group should have 2 members, got %v", got)
	}
}

func sameAssetSet(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	in := map[int64]struct{}{}
	for _, id := range a {
		in[id] = struct{}{}
	}
	for _, id := range b {
		if _, ok := in[id]; !ok {
			return false
		}
	}
	return true
}
