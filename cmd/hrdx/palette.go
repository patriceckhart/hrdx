package main

import (
	"bytes"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/patriceckhart/hrdx/internal/vt"
)

// paletteTimeout bounds the startup color query. Terminals that answer do
// so within milliseconds. The trailing device-attributes query ends the
// wait early on terminals that ignore color queries.
const paletteTimeout = 300 * time.Millisecond

// paletteQuery asks the host terminal for its foreground, background, and
// 16 ANSI colors, then for primary device attributes. Every terminal
// answers DA1, so its reply marks the end of the color replies.
func paletteQuery() string {
	var out strings.Builder
	out.WriteString("\x1b]10;?\x1b\\\x1b]11;?\x1b\\")
	for index := range 16 {
		fmt.Fprintf(&out, "\x1b]4;%d;?\x1b\\", index)
	}
	out.WriteString("\x1b[c")
	return out.String()
}

// readPaletteReplies reads from r until the DA1 reply arrives, the deadline
// passes, or the reader fails, and returns the bytes read. It never returns
// more than one DA1 reply worth of extra input.
func readPaletteReplies(r io.Reader, deadline time.Time) []byte {
	var data []byte
	buf := make([]byte, 512)
	for time.Now().Before(deadline) {
		n, err := r.Read(buf)
		data = append(data, buf[:n]...)
		if hasDA1Reply(data) || err != nil || len(data) > 64<<10 {
			break
		}
	}
	return data
}

// hasDA1Reply reports whether data contains a primary device attributes
// reply, ESC [ ? ... c.
func hasDA1Reply(data []byte) bool {
	start := bytes.Index(data, []byte("\x1b[?"))
	for start >= 0 {
		for i := start + 3; i < len(data); i++ {
			b := data[i]
			if b == 'c' {
				return true
			}
			if (b < '0' || b > '9') && b != ';' {
				break
			}
		}
		next := bytes.Index(data[start+1:], []byte("\x1b[?"))
		if next < 0 {
			return false
		}
		start += 1 + next
	}
	return false
}

// parsePaletteReplies applies every OSC 10, 11, and 4 color reply in data
// to base and returns the result. Missing or malformed replies keep the
// base color, so a partial answer still helps.
func parsePaletteReplies(data []byte, base vt.Palette) (vt.Palette, bool) {
	out := base
	found := false
	for _, reply := range oscReplies(data) {
		fields := strings.Split(reply, ";")
		switch {
		case len(fields) == 2 && (fields[0] == "10" || fields[0] == "11"):
			color, ok := parseXColor(fields[1])
			if !ok {
				continue
			}
			if fields[0] == "10" {
				out.FG = color
			} else {
				out.BG = color
			}
			found = true
		case len(fields) == 3 && fields[0] == "4":
			index, err := strconv.Atoi(fields[1])
			color, ok := parseXColor(fields[2])
			if err != nil || !ok || index < 0 || index >= len(out.ANSI) {
				continue
			}
			out.ANSI[index] = color
			found = true
		}
	}
	return out, found
}

// oscReplies extracts the bodies of OSC sequences ended by BEL or ST.
func oscReplies(data []byte) []string {
	var out []string
	for {
		start := bytes.Index(data, []byte("\x1b]"))
		if start < 0 {
			return out
		}
		data = data[start+2:]
		end, size := -1, 0
		if bel := bytes.IndexByte(data, 0x07); bel >= 0 {
			end, size = bel, 1
		}
		if st := bytes.Index(data, []byte("\x1b\\")); st >= 0 && (end < 0 || st < end) {
			end, size = st, 2
		}
		if end < 0 {
			return out
		}
		out = append(out, string(data[:end]))
		data = data[end+size:]
	}
}

// parseXColor reads an XParseColor "rgb:R/G/B" value with 1 to 4 hex digits
// per channel, as terminals answer color queries.
func parseXColor(value string) (vt.RGB, bool) {
	spec, ok := strings.CutPrefix(strings.ToLower(value), "rgb:")
	if !ok {
		return 0, false
	}
	parts := strings.Split(spec, "/")
	if len(parts) != 3 {
		return 0, false
	}
	var out vt.RGB
	for _, part := range parts {
		if len(part) < 1 || len(part) > 4 {
			return 0, false
		}
		channel, err := strconv.ParseUint(part, 16, 16)
		if err != nil {
			return 0, false
		}
		max := uint64(1)<<(4*len(part)) - 1
		out = out<<8 | vt.RGB((channel*255+max/2)/max)
	}
	return out, true
}
