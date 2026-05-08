package scan

import (
	"sort"
	"strings"

	"joxblox/internal/app/loader"
)

// ScanTag identifies a triage label a user attaches to a scan-result row.
// Tags live in-memory per ScanResultsExplorer (not persisted, not exported
// to JSON) and feed the HTML report grouping.
type ScanTag string

const (
	ScanTagDownscale   ScanTag = "Downscale"
	ScanTagDuplicated  ScanTag = "Duplicated"
	ScanTagDecimate    ScanTag = "Decimate"
	ScanTagAtlas       ScanTag = "Atlas"
	ScanTagRemoveAlpha ScanTag = "Remove Alpha"
	ScanTagRemove      ScanTag = "Remove"
)

// AllScanTags returns the canonical menu order. The HTML report uses the
// same order so the user always sees Downscale first, Remove last.
func AllScanTags() []ScanTag {
	return []ScanTag{
		ScanTagDownscale,
		ScanTagDuplicated,
		ScanTagDecimate,
		ScanTagAtlas,
		ScanTagRemoveAlpha,
		ScanTagRemove,
	}
}

// ScanTagStore maps an asset id to the set of tags currently applied to
// it. The empty value is usable.
//
// Beyond per-asset tags, the store also tracks user-curated duplicate
// groups: lists of asset ids the user has flagged as variants of each
// other. Group membership implies the Duplicated tag (every member is
// also tagged), and dropping out of a group strips that tag. The HTML
// report uses the groups to render related copies together rather than
// as a flat asset list.
type ScanTagStore struct {
	tagsByAssetID  map[int64]map[ScanTag]struct{}
	groupByAssetID map[int64]int
	groupMembers   map[int][]int64
	nextGroupID    int
}

func NewScanTagStore() *ScanTagStore {
	return &ScanTagStore{
		tagsByAssetID:  map[int64]map[ScanTag]struct{}{},
		groupByAssetID: map[int64]int{},
		groupMembers:   map[int][]int64{},
	}
}

func (store *ScanTagStore) Has(assetID int64, tag ScanTag) bool {
	if store == nil || assetID <= 0 {
		return false
	}
	tagSet, found := store.tagsByAssetID[assetID]
	if !found {
		return false
	}
	_, present := tagSet[tag]
	return present
}

func (store *ScanTagStore) Toggle(assetID int64, tag ScanTag) bool {
	if store == nil || assetID <= 0 {
		return false
	}
	if store.tagsByAssetID == nil {
		store.tagsByAssetID = map[int64]map[ScanTag]struct{}{}
	}
	tagSet, found := store.tagsByAssetID[assetID]
	if !found {
		store.tagsByAssetID[assetID] = map[ScanTag]struct{}{tag: {}}
		return true
	}
	if _, present := tagSet[tag]; present {
		delete(tagSet, tag)
		if len(tagSet) == 0 {
			delete(store.tagsByAssetID, assetID)
		}
		if tag == ScanTagDuplicated {
			store.removeFromGroupAndDissolveIfTooSmall(assetID)
		}
		return false
	}
	tagSet[tag] = struct{}{}
	return true
}

// SetDuplicateGroup makes assetIDs a single duplicate group. Every member
// is removed from any prior group it belonged to (which dissolves prior
// groups that drop below 2 members) and is then tagged as Duplicated.
// The order of assetIDs is preserved — callers pass the user-tagged
// "primary" first so the HTML report renders it at the top of the group.
//
// Calling with fewer than 2 unique non-zero ids dissolves any group those
// ids belonged to and strips Duplicated from the lone remaining id, which
// is the natural "I changed my mind" outcome from the picker dialog.
func (store *ScanTagStore) SetDuplicateGroup(assetIDs []int64) {
	if store == nil {
		return
	}
	if store.tagsByAssetID == nil {
		store.tagsByAssetID = map[int64]map[ScanTag]struct{}{}
	}
	if store.groupByAssetID == nil {
		store.groupByAssetID = map[int64]int{}
	}
	if store.groupMembers == nil {
		store.groupMembers = map[int][]int64{}
	}
	cleaned := make([]int64, 0, len(assetIDs))
	seen := map[int64]struct{}{}
	for _, id := range assetIDs {
		if id <= 0 {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		cleaned = append(cleaned, id)
	}
	for _, id := range cleaned {
		store.removeFromGroupNoDissolveCheck(id)
	}
	if len(cleaned) < 2 {
		// Dissolve any group those ids belonged to — already done above —
		// and ensure no Duplicated tag clings to the lone survivor (or
		// nothing) since a single asset isn't a duplicate of anything.
		for _, id := range cleaned {
			store.removeTagInternal(id, ScanTagDuplicated)
		}
		store.compactStaleGroups()
		return
	}
	store.nextGroupID++
	groupID := store.nextGroupID
	store.groupMembers[groupID] = append([]int64(nil), cleaned...)
	for _, id := range cleaned {
		store.groupByAssetID[id] = groupID
		tagSet, ok := store.tagsByAssetID[id]
		if !ok {
			tagSet = map[ScanTag]struct{}{}
			store.tagsByAssetID[id] = tagSet
		}
		tagSet[ScanTagDuplicated] = struct{}{}
	}
	store.compactStaleGroups()
}

// AutoTagDetectedDuplicates groups assets whose FileSHA256 hashes match —
// i.e. byte-identical files the loader already detected as duplicates —
// into ScanTagDuplicated groups. Returns the number of new groups
// created.
//
// Conservative about overwriting user choices: a SHA cluster is skipped
// entirely if ANY member is already in a duplicate group (either via the
// picker dialog or a previous auto-tag run). That preserves manual
// curation across re-scans without requiring callers to reason about
// merge semantics. Idempotent for the same row set: rows already grouped
// by a prior auto-tag pass simply skip on the second call.
func (store *ScanTagStore) AutoTagDetectedDuplicates(rows []loader.ScanResult) int {
	if store == nil || len(rows) == 0 {
		return 0
	}
	clusters := map[string][]int64{}
	clusterOrder := []string{}
	clusterSeen := map[string]map[int64]struct{}{}
	for _, row := range rows {
		if row.AssetID <= 0 {
			continue
		}
		hash := strings.TrimSpace(row.FileSHA256)
		if hash == "" {
			continue
		}
		seen, ok := clusterSeen[hash]
		if !ok {
			seen = map[int64]struct{}{}
			clusterSeen[hash] = seen
			clusterOrder = append(clusterOrder, hash)
		}
		if _, dup := seen[row.AssetID]; dup {
			continue
		}
		seen[row.AssetID] = struct{}{}
		clusters[hash] = append(clusters[hash], row.AssetID)
	}
	created := 0
	for _, hash := range clusterOrder {
		members := clusters[hash]
		if len(members) < 2 {
			continue
		}
		anyAlreadyGrouped := false
		for _, id := range members {
			if _, grouped := store.groupByAssetID[id]; grouped {
				anyAlreadyGrouped = true
				break
			}
		}
		if anyAlreadyGrouped {
			continue
		}
		store.SetDuplicateGroup(members)
		created++
	}
	return created
}

// DuplicateGroupOf returns the list of ids in assetID's duplicate group
// (in the order they were registered) or nil if it isn't grouped.
func (store *ScanTagStore) DuplicateGroupOf(assetID int64) []int64 {
	if store == nil || assetID <= 0 {
		return nil
	}
	groupID, ok := store.groupByAssetID[assetID]
	if !ok {
		return nil
	}
	members := store.groupMembers[groupID]
	if len(members) == 0 {
		return nil
	}
	out := make([]int64, len(members))
	copy(out, members)
	return out
}

// DuplicateGroups returns every group as a slice of member id slices,
// stable-sorted by group creation order so callers can render groups in
// a deterministic top-to-bottom layout.
func (store *ScanTagStore) DuplicateGroups() [][]int64 {
	if store == nil || len(store.groupMembers) == 0 {
		return nil
	}
	groupIDs := make([]int, 0, len(store.groupMembers))
	for id := range store.groupMembers {
		groupIDs = append(groupIDs, id)
	}
	sort.Ints(groupIDs)
	out := make([][]int64, 0, len(groupIDs))
	for _, id := range groupIDs {
		members := store.groupMembers[id]
		if len(members) < 2 {
			continue
		}
		copyOfMembers := make([]int64, len(members))
		copy(copyOfMembers, members)
		out = append(out, copyOfMembers)
	}
	return out
}

func (store *ScanTagStore) removeFromGroupAndDissolveIfTooSmall(assetID int64) {
	groupID, ok := store.groupByAssetID[assetID]
	if !ok {
		return
	}
	store.removeFromGroupNoDissolveCheck(assetID)
	store.dissolveGroupIfTooSmall(groupID)
}

func (store *ScanTagStore) removeFromGroupNoDissolveCheck(assetID int64) {
	groupID, ok := store.groupByAssetID[assetID]
	if !ok {
		return
	}
	delete(store.groupByAssetID, assetID)
	members := store.groupMembers[groupID]
	filtered := members[:0]
	for _, id := range members {
		if id != assetID {
			filtered = append(filtered, id)
		}
	}
	if len(filtered) == 0 {
		delete(store.groupMembers, groupID)
	} else {
		store.groupMembers[groupID] = filtered
	}
}

func (store *ScanTagStore) dissolveGroupIfTooSmall(groupID int) {
	members, ok := store.groupMembers[groupID]
	if !ok {
		return
	}
	if len(members) >= 2 {
		return
	}
	for _, id := range members {
		delete(store.groupByAssetID, id)
		store.removeTagInternal(id, ScanTagDuplicated)
	}
	delete(store.groupMembers, groupID)
}

// compactStaleGroups walks every group and dissolves any whose
// post-mutation member count fell below the duplicate-group threshold of
// two. Used after SetDuplicateGroup so prior groups whose members were
// pulled into the new group don't linger as orphan singletons.
func (store *ScanTagStore) compactStaleGroups() {
	stale := []int{}
	for groupID, members := range store.groupMembers {
		if len(members) < 2 {
			stale = append(stale, groupID)
		}
	}
	for _, groupID := range stale {
		store.dissolveGroupIfTooSmall(groupID)
	}
}

func (store *ScanTagStore) removeTagInternal(assetID int64, tag ScanTag) {
	tagSet, ok := store.tagsByAssetID[assetID]
	if !ok {
		return
	}
	delete(tagSet, tag)
	if len(tagSet) == 0 {
		delete(store.tagsByAssetID, assetID)
	}
}

// Tags returns the tags on assetID in canonical order so callers can render
// them deterministically.
func (store *ScanTagStore) Tags(assetID int64) []ScanTag {
	if store == nil || assetID <= 0 {
		return nil
	}
	tagSet, found := store.tagsByAssetID[assetID]
	if !found || len(tagSet) == 0 {
		return nil
	}
	result := make([]ScanTag, 0, len(tagSet))
	for _, tag := range AllScanTags() {
		if _, present := tagSet[tag]; present {
			result = append(result, tag)
		}
	}
	return result
}

// AssetIDsByTag groups every tagged asset id under each tag, in canonical
// tag order, with asset ids sorted ascending. Tags with zero hits are
// omitted. Used by the HTML report to lay out one section per tag.
func (store *ScanTagStore) AssetIDsByTag() map[ScanTag][]int64 {
	if store == nil {
		return map[ScanTag][]int64{}
	}
	idsByTag := map[ScanTag][]int64{}
	for assetID, tagSet := range store.tagsByAssetID {
		for tag := range tagSet {
			idsByTag[tag] = append(idsByTag[tag], assetID)
		}
	}
	for tag := range idsByTag {
		sort.Slice(idsByTag[tag], func(i, j int) bool {
			return idsByTag[tag][i] < idsByTag[tag][j]
		})
	}
	return idsByTag
}

// TaggedCount is the number of distinct asset ids that carry at least one
// tag. Drives the report button's enabled state and the status hints.
func (store *ScanTagStore) TaggedCount() int {
	if store == nil {
		return 0
	}
	return len(store.tagsByAssetID)
}
