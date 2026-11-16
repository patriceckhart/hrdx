// Package sound owns hrdx's notification sounds: the embedded built-ins,
// sounds.json custom entries, and playback through OS audio players.
package sound

import (
	"embed"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// Embedded notification sounds. The files are bundled into the binary;
// on first play they are written to the state directory so external
// players can read them. Both are synthesized in-repo (no third-party
// audio): ding is a short bell strike, chime a softer two-tone.
//
//go:embed sounds/ding.wav sounds/chime.wav
var soundFiles embed.FS

// File declares custom notification sounds in the state directory.
const File = "sounds.json"

// Builtins are always available, embedded in the binary.
var Builtins = []string{"ding", "chime"}

// DefaultKind is the sound selected when none or an unknown one is saved.
const DefaultKind = "ding"

// customSound is one entry of sounds.json: a name for the settings list
// and an audio file path for the player.
type customSound struct {
	Name string `json:"name"`
	File string `json:"file"`
}

// registry holds the merged sound list.
var registry struct {
	sync.Mutex
	dir    string            // state dir for extraction and sounds.json
	custom []customSound     // loaded from sounds.json
	paths  map[string]string // materialized embedded sounds
}

// Load reads sounds.json from dir and registers valid entries.
// A missing file is fine. Returns a problem description or "".
func Load(dir string) string {
	registry.Lock()
	defer registry.Unlock()
	registry.dir = dir
	registry.custom = nil
	if dir == "" {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(dir, File))
	if err != nil {
		if os.IsNotExist(err) {
			return ""
		}
		return "sounds: " + err.Error()
	}
	var entries []customSound
	if err := json.Unmarshal(data, &entries); err != nil {
		return "sounds: " + err.Error()
	}
	var problems []string
	for _, entry := range entries {
		name := strings.TrimSpace(entry.Name)
		file := strings.TrimSpace(entry.File)
		switch {
		case name == "":
			problems = append(problems, "entry without name")
		case file == "":
			problems = append(problems, name+" has no file")
		case isBuiltin(name):
			problems = append(problems, name+" is built in")
		default:
			if strings.HasPrefix(file, "~/") {
				if home, err := os.UserHomeDir(); err == nil {
					file = filepath.Join(home, file[2:])
				}
			}
			if _, err := os.Stat(file); err != nil {
				problems = append(problems, name+": "+file+" not found")
				continue
			}
			registry.custom = append(registry.custom, customSound{Name: name, File: file})
		}
	}
	if len(problems) > 0 {
		return "sounds: " + strings.Join(problems, ", ")
	}
	return ""
}

func isBuiltin(kind string) bool {
	for _, current := range Builtins {
		if current == kind {
			return true
		}
	}
	return false
}

// Kinds returns every selectable sound: built-ins then custom.
func Kinds() []string {
	registry.Lock()
	defer registry.Unlock()
	kinds := append([]string{}, Builtins...)
	for _, entry := range registry.custom {
		kinds = append(kinds, entry.Name)
	}
	return kinds
}

// IsKind reports whether kind is selectable.
func IsKind(kind string) bool {
	for _, current := range Kinds() {
		if current == kind {
			return true
		}
	}
	return false
}

// Path resolves a kind to a playable file path: custom entries use
// their configured file, embedded sounds are extracted once. "" for the
// bell or when nothing can be resolved.
func Path(kind string) string {
	registry.Lock()
	defer registry.Unlock()
	for _, entry := range registry.custom {
		if entry.Name == kind {
			return entry.File
		}
	}
	if path, ok := registry.paths[kind]; ok {
		return path
	}
	data, err := soundFiles.ReadFile("sounds/" + kind + ".wav")
	if err != nil {
		return ""
	}
	dir := registry.dir
	if dir == "" {
		dir = os.TempDir()
	}
	path := filepath.Join(dir, "hrdx-"+kind+".wav")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return ""
	}
	if registry.paths == nil {
		registry.paths = map[string]string{}
	}
	registry.paths[kind] = path
	return path
}

// Play plays the configured notification sound in the background
// through an OS audio player. Players occasionally fail transiently
// (device switching, coreaudio hiccups), so each is retried once and
// the macOS system beep serves as a further fallback before the
// terminal bell.
func Play(kind string) {
	file := Path(kind)
	go func() {
		if file != "" {
			for _, player := range soundPlayers(file) {
				path, err := exec.LookPath(player[0])
				if err != nil {
					continue
				}
				for attempt := 0; attempt < 2; attempt++ {
					if exec.Command(path, player[1:]...).Run() == nil {
						return
					}
				}
			}
		}
		// macOS system beep, honoring the user's alert sound.
		if path, err := exec.LookPath("osascript"); err == nil {
			if exec.Command(path, "-e", "beep").Run() == nil {
				return
			}
		}
		_, _ = os.Stdout.WriteString("\a")
	}()
}

// soundPlayers lists the OS audio players to try, in order, for file.
// Windows has none of afplay/paplay/aplay; PowerShell's SoundPlayer class
// plays WAV files (the only format hrdx ever writes) without needing any
// extra binary, since PowerShell and .NET ship with every install.
func soundPlayers(file string) [][]string {
	if runtime.GOOS == "windows" {
		script := "(New-Object Media.SoundPlayer '" + strings.ReplaceAll(file, "'", "''") + "').PlaySync()"
		return [][]string{
			{"powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", script},
		}
	}
	return [][]string{
		{"afplay", file},
		{"paplay", file},
		{"aplay", "-q", file},
	}
}
