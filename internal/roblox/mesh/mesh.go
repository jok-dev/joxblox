package mesh

import (
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"

	"joxblox/internal/format"
	"joxblox/internal/roblox"
)

type HeaderInfo struct {
	Version  string
	NumVerts uint32
	NumFaces uint32
}

const VersionPrefix = "version "
const CoreMeshPrefix = "COREMESH"

// CoreMeshFallback is injected at startup by the app package to break
// the circular dependency between mesh parsing and the Rust extractor.
var CoreMeshFallback func(data []byte) (HeaderInfo, error)

func ParseHeader(data []byte) (HeaderInfo, error) {
	if len(data) < 13 {
		return HeaderInfo{}, fmt.Errorf("data too short for mesh header")
	}
	headerStr := string(data[:13])
	if !strings.HasPrefix(headerStr, VersionPrefix) {
		return HeaderInfo{}, fmt.Errorf("not a Roblox mesh file")
	}
	newlineIdx := strings.IndexByte(headerStr, '\n')
	if newlineIdx < 0 {
		return HeaderInfo{}, fmt.Errorf("missing newline after version header")
	}
	version := strings.TrimSpace(headerStr[len(VersionPrefix):newlineIdx])
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
		return HeaderInfo{}, fmt.Errorf("unsupported mesh version: %s", version)
	}
}

func parseMeshV1(data []byte, bodyStart int, version string) (HeaderInfo, error) {
	rest := string(data[bodyStart:])
	newlineIdx := strings.IndexByte(rest, '\n')
	if newlineIdx < 0 {
		return HeaderInfo{}, fmt.Errorf("v1 mesh missing face count line")
	}
	numFaces, err := strconv.ParseUint(strings.TrimSpace(rest[:newlineIdx]), 10, 32)
	if err != nil {
		return HeaderInfo{}, fmt.Errorf("v1 mesh invalid face count: %w", err)
	}
	return HeaderInfo{
		Version:  version,
		NumVerts: uint32(numFaces) * 3,
		NumFaces: uint32(numFaces),
	}, nil
}

func parseMeshV2(data []byte, bodyStart int, version string) (HeaderInfo, error) {
	offset := bodyStart + 4
	if len(data) < offset+8 {
		return HeaderInfo{}, fmt.Errorf("v2 mesh data too short for header")
	}
	numVerts := binary.LittleEndian.Uint32(data[offset:])
	numFaces := binary.LittleEndian.Uint32(data[offset+4:])
	return HeaderInfo{Version: version, NumVerts: numVerts, NumFaces: numFaces}, nil
}

func parseMeshV3(data []byte, bodyStart int, version string) (HeaderInfo, error) {
	offset := bodyStart + 8
	if len(data) < offset+8 {
		return HeaderInfo{}, fmt.Errorf("v3 mesh data too short for header")
	}
	sizeofHeader := int(binary.LittleEndian.Uint16(data[bodyStart:]))
	sizeofVertex := int(data[bodyStart+2])
	sizeofFace := int(data[bodyStart+3])
	numLodOffsets := int(binary.LittleEndian.Uint16(data[bodyStart+6:]))

	numVerts := binary.LittleEndian.Uint32(data[offset:])
	numFaces := binary.LittleEndian.Uint32(data[offset+4:])

	if numLodOffsets >= 2 && sizeofHeader > 0 && sizeofVertex > 0 && sizeofFace > 0 {
		lodTableStart := bodyStart + sizeofHeader + int(numVerts)*sizeofVertex + int(numFaces)*sizeofFace
		numFaces = ReadHighQualityFaceCount(data, lodTableStart, 1, numFaces)
	}

	return HeaderInfo{Version: version, NumVerts: numVerts, NumFaces: numFaces}, nil
}

func parseMeshV4Plus(data []byte, bodyStart int, version string) (HeaderInfo, error) {
	if version == "7.00" && len(data) >= bodyStart+len(CoreMeshPrefix) && string(data[bodyStart:bodyStart+len(CoreMeshPrefix)]) == CoreMeshPrefix {
		return parseMeshV7CoreMesh(data, bodyStart, version)
	}

	offset := bodyStart + 4
	if len(data) < offset+8 {
		return HeaderInfo{}, fmt.Errorf("v%s mesh data too short for header", version)
	}
	numVerts := binary.LittleEndian.Uint32(data[offset:])
	numFaces := binary.LittleEndian.Uint32(data[offset+4:])

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
			numFaces = ReadHighQualityFaceCount(data, lodTableStart, 1, numFaces)
		}
	}

	return HeaderInfo{Version: version, NumVerts: numVerts, NumFaces: numFaces}, nil
}

func ReadHighQualityFaceCount(data []byte, lodTableStart int, numHighQualityLODs int, totalFaces uint32) uint32 {
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

func parseMeshV7CoreMesh(data []byte, bodyStart int, version string) (HeaderInfo, error) {
	if CoreMeshFallback == nil {
		return HeaderInfo{}, fmt.Errorf("v%s COREMESH decode not available (no fallback registered)", version)
	}
	info, err := CoreMeshFallback(data)
	if err != nil {
		return HeaderInfo{}, fmt.Errorf("v%s COREMESH decode failed: %w", version, err)
	}
	if strings.TrimSpace(info.Version) == "" {
		info.Version = version
	}
	return info, nil
}

func LocateDracoPayloadStart(data []byte, bodyStart int) (int, error) {
	searchStart := bodyStart
	if len(data) >= bodyStart+len(CoreMeshPrefix) && string(data[bodyStart:bodyStart+len(CoreMeshPrefix)]) == CoreMeshPrefix {
		searchStart = bodyStart + len(CoreMeshPrefix)
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

func IsMeshAssetType(assetTypeID int) bool {
	return assetTypeID == roblox.AssetTypeMesh || assetTypeID == 40
}

func FormatInfo(info HeaderInfo) string {
	version := info.Version
	if version == "" {
		version = "?"
	}
	return fmt.Sprintf("v%s · %s triangles · %s vertices", version, format.FormatIntCommas(int64(info.NumFaces)), format.FormatIntCommas(int64(info.NumVerts)))
}
