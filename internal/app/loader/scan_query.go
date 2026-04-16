package loader

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"joxblox/internal/format"
)

type scanQueryFieldKind int

const (
	scanQueryFieldKindText scanQueryFieldKind = iota
	scanQueryFieldKindNumeric
	scanQueryFieldKindSize
	scanQueryFieldKindBool
)

type scanQueryFieldSpec struct {
	canonical string
	kind      scanQueryFieldKind
	text      func(ScanResult) string
	numeric   func(ScanResult) float64
	boolean   func(ScanResult, ScanQueryContext) bool
}

type scanQueryOp int

const (
	scanQueryOpSubstring scanQueryOp = iota
	scanQueryOpRegex
	scanQueryOpEq
	scanQueryOpNeq
	scanQueryOpGt
	scanQueryOpGte
	scanQueryOpLt
	scanQueryOpLte
)

type scanQueryToken struct {
	negated  bool
	field    *scanQueryFieldSpec
	op       scanQueryOp
	text     string
	lowered  string
	numeric  float64
	regex    *regexp.Regexp
	boolWant bool
}

type ScanQuery struct {
	tokens []scanQueryToken
	raw    string
}

type ScanQueryContext struct {
	HashCounts map[string]int
}

func (q ScanQuery) IsEmpty() bool {
	return len(q.tokens) == 0
}

var scanQueryFieldAliases = buildScanQueryFieldAliases()

func buildScanQueryFieldAliases() map[string]*scanQueryFieldSpec {
	textField := func(canonical string, getter func(ScanResult) string) *scanQueryFieldSpec {
		return &scanQueryFieldSpec{canonical: canonical, kind: scanQueryFieldKindText, text: getter}
	}
	numericField := func(canonical string, getter func(ScanResult) float64) *scanQueryFieldSpec {
		return &scanQueryFieldSpec{canonical: canonical, kind: scanQueryFieldKindNumeric, numeric: getter}
	}
	sizeField := func(canonical string, getter func(ScanResult) float64) *scanQueryFieldSpec {
		return &scanQueryFieldSpec{canonical: canonical, kind: scanQueryFieldKindSize, numeric: getter}
	}
	boolField := func(canonical string, getter func(ScanResult, ScanQueryContext) bool) *scanQueryFieldSpec {
		return &scanQueryFieldSpec{canonical: canonical, kind: scanQueryFieldKindBool, boolean: getter}
	}

	id := textField("id", func(r ScanResult) string { return strconv.FormatInt(r.AssetID, 10) })
	name := textField("name", func(r ScanResult) string { return r.AssetInput })
	typeName := textField("type", func(r ScanResult) string { return r.AssetTypeName })
	itype := textField("itype", func(r ScanResult) string { return r.InstanceType })
	iname := textField("iname", func(r ScanResult) string { return r.InstanceName })
	ipath := textField("ipath", func(r ScanResult) string { return r.InstancePath })
	prop := textField("prop", func(r ScanResult) string { return r.PropertyName })
	src := textField("src", func(r ScanResult) string { return r.Source })
	state := textField("state", func(r ScanResult) string { return r.State })
	pathField := textField("path", func(r ScanResult) string { return r.FilePath })
	sha := textField("sha", func(r ScanResult) string { return r.FileSHA256 })
	formatField := textField("format", func(r ScanResult) string { return r.Format })
	contentField := textField("content", func(r ScanResult) string { return r.ContentType })
	side := textField("side", func(r ScanResult) string { return r.Side })
	meshversion := textField("meshversion", func(r ScanResult) string { return r.MeshVersion })

	uses := numericField("uses", func(r ScanResult) float64 { return float64(r.UseCount) })
	size := sizeField("size", func(r ScanResult) float64 { return float64(r.BytesSize) })
	totalsize := sizeField("totalsize", func(r ScanResult) float64 { return float64(r.TotalBytesSize) })
	tex := sizeField("tex", func(r ScanResult) float64 { return float64(r.TextureBytes) })
	meshbytes := sizeField("meshbytes", func(r ScanResult) float64 { return float64(r.MeshBytes) })
	tris := numericField("tris", func(r ScanResult) float64 { return float64(r.MeshNumFaces) })
	verts := numericField("verts", func(r ScanResult) float64 { return float64(r.MeshNumVerts) })
	pixels := numericField("pixels", func(r ScanResult) float64 { return float64(r.PixelCount) })
	width := numericField("w", func(r ScanResult) float64 { return float64(r.Width) })
	height := numericField("h", func(r ScanResult) float64 { return float64(r.Height) })

	warn := boolField("warn", func(r ScanResult, _ ScanQueryContext) bool { return r.Warning })
	dup := boolField("dup", func(r ScanResult, ctx ScanQueryContext) bool {
		return IsDuplicateByHash(r, ctx.HashCounts)
	})

	aliases := map[string]*scanQueryFieldSpec{
		"id": id, "assetid": id,
		"name": name, "input": name, "assetname": name,
		"type": typeName, "assettype": typeName,
		"itype": itype, "instancetype": itype,
		"iname": iname, "instance": iname, "instancename": iname,
		"ipath": ipath, "instancepath": ipath,
		"prop": prop, "property": prop, "propertyname": prop,
		"src": src, "source": src,
		"state": state,
		"path":  pathField, "file": pathField, "filepath": pathField,
		"sha": sha, "hash": sha, "sha256": sha, "filesha256": sha,
		"format":  formatField,
		"content": contentField, "contenttype": contentField,
		"side":        side,
		"meshversion": meshversion, "mv": meshversion, "meshver": meshversion,
		"uses":  uses, "usecount": uses,
		"size": size, "bytes": size, "bytessize": size,
		"totalsize": totalsize,
		"tex":       tex, "texbytes": tex, "texturebytes": tex,
		"meshbytes": meshbytes,
		"tris":      tris, "triangles": tris, "meshfaces": tris, "faces": tris,
		"verts":  verts, "vertices": verts,
		"pixels": pixels, "pixelcount": pixels,
		"w": width, "width": width,
		"h": height, "height": height,
		"warn": warn, "warning": warn,
		"dup": dup, "duplicate": dup, "duplicates": dup,
	}
	// Existing aliases win on conflict so hand-rolled DSL behavior is preserved.
	for _, spec := range AssetMetadataSchema() {
		if spec.ScanExtract == nil {
			continue
		}
		field := scanQuerySpecFromMetadata(spec)
		if _, exists := aliases[spec.Key]; !exists {
			aliases[spec.Key] = field
		}
		for _, alias := range spec.Aliases {
			if _, exists := aliases[alias]; !exists {
				aliases[alias] = field
			}
		}
	}
	return aliases
}

// scanQuerySpecFromMetadata builds a scan-query field spec that delegates to
// a MetadataSpec's ScanExtract. Size/Number kinds become numeric fields; the
// rest are treated as text.
func scanQuerySpecFromMetadata(spec MetadataSpec) *scanQueryFieldSpec {
	kind := scanQueryFieldKindText
	switch spec.Kind {
	case MetadataKindSize:
		kind = scanQueryFieldKindSize
	case MetadataKindNumber:
		kind = scanQueryFieldKindNumeric
	}
	extract := spec.ScanExtract
	return &scanQueryFieldSpec{
		canonical: spec.Key,
		kind:      kind,
		text: func(r ScanResult) string {
			text, _, _ := extract(r)
			return text
		},
		numeric: func(r ScanResult) float64 {
			_, n, _ := extract(r)
			return n
		},
	}
}

// DistinctScanFieldValues returns up to `limit` distinct values observed for
// the given field alias across the provided results, ordered by frequency
// (descending) then lexically.
//
// Returns nil for unknown fields, numeric/size fields (where enumerating
// values isn't meaningful), or when the distinct count exceeds
// maxEnumerableCardinality (e.g. name, path — effectively unbounded).
// Boolean fields always return ["true", "false"].
func DistinctScanFieldValues(alias string, results []ScanResult, limit int) []string {
	const maxEnumerableCardinality = 300
	spec, ok := scanQueryFieldAliases[strings.ToLower(strings.TrimSpace(alias))]
	if !ok || spec == nil {
		return nil
	}
	switch spec.kind {
	case scanQueryFieldKindBool:
		return []string{"true", "false"}
	case scanQueryFieldKindNumeric, scanQueryFieldKindSize:
		return nil
	}
	if spec.text == nil {
		return nil
	}
	counts := map[string]int{}
	originals := map[string]string{}
	for i := range results {
		value := strings.TrimSpace(spec.text(results[i]))
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, exists := counts[key]; !exists {
			originals[key] = value
			if len(counts) >= maxEnumerableCardinality {
				return nil
			}
		}
		counts[key]++
	}
	type valueCount struct {
		value string
		count int
	}
	pairs := make([]valueCount, 0, len(counts))
	for key, count := range counts {
		pairs = append(pairs, valueCount{value: originals[key], count: count})
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].count != pairs[j].count {
			return pairs[i].count > pairs[j].count
		}
		return pairs[i].value < pairs[j].value
	})
	if limit > 0 && len(pairs) > limit {
		pairs = pairs[:limit]
	}
	values := make([]string, len(pairs))
	for i, p := range pairs {
		values[i] = p.value
	}
	return values
}

// ScanQueryFieldNames returns every canonical field alias sorted alphabetically.
// Used for autocomplete.
func ScanQueryFieldNames() []string {
	names := make([]string, 0, len(scanQueryFieldAliases))
	for alias := range scanQueryFieldAliases {
		names = append(names, alias)
	}
	sort.Strings(names)
	return names
}

func tokenizeScanQuery(raw string) []string {
	var tokens []string
	var current strings.Builder
	inQuote := false
	inRegex := false
	for i := 0; i < len(raw); i++ {
		character := raw[i]
		switch {
		case inQuote:
			if character == '"' {
				inQuote = false
			}
			current.WriteByte(character)
		case inRegex:
			current.WriteByte(character)
			if character == '/' && (i == 0 || raw[i-1] != '\\') {
				inRegex = false
			}
		case character == '"':
			inQuote = true
			current.WriteByte(character)
		case character == '/':
			// start a regex only if the preceding char was start-of-token or ':'
			precededByDelim := current.Len() == 0 || (current.Len() > 0 && current.String()[current.Len()-1] == ':')
			if precededByDelim {
				inRegex = true
			}
			current.WriteByte(character)
		case character == ' ' || character == '\t':
			if current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
		default:
			current.WriteByte(character)
		}
	}
	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}
	return tokens
}

func unquote(raw string) string {
	if len(raw) >= 2 && raw[0] == '"' && raw[len(raw)-1] == '"' {
		return raw[1 : len(raw)-1]
	}
	return raw
}

func parseOpPrefix(value string) (scanQueryOp, string, bool) {
	if strings.HasPrefix(value, ">=") {
		return scanQueryOpGte, value[2:], true
	}
	if strings.HasPrefix(value, "<=") {
		return scanQueryOpLte, value[2:], true
	}
	if strings.HasPrefix(value, "!=") {
		return scanQueryOpNeq, value[2:], true
	}
	if strings.HasPrefix(value, ">") {
		return scanQueryOpGt, value[1:], true
	}
	if strings.HasPrefix(value, "<") {
		return scanQueryOpLt, value[1:], true
	}
	if strings.HasPrefix(value, "=") {
		return scanQueryOpEq, value[1:], true
	}
	return scanQueryOpSubstring, value, false
}

func parseScanQueryToken(raw string) (scanQueryToken, error) {
	token := scanQueryToken{op: scanQueryOpSubstring}
	if strings.HasPrefix(raw, "-") && len(raw) > 1 {
		token.negated = true
		raw = raw[1:]
	}
	// Bare regex: /pattern/
	if len(raw) >= 2 && raw[0] == '/' && raw[len(raw)-1] == '/' {
		pattern := raw[1 : len(raw)-1]
		compiled, err := regexp.Compile("(?i)" + pattern)
		if err != nil {
			return token, fmt.Errorf("invalid regex %q: %w", pattern, err)
		}
		token.op = scanQueryOpRegex
		token.regex = compiled
		return token, nil
	}
	// field:value
	colonIndex := strings.Index(raw, ":")
	if colonIndex <= 0 {
		text := unquote(raw)
		token.text = text
		token.lowered = strings.ToLower(text)
		return token, nil
	}
	fieldName := strings.ToLower(raw[:colonIndex])
	value := raw[colonIndex+1:]
	spec, ok := scanQueryFieldAliases[fieldName]
	if !ok {
		// Unknown field — treat the whole thing as a bare substring.
		text := unquote(raw)
		token.text = text
		token.lowered = strings.ToLower(text)
		return token, nil
	}
	token.field = spec
	// Regex value: field:/pattern/
	if len(value) >= 2 && value[0] == '/' && value[len(value)-1] == '/' {
		pattern := value[1 : len(value)-1]
		compiled, err := regexp.Compile("(?i)" + pattern)
		if err != nil {
			return token, fmt.Errorf("invalid regex %q: %w", pattern, err)
		}
		token.op = scanQueryOpRegex
		token.regex = compiled
		return token, nil
	}
	// Numeric fields: parse op and number
	if spec.kind == scanQueryFieldKindNumeric || spec.kind == scanQueryFieldKindSize {
		op, rest, _ := parseOpPrefix(value)
		if op == scanQueryOpSubstring {
			op = scanQueryOpEq
		}
		rest = strings.TrimSpace(unquote(rest))
		var numericValue float64
		if spec.kind == scanQueryFieldKindSize {
			sizeBytes, parsed := format.ParseSizeBytes(rest)
			if !parsed {
				return token, fmt.Errorf("invalid size %q for %s", rest, spec.canonical)
			}
			numericValue = float64(sizeBytes)
		} else {
			parsed, err := strconv.ParseFloat(rest, 64)
			if err != nil {
				return token, fmt.Errorf("invalid number %q for %s", rest, spec.canonical)
			}
			numericValue = parsed
		}
		token.op = op
		token.numeric = numericValue
		return token, nil
	}
	if spec.kind == scanQueryFieldKindBool {
		lowered := strings.ToLower(strings.TrimSpace(unquote(value)))
		switch lowered {
		case "", "true", "t", "1", "yes", "y":
			token.boolWant = true
		case "false", "f", "0", "no", "n":
			token.boolWant = false
		default:
			return token, fmt.Errorf("invalid boolean %q for %s", lowered, spec.canonical)
		}
		token.op = scanQueryOpEq
		return token, nil
	}
	// Text field with substring
	text := unquote(value)
	token.text = text
	token.lowered = strings.ToLower(text)
	return token, nil
}

// CompileScanQuery parses a raw query string into a ScanQuery. On any parse
// error, it falls back to a single bare-substring token covering the full raw
// string so users never get a "bad syntax, no results" experience.
func CompileScanQuery(raw string) ScanQuery {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ScanQuery{raw: raw}
	}
	rawTokens := tokenizeScanQuery(trimmed)
	parsed := make([]scanQueryToken, 0, len(rawTokens))
	for _, rawToken := range rawTokens {
		token, err := parseScanQueryToken(rawToken)
		if err != nil {
			// Fallback: single bare substring covering the whole query.
			return ScanQuery{
				tokens: []scanQueryToken{{
					text:    trimmed,
					lowered: strings.ToLower(trimmed),
				}},
				raw: raw,
			}
		}
		parsed = append(parsed, token)
	}
	return ScanQuery{tokens: parsed, raw: raw}
}

var allSearchableTextFields = []*scanQueryFieldSpec{
	scanQueryFieldAliases["id"],
	scanQueryFieldAliases["name"],
	scanQueryFieldAliases["type"],
	scanQueryFieldAliases["itype"],
	scanQueryFieldAliases["iname"],
	scanQueryFieldAliases["ipath"],
	scanQueryFieldAliases["prop"],
	scanQueryFieldAliases["src"],
	scanQueryFieldAliases["state"],
	scanQueryFieldAliases["path"],
	scanQueryFieldAliases["sha"],
	scanQueryFieldAliases["format"],
	scanQueryFieldAliases["content"],
	scanQueryFieldAliases["side"],
}

func (token scanQueryToken) matches(result ScanResult, ctx ScanQueryContext) bool {
	matched := token.evaluate(result, ctx)
	if token.negated {
		return !matched
	}
	return matched
}

func (token scanQueryToken) evaluate(result ScanResult, ctx ScanQueryContext) bool {
	if token.field == nil {
		// Bare token: substring or regex across all text fields.
		if token.op == scanQueryOpRegex && token.regex != nil {
			for _, spec := range allSearchableTextFields {
				if token.regex.MatchString(spec.text(result)) {
					return true
				}
			}
			return false
		}
		if token.lowered == "" {
			return true
		}
		for _, spec := range allSearchableTextFields {
			if strings.Contains(strings.ToLower(spec.text(result)), token.lowered) {
				return true
			}
		}
		return false
	}
	switch token.field.kind {
	case scanQueryFieldKindText:
		fieldValue := token.field.text(result)
		if token.op == scanQueryOpRegex && token.regex != nil {
			return token.regex.MatchString(fieldValue)
		}
		return strings.Contains(strings.ToLower(fieldValue), token.lowered)
	case scanQueryFieldKindNumeric, scanQueryFieldKindSize:
		fieldValue := token.field.numeric(result)
		switch token.op {
		case scanQueryOpEq:
			return fieldValue == token.numeric
		case scanQueryOpNeq:
			return fieldValue != token.numeric
		case scanQueryOpGt:
			return fieldValue > token.numeric
		case scanQueryOpGte:
			return fieldValue >= token.numeric
		case scanQueryOpLt:
			return fieldValue < token.numeric
		case scanQueryOpLte:
			return fieldValue <= token.numeric
		}
		return false
	case scanQueryFieldKindBool:
		return token.field.boolean(result, ctx) == token.boolWant
	}
	return false
}

// Matches evaluates the compiled query against a single result.
func (q ScanQuery) Matches(result ScanResult, ctx ScanQueryContext) bool {
	for _, token := range q.tokens {
		if !token.matches(result, ctx) {
			return false
		}
	}
	return true
}
