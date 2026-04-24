package ktx2

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"sync"

	"github.com/klauspost/compress/zstd"
)

// zstdDecoderPool amortises the cost of zstd decoder allocation across
// many Decompress calls on small payloads (typical for mip chains).
var zstdDecoderPool = sync.Pool{
	New: func() any {
		decoder, err := zstd.NewReader(nil)
		if err != nil {
			return err
		}
		return decoder
	},
}

// DecompressLevel returns the uncompressed payload for the given mip.
// Handles supercompression scheme 0 (None) and 2 (Zstd). Returns an error
// for schemes we don't implement (BasisLZ = 1, ZLIB = 3).
func (container *Container) DecompressLevel(levelIndex int) ([]byte, error) {
	raw, err := container.LevelBytes(levelIndex)
	if err != nil {
		return nil, err
	}
	switch container.Header.SupercompressionScheme {
	case SupercompressionNone:
		return raw, nil
	case SupercompressionZstd:
		return decompressZstd(raw, int(container.Levels[levelIndex].UncompressedByteLength))
	case SupercompressionBasisLZ:
		return nil, fmt.Errorf("ktx2: BasisLZ supercompression not supported (needs external transcoder)")
	case SupercompressionZLIB:
		return nil, fmt.Errorf("ktx2: ZLIB supercompression not implemented")
	}
	return nil, fmt.Errorf("ktx2: unknown supercompression scheme %d", container.Header.SupercompressionScheme)
}

func decompressZstd(compressed []byte, expectedSize int) ([]byte, error) {
	pooled := zstdDecoderPool.Get()
	decoder, ok := pooled.(*zstd.Decoder)
	if !ok {
		if poolErr, isErr := pooled.(error); isErr {
			return nil, fmt.Errorf("ktx2: zstd decoder unavailable: %w", poolErr)
		}
		return nil, fmt.Errorf("ktx2: zstd decoder unavailable")
	}
	defer zstdDecoderPool.Put(decoder)

	out := make([]byte, 0, expectedSize)
	decompressed, err := decoder.DecodeAll(compressed, out)
	if err != nil {
		return nil, fmt.Errorf("ktx2: zstd decode: %w", err)
	}
	return decompressed, nil
}

// DecompressTransportWrapper unwraps the outer transport encoding some
// Roblox CDN responses ship in: gzip (magic 0x1F 0x8B) and zstd (magic
// 0x28 0xB5 0x2F 0xFD) are both recognised. Returns the input unchanged
// if no recognised wrapper is present. Independent of the KTX2 per-level
// supercompression scheme.
func DecompressTransportWrapper(data []byte) ([]byte, error) {
	if len(data) >= 2 && data[0] == 0x1F && data[1] == 0x8B {
		reader, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("ktx2: gzip reader: %w", err)
		}
		defer reader.Close()
		decompressed, readErr := io.ReadAll(reader)
		if readErr != nil {
			return nil, fmt.Errorf("ktx2: gzip decode: %w", readErr)
		}
		return decompressed, nil
	}
	if len(data) >= 4 && data[0] == 0x28 && data[1] == 0xB5 && data[2] == 0x2F && data[3] == 0xFD {
		return decompressZstd(data, 0)
	}
	return data, nil
}
