package diff

import (
	"encoding/json"
	"fmt"
	"image/color"
	"sort"
	"strings"

	"joxblox/internal/extractor"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// largeStringThreshold is the byte length at which we switch to a
// summary rendering instead of dumping the body inline.
const largeStringThreshold = 200

// DiffPropertyTable is the right-hand pane. It swaps between three
// table layouts depending on the row's status.
type DiffPropertyTable struct {
	container *fyne.Container
	scroll    fyne.CanvasObject
	header    *widget.Label
	// nameA / nameB are the basenames of the two compared files, used as
	// column headers. Empty until SetFileNames is called; we fall back to
	// "A" / "B" so the table still renders before the first Compare.
	nameA string
	nameB string
}

// SetFileNames updates the labels used in column headers and status
// captions. Pass the basenames of the two compared rbxl files.
func (t *DiffPropertyTable) SetFileNames(a, b string) {
	t.nameA = a
	t.nameB = b
}

func (t *DiffPropertyTable) labelA() string {
	if t.nameA == "" {
		return "A"
	}
	return t.nameA
}

func (t *DiffPropertyTable) labelB() string {
	if t.nameB == "" {
		return "B"
	}
	return t.nameB
}

// NewDiffPropertyTable builds the empty property panel. The parent tab
// calls Show*/Clear to populate it.
func NewDiffPropertyTable() *DiffPropertyTable {
	header := widget.NewLabel("Select a row to view properties.")
	header.TextStyle = fyne.TextStyle{Bold: true}
	// Truncate the header so a long instance path doesn't blow up the
	// pane's MinSize.Width and lock the HSplit divider.
	header.Truncation = fyne.TextTruncateEllipsis
	body := container.NewVBox()
	table := &DiffPropertyTable{
		container: body,
		header:    header,
	}
	// container.NewScroll (vs NewVScroll) explicitly decouples the
	// scroll's MinSize.Width from the content — long property values
	// scroll horizontally instead of forcing the right pane wider.
	bodyScroll := container.NewScroll(body)
	table.scroll = container.NewBorder(header, nil, nil, nil, bodyScroll)
	return table
}

// CanvasObject returns the scrolling container.
func (t *DiffPropertyTable) CanvasObject() fyne.CanvasObject {
	return t.scroll
}

// Clear resets the table to the empty placeholder.
func (t *DiffPropertyTable) Clear() {
	t.header.SetText("Select a row to view properties.")
	t.container.Objects = nil
	t.container.Refresh()
}

// ShowRow dispatches to the correct renderer based on row status.
func (t *DiffPropertyTable) ShowRow(row DiffRow) {
	switch row.Status {
	case DiffRowAdded:
		if row.AddedRef != nil {
			t.showSingleSided(row, "Added", row.AddedRef.Properties)
		}
	case DiffRowRemoved:
		if row.RemovedRef != nil {
			t.showSingleSided(row, "Removed", row.RemovedRef.Properties)
		}
	case DiffRowChanged:
		if row.ChangedRef != nil {
			t.showChanged(row, row.ChangedRef.PropertyChanges)
		}
	}
}

func (t *DiffPropertyTable) showSingleSided(row DiffRow, label string, properties map[string]extractor.DiffPropValue) {
	// For "Added" the value side is fileB; for "Removed" it's fileA.
	valueLabel := t.labelB()
	if label == "Removed" {
		valueLabel = t.labelA()
	}
	t.header.SetText(fmt.Sprintf("%s in %s: %s  (%s)", label, valueLabel, row.Path, row.Class))

	rows := []fyne.CanvasObject{}
	rows = append(rows, twoColumnHeader("Property", valueLabel))

	names := make([]string, 0, len(properties))
	for name := range properties {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		value := properties[name]
		if attrs, ok := parseAttributesMap(value); ok {
			// Expand each attribute into its own row so they're scannable
			// in the table instead of one JSON blob.
			attrNames := sortedKeys(attrs)
			for _, attrName := range attrNames {
				rows = append(rows, propertyRowTwoColumn(attributeRowLabel(attrName), renderValueSingle(attrs[attrName])))
				rows = append(rows, widget.NewSeparator())
			}
			if len(attrNames) == 0 {
				rows = append(rows, propertyRowTwoColumn(name, widget.NewLabel("(no attributes)")))
				rows = append(rows, widget.NewSeparator())
			}
			continue
		}
		rows = append(rows, propertyRowTwoColumn(name, renderValueSingle(value)))
		rows = append(rows, widget.NewSeparator())
	}
	if len(names) == 0 {
		rows = append(rows, widget.NewLabel("(no properties)"))
	}
	t.container.Objects = rows
	t.container.Refresh()
}

// propertyRowTwoColumn builds one row of the single-sided property
// table — a fixed-width monospace name on the left, the rendered value
// taking the rest of the row.
func propertyRowTwoColumn(name string, value fyne.CanvasObject) fyne.CanvasObject {
	nameLabel := widget.NewLabel(name)
	nameLabel.TextStyle = fyne.TextStyle{Monospace: true}
	return container.NewBorder(nil, nil, fixedWidth(nameLabel, 220), nil, value)
}

func attributeRowLabel(attrName string) string {
	return "Attribute:" + attrName
}

func sortedKeys(m map[string]extractor.DiffPropValue) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// parseAttributesMap recognises Attributes-typed values and unmarshals
// the inner map of {name -> DiffPropValue}. Returns false when the value
// isn't an Attributes payload (so callers can fall back to default
// rendering).
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

func (t *DiffPropertyTable) showChanged(row DiffRow, changes []extractor.DiffPropertyChange) {
	t.header.SetText(fmt.Sprintf("Changed: %s  (%s)", row.Path, row.Class))

	rows := []fyne.CanvasObject{}
	rows = append(rows, threeColumnHeader("Property", t.labelA(), t.labelB()))

	sortedChanges := append([]extractor.DiffPropertyChange(nil), changes...)
	sort.SliceStable(sortedChanges, func(i, j int) bool {
		return sortedChanges[i].Name < sortedChanges[j].Name
	})
	for _, change := range sortedChanges {
		if change.Type == "Attributes" {
			// Expand each per-attribute change into its own row so the
			// user sees Attribute:Name in the leftmost column instead of
			// a JSON map blob in the value cells.
			attrRows := buildAttributeChangeRows(change)
			rows = append(rows, attrRows...)
			continue
		}
		rows = append(rows, propertyRowChanged(change.Name, change))
		rows = append(rows, widget.NewSeparator())
	}
	if len(sortedChanges) == 0 {
		rows = append(rows, widget.NewLabel("(no property changes)"))
	}
	t.container.Objects = rows
	t.container.Refresh()
}

// propertyRowChanged builds one row of the three-column changed table.
// The yellow strip on the left mirrors the badge colour used for
// "Changed" rows in the left-pane tree.
func propertyRowChanged(name string, change extractor.DiffPropertyChange) fyne.CanvasObject {
	nameLabel := widget.NewLabel(name)
	nameLabel.TextStyle = fyne.TextStyle{Monospace: true}
	aWidget := renderValueChangedSide(change.A, change.B, true)
	bWidget := renderValueChangedSide(change.B, change.A, false)
	rowBody := container.NewGridWithColumns(3,
		fixedWidth(nameLabel, 220),
		aWidget,
		bWidget,
	)
	highlight := canvas.NewRectangle(color.NRGBA{R: 0xCE, G: 0xA0, B: 0x2A, A: 0x40})
	highlight.SetMinSize(fyne.NewSize(3, 1))
	return container.NewBorder(nil, nil, highlight, nil, rowBody)
}

// buildAttributeChangeRows decomposes one PropertyChange of type
// "Attributes" into one row per differing attribute. Equal attributes
// are filtered out — the parent change only fires when at least one
// per-key difference exists, but the diff might still include keys that
// happen to match. Returns rows interleaved with separators, ready to
// append to the table.
func buildAttributeChangeRows(change extractor.DiffPropertyChange) []fyne.CanvasObject {
	mapA, _ := parseAttributesMap(change.A)
	mapB, _ := parseAttributesMap(change.B)

	keys := make(map[string]struct{}, len(mapA)+len(mapB))
	for k := range mapA {
		keys[k] = struct{}{}
	}
	for k := range mapB {
		keys[k] = struct{}{}
	}
	sortedKeyList := make([]string, 0, len(keys))
	for k := range keys {
		sortedKeyList = append(sortedKeyList, k)
	}
	sort.Strings(sortedKeyList)

	out := make([]fyne.CanvasObject, 0, len(sortedKeyList)*2)
	for _, key := range sortedKeyList {
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
		change := extractor.DiffPropertyChange{
			Name: attributeRowLabel(key),
			Type: aValue.Type,
			A:    aValue,
			B:    bValue,
		}
		out = append(out, propertyRowChanged(attributeRowLabel(key), change))
		out = append(out, widget.NewSeparator())
	}
	return out
}

// attributeValuesEqual is a lightweight value-equality check used to
// skip attributes that happen to match across the two files. It does
// not need to match the Rust side's epsilon comparison exactly — the
// goal is just to suppress unchanged rows in a per-attribute table.
func attributeValuesEqual(a, b extractor.DiffPropValue) bool {
	if a.Type != b.Type {
		return false
	}
	return string(a.Value) == string(b.Value)
}

func twoColumnHeader(a, b string) fyne.CanvasObject {
	aLabel := widget.NewLabelWithStyle(a, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	bLabel := widget.NewLabelWithStyle(b, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	return container.NewBorder(nil, nil, fixedWidth(aLabel, 220), nil, bLabel)
}

func threeColumnHeader(a, b, c string) fyne.CanvasObject {
	return container.NewGridWithColumns(3,
		widget.NewLabelWithStyle(a, fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewLabelWithStyle(b, fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewLabelWithStyle(c, fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
	)
}

func fixedWidth(content fyne.CanvasObject, width float32) fyne.CanvasObject {
	spacer := canvas.NewRectangle(color.Transparent)
	spacer.SetMinSize(fyne.NewSize(width, 1))
	return container.NewStack(spacer, content)
}

// renderValueSingle renders one value where there is no counterpart
// (added or removed rows).
func renderValueSingle(value extractor.DiffPropValue) fyne.CanvasObject {
	return renderValue(value, nil)
}

// renderValueChangedSide renders one side of a property_changes entry.
// `other` is the counterpart side (used for "N -> M chars" hints on
// large strings). `isASide` is unused for now but kept to make future
// asymmetric rendering easy.
func renderValueChangedSide(value extractor.DiffPropValue, other extractor.DiffPropValue, isASide bool) fyne.CanvasObject {
	_ = isASide
	return renderValue(value, &other)
}

// renderValue is the type-aware renderer shared by single-sided and
// changed-side displays.
func renderValue(value extractor.DiffPropValue, other *extractor.DiffPropValue) fyne.CanvasObject {
	switch value.Type {
	case "Absent":
		label := widget.NewLabel("(absent)")
		label.TextStyle = fyne.TextStyle{Italic: true}
		return label
	case "Color3":
		return renderColor(value.Value, false)
	case "Color3uint8":
		return renderColor(value.Value, true)
	case "Vector3":
		return renderVector3(value.Value)
	case "Vector2":
		return renderVector2(value.Value)
	case "Vector3int16":
		return renderVector3(value.Value)
	case "CFrame":
		return renderCFrame(value.Value)
	case "OptionalCFrame":
		return renderCFrame(value.Value)
	case "Content", "ContentId":
		return renderContent(value.Value)
	case "String":
		return renderString(value.Value, other)
	case "BinaryString", "SharedString":
		return renderOpaqueString(value.Value, other)
	default:
		return renderDefault(value.Value)
	}
}

func renderColor(raw json.RawMessage, isUint8 bool) fyne.CanvasObject {
	var components []float64
	if err := json.Unmarshal(raw, &components); err != nil || len(components) < 3 {
		return renderDefault(raw)
	}
	var r, g, b uint8
	if isUint8 {
		r = clampUint8(components[0])
		g = clampUint8(components[1])
		b = clampUint8(components[2])
	} else {
		r = clampUint8(components[0] * 255)
		g = clampUint8(components[1] * 255)
		b = clampUint8(components[2] * 255)
	}
	swatch := canvas.NewRectangle(color.NRGBA{R: r, G: g, B: b, A: 0xFF})
	swatch.SetMinSize(fyne.NewSize(20, 20))
	hex := fmt.Sprintf("#%02X%02X%02X", r, g, b)
	label := widget.NewLabel(hex)
	label.TextStyle = fyne.TextStyle{Monospace: true}
	return container.NewHBox(swatch, label)
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

func renderVector3(raw json.RawMessage) fyne.CanvasObject {
	var components []float64
	if err := json.Unmarshal(raw, &components); err != nil || len(components) < 3 {
		return renderDefault(raw)
	}
	label := widget.NewLabel(fmt.Sprintf("x=%g  y=%g  z=%g", components[0], components[1], components[2]))
	label.TextStyle = fyne.TextStyle{Monospace: true}
	return label
}

func renderVector2(raw json.RawMessage) fyne.CanvasObject {
	var components []float64
	if err := json.Unmarshal(raw, &components); err != nil || len(components) < 2 {
		return renderDefault(raw)
	}
	label := widget.NewLabel(fmt.Sprintf("x=%g  y=%g", components[0], components[1]))
	label.TextStyle = fyne.TextStyle{Monospace: true}
	return label
}

// renderCFrame tries two shapes: an array of 12 floats
// [px,py,pz, r00..r22] or a struct with `position` + `rotation`. Both
// are common Rust serialisations.
func renderCFrame(raw json.RawMessage) fyne.CanvasObject {
	var components []float64
	if err := json.Unmarshal(raw, &components); err == nil && len(components) >= 3 {
		positionText := fmt.Sprintf("pos: x=%g  y=%g  z=%g", components[0], components[1], components[2])
		if len(components) >= 12 {
			rotationText := fmt.Sprintf(
				"\nrot: [%g %g %g | %g %g %g | %g %g %g]",
				components[3], components[4], components[5],
				components[6], components[7], components[8],
				components[9], components[10], components[11],
			)
			label := widget.NewLabel(positionText + rotationText)
			label.TextStyle = fyne.TextStyle{Monospace: true}
			return label
		}
		label := widget.NewLabel(positionText)
		label.TextStyle = fyne.TextStyle{Monospace: true}
		return label
	}
	return renderDefault(raw)
}

func renderContent(raw json.RawMessage) fyne.CanvasObject {
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		// rbxthumb:// URIs MUST be preserved verbatim — they encode the
		// asset thumbnail variant and stripping the prefix changes which
		// asset endpoint the loader uses. See CLAUDE.md.
		label := widget.NewLabel(asString)
		label.Wrapping = fyne.TextWrapBreak
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(asString)), "rbxthumb://") {
			label.TextStyle = fyne.TextStyle{Monospace: true}
		}
		return label
	}
	return renderDefault(raw)
}

func renderString(raw json.RawMessage, other *extractor.DiffPropValue) fyne.CanvasObject {
	var asString string
	if err := json.Unmarshal(raw, &asString); err != nil {
		return renderDefault(raw)
	}
	if len(asString) <= largeStringThreshold {
		return widget.NewLabel(asString)
	}
	if other != nil {
		var otherString string
		if err := json.Unmarshal(other.Value, &otherString); err == nil {
			return widget.NewLabel(fmt.Sprintf("<changed, %d→%d chars>", len(asString), len(otherString)))
		}
	}
	return widget.NewLabel(fmt.Sprintf("<%d chars>", len(asString)))
}

// renderOpaqueString handles BinaryString / SharedString. The Rust side
// has already hashed these so the raw JSON is normally a short hash
// string; we just show it (or a "N → M" delta when both sides exist).
func renderOpaqueString(raw json.RawMessage, other *extractor.DiffPropValue) fyne.CanvasObject {
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		if other != nil {
			var otherString string
			if err := json.Unmarshal(other.Value, &otherString); err == nil && otherString != asString {
				label := widget.NewLabel(fmt.Sprintf("<hash %s → %s>", short(asString), short(otherString)))
				label.TextStyle = fyne.TextStyle{Monospace: true}
				return label
			}
		}
		label := widget.NewLabel(fmt.Sprintf("<%s>", asString))
		label.TextStyle = fyne.TextStyle{Monospace: true}
		return label
	}
	return renderDefault(raw)
}

func short(value string) string {
	const limit = 12
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "..."
}

func renderDefault(raw json.RawMessage) fyne.CanvasObject {
	text := strings.TrimSpace(string(raw))
	if text == "" {
		text = "(empty)"
	}
	label := widget.NewLabel(text)
	label.Wrapping = fyne.TextWrapBreak
	return label
}
