//go:build windows

package main

import (
	"os"
	"syscall"
	"time"
	"unicode/utf8"
	"unsafe"

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
// Bubble Tea's go-localereader maps most bytes >= 0x80 to individual runes,
// but on DBCS Windows locales it can combine a pair of bytes into one rune.
// Recover those pairs before decoding the VT input as UTF-8. An incomplete
// trailing sequence is held until the next key message. filter runs on the
// Bubble Tea event loop only, so no locking is needed.
type utf8Repair struct {
	pending []byte
	// bytesForRune reverses a DBCS pair produced by go-localereader.
	bytesForRune func(rune) []byte
}

var wideCharToMultiByte = syscall.NewLazyDLL("kernel32.dll").NewProc("WideCharToMultiByte")

// go-localereader uses CP_ACP to decode a two-byte DBCS sequence into a
// single UTF-16 code unit. Reverse that conversion for runes above 0xff.
// A one-byte result is not a DBCS pair, and must remain a decoded rune.
func localePairBytes(c rune) []byte { return codepagePairBytes(c, 0) }

func codepagePairBytes(c rune, codePage uintptr) []byte {
	if c > 0xffff {
		return nil
	}
	unit := uint16(c)
	var pair [2]byte
	var usedDefault uint32
	n, _, _ := wideCharToMultiByte.Call(codePage, 0, uintptr(unsafe.Pointer(&unit)), 1,
		uintptr(unsafe.Pointer(&pair[0])), uintptr(len(pair)), 0, uintptr(unsafe.Pointer(&usedDefault)))
	if n != 2 || usedDefault != 0 {
		return nil
	}
	return pair[:]
}

func (r *utf8Repair) filter(msg tea.Msg) tea.Msg {
	key, ok := msg.(tea.KeyMsg)
	if !ok || key.Type != tea.KeyRunes {
		return msg
	}
	raw := append([]byte(nil), r.pending...)
	r.pending = nil
	for _, c := range key.Runes {
		if c > 0xFF {
			pair := localePairBytes
			if r.bytesForRune != nil {
				pair = r.bytesForRune
			}
			if bytes := pair(c); len(bytes) == 2 {
				raw = append(raw, bytes...)
				continue
			}
			// Already decoded, leave this key alone.
			r.pending = nil
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
func watchPlatformResize(program *tea.Program, stop <-chan struct{}) <-chan struct{} {
	finished := make(chan struct{})
	fd := os.Stdout.Fd()
	lastW, lastH, _ := term.GetSize(fd)
	go func() {
		defer close(finished)
		ticker := time.NewTicker(resizePollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				w, h, err := term.GetSize(fd)
				if err != nil || w <= 0 || h <= 0 || (w == lastW && h == lastH) {
					continue
				}
				lastW, lastH = w, h
				program.Send(tea.WindowSizeMsg{Width: w, Height: h})
			}
		}
	}()
	return finished
}
