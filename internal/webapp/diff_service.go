package webapp

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"joxblox/internal/clipboard"
	"joxblox/internal/extractor"

	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// largeStringThreshold is the byte length at which a string property is
// summarised ("<N chars>") instead of dumped inline. Mirrors the Fyne diff
// tab's behaviour so both UIs read identically.
const largeStringThreshold = 200

// DiffValue is one type-aware, pre-formatted property value. Display is the
// human-readable string the frontend shows verbatim; ColorHex is set ("#RRGGBB")
// only for colour-typed values so the UI can draw a swatch; Absent marks a value
// that exists on only one side of a change.
type DiffValue struct {
	Type     string `json:"type"`
	Display  string `json:"display"`
	ColorHex string `json:"colorHex"`
	Absent   bool   `json:"absent"`
}

// DiffProperty is one row of a diff entry's property table. For added/removed
// entries only A carries a value; for changed entries both A (fileA) and B
// (fileB) are populated.
type DiffProperty struct {
	Name string    `json:"name"`
	A    DiffValue `json:"a"`
	B    DiffValue `json:"b"`
}

// DiffEntry is a single instance that differs between the two files. Status is
// "added", "removed" or "changed". Path is the dot-separated instance path the
// frontend builds its tree from.
type DiffEntry struct {
	Status     string         `json:"status"`
	Path       string         `json:"path"`
	Class      string         `json:"class"`
	Name       string         `json:"name"`
	Properties []DiffProperty `json:"properties"`
}

// DiffResultPayload is the serialisable result of a Compare: the two file
// basenames (used as column headers), per-bucket counts for the summary line,
// and the flat list of differing instances.
type DiffResultPayload struct {
	FileA   string      `json:"fileA"`
	FileB   string      `json:"fileB"`
	Added   int         `json:"added"`
	Removed int         `json:"removed"`
	Changed int         `json:"changed"`
	Entries []DiffEntry `json:"entries"`
}

// DiffService compares two .rbxl/.rbxm files for the web Diff tab. It owns a
// single live extractor.DiffSession (the Rust process holding both DOMs in
// memory) so right-click copies reuse the parsed trees instead of re-parsing.
type DiffService struct {
	ctx context.Context

	mu      sync.Mutex
	session *extractor.DiffSession
	nameA   string
	nameB   string
}

func NewDiffService() *DiffService { return &DiffService{} }

func (s *DiffService) startup(ctx context.Context) { s.ctx = ctx }

// shutdown tears down any live session so the Rust child doesn't outlive the
// app. Called from App's OnShutdown hook.
func (s *DiffService) shutdown() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.session != nil {
		s.session.Close()
		s.session = nil
	}
}

// PickFile opens a native file picker for a place/model file and returns the
// chosen path ("" if cancelled). The Diff tab picks two files independently of
// the global "current file".
func (s *DiffService) PickFile() (string, error) {
	return wailsRuntime.OpenFileDialog(s.ctx, wailsRuntime.OpenDialogOptions{
		Title: "Select a Roblox place or model file",
		Filters: []wailsRuntime.FileFilter{
			{DisplayName: "Roblox place/model files", Pattern: "*.rbxl;*.rbxm"},
		},
	})
}

// Compare diffs fileA against fileB, replacing any previous session. The old
// session (which may hold gigabytes of parsed DOMs) is torn down first so we
// don't hold two in memory at once. The returned payload carries pre-formatted
// values ready for display.
func (s *DiffService) Compare(fileA, fileB string, ignoreScripts bool) (DiffResultPayload, error) {
	if strings.TrimSpace(fileA) == "" || strings.TrimSpace(fileB) == "" {
		return DiffResultPayload{}, fmt.Errorf("Select both File A and File B first.")
	}

	// Drop the previous session before allocating the next one's DOMs.
	s.mu.Lock()
	if s.session != nil {
		s.session.Close()
		s.session = nil
	}
	s.mu.Unlock()

	session, err := extractor.StartDiffSession(fileA, fileB, ignoreScripts)
	if err != nil {
		return DiffResultPayload{}, err
	}

	nameA := filepath.Base(fileA)
	nameB := filepath.Base(fileB)

	s.mu.Lock()
	s.session = session
	s.nameA = nameA
	s.nameB = nameB
	s.mu.Unlock()

	result := session.Result()
	return buildDiffPayload(result, nameA, nameB), nil
}

// CopyInstance serialises the instance subtree at path (from side "a" or "b")
// to .rbxm bytes and places them on the system clipboard in Roblox Studio's
// format. Errors propagate to the frontend (clipboard paste is Windows-only).
func (s *DiffService) CopyInstance(side, path string) error {
	s.mu.Lock()
	session := s.session
	s.mu.Unlock()
	if session == nil {
		return fmt.Errorf("no diff loaded")
	}
	payload, err := session.CopyInstance(side, path)
	if err != nil {
		return err
	}
	return clipboard.SetRobloxModel(payload)
}

// buildDiffPayload converts the raw extractor result into the flat, pre-formatted
// payload the frontend consumes. Property values are sorted and type-formatted
// here so the React layer stays presentation-only.
func buildDiffPayload(result extractor.DiffResult, nameA, nameB string) DiffResultPayload {
	entries := make([]DiffEntry, 0, len(result.Added)+len(result.Removed)+len(result.Changed))

	for i := range result.Added {
		instance := &result.Added[i]
		entries = append(entries, DiffEntry{
			Status:     "added",
			Path:       instance.Path,
			Class:      instance.Class,
			Name:       instance.Name,
			Properties: buildSingleSidedProperties(instance.Properties),
		})
	}
	for i := range result.Removed {
		instance := &result.Removed[i]
		entries = append(entries, DiffEntry{
			Status:     "removed",
			Path:       instance.Path,
			Class:      instance.Class,
			Name:       instance.Name,
			Properties: buildSingleSidedProperties(instance.Properties),
		})
	}
	for i := range result.Changed {
		instance := &result.Changed[i]
		entries = append(entries, DiffEntry{
			Status:     "changed",
			Path:       instance.Path,
			Class:      instance.Class,
			Name:       instance.Name,
			Properties: buildChangedProperties(instance.PropertyChanges),
		})
	}

	// Stable order by path (then status) mirrors the Fyne BuildDiffRows order.
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].Path != entries[j].Path {
			return entries[i].Path < entries[j].Path
		}
		return entries[i].Status < entries[j].Status
	})

	return DiffResultPayload{
		FileA:   nameA,
		FileB:   nameB,
		Added:   len(result.Added),
		Removed: len(result.Removed),
		Changed: len(result.Changed),
		Entries: entries,
	}
}

// buildSingleSidedProperties formats every property of an added/removed
// instance into a single-value row, expanding Attributes maps into one row per
// attribute so they're scannable rather than a JSON blob.
func buildSingleSidedProperties(properties map[string]extractor.DiffPropValue) []DiffProperty {
	rows := make([]DiffProperty, 0, len(properties))
	for _, name := range sortedPropNames(properties) {
		value := properties[name]
		if attrs, ok := parseAttributesMap(value); ok {
			attrNames := sortedPropNames(attrs)
			for _, attrName := range attrNames {
				rows = append(rows, DiffProperty{
					Name: attributeRowLabel(attrName),
					A:    formatDiffValue(attrs[attrName], nil),
				})
			}
			if len(attrNames) == 0 {
				rows = append(rows, DiffProperty{Name: name, A: DiffValue{Display: "(no attributes)"}})
			}
			continue
		}
		rows = append(rows, DiffProperty{Name: name, A: formatDiffValue(value, nil)})
	}
	return rows
}

// buildChangedProperties formats the differing properties of a changed
// instance into two-value rows, expanding Attributes changes per-key.
func buildChangedProperties(changes []extractor.DiffPropertyChange) []DiffProperty {
	sorted := append([]extractor.DiffPropertyChange(nil), changes...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })

	rows := make([]DiffProperty, 0, len(sorted))
	for _, change := range sorted {
		if change.Type == "Attributes" {
			rows = append(rows, buildAttributeChangeProperties(change)...)
			continue
		}
		rows = append(rows, DiffProperty{
			Name: change.Name,
			A:    formatDiffValue(change.A, &change.B),
			B:    formatDiffValue(change.B, &change.A),
		})
	}
	return rows
}

// buildAttributeChangeProperties decomposes one Attributes property change into
// one row per differing attribute, filtering out attributes that happen to be
// equal across both files.
func buildAttributeChangeProperties(change extractor.DiffPropertyChange) []DiffProperty {
	mapA, _ := parseAttributesMap(change.A)
	mapB, _ := parseAttributesMap(change.B)

	keys := make(map[string]struct{}, len(mapA)+len(mapB))
	for k := range mapA {
		keys[k] = struct{}{}
	}
	for k := range mapB {
		keys[k] = struct{}{}
	}
	keyList := make([]string, 0, len(keys))
	for k := range keys {
		keyList = append(keyList, k)
	}
	sort.Strings(keyList)

	rows := make([]DiffProperty, 0, len(keyList))
	for _, key := range keyList {
		aValue, hasA := mapA[key]
		bValue, hasB := mapB[key]
		if hasA && hasB && attributeValuesEqual(aValue, bValue) {
			continue
		}
		if !hasA {
			aValue = extractor.DiffPropValue{Type: "Absent"}
		}
		if !hasB {
			bValue = extractor.DiffPropValue{Type: "Absent"}
		}
		rows = append(rows, DiffProperty{
			Name: attributeRowLabel(key),
			A:    formatDiffValue(aValue, &bValue),
			B:    formatDiffValue(bValue, &aValue),
		})
	}
	return rows
}

func attributeRowLabel(name string) string { return "Attribute:" + name }

// attributeValuesEqual is a lightweight equality check to suppress unchanged
// attribute rows. It doesn't need to match the Rust side's epsilon comparison.
func attributeValuesEqual(a, b extractor.DiffPropValue) bool {
	if a.Type != b.Type {
		return false
	}
	return string(a.Value) == string(b.Value)
}

func parseAttributesMap(value extractor.DiffPropValue) (map[string]extractor.DiffPropValue, bool) {
	if value.Type != "Attributes" {
		return nil, false
	}
	var attrs map[string]extractor.DiffPropValue
	if err := json.Unmarshal(value.Value, &attrs); err != nil {
		return nil, false
	}
	return attrs, true
}

func sortedPropNames(m map[string]extractor.DiffPropValue) []string {
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// formatDiffValue is the type-aware formatter shared by single-sided and
// changed displays, ported from the Fyne diff property table. other is the
// counterpart side (used for "N→M chars" hints on large strings); nil for
// single-sided values.
func formatDiffValue(value extractor.DiffPropValue, other *extractor.DiffPropValue) DiffValue {
	switch value.Type {
	case "Absent":
		return DiffValue{Type: value.Type, Display: "(absent)", Absent: true}
	case "Color3":
		return formatColor(value, false)
	case "Color3uint8":
		return formatColor(value, true)
	case "Vector3", "Vector3int16":
		return DiffValue{Type: value.Type, Display: formatVector(value.Value, 3)}
	case "Vector2":
		return DiffValue{Type: value.Type, Display: formatVector(value.Value, 2)}
	case "CFrame", "OptionalCFrame":
		return DiffValue{Type: value.Type, Display: formatCFrame(value.Value)}
	case "Content", "ContentId":
		return DiffValue{Type: value.Type, Display: formatContent(value.Value)}
	case "String":
		return DiffValue{Type: value.Type, Display: formatString(value.Value, other)}
	case "BinaryString", "SharedString":
		return DiffValue{Type: value.Type, Display: formatOpaqueString(value.Value, other)}
	default:
		return DiffValue{Type: value.Type, Display: formatDefault(value.Value)}
	}
}

func formatColor(value extractor.DiffPropValue, isUint8 bool) DiffValue {
	var components []float64
	if err := json.Unmarshal(value.Value, &components); err != nil || len(components) < 3 {
		return DiffValue{Type: value.Type, Display: formatDefault(value.Value)}
	}
	var r, g, b uint8
	if isUint8 {
		r, g, b = clampUint8(components[0]), clampUint8(components[1]), clampUint8(components[2])
	} else {
		r, g, b = clampUint8(components[0]*255), clampUint8(components[1]*255), clampUint8(components[2]*255)
	}
	hex := fmt.Sprintf("#%02X%02X%02X", r, g, b)
	return DiffValue{Type: value.Type, Display: hex, ColorHex: hex}
}

func clampUint8(value float64) uint8 {
	if value < 0 {
		return 0
	}
	if value > 255 {
		return 255
	}
	return uint8(value)
}

func formatVector(raw json.RawMessage, want int) string {
	var components []float64
	if err := json.Unmarshal(raw, &components); err != nil || len(components) < want {
		return formatDefault(raw)
	}
	if want == 2 {
		return fmt.Sprintf("x=%g  y=%g", components[0], components[1])
	}
	return fmt.Sprintf("x=%g  y=%g  z=%g", components[0], components[1], components[2])
}

// formatCFrame accepts an array of floats: 3 (position only), 6, or 12
// (position + 3x3 rotation), matching common Rust serialisations.
func formatCFrame(raw json.RawMessage) string {
	var components []float64
	if err := json.Unmarshal(raw, &components); err != nil || len(components) < 3 {
		return formatDefault(raw)
	}
	position := fmt.Sprintf("pos: x=%g  y=%g  z=%g", components[0], components[1], components[2])
	if len(components) >= 12 {
		rotation := fmt.Sprintf(
			"\nrot: [%g %g %g | %g %g %g | %g %g %g]",
			components[3], components[4], components[5],
			components[6], components[7], components[8],
			components[9], components[10], components[11],
		)
		return position + rotation
	}
	return position
}

func formatContent(raw json.RawMessage) string {
	// rbxthumb:// URIs MUST be preserved verbatim — see CLAUDE.md.
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		return asString
	}
	return formatDefault(raw)
}

func formatString(raw json.RawMessage, other *extractor.DiffPropValue) string {
	var asString string
	if err := json.Unmarshal(raw, &asString); err != nil {
		return formatDefault(raw)
	}
	if len(asString) <= largeStringThreshold {
		return asString
	}
	if other != nil {
		var otherString string
		if err := json.Unmarshal(other.Value, &otherString); err == nil {
			return fmt.Sprintf("<changed, %d→%d chars>", len(asString), len(otherString))
		}
	}
	return fmt.Sprintf("<%d chars>", len(asString))
}

// formatOpaqueString handles BinaryString / SharedString. The Rust side hashes
// these, so the raw JSON is normally a short hash string we show as-is (or a
// "N → M" delta when both sides differ).
func formatOpaqueString(raw json.RawMessage, other *extractor.DiffPropValue) string {
	var asString string
	if err := json.Unmarshal(raw, &asString); err != nil {
		return formatDefault(raw)
	}
	if other != nil {
		var otherString string
		if err := json.Unmarshal(other.Value, &otherString); err == nil && otherString != asString {
			return fmt.Sprintf("<hash %s → %s>", shortHash(asString), shortHash(otherString))
		}
	}
	return fmt.Sprintf("<%s>", asString)
}

func shortHash(value string) string {
	const limit = 12
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "..."
}

func formatDefault(raw json.RawMessage) string {
	text := strings.TrimSpace(string(raw))
	if text == "" {
		return "(empty)"
	}
	return text
}
