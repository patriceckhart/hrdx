package main

import (
	"slices"
	"testing"
)

// The path is always one argument and never goes through a shell, so file
// names with spaces, quotes, or shell characters cannot run anything.
func TestOpenFileCommandPassesThePathAsOneArgument(t *testing.T) {
	path := `/tmp/a b/"x"; rm -rf $HOME & .txt`
	cases := map[string][]string{
		"darwin":  {"open", "--", path},
		"windows": {"rundll32", "url.dll,FileProtocolHandler", path},
		"linux":   {"xdg-open", path},
		"freebsd": {"xdg-open", path},
	}
	for goos, want := range cases {
		if got := openFileCommand(goos, path).Args; !slices.Equal(got, want) {
			t.Errorf("%s: args %q, want %q", goos, got, want)
		}
	}
	// A name starting with a dash is not read as an option by open.
	if got := openFileCommand("darwin", "-a Calculator").Args; !slices.Equal(got, []string{"open", "--", "-a Calculator"}) {
		t.Errorf("dash path args = %q", got)
	}
}
