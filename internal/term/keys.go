package term

import tea "github.com/charmbracelet/bubbletea"

// EncodePaste translates pasted text into the bytes a real terminal would
// send to the child. When bracketed paste is enabled, the text is wrapped in
// ESC[200~ / ESC[201~ so embedded newlines are not mistaken for Enter.
func EncodePaste(text string, bracketed bool) []byte {
	if bracketed {
		return []byte("\x1b[200~" + text + "\x1b[201~")
	}
	return []byte(text)
}

// EncodeKey translates a Bubble Tea key press into the byte sequence a real
// terminal would send. appCursor selects SS3 arrow encodings (DECCKM).
func EncodeKey(msg tea.KeyMsg, appCursor bool) []byte {
	arrow := func(letter string) []byte {
		if appCursor {
			return []byte("\x1bO" + letter)
		}
		return []byte("\x1b[" + letter)
	}

	switch msg.Type {
	case tea.KeyRunes:
		if msg.Alt {
			return append([]byte{0x1b}, []byte(string(msg.Runes))...)
		}
		return []byte(string(msg.Runes))
	case tea.KeySpace:
		if msg.Alt {
			return []byte{0x1b, ' '}
		}
		return []byte{' '}
	case tea.KeyEnter:
		if msg.Alt {
			return []byte{0x1b, '\r'}
		}
		return []byte{'\r'}
	case tea.KeyTab:
		return []byte{'\t'}
	case tea.KeyShiftTab:
		return []byte("\x1b[Z")
	case tea.KeyBackspace:
		if msg.Alt {
			return []byte{0x1b, 0x7f}
		}
		return []byte{0x7f}
	case tea.KeyEsc:
		return []byte{0x1b}
	case tea.KeyUp:
		return arrow("A")
	case tea.KeyDown:
		return arrow("B")
	case tea.KeyRight:
		return arrow("C")
	case tea.KeyLeft:
		return arrow("D")
	case tea.KeyHome:
		return arrow("H")
	case tea.KeyEnd:
		return arrow("F")
	case tea.KeyPgUp:
		return []byte("\x1b[5~")
	case tea.KeyPgDown:
		return []byte("\x1b[6~")
	case tea.KeyCtrlPgUp:
		return []byte("\x1b[5;5~")
	case tea.KeyCtrlPgDown:
		return []byte("\x1b[6;5~")
	case tea.KeyShiftUp:
		return []byte("\x1b[1;2A")
	case tea.KeyShiftDown:
		return []byte("\x1b[1;2B")
	case tea.KeyShiftRight:
		return []byte("\x1b[1;2C")
	case tea.KeyShiftLeft:
		return []byte("\x1b[1;2D")
	case tea.KeyCtrlUp:
		return []byte("\x1b[1;5A")
	case tea.KeyCtrlDown:
		return []byte("\x1b[1;5B")
	case tea.KeyCtrlRight:
		return []byte("\x1b[1;5C")
	case tea.KeyCtrlLeft:
		return []byte("\x1b[1;5D")
	case tea.KeyCtrlHome:
		return []byte("\x1b[1;5H")
	case tea.KeyCtrlEnd:
		return []byte("\x1b[1;5F")
	case tea.KeyCtrlShiftUp:
		return []byte("\x1b[1;6A")
	case tea.KeyCtrlShiftDown:
		return []byte("\x1b[1;6B")
	case tea.KeyCtrlShiftRight:
		return []byte("\x1b[1;6C")
	case tea.KeyCtrlShiftLeft:
		return []byte("\x1b[1;6D")
	case tea.KeyDelete:
		return []byte("\x1b[3~")
	case tea.KeyInsert:
		return []byte("\x1b[2~")
	case tea.KeyF1:
		return []byte("\x1bOP")
	case tea.KeyF2:
		return []byte("\x1bOQ")
	case tea.KeyF3:
		return []byte("\x1bOR")
	case tea.KeyF4:
		return []byte("\x1bOS")
	case tea.KeyF5:
		return []byte("\x1b[15~")
	case tea.KeyF6:
		return []byte("\x1b[17~")
	case tea.KeyF7:
		return []byte("\x1b[18~")
	case tea.KeyF8:
		return []byte("\x1b[19~")
	case tea.KeyF9:
		return []byte("\x1b[20~")
	case tea.KeyF10:
		return []byte("\x1b[21~")
	case tea.KeyF11:
		return []byte("\x1b[23~")
	case tea.KeyF12:
		return []byte("\x1b[24~")
	}

	// Control characters: Bubble Tea key types 0..31 match the raw byte.
	if msg.Type >= 0 && msg.Type < 32 {
		return []byte{byte(msg.Type)}
	}
	return nil
}
