package ui

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/patriceckhart/hrdx/internal/api/apitest"
	"github.com/patriceckhart/hrdx/internal/state"
)

// TestRecordAPIParityFixture writes the TUI's answers for the shared parity
// script when HRDX_RECORD_PARITY names an output file. hrdx Desktop compares
// its own answers against the recorded file:
//
//	HRDX_RECORD_PARITY=$PWD/desktop/internal/core/testdata/api_parity.json \
//	  go test ./internal/ui -run TestRecordAPIParityFixture
func TestRecordAPIParityFixture(t *testing.T) {
	out := os.Getenv("HRDX_RECORD_PARITY")
	if out == "" {
		t.Skip("set HRDX_RECORD_PARITY to record the API parity fixture")
	}
	dirA, dirB := t.TempDir(), t.TempDir()
	model := New(Config{Shell: "/bin/sh", Version: "test"}, []string{dirA}, "", state.State{})
	var answers []json.RawMessage
	for _, step := range apitest.Script(dirA, dirB) {
		encoded, err := apitest.Encode(step.Method, apiCall(t, &model, step.Method, step.Payload), dirA, dirB)
		if err != nil {
			t.Fatal(err)
		}
		answers = append(answers, encoded)
	}
	data, err := json.MarshalIndent(answers, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(out, append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}
