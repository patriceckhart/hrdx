package core

import (
	"strings"
	"time"

	"github.com/patriceckhart/hrdx/internal/agent"
	"github.com/patriceckhart/hrdx/internal/api"
	"github.com/patriceckhart/hrdx/internal/gitinfo"
)

func gitGroupPath(cwd string) []string { return gitinfo.GroupPath(cwd) }

// startedMsg reports the outcome of an asynchronous pane start.
type startResult struct {
	term Terminal
	err  error
}

// startAll launches every restored pane.
func (m *model) startAll() {
	for _, current := range m.spaces {
		for _, currentTab := range current.tabs {
			for _, target := range currentTab.panes {
				m.startPane(current, target)
			}
		}
	}
}

// launchSpec resolves the command for a pane.
func (m *model) launchSpec(owner *space, target *pane) LaunchSpec {
	spec := LaunchSpec{Command: m.config.Shell, Args: m.config.ShellArgs, CWD: owner.cwd,
		Cols: 80, Rows: 24, Reattach: target.session}
	if target.cwd != "" {
		spec.CWD = target.cwd
	}
	if target.cols >= 2 && target.rows >= 2 {
		spec.Cols, spec.Rows = target.cols, target.rows
	}
	if found := agent.ByKind(target.kind); found != nil {
		spec.Command = agent.BinaryFor(target.kind, m.config.AgentBins)
		var extra []string
		if target.kind == "zot" {
			extra = append(extra, m.config.ZotArgs...)
		}
		// Arguments from workspace creation follow the command-line ones.
		extra = append(extra, target.args...)
		spec.Args = found.LaunchArgs(extra, target.resume)
	}
	return spec
}

// startPane launches target off the loop and reports back. Durable
// placeholders and panes already starting are skipped.
func (m *model) startPane(owner *space, target *pane) {
	if target.durable != nil || target.starting || m.launcher == nil {
		return
	}
	target.starting = true
	target.failure = ""
	spec := m.launchSpec(owner, target)
	id := target.id
	output := m.hooks.Output
	tee := func(data []byte) {
		if output != nil {
			output(id, data)
		}
	}
	launcher := m.launcher
	go func() {
		started, err := launcher.Launch(spec, tee)
		m.post(func() { m.paneStarted(id, startResult{started, err}) })
	}()
}

// paneStarted wires a started process into its pane, or discards it when
// the pane was closed meanwhile.
func (m *model) paneStarted(id int, result startResult) {
	target, _, _ := m.paneByID(id)
	if target == nil || m.quitting {
		if result.term != nil {
			result.term.Close()
		}
		return
	}
	target.starting = false
	if result.err != nil {
		target.failure = result.err.Error()
		target.running = false
		return
	}
	target.term = result.term
	target.running = result.term.Running()
	if target.floating == nil {
		target.session = result.term.HolderSession()
	} else if session := result.term.HolderSession(); session != 0 {
		// Floating panes are ephemeral; remember the session only to kill it.
		target.session = session
	}
	target.resume = false
	m.persist()
	m.watchExit(id, result.term)
}

// watchExit observes the pane's update channel, which closes when the
// process exits.
func (m *model) watchExit(id int, terminal Terminal) {
	updates := terminal.Updates()
	go func() {
		for range updates {
		}
		m.post(func() { m.paneExited(id, terminal) })
	}()
}

func (m *model) paneExited(id int, terminal Terminal) {
	target, _, _ := m.paneByID(id)
	if target == nil || target.term != terminal {
		return
	}
	target.running = false
	delete(m.wasBusy, id)
}

// holderExited marks every pane bound to session as exited.
func (m *model) holderExited(session int64) {
	for _, current := range m.spaces {
		for _, currentTab := range current.tabs {
			for _, target := range currentTab.panes {
				if target.session == session && target.term != nil {
					if exiter, ok := target.term.(interface{ MarkExited() }); ok {
						exiter.MarkExited()
					}
					target.running = false
				}
			}
		}
	}
}

// gcHolderSessions kills holder sessions no persistent pane references,
// left over from --fresh starts or corrupted state.
func (m *model) gcHolderSessions() {
	if m.launcher == nil || !m.launcher.Persistent() {
		return
	}
	referenced := map[int64]bool{}
	for _, current := range m.spaces {
		for _, currentTab := range current.tabs {
			for _, target := range currentTab.panes {
				if target.session != 0 {
					referenced[target.session] = true
				}
			}
		}
	}
	for _, session := range m.launcher.Sessions() {
		if !referenced[session] {
			m.launcher.Kill(session)
		}
	}
}

// shutdown persists state, detaches persistent panes, and kills ephemeral
// floating panes so they never leak detached holder sessions.
func (m *model) shutdown() {
	if m.quitting {
		return
	}
	if m.saveTimer != nil {
		m.saveTimer.Stop()
		m.saveTimer = nil
	}
	m.saveDue = true
	m.flush()
	m.savePrefs()
	m.quitting = true
	persistent := m.launcher != nil && m.launcher.Persistent()
	for _, current := range m.spaces {
		for _, currentTab := range current.tabs {
			for _, target := range currentTab.panes {
				if target.term == nil {
					continue
				}
				if target.floating == nil && persistent && target.term.HolderSession() != 0 {
					target.term.Detach()
					continue
				}
				target.term.Close()
			}
		}
	}
	if m.launcher != nil {
		m.launcher.Close()
	}
}

var agentNames = map[string]string{"claude": "Claude Code", "codex": "Codex", "zot": "zot", "pi": "pi"}

// paneLabel names a pane for notifications: its custom name, or the agent
// it runs when the name is the generated default.
func (m *model) paneLabel(target *pane) string {
	kind := m.paneAgentKind(target)
	if kind == "" {
		return target.name
	}
	if strings.HasPrefix(target.name, target.kind+" ") {
		if name, ok := agentNames[kind]; ok {
			return name
		}
		return kind
	}
	return target.name
}

// updateBadge reports the number of panes waiting for the user.
func (m *model) updateBadge() {
	count := 0
	for id := range m.paneAttention {
		if target, _, _ := m.paneByID(id); target != nil {
			count++
		}
	}
	if count == m.badge {
		return
	}
	m.badge = count
	if m.hooks.Badge != nil {
		m.hooks.Badge(count)
	}
}

// paneBusy reports whether an agent runs in target and is working.
func (m *model) paneBusy(target *pane) bool {
	busy, _ := m.paneState(target)
	return busy
}

// paneState returns the busy flag and the title state of a pane.
func (m *model) paneState(target *pane) (busy bool, titleState string) {
	if target == nil || target.term == nil || !target.running {
		return false, ""
	}
	kind := m.paneAgentKind(target)
	spec := agent.ByKind(kind)
	if spec == nil {
		return false, ""
	}
	status, _ := target.term.ProgramStatus()
	titleState = spec.TitleState(agent.Signals{ProgramStatus: status, Title: target.term.Title()})
	return spec.Busy(titleState, target.term.HasScreenText, target.term.HasSpinner), titleState
}

// paneAgentKind is the agent a pane runs: its own kind, or the agent in the
// foreground of a shell pane.
func (m *model) paneAgentKind(target *pane) string {
	if agent.IsAgent(target.kind) {
		return target.kind
	}
	if target.term == nil || !target.running {
		return ""
	}
	return agent.ForegroundKind(target.term.ForegroundCommand(), m.config.AgentBins)
}

// pollBusy tracks busy transitions for every pane, publishing events and
// scheduling a debounced finish confirmation, as the TUI does.
func (m *model) pollBusy() {
	changed := false
	for _, current := range m.spaces {
		for _, currentTab := range current.tabs {
			for _, target := range currentTab.panes {
				if m.trackBusy(current, target) {
					changed = true
				}
			}
		}
	}
	if changed {
		m.dirty = true
	}
}

func (m *model) trackBusy(owner *space, target *pane) bool {
	if target.durable != nil {
		return false
	}
	busy := m.paneBusy(target)
	if busy {
		if m.wasBusy[target.id] {
			return false
		}
		m.wasBusy[target.id] = true
		m.finishSeq[target.id]++
		m.publish(api.Event{Event: api.EventPaneBusyChanged,
			Data: api.PaneEvent{Pane: target.id, Name: target.name, Kind: target.kind, Busy: true}})
		return true
	}
	if !m.wasBusy[target.id] {
		return false
	}
	delete(m.wasBusy, target.id)
	m.publish(api.Event{Event: api.EventPaneBusyChanged,
		Data: api.PaneEvent{Pane: target.id, Name: target.name, Kind: target.kind, Busy: false}})
	m.finishSeq[target.id]++
	id, seq := target.id, m.finishSeq[target.id]
	time.AfterFunc(m.config.FinishDelay, func() {
		m.post(func() { m.confirmFinish(id, seq) })
	})
	return true
}

// confirmFinish fires after the debounce. A pane that turned busy again was
// only redrawing; otherwise the turn finished.
func (m *model) confirmFinish(id int, seq uint64) {
	if m.finishSeq[id] != seq {
		return
	}
	target, owner, _ := m.paneByID(id)
	if target == nil {
		return
	}
	if m.paneBusy(target) {
		m.wasBusy[id] = true
		return
	}
	unseen := false
	if focused := m.currentPane(); !m.windowFocused || focused == nil || focused.id != id {
		m.paneAttention[id] = true
		unseen = true
	}
	if m.hooks.Notify != nil {
		kind := "finished"
		if _, titleState := m.paneState(target); titleState == "attention" {
			kind = "attention"
		}
		m.hooks.Notify(Notification{Kind: kind, PaneID: id, Workspace: owner.name,
			Pane: m.paneLabel(target), Unseen: unseen, Banner: m.notifyOn})
	}
	m.updateBadge()
	if m.soundOn && m.hooks.PlaySound != nil {
		m.hooks.PlaySound(m.soundKind)
	}
	if m.notifyOn && !m.windowFocused && m.hooks.Attention != nil {
		m.hooks.Attention()
	}
}
