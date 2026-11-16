package term

import (
	"strings"

	"github.com/patriceckhart/hrdx/internal/vt"
)

// ModePreamble synthesizes the escape sequences that put a fresh terminal
// emulator into this pane's current input and screen modes: alternate
// screen, application cursor keys, bracketed paste, mouse reporting, focus
// reporting, and cursor visibility. A frontend writes it before replaying
// raw output so a remounted view behaves like the one it replaces even when
// the bytes that set a mode fell out of the replay buffer.
func (p *Pane) ModePreamble() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.vt.Lock()
	mode := p.vt.Mode()
	visible := p.vt.CursorVisible()
	p.vt.Unlock()
	return modePreamble(mode, visible)
}

func modePreamble(mode vt.ModeFlag, cursorVisible bool) string {
	var out strings.Builder
	set := func(on bool, code string) {
		if on {
			out.WriteString("\x1b[?" + code + "h")
		}
	}
	set(mode&vt.ModeAltScreen != 0, "1049")
	set(mode&vt.ModeAppCursor != 0, "1")
	set(mode&vt.ModeBracketedPaste != 0, "2004")
	set(mode&vt.ModeFocus != 0, "1004")
	switch {
	case mode&vt.ModeMouseMany != 0:
		out.WriteString("\x1b[?1003h")
	case mode&vt.ModeMouseMotion != 0:
		out.WriteString("\x1b[?1002h")
	case mode&vt.ModeMouseButton != 0:
		out.WriteString("\x1b[?1000h")
	case mode&vt.ModeMouseX10 != 0:
		out.WriteString("\x1b[?9h")
	}
	set(mode&vt.ModeMouseSgr != 0, "1006")
	if !cursorVisible {
		out.WriteString("\x1b[?25l")
	}
	return out.String()
}
