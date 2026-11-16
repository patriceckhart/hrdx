package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/patriceckhart/hrdx/internal/api"
	"github.com/patriceckhart/hrdx/internal/paths"
)

func TestPluginCLIRuntimeFallback(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("XDG_RUNTIME_DIR is not used on Windows")
	}
	runtimeDir, err := os.MkdirTemp(os.TempDir(), "hr-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(runtimeDir) })
	t.Setenv("XDG_RUNTIME_DIR", runtimeDir)
	statePath := filepath.Join(t.TempDir(), "state.json")
	sockets, err := paths.Runtime(statePath, runtime.GOOS, runtimeDir)
	if err != nil {
		t.Fatal(err)
	}
	server := api.NewServer(sockets.API, func(request api.Request) {
		request.Reply <- api.Reply{Data: map[string]bool{"accepted": true}}
	}, nil)
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Close)
	var stdout, stderr bytes.Buffer
	if code := runPlugins([]string{"status", "--state", statePath}, &stdout, &stderr); code != 0 {
		t.Fatalf("runtime fallback: code=%d output=%s errors=%s", code, &stdout, &stderr)
	}
}

func TestPluginCLI(t *testing.T) {
	base := t.TempDir()
	requests := make(chan api.Request, 4)
	server := api.NewServer(filepath.Join(base, "hrdx.sock"), func(request api.Request) {
		requests <- request
		request.Reply <- api.Reply{Data: map[string]bool{"accepted": true}}
	}, nil)
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Close)
	for _, action := range []string{"status", "start", "stop", "restart", "reload"} {
		args := []string{action, "--state", filepath.Join(base, "state.json")}
		if action != "status" {
			args = append(args, "--id", "example.test")
		}
		var output, errors bytes.Buffer
		if code := runPlugins(args, &output, &errors); code != 0 {
			t.Fatalf("%s: code=%d output=%s errors=%s", action, code, &output, &errors)
		}
		request := <-requests
		if action == "status" {
			if request.Method != "plugins.status" {
				t.Fatal(request.Method)
			}
		} else {
			params, ok := request.Payload.(api.PluginControl)
			if !ok || params.Action != action || params.Plugin != "example.test" {
				t.Fatalf("payload: %+v", request.Payload)
			}
		}
	}
}
