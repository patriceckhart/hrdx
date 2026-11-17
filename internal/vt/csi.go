package vt

import (
	"fmt"
	"strconv"
	"strings"
)

// CSI (Control Sequence Introducer)
// ESC+[
type csiEscape struct {
	buf    []byte
	args   []int
	mode   byte
	priv   bool // DEC private marker '?'
	prefix byte // private parameter prefix: '?', '>', '=', '<', or 0
}

func (c *csiEscape) reset() {
	c.buf = c.buf[:0]
	c.args = c.args[:0]
	c.mode = 0
	c.priv = false
	c.prefix = 0
}

func (c *csiEscape) put(b byte) bool {
	c.buf = append(c.buf, b)
	if b >= 0x40 && b <= 0x7E || len(c.buf) >= 256 {
		c.parse()
		return true
	}
	return false
}

func (c *csiEscape) parse() {
	c.mode = c.buf[len(c.buf)-1]
	if len(c.buf) == 1 {
		return
	}
	s := string(c.buf)
	c.args = c.args[:0]
	switch s[0] {
	case '?', '>', '=', '<':
		c.prefix = s[0]
		c.priv = s[0] == '?'
		s = s[1:]
	}
	s = s[:len(s)-1]
	ss := strings.Split(s, ";")
	for _, p := range ss {
		// ECMA-48 treats an omitted parameter as zero. Programs including
		// fzf emit SGR sequences such as CSI ;38;5;110m, where the leading
		// empty field resets attributes before applying the color.
		if p == "" {
			c.args = append(c.args, 0)
			continue
		}
		i, err := strconv.Atoi(p)
		if err != nil {
			//t.logf("invalid CSI arg '%s'\n", p)
			break
		}
		c.args = append(c.args, i)
	}
}

func (c *csiEscape) arg(i, def int) int {
	if i >= len(c.args) || i < 0 {
		return def
	}
	return c.args[i]
}

// defaultArg returns def when a numeric parameter is omitted or zero.
// ECMA-48 uses that rule for cursor movement and editing counts.
func (c *csiEscape) defaultArg(i, def int) int {
	value := c.arg(i, def)
	if value <= 0 {
		return def
	}
	return value
}

// prefixedCSI lists the private-prefixed sequences handleCSI implements.
// Every other prefixed sequence is a different command from its unprefixed
// namesake and must be ignored: CSI ? 4 m (an XTQMODKEYS query lazygit
// sends) is not SGR underline, CSI > 4 ; 2 m (modifyOtherKeys) is not an
// SGR reset, and the kitty keyboard CSI > 1 u, CSI < u, and CSI ? u are not
// DECRC.
var prefixedCSI = map[byte]string{
	'?': "hlJKncr", // DEC modes, selective erase, DSR, DA, DECSTBM (rejected)
	'>': "c",       // secondary device attributes
	'=': "c",       // tertiary device attributes
}

func (t *State) handleCSI() {
	c := &t.csi
	if c.prefix != 0 && !strings.ContainsRune(prefixedCSI[c.prefix], rune(c.mode)) {
		return
	}
	switch c.mode {
	default:
		goto unknown
	case '@': // ICH - insert <n> blank char
		t.insertBlanks(c.defaultArg(0, 1))
	case 'A': // CUU - cursor <n> up
		t.moveTo(t.cur.X, t.cur.Y-c.defaultArg(0, 1))
	case 'B', 'e': // CUD, VPR - cursor <n> down
		t.moveTo(t.cur.X, t.cur.Y+c.defaultArg(0, 1))
	case 'c': // DA1 - primary device attributes
		if !c.priv && c.arg(0, 0) == 0 {
			// Identify as a VT100 with the advanced video option. Shells such
			// as fish query this during startup and wait if no reply arrives.
			_, _ = t.w.Write([]byte("\033[?1;2c"))
		}
	case 'C', 'a': // CUF, HPR - cursor <n> forward
		t.moveTo(t.cur.X+c.defaultArg(0, 1), t.cur.Y)
	case 'D': // CUB - cursor <n> backward
		t.moveTo(t.cur.X-c.defaultArg(0, 1), t.cur.Y)
	case 'E': // CNL - cursor <n> down and first col
		t.moveTo(0, t.cur.Y+c.defaultArg(0, 1))
	case 'F': // CPL - cursor <n> up and first col
		t.moveTo(0, t.cur.Y-c.defaultArg(0, 1))
	case 'g': // TBC - tabulation clear
		switch c.arg(0, 0) {
		// clear current tab stop
		case 0:
			t.tabs[t.cur.X] = false
		// clear all tabs
		case 3:
			for i := range t.tabs {
				t.tabs[i] = false
			}
		default:
			goto unknown
		}
	case 'G', '`': // CHA, HPA - Move to <col>
		t.moveTo(c.defaultArg(0, 1)-1, t.cur.Y)
	case 'H', 'f': // CUP, HVP - move to <row> <col>
		t.moveAbsTo(c.defaultArg(1, 1)-1, c.defaultArg(0, 1)-1)
	case 'I': // CHT - cursor forward tabulation <n> tab stops
		n := c.defaultArg(0, 1)
		for i := 0; i < n; i++ {
			t.putTab(true)
		}
	case 'J': // ED - clear screen
		// TODO: sel.ob.x = -1
		switch c.arg(0, 0) {
		case 0: // below
			t.clear(t.cur.X, t.cur.Y, t.cols-1, t.cur.Y)
			if t.cur.Y < t.rows-1 {
				t.clear(0, t.cur.Y+1, t.cols-1, t.rows-1)
			}
		case 1: // above
			if t.cur.Y > 1 {
				t.clear(0, 0, t.cols-1, t.cur.Y-1)
			}
			t.clear(0, t.cur.Y, t.cur.X, t.cur.Y)
		case 2: // all
			t.clear(0, 0, t.cols-1, t.rows-1)
		default:
			goto unknown
		}
	case 'K': // EL - clear line
		switch c.arg(0, 0) {
		case 0: // right
			t.clear(t.cur.X, t.cur.Y, t.cols-1, t.cur.Y)
		case 1: // left
			t.clear(0, t.cur.Y, t.cur.X, t.cur.Y)
		case 2: // all
			t.clear(0, t.cur.Y, t.cols-1, t.cur.Y)
		}
	case 'S': // SU - scroll <n> lines up
		t.scrollUp(t.top, c.defaultArg(0, 1))
	case 'T': // SD - scroll <n> lines down
		t.scrollDown(t.top, c.defaultArg(0, 1))
	case 'L': // IL - insert <n> blank lines
		t.insertBlankLines(c.defaultArg(0, 1))
	case 'l': // RM - reset mode
		t.setMode(c.priv, false, c.args)
	case 'M': // DL - delete <n> lines
		t.deleteLines(c.defaultArg(0, 1))
	case 'X': // ECH - erase <n> chars
		t.clear(t.cur.X, t.cur.Y, t.cur.X+c.defaultArg(0, 1)-1, t.cur.Y)
	case 'P': // DCH - delete <n> chars
		t.deleteChars(c.defaultArg(0, 1))
	case 'Z': // CBT - cursor backward tabulation <n> tab stops
		n := c.defaultArg(0, 1)
		for i := 0; i < n; i++ {
			t.putTab(false)
		}
	case 'd': // VPA - move to <row>
		t.moveAbsTo(t.cur.X, c.defaultArg(0, 1)-1)
	case 'h': // SM - set terminal mode
		t.setMode(c.priv, true, c.args)
	case 'm': // SGR - terminal attribute (color)
		t.setAttr(c.args)
	case 'n':
		switch c.arg(0, 0) {
		case 5: // DSR - device status report
			t.w.Write([]byte("\033[0n"))
		case 6: // CPR - cursor position report
			t.w.Write([]byte(fmt.Sprintf("\033[%d;%dR", t.cur.Y+1, t.cur.X+1)))
		}
	case 'r': // DECSTBM - set scrolling region
		if c.priv {
			goto unknown
		} else {
			t.setScroll(c.defaultArg(0, 1)-1, c.defaultArg(1, t.rows)-1)
			t.moveAbsTo(0, 0)
		}
	case 's': // DECSC - save cursor position (ANSI.SYS)
		t.saveCursor()
	case 'u': // DECRC - restore cursor position (ANSI.SYS)
		t.restoreCursor()
	}
	return
unknown: // TODO: get rid of this goto
	t.logf("unknown CSI sequence '%c'\n", c.mode)
	// TODO: c.dump()
}
