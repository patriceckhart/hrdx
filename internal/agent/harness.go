package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Custom harnesses extend the built-in agent list with user-defined coding
// agent CLIs. They are declared in harness.json next to the state file and
// behave like built-ins everywhere: pickers, cycling, sidebar pane identity,
// settings toggles, resume-on-restore, and busy tracking.

const HarnessFile = "harness.json"

// HarnessSpec is the JSON shape of one custom harness entry.
type HarnessSpec struct {
	// Kind is the identifier used in pickers, pane names, and the state
	// file. Required, must not collide with built-ins or "shell".
	Kind string `json:"kind"`
	// Binary is the executable to launch; defaults to Kind.
	Binary string `json:"binary,omitempty"`
	// Args are extra arguments passed on every launch.
	Args []string `json:"args,omitempty"`
	// Resume are the arguments that resume the latest session when a
	// restored pane relaunches.
	Resume []string `json:"resume,omitempty"`
	// ResumeFirst puts the resume args before Args (for subcommands
	// like "resume --last").
	ResumeFirst bool `json:"resume_first,omitempty"`
	// Busy is a substring of the harness's screen output that is only
	// visible while it is working (e.g. "esc to interrupt"). When empty
	// the braille spinner detection used for the built-ins applies.
	Busy string `json:"busy,omitempty"`
	// IdleTitle and AttentionTitle are terminal-title substrings emitted when
	// work is done or blocked on user input. Either overrides a stale spinner.
	IdleTitle      string `json:"idle_title,omitempty"`
	AttentionTitle string `json:"attention_title,omitempty"`
}

// LoadHarnesses reads harness.json from dir and registers every valid
// entry. A missing file is fine. Returns a description of what was
// skipped or failed, or "" when everything loaded.
func LoadHarnesses(dir string) string {
	if dir == "" {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(dir, HarnessFile))
	if err != nil {
		if os.IsNotExist(err) {
			return ""
		}
		return "harnesses: " + err.Error()
	}
	var specs []HarnessSpec
	if err := json.Unmarshal(data, &specs); err != nil {
		return "harnesses: " + err.Error()
	}
	var problems []string
	for _, spec := range specs {
		if err := RegisterHarness(spec); err != nil {
			problems = append(problems, err.Error())
		}
	}
	if len(problems) > 0 {
		return "harnesses: " + strings.Join(problems, ", ")
	}
	return ""
}

// RegisterHarness validates and adds one custom harness to the agent
// list. Re-registering the same custom kind replaces the earlier entry.
func RegisterHarness(spec HarnessSpec) error {
	kind := strings.TrimSpace(spec.Kind)
	if kind == "" {
		return fmt.Errorf("entry without kind")
	}
	if kind == "shell" {
		return fmt.Errorf("%q is reserved", kind)
	}
	if existing := ByKind(kind); existing != nil && !existing.Custom {
		return fmt.Errorf("%q is built in", kind)
	}
	binary := strings.TrimSpace(spec.Binary)
	if binary == "" {
		binary = kind
	}
	// Title markers are per-harness and have no sensible default: an unset
	// marker simply leaves screen-based busy detection in charge.
	idleTitle := strings.TrimSpace(spec.IdleTitle)
	attentionTitle := strings.TrimSpace(spec.AttentionTitle)
	entry := Spec{
		Kind:           kind,
		Binary:         binary,
		Args:           append([]string{}, spec.Args...),
		Resume:         append([]string{}, spec.Resume...),
		ResumeFirst:    spec.ResumeFirst,
		BusyMatch:      spec.Busy,
		IdleTitle:      idleTitle,
		AttentionTitle: attentionTitle,
		Custom:         true,
	}
	for index := range Specs {
		if Specs[index].Kind == kind {
			Specs[index] = entry
			return nil
		}
	}
	Specs = append(Specs, entry)
	return nil
}
