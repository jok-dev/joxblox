package extractor

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"

	"joxblox/internal/debug"
	"joxblox/internal/procutil"
)

// DiffPropValue is a single typed property snapshot from the Rust diff
// subcommand. Value is left as raw JSON so the UI layer can render it
// per-type without losing precision (e.g. CFrame components).
type DiffPropValue struct {
	Type  string          `json:"type"`
	Value json.RawMessage `json:"value"`
}

// DiffInstance describes one instance that exists only on one side of the
// diff (the "added" or "removed" buckets).
type DiffInstance struct {
	Path       string                   `json:"path"`
	Class      string                   `json:"class"`
	Name       string                   `json:"name"`
	Properties map[string]DiffPropValue `json:"properties"`
}

// DiffPropertyChange is a single property that differs between two
// instances that exist on both sides. The "a" side is fileA, "b" is fileB.
// Either side may be {Type: "Absent"} when the property only exists on
// one side.
type DiffPropertyChange struct {
	Name string        `json:"name"`
	Type string        `json:"type"`
	A    DiffPropValue `json:"a"`
	B    DiffPropValue `json:"b"`
}

// DiffChangedInstance is an instance present on both sides but with at
// least one differing property.
type DiffChangedInstance struct {
	Path            string               `json:"path"`
	Class           string               `json:"class"`
	Name            string               `json:"name"`
	PropertyChanges []DiffPropertyChange `json:"property_changes"`
}

// DiffResult is the top-level payload returned by `rbxl-id-extractor diff`.
type DiffResult struct {
	Added   []DiffInstance        `json:"added"`
	Removed []DiffInstance        `json:"removed"`
	Changed []DiffChangedInstance `json:"changed"`
}

// DiffSession is a long-running Rust process that holds both DOMs in
// memory after a Compare. Subsequent copy requests reuse the parsed
// DOMs so right-click "Copy" is a near-instant `to_writer` on a
// subtree instead of re-parsing the source rbxl every time.
//
// Wire protocol on the process's stdout — every response is framed:
//
//	[ status: 1 byte (0 = OK, 1 = Error) ][ length: 4 LE bytes ][ payload: N bytes ]
//
// Commands on stdin are line-delimited JSON. See run_diff_session_loop
// in tools/rbxl-id-extractor/src/main.rs for the Rust side.
type DiffSession struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader

	cancel context.CancelFunc

	result DiffResult

	mu     sync.Mutex // serialises CopyInstance + Close
	closed bool
}

// StartDiffSession spawns the Rust `diff` subcommand with both files
// and immediately reads the initial diff result. The returned session
// is ready for `Result` and `CopyInstance` calls.
//
// Callers MUST call Close when done — otherwise the Rust child stays
// alive until joxblox exits.
func StartDiffSession(fileA, fileB string, ignoreScripts bool) (*DiffSession, error) {
	if strings.TrimSpace(fileA) == "" || strings.TrimSpace(fileB) == "" {
		return nil, fmt.Errorf("both file paths are required")
	}
	debug.Logf("Rusty Asset Tool diff session: a=%s b=%s ignoreScripts=%t", fileA, fileB, ignoreScripts)

	extraArgs := []string{fileB}
	if ignoreScripts {
		extraArgs = append(extraArgs, "--ignore-scripts")
	}
	commandName, commandArgs, _, resolveErr := resolveSubcommand("diff", fileA, extraArgs...)
	if resolveErr != nil {
		return nil, resolveErr
	}

	commandContext, cancel := context.WithCancel(context.Background())
	command := exec.CommandContext(commandContext, commandName, commandArgs...)
	procutil.HideWindow(command)
	command.Env = appendCargoEnv(os.Environ())

	stdinPipe, pipeErr := command.StdinPipe()
	if pipeErr != nil {
		cancel()
		return nil, fmt.Errorf("stdin pipe: %w", pipeErr)
	}
	stdoutPipe, pipeErr := command.StdoutPipe()
	if pipeErr != nil {
		cancel()
		return nil, fmt.Errorf("stdout pipe: %w", pipeErr)
	}
	// Stderr is collected into a buffered file-like sink so we can
	// surface compile/load errors in the same dialog used elsewhere.
	stderrPipe, pipeErr := command.StderrPipe()
	if pipeErr != nil {
		cancel()
		return nil, fmt.Errorf("stderr pipe: %w", pipeErr)
	}
	if startErr := command.Start(); startErr != nil {
		cancel()
		return nil, fmt.Errorf("start: %w", startErr)
	}

	// Drain stderr in the background so the child never blocks on a
	// full stderr pipe; capture into a buffer for later inspection.
	var stderrBuf strings.Builder
	stderrDone := make(chan struct{})
	go func() {
		defer close(stderrDone)
		_, _ = io.Copy(&stderrBuf, stderrPipe)
	}()

	session := &DiffSession{
		cmd:    command,
		stdin:  stdinPipe,
		stdout: bufio.NewReader(stdoutPipe),
		cancel: cancel,
	}

	// First frame: the diff result JSON. If the Rust process died
	// during parse/diff, the framed read fails and we surface stderr
	// in the error so the user sees the actual cause.
	status, payload, frameErr := session.readFrame()
	if frameErr != nil {
		cancel()
		_ = command.Wait()
		<-stderrDone
		if stderrText := strings.TrimSpace(stderrBuf.String()); stderrText != "" {
			return nil, fmt.Errorf("Rusty Asset Tool diff failed: %s", stderrText)
		}
		return nil, fmt.Errorf("Rusty Asset Tool diff failed: %w", frameErr)
	}
	if status != frameOK {
		cancel()
		_ = command.Wait()
		<-stderrDone
		return nil, fmt.Errorf("Rusty Asset Tool diff failed: %s", string(payload))
	}
	if jsonErr := json.Unmarshal(payload, &session.result); jsonErr != nil {
		cancel()
		_ = command.Wait()
		<-stderrDone
		return nil, fmt.Errorf("Rusty Asset Tool diff JSON parse failed: %w", jsonErr)
	}
	debug.Logf(
		"Diff session ready: +%d -%d ~%d",
		len(session.result.Added), len(session.result.Removed), len(session.result.Changed),
	)
	return session, nil
}

// Result returns the diff payload the session loaded at startup. It's
// safe to call repeatedly; the diff itself doesn't change over the
// session's lifetime.
func (s *DiffSession) Result() DiffResult { return s.result }

// CopyInstance serialises a single instance subtree from the
// previously-loaded DOMs into .rbxm binary bytes. `side` is "a" or
// "b" — the same files that were passed to StartDiffSession in that
// order. `path` is the dot-separated instance path the diff result
// reports.
func (s *DiffSession) CopyInstance(side, path string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, fmt.Errorf("diff session is closed")
	}

	command := struct {
		Op   string `json:"op"`
		File string `json:"file"`
		Path string `json:"path"`
	}{
		Op: "copy", File: side, Path: path,
	}
	commandJSON, marshalErr := json.Marshal(&command)
	if marshalErr != nil {
		return nil, fmt.Errorf("encode copy command: %w", marshalErr)
	}
	commandJSON = append(commandJSON, '\n')
	if _, writeErr := s.stdin.Write(commandJSON); writeErr != nil {
		return nil, fmt.Errorf("send copy command: %w", writeErr)
	}

	status, payload, readErr := s.readFrame()
	if readErr != nil {
		return nil, fmt.Errorf("read copy response: %w", readErr)
	}
	if status != frameOK {
		return nil, fmt.Errorf("Rusty Asset Tool copy failed: %s", string(payload))
	}
	return payload, nil
}

// Close shuts down the underlying Rust process. After Close returns
// the session is unusable; CopyInstance returns an error.
func (s *DiffSession) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	// Try a graceful shutdown first — the Rust loop exits when it
	// reads a "shutdown" command or hits EOF on stdin. Either is fine;
	// the cancel below is the hard fallback if the child is wedged.
	_, _ = s.stdin.Write([]byte("{\"op\":\"shutdown\"}\n"))
	_ = s.stdin.Close()
	s.cancel()
	_ = s.cmd.Wait()
}

const (
	frameOK  byte = 0x00
	frameErr byte = 0x01
)

// readFrame parses one [status][length][payload] frame from the Rust
// process. Returns (status, payload, error). The payload buffer is
// owned by the caller.
func (s *DiffSession) readFrame() (byte, []byte, error) {
	header := make([]byte, 5)
	if _, err := io.ReadFull(s.stdout, header); err != nil {
		return 0, nil, err
	}
	status := header[0]
	length := binary.LittleEndian.Uint32(header[1:5])
	payload := make([]byte, length)
	if length > 0 {
		if _, err := io.ReadFull(s.stdout, payload); err != nil {
			return 0, nil, err
		}
	}
	_ = frameErr // referenced for symmetry with the Rust constant
	return status, payload, nil
}
