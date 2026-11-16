package theme

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveLayersOverridesOnDefault(t *testing.T) {
	t.Cleanup(Reset)
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "themes"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "themes", "x.json"), []byte(`{"name":"x","colors":{"accent":201,"bad":"#ff0000"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if problem := Load(dir); problem != "" {
		t.Fatal(problem)
	}
	got := Resolve("x")
	if got["accent"] != "201" || got["bad"] != "#ff0000" || got["good"] != "78" {
		t.Fatalf("palette = %v", got)
	}
	if Resolve("missing")["accent"] != "81" {
		t.Fatal("unknown theme must resolve to default")
	}
	if Resolve("dracula")["accent"] != "#bd93f9" {
		t.Fatal("preset not applied")
	}
	for _, role := range Roles {
		if got[role] == "" {
			t.Fatalf("role %s empty", role)
		}
	}
}
