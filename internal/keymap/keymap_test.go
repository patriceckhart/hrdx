package keymap

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOverridesReplaceDefaults(t *testing.T) {
	keys := BuildPrefixKeys(map[string]string{"find": "f", "prefix": "ctrl+a"})
	if keys["f"] != "find" || keys["/"] == "find" {
		t.Fatalf("find override not applied: %v", keys)
	}
	if keys["ctrl+a"] != "" {
		t.Fatal("prefix must not become a prefix action")
	}
}

func TestLoadReportsUnknownActions(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, File), []byte(`{"find":"f","bogus":"x","rename":" "}`), 0o644); err != nil {
		t.Fatal(err)
	}
	overrides, problem := Load(dir)
	if problem == "" || overrides["find"] != "f" || len(overrides) != 1 {
		t.Fatalf("overrides = %v, problem = %q", overrides, problem)
	}
	if overrides, problem := Load(t.TempDir()); overrides != nil || problem != "" {
		t.Fatal("missing file must be silent")
	}
}
