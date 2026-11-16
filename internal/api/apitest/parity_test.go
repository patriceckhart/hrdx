package apitest

import (
	"testing"

	"github.com/patriceckhart/hrdx/internal/api"
)

// A Windows-style path below a placeholder must encode like the Unix one, so
// the fixture recorded on macOS also matches on Windows.
func TestEncodeNormalizesWindowsSeparators(t *testing.T) {
	for _, tc := range []struct{ dir, path string }{
		{`/tmp/b`, `/tmp/b/missing`},
		{`C:\Temp\b`, `C:\Temp\b\missing`},
	} {
		got, err := Encode("workspace.create", api.Reply{Err: tc.path + " does not exist"}, "/unused", tc.dir)
		if err != nil {
			t.Fatal(err)
		}
		want := `{"code":"","error":"$DIRB/missing does not exist","method":"workspace.create","result":null}`
		if string(got) != want {
			t.Fatalf("dir %q\n got: %s\nwant: %s", tc.dir, got, want)
		}
	}
}
