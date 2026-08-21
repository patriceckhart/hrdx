package ui

import (
	"bytes"
	"testing"

	"github.com/patriceckhart/hrdx/internal/term"
)

// keyboardCaptureHost is the holder-side transport needed by NewHolderPane.
// It records only pane input; the other operations are irrelevant here.
type keyboardCaptureHost struct {
	input []byte
}

func (h *keyboardCaptureHost) Write(_ int64, data []byte) {
	h.input = append(h.input, data...)
}

func (*keyboardCaptureHost) Resize(int64, int, int)  {}
func (*keyboardCaptureHost) Kill(int64)              {}
func (*keyboardCaptureHost) Foreground(int64) string { return "" }

func TestCSIUControlsReturnToLegacyEncodingAfterChildExitsAltScreen(t *testing.T) {
	model := newTestModel("/tmp/api")
	target := model.currentPane()
	target.kind = "shell"
	host := &keyboardCaptureHost{}
	target.term = term.NewHolderPane(host, 1, 80, 24)
	target.running = true

	// While the full-screen child is active, its kitty request makes CSI-u the
	// correct encoding to pass through.
	target.term.Feed([]byte("\x1b[?1049h\x1b[>1u"))
	childInput := []byte("\x1b[97;5u") // ctrl+a
	model.updateRaw(childInput)
	if !bytes.Equal(host.input, childInput) {
		t.Fatalf("input while child active = %q, want CSI-u %q", host.input, childInput)
	}

	// The child returns to the shell without a kitty pop. The alternate screen
	// switch still restores the shell's independent keyboard-protocol state, so
	// common line-editing controls must be translated back to classic bytes.
	host.input = nil
	target.term.Feed([]byte("\x1b[?1049l"))
	for _, input := range [][]byte{
		[]byte("\x1b[97;5u"),  // ctrl+a
		[]byte("\x1b[101;5u"), // ctrl+e
		[]byte("\x1b[108;5u"), // ctrl+l
	} {
		model.updateRaw(input)
	}
	if want := []byte{0x01, 0x05, 0x0c}; !bytes.Equal(host.input, want) {
		t.Fatalf("shell input after child exit = %q, want legacy controls %q", host.input, want)
	}
}
