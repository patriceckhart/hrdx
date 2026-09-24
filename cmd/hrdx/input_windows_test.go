//go:build windows

package main

import (
	"bytes"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/sys/windows"
)

// bytesAsRunes mimics go-localereader: one rune per input byte.
func bytesAsRunes(s string) []rune {
	out := make([]rune, 0, len(s))
	for i := 0; i < len(s); i++ {
		out = append(out, rune(s[i]))
	}
	return out
}

func TestUTF8RepairRestoresUmlauts(t *testing.T) {
	var r utf8Repair
	for _, tc := range []struct {
		in    string
		paste bool
	}{
		{"ö", false},
		{"äöüÄÖÜß", false},
		{"Grüße aus Zürich\r→ zweite Zeile 🙂", true},
		{"plain ascii", false},
	} {
		got := r.filter(tea.KeyMsg{Type: tea.KeyRunes, Runes: bytesAsRunes(tc.in), Paste: tc.paste})
		key, ok := got.(tea.KeyMsg)
		if !ok || string(key.Runes) != tc.in || key.Paste != tc.paste {
			t.Errorf("%q: got %#v", tc.in, got)
		}
	}
}

func TestUTF8RepairJoinsSplitCharacter(t *testing.T) {
	var r utf8Repair
	b := "ö"
	if got := r.filter(tea.KeyMsg{Type: tea.KeyRunes, Runes: bytesAsRunes(b[:1])}); got != nil {
		t.Fatalf("first half: got %#v, want nil", got)
	}
	got := r.filter(tea.KeyMsg{Type: tea.KeyRunes, Runes: bytesAsRunes(b[1:] + "x")})
	if key, ok := got.(tea.KeyMsg); !ok || string(key.Runes) != "öx" {
		t.Fatalf("second half: got %#v", got)
	}
}

func TestUTF8RepairKeepsSplitCharacterAcrossOtherMessages(t *testing.T) {
	var r utf8Repair
	b := "ö"
	if got := r.filter(tea.KeyMsg{Type: tea.KeyRunes, Runes: bytesAsRunes(b[:1])}); got != nil {
		t.Fatalf("first half: got %#v, want nil", got)
	}
	if got := r.filter(tea.WindowSizeMsg{Width: 80, Height: 24}); got != (tea.WindowSizeMsg{Width: 80, Height: 24}) {
		t.Fatalf("resize: got %#v", got)
	}
	if got := r.filter(tea.KeyMsg{Type: tea.KeyEnter}); got.(tea.KeyMsg).Type != tea.KeyEnter {
		t.Fatalf("enter: got %#v", got)
	}
	got := r.filter(tea.KeyMsg{Type: tea.KeyRunes, Runes: bytesAsRunes(b[1:])})
	if key, ok := got.(tea.KeyMsg); !ok || string(key.Runes) != b {
		t.Fatalf("second half: got %#v", got)
	}
}

func TestUTF8RepairReversesLocalePair(t *testing.T) {
	// A DBCS locale can turn two UTF-8 bytes into one code-page rune.
	// The input filter must put the original bytes back before UTF-8 decoding.
	r := utf8Repair{bytesForRune: func(c rune) []byte {
		if c == '界' {
			return []byte{0xe7, 0x95}
		}
		return nil
	}}
	got := r.filter(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'界', 0x8c}, Paste: true})
	if key, ok := got.(tea.KeyMsg); !ok || string(key.Runes) != "界" || !key.Paste {
		t.Fatalf("DBCS paste: got %#v", got)
	}
}

func TestLocalePairBytesCP932(t *testing.T) {
	// An emoji begins with F0 9F, a DBCS pair in the Japanese ANSI code page.
	input := []byte("🙂")
	var decoded [2]uint16
	for i := range decoded {
		n, err := windows.MultiByteToWideChar(932, 0, &input[i*2], 2, &decoded[i], 1)
		if err != nil || n != 1 {
			t.Fatalf("CP932 decode pair %d: n=%d, err=%v", i, n, err)
		}
		if got := codepagePairBytes(rune(decoded[i]), 932); !bytes.Equal(got, input[i*2:i*2+2]) {
			t.Fatalf("CP932 pair %d: got %x, want %x", i, got, input[i*2:i*2+2])
		}
	}
	r := utf8Repair{bytesForRune: func(c rune) []byte { return codepagePairBytes(c, 932) }}
	got := r.filter(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{rune(decoded[0]), rune(decoded[1])}, Paste: true})
	if key, ok := got.(tea.KeyMsg); !ok || string(key.Runes) != "🙂" {
		t.Fatalf("CP932 emoji: got %#v", got)
	}
}

func TestResizeWatcherStops(t *testing.T) {
	stop := make(chan struct{})
	finished := watchPlatformResize(nil, stop)
	close(stop)
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("resize watcher did not stop")
	}
}

func TestUTF8RepairLeavesOtherInputAlone(t *testing.T) {
	var r utf8Repair
	enter := tea.KeyMsg{Type: tea.KeyEnter}
	if got, ok := r.filter(enter).(tea.KeyMsg); !ok || got.Type != tea.KeyEnter {
		t.Fatalf("enter: got %#v", got)
	}
	wide := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("€")}
	if got := r.filter(wide); string(got.(tea.KeyMsg).Runes) != "€" {
		t.Fatalf("decoded rune: got %#v", got)
	}
	invalid := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{0xFF, 'a'}, Paste: true}
	if got := r.filter(invalid); string(got.(tea.KeyMsg).Runes) != "\u00ffa" {
		t.Fatalf("invalid byte: got %#v", got)
	}
	size := tea.WindowSizeMsg{Width: 1, Height: 1}
	if got := r.filter(size); got != size {
		t.Fatalf("size: got %#v", got)
	}
}
