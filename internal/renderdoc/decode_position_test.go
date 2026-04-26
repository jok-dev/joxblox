package renderdoc

import (
	"encoding/binary"
	"errors"
	"math"
	"testing"
)

func TestDecodePositionsR32G32B32Float(t *testing.T) {
	// Two vertices with stride 16 (12 bytes of position + 4 bytes of padding).
	buf := make([]byte, 32)
	binary.LittleEndian.PutUint32(buf[0:], math.Float32bits(1))
	binary.LittleEndian.PutUint32(buf[4:], math.Float32bits(2))
	binary.LittleEndian.PutUint32(buf[8:], math.Float32bits(3))
	binary.LittleEndian.PutUint32(buf[16:], math.Float32bits(4))
	binary.LittleEndian.PutUint32(buf[20:], math.Float32bits(5))
	binary.LittleEndian.PutUint32(buf[24:], math.Float32bits(6))
	positions, err := DecodePositions(buf, "DXGI_FORMAT_R32G32B32_FLOAT", 0, 16)
	if err != nil {
		t.Fatalf("DecodePositions: %v", err)
	}
	want := []float32{1, 2, 3, 4, 5, 6}
	if len(positions) != len(want) {
		t.Fatalf("positions len = %d, want %d", len(positions), len(want))
	}
	for i, v := range want {
		if positions[i] != v {
			t.Errorf("positions[%d] = %f, want %f", i, positions[i], v)
		}
	}
}

func TestDecodePositionsUnsupportedFormat(t *testing.T) {
	_, err := DecodePositions([]byte{0}, "DXGI_FORMAT_R16G16B16A16_SNORM", 0, 8)
	if !errors.Is(err, ErrUnsupportedPositionFormat) {
		t.Errorf("err = %v, want ErrUnsupportedPositionFormat", err)
	}
}

func TestDecodeIndices16(t *testing.T) {
	buf := []byte{1, 0, 2, 0, 3, 0, 4, 0}
	indices, err := DecodeIndices(buf, "DXGI_FORMAT_R16_UINT")
	if err != nil {
		t.Fatalf("DecodeIndices: %v", err)
	}
	want := []uint32{1, 2, 3, 4}
	if len(indices) != len(want) {
		t.Fatalf("indices len = %d, want %d", len(indices), len(want))
	}
	for i, v := range want {
		if indices[i] != v {
			t.Errorf("indices[%d] = %d, want %d", i, indices[i], v)
		}
	}
}

func TestDecodeIndices32(t *testing.T) {
	buf := []byte{1, 0, 0, 0, 2, 0, 0, 0}
	indices, err := DecodeIndices(buf, "DXGI_FORMAT_R32_UINT")
	if err != nil {
		t.Fatalf("DecodeIndices: %v", err)
	}
	if len(indices) != 2 || indices[0] != 1 || indices[1] != 2 {
		t.Errorf("indices = %v, want [1 2]", indices)
	}
}
