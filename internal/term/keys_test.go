package term

import (
	"bytes"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestEncodeKeyEnterPreservesAlt(t *testing.T) {
	for _, tc := range []struct {
		name string
		key  tea.KeyMsg
		want []byte
	}{
		{name: "enter", key: tea.KeyMsg{Type: tea.KeyEnter}, want: []byte{'\r'}},
		{name: "alt+enter", key: tea.KeyMsg{Type: tea.KeyEnter, Alt: true}, want: []byte{'\x1b', '\r'}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := EncodeKey(tc.key, false); !bytes.Equal(got, tc.want) {
				t.Fatalf("EncodeKey(%s) = %q, want %q", tc.name, got, tc.want)
			}
		})
	}
}
