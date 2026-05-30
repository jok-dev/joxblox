package webapp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"joxblox/internal/app/loader"
	"joxblox/internal/app/scan"
)

// TagDefinition is one entry in the user's global tag library — a name, an
// optional optimization hint shown as a suggestion, and a display color. The
// library is the vocabulary of tags the user can apply to instances; the order
// is significant (it drives the Tag Report section order).
type TagDefinition struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Color       string `json:"color"`
}

// presetTagDefinitions seed a fresh library with the optimization triage labels
// from the feature spec (the three suggested plus the legacy set). They are
// ordinary definitions afterwards — the user can rename, recolor, reorder, or
// delete them in Tag Management. The descriptions double as the "how to
// optimize" hints surfaced as suggestions.
func presetTagDefinitions() []TagDefinition {
	return []TagDefinition{
		{Name: "Decimate", Description: "Reduce mesh triangle count", Color: "#e06c75"},
		{Name: "Reduce", Description: "Downscale texture resolution", Color: "#e5c07b"},
		{Name: "Duplicate", Description: "Redundant copy — dedupe", Color: "#61afef"},
		{Name: "Atlas", Description: "Combine into a texture atlas", Color: "#98c379"},
		{Name: "Remove Alpha", Description: "Drop unused alpha channel", Color: "#c678dd"},
		{Name: "Remove", Description: "Delete / unused asset", Color: "#abb2bf"},
	}
}

// tagStoreFile is the on-disk shape of ~/.config/joxblox/tags.json.
//
//   - Definitions is the global tag library.
//   - Assignments maps an asset id (as a string, since JSON object keys must be
//     strings) to the list of tag names applied to it. Assignments are GLOBAL:
//     a Roblox asset id keeps its tags across every place/model file, so the
//     same asset re-encountered in another file reuses what was tagged before.
//   - Notes maps an asset id to a free-form custom note. Like assignments, notes
//     are global: a note follows the asset id across every file it appears in.
type tagStoreFile struct {
	Definitions []TagDefinition     `json:"definitions"`
	Assignments map[string][]string `json:"assignments"`
	Notes       map[string]string   `json:"notes"`
}

// TagService persists the tag library and the global asset-tag assignments, and
// builds the tagged-instance PDF report. Mirrors SettingsService: a single
// mutex-guarded JSON file under the OS config dir.
type TagService struct {
	ctx context.Context
	mu  sync.Mutex
}

func NewTagService() *TagService { return &TagService{} }

func (s *TagService) startup(ctx context.Context) { s.ctx = ctx }

func tagsFilePath() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "joxblox")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(dir, "tags.json"), nil
}

// loadStore reads the tag store from disk. A missing file yields an
// empty-but-usable store; a malformed file is treated as empty rather than
// failing the whole feature.
func loadStore() tagStoreFile {
	store := tagStoreFile{Assignments: map[string][]string{}, Notes: map[string]string{}}
	path, err := tagsFilePath()
	if err != nil {
		return store
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return store
	}
	_ = json.Unmarshal(data, &store)
	if store.Assignments == nil {
		store.Assignments = map[string][]string{}
	}
	if store.Notes == nil {
		store.Notes = map[string]string{}
	}
	return store
}

func saveStore(store tagStoreFile) error {
	path, err := tagsFilePath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// ListTagDefinitions returns the global tag library, seeding and persisting the
// preset suggestions the first time the store is empty.
func (s *TagService) ListTagDefinitions() ([]TagDefinition, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	store := loadStore()
	if len(store.Definitions) == 0 {
		store.Definitions = presetTagDefinitions()
		_ = saveStore(store)
	}
	return store.Definitions, nil
}

// SaveTagDefinitions replaces the global tag library. Names are trimmed and
// de-duplicated (case-insensitively) so the picker and report never show two
// identical tags. Any assignment referencing a tag no longer in the library is
// pruned, so deleting a tag in Tag Management cleanly untags its instances.
// (Use RenameTag to keep assignments when only the name changes.)
func (s *TagService) SaveTagDefinitions(defs []TagDefinition) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	store := loadStore()
	store.Definitions = dedupeDefinitions(defs)
	pruneAssignments(&store)
	return saveStore(store)
}

func dedupeDefinitions(defs []TagDefinition) []TagDefinition {
	out := make([]TagDefinition, 0, len(defs))
	seen := map[string]struct{}{}
	for _, def := range defs {
		name := strings.TrimSpace(def.Name)
		if name == "" {
			continue
		}
		lower := strings.ToLower(name)
		if _, dup := seen[lower]; dup {
			continue
		}
		seen[lower] = struct{}{}
		out = append(out, TagDefinition{
			Name:        name,
			Description: strings.TrimSpace(def.Description),
			Color:       strings.TrimSpace(def.Color),
		})
	}
	return out
}

// pruneAssignments drops any tag name that no longer exists in the library from
// every asset's assignment list, and removes assets left with no tags.
func pruneAssignments(store *tagStoreFile) {
	known := map[string]struct{}{}
	for _, def := range store.Definitions {
		known[def.Name] = struct{}{}
	}
	for assetKey, tags := range store.Assignments {
		kept := make([]string, 0, len(tags))
		for _, tag := range tags {
			if _, ok := known[tag]; ok {
				kept = append(kept, tag)
			}
		}
		if len(kept) == 0 {
			delete(store.Assignments, assetKey)
		} else {
			store.Assignments[assetKey] = kept
		}
	}
}

// RenameTag renames a tag in the library and migrates every assignment that
// referenced the old name, so existing tagged instances keep their tag.
func (s *TagService) RenameTag(oldName, newName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	from := strings.TrimSpace(oldName)
	to := strings.TrimSpace(newName)
	if from == "" || to == "" {
		return fmt.Errorf("tag name cannot be empty")
	}
	store := loadStore()
	for i := range store.Definitions {
		if strings.EqualFold(store.Definitions[i].Name, from) {
			store.Definitions[i].Name = to
		}
	}
	store.Definitions = dedupeDefinitions(store.Definitions)
	for assetKey, tags := range store.Assignments {
		changed := false
		for j, tag := range tags {
			if tag == from {
				tags[j] = to
				changed = true
			}
		}
		if changed {
			store.Assignments[assetKey] = cleanTagList(tags)
		}
	}
	return saveStore(store)
}

// GetAssignments returns the global asset-id→tags map (empty when nothing is
// tagged). Asset ids are strings to match the JSON shape the frontend consumes.
func (s *TagService) GetAssignments() (map[string][]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	store := loadStore()
	if store.Assignments == nil {
		return map[string][]string{}, nil
	}
	return store.Assignments, nil
}

// TaggedCount is the number of distinct assets carrying at least one tag.
func (s *TagService) TaggedCount() (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(loadStore().Assignments), nil
}

// SetAssetTags replaces the tag set on a single asset id (global across files).
// An empty list removes the asset's entry so the store doesn't accumulate dead
// keys. Tag names that aren't in the library yet are kept as-is — the picker's
// "create + apply" flow saves the new definition separately.
func (s *TagService) SetAssetTags(assetID int64, tags []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if assetID <= 0 {
		return fmt.Errorf("invalid asset id")
	}
	store := loadStore()
	assetKey := strconv.FormatInt(assetID, 10)
	cleaned := cleanTagList(tags)
	if len(cleaned) == 0 {
		delete(store.Assignments, assetKey)
	} else {
		store.Assignments[assetKey] = cleaned
	}
	return saveStore(store)
}

// GetNotes returns the global asset-id→note map (empty when nothing is noted).
// Asset ids are strings to match the JSON shape the frontend consumes.
func (s *TagService) GetNotes() (map[string]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	store := loadStore()
	if store.Notes == nil {
		return map[string]string{}, nil
	}
	return store.Notes, nil
}

// SetAssetNote sets (or clears) the free-form custom note on a single asset id
// (global across files). A blank note removes the asset's entry so the store
// doesn't accumulate dead keys.
func (s *TagService) SetAssetNote(assetID int64, note string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if assetID <= 0 {
		return fmt.Errorf("invalid asset id")
	}
	store := loadStore()
	if store.Notes == nil {
		store.Notes = map[string]string{}
	}
	assetKey := strconv.FormatInt(assetID, 10)
	trimmed := strings.TrimSpace(note)
	if trimmed == "" {
		delete(store.Notes, assetKey)
	} else {
		store.Notes[assetKey] = trimmed
	}
	return saveStore(store)
}

func cleanTagList(tags []string) []string {
	out := make([]string, 0, len(tags))
	seen := map[string]struct{}{}
	for _, tag := range tags {
		name := strings.TrimSpace(tag)
		if name == "" {
			continue
		}
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out
}

// GenerateTagReportPDF builds and saves (via native dialog) the rich card-grid
// PDF of every tagged instance found in the file, grouped by tag in library
// order. Assignments are global, so only the tagged assets actually present in
// this file are resolved and rendered (the others have no preview/instance-path
// to show for this file). Returns false (no error) when the user cancels the
// save dialog.
func (s *TagService) GenerateTagReportPDF(filePath string, embedImages bool) (bool, error) {
	trimmedPath := strings.TrimSpace(filePath)
	if trimmedPath == "" {
		return false, fmt.Errorf("Select an .rbxl or .rbxm file first.")
	}

	s.mu.Lock()
	store := loadStore()
	assignments := store.Assignments
	order := make([]string, 0, len(store.Definitions))
	for _, def := range store.Definitions {
		order = append(order, def.Name)
	}
	s.mu.Unlock()

	if len(assignments) == 0 {
		return false, fmt.Errorf("No tagged instances yet. Right-click a row in Asset Breakdown to tag one.")
	}

	idsByTag, taggedIDs, knownTags := buildTagIndex(assignments)
	// Tags that were applied but aren't in the current library order (e.g.
	// created inline and not yet saved to the library) still render — append
	// them after the library-ordered tags so nothing silently vanishes.
	for _, tag := range order {
		delete(knownTags, tag)
	}
	leftover := make([]string, 0, len(knownTags))
	for tag := range knownTags {
		leftover = append(leftover, tag)
	}
	sort.Strings(leftover)
	order = append(order, leftover...)

	results, err := s.resolveTaggedResults(trimmedPath, taggedIDs)
	if err != nil {
		return false, err
	}

	present := map[int64]bool{}
	for _, result := range results {
		if result.AssetID > 0 {
			present[result.AssetID] = true
		}
	}
	if len(present) == 0 {
		return false, fmt.Errorf("None of your tagged instances were found in this file.")
	}
	idsByTag = restrictIdsByTag(idsByTag, present)

	options := scan.TagPDFReportOptions{
		Title:       "Joxblox Tag Report",
		SourcePath:  trimmedPath,
		EmbedImages: embedImages,
	}
	pdfBytes, err := scan.BuildTagPDFReportOrdered(results, idsByTag, order, len(present), options)
	if err != nil {
		return false, err
	}

	defaultName := tagReportBaseName(trimmedPath) + ".pdf"
	return saveBytesViaDialog(s.ctx, "Export tag report as PDF", defaultName, "PDF report", "*.pdf", pdfBytes)
}

// buildTagIndex inverts an asset-id→tags map into a tag→asset-ids map (sorted),
// the set of referenced asset ids, and the set of tag names seen.
func buildTagIndex(assignments map[string][]string) (map[string][]int64, map[int64]bool, map[string]struct{}) {
	idsByTag := map[string][]int64{}
	taggedIDs := map[int64]bool{}
	knownTags := map[string]struct{}{}
	for assetKey, tags := range assignments {
		assetID, err := strconv.ParseInt(assetKey, 10, 64)
		if err != nil || assetID <= 0 {
			continue
		}
		taggedIDs[assetID] = true
		for _, tag := range tags {
			idsByTag[tag] = append(idsByTag[tag], assetID)
			knownTags[tag] = struct{}{}
		}
	}
	for tag := range idsByTag {
		sort.Slice(idsByTag[tag], func(i, j int) bool { return idsByTag[tag][i] < idsByTag[tag][j] })
	}
	return idsByTag, taggedIDs, knownTags
}

// restrictIdsByTag keeps only the asset ids present in the file (those that
// resolved), dropping tags left with no ids.
func restrictIdsByTag(idsByTag map[string][]int64, present map[int64]bool) map[string][]int64 {
	out := map[string][]int64{}
	for tag, ids := range idsByTag {
		kept := make([]int64, 0, len(ids))
		for _, id := range ids {
			if present[id] {
				kept = append(kept, id)
			}
		}
		if len(kept) > 0 {
			out[tag] = kept
		}
	}
	return out
}

// resolveTaggedResults extracts the file's asset references and resolves only
// the tagged ones (with preview bytes) so the PDF can show thumbnails. Reuses
// the shared resolve path so behavior matches the Asset Breakdown scan.
func (s *TagService) resolveTaggedResults(filePath string, taggedIDs map[int64]bool) ([]loader.ScanResult, error) {
	hits, err := loader.ScanRBXLFileForAssetIDs(filePath, 0, nil)
	if err != nil {
		return nil, err
	}
	filtered := make([]loader.ScanHit, 0, len(taggedIDs))
	for _, hit := range hits {
		if taggedIDs[hit.AssetID] {
			filtered = append(filtered, hit)
		}
	}
	resolved := resolveScanResults(s.ctx, filtered, "tag-report:progress")
	results := make([]loader.ScanResult, 0, len(resolved))
	for _, result := range resolved {
		results = append(results, result)
	}
	return results, nil
}

func tagReportBaseName(filePath string) string {
	base := filepath.Base(filePath)
	if ext := filepath.Ext(base); ext != "" {
		base = strings.TrimSuffix(base, ext)
	}
	base = strings.TrimSpace(base)
	if base == "" {
		base = "joxblox"
	}
	return base + "-tags"
}
