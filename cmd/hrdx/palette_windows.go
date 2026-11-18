//go:build windows

package main

import "github.com/patriceckhart/hrdx/internal/vt"

// queryHostPalette returns the default palette on Windows. Console input
// is read through a separate CONIN$ handle once Bubble Tea starts, and
// conhost does not answer color queries, so panes report the defaults.
func queryHostPalette() vt.Palette { return vt.DefaultPalette }
