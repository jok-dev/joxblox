// RenderDoc target-control client. Each process with the renderdoc
// capture library injected listens on a TCP port in 38920..38927 for
// commands like "trigger a capture on the next frame swap." This file
// implements just enough of the protocol to send a TriggerCapture from
// outside Studio — replaces the previous SendInput-based approach,
// which RenderDoc filters because Windows tags injected keystrokes
// with the LLKHF_INJECTED flag and renderdoc's hook ignores them.
//
// Protocol reference: renderdoc/core/target_control.cpp in the
// renderdoc repo. Streaming-mode chunk format: uint32 chunkID, uint32
// length-placeholder (always 0 in streaming), payload bytes, then zero
// padding to the next 64-byte boundary. Strings are uint32 length +
// bytes (no null terminator).

package renderdoc

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"joxblox/internal/debug"
)

const (
	targetControlFirstPort  = 38920
	targetControlLastPort   = 38927
	targetControlMyVersion  = 9
	targetControlClientName = "joxblox"
	targetControlAlignment  = 64
	targetControlDialTO     = 250 * time.Millisecond
	targetControlIOTO       = 2 * time.Second

	packetHandshake      = 2
	packetBusy           = 3
	packetTriggerCapture = 6
)

// TriggerCapture asks any RenderDoc-attached process listening on the
// local target-control protocol to take a frame capture on the next
// swap. Tries each port in 38920..38927 (renderdoc's reserved range);
// returns nil on the first successful trigger. Returns an error
// summary when no target accepts a connection or completes a handshake.
func TriggerCapture() error {
	errs := make([]string, 0, targetControlLastPort-targetControlFirstPort+1)
	for port := targetControlFirstPort; port <= targetControlLastPort; port++ {
		err := triggerCaptureOnPort(port)
		if err == nil {
			debug.Logf("target-control: port %d → TriggerCapture sent", port)
			return nil
		}
		errs = append(errs, fmt.Sprintf("port %d: %v", port, err))
	}
	debug.Logf("target-control: no port worked, errors: %v", errs)
	return fmt.Errorf("no RenderDoc target accepted on ports %d-%d:\n  %s",
		targetControlFirstPort, targetControlLastPort, strings.Join(errs, "\n  "))
}

func triggerCaptureOnPort(port int) error {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), targetControlDialTO)
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(targetControlIOTO))

	// 1. Send our handshake: version + clientName + forceConnection=false.
	clientHandshake := make([]byte, 0, 32)
	clientHandshake = appendUint32(clientHandshake, targetControlMyVersion)
	clientHandshake = appendString(clientHandshake, targetControlClientName)
	clientHandshake = append(clientHandshake, 0) // forceConnection bool, 1 byte
	if err := writeChunk(conn, packetHandshake, clientHandshake); err != nil {
		return fmt.Errorf("write handshake: %w", err)
	}

	// 2. Read server's handshake to confirm we're talking to a renderdoc target.
	chunkID, err := readChunkHeader(conn)
	if err != nil {
		return fmt.Errorf("read server handshake header: %w", err)
	}
	switch chunkID {
	case packetHandshake:
		// proceed
	case packetBusy:
		return fmt.Errorf("renderdoc target on port %d is busy with another client", port)
	default:
		return fmt.Errorf("unexpected packet %d from renderdoc target on port %d", chunkID, port)
	}
	serverVersion, err := readUint32(conn)
	if err != nil {
		return fmt.Errorf("read server version: %w", err)
	}
	if !isProtocolVersionSupported(serverVersion) {
		return fmt.Errorf("renderdoc target reports protocol version %d, joxblox knows %d",
			serverVersion, targetControlMyVersion)
	}
	// Skip the rest of the server handshake payload (target name + pid)
	// + chunk alignment. We've consumed 8 (chunk header) + 4 (version) so
	// far. Read string + uint32, then align.
	target, err := readString(conn)
	if err != nil {
		return fmt.Errorf("read server target name: %w", err)
	}
	if _, err := readUint32(conn); err != nil {
		return fmt.Errorf("read server pid: %w", err)
	}
	consumed := 8 + 4 + 4 + len(target) + 4
	if err := skipAlignment(conn, consumed); err != nil {
		return fmt.Errorf("skip server handshake alignment: %w", err)
	}

	// 3. Send TriggerCapture: numFrames=1.
	triggerPayload := appendUint32(nil, 1)
	if err := writeChunk(conn, packetTriggerCapture, triggerPayload); err != nil {
		return fmt.Errorf("write TriggerCapture: %w", err)
	}
	return nil
}

// writeChunk writes a streaming-mode chunk: uint32 chunkID + uint32
// length-placeholder (zero) + payload + zero padding to the next
// 64-byte boundary. Renderdoc's reader expects exactly this layout
// for chunks emitted by a streaming-mode WriteSerialiser.
func writeChunk(w io.Writer, chunkID uint32, payload []byte) error {
	header := make([]byte, 8)
	binary.LittleEndian.PutUint32(header[0:4], chunkID)
	binary.LittleEndian.PutUint32(header[4:8], 0) // streaming length sentinel
	if _, err := w.Write(header); err != nil {
		return err
	}
	if len(payload) > 0 {
		if _, err := w.Write(payload); err != nil {
			return err
		}
	}
	total := 8 + len(payload)
	pad := (targetControlAlignment - (total % targetControlAlignment)) % targetControlAlignment
	if pad > 0 {
		if _, err := w.Write(make([]byte, pad)); err != nil {
			return err
		}
	}
	return nil
}

// readChunkHeader reads the 8-byte chunk header (chunkID + length).
// Returns the chunkID with flag bits stripped. Length is discarded —
// in streaming mode it's always 0 and the schema dictates payload size.
func readChunkHeader(r io.Reader) (uint32, error) {
	var hdr [8]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return 0, err
	}
	chunkID := binary.LittleEndian.Uint32(hdr[0:4]) & 0x0000ffff
	return chunkID, nil
}

// skipAlignment reads padding bytes to advance the stream to the next
// targetControlAlignment-byte boundary, given how many bytes have been
// consumed since the start of the chunk.
func skipAlignment(r io.Reader, consumed int) error {
	pad := (targetControlAlignment - (consumed % targetControlAlignment)) % targetControlAlignment
	if pad == 0 {
		return nil
	}
	scratch := make([]byte, pad)
	_, err := io.ReadFull(r, scratch)
	return err
}

func appendUint32(dst []byte, v uint32) []byte {
	var buf [4]byte
	binary.LittleEndian.PutUint32(buf[:], v)
	return append(dst, buf[:]...)
}

func appendString(dst []byte, s string) []byte {
	dst = appendUint32(dst, uint32(len(s)))
	dst = append(dst, s...)
	return dst
}

func readUint32(r io.Reader) (uint32, error) {
	var buf [4]byte
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint32(buf[:]), nil
}

func readString(r io.Reader) (string, error) {
	length, err := readUint32(r)
	if err != nil {
		return "", err
	}
	if length > 1<<20 {
		return "", fmt.Errorf("implausible string length %d", length)
	}
	if length == 0 {
		return "", nil
	}
	buf := make([]byte, length)
	if _, err := io.ReadFull(r, buf); err != nil {
		return "", err
	}
	return string(buf), nil
}

// isProtocolVersionSupported mirrors renderdoc's IsProtocolVersionSupported
// in target_control.cpp — accepts any minor revision the renderdoc team
// has shipped, so a slightly newer/older renderdoc still pairs with us.
func isProtocolVersionSupported(version uint32) bool {
	switch version {
	case 2, 3, 4, 5, 6, 7, 8, 9:
		return true
	}
	return version == targetControlMyVersion
}
