package extractor

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
)

// CountObjTriangles counts triangles in a Wavefront OBJ file.
// Each `f` line with N vertex refs contributes max(0, N-2) triangles (fan).
// Non-face directives (v, vn, vt, g, usemtl, comments, blank lines) are ignored.
func CountObjTriangles(path string) (uint64, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, fmt.Errorf("open obj: %w", err)
	}
	defer file.Close()
	return CountObjTrianglesFromReader(file)
}

// CountObjTrianglesFromReader is the streaming form of CountObjTriangles.
func CountObjTrianglesFromReader(r io.Reader) (uint64, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)

	var total uint64
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) < 2 || line[0] != 'f' {
			continue
		}
		if line[1] != ' ' && line[1] != '\t' {
			continue
		}
		fields := strings.Fields(line[2:])
		if len(fields) >= 3 {
			total += uint64(len(fields) - 2)
		}
	}
	if err := scanner.Err(); err != nil {
		return 0, fmt.Errorf("scan obj: %w", err)
	}
	return total, nil
}
