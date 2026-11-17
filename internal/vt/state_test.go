package vt

import (
	"bytes"
	"testing"
)

func TestPrimaryDeviceAttributesResponse(t *testing.T) {
	var response bytes.Buffer
	terminal := New(WithSize(8, 2), WithWriter(&response))
	if _, err := terminal.Write([]byte("\x1b[c")); err != nil {
		t.Fatal(err)
	}
	if got, want := response.String(), "\x1b[?1;2c"; got != want {
		t.Fatalf("device attributes response = %q, want %q", got, want)
	}
}

// Issue #49: lazygit (tcell) sends CSI ? 4 m, an XTQMODKEYS query, at
// startup. Read as SGR 4 it underlined every cell, so blank areas showed
// as rows of lines and spaces as underscores. Prefixed sequences are
// distinct commands and must not run their unprefixed namesakes.
func TestPrivatePrefixedCSIIsNotItsNamesake(t *testing.T) {
	cases := []struct {
		name, seq string
	}{
		{"xterm modifyOtherKeys query", "\x1b[?4m"},
		{"xterm modifyOtherKeys set", "\x1b[>4;2m"},
		{"xterm modifyOtherKeys reset", "\x1b[>4;0m"},
		{"kitty keyboard push", "\x1b[>1u"},
		{"kitty keyboard pop", "\x1b[<u"},
		{"kitty keyboard query", "\x1b[?u"},
		{"kitty keyboard set", "\x1b[=1;1u"},
		{"XTSAVE", "\x1b[?1049s"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			terminal := New(WithSize(8, 4))
			// Bold and a cursor saved at the origin, then move to (2,1).
			setup := "\x1b[1m\x1b[s\x1b[2;3H"
			if _, err := terminal.Write([]byte(setup + tc.seq + "X")); err != nil {
				t.Fatal(err)
			}
			terminal.Lock()
			defer terminal.Unlock()
			cell := terminal.Cell(2, 1)
			if cell.Char != 'X' {
				t.Fatalf("X landed elsewhere (cursor moved by %q)", tc.seq)
			}
			if cell.Mode&attrUnderline != 0 {
				t.Fatalf("%q turned on underline", tc.seq)
			}
			if cell.Mode&attrBold == 0 {
				t.Fatalf("%q reset attributes", tc.seq)
			}
		})
	}
}

// The prefixed sequences hrdx implements keep working.
func TestPrivatePrefixedCSIStillHandled(t *testing.T) {
	var response bytes.Buffer
	terminal := New(WithSize(8, 4), WithWriter(&response))
	if _, err := terminal.Write([]byte("\x1b[?25l\x1b[3;2H\x1b[?6n")); err != nil {
		t.Fatal(err)
	}
	if terminal.CursorVisible() {
		t.Fatal("DECTCEM hide ignored")
	}
	if got := response.String(); got != "\x1b[3;2R" {
		t.Fatalf("cursor report = %q", got)
	}
	response.Reset()
	if _, err := terminal.Write([]byte("\x1b[>c")); err != nil {
		t.Fatal(err)
	}
	if response.Len() == 0 {
		t.Fatal("secondary device attributes got no reply")
	}
}

func TestSGRWithOmittedLeadingParameter(t *testing.T) {
	terminal := New(WithSize(8, 2))
	// fzf prefixes its SGR parameter list with an omitted parameter.
	if _, err := terminal.Write([]byte("\x1b[;1;38;5;110;48;5;236mX")); err != nil {
		t.Fatal(err)
	}
	terminal.Lock()
	defer terminal.Unlock()

	got := terminal.Cell(0, 0)
	if got.Char != 'X' || got.Mode&attrBold == 0 || got.FG != Color(110) || got.BG != Color(236) {
		t.Fatalf("styled cell = %+v, want bold fg=110 bg=236", got)
	}
}

func TestOmittedCSIParametersUseCommandDefaults(t *testing.T) {
	emulator := New(WithSize(8, 4))
	if _, err := emulator.Write([]byte("\x1b[;E")); err != nil {
		t.Fatal(err)
	}
	if got := emulator.Cursor().Y; got != 1 {
		t.Fatalf("CNL with omitted count moved to row %d, want 1", got)
	}

	if _, err := emulator.Write([]byte("\x1b[2;r")); err != nil {
		t.Fatal(err)
	}
	state := emulator.(*terminal).State
	if state.top != 1 || state.bottom != 3 {
		t.Fatalf("scroll region = %d..%d, want 1..3", state.top, state.bottom)
	}
}

func TestWriteBuffersUTF8RunesSplitAcrossCalls(t *testing.T) {
	input := []byte("└──┘")
	for split := 1; split < len(input); split++ {
		terminal := New(WithSize(8, 2))
		if n, err := terminal.Write(input[:split]); err != nil || n != split {
			t.Fatalf("split %d first write = (%d, %v)", split, n, err)
		}
		if n, err := terminal.Write(input[split:]); err != nil || n != len(input)-split {
			t.Fatalf("split %d second write = (%d, %v)", split, n, err)
		}
		terminal.Lock()
		for column, want := range []rune("└──┘") {
			if got := terminal.Cell(column, 0).Char; got != want {
				terminal.Unlock()
				t.Fatalf("split %d cell %d = %q, want %q", split, column, got, want)
			}
		}
		terminal.Unlock()
	}
}

func TestWideRunesOccupyTwoCells(t *testing.T) {
	terminal := New(WithSize(8, 2))
	if _, err := terminal.Write([]byte("a界b")); err != nil {
		t.Fatal(err)
	}
	terminal.Lock()
	defer terminal.Unlock()

	if cursor := terminal.Cursor(); cursor.X != 4 || cursor.Y != 0 {
		t.Fatalf("cursor = (%d,%d), want (4,0)", cursor.X, cursor.Y)
	}
	if got := terminal.Cell(1, 0); got.Char != '界' || got.Mode&attrWide == 0 {
		t.Fatalf("wide cell = %+v", got)
	}
	if got := terminal.Cell(2, 0); got.Mode&attrWideDummy == 0 {
		t.Fatalf("wide continuation = %+v", got)
	}
	if got := terminal.Cell(3, 0).Char; got != 'b' {
		t.Fatalf("cell after wide rune = %q, want b", got)
	}
}

func TestOverwritingWideContinuationClearsWholeGlyph(t *testing.T) {
	terminal := New(WithSize(8, 2))
	if _, err := terminal.Write([]byte("界\x1b[1;2Hx")); err != nil {
		t.Fatal(err)
	}
	terminal.Lock()
	defer terminal.Unlock()

	if got := terminal.Cell(0, 0); got.Char != ' ' || got.Mode&(attrWide|attrWideDummy) != 0 {
		t.Fatalf("wide lead after continuation overwrite = %+v", got)
	}
	if got := terminal.Cell(1, 0); got.Char != 'x' || got.Mode&(attrWide|attrWideDummy) != 0 {
		t.Fatalf("replacement cell = %+v", got)
	}
}

func TestEraseIntersectingWideRuneClearsWholeGlyph(t *testing.T) {
	terminal := New(WithSize(8, 2))
	if _, err := terminal.Write([]byte("界x\x1b[1;2H\x1b[K")); err != nil {
		t.Fatal(err)
	}
	terminal.Lock()
	defer terminal.Unlock()

	if got := terminal.Cell(0, 0); got.Char != ' ' || got.Mode&(attrWide|attrWideDummy) != 0 {
		t.Fatalf("wide lead after erase = %+v", got)
	}
}

func TestCombiningMarkDoesNotAdvanceCursor(t *testing.T) {
	terminal := New(WithSize(8, 2))
	if _, err := terminal.Write([]byte("e\u0301x")); err != nil {
		t.Fatal(err)
	}
	terminal.Lock()
	defer terminal.Unlock()
	if cursor := terminal.Cursor(); cursor.X != 2 {
		t.Fatalf("cursor X = %d, want 2", cursor.X)
	}
	if got := terminal.Cell(1, 0).Char; got != 'x' {
		t.Fatalf("cell after combining mark = %q, want x", got)
	}
}
