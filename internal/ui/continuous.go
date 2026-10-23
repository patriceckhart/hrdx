package ui

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/patriceckhart/hrdx/internal/api"
	"github.com/patriceckhart/hrdx/internal/continuous"
	"github.com/patriceckhart/hrdx/internal/state"
)

// Durable execution: a durable pane is a real PTY running zot's attached
// TUI as a view of one conversation on a zot continuous host. The host owns
// execution, queues, approvals, and recovery; hrdx owns layout, host
// connections, status, and explicit control requests. Ordinary panes never
// touch anything in this file.

// durablePollInterval is how often bound conversations are reconciled from
// host snapshots. Snapshots are authoritative, so a missed poll can delay a
// state change but never invent or lose one.
const durablePollInterval = 2 * time.Second

// durableCallTimeout bounds background host requests. It stays below the
// socket API's round-trip timeout so API callers get the real error.
const durableCallTimeout = 4 * time.Second

// durableInfo is the last observed state of one conversation.
type durableInfo struct {
	exec      string // continuous.State* when observed
	connected bool   // the last poll reached the host and read the snapshot
	err       string // sanitized failure of the last poll
	at        time.Time
	snap      continuous.Snapshot
	seen      bool // at least one successful observation
}

// hostInfo is the last observed state of one host profile.
type hostInfo struct {
	connected bool
	err       string
	blocked   []string
	at        time.Time
}

type durablePreparedMsg struct {
	id  int
	ref state.DurableRef
	err error
}

type durableTickMsg struct{}

// durableKickMsg starts polling from Init, whose value receiver cannot
// record the ticking flag on the running model itself.
type durableKickMsg struct{}

func durableKick() tea.Msg { return durableKickMsg{} }

type durablePollMsg struct {
	hosts map[string]durableHostResult
}

type durableHostResult struct {
	err    error
	status continuous.RuntimeStatus
	snaps  map[string]continuous.Snapshot
	errs   map[string]error
}

// durableActionMsg reports a finished user-requested host operation.
type durableActionMsg struct {
	notice string
	err    error
	open   *durableOpen // follow-up: open a menu after the operation
}

// durableOpen asks the loop to show a review menu with fetched data.
type durableOpen struct {
	kind      string // "approvals" or "recovery"
	profile   string
	conv      string
	approvals []continuous.Approval
	recovery  []continuous.RecoveryAction
	tasks     []continuous.Task
	children  []continuous.Conversation
	budgets   []continuous.BudgetStatus
}

// durableCategories are the durable notification categories, in settings
// order. Each can be muted independently; muting never hides state.
var durableCategories = []string{"finished", "failed", "approval", "recovery", "host"}

func durableKey(profile, conversation string) string {
	return profile + "\x00" + conversation
}

// stateDir is the directory next to the state file, "" without persistence.
func (m Model) stateDir() string {
	if m.statePath == "" {
		return ""
	}
	return filepath.Dir(m.statePath)
}

// durableAvailable reports whether new durable panes can be offered.
func (m Model) durableAvailable() bool {
	return continuous.LocalSupported && m.stateDir() != "" && !m.disabled["zot"]
}

// workspaceProfile returns the host profile new durable panes in owner use:
// the workspace's explicit choice, or the project's managed host.
func (m *Model) workspaceProfile(owner *space) (continuous.Profile, error) {
	if owner.host != "" {
		profile, ok := m.profiles[owner.host]
		if !ok {
			return continuous.Profile{}, fmt.Errorf("workspace host profile %q is not defined", owner.host)
		}
		return profile, nil
	}
	dir := m.stateDir()
	if dir == "" {
		return continuous.Profile{}, errors.New("durable agents need a state directory (--state)")
	}
	profile := continuous.ManagedProfile(dir, owner.cwd)
	if _, ok := m.profiles[profile.ID]; !ok {
		if err := continuous.UpsertProfile(dir, profile); err != nil {
			return continuous.Profile{}, fmt.Errorf("register managed host: %w", err)
		}
		m.profiles[profile.ID] = profile
	}
	return m.profiles[profile.ID], nil
}

// nextWorkspaceKey picks a host workspace key for a new independent
// conversation. The first one uses the project directory, matching a manual
// `zot --continuous` started there; later ones get a numbered suffix.
func nextWorkspaceKey(owner *space, profile string) string {
	used := map[string]bool{}
	for _, ref := range owner.durables {
		if ref.Profile == profile {
			used[ref.Workspace] = true
		}
	}
	for _, currentTab := range owner.tabs {
		for _, current := range currentTab.panes {
			if current.durable != nil && current.durable.Profile == profile {
				used[current.durable.Workspace] = true
			}
		}
	}
	if !used[owner.cwd] {
		return owner.cwd
	}
	for n := 2; ; n++ {
		key := fmt.Sprintf("%s#%d", owner.cwd, n)
		if !used[key] {
			return key
		}
	}
}

// newDurablePane builds an unstarted durable pane. ref may name an existing
// conversation (another view) or be empty for a new independent agent.
func (m *Model) newDurablePane(target *space, ref *state.DurableRef) *pane {
	count := 1
	for _, currentTab := range target.tabs {
		for _, existing := range currentTab.panes {
			if existing.durable != nil {
				count++
			}
		}
	}
	newPane := &pane{id: m.nextID, name: fmt.Sprintf("durable %d", count), kind: "zot"}
	m.nextID++
	if ref != nil {
		copied := *ref
		newPane.durable = &copied
	} else {
		newPane.durable = &state.DurableRef{}
	}
	return newPane
}

// addDurablePane inserts a durable pane like addPaneSide and returns it.
func (m *Model) addDurablePane(target *space, ref *state.DurableRef, split string) (*pane, error) {
	newPane := m.newDurablePane(target, ref)
	switch split {
	case "", "right":
		m.insertPane(target, newPane, true, false)
	case "down":
		m.insertPane(target, newPane, false, false)
	case "left":
		m.insertPane(target, newPane, true, true)
	case "up":
		m.insertPane(target, newPane, false, true)
	case "tab":
		target.tabs = append(target.tabs, &tab{})
		target.active = len(target.tabs) - 1
		m.insertPane(target, newPane, true, false)
	case "space":
		m.insertPane(target, newPane, true, false)
	default:
		return nil, fmt.Errorf("unknown split %q (right, down, tab)", split)
	}
	return newPane, nil
}

// insertPane is addPaneSide for an already built pane.
func (m *Model) insertPane(target *space, newPane *pane, vertical, before bool) {
	currentTab := target.tab()
	if currentTab.layout == nil {
		currentTab.layout = leafNode(newPane)
	} else {
		focused := currentTab.panes[currentTab.selected]
		if focused.floating != nil {
			currentTab.layout.walk(func(candidate *pane) { focused = candidate })
		}
		insertAtSide(currentTab.layout, focused, newPane, vertical, before)
	}
	currentTab.panes = append(currentTab.panes, newPane)
	currentTab.selected = len(currentTab.panes) - 1
}

// prepareDurable resolves and verifies a durable pane's binding off the
// loop before its view process starts. A new binding opens (or finds) the
// host root for its workspace key. A restored binding must still exist on
// the host: a missing conversation means a different store and is never
// silently replaced by a fresh one.
func (m *Model) prepareDurable(owner *space, target *pane) tea.Cmd {
	if !continuous.LocalSupported {
		target.failure = "durable agents: local continuous hosts are not supported on this platform yet"
		return nil
	}
	ref := *target.durable
	// Forks and owned conversations have no workspace key; their views
	// attach by conversation ID, which needs a zot that supports it.
	byID := ref.Conversation != "" && ref.Workspace == ""
	if ref.Profile == "" {
		profile, err := m.workspaceProfile(owner)
		if err != nil {
			target.failure = err.Error()
			return nil
		}
		ref.Profile = profile.ID
	}
	profile, ok := m.profiles[ref.Profile]
	if !ok {
		target.failure = fmt.Sprintf("unknown continuous host profile %q (add it with hrdx continuous add)", ref.Profile)
		return nil
	}
	if ref.Workspace == "" && !byID {
		ref.Workspace = nextWorkspaceKey(owner, ref.Profile)
	}
	target.durable = &ref
	target.failure = ""
	pool, config := m.cpool, m.durableCreateConfig()
	restoring := ref.Conversation != ""
	zotBin := m.config.binaryFor("zot")
	id := target.id
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		// Probed here, off the UI loop: it runs the zot binary once.
		if byID && !zotSupportsConversationFlag(zotBin) {
			return durablePreparedMsg{id: id, ref: ref, err: errors.New("this zot cannot attach to a conversation by ID, update zot")}
		}
		if profile.Managed {
			if continuous.Probe(ctx, profile) != nil {
				if restoring && continuous.IntentionallyStopped(profile) {
					return durablePreparedMsg{id: id, ref: ref, err: errors.New("host was stopped intentionally, start it from the continuous panel")}
				}
				if err := continuous.StartManaged(ctx, profile, zotBin); err != nil {
					return durablePreparedMsg{id: id, ref: ref, err: err}
				}
			}
		}
		host, err := pool.Get(ctx, profile)
		if err != nil {
			return durablePreparedMsg{id: id, ref: ref, err: err}
		}
		if restoring {
			snap, err := host.Snapshot(ctx, ref.Conversation)
			if err != nil {
				if continuous.Code(err) == "not_found" {
					err = errors.New("conversation not found on this host (different store?), view not attached")
				}
				return durablePreparedMsg{id: id, ref: ref, err: err}
			}
			if byID {
				// The view attaches to this exact ID; nothing else to verify.
				ref.Title = snap.Conversation.Title
				return durablePreparedMsg{id: id, ref: ref}
			}
			// The attached view opens the root for the workspace key. It must
			// resolve to the bound conversation, never to a different one.
			root, err := host.OpenRoot(ctx, ref.Workspace, nil)
			if err != nil {
				return durablePreparedMsg{id: id, ref: ref, err: err}
			}
			if root.ID != ref.Conversation {
				return durablePreparedMsg{id: id, ref: ref, err: errors.New("host workspace key resolves to a different conversation, view not attached")}
			}
			ref.Title = snap.Conversation.Title
			return durablePreparedMsg{id: id, ref: ref}
		}
		conv, err := host.OpenRoot(ctx, ref.Workspace, config)
		if err != nil {
			return durablePreparedMsg{id: id, ref: ref, err: err}
		}
		ref.Conversation = conv.ID
		ref.Title = conv.Title
		return durablePreparedMsg{id: id, ref: ref}
	}
}

// durableCreateConfig returns explicitly requested provider, model, and
// reasoning for new conversations. Existing conversations keep theirs.
func (m Model) durableCreateConfig() *continuous.AgentConfig {
	var config continuous.AgentConfig
	args := m.config.ZotArgs
	for index := 0; index+1 < len(args); index++ {
		switch args[index] {
		case "--provider":
			config.Provider = args[index+1]
		case "--model":
			config.Model = args[index+1]
		case "--reasoning":
			config.Reasoning = args[index+1]
		}
	}
	if config == (continuous.AgentConfig{}) {
		return nil
	}
	return &config
}

// durableLaunch returns the attached zot command for a prepared pane.
func (m Model) durableLaunch(target *pane) (string, []string, error) {
	profile, ok := m.profiles[target.durable.Profile]
	if !ok {
		return "", nil, fmt.Errorf("unknown continuous host profile %q", target.durable.Profile)
	}
	args := []string{"--continuous", profile.Address}
	if target.durable.Workspace == "" {
		args = append(args, "--continuous-conversation", target.durable.Conversation)
	} else {
		args = append(args, "--continuous-workspace", target.durable.Workspace)
	}
	if profile.TokenFile != "" {
		args = append(args, "--continuous-token-file", profile.TokenFile)
	}
	if profile.TLSCA != "" {
		args = append(args, "--continuous-tls-ca", profile.TLSCA)
	}
	// Explicit selections only matter when the root is new; zot ignores
	// them for an existing root. Session flags do not apply to attached views.
	for index := 0; index+1 < len(m.config.ZotArgs); index++ {
		switch m.config.ZotArgs[index] {
		case "--provider", "--model", "--reasoning":
			args = append(args, m.config.ZotArgs[index], m.config.ZotArgs[index+1])
			index++
		}
	}
	return m.config.binaryFor("zot"), args, nil
}

// associate records a conversation in its workspace's durable list.
func associate(owner *space, ref state.DurableRef) {
	for index, existing := range owner.durables {
		if existing.Profile == ref.Profile && existing.Conversation == ref.Conversation {
			owner.durables[index].Title = ref.Title
			owner.durables[index].Workspace = ref.Workspace
			return
		}
	}
	owner.durables = append(owner.durables, ref)
}

// updateContinuous handles durable messages. It reports false for every
// other message so ordinary handling is untouched.
func (m *Model) updateContinuous(message tea.Msg) (bool, tea.Cmd) {
	switch msg := message.(type) {
	case durablePreparedMsg:
		target, owner := m.paneByID(msg.id)
		if target == nil || target.durable == nil {
			return true, nil
		}
		if msg.err != nil {
			target.failure = "durable: " + sanitizeLine(msg.err.Error(), 300)
			target.durable = &msg.ref
			m.persist()
			m.resolveAttaches()
			return true, nil
		}
		target.durable = &msg.ref
		target.durableReady = true
		associate(owner, msg.ref)
		m.persist()
		m.publish(api.Event{Event: api.EventContinuousBound, Data: m.durableEvent(owner, msg.ref, "")})
		m.resolveAttaches()
		return true, tea.Batch(m.startPane(owner, target), m.ensureDurableTick(), m.pollDurable())
	case durableAPIReply:
		if msg.err != nil {
			sendReply(msg.reply, nil, hostErrorCode(msg.err), sanitizeLine(msg.err.Error(), 300))
		} else {
			sendReply(msg.reply, msg.data, "", "")
		}
		return true, m.pollDurable()
	case durableKickMsg:
		return true, tea.Batch(m.pollDurable(), m.ensureDurableTick())
	case durableTickMsg:
		m.durableTicking = false
		return true, m.pollDurable()
	case durablePollMsg:
		m.durablePolling = false
		cmd := m.applyDurablePoll(msg)
		var tick tea.Cmd
		if !m.ticking && m.anyBusy() {
			m.ticking = true
			tick = spinTick()
		}
		return true, tea.Batch(cmd, tick, m.ensureDurableTick())
	case durableActionMsg:
		var cmd tea.Cmd
		if msg.err != nil {
			cmd = m.flashStatus(sanitizeLine(msg.err.Error(), 200))
		} else if msg.notice != "" {
			cmd = m.flashInfo(msg.notice)
		}
		if msg.open != nil {
			m.openDurableReview(*msg.open)
		}
		return true, tea.Batch(cmd, m.pollDurable())
	}
	return false, nil
}

// durableTargets lists every bound conversation per profile.
func (m Model) durableTargets() map[string][]string {
	targets := map[string][]string{}
	add := func(ref state.DurableRef) {
		if ref.Profile == "" || ref.Conversation == "" {
			return
		}
		if !slices.Contains(targets[ref.Profile], ref.Conversation) {
			targets[ref.Profile] = append(targets[ref.Profile], ref.Conversation)
		}
	}
	for _, currentSpace := range m.spaces {
		for _, ref := range currentSpace.durables {
			add(ref)
		}
		for _, currentTab := range currentSpace.tabs {
			for _, current := range currentTab.panes {
				if current.durable != nil {
					add(*current.durable)
				}
			}
		}
	}
	return targets
}

// ensureDurableTick schedules the next poll while anything is bound.
func (m *Model) ensureDurableTick() tea.Cmd {
	if m.durableTicking || m.quitting || len(m.durableTargets()) == 0 {
		return nil
	}
	m.durableTicking = true
	return tea.Tick(durablePollInterval, func(time.Time) tea.Msg { return durableTickMsg{} })
}

// pollDurable reads host status and snapshots in the background. At most
// one poll is in flight, so a slow host never queues work.
func (m *Model) pollDurable() tea.Cmd {
	if m.durablePolling || m.quitting {
		return nil
	}
	targets := m.durableTargets()
	if len(targets) == 0 {
		return nil
	}
	profiles := map[string]continuous.Profile{}
	for id := range targets {
		if profile, ok := m.profiles[id]; ok {
			profiles[id] = profile
		}
	}
	m.durablePolling = true
	pool := m.cpool
	return func() tea.Msg {
		results := map[string]durableHostResult{}
		for id, conversations := range targets {
			profile, ok := profiles[id]
			if !ok {
				results[id] = durableHostResult{err: fmt.Errorf("unknown host profile %q", id)}
				continue
			}
			ctx, cancel := context.WithTimeout(context.Background(), durableCallTimeout)
			result := durableHostResult{snaps: map[string]continuous.Snapshot{}, errs: map[string]error{}}
			host, err := pool.Get(ctx, profile)
			if err == nil {
				result.status, err = host.Status(ctx)
			}
			if err != nil {
				pool.Drop(id)
				result.err = err
				results[id] = result
				cancel()
				continue
			}
			for _, conv := range conversations {
				snap, err := host.Snapshot(ctx, conv)
				if err != nil {
					result.errs[conv] = err
					continue
				}
				result.snaps[conv] = snap
			}
			cancel()
			results[id] = result
		}
		return durablePollMsg{hosts: results}
	}
}

// applyDurablePoll records observations and raises notifications for
// transitions. The first observation of a conversation never notifies, so
// reconnects and restarts do not replay historical completions.
func (m *Model) applyDurablePoll(msg durablePollMsg) tea.Cmd {
	now := time.Now()
	var notices []string
	ids := make([]string, 0, len(msg.hosts))
	for id := range msg.hosts {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, profileID := range ids {
		result := msg.hosts[profileID]
		previous := m.hostState[profileID]
		current := &hostInfo{at: now, connected: result.err == nil}
		if result.err != nil {
			current.err = sanitizeLine(result.err.Error(), 200)
		} else {
			current.blocked = result.status.BlockedRuns
		}
		m.hostState[profileID] = current
		if previous != nil && previous.connected != current.connected {
			if !current.connected && !m.durableMuted["host"] {
				notices = append(notices, "continuous host "+profileID+" disconnected")
			}
			m.publish(api.Event{Event: api.EventContinuousHost, Data: api.ContinuousHostEvent{
				Profile: profileID, Connected: current.connected, Error: current.err}})
		}
		for _, conv := range m.durableTargets()[profileID] {
			key := durableKey(profileID, conv)
			info := m.durState[key]
			if info == nil {
				info = &durableInfo{}
				m.durState[key] = info
			}
			before, wasSeen, wasConnected := info.exec, info.seen, info.connected
			if result.err != nil {
				info.connected, info.err = false, current.err
				continue
			}
			snap, ok := result.snaps[conv]
			if !ok {
				info.connected = false
				if err := result.errs[conv]; err != nil {
					info.err = sanitizeLine(err.Error(), 200)
				}
				continue
			}
			info.connected, info.err, info.at, info.snap = true, "", now, snap
			info.exec = snap.Execution(current.blocked)
			m.refreshDurableTitles(profileID, conv, snap.Conversation.Title)
			if !wasSeen {
				info.seen = true
				continue
			}
			if info.exec == before {
				continue
			}
			label := m.durableLabel(profileID, conv)
			category, text := "", ""
			switch {
			case info.exec == continuous.StateApproval:
				category, text = "approval", "approval required"
			case info.exec == continuous.StateRecovery:
				category, text = "recovery", "recovery review required"
			case info.exec == continuous.StateFailed:
				category, text = "failed", "failed"
			case info.exec == continuous.StateReady && wasConnected && (before == continuous.StateRunning || before == continuous.StateQueued):
				category, text = "finished", "finished"
			}
			if category != "" {
				// The attention dot always follows; muting only silences.
				m.markDurableAttention(profileID, conv)
				if !m.durableMuted[category] {
					notices = append(notices, label+": "+text)
				}
			}
			m.publish(api.Event{Event: api.EventContinuousState, Data: api.ContinuousStateEvent{
				Profile: profileID, Conversation: conv, State: info.exec, Previous: before}})
		}
	}
	if len(notices) == 0 {
		return nil
	}
	if m.soundOn {
		playSound(m.soundKind)
	}
	if m.notifyOn {
		systemNotify()
	}
	return m.flashInfo(strings.Join(notices, ", "))
}

// refreshDurableTitles mirrors a host title into local bindings.
func (m *Model) refreshDurableTitles(profile, conv, title string) {
	changed := false
	for _, currentSpace := range m.spaces {
		for index := range currentSpace.durables {
			ref := &currentSpace.durables[index]
			if ref.Profile == profile && ref.Conversation == conv && ref.Title != title {
				ref.Title, changed = title, true
			}
		}
		for _, currentTab := range currentSpace.tabs {
			for _, current := range currentTab.panes {
				if ref := current.durable; ref != nil && ref.Profile == profile && ref.Conversation == conv && ref.Title != title {
					ref.Title, changed = title, true
				}
			}
		}
	}
	if changed {
		m.persist()
	}
}

// markDurableAttention flags unfocused views of a conversation.
func (m *Model) markDurableAttention(profile, conv string) {
	focused := m.currentPane()
	for _, currentSpace := range m.spaces {
		for _, currentTab := range currentSpace.tabs {
			for _, current := range currentTab.panes {
				if ref := current.durable; ref != nil && ref.Profile == profile && ref.Conversation == conv {
					if m.appBlurred || focused == nil || focused != current {
						m.paneAttention[current.id] = true
					}
				}
			}
		}
	}
}

// durableLabel names a conversation for notices without exposing content.
func (m Model) durableLabel(profile, conv string) string {
	for _, currentSpace := range m.spaces {
		for _, ref := range currentSpace.durables {
			if ref.Profile == profile && ref.Conversation == conv {
				return durableRefName(currentSpace, ref)
			}
		}
	}
	return shortID(conv)
}

func durableRefName(owner *space, ref state.DurableRef) string {
	if ref.Title != "" {
		return sanitizeLine(ref.Title, 40)
	}
	if ref.Workspace == owner.cwd {
		return owner.name
	}
	if suffix, ok := strings.CutPrefix(ref.Workspace, owner.cwd); ok {
		return owner.name + suffix
	}
	return shortID(ref.Conversation)
}

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

// durablePaneInfo returns the observed state of a durable pane's
// conversation and whether its host is connected.
func (m Model) durablePaneInfo(target *pane) (*durableInfo, bool) {
	if target == nil || target.durable == nil || target.durable.Conversation == "" {
		return nil, false
	}
	host := m.hostState[target.durable.Profile]
	info := m.durState[durableKey(target.durable.Profile, target.durable.Conversation)]
	return info, host != nil && host.connected
}

// durableSidebarState is the sidebar label for a durable pane. Running work
// shows as the spinner; disconnected never reads as idle or finished.
func (m Model) durableSidebarState(target *pane) string {
	if target.failure != "" {
		return styleDotOff.Render("failed")
	}
	if !target.durableReady {
		return stylePaneDim.Render("binding")
	}
	info, connected := m.durablePaneInfo(target)
	if host := m.hostState[target.durable.Profile]; host != nil && !connected {
		return styleDotOff.Render("offline")
	}
	if info == nil || !info.seen {
		return stylePaneDim.Render("durable")
	}
	switch info.exec {
	case continuous.StateApproval:
		return styleDotBusy.Render("approve")
	case continuous.StateRecovery:
		return styleDotOff.Render("recover")
	case continuous.StateFailed:
		return styleDotOff.Render("failed")
	case continuous.StateQueued:
		return styleDotBusy.Render("queued")
	}
	return stylePaneDim.Render("durable")
}

// durableBusy reports authoritative running state for durable panes.
func (m Model) durableBusy(target *pane) bool {
	info, connected := m.durablePaneInfo(target)
	return connected && info != nil && info.connected && info.exec == continuous.StateRunning
}

// sanitizeLine removes control characters, including escape sequences'
// introducers, so host-provided text cannot drive the terminal.
func sanitizeLine(value string, limit int) string {
	cleaned := strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' || r == '\r' {
			return ' '
		}
		if unicode.IsControl(r) || r == 0x9b {
			return -1
		}
		return r
	}, value)
	runes := []rune(cleaned)
	if limit > 0 && len(runes) > limit {
		return string(runes[:limit]) + "..."
	}
	return cleaned
}

func newRequestID() string {
	var buf [12]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return fmt.Sprintf("hrdx-%d", time.Now().UnixNano())
	}
	return "hrdx-" + hex.EncodeToString(buf[:])
}

// durableEvent builds an event payload for a binding in owner.
func (m Model) durableEvent(owner *space, ref state.DurableRef, stateName string) api.ContinuousStateEvent {
	return api.ContinuousStateEvent{Profile: ref.Profile, Conversation: ref.Conversation,
		WorkspaceKey: ref.Workspace, Workspace: owner.name, State: stateName}
}

// durableStatus projects a binding for the socket API.
func (m Model) durableStatus(ref state.DurableRef) *api.DurableStatus {
	status := &api.DurableStatus{Profile: ref.Profile, WorkspaceKey: ref.Workspace,
		Conversation: ref.Conversation, Title: ref.Title, Connection: "unknown"}
	if host := m.hostState[ref.Profile]; host != nil {
		status.Connection = "disconnected"
		if host.connected {
			status.Connection = "connected"
		}
	}
	if info := m.durState[durableKey(ref.Profile, ref.Conversation)]; info != nil && info.seen {
		status.Execution = info.exec
		status.Revision = info.snap.Revision
		status.ObservedAt = info.at.UTC().Format(time.RFC3339)
		status.Queued = len(info.snap.Queue)
		status.Approvals = len(info.snap.Approvals)
	}
	return status
}

// runDurable runs one host operation for profile off the loop.
func (m Model) runDurable(profileID string, op func(context.Context, continuous.Host) durableActionMsg) tea.Cmd {
	profile, ok := m.profiles[profileID]
	if !ok {
		return func() tea.Msg {
			return durableActionMsg{err: fmt.Errorf("unknown host profile %q", profileID)}
		}
	}
	pool := m.cpool
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), durableCallTimeout)
		defer cancel()
		host, err := pool.Get(ctx, profile)
		if err != nil {
			return durableActionMsg{err: err}
		}
		return op(ctx, host)
	}
}

// startDurableHost explicitly starts a managed host.
func (m Model) startDurableHost(profileID string) tea.Cmd {
	profile, ok := m.profiles[profileID]
	if !ok {
		return nil
	}
	zotBin := m.config.binaryFor("zot")
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := continuous.StartManaged(ctx, profile, zotBin); err != nil {
			return durableActionMsg{err: err}
		}
		return durableActionMsg{notice: "continuous host " + profileID + " running"}
	}
}

// stopDurableHost explicitly stops a managed host. Running work is
// interrupted and stays recoverable in the store.
func (m Model) stopDurableHost(profileID string) tea.Cmd {
	profile, ok := m.profiles[profileID]
	if !ok {
		return nil
	}
	pool := m.cpool
	return func() tea.Msg {
		pool.Drop(profileID)
		if err := continuous.StopManaged(profile); err != nil {
			return durableActionMsg{err: err}
		}
		return durableActionMsg{notice: "continuous host " + profileID + " stopped"}
	}
}

// retryDurable re-verifies a failed durable pane binding.
func (m *Model) retryDurable(target *pane) tea.Cmd {
	_, owner := m.paneByID(target.id)
	if owner == nil || target.durable == nil {
		return nil
	}
	target.failure = ""
	target.durableReady = false
	if target.term != nil {
		target.term.Close()
		target.term = nil
		target.running = false
	}
	return m.startPane(owner, target)
}
