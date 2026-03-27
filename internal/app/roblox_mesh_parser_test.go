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
