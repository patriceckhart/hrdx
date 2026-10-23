package holder

import (
	"errors"
	"net"
	"os"
	"testing"
	"time"
)

// spawnInProcess replaces the detached hrdx binary with an in-process holder.
func spawnInProcess(t *testing.T, spawned *int) func(string) error {
	t.Helper()
	return func(socket string) error {
		*spawned++
		server := NewServer(socket, "test")
		go func() { _ = server.Run() }()
		t.Cleanup(func() {
			if client, err := Connect(socket); err == nil {
				client.Shutdown()
				client.Close()
			}
		})
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if conn, err := net.DialTimeout("unix", socket, 200*time.Millisecond); err == nil {
				conn.Close()
				return nil
			}
			time.Sleep(20 * time.Millisecond)
		}
		return errors.New("test holder did not come up")
	}
}

// Replacing a holder that serves another terminal would orphan its sessions.
func TestConnectOrSpawnKeepsBusyHolder(t *testing.T) {
	socket := startTestHolder(t)
	first, err := Connect(socket)
	if err != nil {
		t.Fatalf("first Connect: %v", err)
	}
	t.Cleanup(first.Close)
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	env := append(os.Environ(), "HRDX_HOLDER_ECHO_HELPER=1")
	session, err := first.Start(path, []string{"-test.run=^TestHolderEchoHelper$"}, t.TempDir(), env, 80, 24)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	// Kill the helper before Windows removes its working directory.
	t.Cleanup(func() {
		if cleanup, connectErr := Connect(socket); connectErr == nil {
			_, _ = cleanup.call(request{Op: "kill", Session: session})
			cleanup.Close()
		}
	})

	spawned := 0
	second, err := connectOrSpawn(socket, 300*time.Millisecond, spawnInProcess(t, &spawned))
	if second != nil {
		second.Close()
		t.Fatal("second terminal attached to a busy holder")
	}
	if !errors.Is(err, ErrHolderBusy) {
		t.Fatalf("err = %v, want ErrHolderBusy", err)
	}
	if spawned != 0 {
		t.Fatalf("spawned %d replacement holders, want 0", spawned)
	}
	if _, statErr := os.Stat(socket); statErr != nil {
		t.Fatalf("busy holder's socket was removed: %v", statErr)
	}

	first.Close()
	var again *Client
	deadline := time.Now().Add(5 * time.Second)
	for {
		again, err = connectOrSpawn(socket, time.Second, spawnInProcess(t, &spawned))
		if err == nil || time.Now().After(deadline) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("reconnect after the first terminal quit: %v", err)
	}
	defer again.Close()
	if spawned != 0 {
		t.Fatalf("spawned %d holders on reconnect, want 0", spawned)
	}
	sessions, err := again.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	found := false
	for _, info := range sessions {
		found = found || info.ID == session
	}
	if !found {
		t.Fatalf("session %d missing after reconnect: %+v", session, sessions)
	}
}

// Two concurrent `hrdx stop` runs both send shutdown.
func TestServerShutdownIsIdempotent(t *testing.T) {
	server := NewServer(shortSocketPath(t), "test")
	server.shutdown()
	server.shutdown()
	select {
	case <-server.done:
	default:
		t.Fatal("shutdown did not stop the server")
	}
}

func TestConnectOrSpawnStartsHolderWhenNoneRuns(t *testing.T) {
	socket := shortSocketPath(t)
	spawned := 0
	client, err := connectOrSpawn(socket, time.Second, spawnInProcess(t, &spawned))
	if err != nil {
		t.Fatalf("connectOrSpawn: %v", err)
	}
	defer client.Close()
	if spawned != 1 {
		t.Fatalf("spawned %d holders, want 1", spawned)
	}
}

func TestConnectOrSpawnReplacesStaleSocket(t *testing.T) {
	socket := shortSocketPath(t)
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	if unixListener, ok := listener.(*net.UnixListener); ok {
		unixListener.SetUnlinkOnClose(false)
	}
	listener.Close()
	if _, err := os.Stat(socket); err != nil {
		t.Skipf("platform removed the socket file on close: %v", err)
	}

	spawned := 0
	client, err := connectOrSpawn(socket, time.Second, spawnInProcess(t, &spawned))
	if err != nil {
		t.Fatalf("connectOrSpawn: %v", err)
	}
	defer client.Close()
	if spawned != 1 {
		t.Fatalf("spawned %d holders, want 1", spawned)
	}
}
