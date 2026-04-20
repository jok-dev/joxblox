package loader

import (
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"

	"joxblox/internal/extractor"
	"joxblox/internal/roblox"
	"joxblox/internal/roblox/mesh"
)

const privateTestsDirName = "joxblox-private-tests"
const privateTrianglesTolerancePercent = 5.0

// findPrivateTestsDir walks up from this test file's directory looking for a
// sibling directory named joxblox-private-tests. "" when not found.
func findPrivateTestsDir() string {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		return ""
	}
	dir := filepath.Dir(thisFile)
	for i := 0; i < 10; i++ {
		candidate := filepath.Join(dir, privateTestsDirName)
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
	return ""
}

// findPairFile returns the single file in dir whose extension is in exts.
// Returns "" with no error if none match; errors if multiple match.
func findPairFile(dir string, exts ...string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	matches := []string{}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		for _, want := range exts {
			if ext == want {
				matches = append(matches, filepath.Join(dir, entry.Name()))
				break
			}
		}
	}
	if len(matches) > 1 {
		names := make([]string, len(matches))
		for i, p := range matches {
			names[i] = filepath.Base(p)
		}
		return "", &multiMatchError{dir: dir, exts: exts, names: names}
	}
	if len(matches) == 0 {
		return "", nil
	}
	return matches[0], nil
}

// isMeshReferenceProperty matches the property names Roblox uses for mesh
// references: MeshId (legacy SpecialMesh/MeshPart), MeshContent (new MeshPart
// content-ID field), MeshAssetId (scripting APIs / rare variants).
func isMeshReferenceProperty(propertyName string) bool {
	switch strings.ToLower(strings.TrimSpace(propertyName)) {
	case "meshid", "meshcontent", "meshassetid":
		return true
	}
	return false
}

type multiMatchError struct {
	dir   string
	exts  []string
	names []string
}

func (e *multiMatchError) Error() string {
	return "expected exactly one " + strings.Join(e.exts, "/") + " file in " + e.dir + ", found " + strings.Join(e.names, ", ")
}

// meshFaceCountCache memoizes asset-id -> face-count lookups across a test run
// so repeated MeshIds in the rbxl don't re-download.
var meshFaceCountCache = struct {
	sync.Mutex
	byID map[int64]uint32
}{byID: map[int64]uint32{}}

// fetchMeshFaceCount downloads the mesh bytes for assetID using joxblox's
// authenticated asset-delivery pipeline and returns the parsed face count.
// Returns 0 with no error when the mesh header is unparseable (rare non-mesh
// asset types, deleted assets) so one bad mesh doesn't abort the whole pair.
func fetchMeshFaceCount(assetID int64) (uint32, error) {
	meshFaceCountCache.Lock()
	if cached, ok := meshFaceCountCache.byID[assetID]; ok {
		meshFaceCountCache.Unlock()
		return cached, nil
	}
	meshFaceCountCache.Unlock()

	info, err := FetchAssetDeliveryInfo(assetID)
	if err != nil {
		return 0, err
	}
	if info == nil || strings.TrimSpace(info.Location) == "" {
		return 0, nil
	}
	bodyBytes, _, err := DownloadRobloxContentBytesWithCacheKey(
		info.Location,
		BuildAssetFileContentCacheKey(assetID, info.AssetTypeID),
		requestTimeout,
	)
	if err != nil {
		return 0, err
	}
	header, headerErr := mesh.ParseHeader(bodyBytes)
	faces := uint32(0)
	if headerErr == nil {
		faces = header.NumFaces
	}

	meshFaceCountCache.Lock()
	meshFaceCountCache.byID[assetID] = faces
	meshFaceCountCache.Unlock()
	return faces, nil
}

type rbxlTriangleBreakdown struct {
	Total          uint64
	Primitive      uint64
	Mesh           uint64
	PrimitiveParts int
	UniqueMeshIDs  int
	MeshInstances  int
	FetchErrors    int
}

// computeRbxlTriangles sums primitive-part triangles (local) with mesh
// triangles (fetched via asset-delivery pipeline). Requires a valid
// .ROBLOSECURITY cookie to be configured via roblox.SetRoblosecurityCookie.
func computeRbxlTriangles(t *testing.T, rbxlPath string) rbxlTriangleBreakdown {
	t.Helper()
	breakdown := rbxlTriangleBreakdown{}

	parts, err := extractor.ExtractMapRenderParts(rbxlPath, nil, nil)
	if err != nil {
		t.Fatalf("ExtractMapRenderParts: %s", err.Error())
	}
	for _, part := range parts {
		if tris, ok := extractor.PrimitiveTrianglesForClass(part.InstanceType); ok {
			breakdown.Primitive += tris
			breakdown.PrimitiveParts++
		}
	}

	refs, err := extractor.ExtractAssetIDsWithCounts(rbxlPath, 0, 0, nil)
	if err != nil {
		t.Fatalf("ExtractAssetIDsWithCounts: %s", err.Error())
	}

	meshAssetIDs := map[int64]struct{}{}
	for _, ref := range refs.References {
		if ref.ID <= 0 {
			continue
		}
		if !isMeshReferenceProperty(ref.PropertyName) {
			continue
		}
		meshAssetIDs[ref.ID] = struct{}{}
	}
	breakdown.UniqueMeshIDs = len(meshAssetIDs)

	sortedIDs := make([]int64, 0, len(meshAssetIDs))
	for id := range meshAssetIDs {
		sortedIDs = append(sortedIDs, id)
	}
	sort.Slice(sortedIDs, func(i, j int) bool { return sortedIDs[i] < sortedIDs[j] })

	for idx, id := range sortedIDs {
		useCount := refs.UseCounts[id]
		if useCount <= 0 {
			useCount = 1
		}
		faces, err := fetchMeshFaceCount(id)
		if err != nil {
			breakdown.FetchErrors++
			t.Logf("fetch mesh %d failed: %s", id, err.Error())
			continue
		}
		breakdown.Mesh += uint64(faces) * uint64(useCount)
		breakdown.MeshInstances += useCount
		if idx > 0 && idx%100 == 0 {
			t.Logf("fetched %d/%d unique meshes (mesh tris so far: %d)",
				idx, len(sortedIDs), breakdown.Mesh)
		}
	}

	breakdown.Total = breakdown.Primitive + breakdown.Mesh
	return breakdown
}

func TestPrivateTriangleCounts(t *testing.T) {
	privateDir := findPrivateTestsDir()
	if privateDir == "" {
		t.Skip("private test suite not present (no sibling joxblox-private-tests/ directory)")
	}

	cookie, cookieErr := roblox.LoadRoblosecurityCookieFromKeyring()
	if cookieErr != nil {
		t.Fatalf("load .ROBLOSECURITY from keyring: %s", cookieErr.Error())
	}
	if strings.TrimSpace(cookie) == "" {
		t.Fatalf("private test suite found at %s but no .ROBLOSECURITY cookie in keyring - sign in via the app's Auth panel first", privateDir)
	}
	roblox.SetRoblosecurityCookie(cookie)
	t.Cleanup(func() { roblox.ClearRoblosecurityCookie() })
	if err := roblox.ValidateCurrentAuthCookie(); err != nil {
		t.Fatalf(".ROBLOSECURITY cookie validation failed: %s", err.Error())
	}

	entries, err := os.ReadDir(privateDir)
	if err != nil {
		t.Fatalf("read %s: %s", privateDir, err.Error())
	}
	pairNames := []string{}
	for _, entry := range entries {
		if entry.IsDir() {
			pairNames = append(pairNames, entry.Name())
		}
	}
	sort.Strings(pairNames)
	if len(pairNames) == 0 {
		t.Skipf("private test directory %s has no pair subdirectories", privateDir)
	}

	for _, name := range pairNames {
		pairDir := filepath.Join(privateDir, name)
		t.Run(name, func(t *testing.T) {
			rbxlPath, err := findPairFile(pairDir, ".rbxl", ".rbxm")
			if err != nil {
				t.Fatal(err.Error())
			}
			objPath, err := findPairFile(pairDir, ".obj")
			if err != nil {
				t.Fatal(err.Error())
			}
			if rbxlPath == "" || objPath == "" {
				t.Skipf("pair %q missing rbxl/rbxm or obj file (rbxl=%q obj=%q)", name, rbxlPath, objPath)
			}

			objTris, err := extractor.CountObjTriangles(objPath)
			if err != nil {
				t.Fatalf("obj triangle count: %s", err.Error())
			}
			if objTris == 0 {
				t.Fatalf("obj triangle count is 0 - fixture may be malformed")
			}

			breakdown := computeRbxlTriangles(t, rbxlPath)

			t.Logf("obj triangles:     %d", objTris)
			t.Logf("rbxl triangles:    %d (primitive=%d across %d parts, mesh=%d across %d unique / %d uses)",
				breakdown.Total, breakdown.Primitive, breakdown.PrimitiveParts,
				breakdown.Mesh, breakdown.UniqueMeshIDs, breakdown.MeshInstances)
			if breakdown.FetchErrors > 0 {
				t.Logf("rbxl fetch errors: %d meshes failed to download (counted as 0)", breakdown.FetchErrors)
			}

			diff := math.Abs(float64(breakdown.Total) - float64(objTris))
			pct := diff / float64(objTris) * 100.0
			t.Logf("diff:              %.0f (%.2f%% of obj)", diff, pct)
			if pct > privateTrianglesTolerancePercent {
				t.Fatalf("triangle count mismatch exceeds tolerance: obj=%d rbxl=%d diff=%.0f (%.2f%% > %.2f%% tolerance)",
					objTris, breakdown.Total, diff, pct, privateTrianglesTolerancePercent)
			}
		})
	}
}
