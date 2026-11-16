package startup

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
	"time"
)

func TestParseMatchesHrdxFlags(t *testing.T) {
	options, err := Parse([]string{"--cwd", "/a", "--cwd", "/b", "--agent", "claude", "--zot-bin", "/z",
		"--provider", "p", "--continue", "--persist=false", "--api=false", "--state", "", "-psn_0_123"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(options.CWDs, []string{"/a", "/b"}) || options.Agent != "claude" || options.AgentBins["zot"] != "/z" ||
		options.Persist || options.API || options.StatePath != "" || !options.Resume {
		t.Fatalf("options = %+v", options)
	}
	if got := options.ZotArgs(); !slices.Equal(got, []string{"--provider", "p", "--continue"}) {
		t.Fatalf("zot args = %v", got)
	}
	if _, err := Parse([]string{"--plugins"}, io.Discard); err == nil {
		t.Fatal("unsupported flag accepted")
	}
}

func TestExtractPathIgnoresProfileNoise(t *testing.T) {
	got := extractPath("welcome!\n__HRDX_PATH__/usr/bin:/opt/bin__HRDX_END__")
	if got != "/usr/bin:/opt/bin" {
		t.Fatalf("path = %q", got)
	}
	if extractPath("no marker") != "" {
		t.Fatal("missing marker must yield empty")
	}
}

func TestLoginPathFromShell(t *testing.T) {
	if runtime.GOOS == "windows" {
		if LoginPath("windows", "cmd.exe", time.Second) != "" {
			t.Fatal("windows must skip login shell lookup")
		}
		return
	}
	dir := t.TempDir()
	shell := filepath.Join(dir, "fake-shell")
	script := "#!/bin/sh\necho noise\nprintf '\\n__HRDX_PATH__/synthetic/bin__HRDX_END__'\n"
	if err := os.WriteFile(shell, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := LoginPath(runtime.GOOS, shell, 3*time.Second); got != "/synthetic/bin" {
		t.Fatalf("login path = %q", got)
	}
	if LoginPath(runtime.GOOS, filepath.Join(dir, "missing"), time.Second) != "" {
		t.Fatal("missing shell must yield empty")
	}
}
