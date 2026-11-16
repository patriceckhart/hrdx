package ui

import (
	"os"

	"github.com/patriceckhart/hrdx/internal/sound"
)

const defaultSoundKind = sound.DefaultKind

func loadSounds(dir string) string { return sound.Load(dir) }

func soundKindList() []string { return sound.Kinds() }

func isSoundKind(kind string) bool { return sound.IsKind(kind) }

// Replaceable so tests stay silent.
var (
	systemNotify = ringTerminalBell
	playSound    = sound.Play
)

// ringTerminalBell requests the platform's native attention indicator by
// ringing the terminal bell. Terminals translate BEL into their OS
// convention: on macOS a dock badge and bounce, on Linux the window
// manager's urgency hint (highlighted taskbar entry / attention flag).
// No notification daemon or permission is involved, which makes this
// the same native behavior everywhere.
func ringTerminalBell() {
	_, _ = os.Stdout.WriteString("\a")
}
