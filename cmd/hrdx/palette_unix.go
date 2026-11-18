//go:build !windows

package main

import (
	"os"
	"time"

	"github.com/charmbracelet/x/term"
	"golang.org/x/sys/unix"

	"github.com/patriceckhart/hrdx/internal/vt"
)

// queryHostPalette asks the terminal hrdx runs in for its colors, so panes
// can answer color queries the way the host would. It runs before Bubble
// Tea starts, while hrdx still owns stdin, and returns the defaults when
// stdin or stdout is not a terminal or the terminal does not answer.
func queryHostPalette() vt.Palette {
	in, out := os.Stdin, os.Stdout
	if !term.IsTerminal(in.Fd()) || !term.IsTerminal(out.Fd()) {
		return vt.DefaultPalette
	}
	saved, err := term.MakeRaw(in.Fd())
	if err != nil {
		return vt.DefaultPalette
	}
	defer term.Restore(in.Fd(), saved) //nolint:errcheck

	if _, err := out.WriteString(paletteQuery()); err != nil {
		return vt.DefaultPalette
	}
	deadline := time.Now().Add(paletteTimeout)
	data := readPaletteReplies(pollReader{fd: int(in.Fd()), deadline: deadline}, deadline)
	palette, _ := parsePaletteReplies(data, vt.DefaultPalette)
	return palette
}

// pollReader reads only when input is ready before the deadline, so a
// terminal that never answers cannot block startup.
type pollReader struct {
	fd       int
	deadline time.Time
}

func (r pollReader) Read(buf []byte) (int, error) {
	remaining := time.Until(r.deadline)
	if remaining <= 0 {
		return 0, os.ErrDeadlineExceeded
	}
	fds := []unix.PollFd{{Fd: int32(r.fd), Events: unix.POLLIN}}
	n, err := unix.Poll(fds, int(remaining/time.Millisecond)+1)
	if err != nil {
		if err == unix.EINTR {
			return 0, nil
		}
		return 0, err
	}
	if n == 0 {
		return 0, os.ErrDeadlineExceeded
	}
	return unix.Read(r.fd, buf)
}
