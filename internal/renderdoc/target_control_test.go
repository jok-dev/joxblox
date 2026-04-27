package renderdoc

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestWriteChunkEncodesHeaderAndPayloadAndPadsTo64(t *testing.T) {
	var buf bytes.Buffer
	payload := appendUint32(nil, 1) // 4-byte payload (numFrames=1)
	if err := writeChunk(&buf, packetTriggerCapture, payload); err != nil {
		t.Fatalf("writeChunk: %v", err)
	}
	got := buf.Bytes()
	if len(got) != 64 {
		t.Fatalf("chunk total len: got %d, want 64 (8 header + 4 payload + 52 pad)", len(got))
	}
	if id := binary.LittleEndian.Uint32(got[0:4]); id != packetTriggerCapture {
		t.Errorf("chunk ID: got %d, want %d", id, packetTriggerCapture)
	}
	if length := binary.LittleEndian.Uint32(got[4:8]); length != 0 {
		t.Errorf("streaming length sentinel: got %d, want 0", length)
	}
	if frames := binary.LittleEndian.Uint32(got[8:12]); frames != 1 {
		t.Errorf("payload (numFrames): got %d, want 1", frames)
	}
	for i := 12; i < 64; i++ {
		if got[i] != 0 {
			t.Errorf("padding byte %d should be 0, got 0x%02x", i, got[i])
			break
		}
	}
}

func TestWriteChunkLargerThan64BytesPadsToNextBoundary(t *testing.T) {
	var buf bytes.Buffer
	payload := make([]byte, 100) // 100-byte payload
	if err := writeChunk(&buf, packetHandshake, payload); err != nil {
		t.Fatalf("writeChunk: %v", err)
	}
	// 8 header + 100 payload = 108. Next 64-boundary = 128. So 20 bytes padding.
	if got := buf.Len(); got != 128 {
		t.Errorf("chunk total len: got %d, want 128", got)
	}
}

func TestAppendStringWritesLengthPrefixedBytes(t *testing.T) {
	got := appendString(nil, "joxblox")
	if len(got) != 4+7 {
		t.Fatalf("len: got %d, want 11", len(got))
	}
	if length := binary.LittleEndian.Uint32(got[0:4]); length != 7 {
		t.Errorf("length prefix: got %d, want 7", length)
	}
	if string(got[4:]) != "joxblox" {
		t.Errorf("payload: got %q, want %q", got[4:], "joxblox")
	}
}

func TestIsProtocolVersionSupportedAcceptsKnownRange(t *testing.T) {
	for _, v := range []uint32{2, 3, 4, 5, 6, 7, 8, 9} {
		if !isProtocolVersionSupported(v) {
			t.Errorf("version %d should be supported", v)
		}
	}
	if isProtocolVersionSupported(1) {
		t.Errorf("version 1 should not be supported")
	}
	if isProtocolVersionSupported(99) {
		t.Errorf("version 99 should not be supported")
	}
}
