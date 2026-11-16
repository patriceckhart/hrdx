// Package holdermode runs hrdx Desktop as a session holder. main calls Run
// before anything else so a holder spawned by the desktop binary never
// creates a window, a dock icon, or a webview. This package must never import
// Wails or anything that initializes a GUI.
package holdermode

import (
	"flag"
	"fmt"
	"io"

	"github.com/patriceckhart/hrdx/internal/holder"
)

// Serve runs a holder on a socket until it shuts down. Replaceable in tests.
var Serve = func(socket, version string) error {
	return holder.NewServer(socket, version).Run()
}

// Run handles "--holder --holder-socket PATH". handled is false for any
// other invocation, in which case nothing ran.
func Run(args []string, version string, stderr io.Writer) (code int, handled bool) {
	if len(args) == 0 || args[0] != "--holder" {
		return 0, false
	}
	flags := flag.NewFlagSet("holder", flag.ContinueOnError)
	flags.SetOutput(stderr)
	socket := flags.String("holder-socket", "", "unix socket to serve on")
	if err := flags.Parse(args[1:]); err != nil {
		return 2, true
	}
	if *socket == "" {
		fmt.Fprintln(stderr, "hrdx-desktop: --holder requires --holder-socket")
		return 2, true
	}
	if err := Serve(*socket, version); err != nil {
		fmt.Fprintln(stderr, "hrdx-desktop holder:", err)
		return 1, true
	}
	return 0, true
}
