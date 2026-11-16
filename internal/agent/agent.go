// Package agent owns the coding agent registry shared by every hrdx
// frontend: built-in agent CLIs, custom harnesses from harness.json, launch
// argument assembly, and the busy, idle, and attention classification.
package agent

import (
	"path/filepath"
	"slices"
	"strings"
)

// Spec describes one supported coding agent CLI, built in or registered as
// a custom harness from harness.json.
type Spec struct {
	Kind           string
	Binary         string   // default binary name on $PATH
	Args           []string // extra launch args (custom harnesses)
	Resume         []string // args that resume the latest session
	ResumeFirst    bool     // resume args are a subcommand and must come first
	BusyMatch      string   // screen substring visible while working; "" = spinner
	IdleTitle      string   // terminal-title substring that overrides stale busy UI
	AttentionTitle string   // title substring indicating the agent is waiting on the user
	// Built-in title prefixes for agents that cannot be made to send OSC 7501
	// reports through ConPTY; matched after reports, before the screen.
	BusyTitlePrefixes []string
	IdleTitlePrefixes []string
	Custom            bool // registered from harness.json
}

// Specs lists the supported agents in menu/cycle order (alphabetical;
// custom harnesses append after the built-ins). It is process-global and
// is only mutated during startup harness loading and in tests.
var Specs = []Spec{
	{Kind: "codex", Binary: "codex", Resume: []string{"resume", "--last"}, ResumeFirst: true},
	// Claude Code animates ◐/◑ in its title during a turn and shows ✳ when
	// idle. Its OSC 7501 support needs a probe reply ConPTY never delivers.
	{Kind: "claude", Binary: "claude", Resume: []string{"--continue"},
		BusyTitlePrefixes: []string{"◐ ", "◑ "}, IdleTitlePrefixes: []string{"✳ "}},
	{Kind: "pi", Binary: "pi", Resume: []string{"--continue"}},
	{Kind: "zot", Binary: "zot", Resume: []string{"--continue"}},
}

// ByKind returns the spec for kind, nil for shells and unknown kinds.
func ByKind(kind string) *Spec {
	for index := range Specs {
		if Specs[index].Kind == kind {
			return &Specs[index]
		}
	}
	return nil
}

// IsAgent reports whether kind names a registered agent.
func IsAgent(kind string) bool { return ByKind(kind) != nil }

// BinaryFor resolves the launch binary for an agent kind, honoring
// per-agent overrides such as the --zot-bin flag.
func BinaryFor(kind string, overrides map[string]string) string {
	if override, ok := overrides[kind]; ok && override != "" {
		return override
	}
	if spec := ByKind(kind); spec != nil {
		return spec.Binary
	}
	return kind
}

// ForegroundKind maps a PTY foreground process name to the agent it runs,
// "" when it is none of them.
func ForegroundKind(foreground string, overrides map[string]string) string {
	if foreground == "" {
		return ""
	}
	for _, spec := range Specs {
		if foreground == spec.Binary || foreground == filepath.Base(BinaryFor(spec.Kind, overrides)) {
			return spec.Kind
		}
	}
	return ""
}

// LaunchArgs assembles the arguments for an agent pane: the spec's own
// arguments, extra (for example zot's provider flags), and, when resume is
// set, the resume arguments unless extra already contains them.
func (s Spec) LaunchArgs(extra []string, resume bool) []string {
	args := append(append([]string{}, s.Args...), extra...)
	if resume && len(s.Resume) > 0 && !slices.Contains(args, s.Resume[0]) {
		if s.ResumeFirst {
			return append(append([]string{}, s.Resume...), args...)
		}
		args = append(args, s.Resume...)
	}
	return args
}

// Signals are the observations one agent pane state classification needs.
type Signals struct {
	ProgramStatus string // OSC 7501 state: working, blocked, idle, done, error
	Title         string
}

// TitleState interprets machine-readable agent state: an OSC 7501 program
// status report first, then title markers. The result is "working",
// "attention", "idle", or empty. A reported state overrides on-screen
// spinners in both directions; an empty result leaves screen-based
// detection in charge.
func (s Spec) TitleState(signals Signals) string {
	switch signals.ProgramStatus {
	case "working":
		return "working"
	case "blocked":
		return "attention"
	case "idle", "done", "error":
		return "idle"
	}
	title := signals.Title
	for _, prefix := range s.BusyTitlePrefixes {
		if strings.HasPrefix(title, prefix) {
			return "working"
		}
	}
	for _, prefix := range s.IdleTitlePrefixes {
		if strings.HasPrefix(title, prefix) {
			return "idle"
		}
	}
	if s.AttentionTitle != "" && strings.Contains(title, s.AttentionTitle) {
		return "attention"
	}
	if s.IdleTitle != "" && strings.Contains(title, s.IdleTitle) {
		return "idle"
	}
	return ""
}

// Busy decides whether an agent is working given its title state and screen
// probes. A non-empty title state is authoritative. Otherwise a custom busy
// substring or the braille spinner decides.
func (s Spec) Busy(titleState string, hasScreenText func(string) bool, hasSpinner func() bool) bool {
	if titleState == "working" {
		return true
	}
	if titleState != "" {
		return false
	}
	if s.BusyMatch != "" {
		return hasScreenText(s.BusyMatch)
	}
	return hasSpinner()
}
