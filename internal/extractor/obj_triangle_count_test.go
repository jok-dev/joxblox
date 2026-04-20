package extractor

import (
	"strings"
	"testing"
)

func TestCountObjTriangles_InlineFixture(t *testing.T) {
	// Unit cube as 6 quads -> 6 * 2 = 12 triangles.
	obj := `# inline cube
v -0.5 -0.5 -0.5
v  0.5 -0.5 -0.5
v  0.5  0.5 -0.5
v -0.5  0.5 -0.5
v -0.5 -0.5  0.5
v  0.5 -0.5  0.5
v  0.5  0.5  0.5
v -0.5  0.5  0.5
f 1 2 3 4
f 5 6 7 8
f 1 2 6 5
f 2 3 7 6
f 3 4 8 7
f 4 1 5 8
`
	got, err := CountObjTrianglesFromReader(strings.NewReader(obj))
	if err != nil {
		t.Fatalf("CountObjTrianglesFromReader: %s", err.Error())
	}
	if got != 12 {
		t.Fatalf("expected 12 triangles for 6-quad cube, got %d", got)
	}

	tri := "f 1 2 3\nf 4 5 6\n"
	got, err = CountObjTrianglesFromReader(strings.NewReader(tri))
	if err != nil {
		t.Fatalf("CountObjTrianglesFromReader tris: %s", err.Error())
	}
	if got != 2 {
		t.Fatalf("expected 2 triangles for 2 f-lines, got %d", got)
	}

	ngon := "f 1 2 3 4 5\n"
	got, err = CountObjTrianglesFromReader(strings.NewReader(ngon))
	if err != nil {
		t.Fatalf("CountObjTrianglesFromReader ngon: %s", err.Error())
	}
	if got != 3 {
		t.Fatalf("expected 3 triangles for pentagon fan, got %d", got)
	}
}
