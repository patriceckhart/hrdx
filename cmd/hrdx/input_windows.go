//go:build windows

package main

import (
	"os"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/term"
)

// platformInputOptions makes Bubble Tea read Windows console input as a VT
// byte stream instead of as console input records.
//
// With the default stdin handle, Bubble Tea v1 decodes ReadConsoleInput
// records. That path has no notion of bracketed paste: a pasted block
// arrives as individual key events and every line break as Enter, so a
// multi-line paste into an agent pane submitted one prompt per line.
// WithInputTTY opens CONIN$ as a separate handle, which routes input
// through the ANSI reader with ENABLE_VIRTUAL_TERMINAL_INPUT set. The
// terminal then delivers ESC[200~ … ESC[201~ and Bubble Tea emits one
// paste message, exactly as on Unix. Mouse and focus reports arrive as
// the VT sequences hrdx already requests.
func platformInputOptions() []tea.ProgramOption {
	var repair utf8Repair
	return []tea.ProgramOption{tea.WithInputTTY(), tea.WithFilter(func(_ tea.Model, msg tea.Msg) tea.Msg {
		return repair.filter(msg)
	})}
}

// utf8Repair undoes Bubble Tea's per-byte decoding of VT input on Windows.
//
// The byte-stream reader wraps input in go-localereader, which maps every
// byte >= 0x80 to the rune with the same value. With
// ENABLE_VIRTUAL_TERMINAL_INPUT the console delivers UTF-8, so "ö" (C3 B6)
// arrived as "Ã¶". Every rune on this path is therefore one input byte;
// reassembling the bytes and decoding them as UTF-8 restores the text. An
// incomplete trailing sequence is held until the next key message.
// filter runs on Bubble Tea's event loop only, so no locking is needed.
type utf8Repair struct {
	pending []byte
}

func (r *utf8Repair) filter(msg tea.Msg) tea.Msg {
	key, ok := msg.(tea.KeyMsg)
	if !ok || key.Type != tea.KeyRunes {
		r.pending = nil
		return msg
	}
	raw := append([]byte(nil), r.pending...)
	r.pending = nil
	for _, c := range key.Runes {
		if c > 0xFF {
			// Not per-byte decoded input; leave it untouched.
			return msg
		}
		raw = append(raw, byte(c))
	}
	out := make([]rune, 0, len(raw))
	for len(raw) > 0 {
		if !utf8.FullRune(raw) && !key.Paste {
			r.pending = raw
			break
		}
		c, n := utf8.DecodeRune(raw)
		if c == utf8.RuneError && n <= 1 {
			c = rune(raw[0]) // invalid UTF-8: keep the byte as before
		}
		out = append(out, c)
		raw = raw[n:]
	}
	if len(out) == 0 {
		return nil // wait for the rest of the character
	}
	key.Runes = out
	return key
}

// resizePollInterval bounds how long a window size change goes unnoticed.
const resizePollInterval = 100 * time.Millisecond

// watchPlatformResize replaces the console buffer-size records that the
// byte-stream input path no longer receives. Windows has no SIGWINCH, so
// the size is polled and changes are sent as WindowSizeMsg.
func watchPlatformResize(program *tea.Program) {
	fd := os.Stdout.Fd()
	lastW, lastH, _ := term.GetSize(fd)
	go func() {
		ticker := time.NewTicker(resizePollInterval)
		defer ticker.Stop()
		for range ticker.C {
			w, h, err := term.GetSize(fd)
			if err != nil || w <= 0 || h <= 0 || (w == lastW && h == lastH) {
				continue
			}
			lastW, lastH = w, h
			program.Send(tea.WindowSizeMsg{Width: w, Height: h})
		}
	}()
}
