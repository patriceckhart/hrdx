package ui

import (
	"os/exec"

	"github.com/patriceckhart/hrdx/internal/agent"
)

// agentSpec is the shared agent registry entry.
type agentSpec = agent.Spec

func agentByKind(kind string) *agentSpec { return agent.ByKind(kind) }

func isAgentKind(kind string) bool { return agent.IsAgent(kind) }

// binaryFor resolves the launch binary for an agent kind, honoring
// per-agent overrides from the CLI flags.
func (c Config) binaryFor(kind string) string { return agent.BinaryFor(kind, c.AgentBins) }

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
	return agent.ForegroundKind(currentPane.term.ForegroundCommand(), m.config.AgentBins)
}

// paneAgentTitleState interprets machine-readable agent state: an OSC 7501
// program status report first, then optional title markers from custom
// harnesses. The result is "working", "attention", "idle", or empty. A
// reported state overrides on-screen spinners in both directions; an empty
// result leaves normal screen-based detection in charge.
func (m Model) paneAgentTitleState(currentPane *pane) string {
	if currentPane == nil || currentPane.term == nil || !currentPane.running {
		return ""
	}
	kind := m.paneAgentKind(currentPane)
	spec := agentByKind(kind)
	if spec == nil {
		return ""
	}
	status, _ := currentPane.term.ProgramStatus()
	return spec.TitleState(agent.Signals{ProgramStatus: status, Title: currentPane.term.Title()})
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
	for _, spec := range agent.Specs {
		if _, err := exec.LookPath(m.config.binaryFor(spec.Kind)); err == nil {
			found = append(found, spec.Kind)
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
		for _, spec := range agent.Specs {
			if !m.disabled[spec.Kind] {
				found = append(found, spec.Kind)
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
