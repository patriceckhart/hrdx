//go:build windows

package continuous

import (
	"context"
	"strings"
	"testing"
	"time"
)

// The pipe name must match zot's derivation for the same socket path, or a
// profile address would never reach the host on Windows.
func TestLocalEndpointNameMatchesZotDerivation(t *testing.T) {
	a := LocalEndpointName(`C:\Users\dev\store\host.sock`)
	if !strings.HasPrefix(a, `\\.\pipe\zot-continuous-`) || !strings.HasSuffix(a, "-host-sock") {
		t.Fatalf("pipe name = %q", a)
	}
	if b := LocalEndpointName(`c:\users\DEV\store\host.sock`); b != a {
		t.Fatalf("pipe names differ by case: %q vs %q", a, b)
	}
	if c := LocalEndpointName(`C:\Users\dev\other\host.sock`); c == a {
		t.Fatal("different stores share a pipe name")
	}
	if raw := `\\.\pipe\custom`; LocalEndpointName(raw) != raw {
		t.Fatal("explicit pipe names must pass through")
	}
}

func TestDialMissingPipeFails(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := dialLocal(ctx, `C:\nonexistent-hrdx-test\host.sock`); err == nil {
		t.Fatal("dialed a pipe that does not exist")
	}
}
