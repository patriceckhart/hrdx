package sound

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// resetSounds clears custom sound registrations and the extraction
// cache after a test.
func resetSounds() {
	registry.Lock()
	registry.custom = nil
	registry.dir = ""
	registry.paths = nil
	registry.Unlock()
}

func TestLoadSoundsRegistersCustomKinds(t *testing.T) {
	defer resetSounds()
	dir := t.TempDir()
	wav := filepath.Join(dir, "moo.wav")
	if err := os.WriteFile(wav, []byte("RIFF"), 0o644); err != nil {
		t.Fatal(err)
	}
	fixture, err := json.Marshal([]map[string]string{{"name": "moo", "file": wav}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, File), fixture, 0o644); err != nil {
		t.Fatal(err)
	}

	if problem := Load(dir); problem != "" {
		t.Fatalf("loadSounds = %q", problem)
	}
	if !IsKind("moo") {
		t.Fatal("custom sound should be selectable")
	}
	if got := Path("moo"); got != wav {
		t.Fatalf("soundFile = %q, want %q", got, wav)
	}
	kinds := Kinds()
	if kinds[0] != "ding" || kinds[len(kinds)-1] != "moo" {
		t.Fatalf("kinds = %v, want built-ins first, moo last", kinds)
	}
}

func TestLoadSoundsRejectsInvalidEntries(t *testing.T) {
	defer resetSounds()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, File), []byte(`[
		{"name": "ding", "file": "/tmp/x.wav"},
		{"name": "", "file": "/tmp/x.wav"},
		{"name": "ghost", "file": "/definitely/not/here.wav"},
		{"name": "nofile"}
	]`), 0o644); err != nil {
		t.Fatal(err)
	}

	problem := Load(dir)
	if problem == "" {
		t.Fatal("invalid entries should be reported")
	}
	if len(Kinds()) != len(Builtins) {
		t.Fatalf("kinds = %v, want built-ins only", Kinds())
	}
}

func TestLoadSoundsMissingFileIsFine(t *testing.T) {
	defer resetSounds()
	if problem := Load(t.TempDir()); problem != "" {
		t.Fatalf("missing sounds.json reported %q", problem)
	}
}

func TestSoundFileExtractsEmbedded(t *testing.T) {
	resetSounds() // drop cached extractions from other tests
	defer resetSounds()
	dir := t.TempDir()
	if problem := Load(dir); problem != "" {
		t.Fatal(problem)
	}
	path := Path("ding")
	if path == "" {
		t.Fatal("embedded ding not materialized")
	}
	info, err := os.Stat(path)
	if err != nil || info.Size() == 0 {
		t.Fatalf("materialized file bad: %v", err)
	}
	if filepath.Dir(path) != dir {
		t.Fatalf("extracted to %q, want state dir %q", filepath.Dir(path), dir)
	}
}
