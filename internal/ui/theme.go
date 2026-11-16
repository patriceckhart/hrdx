package ui

import (
	"encoding/json"

	"github.com/charmbracelet/lipgloss"
	"github.com/patriceckhart/hrdx/internal/theme"
)

// hrdx themes are JSON files that override any subset of the built-in
// colors. Loading, presets, and resolution live in internal/theme; this file
// maps a resolved palette onto the TUI's lipgloss styles.

const defaultThemeName = theme.DefaultName

var builtInThemePresets = theme.Presets

func loadThemes(dir string) string { return theme.Load(dir) }

func themeNames() []string { return theme.Names() }

func isThemeName(name string) bool { return theme.IsName(name) }

func parseThemeColor(raw json.RawMessage) (lipgloss.Color, bool) {
	color, ok := theme.ParseColor(raw)
	return lipgloss.Color(color), ok
}

// applyTheme activates a theme by name: the default palette with the
// theme's overrides on top. Unknown names fall back to the default.
func applyTheme(name string) {
	palette := theme.Resolve(name)
	colorAccent = lipgloss.Color(palette["accent"])
	colorAlt = lipgloss.Color(palette["alt"])
	colorMuted = lipgloss.Color(palette["muted"])
	colorFaint = lipgloss.Color(palette["faint"])
	colorGood = lipgloss.Color(palette["good"])
	colorBusy = lipgloss.Color(palette["busy"])
	colorBad = lipgloss.Color(palette["bad"])
	colorBarBg = lipgloss.Color(palette["bar_bg"])
	colorBarFg = lipgloss.Color(palette["bar_fg"])
	colorInk = lipgloss.Color(palette["ink"])
	rebuildStyles()
}
