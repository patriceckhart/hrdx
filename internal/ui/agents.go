package ui

import (
	"os/exec"
	"path/filepath"
	"strings"
)

// agentSpec describes one supported coding agent CLI, built in or
// registered as a custom harness from harness.json.
type agentSpec struct {
	kind           string
	binary         string   // default binary name on $PATH
	args           []string // extra launch args (custom harnesses)
	resume         []string // args that resume the latest session
	resumeFirst    bool     // resume args are a subcommand and must come first
	busyMatch      string   // screen substring visible while working; "" = spinner
	idleTitle      string   // terminal-title substring that overrides stale busy UI
	attentionTitle string   // title substring indicating the agent is waiting on the user
	custom         bool     // registered from harness.json
}

// agentSpecs lists the supported agents in menu/cycle order. Custom harnesses
// append after the built-ins.
var agentSpecs = []agentSpec{
	{kind: "codex", binary: "codex", resume: []string{"resume", "--last"}, resumeFirst: true},
	{kind: "claude", binary: "claude", resume: []string{"--continue"}},
	{kind: "copilot", binary: "copilot", resume: []string{"--continue"}},
	{kind: "pi", binary: "pi", resume: []string{"--continue"}},
	{kind: "zot", binary: "zot", resume: []string{"--continue"}},
}

func agentByKind(kind string) *agentSpec {
	for index := range agentSpecs {
		if agentSpecs[index].kind == kind {
			return &agentSpecs[index]
		}
	}
	return nil
}

func isAgentKind(kind string) bool {
	return agentByKind(kind) != nil
}

// binaryFor resolves the launch binary for an agent kind, honoring
// per-agent overrides from the CLI flags.
func (c Config) binaryFor(kind string) string {
	if override, ok := c.AgentBins[kind]; ok && override != "" {
		return override
	}
	if spec := agentByKind(kind); spec != nil {
		return spec.binary
	}
	return kind
}

// paneAgentKind returns the agent kind a pane is effectively running: the
// pane's own kind for agent panes, or the agent matching the PTY's
// foreground process for shell panes. "" when the pane runs no agent.
func (m Model) paneAgentKind(currentPane *pane) string {
	if isAgentKind(currentPane.kind) {
		return currentPane.kind
	}
	if currentPane.term == nil || !currentPane.running {
		return ""
	}
	foreground := currentPane.term.ForegroundCommand()
	if foreground == "" {
		return ""
	}
	for _, spec := range agentSpecs {
		if foreground == spec.binary || foreground == filepath.Base(m.config.binaryFor(spec.kind)) {
			return spec.kind
		}
	}
	return ""
}

// paneAgentTitleState interprets optional machine-readable title markers from
// custom harnesses. Attention and idle titles override stale on-screen
// spinners; an empty result leaves normal screen-based detection in charge.
func (m Model) paneAgentTitleState(currentPane *pane) string {
	if currentPane == nil || currentPane.term == nil || !currentPane.running {
		return ""
	}
	kind := m.paneAgentKind(currentPane)
	spec := agentByKind(kind)
	if spec == nil {
		return ""
	}
	title := currentPane.term.Title()
	if spec.attentionTitle != "" && strings.Contains(title, spec.attentionTitle) {
		return "attention"
	}
	if spec.idleTitle != "" && strings.Contains(title, spec.idleTitle) {
		return "idle"
	}
	return ""
}

// paneDisplayName returns the name a pane should show in lists: its own
// name, or "<agent> (<name>)" when a shell pane currently runs an agent
// in the foreground.
func (m Model) paneDisplayName(currentPane *pane) string {
	if isAgentKind(currentPane.kind) {
		return currentPane.name
	}
	if kind := m.paneAgentKind(currentPane); kind != "" {
		return kind + " (" + currentPane.name + ")"
	}
	return currentPane.name
}

// installedAgents returns the agent kinds whose binary is on $PATH (or
// overridden), regardless of the enabled/disabled setting.
func (m Model) installedAgents() []string {
	var found []string
	for _, spec := range agentSpecs {
		if _, err := exec.LookPath(m.config.binaryFor(spec.kind)); err == nil {
			found = append(found, spec.kind)
		}
	}
	return found
}

// availableAgents returns the installed agent kinds that are not disabled
// in settings. When no binary is found at all, it falls back to every
// enabled kind so pickers still offer something to launch.
func (m Model) availableAgents() []string {
	var found []string
	for _, kind := range m.installedAgents() {
		if !m.disabled[kind] {
			found = append(found, kind)
		}
	}
	if len(found) == 0 {
		for _, spec := range agentSpecs {
			if !m.disabled[spec.kind] {
				found = append(found, spec.kind)
			}
		}
	}
	if len(found) == 0 {
		found = []string{m.config.DefaultAgent}
	}
	return found
}

// cycleAgent advances the default agent to the next installed one.
func (m *Model) cycleAgent() {
	available := m.availableAgents()
	for index, kind := range available {
		if kind == m.config.DefaultAgent {
			m.config.DefaultAgent = available[(index+1)%len(available)]
			return
		}
	}
	m.config.DefaultAgent = available[0]
}
