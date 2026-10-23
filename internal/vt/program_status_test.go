package vt

import (
	"bytes"
	"testing"
)

func programStatus(terminal Terminal) (string, string) {
	terminal.Lock()
	defer terminal.Unlock()
	return terminal.ProgramStatus()
}

func TestProgramStatusQueryIsAcknowledged(t *testing.T) {
	var response bytes.Buffer
	terminal := New(WithSize(8, 2), WithWriter(&response))
	if _, err := terminal.Write([]byte("\x1b]7501;?\x1b\\")); err != nil {
		t.Fatal(err)
	}
	if got, want := response.String(), "\x1b]7501;?\x1b\\"; got != want {
		t.Fatalf("query response = %q, want %q", got, want)
	}
	if state, app := programStatus(terminal); state != "" || app != "" {
		t.Fatalf("a query must not set a state, got %q/%q", state, app)
	}
}

func TestProgramStatusReportsFollowLatestState(t *testing.T) {
	terminal := New(WithSize(8, 2))
	steps := []struct {
		input, state, app string
	}{
		{"\x1b]7501;state=idle:app=zot\x1b\\", "idle", "zot"},
		{"\x1b]7501;state=working:app=zot:msg=bmFtZQ==\x07", "working", "zot"},
		{"\x1b]7501;state=blocked:app=zot:kind=permission\x1b\\", "blocked", "zot"},
		{"\x1b]7501;state=bogus:app=zot\x1b\\", "blocked", "zot"}, // unknown states are ignored
		{"\x1b]7501;state=done:app=zot\x1b\\", "done", "zot"},
		{"\x1b]7501;state=clear\x1b\\", "", ""},
	}
	for _, step := range steps {
		if _, err := terminal.Write([]byte(step.input)); err != nil {
			t.Fatal(err)
		}
		if state, app := programStatus(terminal); state != step.state || app != step.app {
			t.Fatalf("after %q: status = %q/%q, want %q/%q", step.input, state, app, step.state, step.app)
		}
	}
}

func TestProgramStatusSurvivesSplitWrites(t *testing.T) {
	terminal := New(WithSize(8, 2))
	report := []byte("\x1b]7501;state=working:app=zot\x1b\\")
	for index := range report {
		if _, err := terminal.Write(report[index : index+1]); err != nil {
			t.Fatal(err)
		}
	}
	if state, _ := programStatus(terminal); state != "working" {
		t.Fatalf("state after byte-wise feed = %q, want working", state)
	}
}
