package agent

import (
	"slices"
	"testing"
)

func TestLaunchArgs(t *testing.T) {
	codex := *ByKind("codex")
	if got := codex.LaunchArgs(nil, true); !slices.Equal(got, []string{"resume", "--last"}) {
		t.Fatalf("codex resume = %v", got)
	}
	zot := *ByKind("zot")
	got := zot.LaunchArgs([]string{"--model", "m"}, true)
	if !slices.Equal(got, []string{"--model", "m", "--continue"}) {
		t.Fatalf("zot resume = %v", got)
	}
	if got := zot.LaunchArgs([]string{"--continue"}, true); len(got) != 1 {
		t.Fatalf("duplicate resume flag: %v", got)
	}
	if got := zot.LaunchArgs(nil, false); len(got) != 0 {
		t.Fatalf("fresh launch = %v", got)
	}
}

func TestTitleStateAndBusy(t *testing.T) {
	claude := *ByKind("claude")
	cases := []struct {
		signals Signals
		want    string
	}{
		{Signals{ProgramStatus: "working"}, "working"},
		{Signals{ProgramStatus: "blocked"}, "attention"},
		{Signals{ProgramStatus: "done", Title: "◐ x"}, "idle"},
		{Signals{Title: "◐ thinking"}, "working"},
		{Signals{Title: "✳ ready"}, "idle"},
		{Signals{Title: "plain"}, ""},
	}
	for _, c := range cases {
		if got := claude.TitleState(c.signals); got != c.want {
			t.Errorf("%+v = %q, want %q", c.signals, got, c.want)
		}
	}
	spinner := func() bool { return true }
	never := func(string) bool { return false }
	if !claude.Busy("", never, spinner) || claude.Busy("idle", never, spinner) {
		t.Fatal("spinner fallback wrong")
	}
	custom := Spec{BusyMatch: "esc to interrupt"}
	if custom.Busy("", never, spinner) {
		t.Fatal("custom busy match must replace the spinner")
	}
}

func TestBinaryAndForeground(t *testing.T) {
	overrides := map[string]string{"zot": "/opt/bin/zot-dev"}
	if BinaryFor("zot", overrides) != "/opt/bin/zot-dev" || BinaryFor("pi", overrides) != "pi" {
		t.Fatal("binary override wrong")
	}
	if ForegroundKind("zot-dev", overrides) != "zot" || ForegroundKind("bash", overrides) != "" {
		t.Fatal("foreground mapping wrong")
	}
}
