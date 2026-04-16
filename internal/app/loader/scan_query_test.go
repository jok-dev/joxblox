package loader

import "testing"

func sample() ScanResult {
	return ScanResult{
		AssetID:       12345,
		AssetInput:    "Mesh v3.0 Blade",
		AssetTypeName: "Mesh",
		InstanceType:  "MeshPart",
		InstanceName:  "Sword",
		InstancePath:  "Workspace.Props.Sword",
		PropertyName:  "MeshId",
		Source:        "place",
		State:         "ok",
		FilePath:      "assets/sword_v3.0.rbxm",
		FileSHA256:    "ABCDEF",
		Format:        "mesh",
		ContentType:   "application/octet-stream",
		Side:          "left",
		UseCount:      4,
		BytesSize:     2 * 1024 * 1024,
		MeshNumFaces:  12000,
		Width:         512,
		Height:        512,
		Warning:       false,
	}
}

func match(t *testing.T, query string, want bool) {
	t.Helper()
	compiled := CompileScanQuery(query)
	got := compiled.Matches(sample(), ScanQueryContext{})
	if got != want {
		t.Fatalf("query %q: got %v, want %v", query, got, want)
	}
}

func TestEmptyQueryMatchesAll(t *testing.T) {
	match(t, "", true)
	match(t, "   ", true)
}

func TestBareSubstringParity(t *testing.T) {
	match(t, "v3.0", true)
	match(t, "V3.0", true)
	match(t, "nonexistent", false)
}

func TestFieldSubstring(t *testing.T) {
	match(t, "type:mesh", true)
	match(t, "type:image", false)
	match(t, "name:v3.0", true)
	match(t, "name:blade", true)
	match(t, "name:katana", false)
}

func TestImplicitAnd(t *testing.T) {
	match(t, "type:mesh v3.0", true)
	match(t, "type:mesh katana", false)
	match(t, "name:v3.0 name:blade", true)
}

func TestNumericComparison(t *testing.T) {
	match(t, "tris:>5000", true)
	match(t, "tris:>50000", false)
	match(t, "tris:>=12000", true)
	match(t, "tris:<=12000", true)
	match(t, "tris:=12000", true)
	match(t, "tris:!=12000", false)
	match(t, "uses:>3", true)
	match(t, "uses:<3", false)
}

func TestSizeUnits(t *testing.T) {
	match(t, "size:>1mb", true)
	match(t, "size:<1mb", false)
	match(t, "size:>=2mb", true)
	match(t, "size:<=2mb", true)
	match(t, "size:>500kb", true)
}

func TestRegexBare(t *testing.T) {
	match(t, `/v\d+\.\d+/`, true)
	match(t, `/v\d{5}/`, false)
}

func TestRegexField(t *testing.T) {
	match(t, `name:/v\d+\.\d+/`, true)
	match(t, `name:/katana/`, false)
}

func TestNegation(t *testing.T) {
	match(t, "-type:image", true)
	match(t, "-type:mesh", false)
	match(t, "-v3.0", false)
	match(t, "-katana", true)
}

func TestBooleanDerivedDuplicate(t *testing.T) {
	compiled := CompileScanQuery("dup:true")
	r := sample()
	r.FileSHA256 = "HASH1"
	ctxDup := ScanQueryContext{HashCounts: map[string]int{"hash1": 2}}
	ctxNoDup := ScanQueryContext{HashCounts: map[string]int{"hash1": 1}}
	if !compiled.Matches(r, ctxDup) {
		t.Fatalf("dup:true should match when hash count >= 2")
	}
	if compiled.Matches(r, ctxNoDup) {
		t.Fatalf("dup:true should not match when hash count < 2")
	}
	compiledFalse := CompileScanQuery("dup:false")
	if !compiledFalse.Matches(r, ctxNoDup) {
		t.Fatalf("dup:false should match non-duplicate")
	}
}

func TestBooleanWarning(t *testing.T) {
	r := sample()
	r.Warning = true
	compiled := CompileScanQuery("warn:true")
	if !compiled.Matches(r, ScanQueryContext{}) {
		t.Fatalf("warn:true should match Warning=true")
	}
	match(t, "warn:true", false) // sample() has Warning=false
	match(t, "warn:false", true)
}

func TestQuotedValue(t *testing.T) {
	match(t, `name:"Mesh v3.0"`, true)
	match(t, `name:"katana"`, false)
}

func TestUnknownFieldFallsBackToSubstring(t *testing.T) {
	// "zzzz:foo" isn't a known field; treat as bare substring.
	match(t, "zzzz:foo", false)
	// Works if the substring actually exists somewhere.
	r := sample()
	r.AssetInput = "zzzz:foo blah"
	compiled := CompileScanQuery("zzzz:foo")
	if !compiled.Matches(r, ScanQueryContext{}) {
		t.Fatalf("unknown field token should be searched as substring")
	}
}

func TestMalformedFallsBackToSubstring(t *testing.T) {
	// Bad size → fallback to treating whole query as substring; won't match.
	match(t, "size:>abc", false)
	// Bad regex → fallback, won't match either since raw text has no such chars.
	match(t, "/[/", false)
}

func TestAssetIDSearch(t *testing.T) {
	match(t, "id:12345", true)
	match(t, "12345", true)
	match(t, "id:99999", false)
}

func TestCaseInsensitive(t *testing.T) {
	match(t, "TYPE:MESH", true)
	match(t, "Name:BLADE", true)
}

func TestDistinctScanFieldValuesRanksByFrequency(t *testing.T) {
	results := []ScanResult{
		{MeshVersion: "7.00"},
		{MeshVersion: "7.00"},
		{MeshVersion: "3.00"},
		{MeshVersion: "4.00"},
		{MeshVersion: "7.00"},
		{MeshVersion: ""},
	}
	got := DistinctScanFieldValues("meshver", results, 10)
	want := []string{"7.00", "3.00", "4.00"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("index %d: got %q, want %q (full: %v)", i, got[i], want[i], got)
		}
	}
}

func TestDistinctScanFieldValuesBoolField(t *testing.T) {
	got := DistinctScanFieldValues("warn", nil, 10)
	if len(got) != 2 || got[0] != "true" || got[1] != "false" {
		t.Fatalf("bool field should suggest true/false, got %v", got)
	}
}

func TestDistinctScanFieldValuesNumericReturnsNil(t *testing.T) {
	if got := DistinctScanFieldValues("size", []ScanResult{{BytesSize: 1024}}, 10); got != nil {
		t.Fatalf("numeric/size fields should not enumerate values, got %v", got)
	}
}

func TestDistinctScanFieldValuesUnknownField(t *testing.T) {
	if got := DistinctScanFieldValues("zzz", []ScanResult{{}}, 10); got != nil {
		t.Fatalf("unknown field should return nil, got %v", got)
	}
}

func TestSchemaDerivedAliasUseCount(t *testing.T) {
	// "usecount" is defined on the metadata schema (not in the hand-rolled
	// alias list). Confirm the schema auto-registration makes it searchable.
	r := sample()
	r.UseCount = 7
	ctx := ScanQueryContext{}
	if !CompileScanQuery("usecount:>5").Matches(r, ctx) {
		t.Fatalf("expected usecount:>5 to match UseCount=7 via schema registration")
	}
	if CompileScanQuery("usecount:<3").Matches(r, ctx) {
		t.Fatalf("expected usecount:<3 to NOT match UseCount=7")
	}
}

func TestMeshVersion(t *testing.T) {
	r := sample()
	r.MeshVersion = "7.00"
	ctx := ScanQueryContext{}
	for _, c := range []struct {
		q    string
		want bool
	}{
		{"meshversion:7.00", true},
		{"meshversion:7", true},
		{"meshversion:4", false},
		{"mv:7.00", true},
		{"meshver:3", false},
		{`meshversion:/^7\./`, true},
		{`meshversion:/^[35]\./`, false},
	} {
		got := CompileScanQuery(c.q).Matches(r, ctx)
		if got != c.want {
			t.Fatalf("%q: got %v, want %v", c.q, got, c.want)
		}
	}
}
