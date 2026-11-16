//go:build !windows

package bridge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/patriceckhart/hrdx/desktop/internal/core"
	"github.com/patriceckhart/hrdx/internal/holder"
)

// TestHolderLauncherReattaches runs a real holder in-process, starts a shell
// session through the desktop launcher, detaches, and reattaches, the path a
// TUI to desktop handover takes.
func TestHolderLauncherReattaches(t *testing.T) {
	dir, err := os.MkdirTemp("", "hdh")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	socket := filepath.Join(dir, "h.sock")
	server := holder.NewServer(socket, "test")
	go func() { _ = server.Run() }()
	var client *holder.Client
	deadline := time.Now().Add(5 * time.Second)
	for {
		client, err = holder.Connect(socket)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal(err)
		}
		time.Sleep(20 * time.Millisecond)
	}

	output := make(chan string, 64)
	launcher := HolderLauncher{Client: client}
	spec := core.LaunchSpec{Command: "/bin/sh", CWD: dir, Cols: 80, Rows: 24}
	first, err := launcher.Launch(spec, func(data []byte) { output <- string(data) })
	if err != nil {
		t.Fatal(err)
	}
	session := first.HolderSession()
	if session == 0 {
		t.Fatal("no holder session")
	}
	first.Write([]byte("echo hrdx-$((40+2))\n"))
	waitOutput(t, output, "hrdx-42")
	first.Detach()
	client.Close()

	// A second frontend connects and reattaches by session id. The process
	// is the same one: shell state survives the handover.
	client2, err := holder.Connect(socket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client2.Shutdown(); client2.Close() })
	replay := make(chan string, 64)
	spec.Reattach = session
	second, err := HolderLauncher{Client: client2}.Launch(spec, func(data []byte) { replay <- string(data) })
	if err != nil {
		t.Fatal(err)
	}
	if second.HolderSession() != session || !second.Running() {
		t.Fatalf("reattached session %d running %v", second.HolderSession(), second.Running())
	}
	second.Write([]byte("MARK=kept; echo again-$((1+1))-$MARK\n"))
	waitOutput(t, replay, "again-2-kept")
	second.Write([]byte("echo still-$MARK\n"))
	waitOutput(t, replay, "still-kept")
	if !strings.Contains(second.PlainScreen(), "again-2") {
		t.Fatal("VT model did not track reattached output")
	}
}

func waitOutput(t *testing.T, output <-chan string, want string) {
	t.Helper()
	var seen strings.Builder
	timeout := time.After(5 * time.Second)
	for {
		select {
		case chunk := <-output:
			seen.WriteString(chunk)
			if strings.Contains(seen.String(), want) {
				return
			}
		case <-timeout:
			t.Fatalf("timed out waiting for %q", want)
		}
	}
}
