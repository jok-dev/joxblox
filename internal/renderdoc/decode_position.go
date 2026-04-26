package renderdoc

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

// ErrUnsupportedPositionFormat is returned when DecodePositions sees a
// DXGI format it can't decode. Callers (the meshes preview) show
// "format not supported yet" rather than failing outright.
var ErrUnsupportedPositionFormat = errors.New("unsupported position format")

// DecodePositions reads XYZ positions out of a vertex buffer byte slice
// using the given format, byte offset within each vertex, and stride.
// Returns a flat []float32{x0,y0,z0,x1,y1,z1,...}.
func DecodePositions(vertexBytes []byte, format string, alignedByteOffset, stride int) ([]float32, error) {
	switch format {
	case "DXGI_FORMAT_R32G32B32_FLOAT":
		return decodeR32G32B32Float(vertexBytes, alignedByteOffset, stride)
	}
	return nil, fmt.Errorf("%w: %s", ErrUnsupportedPositionFormat, format)
}

func decodeR32G32B32Float(vertexBytes []byte, offset, stride int) ([]float32, error) {
	if stride <= 0 {
		return nil, fmt.Errorf("invalid stride %d", stride)
	}
	if offset < 0 || offset+12 > stride {
		return nil, fmt.Errorf("position offset %d + 12 exceeds stride %d", offset, stride)
	}
	vertexCount := len(vertexBytes) / stride
	out := make([]float32, 0, vertexCount*3)
	for i := 0; i < vertexCount; i++ {
		base := i*stride + offset
		if base+12 > len(vertexBytes) {
			break
		}
		out = append(out,
			math.Float32frombits(binary.LittleEndian.Uint32(vertexBytes[base:])),
			math.Float32frombits(binary.LittleEndian.Uint32(vertexBytes[base+4:])),
			math.Float32frombits(binary.LittleEndian.Uint32(vertexBytes[base+8:])),
		)
	}
	return out, nil
}

// DecodeIndices reads indices from an index buffer byte slice in the
// given format, returning them as []uint32 for uniform downstream use.
func DecodeIndices(indexBytes []byte, format string) ([]uint32, error) {
	switch format {
	case "DXGI_FORMAT_R16_UINT":
		count := len(indexBytes) / 2
		out := make([]uint32, count)
		for i := 0; i < count; i++ {
			out[i] = uint32(binary.LittleEndian.Uint16(indexBytes[i*2:]))
		}
		return out, nil
	case "DXGI_FORMAT_R32_UINT":
		count := len(indexBytes) / 4
		out := make([]uint32, count)
		for i := 0; i < count; i++ {
			out[i] = binary.LittleEndian.Uint32(indexBytes[i*4:])
		}
		return out, nil
	}
	return nil, fmt.Errorf("unsupported index format: %s", format)
}
