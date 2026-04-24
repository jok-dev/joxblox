package ktx2

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"errors"
	"testing"

	"github.com/klauspost/compress/zstd"
)

// makeKTX2 builds a minimal KTX2 byte stream for testing. levels are
// laid out back-to-back right after the level index. Payloads carry no
// supercompression unless scheme != SupercompressionNone, in which case
// the caller is responsible for feeding already-compressed bytes per
// level and setting matching UncompressedByteLength.
func makeKTX2(vkFormat VkFormat, width, height uint32, scheme SupercompressionScheme, levels [][]byte, uncompressedLens []uint64) []byte {
	var buf bytes.Buffer
	buf.Write(Magic[:])
	header := make([]byte, fixedHeaderSize-magicSize)
	binary.LittleEndian.PutUint32(header[0:4], uint32(vkFormat))
	binary.LittleEndian.PutUint32(header[4:8], 1)
	binary.LittleEndian.PutUint32(header[8:12], width)
	binary.LittleEndian.PutUint32(header[12:16], height)
	binary.LittleEndian.PutUint32(header[16:20], 0)
	binary.LittleEndian.PutUint32(header[20:24], 0)
	binary.LittleEndian.PutUint32(header[24:28], 1)
	binary.LittleEndian.PutUint32(header[28:32], uint32(len(levels)))
	binary.LittleEndian.PutUint32(header[32:36], uint32(scheme))
	buf.Write(header)

	levelIndexOffset := uint64(fixedHeaderSize + len(levels)*levelEntrySize)
	levelIndex := make([]byte, len(levels)*levelEntrySize)
	payloadCursor := levelIndexOffset
	for i, level := range levels {
		binary.LittleEndian.PutUint64(levelIndex[i*levelEntrySize:], payloadCursor)
		binary.LittleEndian.PutUint64(levelIndex[i*levelEntrySize+8:], uint64(len(level)))
		uncompressed := uncompressedLens[i]
		if uncompressed == 0 {
			uncompressed = uint64(len(level))
		}
		binary.LittleEndian.PutUint64(levelIndex[i*levelEntrySize+16:], uncompressed)
		payloadCursor += uint64(len(level))
	}
	buf.Write(levelIndex)
	for _, level := range levels {
		buf.Write(level)
	}
	return buf.Bytes()
}

func TestParse_RejectsBadMagic(t *testing.T) {
	// Use an input long enough to pass the length guard so the magic
	// check is actually exercised.
	input := bytes.Repeat([]byte{0x00}, fixedHeaderSize)
	_, err := Parse(input)
	if !errors.Is(err, ErrBadMagic) {
		t.Errorf("expected ErrBadMagic, got %v", err)
	}
}

func TestParse_RejectsTooShort(t *testing.T) {
	_, err := Parse([]byte("tiny"))
	if err == nil || errors.Is(err, ErrBadMagic) {
		t.Errorf("expected length-based error, got %v", err)
	}
}

func TestParse_ReadsDimensionsAndLevels(t *testing.T) {
	levels := [][]byte{
		bytes.Repeat([]byte{0xAA}, 131072), // mip 0: 512x512 BC1 base
		bytes.Repeat([]byte{0xBB}, 32768),  // mip 1: 256x256 BC1
	}
	data := makeKTX2(VkFormatBC1RGBUnorm, 512, 512, SupercompressionNone, levels, []uint64{131072, 32768})

	container, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if container.Header.PixelWidth != 512 || container.Header.PixelHeight != 512 {
		t.Errorf("dims = %dx%d, want 512x512", container.Header.PixelWidth, container.Header.PixelHeight)
	}
	if container.Header.VkFormat != VkFormatBC1RGBUnorm {
		t.Errorf("vkFormat = %d, want BC1RGBUnorm", container.Header.VkFormat)
	}
	if len(container.Levels) != 2 {
		t.Fatalf("got %d levels, want 2", len(container.Levels))
	}
}

func TestLevelBytes_ReturnsRawPayload(t *testing.T) {
	mip0 := bytes.Repeat([]byte{0xAA}, 64)
	mip1 := bytes.Repeat([]byte{0xBB}, 16)
	data := makeKTX2(VkFormatBC1RGBUnorm, 8, 8, SupercompressionNone, [][]byte{mip0, mip1}, []uint64{64, 16})
	container, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	got, err := container.LevelBytes(0)
	if err != nil {
		t.Fatalf("LevelBytes(0): %v", err)
	}
	if !bytes.Equal(got, mip0) {
		t.Errorf("mip 0 content mismatch")
	}
	got, err = container.LevelBytes(1)
	if err != nil {
		t.Fatalf("LevelBytes(1): %v", err)
	}
	if !bytes.Equal(got, mip1) {
		t.Errorf("mip 1 content mismatch")
	}
}

func TestDecompressLevel_ZstdRoundTrip(t *testing.T) {
	uncompressed := bytes.Repeat([]byte{0xAB, 0xCD, 0xEF, 0x12}, 1024) // 4KB
	encoder, err := zstd.NewWriter(nil)
	if err != nil {
		t.Fatalf("zstd writer: %v", err)
	}
	compressed := encoder.EncodeAll(uncompressed, nil)
	encoder.Close()

	data := makeKTX2(VkFormatBC1RGBUnorm, 256, 256, SupercompressionZstd, [][]byte{compressed}, []uint64{uint64(len(uncompressed))})
	container, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	got, err := container.DecompressLevel(0)
	if err != nil {
		t.Fatalf("DecompressLevel: %v", err)
	}
	if !bytes.Equal(got, uncompressed) {
		t.Errorf("decompressed payload doesn't round-trip (got %d bytes, want %d)", len(got), len(uncompressed))
	}
}

func TestDecompressLevel_BasisLZReturnsError(t *testing.T) {
	data := makeKTX2(VkFormatUndefined, 128, 128, SupercompressionBasisLZ, [][]byte{{0x00}}, []uint64{1})
	container, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if _, err := container.DecompressLevel(0); err == nil {
		t.Errorf("expected BasisLZ to be unsupported, got nil error")
	}
}

func TestMipDimensions_HalvesEachLevelFloor1(t *testing.T) {
	data := makeKTX2(VkFormatBC1RGBUnorm, 8, 4, SupercompressionNone,
		[][]byte{{0x00}, {0x00}, {0x00}, {0x00}}, []uint64{1, 1, 1, 1})
	container, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	tests := []struct {
		level   int
		wantW   int
		wantH   int
	}{
		{0, 8, 4},
		{1, 4, 2},
		{2, 2, 1},
		{3, 1, 1}, // below 1x1 still floors to 1
	}
	for _, tt := range tests {
		w, h := container.MipDimensions(tt.level)
		if w != tt.wantW || h != tt.wantH {
			t.Errorf("MipDimensions(%d) = %dx%d, want %dx%d", tt.level, w, h, tt.wantW, tt.wantH)
		}
	}
}

func TestIsBCFormat(t *testing.T) {
	cases := []struct {
		format VkFormat
		want   bool
	}{
		{VkFormatBC1RGBUnorm, true},
		{VkFormatBC3Unorm, true},
		{VkFormatBC7Srgb, true},
		{VkFormatUndefined, false},
	}
	for _, tc := range cases {
		got := Header{VkFormat: tc.format}.IsBCFormat()
		if got != tc.want {
			t.Errorf("IsBCFormat(%d) = %v, want %v", tc.format, got, tc.want)
		}
	}
}

func TestDecompressTransportWrapper_PassesThroughUnknown(t *testing.T) {
	input := []byte{0xAB, 'K', 'T', 'X'}
	out, err := DecompressTransportWrapper(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Equal(out, input) {
		t.Errorf("expected passthrough, got different bytes")
	}
}

func TestDecompressTransportWrapper_UnwrapsGzip(t *testing.T) {
	original := []byte("hello ktx2")
	var buf bytes.Buffer
	writer := gzip.NewWriter(&buf)
	if _, err := writer.Write(original); err != nil {
		t.Fatalf("gzip write: %v", err)
	}
	writer.Close()
	out, err := DecompressTransportWrapper(buf.Bytes())
	if err != nil {
		t.Fatalf("unwrap gzip: %v", err)
	}
	if !bytes.Equal(out, original) {
		t.Errorf("gzip unwrap mismatch")
	}
}

func TestDecompressTransportWrapper_UnwrapsZstd(t *testing.T) {
	original := []byte("hello ktx2 zstd")
	encoder, err := zstd.NewWriter(nil)
	if err != nil {
		t.Fatalf("zstd writer: %v", err)
	}
	compressed := encoder.EncodeAll(original, nil)
	encoder.Close()
	out, err := DecompressTransportWrapper(compressed)
	if err != nil {
		t.Fatalf("unwrap zstd: %v", err)
	}
	if !bytes.Equal(out, original) {
		t.Errorf("zstd unwrap mismatch")
	}
}
