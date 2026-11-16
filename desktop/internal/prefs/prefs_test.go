package prefs

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDefaultsAndCorruptFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	got := Load(path)
	if got.FontSize != DefaultFontSize || got.ScrollbackLines != DefaultScrollbackLines || !*got.CursorBlink {
		t.Fatalf("defaults = %+v", got)
	}
	if err := os.WriteFile(path, []byte("{broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	if Load(path).FontSize != DefaultFontSize {
		t.Fatal("corrupt file must yield defaults")
	}
}

func TestSaveRoundTrip(t *testing.T) {
	path := Path(filepath.Join(t.TempDir(), "state.json"))
	blink := false
	want := Prefs{FontFamily: "Menlo", FontSize: 15, LineHeight: 1.4, CursorBlink: &blink,
		ScrollbackLines: 5000, SidebarWidth: 300, Window: &Window{X: 1, Y: 2, Width: 800, Height: 600}}
	if err := Save(path, want); err != nil {
		t.Fatal(err)
	}
	got := Load(path)
	if got.FontFamily != "Menlo" || got.FontSize != 15 || *got.CursorBlink || got.Window.Width != 800 || got.SidebarWidth != 300 {
		t.Fatalf("round trip = %+v", got)
	}
	if Path("") != "" || Save("", want) != nil {
		t.Fatal("empty state path must disable prefs")
	}
}

func TestNormalizeRejectsTinyWindow(t *testing.T) {
	got := Prefs{Window: &Window{Width: 10, Height: 10}, FontSize: 500}.Normalized()
	if got.Window != nil || got.FontSize != DefaultFontSize {
		t.Fatalf("normalized = %+v", got)
	}
}
