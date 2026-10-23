package ui

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/patriceckhart/hrdx/internal/continuous"
	"github.com/patriceckhart/hrdx/internal/state"
)

// The continuous panel is a menu of the selected workspace's durable
// conversations and hosts. It reuses the context-menu mode and its stale
// target protection: every entry carries stable IDs, never a focus index.

const durablePickLabel = "zot (durable)"

// durablePrompt is a pending native prompt submission.
type durablePrompt struct {
	profile, conv string
	policy        string // "" queue, "steer"
	base          string // workspace directory for @file attachments
}

// openContinuousPanel shows the continuous panel for the selected workspace.
func (m *Model) openContinuousPanel(at rect) tea.Cmd {
	owner := m.currentSpace()
	if owner == nil {
		return nil
	}
	items := m.continuousPanelItems(owner)
	m.mode = modeMenu
	m.menuPane, m.menuTab, m.menuSpace = nil, nil, nil
	m.pickItems = items
	m.pickAction = "continuous"
	m.pickSpace = owner
	m.openMenuBox(at)
	return m.pollDurable()
}

func (m Model) continuousPanelItems(owner *space) []menuItem {
	var items []menuItem
	if !continuous.LocalSupported {
		return []menuItem{{"durable agents are not supported on this platform yet", "noop"}}
	}
	if m.stateDir() == "" {
		return []menuItem{{"durable agents need a state directory (--state)", "noop"}}
	}
	items = append(items, menuItem{"New durable agent (split right)", "dur-new"})
	open := map[string]int{}
	for _, currentTab := range owner.tabs {
		for _, current := range currentTab.panes {
			if current.durable != nil {
				open[durableKey(current.durable.Profile, current.durable.Conversation)]++
			}
		}
	}
	for _, ref := range owner.durables {
		key := durableKey(ref.Profile, ref.Conversation)
		label := durableRefName(owner, ref)
		info := m.durState[key]
		stateName := "unknown"
		if host := m.hostState[ref.Profile]; host != nil && !host.connected {
			stateName = "offline"
		} else if info != nil && info.seen {
			stateName = info.exec
		}
		views := open[key]
		items = append(items, menuItem{fmt.Sprintf("%s  [%s, %d view(s)]", label, stateName, views), "noop"})
		target := ref.Profile + "|" + ref.Conversation
		items = append(items, menuItem{"  open view", "dur-open:" + target})
		items = append(items, menuItem{"  queue prompt", "dur-prompt:" + target})
		items = append(items, menuItem{"  steer prompt", "dur-steer:" + target})
		if info != nil && info.seen && len(info.snap.Approvals) > 0 {
			items = append(items, menuItem{fmt.Sprintf("  review %d approval(s)", len(info.snap.Approvals)), "dur-approvals:" + target})
		}
		if info != nil && info.seen && info.exec == continuous.StateRecovery {
			items = append(items, menuItem{"  review recovery", "dur-recovery:" + target})
		}
		if info != nil && info.seen {
			usage := info.snap.Usage.Usage
			items = append(items, menuItem{fmt.Sprintf("  usage: %d in, %d out, $%.3f, %d task(s)", usage.Input, usage.Output, usage.Cost, len(info.snap.Tasks)), "noop"})
		}
		items = append(items, menuItem{"  tasks and subagents", "dur-tasks:" + target})
		items = append(items, menuItem{"  budgets", "dur-budgets:" + target})
		items = append(items, menuItem{"  change model", "dur-model:" + target})
		items = append(items, menuItem{"  rename", "dur-rename:" + target})
		if info != nil && info.seen && (info.exec == continuous.StateRunning || info.exec == continuous.StateQueued || info.exec == continuous.StateApproval) {
			items = append(items, menuItem{"  abort work (explicit)", "dur-abort:" + target})
		}
		if views == 0 {
			items = append(items, menuItem{"  forget (keeps host history)", "dur-forget:" + target})
		}
	}
	profileIDs := map[string]bool{}
	for _, ref := range owner.durables {
		profileIDs[ref.Profile] = true
	}
	if owner.host != "" {
		profileIDs[owner.host] = true
	}
	ids := make([]string, 0, len(profileIDs))
	for id := range profileIDs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		profile, ok := m.profiles[id]
		status := "unknown"
		if host := m.hostState[id]; host != nil {
			status = "connected"
			if !host.connected {
				status = "offline"
			}
		}
		if !ok {
			items = append(items, menuItem{"host " + id + " [undefined profile]", "noop"})
			continue
		}
		kind := "external"
		if profile.Managed {
			kind = "managed"
		}
		items = append(items, menuItem{fmt.Sprintf("host %s [%s, %s]", id, kind, status), "noop"})
		if profile.Managed {
			items = append(items, menuItem{"  start host", "dur-host-start:" + id})
			items = append(items, menuItem{"  stop host (interrupts work)", "dur-host-stop:" + id})
		}
		items = append(items, menuItem{"  review recovery holds", "dur-host-recovery:" + id})
	}
	if len(m.profiles) > 0 {
		items = append(items, menuItem{"Use host for new durable agents...", "dur-choose-host"})
	}
	return items
}

func splitTarget(value string) (string, string, bool) {
	profile, conv, ok := strings.Cut(value, "|")
	return profile, conv, ok && profile != "" && conv != ""
}

// findDurable returns the workspace binding for a profile and conversation.
func findDurable(owner *space, profile, conv string) (state.DurableRef, bool) {
	for _, ref := range owner.durables {
		if ref.Profile == profile && ref.Conversation == conv {
			return ref, true
		}
	}
	return state.DurableRef{}, false
}

// runContinuousAction executes one continuous panel entry.
func (m Model) runContinuousAction(action string, owner *space, at rect) (tea.Model, tea.Cmd) {
	if owner == nil {
		return m, nil
	}
	name, value, _ := strings.Cut(action, ":")
	switch name {
	case "noop":
		return m, nil
	case "dur-new":
		return m.splitDurable(owner, nil, "right")
	case "dur-choose-host":
		m.openHostChooser(owner, at)
		return m, nil
	case "dur-host-use":
		owner.host = value
		m.persist()
		return m, m.flashInfo("new durable agents use host " + value)
	case "dur-host-default":
		owner.host = ""
		m.persist()
		return m, m.flashInfo("new durable agents use this project's managed host")
	case "dur-host-start":
		return m, m.startDurableHost(value)
	case "dur-host-stop":
		m.openConfirm(at, "Stop host "+value+"? Running work is interrupted and recoverable.", "dur-host-stop-yes:"+value, owner)
		return m, nil
	case "dur-host-stop-yes":
		return m, m.stopDurableHost(value)
	case "dur-host-recovery":
		profileID := value
		return m, m.runDurable(profileID, func(ctx context.Context, host continuous.Host) durableActionMsg {
			actions, err := host.RecoveryPreview(ctx)
			if err != nil {
				return durableActionMsg{err: err}
			}
			return durableActionMsg{open: &durableOpen{kind: "recovery", profile: profileID, recovery: actions}}
		})
	}
	profile, conv, ok := splitTarget(value)
	if !ok {
		return m, nil
	}
	ref, bound := findDurable(owner, profile, conv)
	switch name {
	case "dur-open":
		if !bound {
			return m, nil
		}
		return m.splitDurable(owner, &ref, "right")
	case "dur-prompt", "dur-steer":
		m.prompt = &durablePrompt{profile: profile, conv: conv, base: owner.cwd}
		if name == "dur-steer" {
			m.prompt.policy = "steer"
		}
		m.mode = modeDurablePrompt
		m.input.Placeholder = "prompt for " + durableRefName(owner, ref) + " (@file attaches)"
		m.input.SetValue("")
		m.input.Focus()
		return m, textinput.Blink
	case "dur-tasks":
		return m, m.runDurable(profile, func(ctx context.Context, host continuous.Host) durableActionMsg {
			tasks, err := host.Tasks(ctx, conv)
			if err != nil {
				return durableActionMsg{err: err}
			}
			list, _, err := host.List(ctx, "", 500)
			if err != nil {
				return durableActionMsg{err: err}
			}
			var children []continuous.Conversation
			for _, candidate := range list {
				if (candidate.Owner != nil && candidate.Owner.ConversationID == conv) || (candidate.Parent != nil && candidate.Parent.ID == conv) {
					children = append(children, candidate)
				}
			}
			return durableActionMsg{open: &durableOpen{kind: "tasks", profile: profile, conv: conv, tasks: tasks, children: children}}
		})
	case "dur-budgets":
		return m, m.runDurable(profile, func(ctx context.Context, host continuous.Host) durableActionMsg {
			budgets, _, err := host.Budgets(ctx, conv)
			if err != nil {
				return durableActionMsg{err: err}
			}
			return durableActionMsg{open: &durableOpen{kind: "budgets", profile: profile, conv: conv, budgets: budgets}}
		})
	case "dur-model":
		current := ""
		if info := m.durState[durableKey(profile, conv)]; info != nil && info.seen {
			config := info.snap.Conversation.Config
			current = strings.Trim(config.Provider+"/"+config.Model, "/")
		}
		m.prompt = &durablePrompt{profile: profile, conv: conv, policy: "model"}
		m.mode = modeDurablePrompt
		m.input.Placeholder = "provider/model, or model"
		m.input.SetValue(current)
		m.input.CursorEnd()
		m.input.Focus()
		return m, textinput.Blink
	case "dur-rename":
		m.prompt = &durablePrompt{profile: profile, conv: conv, policy: "rename"}
		m.mode = modeDurablePrompt
		m.input.Placeholder = "conversation title (empty clears)"
		m.input.SetValue(ref.Title)
		m.input.CursorEnd()
		m.input.Focus()
		return m, textinput.Blink
	case "dur-abort":
		m.openConfirm(at, "Abort work in "+durableRefName(owner, ref)+"? Queued prompts are withdrawn.", "dur-abort-yes:"+value, owner)
		return m, nil
	case "dur-abort-yes":
		return m, m.runDurable(profile, func(ctx context.Context, host continuous.Host) durableActionMsg {
			if err := host.Abort(ctx, conv); err != nil {
				return durableActionMsg{err: err}
			}
			return durableActionMsg{notice: "abort requested"}
		})
	case "dur-approvals":
		return m, m.runDurable(profile, func(ctx context.Context, host continuous.Host) durableActionMsg {
			approvals, err := host.Approvals(ctx, conv)
			if err != nil {
				return durableActionMsg{err: err}
			}
			return durableActionMsg{open: &durableOpen{kind: "approvals", profile: profile, conv: conv, approvals: approvals}}
		})
	case "dur-recovery":
		return m, m.runDurable(profile, func(ctx context.Context, host continuous.Host) durableActionMsg {
			actions, err := host.RecoveryPreview(ctx)
			if err != nil {
				return durableActionMsg{err: err}
			}
			var own []continuous.RecoveryAction
			for _, action := range actions {
				if action.ConversationID == conv {
					own = append(own, action)
				}
			}
			return durableActionMsg{open: &durableOpen{kind: "recovery", profile: profile, conv: conv, recovery: own}}
		})
	case "dur-forget":
		for _, currentTab := range owner.tabs {
			for _, current := range currentTab.panes {
				if current.durable != nil && current.durable.Profile == profile && current.durable.Conversation == conv {
					return m, m.flashStatus("close its views first")
				}
			}
		}
		kept := owner.durables[:0]
		for _, existing := range owner.durables {
			if existing.Profile != profile || existing.Conversation != conv {
				kept = append(kept, existing)
			}
		}
		owner.durables = kept
		delete(m.durState, durableKey(profile, conv))
		m.persist()
		return m, m.flashInfo("forgot conversation locally, host history kept")
	}
	return m, nil
}

// runDurableReview executes approval and recovery decisions. Decisions
// always name the exact approval or run ID shown to the user; the host
// rejects stale or already decided requests.
func (m Model) runDurableReview(action string) (tea.Model, tea.Cmd) {
	name, value, _ := strings.Cut(action, ":")
	parts := strings.SplitN(value, "|", 3)
	if len(parts) < 2 {
		return m, nil
	}
	profile, id := parts[0], parts[1]
	switch name {
	case "dur-allow", "dur-allow-tool", "dur-deny":
		allow := name != "dur-deny"
		scope := "call"
		if name == "dur-allow-tool" {
			scope = "tool"
		}
		return m, m.runDurable(profile, func(ctx context.Context, host continuous.Host) durableActionMsg {
			decided, err := host.Decide(ctx, id, allow, scope, "decided in hrdx")
			if err != nil {
				if continuous.Code(err) == "conflict" || continuous.Code(err) == "not_found" {
					return durableActionMsg{err: fmt.Errorf("approval is no longer pending, nothing was decided")}
				}
				return durableActionMsg{err: err}
			}
			return durableActionMsg{notice: "approval " + decided.State}
		})
	case "dur-unblock":
		return m, m.runDurable(profile, func(ctx context.Context, host continuous.Host) durableActionMsg {
			if err := host.Unblock(ctx, id); err != nil {
				return durableActionMsg{err: err}
			}
			return durableActionMsg{notice: "recovery hold released, the host applies its recovery plan"}
		})
	case "dur-task-abort":
		m.openReviewConfirm("Abort task "+shortID(id)+"?", "dur-task-abort-yes:"+value)
		return m, nil
	case "dur-task-abort-yes":
		return m, m.runDurable(profile, func(ctx context.Context, host continuous.Host) durableActionMsg {
			if _, err := host.AbortTask(ctx, id, false); err != nil {
				return durableActionMsg{err: err}
			}
			return durableActionMsg{notice: "task abort requested"}
		})
	case "dur-task-retry":
		return m, m.runDurable(profile, func(ctx context.Context, host continuous.Host) durableActionMsg {
			if _, err := host.RetryCleanup(ctx, id); err != nil {
				return durableActionMsg{err: err}
			}
			return durableActionMsg{notice: "task cleanup retried"}
		})
	case "dur-child-open":
		owner := m.currentSpace()
		if owner == nil {
			return m, nil
		}
		// Children have no workspace key; the view attaches by ID and the
		// conversation joins the workspace's associations once bound.
		return m.splitDurable(owner, &state.DurableRef{Profile: profile, Conversation: id}, "right")
	case "dur-child-prompt":
		m.prompt = &durablePrompt{profile: profile, conv: id}
		m.mode = modeDurablePrompt
		m.input.Placeholder = "prompt for " + shortID(id) + " (@file attaches)"
		m.input.SetValue("")
		m.input.Focus()
		return m, textinput.Blink
	case "dur-child-abort":
		m.openReviewConfirm("Abort work in "+shortID(id)+"? Queued prompts are withdrawn.", "dur-child-abort-yes:"+value)
		return m, nil
	case "dur-child-abort-yes":
		return m, m.runDurable(profile, func(ctx context.Context, host continuous.Host) durableActionMsg {
			if err := host.Abort(ctx, id); err != nil {
				return durableActionMsg{err: err}
			}
			return durableActionMsg{notice: "abort requested"}
		})
	case "dur-budget-open":
		m.prompt = &durablePrompt{profile: profile, conv: id, policy: "budget"}
		m.mode = modeDurablePrompt
		m.input.Placeholder = "conversation cost limit in USD (0 removes the cap)"
		m.input.SetValue("")
		m.input.Focus()
		return m, textinput.Blink
	}
	return m, nil
}

// openDurableReview shows fetched approvals or recovery actions as a menu.
func (m *Model) openDurableReview(open durableOpen) {
	var items []menuItem
	switch open.kind {
	case "approvals":
		if len(open.approvals) == 0 {
			items = append(items, menuItem{"no pending approvals", "noop"})
		}
		for _, approval := range open.approvals {
			target := open.profile + "|" + approval.ID
			summary := approval.Summary
			if summary == "" {
				summary = string(approval.Args)
			}
			items = append(items, menuItem{"tool " + sanitizeLine(approval.Tool, 40) + ": " + sanitizeLine(summary, 70), "noop"})
			if approval.ExpiresAt != nil {
				items = append(items, menuItem{"  expires " + approval.ExpiresAt.Local().Format(time.Kitchen), "noop"})
			}
			items = append(items, menuItem{"  allow this call", "dur-allow:" + target})
			items = append(items, menuItem{"  allow this tool in the conversation", "dur-allow-tool:" + target})
			items = append(items, menuItem{"  deny", "dur-deny:" + target})
		}
	case "tasks":
		if len(open.tasks) == 0 && len(open.children) == 0 {
			items = append(items, menuItem{"no tasks or subagents", "noop"})
		}
		for _, task := range open.tasks {
			kind := sanitizeLine(task.Kind, 24)
			if task.Background {
				kind += ", background"
			}
			items = append(items, menuItem{fmt.Sprintf("task %s: %s (%s)", shortID(task.ID), sanitizeLine(task.State, 16), kind), "noop"})
			target := open.profile + "|" + task.ID
			if task.State != "terminal" {
				items = append(items, menuItem{"  abort task", "dur-task-abort:" + target})
			} else {
				items = append(items, menuItem{"  retry cleanup", "dur-task-retry:" + target})
			}
		}
		for _, child := range open.children {
			relation := "fork"
			if child.Owner != nil {
				relation = "subagent"
			}
			title := child.Title
			if title == "" {
				title = shortID(child.ID)
			}
			items = append(items, menuItem{fmt.Sprintf("%s %s (%s/%s)", relation, sanitizeLine(title, 40),
				sanitizeLine(child.Config.Provider, 20), sanitizeLine(child.Config.Model, 30)), "noop"})
			items = append(items, menuItem{"  open view", "dur-child-open:" + open.profile + "|" + child.ID})
			items = append(items, menuItem{"  queue prompt", "dur-child-prompt:" + open.profile + "|" + child.ID})
			items = append(items, menuItem{"  abort work", "dur-child-abort:" + open.profile + "|" + child.ID})
		}
	case "budgets":
		if len(open.budgets) == 0 {
			items = append(items, menuItem{"no budgets apply", "noop"})
		}
		for _, status := range open.budgets {
			limit := "no cost cap"
			if status.Budget.LimitUSD > 0 {
				limit = fmt.Sprintf("$%.2f cap", status.Budget.LimitUSD)
			}
			line := fmt.Sprintf("%s: $%.3f spent, %s", status.Budget.Scope, status.SpentUSD, limit)
			if status.Exceeded != "" {
				line += " (exceeded)"
			}
			items = append(items, menuItem{line, "noop"})
		}
		items = append(items, menuItem{"set conversation cost limit...", "dur-budget-open:" + open.profile + "|" + open.conv})
	case "recovery":
		if len(open.recovery) == 0 {
			items = append(items, menuItem{"no interrupted runs", "noop"})
		}
		for _, action := range open.recovery {
			mode := "automatic"
			if !action.Automatic {
				mode = "needs review"
			}
			items = append(items, menuItem{fmt.Sprintf("run %s: %s (%s)", shortID(action.RunID), sanitizeLine(action.Action, 20), mode), "noop"})
			if action.Detail != "" {
				items = append(items, menuItem{"  " + sanitizeLine(action.Detail, 90), "noop"})
			}
			items = append(items, menuItem{"  release hold, apply the host recovery plan", "dur-unblock:" + open.profile + "|" + action.RunID})
		}
	}
	m.mode = modeMenu
	m.menuPane, m.menuTab, m.menuSpace = nil, nil, nil
	m.pickItems = items
	m.pickAction = "continuous-review"
	m.pickSpace = m.currentSpace()
	m.openMenuBox(rect{x: m.sidebarContentWidth() + 2, y: 1})
}

// openReviewConfirm confirms an action raised from a review menu.
func (m *Model) openReviewConfirm(question, yes string) {
	m.mode = modeMenu
	m.menuPane, m.menuTab, m.menuSpace = nil, nil, nil
	m.pickItems = []menuItem{{question, "noop"}, {"  yes", yes}, {"  no", "noop"}}
	m.pickAction = "continuous-review"
	m.pickSpace = m.currentSpace()
	m.openMenuBox(rect{x: m.sidebarContentWidth() + 2, y: 1})
	m.menuIndex = 2
}

// openConfirm asks for explicit confirmation of a destructive action.
func (m *Model) openConfirm(at rect, question, yes string, owner *space) {
	m.mode = modeMenu
	m.menuPane, m.menuTab, m.menuSpace = nil, nil, nil
	m.pickItems = []menuItem{{question, "noop"}, {"  yes", yes}, {"  no", "noop"}}
	m.pickAction = "continuous"
	m.pickSpace = owner
	m.openMenuBox(at)
	m.menuIndex = 2
}

func (m *Model) openHostChooser(owner *space, at rect) {
	items := []menuItem{{"this project's managed host", "dur-host-default"}}
	ids := make([]string, 0, len(m.profiles))
	for id := range m.profiles {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		label := id
		if name := m.profiles[id].Name; name != "" {
			label += " (" + sanitizeLine(name, 30) + ")"
		}
		if owner.host == id {
			label += " [current]"
		}
		items = append(items, menuItem{label, "dur-host-use:" + id})
	}
	m.mode = modeMenu
	m.menuPane, m.menuTab, m.menuSpace = nil, nil, nil
	m.pickItems = items
	m.pickAction = "continuous"
	m.pickSpace = owner
	m.openMenuBox(at)
}

// durableSettingsRows lists notification categories and the host used for
// new durable panes in the current workspace.
func (m Model) durableSettingsRows() []settingsRow {
	labels := map[string]string{"finished": "notify when durable work finishes", "failed": "notify on failures",
		"approval": "notify on approval requests", "recovery": "notify on recovery holds", "host": "notify when a host disconnects"}
	var rows []settingsRow
	for _, category := range durableCategories {
		rows = append(rows, settingsRow{settingsCheck(!m.durableMuted[category]) + labels[category], "durable-notify:" + category})
	}
	owner := m.currentSpace()
	if owner == nil {
		return rows
	}
	mark := func(on bool) string {
		if on {
			return "(•) "
		}
		return "( ) "
	}
	rows = append(rows, settingsRow{"host for new durable agents in " + sanitizeLine(owner.name, 20) + ":", "noop"})
	rows = append(rows, settingsRow{"  " + mark(owner.host == "") + "project managed host", "durable-host:"})
	ids := make([]string, 0, len(m.profiles))
	for id, profile := range m.profiles {
		if !profile.Managed {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	for _, id := range ids {
		rows = append(rows, settingsRow{"  " + mark(owner.host == id) + id, "durable-host:" + id})
	}
	return rows
}

// runDurablePick runs the pending picker action with a new durable agent,
// using the same targets as the ordinary kinds.
func (m Model) runDurablePick() (tea.Model, tea.Cmd) {
	action, target := m.pickAction, m.pickSpace
	m.closeMenu()
	if target != nil && !slices.Contains(m.spaces, target) {
		return m, nil
	}
	if target == nil {
		target = m.currentSpace()
	}
	if target == nil {
		return m, nil
	}
	split := map[string]string{"tab": "tab", "split-left": "left", "split-right": "right",
		"split-up": "up", "split-down": "down"}[action]
	if split == "" {
		return m, nil
	}
	return m.splitDurable(target, nil, split)
}

// splitDurable adds and starts a durable pane next to the focused one.
func (m Model) splitDurable(owner *space, ref *state.DurableRef, split string) (tea.Model, tea.Cmd) {
	for index, currentSpace := range m.spaces {
		if currentSpace == owner {
			m.selected = index
		}
	}
	newPane, err := m.addDurablePane(owner, ref, split)
	if err != nil {
		return m, m.flashStatus(err.Error())
	}
	m.resizePanes(owner)
	m.persist()
	return m, m.startPane(owner, newPane)
}

// updateDurablePromptKey handles the native prompt and rename input.
func (m Model) updateDurablePromptKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.prompt = nil
		m.mode = modeTerminal
		m.input.Blur()
		return m, nil
	case "enter":
		prompt := m.prompt
		value := m.input.Value()
		m.prompt = nil
		m.mode = modeTerminal
		m.input.Blur()
		m.input.SetValue("")
		if prompt == nil {
			return m, nil
		}
		if prompt.policy == "rename" {
			return m, m.renameDurable(prompt.profile, prompt.conv, strings.TrimSpace(value))
		}
		if prompt.policy == "model" {
			return m, m.configureDurable(prompt.profile, prompt.conv, strings.TrimSpace(value))
		}
		if prompt.policy == "budget" {
			return m, m.setDurableBudget(prompt.profile, prompt.conv, strings.TrimSpace(value))
		}
		base := prompt.base
		if base == "" {
			if current := m.currentSpace(); current != nil {
				base = current.cwd
			}
		}
		text, paths := splitPromptFiles(base, value)
		if text == "" && len(paths) == 0 {
			return m, nil
		}
		files, err := readAttachments(base, paths)
		if err != nil {
			// Report and keep the typed prompt for correction.
			m.prompt = prompt
			m.mode = modeDurablePrompt
			m.input.SetValue(value)
			m.input.CursorEnd()
			m.input.Focus()
			return m, m.flashStatus(sanitizeLine(err.Error(), 200))
		}
		return m, m.submitDurable(prompt.profile, prompt.conv, text, prompt.policy, newRequestID(), files)
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// submitDurable admits text with a stable request ID. A transport failure
// leaves the outcome unknown; it is reported, never resubmitted under a new
// ID. Socket clients can retry with the same request ID, which the host
// answers with the original submission.
func (m Model) submitDurable(profile, conv, content, policy, requestID string, files []continuous.FileAttachment) tea.Cmd {
	return m.runDurable(profile, func(ctx context.Context, host continuous.Host) durableActionMsg {
		submission, err := submitWithFiles(ctx, host, conv, content, requestID, policy, files)
		if err != nil && continuous.Code(err) == "" {
			return durableActionMsg{err: fmt.Errorf("submission outcome unknown, check the continuous panel before resubmitting: %w", err)}
		}
		if err != nil {
			return durableActionMsg{err: err}
		}
		return durableActionMsg{notice: "prompt " + submission.State}
	})
}

// configureDurable changes the model selection: "provider/model" sets both,
// a bare value sets only the model. Other configuration is preserved.
func (m Model) configureDurable(profile, conv, value string) tea.Cmd {
	if value == "" {
		return nil
	}
	var change continuous.AgentConfig
	if provider, model, ok := strings.Cut(value, "/"); ok {
		change.Provider, change.Model = strings.TrimSpace(provider), strings.TrimSpace(model)
	} else {
		change.Model = value
	}
	return m.runDurable(profile, func(ctx context.Context, host continuous.Host) durableActionMsg {
		if _, err := host.Configure(ctx, conv, change); err != nil {
			if continuous.Code(err) == "conflict" {
				return durableActionMsg{err: fmt.Errorf("configuration changed meanwhile, try again")}
			}
			return durableActionMsg{err: err}
		}
		return durableActionMsg{notice: "model changed for the next request"}
	})
}

// setDurableBudget writes a conversation cost cap with a revision check.
func (m Model) setDurableBudget(profile, conv, value string) tea.Cmd {
	if value == "" {
		return nil
	}
	limit, err := strconv.ParseFloat(value, 64)
	if err != nil || limit < 0 {
		return func() tea.Msg { return durableActionMsg{err: fmt.Errorf("budget must be a non-negative number")} }
	}
	return m.runDurable(profile, func(ctx context.Context, host continuous.Host) durableActionMsg {
		budgets, _, err := host.Budgets(ctx, conv)
		if err != nil {
			return durableActionMsg{err: err}
		}
		budget := continuous.Budget{Scope: "conversation", ConversationID: conv}
		for _, status := range budgets {
			if status.Budget.Scope == "conversation" && status.Budget.ConversationID == conv {
				budget = status.Budget
			}
		}
		expected := budget.Revision
		budget.LimitUSD = limit
		if _, err := host.SetBudget(ctx, budget, expected); err != nil {
			if continuous.Code(err) == "conflict" {
				return durableActionMsg{err: fmt.Errorf("budget changed meanwhile, try again")}
			}
			return durableActionMsg{err: err}
		}
		return durableActionMsg{notice: "budget saved"}
	})
}

func (m Model) renameDurable(profile, conv, title string) tea.Cmd {
	return m.runDurable(profile, func(ctx context.Context, host continuous.Host) durableActionMsg {
		snap, err := host.Snapshot(ctx, conv)
		if err != nil {
			return durableActionMsg{err: err}
		}
		if _, err := host.Rename(ctx, conv, snap.Conversation.Revision, title); err != nil {
			if continuous.Code(err) == "conflict" {
				return durableActionMsg{err: fmt.Errorf("conversation changed meanwhile, rename again")}
			}
			return durableActionMsg{err: err}
		}
		return durableActionMsg{notice: "conversation renamed"}
	})
}
