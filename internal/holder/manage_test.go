package holder

import (
	"errors"
	"net"
	"os"
	"testing"
	"time"
)

// TestManagerStopsIdleHolder verifies the CLI path against a real holder
// with no sessions: list, then stop, and the socket goes away.
func TestManagerStopsIdleHolder(t *testing.T) {
	socket := startTestHolder(t)
	manager, err := DialManager(socket, 2*time.Second)
	if err != nil {
		t.Fatalf("DialManager: %v", err)
	}
	defer manager.Close()

	sessions, err := manager.Sessions()
	if err != nil || len(sessions) != 0 {
		t.Fatalf("Sessions = %v, err = %v", sessions, err)
	}
	if err := manager.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(socket); os.IsNotExist(err) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("holder socket was not removed after stop")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestManagerNoHolder verifies a missing holder is reported distinctly from
// a holder that accepts but never answers.
func TestManagerNoHolder(t *testing.T) {
	socket := shortSocketPath(t)
	manager, err := DialManager(socket, 200*time.Millisecond)
	if manager != nil {
		manager.Close()
		t.Fatal("DialManager returned a manager for a missing holder")
	}
	if !errors.Is(err, ErrNoHolder) {
		t.Fatalf("err = %v, want ErrNoHolder", err)
	}
}

// TestManagerBusyWhileAttached verifies the holder's single-client slot is
// reported as busy rather than hanging when a TUI is already attached.
func TestManagerBusyWhileAttached(t *testing.T) {
	socket := startTestHolder(t)
	attached, err := Connect(socket)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(attached.Close)

	manager, err := DialManager(socket, 200*time.Millisecond)
	if manager != nil {
		manager.Close()
		t.Fatal("DialManager returned a manager while the holder was attached")
	}
	if !errors.Is(err, ErrHolderBusy) {
		t.Fatalf("err = %v, want ErrHolderBusy", err)
	}
}

// TestManagerStopsLegacyHolder verifies an incompatible-protocol holder can
// still be listed-as-unknown and stopped, since shutdown needs no handshake.
func TestManagerStopsLegacyHolder(t *testing.T) {
	socket := shortSocketPath(t)
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	stopped := make(chan struct{})
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			f, err := readFrame(conn)
			if err != nil {
				return
			}
			if f.Type != TCtrl {
				continue
			}
			var req request
			if unmarshal(f.Payload, &req) != nil {
				continue
			}
			switch req.Op {
			case "hello":
				_ = writeFrame(conn, frame{Type: TResp, Payload: marshal(response{Req: req.Req, Err: "protocol mismatch: holder 0, client 1"})})
			case "shutdown":
				_ = writeFrame(conn, frame{Type: TResp, Payload: marshal(response{Req: req.Req})})
				close(stopped)
				_ = listener.Close()
				return
			}
		}
	}()

	manager, err := DialManager(socket, 2*time.Second)
	if err != nil {
		t.Fatalf("DialManager: %v", err)
	}
	defer manager.Close()
	if _, err := manager.Sessions(); err == nil {
		t.Fatal("Sessions succeeded against an incompatible holder")
	}
	if err := manager.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("legacy holder never received shutdown")
	}
}
