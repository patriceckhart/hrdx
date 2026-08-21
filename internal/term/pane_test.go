package term

import (
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"
)

// shellCommand returns a shell and args that run script as one inline
// command. The scripts passed to startShellPane are written to be valid
// under both /bin/sh and PowerShell (quoted multi-word echo args, and
// the "sleep" alias PowerShell ships for Start-Sleep).
func shellCommand(script string) (string, []string) {
	if runtime.GOOS != "windows" {
		return "/bin/sh", []string{"-c", script}
	}
	return "powershell.exe", []string{"-NoLogo", "-NoProfile", "-NonInteractive", "-Command", script}
}

func startShellPane(t *testing.T, script string) *Pane {
	t.Helper()
	path, args := shellCommand(script)
	pane, err := Start(path, args, t.TempDir(), 40, 6)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(pane.Close)
	return pane
}

func waitExit(t *testing.T, pane *Pane) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case _, open := <-pane.Updates():
			if !open {
				return
			}
		case <-deadline:
			t.Fatal("pane did not exit")
		}
	}
}

func TestScrollbackAndSelection(t *testing.T) {
	pane := startShellPane(t, "sleep 30")
	var initial strings.Builder
	for i := 1; i <= 20; i++ {
		fmt.Fprintf(&initial, "line-%d\n", i)
	}
	pane.Feed([]byte(initial.String()))

	// 20 lines on a 6-row screen must have filled history.
	if pane.Scroll(1000) == false {
		t.Fatal("expected scrollback to be available")
	}
	offset := pane.ScrollOffset()
	if offset == 0 {
		t.Fatal("offset = 0 after scrolling up")
	}

	top := pane.Render(false)
	if !strings.Contains(stripANSI(top), "line-1") {
		t.Fatalf("scrolled view missing early output:\n%s", stripANSI(top))
	}

	// Output arriving while scrolled back must not move the visible window.
	pane.Feed([]byte("later-1\nlater-2\nlater-3\nlater-4\nlater-5\nlater-6\n"))
	if got := pane.Render(false); got != top {
		t.Fatalf("scrollback view shifted after new output:\nwant %s\ngot  %s", stripANSI(top), stripANSI(got))
	}

	pane.ResetScroll()
	if pane.ScrollOffset() != 0 {
		t.Fatal("ResetScroll did not reset")
	}
	live := stripANSI(pane.Render(false))
	if !strings.Contains(live, "later-6") {
		t.Fatalf("live view missing latest output:\n%s", live)
	}

	// Select the first visible row in live view and check the text.
	pane.StartSelection(0, 0)
	pane.ExtendSelection(39, 1)
	pane.FinishSelection()
	text := pane.SelectionText()
	if !strings.Contains(text, "later-") {
		t.Fatalf("selection text = %q", text)
	}
	pane.ClearSelection()
	if pane.HasSelection() {
		t.Fatal("selection still active after clear")
	}
}

func TestPaneEnv(t *testing.T) {
	t.Setenv("KITTY_WINDOW_ID", "7")
	t.Setenv("TERM_PROGRAM", "ghostty")
	t.Setenv("TERM", "xterm-kitty")
	t.Setenv("HRDX", "stale")
	t.Setenv("MY_APP_SETTING", "kept")

	env := PaneEnv()
	got := map[string]string{}
	for _, entry := range env {
		key, value, _ := strings.Cut(entry, "=")
		got[key] = value
	}
	if got["TERM"] != "xterm-256color" {
		t.Fatalf("TERM = %q, want xterm-256color", got["TERM"])
	}
	if got["TERM_PROGRAM"] != "vscode" {
		t.Fatalf("TERM_PROGRAM = %q, want vscode", got["TERM_PROGRAM"])
	}
	if got["HRDX"] != "1" {
		t.Fatalf("HRDX = %q, want 1", got["HRDX"])
	}
	if _, ok := got["KITTY_WINDOW_ID"]; ok {
		t.Fatal("KITTY_WINDOW_ID must be scrubbed")
	}
	if got["MY_APP_SETTING"] != "kept" {
		t.Fatal("unrelated variables must survive")
	}
	// No duplicate keys: the appended values must be the only ones.
	count := 0
	for _, entry := range env {
		if strings.HasPrefix(entry, "TERM=") {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("TERM appears %d times, want 1", count)
	}
}

func TestHasScreenText(t *testing.T) {
	pane := startShellPane(t, "echo 'working on it'")
	waitExit(t, pane)
	// The pane has exited, so HasScreenText must be false even though
	// the text is on screen.
	if pane.HasScreenText("working on it") {
		t.Fatal("exited pane must not report screen text")
	}

	live := startShellPane(t, "echo 'busy marker'; sleep 30")
	deadline := time.Now().Add(5 * time.Second)
	for !live.HasScreenText("busy marker") {
		if time.Now().After(deadline) {
			t.Fatal("screen text not found")
		}
		time.Sleep(50 * time.Millisecond)
	}
	if live.HasScreenText("absent needle") {
		t.Fatal("absent text must not match")
	}
	if live.HasScreenText("") {
		t.Fatal("empty needle must not match")
	}
}

func TestForegroundCommand(t *testing.T) {
	// PowerShell's "sleep" is an in-process alias for Start-Sleep (no
	// child process), so Windows needs a script that spawns a real
	// external process for the toolhelp32 walk to find.
	script, match := "sleep 30", func(name string) bool { return name == "sleep" || name == "sh" }
	if runtime.GOOS == "windows" {
		script = "ping -n 31 127.0.0.1 > $null"
		match = func(name string) bool { return name == "ping" } // exeBaseName lowercases PING.EXE
	}
	pane := startShellPane(t, script)
	deadline := time.Now().Add(5 * time.Second)
	for {
		pane.mu.Lock()
		pane.fgCheckedAt = time.Time{} // bypass the cache while polling
		pane.mu.Unlock()
		name := pane.ForegroundCommand()
		if match(name) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("foreground = %q, no match", name)
		}
		time.Sleep(50 * time.Millisecond)
	}
	pane.Close()
	waitExit(t, pane)
	if name := pane.ForegroundCommand(); name != "" {
		t.Fatalf("foreground after exit = %q, want empty", name)
	}
}

func TestEncodePaste(t *testing.T) {
	if got := string(EncodePaste("hello\nworld\r\n", false)); got != "hello\nworld\r\n" {
		t.Fatalf("plain paste = %q", got)
	}
	if got := string(EncodePaste("hi\nthere", true)); got != "\x1b[200~hi\nthere\x1b[201~" {
		t.Fatalf("bracketed paste = %q", got)
	}
}

func TestSynchronizedOutputStillNotifiesOuterRenderer(t *testing.T) {
	pane := NewHolderPane(nil, 1, 40, 6)
	pane.Feed([]byte("\x1b[?2026h~"))

	select {
	case <-pane.Updates():
	case <-time.After(time.Second):
		t.Fatal("output update was suppressed while synchronized mode remained set")
	}
}

type foregroundHost struct{ name string }

func (h *foregroundHost) Write(int64, []byte)     {}
func (h *foregroundHost) Resize(int64, int, int)  {}
func (h *foregroundHost) Kill(int64)              {}
func (h *foregroundHost) Foreground(int64) string { return h.name }

func TestMouseCapturingRejectsStaleShellMode(t *testing.T) {
	host := &foregroundHost{name: "zsh"}
	pane := NewHolderPane(host, 1, 40, 6)
	pane.Feed([]byte("\x1b[?1000h\x1b[?1006h"))
	if !pane.MouseEnabled() {
		t.Fatal("mouse mode was not enabled")
	}
	if pane.MouseCapturing() {
		t.Fatal("stale mouse mode at a shell prompt must not capture")
	}

	host.name = "vim"
	pane.mu.Lock()
	pane.mouseFgCheckedAt = time.Time{}
	pane.mu.Unlock()
	if !pane.MouseCapturing() {
		t.Fatal("mouse-enabled foreground TUI should capture")
	}
}

func TestShellCommandNames(t *testing.T) {
	for _, name := range []string{"zsh", "-bash", "fish", "pwsh.exe", "PowerShell.EXE"} {
		if !isShellCommand(name) {
			t.Errorf("isShellCommand(%q) = false", name)
		}
	}
	for _, name := range []string{"vim", "zot", "", "shellcheck"} {
		if isShellCommand(name) {
			t.Errorf("isShellCommand(%q) = true", name)
		}
	}
}

func TestKittyKeyboardModeDoesNotLeakOutOfAltScreen(t *testing.T) {
	pane := NewHolderPane(nil, 1, 40, 6)

	// Full-screen children enable kitty keyboard reporting on the alternate
	// screen. Its keyboard-mode stack is separate from the shell's main-screen
	// stack, so leaving the alternate screen must restore the shell's mode even
	// when the child exits without an explicit kitty pop sequence.
	pane.Feed([]byte("\x1b[?1049h"))
	pane.Feed([]byte("\x1b[>1u"))
	if !pane.KittyKeys() {
		t.Fatal("kitty keyboard mode was not detected on the alternate screen")
	}

	pane.Feed([]byte("\x1b[?1049l"))
	if pane.KittyKeys() {
		t.Fatal("kitty keyboard mode leaked from the exited child into the shell")
	}
}

func TestKittyKeyboardModesFollowProtocolSemantics(t *testing.T) {
	pane := NewHolderPane(nil, 1, 40, 6)

	pane.Feed([]byte("\x1b[=3u"))
	pane.Feed([]byte("\x1b[=1;3u")) // clear flag 1, leave flag 2 set
	if got := pane.keyboardMode[0].kittyFlags; got != 2 {
		t.Fatalf("flags after selective clear = %d, want 2", got)
	}
	pane.Feed([]byte("\x1b[=1;2u")) // add flag 1
	if got := pane.keyboardMode[0].kittyFlags; got != 3 {
		t.Fatalf("flags after selective set = %d, want 3", got)
	}
	pane.Feed([]byte("\x1b[=4;1u")) // replace all flags
	if got := pane.keyboardMode[0].kittyFlags; got != 4 {
		t.Fatalf("flags after replacement = %d, want 4", got)
	}
}

func TestKittyKeyboardViewsKeepIndependentModes(t *testing.T) {
	pane := NewHolderPane(nil, 1, 40, 6)
	pane.Feed([]byte("\x1b[>1u"))
	pane.Feed([]byte("\x1b[?1049h"))
	if pane.KittyKeys() {
		t.Fatal("shell keyboard mode leaked into the full-screen view")
	}
	pane.Feed([]byte("\x1b[>2u"))
	pane.Feed([]byte("\x1b[?1049l"))
	if !pane.KittyKeys() || pane.keyboardMode[0].kittyFlags != 1 {
		t.Fatal("returning to the shell did not restore its keyboard mode")
	}
}

func TestModifyOtherKeysIsSharedAcrossViews(t *testing.T) {
	pane := NewHolderPane(nil, 1, 40, 6)
	pane.Feed([]byte("\x1b[>4;2m"))
	pane.Feed([]byte("\x1b[?1049h"))
	if !pane.KittyKeys() {
		t.Fatal("modifyOtherKeys was lost on entry to the full-screen view")
	}
	pane.Feed([]byte("\x1b[?1049l"))
	if !pane.KittyKeys() {
		t.Fatal("modifyOtherKeys was lost on return to the shell view")
	}
	pane.Feed([]byte("\x1b[>4m"))
	if pane.KittyKeys() {
		t.Fatal("modifyOtherKeys remained enabled after its reset")
	}
}

func TestTerminalResetClearsKeyboardModes(t *testing.T) {
	pane := NewHolderPane(nil, 1, 40, 6)
	pane.Feed([]byte("\x1b[>1u\x1b[?1049h\x1b[>2u\x1b[>4;2m\x1bc"))
	if pane.KittyKeys() || pane.keyboardAlt || pane.modifyOtherKeys {
		t.Fatal("terminal reset left keyboard mode enabled")
	}
	for index, mode := range pane.keyboardMode {
		if mode.kittyFlags != 0 || len(mode.kittyStack) != 0 {
			t.Fatalf("terminal reset left view %d state: %+v", index, mode)
		}
	}
}

func TestKittyKeyboardStackIsBounded(t *testing.T) {
	pane := NewHolderPane(nil, 1, 40, 6)
	for value := 1; value <= kittyKeyboardStackLimit+5; value++ {
		pane.Feed([]byte(fmt.Sprintf("\x1b[>%du", value)))
	}
	if got := len(pane.keyboardMode[0].kittyStack); got != kittyKeyboardStackLimit {
		t.Fatalf("keyboard stack size = %d, want %d", got, kittyKeyboardStackLimit)
	}
	pane.Feed([]byte("\x1b[<999u"))
	if pane.KittyKeys() || len(pane.keyboardMode[0].kittyStack) != 0 {
		t.Fatal("oversized pop did not empty and reset the keyboard stack")
	}
}

func TestKeyboardProtocolScannerRecoversAcrossReads(t *testing.T) {
	pane := NewHolderPane(nil, 1, 40, 6)
	pane.Feed(nil) // empty output must be harmless

	for _, part := range []byte("\x1b[>1u") {
		pane.Feed([]byte{part})
	}
	if !pane.KittyKeys() {
		t.Fatal("split kitty sequence was not detected")
	}

	pane.Feed([]byte("\x1bc"))
	pane.Feed([]byte("\x1b[>1\x1b[>1u"))
	if !pane.KittyKeys() {
		t.Fatal("scanner did not recover from an interrupted CSI sequence")
	}

	pane.Feed([]byte("\x1bc"))
	long := "\x1b[" + strings.Repeat("1;", 80)
	pane.Feed([]byte(long))
	pane.Feed([]byte("1m\x1b[>1u"))
	if !pane.KittyKeys() {
		t.Fatal("scanner lost the sequence after a long split CSI")
	}
}

func TestBracketedPasteMode(t *testing.T) {
	script := "printf '\\033[?2004h'; sleep 5"
	if runtime.GOOS == "windows" {
		script = `[Console]::Out.Write("$([char]27)[?2004h"); Start-Sleep -Seconds 5`
	}
	pane := startShellPane(t, script)
	deadline := time.Now().Add(3 * time.Second)
	for !pane.BracketedPaste() {
		if time.Now().After(deadline) {
			t.Fatal("pane never reported bracketed paste mode")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func stripANSI(value string) string {
	var out strings.Builder
	inEscape := false
	for _, r := range value {
		switch {
		case inEscape:
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEscape = false
			}
		case r == 0x1b:
			inEscape = true
		default:
			out.WriteRune(r)
		}
	}
	return out.String()
}
