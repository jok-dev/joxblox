package extractor

import "strings"

// Approximate triangle counts for Roblox primitive render parts.
// These are rough figures for the renderer's low-poly meshes; callers should
// apply a tolerance when comparing totals against other formats.
const (
	PrimitiveTrianglesCube        = 12
	PrimitiveTrianglesWedge       = 8
	PrimitiveTrianglesCornerWedge = 7
	PrimitiveTrianglesCylinder    = 40
	PrimitiveTrianglesBall        = 80
	PrimitiveTrianglesTruss       = 24
)

// PrimitiveTrianglesForClass returns the approximate triangle count for a
// single instance of the given class name. Returns (0, false) for classes
// whose triangle count depends on external data (MeshPart, UnionOperation).
func PrimitiveTrianglesForClass(instanceClass string) (uint64, bool) {
	switch strings.ToLower(strings.TrimSpace(instanceClass)) {
	case "part", "spawnlocation", "seat", "vehicleseat":
		return PrimitiveTrianglesCube, true
	case "wedgepart":
		return PrimitiveTrianglesWedge, true
	case "cornerwedgepart":
		return PrimitiveTrianglesCornerWedge, true
	case "trusspart":
		return PrimitiveTrianglesTruss, true
	}
	return 0, false
}
