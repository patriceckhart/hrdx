// Package theme loads hrdx color themes, built-in presets and
// <statedir>/themes/*.json, as plain color strings free of any rendering
// library so every frontend can use them.
//
// Colors are ANSI 256 numbers ("81") or "#rrggbb" strings. A theme file can
// override any subset of the roles; missing values inherit the default theme.
package theme

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// Roles lists the palette roles in a stable order.
var Roles = []string{"accent", "alt", "muted", "faint", "good", "busy", "bad", "bar_bg", "bar_fg", "ink"}

// Palette maps a role to its color.
type Palette map[string]string

// Colors is the JSON color table. Values are ANSI 256 numbers or "#rrggbb"
// strings; both arrive as json.RawMessage and are normalized.
type Colors struct {
	Accent json.RawMessage `json:"accent,omitempty"` // frames, highlights, logo
	Alt    json.RawMessage `json:"alt,omitempty"`    // prefix badge, behind-count
	Muted  json.RawMessage `json:"muted,omitempty"`  // secondary text
	Faint  json.RawMessage `json:"faint,omitempty"`  // inactive borders
	Good   json.RawMessage `json:"good,omitempty"`   // running dots, ok badge
	Busy   json.RawMessage `json:"busy,omitempty"`   // busy spinner, completed-work dot
	Bad    json.RawMessage `json:"bad,omitempty"`    // errors, exited dots
	BarBg  json.RawMessage `json:"bar_bg,omitempty"` // header/footer background
	BarFg  json.RawMessage `json:"bar_fg,omitempty"` // header/footer text
	Ink    json.RawMessage `json:"ink,omitempty"`    // text on accent backgrounds
}

func (c Colors) byRole() map[string]json.RawMessage {
	return map[string]json.RawMessage{
		"accent": c.Accent, "alt": c.Alt, "muted": c.Muted, "faint": c.Faint,
		"good": c.Good, "busy": c.Busy, "bad": c.Bad,
		"bar_bg": c.BarBg, "bar_fg": c.BarFg, "ink": c.Ink,
	}
}

// File is one theme JSON document.
type File struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Colors      Colors `json:"colors,omitempty"`
}

// DefaultName identifies the original built-in theme.
const DefaultName = "default"

var registry struct {
	sync.Mutex
	themes map[string]File
}

// Reset forgets every loaded user theme.
func Reset() {
	registry.Lock()
	registry.themes = map[string]File{}
	registry.Unlock()
}

// Load reads <dir>/themes/*.json. Returns a problem summary or "".
func Load(dir string) string {
	registry.Lock()
	defer registry.Unlock()
	registry.themes = map[string]File{}
	if dir == "" {
		return ""
	}
	matches, err := filepath.Glob(filepath.Join(dir, "themes", "*.json"))
	if err != nil || len(matches) == 0 {
		return ""
	}
	sort.Strings(matches)
	var problems []string
	for _, path := range matches {
		data, err := os.ReadFile(path)
		if err != nil {
			problems = append(problems, filepath.Base(path)+": "+err.Error())
			continue
		}
		var loaded File
		if err := json.Unmarshal(data, &loaded); err != nil {
			problems = append(problems, filepath.Base(path)+": "+err.Error())
			continue
		}
		name := strings.TrimSpace(loaded.Name)
		if name == "" {
			name = strings.TrimSuffix(filepath.Base(path), ".json")
		}
		if IsBuiltIn(name) {
			problems = append(problems, filepath.Base(path)+": name '"+name+"' is reserved")
			continue
		}
		loaded.Name = name
		registry.themes[name] = loaded
	}
	if len(problems) > 0 {
		return "themes: " + strings.Join(problems, ", ")
	}
	return ""
}

// Names returns the selectable themes: the original default, bundled presets
// in their curated order, then user themes alphabetically.
func Names() []string {
	registry.Lock()
	defer registry.Unlock()
	names := []string{DefaultName}
	for _, preset := range Presets {
		names = append(names, preset.Name)
	}
	var user []string
	for name := range registry.themes {
		user = append(user, name)
	}
	sort.Strings(user)
	return append(names, user...)
}

// IsName reports whether name is selectable.
func IsName(name string) bool {
	for _, current := range Names() {
		if current == name {
			return true
		}
	}
	return false
}

// IsBuiltIn reports whether name is the default or a bundled preset.
func IsBuiltIn(name string) bool {
	if name == DefaultName {
		return true
	}
	_, ok := preset(name)
	return ok
}

// ParseColor converts a raw JSON value (number or "#hex"/"123" string) into
// a color string. ok is false for absent or invalid values.
func ParseColor(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 {
		return "", false
	}
	var number int
	if json.Unmarshal(raw, &number) == nil {
		if number >= 0 && number <= 255 {
			return fmt.Sprintf("%d", number), true
		}
		return "", false
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		text = strings.TrimSpace(text)
		if text != "" {
			return text, true
		}
	}
	return "", false
}

// Default is the built-in theme, matching hrdx's original look.
func Default() Palette {
	return Palette{
		"accent": "81",  // cyan
		"alt":    "212", // pink
		"muted":  "243",
		"faint":  "238",
		"good":   "78",
		"busy":   "214",
		"bad":    "203",
		"bar_bg": "234",
		"bar_fg": "250",
		"ink":    "232",
	}
}

// Resolve returns a theme's palette: the default with the theme's overrides
// on top. Unknown names resolve to the default.
func Resolve(name string) Palette {
	palette := Default()
	if found, ok := preset(name); ok {
		for key, color := range found.Colors {
			palette[key] = color
		}
		return palette
	}
	if name == DefaultName {
		return palette
	}
	registry.Lock()
	loaded, ok := registry.themes[name]
	registry.Unlock()
	if !ok {
		return palette
	}
	for key, raw := range loaded.Colors.byRole() {
		if color, ok := ParseColor(raw); ok {
			palette[key] = color
		}
	}
	return palette
}
