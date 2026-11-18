package main

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/patriceckhart/hrdx/internal/vt"
)

func TestPaletteQueryAsksColorsThenDA1(t *testing.T) {
	query := paletteQuery()
	if !strings.HasPrefix(query, "\x1b]10;?\x1b\\\x1b]11;?\x1b\\\x1b]4;0;?\x1b\\") {
		t.Fatalf("query starts %q", query[:30])
	}
	if !strings.Contains(query, "\x1b]4;15;?\x1b\\") || strings.Contains(query, "\x1b]4;16;?") {
		t.Fatal("query must ask exactly the 16 ANSI colors")
	}
	if !strings.HasSuffix(query, "\x1b[c") {
		t.Fatal("DA1 must come last so its reply ends the wait")
	}
}

// Replies as Ghostty and kitty send them: 4 hex digits, ST or BEL ends.
func TestParsePaletteReplies(t *testing.T) {
	data := []byte("\x1b]10;rgb:dcdc/d7d7/baba\x1b\\" +
		"\x1b]11;rgb:1f/1f/28\x07" +
		"\x1b]4;1;rgb:ffff/0000/0000\x1b\\" +
		"\x1b]4;15;rgb:f/f/f\x1b\\" +
		"\x1b]4;99;rgb:0/0/0\x1b\\" + // out of range: ignored
		"\x1b]4;2;bogus\x1b\\" + // malformed: ignored
		"\x1b[?62;22c")
	got, found := parsePaletteReplies(data, vt.DefaultPalette)
	if !found {
		t.Fatal("replies not recognized")
	}
	if got.FG != 0xdcd7ba || got.BG != 0x1f1f28 || got.ANSI[1] != 0xff0000 || got.ANSI[15] != 0xffffff {
		t.Fatalf("palette = fg %06x bg %06x red %06x white %06x", got.FG, got.BG, got.ANSI[1], got.ANSI[15])
	}
	if got.ANSI[2] != vt.DefaultPalette.ANSI[2] {
		t.Fatal("a malformed reply must keep the default")
	}
}

func TestParsePaletteRepliesWithoutColors(t *testing.T) {
	got, found := parsePaletteReplies([]byte("\x1b[?1;2c"), vt.DefaultPalette)
	if found || got != vt.DefaultPalette {
		t.Fatal("a terminal that only answers DA1 must keep the defaults")
	}
}

func TestParseXColor(t *testing.T) {
	cases := map[string]vt.RGB{
		"rgb:ffff/8080/0000": 0xff8000,
		"rgb:ff/80/00":       0xff8000,
		"rgb:f/8/0":          0xff8800,
		"RGB:FFF/000/000":    0xff0000,
	}
	for value, want := range cases {
		if got, ok := parseXColor(value); !ok || got != want {
			t.Errorf("%q = %06x %v, want %06x", value, got, ok, want)
		}
	}
	for _, bad := range []string{"#ff0000", "rgb:ff/00", "rgb:fffff/0/0", "rgb:zz/00/00", ""} {
		if _, ok := parseXColor(bad); ok {
			t.Errorf("%q should be rejected", bad)
		}
	}
}

// chunkReader returns one chunk per Read, like a terminal answering in
// pieces.
type chunkReader struct{ chunks [][]byte }

func (r *chunkReader) Read(buf []byte) (int, error) {
	if len(r.chunks) == 0 {
		return 0, io.EOF
	}
	n := copy(buf, r.chunks[0])
	r.chunks = r.chunks[1:]
	return n, nil
}

func TestReadPaletteRepliesStopsAtDA1(t *testing.T) {
	reader := &chunkReader{chunks: [][]byte{
		[]byte("\x1b]10;rgb:ffff/ffff/ffff\x1b\\\x1b[?6"),
		[]byte("2;22c"),
		[]byte("typed-later"),
	}}
	got := readPaletteReplies(reader, time.Now().Add(time.Second))
	if !bytes.HasSuffix(got, []byte("\x1b[?62;22c")) || bytes.Contains(got, []byte("typed-later")) {
		t.Fatalf("read %q", got)
	}
}

func TestHasDA1Reply(t *testing.T) {
	if hasDA1Reply([]byte("\x1b[?62;22")) {
		t.Fatal("incomplete DA1 reply")
	}
	if hasDA1Reply([]byte("\x1b[?2004h")) {
		t.Fatal("a mode sequence is not a DA1 reply")
	}
	if !hasDA1Reply([]byte("noise\x1b[?1;2c")) {
		t.Fatal("DA1 reply missed")
	}
}
