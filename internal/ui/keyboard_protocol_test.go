package ui

import (
	"bytes"
	"sync"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/patriceckhart/hrdx/internal/term"
)

// keyboardCaptureHost is the holder-side transport needed by NewHolderPane.
// It records only pane input; the other operations are irrelevant here.
// Pane input is written by a background goroutine, so callers flush the
// pane and read through bytes() to observe it deterministically.
type keyboardCaptureHost struct {
	mu    sync.Mutex
	input []byte
}

func (h *keyboardCaptureHost) Write(_ int64, data []byte) {
	h.mu.Lock()
	h.input = append(h.input, data...)
	h.mu.Unlock()
}

func (h *keyboardCaptureHost) bytes(target *term.Pane) []byte {
	target.FlushInput()
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]byte(nil), h.input...)
}

func (h *keyboardCaptureHost) reset(target *term.Pane) {
	target.FlushInput()
	h.mu.Lock()
	h.input = nil
	h.mu.Unlock()
}

func (*keyboardCaptureHost) Resize(int64, int, int)  {}
func (*keyboardCaptureHost) Kill(int64)              {}
func (*keyboardCaptureHost) Foreground(int64) string { return "" }

func TestAltEnterReachesFocusedPane(t *testing.T) {
	model := newTestModel("/tmp/api")
	target := model.currentPane()
	host := &keyboardCaptureHost{}
	target.term = term.NewHolderPane(host, 1, 80, 24)
	target.running = true

	model.updateKey(tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	if got, want := host.bytes(target.term), []byte{'\x1b', '\r'}; !bytes.Equal(got, want) {
		t.Fatalf("alt+enter pane input = %q, want %q", got, want)
	}
}

func TestEnhancedFunctionalKeysDriveLocalModes(t *testing.T) {
	model := newTestModel("/tmp/api")
	target := model.currentPane()
	host := &keyboardCaptureHost{}
	target.term = term.NewHolderPane(host, 1, 80, 24)
	target.running = true
	model.openKindPicker("tab", model.currentSpace(), "", rect{x: 1, y: 1})
	model.menuIndex = 1

	updated, _ := model.updateRaw([]byte("\x1b[1;129A"))
	model = updated.(Model)
	if model.menuIndex != 0 {
		t.Fatalf("menuIndex after enhanced Num Lock up = %d, want 0", model.menuIndex)
	}
	updated, _ = model.updateRaw([]byte("\x1b[1;1B"))
	model = updated.(Model)
	if model.menuIndex != 1 {
		t.Fatalf("menuIndex after enhanced down = %d, want 1", model.menuIndex)
	}
	model.navKeys = buildNavigationKeys(map[string]string{"navigate-up": "home"})
	updated, _ = model.updateRaw([]byte("\x1b[1;1H"))
	model = updated.(Model)
	if model.menuIndex != 0 {
		t.Fatalf("menuIndex after enhanced custom home = %d, want 0", model.menuIndex)
	}
	model.updateRaw([]byte("\x1b[999~"))
	if len(host.bytes(target.term)) != 0 {
		t.Fatalf("local mode leaked raw input to pane: %q", host.bytes(target.term))
	}
}

func TestEnhancedFunctionalKeysFollowChildProtocolInTerminalMode(t *testing.T) {
	model := newTestModel("/tmp/api")
	target := model.currentPane()
	host := &keyboardCaptureHost{}
	target.term = term.NewHolderPane(host, 1, 80, 24)
	target.running = true

	for _, input := range []string{
		"\x1b[1;1A",   // no lock keys
		"\x1b[1;65A",  // Caps Lock
		"\x1b[1;129A", // Num Lock
		"\x1b[1;193A", // Caps Lock and Num Lock
	} {
		host.reset(target.term)
		model.updateRaw([]byte(input))
		if want := []byte("\x1b[A"); !bytes.Equal(host.bytes(target.term), want) {
			t.Errorf("legacy terminal input for %q = %q, want %q", input, host.bytes(target.term), want)
		}
	}

	host.reset(target.term)
	target.term.Feed([]byte("\x1b[>1u"))
	input := []byte("\x1b[1;129A")
	model.updateRaw(input)
	if !bytes.Equal(host.bytes(target.term), input) {
		t.Fatalf("kitty terminal input = %q, want %q", host.bytes(target.term), input)
	}
}

func TestCSIUControlsReturnToLegacyEncodingAfterChildExitsAltScreen(t *testing.T) {
	model := newTestModel("/tmp/api")
	target := model.currentPane()
	target.kind = "shell"
	host := &keyboardCaptureHost{}
	target.term = term.NewHolderPane(host, 1, 80, 24)
	target.running = true

	// While the full-screen child is active, its kitty request and xterm
	// fallback make CSI-u the correct encoding to pass through.
	target.term.Feed([]byte("\x1b[?1049h\x1b[>1u\x1b[>4;2m"))
	childInput := []byte("\x1b[97;5u") // ctrl+a
	model.updateRaw(childInput)
	if !bytes.Equal(host.bytes(target.term), childInput) {
		t.Fatalf("input while child active = %q, want CSI-u %q", host.bytes(target.term), childInput)
	}

	// The child returns to the shell without a kitty pop or xterm fallback
	// reset. The alternate screen switch still restores the shell's keyboard
	// state, so common line-editing controls return to classic bytes.
	host.reset(target.term)
	target.term.Feed([]byte("\x1b[?1049l"))
	for _, input := range [][]byte{
		[]byte("\x1b[97;5u"),  // ctrl+a
		[]byte("\x1b[101;5u"), // ctrl+e
		[]byte("\x1b[108;5u"), // ctrl+l
	} {
		model.updateRaw(input)
	}
	if want := []byte{0x01, 0x05, 0x0c}; !bytes.Equal(host.bytes(target.term), want) {
		t.Fatalf("shell input after child exit = %q, want legacy controls %q", host.bytes(target.term), want)
	}
}
