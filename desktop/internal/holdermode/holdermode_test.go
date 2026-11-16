package holdermode

import (
	"bytes"
	"errors"
	"go/build"
	"strings"
	"testing"
)

func TestRunRoutesOnlyHolderInvocations(t *testing.T) {
	var served string
	previous := Serve
	t.Cleanup(func() { Serve = previous })
	Serve = func(socket, version string) error { served = socket; return nil }

	var stderr bytes.Buffer
	if _, handled := Run([]string{"--cwd", "/tmp"}, "v", &stderr); handled {
		t.Fatal("ordinary launch handled as holder")
	}
	if _, handled := Run(nil, "v", &stderr); handled {
		t.Fatal("no arguments handled as holder")
	}
	if code, handled := Run([]string{"--holder"}, "v", &stderr); !handled || code != 2 {
		t.Fatalf("missing socket = %d %v", code, handled)
	}
	if code, handled := Run([]string{"--holder", "--holder-socket", "/tmp/h.sock"}, "v", &stderr); !handled || code != 0 || served != "/tmp/h.sock" {
		t.Fatalf("holder = %d %v %q", code, handled, served)
	}
	Serve = func(string, string) error { return errors.New("listen failed") }
	if code, _ := Run([]string{"--holder", "--holder-socket", "/tmp/h.sock"}, "v", &stderr); code != 1 {
		t.Fatalf("failure code = %d", code)
	}
}

// The holder must never load a GUI. Guard the import graph of this package.
func TestHolderModeImportsNoGUI(t *testing.T) {
	pkg, err := build.ImportDir(".", 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range pkg.Imports {
		if strings.Contains(path, "wailsapp") {
			t.Fatalf("holder mode imports %s", path)
		}
	}
}
