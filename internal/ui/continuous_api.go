package ui

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/patriceckhart/hrdx/internal/api"
	"github.com/patriceckhart/hrdx/internal/continuous"
	"github.com/patriceckhart/hrdx/internal/state"
)

// durableAPIReply carries the result of a background host request back
// into the loop so the reply is sent from the same place as every other.
type durableAPIReply struct {
	reply chan api.Reply
	data  any
	err   error
}

// pendingAttach waits for a durable pane to bind before answering.
type pendingAttach struct {
	reply chan api.Reply
	pane  int
}

func sendReply(reply chan api.Reply, data any, code, err string) {
	select {
	case reply <- api.Reply{Data: data, Err: err, Code: code}:
	default:
	}
}

// hostErrorCode maps host errors onto socket API codes. Host codes such as
// conflict or busy pass through unchanged so clients can react to them.
func hostErrorCode(err error) string {
	if errors.Is(err, continuous.ErrUnsupported) {
		return "unsupported"
	}
	if code := continuous.Code(err); code != "" {
		return code
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return api.CodeTimeout
	}
	return "unavailable"
}

// handleContinuousAPI routes one durable socket request. Host round trips
// run as commands; the loop is never blocked waiting on a host.
func (m *Model) handleContinuousAPI(request api.Request) tea.Cmd {
	reply := request.Reply
	answer := func(data any, code, err string) { sendReply(reply, data, code, err) }
	if !continuous.LocalSupported {
		answer(nil, "unsupported", "durable agents are not supported on this platform yet")
		return nil
	}
	host := func(profileID string, op func(context.Context, continuous.Host) (any, error)) tea.Cmd {
		profile, ok := m.profiles[profileID]
		if !ok {
			answer(nil, api.CodeNotFound, fmt.Sprintf("unknown host profile %q", profileID))
			return nil
		}
		pool := m.cpool
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			client, err := pool.Get(ctx, profile)
			if err != nil {
				return durableAPIReply{reply: reply, err: err}
			}
			data, err := op(ctx, client)
			return durableAPIReply{reply: reply, data: data, err: err}
		}
	}

	switch payload := request.Payload.(type) {
	case nil:
		if request.Method == api.MethodContinuousHosts {
			answer(m.apiHosts(), "", "")
			return m.pollDurable()
		}
	case api.ContinuousHostRef:
		switch request.Method {
		case api.MethodContinuousHostStart:
			profile, ok := m.profiles[payload.Profile]
			if !ok || !profile.Managed {
				answer(nil, api.CodeInvalidParams, "host start needs a managed host profile")
				return nil
			}
			zotBin := m.config.binaryFor("zot")
			return func() tea.Msg {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				err := continuous.StartManaged(ctx, profile, zotBin)
				return durableAPIReply{reply: reply, data: map[string]any{"type": "continuous_host_started", "profile": profile.ID}, err: err}
			}
		case api.MethodContinuousHostStop:
			profile, ok := m.profiles[payload.Profile]
			if !ok || !profile.Managed {
				answer(nil, api.CodeInvalidParams, "host stop needs a managed host profile; external hosts are stopped where they run")
				return nil
			}
			pool := m.cpool
			return func() tea.Msg {
				pool.Drop(profile.ID)
				err := continuous.StopManaged(profile)
				return durableAPIReply{reply: reply, data: map[string]any{"type": "continuous_host_stopped", "profile": profile.ID}, err: err}
			}
		case api.MethodContinuousRecovery:
			return host(payload.Profile, func(ctx context.Context, h continuous.Host) (any, error) {
				actions, err := h.RecoveryPreview(ctx)
				if actions == nil {
					actions = []continuous.RecoveryAction{}
				}
				return map[string]any{"type": "continuous_recovery", "actions": actions}, err
			})
		}
	case api.ContinuousList:
		limit := payload.Limit
		if limit <= 0 || limit > 500 {
			limit = 100
		}
		return host(payload.Profile, func(ctx context.Context, h continuous.Host) (any, error) {
			list, after, err := h.List(ctx, payload.After, limit)
			if list == nil {
				list = []continuous.Conversation{}
			}
			return map[string]any{"type": "continuous_list", "conversations": list, "after": after}, err
		})
	case api.ContinuousTarget:
		if payload.Conversation == "" {
			answer(nil, api.CodeInvalidParams, "conversation is required")
			return nil
		}
		switch request.Method {
		case api.MethodContinuousSnapshot:
			return host(payload.Profile, func(ctx context.Context, h continuous.Host) (any, error) {
				snap, err := h.Snapshot(ctx, payload.Conversation)
				if err != nil {
					return nil, err
				}
				status, err := h.Status(ctx)
				if err != nil {
					return nil, err
				}
				return map[string]any{"type": "continuous_snapshot", "execution": snap.Execution(status.BlockedRuns),
					"conversation": snap.Conversation, "queue": snap.Queue, "run": snap.Run,
					"approvals": snap.Approvals, "recovery": snap.Recovery, "tasks": snap.Tasks,
					"usage": snap.Usage.Usage, "revision": snap.Revision}, nil
			})
		case api.MethodContinuousAbort:
			return host(payload.Profile, func(ctx context.Context, h continuous.Host) (any, error) {
				return map[string]any{"type": "continuous_abort_requested"}, h.Abort(ctx, payload.Conversation)
			})
		case api.MethodContinuousApprovals:
			return host(payload.Profile, func(ctx context.Context, h continuous.Host) (any, error) {
				approvals, err := h.Approvals(ctx, payload.Conversation)
				if approvals == nil {
					approvals = []continuous.Approval{}
				}
				return map[string]any{"type": "continuous_approvals", "approvals": approvals}, err
			})
		case api.MethodContinuousTasks:
			return host(payload.Profile, func(ctx context.Context, h continuous.Host) (any, error) {
				tasks, err := h.Tasks(ctx, payload.Conversation)
				if tasks == nil {
					tasks = []continuous.Task{}
				}
				return map[string]any{"type": "continuous_tasks", "tasks": tasks}, err
			})
		case api.MethodContinuousBudgets:
			return host(payload.Profile, func(ctx context.Context, h continuous.Host) (any, error) {
				budgets, exceeded, err := h.Budgets(ctx, payload.Conversation)
				if budgets == nil {
					budgets = []continuous.BudgetStatus{}
				}
				return map[string]any{"type": "continuous_budgets", "budgets": budgets, "exceeded": exceeded}, err
			})
		}
	case api.ContinuousTaskRef:
		if payload.Task == "" {
			answer(nil, api.CodeInvalidParams, "task is required")
			return nil
		}
		return host(payload.Profile, func(ctx context.Context, h continuous.Host) (any, error) {
			if request.Method == api.MethodContinuousTaskRetry {
				task, err := h.RetryCleanup(ctx, payload.Task)
				return map[string]any{"type": "continuous_task", "task": task}, err
			}
			task, err := h.AbortTask(ctx, payload.Task, payload.IncludeBackground)
			return map[string]any{"type": "continuous_task", "task": task}, err
		})
	case api.ContinuousBudgetSet:
		if payload.Scope != "runtime" && payload.Scope != "conversation" {
			answer(nil, api.CodeInvalidParams, "scope must be runtime or conversation")
			return nil
		}
		if payload.Scope == "conversation" && payload.Conversation == "" {
			answer(nil, api.CodeInvalidParams, "a conversation budget needs conversation")
			return nil
		}
		if payload.LimitUSD < 0 || payload.LimitTokens < 0 || payload.ReserveUSD < 0 || payload.WindowSeconds < 0 {
			answer(nil, api.CodeInvalidParams, "budget values must not be negative")
			return nil
		}
		budget := continuous.Budget{Scope: payload.Scope, ConversationID: payload.Conversation, LimitUSD: payload.LimitUSD,
			LimitTokens: payload.LimitTokens, ReserveUSD: payload.ReserveUSD, Window: payload.WindowSeconds * int64(time.Second)}
		return host(payload.Profile, func(ctx context.Context, h continuous.Host) (any, error) {
			saved, err := h.SetBudget(ctx, budget, payload.ExpectedRevision)
			return map[string]any{"type": "continuous_budget", "budget": saved}, err
		})
	case api.ContinuousConfigure:
		if payload.Conversation == "" || (payload.Provider == "" && payload.Model == "" && payload.Reasoning == "") {
			answer(nil, api.CodeInvalidParams, "conversation and at least one of provider, model, reasoning are required")
			return nil
		}
		change := continuous.AgentConfig{Provider: payload.Provider, Model: payload.Model, Reasoning: payload.Reasoning}
		return host(payload.Profile, func(ctx context.Context, h continuous.Host) (any, error) {
			conv, err := h.Configure(ctx, payload.Conversation, change)
			return map[string]any{"type": "continuous_configured", "conversation": conv}, err
		})
	case api.ContinuousSubmit:
		if payload.Conversation == "" || (strings.TrimSpace(payload.Content) == "" && len(payload.Files) == 0) {
			answer(nil, api.CodeInvalidParams, "conversation and content or files are required")
			return nil
		}
		if strings.TrimSpace(payload.RequestID) == "" {
			answer(nil, api.CodeInvalidParams, "request_id is required so a retry cannot duplicate work")
			return nil
		}
		if len(payload.Content) > api.MaxContinuousSubmitBytes {
			answer(nil, api.CodeInvalidParams, "content is too large")
			return nil
		}
		if payload.Policy != "" && payload.Policy != "queue" && payload.Policy != "steer" {
			answer(nil, api.CodeInvalidParams, "policy must be queue or steer")
			return nil
		}
		policy := payload.Policy
		if policy == "queue" {
			policy = ""
		}
		// Relative attachment paths resolve against the selected workspace,
		// or the hrdx process directory when none is open.
		base := "."
		if current := m.currentSpace(); current != nil {
			base = current.cwd
		}
		files, err := readAttachments(base, payload.Files)
		if err != nil {
			answer(nil, api.CodeInvalidParams, err.Error())
			return nil
		}
		return host(payload.Profile, func(ctx context.Context, h continuous.Host) (any, error) {
			submission, err := submitWithFiles(ctx, h, payload.Conversation, payload.Content, payload.RequestID, policy, files)
			if err != nil {
				return nil, err
			}
			return map[string]any{"type": "continuous_submitted", "submission": submission}, nil
		})
	case api.ContinuousSubmissionRef:
		if payload.Submission == "" {
			answer(nil, api.CodeInvalidParams, "submission is required")
			return nil
		}
		return host(payload.Profile, func(ctx context.Context, h continuous.Host) (any, error) {
			submission, err := h.GetSubmission(ctx, payload.Submission)
			return map[string]any{"type": "continuous_submission", "submission": submission}, err
		})
	case api.ContinuousRename:
		if payload.Conversation == "" || payload.Title == nil {
			answer(nil, api.CodeInvalidParams, "conversation and title are required")
			return nil
		}
		return host(payload.Profile, func(ctx context.Context, h continuous.Host) (any, error) {
			conv, err := h.Rename(ctx, payload.Conversation, payload.ExpectedRevision, *payload.Title)
			return map[string]any{"type": "continuous_renamed", "conversation": conv}, err
		})
	case api.ContinuousDecide:
		if payload.Approval == "" {
			answer(nil, api.CodeInvalidParams, "approval is required")
			return nil
		}
		if payload.Scope != "" && payload.Scope != "call" && payload.Scope != "tool" {
			answer(nil, api.CodeInvalidParams, "scope must be call or tool")
			return nil
		}
		return host(payload.Profile, func(ctx context.Context, h continuous.Host) (any, error) {
			approval, err := h.Decide(ctx, payload.Approval, payload.Allow, payload.Scope, payload.Reason)
			return map[string]any{"type": "continuous_decided", "approval": approval}, err
		})
	case api.ContinuousUnblock:
		if payload.RunID == "" {
			answer(nil, api.CodeInvalidParams, "run_id is required")
			return nil
		}
		return host(payload.Profile, func(ctx context.Context, h continuous.Host) (any, error) {
			return map[string]any{"type": "continuous_unblocked", "run_id": payload.RunID}, h.Unblock(ctx, payload.RunID)
		})
	case api.ContinuousWorkspaceHost:
		target := m.currentSpace()
		if payload.Workspace != "" {
			target = m.spaceByRef(payload.Workspace)
		}
		if target == nil {
			answer(nil, api.CodeNotFound, "workspace not found")
			return nil
		}
		if payload.Profile != "" {
			if _, ok := m.profiles[payload.Profile]; !ok {
				answer(nil, api.CodeNotFound, fmt.Sprintf("unknown host profile %q", payload.Profile))
				return nil
			}
		}
		target.host = payload.Profile
		m.persist()
		answer(map[string]any{"type": "continuous_workspace_host", "workspace": target.name, "profile": payload.Profile}, "", "")
		return nil
	case api.ContinuousAttach:
		return m.apiContinuousAttach(payload, reply)
	}
	answer(nil, api.CodeInvalidParams, "invalid payload")
	return nil
}

// apiContinuousAttach opens a durable view and answers once it is bound.
func (m *Model) apiContinuousAttach(payload api.ContinuousAttach, reply chan api.Reply) tea.Cmd {
	answer := func(data any, code, err string) { sendReply(reply, data, code, err) }
	target := m.currentSpace()
	if payload.Workspace != "" {
		target = m.spaceByRef(payload.Workspace)
	}
	if target == nil {
		answer(nil, api.CodeNotFound, "workspace not found")
		return nil
	}
	if m.disabled["zot"] {
		answer(nil, api.CodeError, "zot is disabled in settings")
		return nil
	}
	var ref *state.DurableRef
	if payload.Conversation != "" {
		bound, ok := findDurable(target, payload.Profile, payload.Conversation)
		if !ok {
			if payload.Profile == "" {
				answer(nil, api.CodeInvalidParams, "an unassociated conversation needs profile")
				return nil
			}
			if _, known := m.profiles[payload.Profile]; !known {
				answer(nil, api.CodeNotFound, fmt.Sprintf("unknown host profile %q", payload.Profile))
				return nil
			}
			// Without workspace_key the view attaches by conversation ID,
			// which is how forks and owned subagents are opened.
			bound = state.DurableRef{Profile: payload.Profile, Workspace: payload.WorkspaceKey, Conversation: payload.Conversation}
		}
		ref = &bound
	} else if payload.Profile != "" {
		if _, ok := m.profiles[payload.Profile]; !ok {
			answer(nil, api.CodeNotFound, fmt.Sprintf("unknown host profile %q", payload.Profile))
			return nil
		}
		ref = &state.DurableRef{Profile: payload.Profile, Workspace: payload.WorkspaceKey}
	}
	split := strings.ToLower(strings.TrimSpace(payload.Split))
	if split != "" && split != "right" && split != "down" && split != "tab" {
		answer(nil, api.CodeInvalidParams, "split must be right, down, or tab")
		return nil
	}
	for index, currentSpace := range m.spaces {
		if currentSpace == target {
			m.selected = index
		}
	}
	newPane, err := m.addDurablePane(target, ref, split)
	if err != nil {
		answer(nil, api.CodeInvalidParams, err.Error())
		return nil
	}
	m.resizePanes(target)
	m.persist()
	m.publish(api.Event{Event: api.EventPaneCreated,
		Data: api.PaneEvent{Pane: newPane.id, Name: newPane.name, Kind: newPane.kind, Workspace: target.name}})
	cmd := m.startPane(target, newPane)
	if newPane.failure != "" {
		answer(map[string]any{"type": "continuous_attached", "pane_id": newPane.id}, "error", newPane.failure)
		return cmd
	}
	m.pendingAttaches = append(m.pendingAttaches, pendingAttach{reply: reply, pane: newPane.id})
	return cmd
}

// resolveAttaches answers attach requests whose pane finished binding.
func (m *Model) resolveAttaches() {
	kept := m.pendingAttaches[:0]
	for _, pending := range m.pendingAttaches {
		target, _ := m.paneByID(pending.pane)
		switch {
		case target == nil:
			sendReply(pending.reply, nil, api.CodeError, "pane closed before it was bound")
		case target.failure != "":
			sendReply(pending.reply, map[string]any{"pane_id": target.id}, api.CodeError, target.failure)
		case target.durableReady:
			sendReply(pending.reply, map[string]any{"type": "continuous_attached", "pane_id": target.id,
				"durable": m.durableStatus(*target.durable)}, "", "")
		default:
			kept = append(kept, pending)
		}
	}
	m.pendingAttaches = kept
}

// apiHosts lists host profiles and their observed connection state. Token
// files are reported only as present; their contents never leave hrdx.
func (m Model) apiHosts() map[string]any {
	ids := make([]string, 0, len(m.profiles))
	for id := range m.profiles {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	hosts := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		profile := m.profiles[id]
		connection := "unknown"
		errText := ""
		if host := m.hostState[id]; host != nil {
			connection = "disconnected"
			if host.connected {
				connection = "connected"
			}
			errText = host.err
		}
		entry := map[string]any{"profile": id, "name": profile.Name, "address": profile.Address,
			"managed": profile.Managed, "authenticated": profile.TokenFile != "", "connection": connection}
		if profile.Managed {
			entry["cwd"] = profile.CWD
			entry["stopped"] = continuous.IntentionallyStopped(profile)
		}
		if errText != "" {
			entry["error"] = errText
		}
		hosts = append(hosts, entry)
	}
	return map[string]any{"type": "continuous_hosts", "supported": continuous.LocalSupported, "hosts": hosts}
}
