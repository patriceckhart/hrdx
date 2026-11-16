package ui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/patriceckhart/hrdx/internal/theme"
)

// resetThemes restores the default palette and clears loaded themes.
func resetThemes() {
	theme.Reset()
	applyTheme(defaultThemeName)
}

func writeTheme(t *testing.T, dir, file, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "themes"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "themes", file), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadThemesAndApply(t *testing.T) {
	defer resetThemes()
	dir := t.TempDir()
	writeTheme(t, dir, "neon.json", `{
		"name": "neon",
		"colors": { "accent": 201, "bar_bg": "#101010" }
	}`)

	if problem := loadThemes(dir); problem != "" {
		t.Fatalf("loadThemes = %q", problem)
	}
	names := themeNames()
	if len(names) != len(builtInThemePresets)+2 || names[0] != defaultThemeName || names[len(names)-1] != "neon" {
		t.Fatalf("themes = %v", names)
	}

	applyTheme("neon")
	if colorAccent != lipgloss.Color("201") {
		t.Fatalf("accent = %q, want 201", colorAccent)
	}
	if colorBarBg != lipgloss.Color("#101010") {
		t.Fatalf("bar_bg = %q, want #101010", colorBarBg)
	}
	// Unset values keep the default.
	if colorGood != lipgloss.Color("78") {
		t.Fatalf("good = %q, want default 78", colorGood)
	}

	applyTheme(defaultThemeName)
	if colorAccent != lipgloss.Color("81") {
		t.Fatalf("accent after reset = %q, want 81", colorAccent)
	}
}

func TestLoadThemesValidation(t *testing.T) {
	defer resetThemes()
	dir := t.TempDir()
	writeTheme(t, dir, "broken.json", `{not json`)
	writeTheme(t, dir, "reserved.json", `{"name": "default"}`)
	writeTheme(t, dir, "bundled.json", `{"name": "dracula"}`)
	writeTheme(t, dir, "unnamed.json", `{"colors": {"accent": 100}}`)

	problem := loadThemes(dir)
	if problem == "" {
		t.Fatal("broken and reserved themes should be reported")
	}
	// The unnamed one falls back to its file name.
	if !isThemeName("unnamed") {
		t.Fatalf("themes = %v, want unnamed registered", themeNames())
	}
}

func TestThemesMissingDirIsFine(t *testing.T) {
	defer resetThemes()
	if problem := loadThemes(t.TempDir()); problem != "" {
		t.Fatalf("missing themes dir reported %q", problem)
	}
	if len(themeNames()) != len(builtInThemePresets)+1 {
		t.Fatalf("themes = %v, want default and bundled presets", themeNames())
	}
}

func TestApplyBuiltInTheme(t *testing.T) {
	defer resetThemes()
	applyTheme("dracula")
	if colorAccent != lipgloss.Color("#bd93f9") || colorGood != lipgloss.Color("#50fa7b") ||
		colorBarBg != lipgloss.Color("#343746") || colorInk != lipgloss.Color("#282a36") {
		t.Fatalf("dracula palette = accent %q good %q bar_bg %q ink %q", colorAccent, colorGood, colorBarBg, colorInk)
	}
}

func TestApplyUnknownThemeFallsBack(t *testing.T) {
	defer resetThemes()
	applyTheme("nope")
	if colorAccent != lipgloss.Color("81") {
		t.Fatalf("accent = %q, want default", colorAccent)
	}
}

func TestParseThemeColor(t *testing.T) {
	if color, ok := parseThemeColor([]byte("204")); !ok || color != lipgloss.Color("204") {
		t.Fatalf("number = %q/%v", color, ok)
	}
	if color, ok := parseThemeColor([]byte(`"#ff00aa"`)); !ok || color != lipgloss.Color("#ff00aa") {
		t.Fatalf("hex = %q/%v", color, ok)
	}
	if _, ok := parseThemeColor([]byte("999")); ok {
		t.Fatal("out-of-range number accepted")
	}
	if _, ok := parseThemeColor(nil); ok {
		t.Fatal("absent value accepted")
	}
	if _, ok := parseThemeColor([]byte(`""`)); ok {
		t.Fatal("empty string accepted")
	}
}
