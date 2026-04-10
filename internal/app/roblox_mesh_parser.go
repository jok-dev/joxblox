package app

import (
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"
)

type meshHeaderInfo struct {
	Version  string
	NumVerts uint32
	NumFaces uint32
}

const meshVersionPrefix = "version "
const meshCoreMeshPrefix = "COREMESH"
func parseMeshHeader(data []byte) (meshHeaderInfo, error) {
	if len(data) < 13 {
		return meshHeaderInfo{}, fmt.Errorf("data too short for mesh header")
	}
	headerStr := string(data[:13])
	if !strings.HasPrefix(headerStr, meshVersionPrefix) {
		return meshHeaderInfo{}, fmt.Errorf("not a Roblox mesh file")
	}
	newlineIdx := strings.IndexByte(headerStr, '\n')
	if newlineIdx < 0 {
		return meshHeaderInfo{}, fmt.Errorf("missing newline after version header")
	}
	version := strings.TrimSpace(headerStr[len(meshVersionPrefix):newlineIdx])
	bodyStart := newlineIdx + 1

	switch version {
	case "1.00", "1.01":
		return parseMeshV1(data, bodyStart, version)
	case "2.00":
		return parseMeshV2(data, bodyStart, version)
	case "3.00", "3.01":
		return parseMeshV3(data, bodyStart, version)
	case "4.00", "4.01", "5.00", "6.00", "7.00":
		return parseMeshV4Plus(data, bodyStart, version)
	default:
		return meshHeaderInfo{}, fmt.Errorf("unsupported mesh version: %s", version)
	}
}

func parseMeshV1(data []byte, bodyStart int, version string) (meshHeaderInfo, error) {
	rest := string(data[bodyStart:])
	newlineIdx := strings.IndexByte(rest, '\n')
	if newlineIdx < 0 {
		return meshHeaderInfo{}, fmt.Errorf("v1 mesh missing face count line")
	}
	numFaces, err := strconv.ParseUint(strings.TrimSpace(rest[:newlineIdx]), 10, 32)
	if err != nil {
		return meshHeaderInfo{}, fmt.Errorf("v1 mesh invalid face count: %w", err)
	}
	return meshHeaderInfo{
		Version:  version,
		NumVerts: uint32(numFaces) * 3,
		NumFaces: uint32(numFaces),
	}, nil
}

func parseMeshV2(data []byte, bodyStart int, version string) (meshHeaderInfo, error) {
	// v2 sub-header: sizeof_header(u16) + sizeof_vertex(u8) + sizeof_face(u8) = 4 bytes, then numVerts(u32) + numFaces(u32)
	offset := bodyStart + 4
	if len(data) < offset+8 {
		return meshHeaderInfo{}, fmt.Errorf("v2 mesh data too short for header")
	}
	numVerts := binary.LittleEndian.Uint32(data[offset:])
	numFaces := binary.LittleEndian.Uint32(data[offset+4:])
	return meshHeaderInfo{Version: version, NumVerts: numVerts, NumFaces: numFaces}, nil
}

func parseMeshV3(data []byte, bodyStart int, version string) (meshHeaderInfo, error) {
	// v3 sub-header: sizeof_header(u16) + sizeof_vertex(u8) + sizeof_face(u8) + sizeof_lodOffset(u16) + numLodOffsets(u16) = 8 bytes
	offset := bodyStart + 8
	if len(data) < offset+8 {
		return meshHeaderInfo{}, fmt.Errorf("v3 mesh data too short for header")
	}
	sizeofHeader := int(binary.LittleEndian.Uint16(data[bodyStart:]))
	sizeofVertex := int(data[bodyStart+2])
	sizeofFace := int(data[bodyStart+3])
	numLodOffsets := int(binary.LittleEndian.Uint16(data[bodyStart+6:]))

	numVerts := binary.LittleEndian.Uint32(data[offset:])
	numFaces := binary.LittleEndian.Uint32(data[offset+4:])

	if numLodOffsets >= 2 && sizeofHeader > 0 && sizeofVertex > 0 && sizeofFace > 0 {
		lodTableStart := bodyStart + sizeofHeader + int(numVerts)*sizeofVertex + int(numFaces)*sizeofFace
		numFaces = readHighQualityFaceCount(data, lodTableStart, 1, numFaces)
	}

	return meshHeaderInfo{Version: version, NumVerts: numVerts, NumFaces: numFaces}, nil
}

func parseMeshV4Plus(data []byte, bodyStart int, version string) (meshHeaderInfo, error) {
	if version == "7.00" && len(data) >= bodyStart+len(meshCoreMeshPrefix) && string(data[bodyStart:bodyStart+len(meshCoreMeshPrefix)]) == meshCoreMeshPrefix {
		return parseMeshV7CoreMesh(data, bodyStart, version)
	}

	// v4+ sub-header: sizeof_header(u16) + lodType(u16) = 4 bytes, then numVerts(u32) + numFaces(u32)
	offset := bodyStart + 4
	if len(data) < offset+8 {
		return meshHeaderInfo{}, fmt.Errorf("v%s mesh data too short for header", version)
	}
	numVerts := binary.LittleEndian.Uint32(data[offset:])
	numFaces := binary.LittleEndian.Uint32(data[offset+4:])

	// v4/v5 header: numLodOffsets(u16) at +12, numBones(u16) at +14.
	// The header numFaces is the total across all LOD levels; extract LOD0 only.
	if (version == "4.00" || version == "4.01" || version == "5.00") && len(data) >= bodyStart+16 {
		sizeofHeader := int(binary.LittleEndian.Uint16(data[bodyStart:]))
		numLodOffsets := int(binary.LittleEndian.Uint16(data[bodyStart+12:]))
		numBones := int(binary.LittleEndian.Uint16(data[bodyStart+14:]))
		if numLodOffsets >= 2 && sizeofHeader >= 24 {
			const vertexStride = 40
			const faceStride = 12
			skinningSize := 0
			if numBones > 0 {
				const skinningStride = 8
				skinningSize = int(numVerts) * skinningStride
			}
			lodTableStart := bodyStart + sizeofHeader + int(numVerts)*vertexStride + skinningSize + int(numFaces)*faceStride
			numFaces = readHighQualityFaceCount(data, lodTableStart, 1, numFaces)
		}
	}

	return meshHeaderInfo{Version: version, NumVerts: numVerts, NumFaces: numFaces}, nil
}

// readHighQualityFaceCount reads the LOD offset table and returns the
// face count that spans the first numHighQualityLODs levels.  For v4/v5
// meshes Roblox combines all "high quality" LOD levels into the reported
// triangle count; the boundary index into the offset table equals
// numHighQualityLODs (minimum 1).  Falls back to totalFaces when the
// table cannot be read or the values look invalid.
func readHighQualityFaceCount(data []byte, lodTableStart int, numHighQualityLODs int, totalFaces uint32) uint32 {
	lodIndex := numHighQualityLODs
	if lodIndex < 1 {
		lodIndex = 1
	}
	endOffset := lodTableStart + lodIndex*4
	if lodTableStart <= 0 || endOffset <= 0 || len(data) < endOffset+4 {
		return totalFaces
	}
	lod0Start := binary.LittleEndian.Uint32(data[lodTableStart:])
	hqEnd := binary.LittleEndian.Uint32(data[endOffset:])
	if lod0Start == 0 && hqEnd > 0 && hqEnd <= totalFaces {
		return hqEnd
	}
	return totalFaces
}

func parseMeshV7CoreMesh(data []byte, bodyStart int, version string) (meshHeaderInfo, error) {
	info, err := extractMeshStatsWithRustyAssetToolFromBytes(data)
	if err != nil {
		return meshHeaderInfo{}, fmt.Errorf("v%s COREMESH decode failed: %w", version, err)
	}
	if strings.TrimSpace(info.Version) == "" {
		info.Version = version
	}
	return info, nil
}

func locateDracoPayloadStart(data []byte, bodyStart int) (int, error) {
	searchStart := bodyStart
	if len(data) >= bodyStart+len(meshCoreMeshPrefix) && string(data[bodyStart:bodyStart+len(meshCoreMeshPrefix)]) == meshCoreMeshPrefix {
		searchStart = bodyStart + len(meshCoreMeshPrefix)
	}
	relativeIdx := strings.Index(string(data[searchStart:]), "DRACO")
	if relativeIdx < 0 {
		return 0, fmt.Errorf("missing DRACO marker")
	}
	absoluteIdx := searchStart + relativeIdx
	if len(data) < absoluteIdx+11 {
		return 0, fmt.Errorf("truncated Draco header")
	}
	if data[absoluteIdx+7] != 0 && data[absoluteIdx+7] != 1 {
		return 0, fmt.Errorf("unsupported Draco encoder type: %d", data[absoluteIdx+7])
	}
	return absoluteIdx, nil
}

func isMeshAssetType(assetTypeID int) bool {
	return assetTypeID == assetTypeMesh || assetTypeID == 40
}

func formatMeshInfo(info meshHeaderInfo) string {
	return fmt.Sprintf("%s triangles · %s vertices", formatIntCommas(int64(info.NumFaces)), formatIntCommas(int64(info.NumVerts)))
}

func formatIntCommas(n int64) string {
	s := strconv.FormatInt(n, 10)
	if len(s) <= 3 {
		return s
	}
	var result []byte
	for i, ch := range s {
		remaining := len(s) - i
		if i > 0 && remaining%3 == 0 {
			result = append(result, ',')
		}
		result = append(result, byte(ch))
	}
	return string(result)
}
