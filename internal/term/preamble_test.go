package term

import (
	"testing"

	"github.com/patriceckhart/hrdx/internal/vt"
)

func TestModePreambleCombinations(t *testing.T) {
	cases := []struct {
		mode    vt.ModeFlag
		visible bool
		want    string
	}{
		{0, true, ""},
		{0, false, "\x1b[?25l"},
		{vt.ModeAltScreen | vt.ModeAppCursor, true, "\x1b[?1049h\x1b[?1h"},
		{vt.ModeBracketedPaste | vt.ModeMouseMotion | vt.ModeMouseSgr, true, "\x1b[?2004h\x1b[?1002h\x1b[?1006h"},
		{vt.ModeMouseMany | vt.ModeMouseButton, true, "\x1b[?1003h"},
		{vt.ModeFocus | vt.ModeMouseX10, true, "\x1b[?1004h\x1b[?9h"},
	}
	for _, c := range cases {
		if got := modePreamble(c.mode, c.visible); got != c.want {
			t.Errorf("mode %b visible %v = %q, want %q", c.mode, c.visible, got, c.want)
		}
	}
}

func TestModePreambleFollowsFeed(t *testing.T) {
	pane := NewHolderPane(nopHost{}, 1, 20, 5)
	pane.Feed([]byte("\x1b[?1049h\x1b[?2004h\x1b[?25l"))
	if got := pane.ModePreamble(); got != "\x1b[?1049h\x1b[?2004h\x1b[?25l" {
		t.Fatalf("preamble = %q", got)
	}
	pane.Feed([]byte("\x1b[?1049l\x1b[?2004l\x1b[?25h"))
	if got := pane.ModePreamble(); got != "" {
		t.Fatalf("preamble after reset = %q", got)
	}
}

// Feed and MarkExited run on different goroutines when a holder session is
// detached while output arrives. A notify after close must not panic.
func TestFeedRacesMarkExited(t *testing.T) {
	for range 200 {
		pane := NewHolderPane(nopHost{}, 1, 20, 5)
		done := make(chan struct{})
		go func() {
			defer close(done)
			for range 20 {
				pane.Feed([]byte("x"))
			}
		}()
		pane.MarkExited()
		<-done
	}
}

type nopHost struct{}

func (nopHost) Write(int64, []byte)     {}
func (nopHost) Resize(int64, int, int)  {}
func (nopHost) Kill(int64)              {}
func (nopHost) Foreground(int64) string { return "" }
