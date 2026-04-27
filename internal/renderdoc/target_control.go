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
// swap. The renderdoc server only allows ONE connected client at a
// time — so the trigger has to ride on the same connection that
// completed the handshake; opening a fresh second connection races
// against the server's cleanup of the prior client and gets rejected
// with "busy".
//
// Strategy: walk ports 38920..38927, open a connection on each one
// in turn, do the handshake, and if the target name looks like
// Roblox Studio send TriggerCapture immediately on that connection.
// If we hit a non-Studio target first, drop it and try the next port.
func TriggerCapture() error {
	errs := make([]string, 0, targetControlLastPort-targetControlFirstPort+1)
	for port := targetControlFirstPort; port <= targetControlLastPort; port++ {
		conn, target, pid, err := dialAndCompleteHandshake(port)
		if err != nil {
			errs = append(errs, fmt.Sprintf("port %d: %v", port, err))
			continue
		}
		debug.Logf("target-control: port %d → handshake from %q pid=%d", port, target, pid)
		if !strings.Contains(strings.ToLower(target), "robloxstudio") {
			// Wrong target. Drop the connection and try the next port —
			// don't waste our trigger on something that isn't Studio.
			conn.Close()
			continue
		}
		debug.Logf("target-control: triggering on port %d (target %q pid=%d)", port, target, pid)
		err = sendTriggerCapture(conn)
		conn.Close()
		if err != nil {
			return fmt.Errorf("trigger on port %d (%q): %w", port, target, err)
		}
		return nil
	}
	debug.Logf("target-control: no Studio target found, errors: %v", errs)
	return fmt.Errorf("no Roblox Studio renderdoc target found on ports %d-%d:\n  %s",
		targetControlFirstPort, targetControlLastPort, strings.Join(errs, "\n  "))
}

// dialAndCompleteHandshake opens a TCP connection, exchanges client and
// server handshakes, and returns the still-open connection along with
// the target's name and PID. Caller is responsible for closing the
// returned connection.
//
// The connection is kept open so the caller can send commands (like
// TriggerCapture) on the same socket that completed the handshake.
// renderdoc only allows one connected client at a time, and reconnecting
// races against the server's cleanup of the prior client (gets rejected
// with ePacket_Busy), so we MUST piggyback further work on this conn.
func dialAndCompleteHandshake(port int) (net.Conn, string, uint32, error) {
	conn, err := dialAndHandshake(port)
	if err != nil {
		return nil, "", 0, err
	}
	target, pid, err := readServerHandshake(conn)
	if err != nil {
		conn.Close()
		return nil, "", 0, err
	}
	return conn, target, pid, nil
}

// sendTriggerCapture writes a TriggerCapture(numFrames=1) chunk on an
// already-handshook connection, then briefly lingers so the server's
// reader has a chance to consume the chunk before the caller closes
// the socket. Closing immediately after Write can race the server's
// read on some systems.
func sendTriggerCapture(conn net.Conn) error {
	payload := appendUint32(nil, 1)
	if err := writeChunk(conn, packetTriggerCapture, payload); err != nil {
		return fmt.Errorf("write TriggerCapture: %w", err)
	}
	time.Sleep(50 * time.Millisecond)
	return nil
}

// dialAndHandshake opens a TCP connection and writes our client
// handshake chunk. Caller is responsible for reading the server's
// handshake before issuing any further commands.
func dialAndHandshake(port int) (net.Conn, error) {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), targetControlDialTO)
	if err != nil {
		return nil, err
	}
	_ = conn.SetDeadline(time.Now().Add(targetControlIOTO))
	clientHandshake := make([]byte, 0, 32)
	clientHandshake = appendUint32(clientHandshake, targetControlMyVersion)
	clientHandshake = appendString(clientHandshake, targetControlClientName)
	clientHandshake = append(clientHandshake, 0) // forceConnection bool
	if err := writeChunk(conn, packetHandshake, clientHandshake); err != nil {
		conn.Close()
		return nil, fmt.Errorf("write handshake: %w", err)
	}
	return conn, nil
}

// readServerHandshake reads the server's handshake chunk and returns
// (targetName, pid). Errors out if the response isn't ePacket_Handshake.
func readServerHandshake(conn net.Conn) (string, uint32, error) {
	chunkID, err := readChunkHeader(conn)
	if err != nil {
		return "", 0, fmt.Errorf("read header: %w", err)
	}
	switch chunkID {
	case packetHandshake:
	case packetBusy:
		return "", 0, fmt.Errorf("target busy with another client")
	default:
		return "", 0, fmt.Errorf("unexpected chunk %d (want %d)", chunkID, packetHandshake)
	}
	serverVersion, err := readUint32(conn)
	if err != nil {
		return "", 0, fmt.Errorf("read version: %w", err)
	}
	if !isProtocolVersionSupported(serverVersion) {
		return "", 0, fmt.Errorf("unsupported protocol version %d", serverVersion)
	}
	target, err := readString(conn)
	if err != nil {
		return "", 0, fmt.Errorf("read target name: %w", err)
	}
	pid, err := readUint32(conn)
	if err != nil {
		return "", 0, fmt.Errorf("read pid: %w", err)
	}
	consumed := 8 + 4 + 4 + len(target) + 4
	if err := skipAlignment(conn, consumed); err != nil {
		return "", 0, fmt.Errorf("skip alignment: %w", err)
	}
	return target, pid, nil
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
