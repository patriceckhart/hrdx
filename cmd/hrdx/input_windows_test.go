//go:build windows

package main

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
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
