package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// A stub holder speaks the holder wire protocol just well enough for the
// stop command: hello, list, and shutdown. It keeps the CLI test free of
// PTYs and real shells.

const (
	stubCtrl = 1
	stubResp = 2
)

func writeStubFrame(conn net.Conn, frameType byte, payload []byte) error {
	head := make([]byte, 13)
	binary.BigEndian.PutUint32(head[0:4], uint32(len(payload)))
	head[4] = frameType
	if _, err := conn.Write(head); err != nil {
		return err
	}
	_, err := conn.Write(payload)
	return err
}

func readStubFrame(conn net.Conn) (byte, []byte, error) {
	head := make([]byte, 13)
	if _, err := io.ReadFull(conn, head); err != nil {
		return 0, nil, err
	}
	length := binary.BigEndian.Uint32(head[0:4])
	payload := make([]byte, length)
	if length > 0 {
		if _, err := io.ReadFull(conn, payload); err != nil {
			return 0, nil, err
		}
	}
	return head[4], payload, nil
}

type stubHolder struct {
	sessions []map[string]any
	mismatch bool // reply to hello with an incompatible protocol error

	mu      sync.Mutex
	stopped bool
}

func (s *stubHolder) wasStopped() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopped
}

func (s *stubHolder) serve(conn net.Conn, listener net.Listener) {
	defer conn.Close()
	for {
		frameType, payload, err := readStubFrame(conn)
		if err != nil {
			return
		}
		if frameType != stubCtrl {
			continue
		}
		var req struct {
			Req int64  `json:"req"`
			Op  string `json:"op"`
		}
		if json.Unmarshal(payload, &req) != nil {
			continue
		}
		var reply []byte
		switch req.Op {
		case "hello":
			hello := map[string]any{"req": req.Req, "protocol": 1, "version": "test"}
			if s.mismatch {
				hello = map[string]any{"req": req.Req, "err": "protocol mismatch: holder 0, client 1"}
			}
			reply, _ = json.Marshal(hello)
		case "list":
			reply, _ = json.Marshal(map[string]any{"req": req.Req, "sessions": s.sessions})
		case "shutdown":
			s.mu.Lock()
			s.stopped = true
			s.mu.Unlock()
			reply, _ = json.Marshal(map[string]any{"req": req.Req})
			_ = writeStubFrame(conn, stubResp, reply)
			_ = listener.Close()
			return
		default:
			continue
		}
		if writeStubFrame(conn, stubResp, reply) != nil {
			return
		}
	}
}

// startStubHolder listens on the legacy holder socket for statePath. The
// state directory is created with os.MkdirTemp because a deep t.TempDir path
// can exceed the macOS sun_path limit.
func startStubHolder(t *testing.T, statePath string, sessions ...map[string]any) *stubHolder {
	t.Helper()
	return startStubHolderMismatch(t, statePath, false, sessions...)
}

func startStubHolderMismatch(t *testing.T, statePath string, mismatch bool, sessions ...map[string]any) *stubHolder {
	t.Helper()
	socket := legacySocketPaths(statePath).holder
	if err := os.MkdirAll(filepath.Dir(socket), 0o755); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	stub := &stubHolder{sessions: sessions, mismatch: mismatch}
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			stub.serve(conn, listener)
		}
	}()
	t.Cleanup(func() {
		_ = listener.Close()
		_ = os.Remove(socket)
	})
	return stub
}

func shortStateDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "hrdx")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// statePathFor returns a fresh state path plus a short containing directory.
func statePathFor(t *testing.T) string {
	t.Helper()
	return filepath.Join(shortStateDir(t), "state.json")
}

func TestStopNoHolder(t *testing.T) {
	statePath := statePathFor(t)
	var stdout, stderr bytes.Buffer
	if code := stopHolder(statePath, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "No session holder") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestStopDeclinesByDefault(t *testing.T) {
	for _, answer := range []string{"", "\n", "n\n", "no\n", "x\n"} {
		t.Run("answer="+strings.TrimSpace(answer), func(t *testing.T) {
			statePath := statePathFor(t)
			stub := startStubHolder(t, statePath, map[string]any{"id": 7, "command": "/bin/sh", "cwd": "/tmp/project", "running": true})

			var stdout, stderr bytes.Buffer
			if code := stopHolder(statePath, strings.NewReader(answer), &stdout, &stderr); code != 1 {
				t.Fatalf("code = %d, stdout = %q, stderr = %q", code, stdout.String(), stderr.String())
			}
			if !strings.Contains(stderr.String(), "[y/N]") || !strings.Contains(stderr.String(), "/bin/sh") {
				t.Fatalf("stderr = %q", stderr.String())
			}
			if stub.wasStopped() {
				t.Fatal("declined prompt still sent shutdown")
			}
		})
	}
}

func TestStopAcceptsYes(t *testing.T) {
	for _, answer := range []string{"y\n", "Y\n", " y \n"} {
		t.Run("answer="+strings.TrimSpace(answer), func(t *testing.T) {
			statePath := statePathFor(t)
			stub := startStubHolder(t, statePath, map[string]any{"id": 7, "command": "/bin/sh", "cwd": "/tmp/project", "running": true})

			var stdout, stderr bytes.Buffer
			if code := stopHolder(statePath, strings.NewReader(answer), &stdout, &stderr); code != 0 {
				t.Fatalf("code = %d, stderr = %q", code, stderr.String())
			}
			if !stub.wasStopped() {
				t.Fatal("accepted prompt did not send shutdown")
			}
			if !strings.Contains(stdout.String(), "killed 1 held session") {
				t.Fatalf("stdout = %q", stdout.String())
			}
		})
	}
}

func TestStopEmptyHolderDoesNotPrompt(t *testing.T) {
	statePath := statePathFor(t)
	stub := startStubHolder(t, statePath)

	var stdout, stderr bytes.Buffer
	if code := stopHolder(statePath, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, stderr.String())
	}
	if !stub.wasStopped() {
		t.Fatal("stop did not send shutdown to an idle holder")
	}
	if strings.Contains(stderr.String(), "[y/N]") {
		t.Fatalf("prompted without sessions: %q", stderr.String())
	}
}

func TestStopRefusesWhenSessionsUnlistable(t *testing.T) {
	statePath := statePathFor(t)
	stub := startStubHolderMismatch(t, statePath, true, map[string]any{"id": 7, "command": "/bin/sh", "running": true})

	var stdout, stderr bytes.Buffer
	if code := stopHolder(statePath, strings.NewReader("y\n"), &stdout, &stderr); code != 1 {
		t.Fatalf("code = %d, stderr = %q", code, stderr.String())
	}
	if stub.wasStopped() {
		t.Fatal("unlistable holder was stopped")
	}
}

func TestStopUsage(t *testing.T) {
	var stdout, stderr bytes.Buffer
	// runStop rejects anything but a bare subcommand before it dials.
	for _, args := range [][]string{{"--force"}, {"--state", "/tmp/x"}, {"extra"}} {
		if code := runStop(args, strings.NewReader("y\n"), &stdout, &stderr); code != 2 {
			t.Fatalf("args %v: code = %d", args, code)
		}
	}
}
