package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/patriceckhart/hrdx/internal/continuous"
)

func runContinuousCLI(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := runContinuous(args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func TestContinuousCLIAddListRemove(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	socket := filepath.Join(dir, "host.sock")

	if code, _, stderr := runContinuousCLI(t, "add", "--state", statePath, "--id", "ext"); code != 2 || !strings.Contains(stderr, "required") {
		t.Fatalf("add without address = %d %q", code, stderr)
	}
	if code, _, stderr := runContinuousCLI(t, "add", "--state", statePath, "--id", "r", "--address", "example.com:7000"); code != 2 || !strings.Contains(stderr, "tls_ca") {
		t.Fatalf("remote add without TLS = %d %q", code, stderr)
	}
	if code, _, stderr := runContinuousCLI(t, "add", "--state", statePath, "--id", "r", "--address", "example.com:7000",
		"--tls-ca", filepath.Join(dir, "ca.pem"), "--token-file", filepath.Join(dir, "token")); code != 0 {
		t.Fatalf("remote add with TLS = %d %q", code, stderr)
	}
	if code, _, _ := runContinuousCLI(t, "remove", "--state", statePath, "--id", "r"); code != 0 {
		t.Fatal("remove remote failed")
	}
	if code, _, stderr := runContinuousCLI(t, "add", "--state", statePath, "--id", "ext", "--address", socket, "--name", "Example"); code != 0 {
		t.Fatalf("add = %d %q", code, stderr)
	}
	profiles, problems := continuous.LoadProfiles(dir)
	if len(problems) != 0 || len(profiles) != 1 || profiles[0].Address != socket || profiles[0].Managed {
		t.Fatalf("profiles = %+v %v", profiles, problems)
	}

	code, stdout, _ := runContinuousCLI(t, "hosts", "--state", statePath, "--json")
	if code != 0 {
		t.Fatalf("hosts = %d", code)
	}
	var listed struct {
		Hosts []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"hosts"`
	}
	if err := json.Unmarshal([]byte(stdout), &listed); err != nil || len(listed.Hosts) != 1 || listed.Hosts[0].Status != "unreachable" {
		t.Fatalf("hosts output = %s (%v)", stdout, err)
	}

	if code, _, stderr := runContinuousCLI(t, "stop", "--state", statePath, "--id", "ext"); code != 1 || !strings.Contains(stderr, "not managed") {
		t.Fatalf("stop external = %d %q", code, stderr)
	}
	if code, _, _ := runContinuousCLI(t, "remove", "--state", statePath, "--id", "ext"); code != 0 {
		t.Fatalf("remove = %d", code)
	}
	if code, _, _ := runContinuousCLI(t, "remove", "--state", statePath, "--id", "ext"); code != 1 {
		t.Fatalf("second remove = %d, want 1", code)
	}
}

func TestContinuousCLIUsage(t *testing.T) {
	if code, stdout, _ := runContinuousCLI(t); code != 2 || !strings.Contains(stdout, "usage: hrdx continuous") {
		t.Fatalf("bare = %d %q", code, stdout)
	}
	if code, _, _ := runContinuousCLI(t, "--help"); code != 0 {
		t.Fatalf("help = %d", code)
	}
	if code, _, _ := runContinuousCLI(t, "bogus", "--state", filepath.Join(t.TempDir(), "s.json")); code != 2 {
		t.Fatalf("unknown command = %d", code)
	}
}
