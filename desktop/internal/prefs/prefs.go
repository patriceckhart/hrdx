// Package prefs loads and saves desktop.json, the desktop-only preferences
// stored next to state.json. Every field is optional; a missing or corrupt
// file yields defaults and is never fatal.
package prefs

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// FileName is the preferences file beside state.json.
const FileName = "desktop.json"

// Window is the last main window geometry.
type Window struct {
	X         int  `json:"x"`
	Y         int  `json:"y"`
	Width     int  `json:"width"`
	Height    int  `json:"height"`
	Maximized bool `json:"maximized"`
}

// Prefs is the desktop.json document.
type Prefs struct {
	Window          *Window `json:"window,omitempty"`
	FontFamily      string  `json:"font_family"`
	FontSize        int     `json:"font_size,omitempty"`
	LineHeight      float64 `json:"line_height,omitempty"`
	CursorBlink     *bool   `json:"cursor_blink,omitempty"`
	ScrollbackLines int     `json:"scrollback_lines,omitempty"`
	SidebarWidth    int     `json:"sidebar_width,omitempty"` // pixels
}

// Defaults for unset fields.
const (
	DefaultFontSize        = 13
	DefaultLineHeight      = 1.2
	DefaultScrollbackLines = 10000
	DefaultSidebarWidth    = 260
)

// Normalized returns p with zero or out-of-range fields replaced by defaults.
func (p Prefs) Normalized() Prefs {
	if p.FontSize < 6 || p.FontSize > 72 {
		p.FontSize = DefaultFontSize
	}
	if p.LineHeight < 0.8 || p.LineHeight > 3 {
		p.LineHeight = DefaultLineHeight
	}
	if p.CursorBlink == nil {
		blink := true
		p.CursorBlink = &blink
	}
	if p.ScrollbackLines <= 0 || p.ScrollbackLines > 1_000_000 {
		p.ScrollbackLines = DefaultScrollbackLines
	}
	if p.SidebarWidth < 140 || p.SidebarWidth > 800 {
		p.SidebarWidth = DefaultSidebarWidth
	}
	if w := p.Window; w != nil && (w.Width < 200 || w.Height < 150) {
		p.Window = nil
	}
	return p
}

// Path returns the desktop.json path for a state file, "" when persistence
// is disabled.
func Path(statePath string) string {
	if statePath == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(statePath), FileName)
}

// Load reads path. Missing, unreadable, or corrupt files return defaults.
func Load(path string) Prefs {
	var loaded Prefs
	if path != "" {
		if data, err := os.ReadFile(path); err == nil {
			if json.Unmarshal(data, &loaded) != nil {
				loaded = Prefs{}
			}
		}
	}
	return loaded.Normalized()
}

// Save writes path atomically. An empty path is a no-op.
func Save(path string, p Prefs) error {
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	temp := path + ".tmp"
	if err := os.WriteFile(temp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(temp, path)
}
