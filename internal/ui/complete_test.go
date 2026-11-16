package ui

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAdvanceCompletionCycles(t *testing.T) {
	base := t.TempDir()
	for _, name := range []string{"apple", "apricot"} {
		if err := os.Mkdir(filepath.Join(base, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	model := newTestModel()
	model.input.SetValue(base + "/ap")

	// First tab: fills common prefix and records candidates.
	model.advanceCompletion(1)
	if len(model.completions) != 2 {
		t.Fatalf("completions = %v", model.completions)
	}
	// Next tabs cycle through the candidates.
	model.advanceCompletion(1)
	if model.input.Value() != base+"/apple" {
		t.Fatalf("value = %q, want apple", model.input.Value())
	}
	model.advanceCompletion(1)
	if model.input.Value() != base+"/apricot" {
		t.Fatalf("value = %q, want apricot", model.input.Value())
	}
	model.advanceCompletion(1)
	if model.input.Value() != base+"/apple" {
		t.Fatalf("value = %q, want wrap to apple", model.input.Value())
	}
}
