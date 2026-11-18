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

// GitHub Copilot CLI derives its colors from OSC 10, 11, and 4 answers.
// They used to echo hrdx's internal color codes (black foreground, palette
// entries as tiny blue values), so dimmed text vanished on a dark screen.
func TestColorQueriesReportDisplayedColors(t *testing.T) {
	SetReportPalette(DefaultPalette)
	t.Cleanup(func() { SetReportPalette(DefaultPalette) })

	cases := []struct{ query, want string }{
		{"\x1b]10;?\x07", "\x1b]10;rgb:e4e4/e4e4/e7e7\a"},
		{"\x1b]11;?\x1b\\", "\x1b]11;rgb:1818/1818/1b1b\a"},
		{"\x1b]4;1;?\x07", "\x1b]4;1;rgb:cccc/0000/0000\a"},
		{"\x1b]4;15;?\x07", "\x1b]4;15;rgb:eeee/eeee/ecec\a"},
		{"\x1b]4;16;?\x07", "\x1b]4;16;rgb:0000/0000/0000\a"},
		{"\x1b]4;200;?\x07", "\x1b]4;200;rgb:ffff/0000/d7d7\a"}, // cube 5,0,4
		{"\x1b]4;244;?\x07", "\x1b]4;244;rgb:8080/8080/8080\a"}, // gray ramp
		{"\x1b]4;300;?\x07", ""},                                // out of range
	}
	for _, tc := range cases {
		var response bytes.Buffer
		terminal := New(WithWriter(&response))
		if _, err := terminal.Write([]byte(tc.query)); err != nil {
			t.Fatal(err)
		}
		if got := response.String(); got != tc.want {
			t.Errorf("%q answered %q, want %q", tc.query, got, tc.want)
		}
	}
}

func TestColorQueriesUseFrontendPalette(t *testing.T) {
	custom := DefaultPalette
	custom.FG, custom.BG, custom.ANSI[2] = 0x112233, 0xfafafa, 0x00ff00
	SetReportPalette(custom)
	t.Cleanup(func() { SetReportPalette(DefaultPalette) })

	var response bytes.Buffer
	terminal := New(WithWriter(&response))
	if _, err := terminal.Write([]byte("\x1b]10;?\x07\x1b]11;?\x07\x1b]4;2;?\x07")); err != nil {
		t.Fatal(err)
	}
	want := "\x1b]10;rgb:1111/2222/3333\a\x1b]11;rgb:fafa/fafa/fafa\a\x1b]4;2;rgb:0000/ffff/0000\a"
	if got := response.String(); got != want {
		t.Fatalf("answers = %q, want %q", got, want)
	}
}

// A color the program set itself is what it reads back.
func TestColorQueriesReportProgramOverrides(t *testing.T) {
	var response bytes.Buffer
	terminal := New(WithWriter(&response))
	if _, err := terminal.Write([]byte("\x1b]11;rgb:01/02/03\x07\x1b]4;3;#a0b0c0\x07\x1b]11;?\x07\x1b]4;3;?\x07")); err != nil {
		t.Fatal(err)
	}
	want := "\x1b]11;rgb:0101/0202/0303\a\x1b]4;3;rgb:a0a0/b0b0/c0c0\a"
	if got := response.String(); got != want {
		t.Fatalf("answers = %q, want %q", got, want)
	}
}

// Copilot sets the background with OSC 11 while it runs and resets it with
// OSC 111 on exit. The override used to outlive it, so the shell after
// Copilot drew on Copilot's black instead of the terminal background.
func TestColorResetsDropProgramOverrides(t *testing.T) {
	cases := []struct {
		name, set, reset string
		color            Color
	}{
		{"OSC 110 foreground", "\x1b]10;#010203\x07", "\x1b]110\x07", DefaultFG},
		{"OSC 111 background", "\x1b]11;#0d1117\x07", "\x1b]111\x1b\\", DefaultBG},
		{"OSC 104 one entry", "\x1b]4;1;#010203\x07", "\x1b]104;1\x07", Color(1)},
		{"OSC 104 whole palette", "\x1b]4;200;#010203\x07", "\x1b]104\x07", Color(200)},
		{"RIS", "\x1b]11;#0d1117\x07", "\x1bc", DefaultBG},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			emulator := New(WithSize(4, 2))
			if _, err := emulator.Write([]byte(tc.set)); err != nil {
				t.Fatal(err)
			}
			state := emulator.(*terminal).State
			if _, ok := state.colorOverride[tc.color]; !ok {
				t.Fatal("override was not set")
			}
			if _, err := emulator.Write([]byte(tc.reset)); err != nil {
				t.Fatal(err)
			}
			if _, ok := state.colorOverride[tc.color]; ok {
				t.Fatalf("%q kept the override", tc.reset)
			}
		})
	}
}

// After a background reset, cells render with the default background again,
// which the host draws in its own color.
func TestBackgroundResetRendersDefault(t *testing.T) {
	terminal := New(WithSize(4, 2))
	if _, err := terminal.Write([]byte("\x1b]11;#0d1117\x07\x1b]111\x07X")); err != nil {
		t.Fatal(err)
	}
	terminal.Lock()
	defer terminal.Unlock()
	if got := terminal.Cell(0, 0).BG; got != DefaultBG {
		t.Fatalf("background = %x, want the default", got)
	}
}
