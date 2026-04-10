package app

import (
	"encoding/binary"
	"testing"
)

func TestLocateDracoPayloadStartVersion7CoreMesh(t *testing.T) {
	data := append([]byte("version 7.00\n"+meshCoreMeshPrefix), 0, 0, 0, 0)
	data = append(data, 0x3e, 0x27, 0x00, 0x00, 0x3a, 0x27, 0x00, 0x00)
	data = append(data, []byte("DRACO\x02\x02\x01\x00\x00\x00")...)

	start, err := locateDracoPayloadStart(data, len("version 7.00\n"))
	if err != nil {
		t.Fatalf("locateDracoPayloadStart returned error: %v", err)
	}
	if start != 33 {
		t.Fatalf("expected Draco payload start 33, got %d", start)
	}
}

func TestLocateDracoPayloadStartRejectsMissingMarker(t *testing.T) {
	data := append([]byte("version 7.00\n"+meshCoreMeshPrefix), 0, 0, 0, 0)
	data = append(data, []byte("NOTDRACO")...)

	if _, err := locateDracoPayloadStart(data, len("version 7.00\n")); err == nil {
		t.Fatal("expected locateDracoPayloadStart to fail when the Draco marker is missing")
	}
}

func TestParseMeshHeaderVersion4LegacyLayout(t *testing.T) {
	data := append([]byte("version 4.00\n"), 0, 0, 0, 0)

	countBytes := make([]byte, 8)
	binary.LittleEndian.PutUint32(countBytes[:4], 321)
	binary.LittleEndian.PutUint32(countBytes[4:], 123)
	data = append(data, countBytes...)

	info, err := parseMeshHeader(data)
	if err != nil {
		t.Fatalf("parseMeshHeader returned error: %v", err)
	}
	if info.NumVerts != 321 {
		t.Fatalf("expected 321 vertices, got %d", info.NumVerts)
	}
	if info.NumFaces != 123 {
		t.Fatalf("expected 123 faces, got %d", info.NumFaces)
	}
}

func buildV4MeshData(numVerts, totalFaces uint32, numBones uint16, numHighQualityLODs uint8, lodOffsetValues []uint32) []byte {
	const sizeofHeader = 24
	const vertexStride = 40
	const skinningStride = 8
	const faceStride = 12

	numLodOffsets := uint16(len(lodOffsetValues))

	header := make([]byte, sizeofHeader)
	binary.LittleEndian.PutUint16(header[0:], sizeofHeader)
	binary.LittleEndian.PutUint16(header[2:], 0)
	binary.LittleEndian.PutUint32(header[4:], numVerts)
	binary.LittleEndian.PutUint32(header[8:], totalFaces)
	binary.LittleEndian.PutUint16(header[12:], numLodOffsets)
	binary.LittleEndian.PutUint16(header[14:], numBones)
	binary.LittleEndian.PutUint32(header[16:], 0)
	binary.LittleEndian.PutUint16(header[20:], 0)
	header[22] = numHighQualityLODs
	header[23] = 0

	vertexData := make([]byte, int(numVerts)*vertexStride)
	var skinningData []byte
	if numBones > 0 {
		skinningData = make([]byte, int(numVerts)*skinningStride)
	}
	faceData := make([]byte, int(totalFaces)*faceStride)

	data := []byte("version 4.00\n")
	data = append(data, header...)
	data = append(data, vertexData...)
	data = append(data, skinningData...)
	data = append(data, faceData...)

	for _, v := range lodOffsetValues {
		b := make([]byte, 4)
		binary.LittleEndian.PutUint32(b, v)
		data = append(data, b...)
	}

	return data
}

func TestParseMeshV4WithMultipleLODs(t *testing.T) {
	data := buildV4MeshData(4, 10, 0, 1, []uint32{0, 6, 10})

	info, err := parseMeshHeader(data)
	if err != nil {
		t.Fatalf("parseMeshHeader returned error: %v", err)
	}
	if info.NumFaces != 6 {
		t.Fatalf("expected LOD0 face count 6, got %d", info.NumFaces)
	}
	if info.NumVerts != 4 {
		t.Fatalf("expected 4 vertices, got %d", info.NumVerts)
	}
}

func TestParseMeshV4SingleLOD(t *testing.T) {
	data := buildV4MeshData(4, 8, 0, 1, []uint32{0, 8})

	info, err := parseMeshHeader(data)
	if err != nil {
		t.Fatalf("parseMeshHeader returned error: %v", err)
	}
	if info.NumFaces != 8 {
		t.Fatalf("expected 8 faces with single LOD, got %d", info.NumFaces)
	}
}

func TestParseMeshV4WithSkinnedLODs(t *testing.T) {
	data := buildV4MeshData(4, 10, 2, 1, []uint32{0, 7, 10})

	info, err := parseMeshHeader(data)
	if err != nil {
		t.Fatalf("parseMeshHeader returned error: %v", err)
	}
	if info.NumFaces != 7 {
		t.Fatalf("expected LOD0 face count 7 with skinning, got %d", info.NumFaces)
	}
}

func buildV3MeshData(numVerts, totalFaces uint32, lodOffsetValues []uint32) []byte {
	const sizeofHeader = 16
	const sizeofVertex = 36
	const sizeofFace = 12

	numLodOffsets := uint16(len(lodOffsetValues))

	subHeader := make([]byte, 8)
	binary.LittleEndian.PutUint16(subHeader[0:], sizeofHeader)
	subHeader[2] = sizeofVertex
	subHeader[3] = sizeofFace
	binary.LittleEndian.PutUint16(subHeader[4:], 4)
	binary.LittleEndian.PutUint16(subHeader[6:], numLodOffsets)

	counts := make([]byte, 8)
	binary.LittleEndian.PutUint32(counts[0:], numVerts)
	binary.LittleEndian.PutUint32(counts[4:], totalFaces)

	vertexData := make([]byte, int(numVerts)*sizeofVertex)
	faceData := make([]byte, int(totalFaces)*sizeofFace)

	data := []byte("version 3.00\n")
	data = append(data, subHeader...)
	data = append(data, counts...)
	data = append(data, vertexData...)
	data = append(data, faceData...)

	for _, v := range lodOffsetValues {
		b := make([]byte, 4)
		binary.LittleEndian.PutUint32(b, v)
		data = append(data, b...)
	}

	return data
}

func TestParseMeshV3WithMultipleLODs(t *testing.T) {
	data := buildV3MeshData(4, 10, []uint32{0, 6, 10})

	info, err := parseMeshHeader(data)
	if err != nil {
		t.Fatalf("parseMeshHeader returned error: %v", err)
	}
	if info.NumFaces != 6 {
		t.Fatalf("expected LOD0 face count 6, got %d", info.NumFaces)
	}
}

func TestParseMeshV3SingleLOD(t *testing.T) {
	data := buildV3MeshData(4, 8, nil)

	info, err := parseMeshHeader(data)
	if err != nil {
		t.Fatalf("parseMeshHeader returned error: %v", err)
	}
	if info.NumFaces != 8 {
		t.Fatalf("expected 8 faces with no LODs, got %d", info.NumFaces)
	}
}

func TestReadHighQualityFaceCountFallsBackOnInvalidTable(t *testing.T) {
	data := make([]byte, 16)
	binary.LittleEndian.PutUint32(data[0:], 5)
	binary.LittleEndian.PutUint32(data[4:], 3)

	result := readHighQualityFaceCount(data, 0, 1, 10)
	if result != 10 {
		t.Fatalf("expected fallback to totalFaces 10 when lod0Start != 0, got %d", result)
	}
}

func TestReadHighQualityFaceCountFallsBackOnTruncatedData(t *testing.T) {
	result := readHighQualityFaceCount([]byte{0, 0, 0, 0}, 0, 1, 10)
	if result != 10 {
		t.Fatalf("expected fallback to totalFaces 10 when data is too short, got %d", result)
	}
}
