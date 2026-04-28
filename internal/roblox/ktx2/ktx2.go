// Package ktx2 parses KTX 2.0 (Khronos Texture 2.0) containers produced by
// Roblox's modern asset-delivery pipeline. The parser extracts the fixed
// header (dimensions, format, mip count), decodes any supercompression
// scheme, and exposes raw per-mip payload bytes.
//
// Scope: what we need to replace the external tool's 1024-capped PNG path
// with the real upload-resolution data. See the Roblox investigation notes
// for why this matters (assetdelivery caps the PNG variant at 1024^2 but
// serves KTX2 at full upload resolution when the client signals
// Accept-Encoding: zstd).
//
// This implementation handles:
//   - KTX 2.0 magic/header decoding
//   - Supercompression schemes None (0) and Zstandard (2)
//   - BCn VkFormat recognition (BC1/BC3/BC7 and SRGB variants)
//
// Out of scope (needs an external transcoder — tier 4 in the plan):
//   - BasisLZ (scheme 1)
//   - UASTC payloads with vkFormat=UNDEFINED
//   - ZLIB (scheme 3 — rare in Roblox captures)
package ktx2

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// Magic is the required identifier at the start of every KTX 2.0 file.
var Magic = [12]byte{0xAB, 'K', 'T', 'X', ' ', '2', '0', 0xBB, '\r', '\n', 0x1A, '\n'}

// SupercompressionScheme enumerates the compression applied to each mip
// level's payload. Only None and Zstd are decoded natively.
type SupercompressionScheme uint32

const (
	SupercompressionNone    SupercompressionScheme = 0
	SupercompressionBasisLZ SupercompressionScheme = 1
	SupercompressionZstd    SupercompressionScheme = 2
	SupercompressionZLIB    SupercompressionScheme = 3
)

// VkFormat enumerates the Vulkan pixel formats we recognise. Zero (UNDEFINED)
// is valid KTX2 — it usually means BasisLZ/UASTC is in play and the actual
// format is described elsewhere. We surface the raw value so callers can
// decide how to proceed.
type VkFormat uint32

const (
	VkFormatUndefined       VkFormat = 0
	VkFormatBC1RGBUnorm     VkFormat = 131
	VkFormatBC1RGBSrgb      VkFormat = 132
	VkFormatBC1RGBAUnorm    VkFormat = 133
	VkFormatBC1RGBASrgb     VkFormat = 134
	VkFormatBC2Unorm        VkFormat = 135
	VkFormatBC2Srgb         VkFormat = 136
	VkFormatBC3Unorm        VkFormat = 137
	VkFormatBC3Srgb         VkFormat = 138
	VkFormatBC4Unorm        VkFormat = 139
	VkFormatBC4Snorm        VkFormat = 140
	VkFormatBC5Unorm        VkFormat = 141
	VkFormatBC5Snorm        VkFormat = 142
	VkFormatBC6HUfloat      VkFormat = 143
	VkFormatBC6HSfloat      VkFormat = 144
	VkFormatBC7Unorm        VkFormat = 145
	VkFormatBC7Srgb         VkFormat = 146
	VkFormatETC2_R8G8B8_UnormBlock VkFormat = 147
)

// Header captures the fixed 80-byte KTX2 header plus the index section.
// Offsets are relative to the start of the file.
type Header struct {
	VkFormat              VkFormat
	TypeSize              uint32
	PixelWidth            uint32
	PixelHeight           uint32
	PixelDepth            uint32
	LayerCount            uint32
	FaceCount             uint32
	LevelCount            uint32
	SupercompressionScheme SupercompressionScheme

	DFDByteOffset uint32
	DFDByteLength uint32
	KVDByteOffset uint32
	KVDByteLength uint32
	SGDByteOffset uint64
	SGDByteLength uint64
}

// Level describes one mip level's storage in the file. Offset and Length
// locate the compressed payload inside the container; UncompressedLength
// is the size after supercompression has been undone (equal to Length when
// no supercompression is applied).
type Level struct {
	ByteOffset             uint64
	ByteLength             uint64
	UncompressedByteLength uint64
}

// Container is a parsed KTX2 file. Levels are ordered largest (mip 0)
// first, matching the Khronos spec's on-disk order.
type Container struct {
	Header Header
	Levels []Level
	// Payload is the raw file bytes kept around so LevelBytes/Decompress
	// can slice into them. Pointed-to but not copied per level to keep
	// memory use low on large textures.
	Payload []byte
}

const (
	magicSize       = 12
	fixedHeaderSize = 80
	levelEntrySize  = 24
)

// ErrBadMagic is returned when the input doesn't start with the KTX2
// identifier. Callers typically treat this as "not a KTX2 file" rather
// than a fatal parse error.
var ErrBadMagic = errors.New("ktx2: bad magic bytes")

// Parse reads the fixed header and level index of a KTX2 file and returns
// a Container that retains the input bytes for later LevelBytes calls.
// Returns ErrBadMagic if the input doesn't start with the KTX2 identifier.
func Parse(data []byte) (*Container, error) {
	if len(data) < fixedHeaderSize {
		return nil, fmt.Errorf("ktx2: input too short (%d bytes, need >= %d)", len(data), fixedHeaderSize)
	}
	for i, b := range Magic {
		if data[i] != b {
			return nil, ErrBadMagic
		}
	}
	header := Header{
		VkFormat:               VkFormat(binary.LittleEndian.Uint32(data[12:16])),
		TypeSize:               binary.LittleEndian.Uint32(data[16:20]),
		PixelWidth:             binary.LittleEndian.Uint32(data[20:24]),
		PixelHeight:            binary.LittleEndian.Uint32(data[24:28]),
		PixelDepth:             binary.LittleEndian.Uint32(data[28:32]),
		LayerCount:             binary.LittleEndian.Uint32(data[32:36]),
		FaceCount:              binary.LittleEndian.Uint32(data[36:40]),
		LevelCount:             binary.LittleEndian.Uint32(data[40:44]),
		SupercompressionScheme: SupercompressionScheme(binary.LittleEndian.Uint32(data[44:48])),
		DFDByteOffset:          binary.LittleEndian.Uint32(data[48:52]),
		DFDByteLength:          binary.LittleEndian.Uint32(data[52:56]),
		KVDByteOffset:          binary.LittleEndian.Uint32(data[56:60]),
		KVDByteLength:          binary.LittleEndian.Uint32(data[60:64]),
		SGDByteOffset:          binary.LittleEndian.Uint64(data[64:72]),
		SGDByteLength:          binary.LittleEndian.Uint64(data[72:80]),
	}
	if header.PixelWidth == 0 {
		return nil, fmt.Errorf("ktx2: pixelWidth is zero")
	}
	levelCount := header.LevelCount
	if levelCount == 0 {
		// Per the spec: 0 means "recipient infers the mip count from
		// dimensions". We could compute floor(log2(max(W,H))) + 1, but in
		// practice Roblox always sets an explicit count so reject for now.
		return nil, fmt.Errorf("ktx2: levelCount=0 not supported")
	}
	levelsEnd := int(fixedHeaderSize) + int(levelCount)*levelEntrySize
	if levelsEnd > len(data) {
		return nil, fmt.Errorf("ktx2: level index overruns file (need %d bytes, have %d)", levelsEnd, len(data))
	}
	levels := make([]Level, levelCount)
	for i := uint32(0); i < levelCount; i++ {
		offset := fixedHeaderSize + int(i)*levelEntrySize
		levels[i] = Level{
			ByteOffset:             binary.LittleEndian.Uint64(data[offset : offset+8]),
			ByteLength:             binary.LittleEndian.Uint64(data[offset+8 : offset+16]),
			UncompressedByteLength: binary.LittleEndian.Uint64(data[offset+16 : offset+24]),
		}
	}
	return &Container{Header: header, Levels: levels, Payload: data}, nil
}

// LevelBytes returns the raw (still-compressed if applicable) bytes of the
// requested mip level. Use Decompress to get the uncompressed payload.
func (container *Container) LevelBytes(levelIndex int) ([]byte, error) {
	if levelIndex < 0 || levelIndex >= len(container.Levels) {
		return nil, fmt.Errorf("ktx2: level %d out of range [0, %d)", levelIndex, len(container.Levels))
	}
	level := container.Levels[levelIndex]
	start := level.ByteOffset
	end := start + level.ByteLength
	if end > uint64(len(container.Payload)) {
		return nil, fmt.Errorf("ktx2: level %d payload at [%d, %d) exceeds file length %d",
			levelIndex, start, end, len(container.Payload))
	}
	return container.Payload[start:end], nil
}

// MipDimensions returns (width, height) of the given mip level. Mip 0 is
// the base level; higher indices are successive halvings (min 1).
func (container *Container) MipDimensions(levelIndex int) (width, height int) {
	width = int(container.Header.PixelWidth) >> levelIndex
	height = int(container.Header.PixelHeight) >> levelIndex
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}
	return
}

// VkFormatName returns a short human-readable label for the recognised
// formats joxblox currently encounters in Roblox-served KTX2 files
// ("BC1", "BC3", "BC7", "ETC2"). Returns the numeric vkFormat as a string
// for unrecognised values.
func (header Header) VkFormatName() string {
	switch header.VkFormat {
	case VkFormatBC1RGBUnorm, VkFormatBC1RGBSrgb, VkFormatBC1RGBAUnorm, VkFormatBC1RGBASrgb:
		return "BC1"
	case VkFormatBC2Unorm, VkFormatBC2Srgb:
		return "BC2"
	case VkFormatBC3Unorm, VkFormatBC3Srgb:
		return "BC3"
	case VkFormatBC4Unorm, VkFormatBC4Snorm:
		return "BC4"
	case VkFormatBC5Unorm, VkFormatBC5Snorm:
		return "BC5"
	case VkFormatBC6HUfloat, VkFormatBC6HSfloat:
		return "BC6H"
	case VkFormatBC7Unorm, VkFormatBC7Srgb:
		return "BC7"
	case VkFormatETC2_R8G8B8_UnormBlock:
		return "ETC2"
	}
	return fmt.Sprintf("vkFormat=%d", header.VkFormat)
}

// HasAlpha reports whether the container's VkFormat carries an alpha
// channel. BC1-RGB and BC4/5 (single/dual-channel) are alpha-less; BC3,
// BC7, and BC1-RGBA carry alpha. ETC2 has alpha-bearing siblings but the
// plain RGB variant doesn't.
func (header Header) HasAlpha() bool {
	switch header.VkFormat {
	case VkFormatBC1RGBAUnorm, VkFormatBC1RGBASrgb,
		VkFormatBC2Unorm, VkFormatBC2Srgb,
		VkFormatBC3Unorm, VkFormatBC3Srgb,
		VkFormatBC7Unorm, VkFormatBC7Srgb:
		return true
	}
	return false
}

// IsBCFormat reports whether this container's VkFormat is one of the BCn
// compressed block formats (BC1-BC7). Callers can use this to decide
// whether to route to an existing BC decoder.
func (header Header) IsBCFormat() bool {
	switch header.VkFormat {
	case VkFormatBC1RGBUnorm, VkFormatBC1RGBSrgb,
		VkFormatBC1RGBAUnorm, VkFormatBC1RGBASrgb,
		VkFormatBC2Unorm, VkFormatBC2Srgb,
		VkFormatBC3Unorm, VkFormatBC3Srgb,
		VkFormatBC4Unorm, VkFormatBC4Snorm,
		VkFormatBC5Unorm, VkFormatBC5Snorm,
		VkFormatBC6HUfloat, VkFormatBC6HSfloat,
		VkFormatBC7Unorm, VkFormatBC7Srgb:
		return true
	}
	return false
}
