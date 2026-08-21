package term

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aymanbagabas/go-pty"
	"github.com/patriceckhart/hrdx/internal/vt"
)

// SessionHost is the holder-side transport of a pane: a background
// process that owns the PTY so the session survives TUI restarts.
// Implemented by holder.Client.
type SessionHost interface {
	Write(session int64, data []byte)
	Resize(session int64, cols, rows int)
	Kill(session int64)
	Foreground(session int64) string
}

// Pane is one real terminal session: a subprocess on a PTY whose output is
// parsed into a virtual screen that the TUI renders as ANSI text. The PTY
// is either owned locally (pty/cmd) or lives in the session holder
// (host/session), in which case output arrives via Feed.
type Pane struct {
	mu              sync.Mutex
	vt              vt.Terminal
	pty             pty.Pty
	ptyCloseOnce    sync.Once
	cmd             *pty.Cmd
	host            SessionHost
	session         int64
	updates         chan struct{}
	exited          bool
	keyboardMode    [2]keyboardProtocolMode // shell and full-screen app views
	keyboardAlt     bool
	modifyOtherKeys bool // xterm fallback shared by both views
	scanTail        []byte

	// Foreground process caches. Mouse capture uses its own short-lived
	// cache so an exited TUI cannot leave mouse reporting active in a shell.
	fgName           string
	fgCheckedAt      time.Time
	mouseFgName      string
	mouseFgCheckedAt time.Time

	// scrollOffset counts lines scrolled back into history; 0 is live.
	scrollOffset int

	// selection, in stable line coordinates: line 0 is the oldest history
	// line, history length + screen row addresses the visible screen.
	selecting bool
	selStartX int
	selStartL int
	selEndX   int
	selEndL   int
	selActive bool
}

// Start launches command in cwd on a fresh PTY sized cols x rows.
func Start(command string, args []string, cwd string, cols, rows int) (*Pane, error) {
	if cols < 2 {
		cols = 80
	}
	if rows < 2 {
		rows = 24
	}

	ptmx, err := pty.New()
	if err != nil {
		return nil, fmt.Errorf("open pty: %w", err)
	}
	if err := ptmx.Resize(cols, rows); err != nil {
		ptmx.Close()
		return nil, fmt.Errorf("resize pty: %w", err)
	}

	cmd := ptmx.Command(resolveCommand(command), args...)
	cmd.Dir = cwd
	cmd.Env = PaneEnv()

	if err := cmd.Start(); err != nil {
		ptmx.Close()
		return nil, fmt.Errorf("start %s: %w", command, err)
	}

	pane := &Pane{
		vt:      vt.New(vt.WithSize(cols, rows), vt.WithWriter(ptmx)),
		pty:     ptmx,
		cmd:     cmd,
		updates: make(chan struct{}, 1),
	}
	go pane.reader()
	return pane, nil
}

// hostWriter forwards emulator responses (cursor reports etc.) to the
// holder session's PTY.
type hostWriter struct {
	host    SessionHost
	session int64
}

func (w hostWriter) Write(p []byte) (int, error) {
	w.host.Write(w.session, p)
	return len(p), nil
}

// NewHolderPane builds a pane whose PTY lives in the session holder.
// Output must be pushed in via Feed; exit via MarkExited.
func NewHolderPane(host SessionHost, session int64, cols, rows int) *Pane {
	if cols < 2 {
		cols = 80
	}
	if rows < 2 {
		rows = 24
	}
	return &Pane{
		vt:      vt.New(vt.WithSize(cols, rows), vt.WithWriter(hostWriter{host, session})),
		host:    host,
		session: session,
		updates: make(chan struct{}, 1),
	}
}

// HolderSession returns the holder session id, 0 for local panes.
func (p *Pane) HolderSession() int64 { return p.session }

// Feed processes output pushed from the holder. Safe from any goroutine.
func (p *Pane) Feed(data []byte) {
	p.mu.Lock()
	p.scanKeyboardProtocol(data)
	p.writeOutput(data)
	p.mu.Unlock()
	p.notify()
}

// MarkExited flags the subprocess as gone and closes the updates channel.
// Idempotent, safe from any goroutine.
func (p *Pane) MarkExited() {
	p.mu.Lock()
	if p.exited {
		p.mu.Unlock()
		return
	}
	p.exited = true
	p.mu.Unlock()
	close(p.updates)
}

// resolveCommand finds command on $PATH before handing it to go-pty. On
// Windows, go-pty's Cmd.Start resolves a bare command name against the
// child's working directory instead of $PATH when Dir is set, so agent
// binaries elsewhere on PATH would otherwise fail to start.
func resolveCommand(command string) string {
	if resolved, err := exec.LookPath(command); err == nil {
		return resolved
	}
	return command
}

// PaneEnv builds the environment for pane subprocesses. The outer
// terminal's identity is scrubbed so children never detect capabilities
// (like inline images) that hrdx's character-grid panes cannot deliver;
// TERM_PROGRAM=vscode makes capability-sniffing TUIs fall back to their
// conservative rendering path. HRDX=1 announces the multiplexer, like
// TMUX does, so tools can detect they run inside hrdx.
func PaneEnv() []string {
	drop := []string{
		"TERM=", "TERM_PROGRAM=", "TERM_PROGRAM_VERSION=",
		"KITTY_WINDOW_ID=", "KITTY_PID=", "KITTY_PUBLIC_KEY=", "KITTY_INSTALLATION_DIR=", "KITTY_LISTEN_ON=",
		"GHOSTTY_RESOURCES_DIR=", "GHOSTTY_BIN_DIR=", "GHOSTTY_SHELL_FEATURES=",
		"WEZTERM_EXECUTABLE=", "WEZTERM_PANE=", "WEZTERM_CONFIG_FILE=", "WEZTERM_CONFIG_DIR=", "WEZTERM_UNIX_SOCKET=", "WEZTERM_EXECUTABLE_DIR=",
		"ITERM_PROFILE=", "ITERM_SESSION_ID=",
		"HRDX=",
	}
	var env []string
	for _, entry := range os.Environ() {
		skip := false
		for _, prefix := range drop {
			if strings.HasPrefix(entry, prefix) {
				skip = true
				break
			}
		}
		if !skip {
			env = append(env, entry)
		}
	}
	return append(env,
		"TERM=xterm-256color",
		"TERM_PROGRAM=vscode",
		"HRDX=1",
	)
}

// Updates delivers a signal whenever the screen content changed. The channel
// closes when the subprocess exits.
func (p *Pane) Updates() <-chan struct{} { return p.updates }

func (p *Pane) reader() {
	// On Unix, the blocking Read below returns EOF on its own once the
	// child exits and the kernel drains the pty's buffer. ConPTY on
	// Windows has no such guarantee: the output pipe can stay open past
	// process exit, so Read would block forever. Waiting for the process
	// and then closing the pty unblocks Read there too; the brief delay
	// gives the last bit of ConPTY output a chance to arrive first. On
	// Unix this races harmlessly behind the natural EOF, which always
	// wins first in practice.
	go func() {
		_ = p.cmd.Wait()
		time.Sleep(150 * time.Millisecond)
		p.closePty()
	}()

	buffer := make([]byte, 32*1024)
	for {
		n, err := p.pty.Read(buffer)
		if n > 0 {
			p.mu.Lock()
			p.scanKeyboardProtocol(buffer[:n])
			p.writeOutput(buffer[:n])
			p.mu.Unlock()
			p.notify()
		}
		if err != nil {
			break
		}
	}
	p.MarkExited()
}

// writeOutput applies child output while preserving the user's scrollback
// position. New output adds history lines below the viewed window, so the
// offset must grow by the same amount. HistorySerial still advances after
// the bounded history buffer reaches its cap.
//
// p.mu must be held by the caller.
func (p *Pane) writeOutput(data []byte) {
	p.vt.Lock()
	before := p.vt.HistorySerial()
	p.vt.Unlock()
	_, _ = p.vt.Write(data)
	if p.scrollOffset == 0 {
		return
	}
	p.vt.Lock()
	after := p.vt.HistorySerial()
	limit := p.vt.HistoryLen()
	p.vt.Unlock()
	p.scrollOffset = min(limit, p.scrollOffset+int(after-before))
}

// keyboardProtocolMode is the Kitty keyboard setting for one terminal view.
// A pane has one view for its shell and another for full-screen apps, and Kitty
// requires each view to keep its own small push/pop stack.
type keyboardProtocolMode struct {
	kittyFlags int
	kittyStack []int
}

const (
	kittyKeyboardStackLimit = 8
	keyboardScanTailLimit   = 4096
)

// scanKeyboardProtocol follows keyboard-related escape sequences in pane
// output. It uses a small streaming parser because a PTY can split a sequence
// across reads. Called with p.mu held.
func (p *Pane) scanKeyboardProtocol(chunk []byte) {
	if len(chunk) == 0 && len(p.scanTail) == 0 {
		return
	}
	data := make([]byte, 0, len(p.scanTail)+len(chunk))
	data = append(data, p.scanTail...)
	data = append(data, chunk...)
	p.scanTail = p.scanTail[:0]

	for offset := 0; offset < len(data); {
		if data[offset] != '\x1b' {
			offset++
			continue
		}
		start := offset
		if offset+1 == len(data) {
			p.keepKeyboardScanTail(data[start:])
			return
		}

		switch data[offset+1] {
		case 'c': // RIS: reset the terminal, including keyboard modes
			p.resetKeyboardProtocol()
			offset += 2
		case '[':
			final := offset + 2
			for {
				if final == len(data) {
					p.keepKeyboardScanTail(data[start:])
					return
				}
				char := data[final]
				switch {
				case char == '\x1b':
					// ESC cancels an incomplete CSI and starts a new sequence.
					offset = final
				case char == 0x18 || char == 0x1a:
					// CAN and SUB cancel an incomplete CSI.
					offset = final + 1
				case char >= 0x40 && char <= 0x7e:
					p.applyKeyboardCSI(data[offset+2:final], char)
					offset = final + 1
				case char < 0x20 || char > 0x3f:
					// Invalid CSI byte: discard this sequence and recover.
					offset = final + 1
				default:
					final++
					continue
				}
				break
			}
		case '\x1b':
			// The second ESC starts a replacement sequence.
			offset++
		default:
			offset += 2
		}
	}
}

func (p *Pane) keepKeyboardScanTail(data []byte) {
	if len(data) <= keyboardScanTailLimit {
		p.scanTail = append(p.scanTail, data...)
	}
}

func (p *Pane) resetKeyboardProtocol() {
	p.keyboardMode = [2]keyboardProtocolMode{}
	p.keyboardAlt = false
	p.modifyOtherKeys = false
}

func parseKeyboardParams(body []byte) ([]int, bool) {
	if len(body) == 0 {
		return nil, true
	}
	fields := strings.Split(string(body), ";")
	values := make([]int, len(fields))
	for index, field := range fields {
		if field == "" {
			continue
		}
		value, err := strconv.Atoi(field)
		if err != nil || value < 0 {
			return nil, false
		}
		values[index] = value
	}
	return values, true
}

func (p *Pane) applyKeyboardCSI(body []byte, final byte) {
	// These sequences switch between the shell view and the view used by
	// full-screen apps. Kitty gives each view its own keyboard stack.
	if (final == 'h' || final == 'l') && len(body) > 1 && body[0] == '?' {
		for _, field := range strings.Split(string(body[1:]), ";") {
			value, err := strconv.Atoi(field)
			if err == nil && (value == 47 || value == 1047 || value == 1049) {
				p.keyboardAlt = final == 'h'
			}
		}
		return
	}

	mode := &p.keyboardMode[0]
	if p.keyboardAlt {
		mode = &p.keyboardMode[1]
	}

	if final == 'u' && len(body) > 0 {
		marker := body[0]
		if marker != '>' && marker != '=' && marker != '<' {
			return // a key report or query, not a mode change
		}
		params, ok := parseKeyboardParams(body[1:])
		if !ok {
			return
		}
		value := 0
		if len(params) > 0 {
			value = params[0]
		}
		switch marker {
		case '>': // save the current flags and use the requested flags
			if len(mode.kittyStack) == kittyKeyboardStackLimit {
				copy(mode.kittyStack, mode.kittyStack[1:])
				mode.kittyStack = mode.kittyStack[:kittyKeyboardStackLimit-1]
			}
			mode.kittyStack = append(mode.kittyStack, mode.kittyFlags)
			mode.kittyFlags = value
		case '=': // replace, add, or remove selected flags
			how := 1
			if len(params) > 1 {
				how = params[1]
			}
			switch how {
			case 1:
				mode.kittyFlags = value
			case 2:
				mode.kittyFlags |= value
			case 3:
				mode.kittyFlags &^= value
			}
		case '<': // restore one saved mode by default
			count := value
			if count == 0 {
				count = 1
			}
			for range count {
				if len(mode.kittyStack) == 0 {
					mode.kittyFlags = 0
					break
				}
				last := len(mode.kittyStack) - 1
				mode.kittyFlags = mode.kittyStack[last]
				mode.kittyStack = mode.kittyStack[:last]
			}
		}
		return
	}

	// Xterm's fallback setting belongs to the whole terminal rather than one
	// view. CSI > 4 ; 2 m enables it; CSI > 4 m disables it.
	if final == 'm' && len(body) > 0 && body[0] == '>' {
		fields := strings.Split(string(body[1:]), ";")
		if fields[0] != "4" {
			return
		}
		if len(fields) == 1 {
			p.modifyOtherKeys = false
			return
		}
		if len(fields) != 2 {
			return
		}
		value, err := strconv.Atoi(fields[1])
		p.modifyOtherKeys = err == nil && value == 2
	}
}

// fgCacheTTL bounds how often ForegroundCommand does the ioctl + process
// name lookup; the sidebar queries it on every render.
const fgCacheTTL = 2 * time.Second

// ForegroundCommand returns the name of the foreground process on the
// pane's PTY (e.g. "zsh" for an idle shell, "zot" while zot runs in it).
// The result is cached briefly; "" when it cannot be determined.
func (p *Pane) ForegroundCommand() string {
	p.mu.Lock()
	if p.exited {
		p.mu.Unlock()
		return ""
	}
	if time.Since(p.fgCheckedAt) < fgCacheTTL {
		name := p.fgName
		p.mu.Unlock()
		return name
	}
	p.fgCheckedAt = time.Now()
	host, session := p.host, p.session
	local := p.pty
	var rootPID int
	if host == nil && p.cmd.Process != nil {
		rootPID = p.cmd.Process.Pid
	}
	p.mu.Unlock()

	// Holder panes ask the holder, which resolves the foreground process
	// on its side; local panes resolve it against their own PTY.
	name := ""
	if host != nil {
		name = host.Foreground(session)
	} else if local != nil {
		name = foregroundName(local, rootPID)
	}

	p.mu.Lock()
	p.fgName = name
	p.mu.Unlock()
	return name
}

// KittyKeys reports whether the child requested enhanced (CSI-u) keyboard
// input, so chords like ctrl+1 can be forwarded in their native encoding.
func (p *Pane) KittyKeys() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	index := 0
	if p.keyboardAlt {
		index = 1
	}
	return p.keyboardMode[index].kittyFlags != 0 || p.modifyOtherKeys
}

// HasSpinner reports whether the visible screen contains a braille spinner
// glyph (U+2800..U+28FF). zot's TUI shows one while a turn is running, so
// this is screen-evidence that the agent is working rather than idle.
func (p *Pane) HasSpinner() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.exited {
		return false
	}
	p.vt.Lock()
	defer p.vt.Unlock()
	cols, rows := p.vt.Size()
	for y := 0; y < rows; y++ {
		for x := 0; x < cols; x++ {
			char := p.vt.Cell(x, y).Char
			if char >= 0x2801 && char <= 0x28ff {
				return true
			}
		}
	}
	return false
}

// HasScreenText reports whether the visible screen contains the given
// substring, ignoring styling. Rows are matched individually with runs of
// nulls/spaces collapsed, so cell padding does not break the match.
func (p *Pane) HasScreenText(needle string) bool {
	if needle == "" {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.exited {
		return false
	}
	p.vt.Lock()
	defer p.vt.Unlock()
	cols, rows := p.vt.Size()
	var row bytes.Buffer
	for y := 0; y < rows; y++ {
		row.Reset()
		lastSpace := false
		for x := 0; x < cols; x++ {
			char := p.vt.Cell(x, y).Char
			if char == 0 {
				char = ' '
			}
			if char == ' ' {
				if lastSpace {
					continue
				}
				lastSpace = true
			} else {
				lastSpace = false
			}
			row.WriteRune(char)
		}
		if strings.Contains(row.String(), needle) {
			return true
		}
	}
	return false
}

// PlainScreen returns the visible screen as unstyled text, one line per
// row with trailing spaces trimmed.
func (p *Pane) PlainScreen() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.vt.Lock()
	defer p.vt.Unlock()
	cols, rows := p.vt.Size()
	var out bytes.Buffer
	for y := 0; y < rows; y++ {
		line := make([]rune, 0, cols)
		for x := 0; x < cols; x++ {
			line = append(line, glyphChar(p.vt.Cell(x, y).Char))
		}
		end := len(line)
		for end > 0 && line[end-1] == ' ' {
			end--
		}
		out.WriteString(string(line[:end]))
		if y != rows-1 {
			out.WriteByte('\n')
		}
	}
	return out.String()
}

func (p *Pane) notify() {
	select {
	case p.updates <- struct{}{}:
	default:
	}
}

// Write forwards raw input bytes (key encodings) to the subprocess and
// snaps the view back to live output.
func (p *Pane) Write(data []byte) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.scrollOffset = 0
	if p.exited {
		return
	}
	if p.host != nil {
		p.host.Write(p.session, data)
		return
	}
	_, _ = p.pty.Write(data)
}

// Resize grows or shrinks both the PTY and the virtual screen.
func (p *Pane) Resize(cols, rows int) {
	if cols < 2 || rows < 2 {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.exited {
		return
	}
	p.vt.Resize(cols, rows)
	if p.host != nil {
		p.host.Resize(p.session, cols, rows)
		return
	}
	_ = p.pty.Resize(cols, rows)
}

// Running reports whether the subprocess is still alive.
func (p *Pane) Running() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return !p.exited
}

// Title returns the OSC window title set by the subprocess, if any.
func (p *Pane) Title() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.vt.Title()
}

// CursorCell reports the child's cursor cell and whether it is visible in
// live view. Used to park the host terminal's hardware cursor there so IME
// and dead-key composition previews appear at the focused input point.
func (p *Pane) CursorCell() (x, y int, visible bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.vt.Lock()
	defer p.vt.Unlock()
	cursor := p.vt.Cursor()
	return cursor.X, cursor.Y, p.vt.CursorVisible() && !p.exited && p.scrollOffset == 0
}

// AppCursor reports whether the subprocess enabled application cursor keys.
func (p *Pane) AppCursor() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.vt.Mode()&vt.ModeAppCursor != 0
}

// BracketedPaste reports whether the subprocess enabled bracketed paste
// mode (DECSET 2004), so pastes can be wrapped in ESC[200~ / ESC[201~.
func (p *Pane) BracketedPaste() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.vt.Mode()&vt.ModeBracketedPaste != 0
}

// MouseEnabled reports whether the subprocess asked for mouse reporting.
func (p *Pane) MouseEnabled() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.vt.Mode()&vt.ModeMouseMask != 0
}

// MouseCapturing reports whether mouse events should be forwarded to the
// foreground application. A TUI can crash or exit without disabling mouse
// reporting, leaving the VT mode set after control returns to the shell. Check
// the foreground process before forwarding so trackpad events do not become
// visible SGR mouse sequences at the shell prompt.
func (p *Pane) MouseCapturing() bool {
	p.mu.Lock()
	if p.exited || p.vt.Mode()&vt.ModeMouseMask == 0 {
		p.mu.Unlock()
		return false
	}
	if time.Since(p.mouseFgCheckedAt) < 250*time.Millisecond {
		name := p.mouseFgName
		p.mu.Unlock()
		return name == "" || !isShellCommand(name)
	}
	host, session := p.host, p.session
	local := p.pty
	var rootPID int
	if host == nil && p.cmd != nil && p.cmd.Process != nil {
		rootPID = p.cmd.Process.Pid
	}
	p.mu.Unlock()

	name := ""
	if host != nil {
		name = host.Foreground(session)
	} else if local != nil {
		name = foregroundName(local, rootPID)
	}

	p.mu.Lock()
	p.mouseFgName = name
	p.mouseFgCheckedAt = time.Now()
	p.mu.Unlock()
	return name == "" || !isShellCommand(name)
}

func isShellCommand(name string) bool {
	name = strings.ToLower(strings.TrimPrefix(name, "-"))
	name = strings.TrimSuffix(name, ".exe")
	switch name {
	case "sh", "bash", "dash", "zsh", "fish", "ksh", "tcsh", "csh", "ash",
		"nu", "nushell", "elvish", "xonsh", "cmd", "command", "powershell", "pwsh":
		return true
	default:
		return false
	}
}

// AltScreen reports whether the child runs a full-screen app.
func (p *Pane) AltScreen() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.vt.Lock()
	defer p.vt.Unlock()
	return p.vt.AltScreen()
}

// Scroll moves the view into history (positive delta) or back toward live
// output (negative delta). Returns true when the offset changed.
func (p *Pane) Scroll(delta int) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.vt.Lock()
	limit := p.vt.HistoryLen()
	p.vt.Unlock()
	next := p.scrollOffset + delta
	if next < 0 {
		next = 0
	}
	if next > limit {
		next = limit
	}
	changed := next != p.scrollOffset
	p.scrollOffset = next
	return changed
}

// ScrollOffset returns the current scrollback offset in lines.
func (p *Pane) ScrollOffset() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.scrollOffset
}

// ResetScroll snaps back to live output.
func (p *Pane) ResetScroll() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.scrollOffset = 0
}

// StartSelection begins a text selection at visible cell (x, y).
func (p *Pane) StartSelection(x, y int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	line := p.lineForRow(y)
	p.selecting = true
	p.selActive = true
	p.selStartX, p.selStartL = x, line
	p.selEndX, p.selEndL = x, line
}

// ExtendSelection updates the selection end while dragging.
func (p *Pane) ExtendSelection(x, y int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.selecting {
		return
	}
	p.selEndX, p.selEndL = x, p.lineForRow(y)
}

// FinishSelection ends the drag, keeping the highlight.
func (p *Pane) FinishSelection() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.selecting = false
}

// ClearSelection removes the highlight.
func (p *Pane) ClearSelection() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.selecting = false
	p.selActive = false
}

// HasSelection reports whether a selection highlight exists.
func (p *Pane) HasSelection() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.selActive
}

// lineForRow converts a visible row into a stable line index taking the
// scroll offset into account. Called with p.mu held.
func (p *Pane) lineForRow(y int) int {
	p.vt.Lock()
	defer p.vt.Unlock()
	return p.vt.HistoryLen() - p.scrollOffset + y
}

// SelectionText returns the selected text with trailing spaces trimmed per
// line and newlines between lines.
func (p *Pane) SelectionText() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.selActive {
		return ""
	}
	p.vt.Lock()
	defer p.vt.Unlock()

	startL, startX, endL, endX := p.orderedSelection()
	cols, _ := p.vt.Size()

	var out bytes.Buffer
	for line := startL; line <= endL; line++ {
		fromX, toX := 0, cols-1
		if line == startL {
			fromX = startX
		}
		if line == endL {
			toX = endX
		}
		text := p.lineText(line, fromX, toX)
		out.WriteString(text)
		if line != endL {
			out.WriteByte('\n')
		}
	}
	return out.String()
}

// lineText extracts trimmed text from one stable line. Lock held.
func (p *Pane) lineText(line, fromX, toX int) string {
	cols, rows := p.vt.Size()
	historyLen := p.vt.HistoryLen()
	var runes []rune
	if line < historyLen {
		glyphs := p.vt.HistoryLine(line)
		for x := fromX; x <= toX && x < len(glyphs); x++ {
			runes = append(runes, glyphChar(glyphs[x].Char))
		}
	} else {
		row := line - historyLen
		if row < 0 || row >= rows {
			return ""
		}
		for x := fromX; x <= toX && x < cols; x++ {
			runes = append(runes, glyphChar(p.vt.Cell(x, row).Char))
		}
	}
	end := len(runes)
	for end > 0 && runes[end-1] == ' ' {
		end--
	}
	return string(runes[:end])
}

func glyphChar(char rune) rune {
	if char == 0 {
		return ' '
	}
	return char
}

// orderedSelection normalizes the selection so start <= end. Lock held.
func (p *Pane) orderedSelection() (startL, startX, endL, endX int) {
	startL, startX = p.selStartL, p.selStartX
	endL, endX = p.selEndL, p.selEndX
	if startL > endL || (startL == endL && startX > endX) {
		startL, endL = endL, startL
		startX, endX = endX, startX
	}
	return startL, startX, endL, endX
}

// closePty closes the local PTY exactly once. The exit-detection goroutine
// in reader and an explicit Close can both race to close it (Windows'
// ConPTY especially: closing an already-closed handle while a read is in
// flight can crash rather than error out cleanly), so every close goes
// through this guard.
func (p *Pane) closePty() {
	p.ptyCloseOnce.Do(func() {
		_ = p.pty.Close()
	})
}

// Close terminates the subprocess and releases the PTY. For holder
// panes the session is killed in the holder.
func (p *Pane) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.exited {
		return
	}
	if p.host != nil {
		p.host.Kill(p.session)
		return
	}
	if p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
	}
	p.closePty()
}

// Detach releases the pane's local resources without touching the
// remote session; used when the TUI quits but the holder keeps running.
func (p *Pane) Detach() {
	p.MarkExited()
}
