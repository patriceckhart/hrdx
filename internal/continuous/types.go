package continuous

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// AgentConfig is the subset of a conversation configuration hrdx shows and
// sends. Unknown fields are preserved by the host because hrdx never writes
// configuration except through explicit configure requests.
type AgentConfig struct {
	Provider  string `json:"provider,omitempty"`
	Model     string `json:"model,omitempty"`
	Reasoning string `json:"reasoning,omitempty"`
}

// Conversation is the host's conversation record.
type Conversation struct {
	ID        string      `json:"id"`
	Title     string      `json:"title"`
	Created   time.Time   `json:"created"`
	Revision  uint64      `json:"revision"`
	Config    AgentConfig `json:"config"`
	QueueHeld bool        `json:"queue_held,omitempty"`
	Parent    *struct {
		ID string `json:"id"`
	} `json:"parent,omitempty"`
	Owner *struct {
		ConversationID string `json:"conversation_id"`
		ID             string `json:"id"`
	} `json:"owner,omitempty"`
}

// Submission is one admitted input.
type Submission struct {
	ID        string `json:"id"`
	RequestID string `json:"request_id,omitempty"`
	Content   string `json:"content"`
	State     string `json:"state"`
	Policy    string `json:"policy,omitempty"`
	Kind      string `json:"kind,omitempty"`
	Error     string `json:"error,omitempty"`
}

// Run is the run-shaped projection of the active or last settled chain.
type Run struct {
	ID      string `json:"id"`
	Phase   string `json:"phase"`
	Outcome string `json:"outcome,omitempty"`
	Error   string `json:"error,omitempty"`
}

// Approval is one persisted approval request.
type Approval struct {
	ID             string          `json:"id"`
	ConversationID string          `json:"conversation_id"`
	RunID          string          `json:"run_id"`
	Tool           string          `json:"tool"`
	Args           json.RawMessage `json:"args"`
	Summary        string          `json:"summary,omitempty"`
	State          string          `json:"state"`
	ExpiresAt      *time.Time      `json:"expires_at,omitempty"`
}

// RecoveryAction is the host's plan for an interrupted run.
type RecoveryAction struct {
	ConversationID string `json:"conversation_id"`
	RunID          string `json:"run_id"`
	Phase          string `json:"phase"`
	Action         string `json:"action"`
	Detail         string `json:"detail,omitempty"`
	Automatic      bool   `json:"automatic"`
}

// Task is a host task attached to a conversation snapshot.
type Task struct {
	ID             string `json:"id"`
	ConversationID string `json:"conversation_id"`
	Kind           string `json:"kind,omitempty"`
	State          string `json:"state"`
	Background     bool   `json:"background"`
}

// Usage is the conversation usage total.
type Usage struct {
	Usage struct {
		Input  int64   `json:"input_tokens"`
		Output int64   `json:"output_tokens"`
		Cost   float64 `json:"cost_usd"`
	} `json:"usage"`
}

// Snapshot is one committed view of a conversation.
type Snapshot struct {
	Revision     uint64          `json:"revision"`
	Conversation Conversation    `json:"conversation"`
	Queue        []Submission    `json:"queue"`
	Run          *Run            `json:"run,omitempty"`
	Approvals    []Approval      `json:"approvals,omitempty"`
	Recovery     *RecoveryAction `json:"recovery,omitempty"`
	Tasks        []Task          `json:"tasks,omitempty"`
	Usage        Usage           `json:"usage"`
}

// Execution states derived from a snapshot. They are exclusive and ordered
// by urgency: a pending approval outranks a running chain.
const (
	StateReady    = "ready"
	StateQueued   = "queued"
	StateRunning  = "running"
	StateApproval = "approval"
	StateRecovery = "recovery"
	StateFailed   = "failed"
	StateAborted  = "aborted"
)

// Execution derives the conversation's execution state. blocked lists run
// IDs the host holds for recovery review.
func (s Snapshot) Execution(blocked []string) string {
	if len(s.Approvals) > 0 {
		return StateApproval
	}
	if s.Run != nil && s.Run.Phase != "done" {
		for _, id := range blocked {
			if id == s.Run.ID {
				return StateRecovery
			}
		}
		return StateRunning
	}
	queued := false
	for _, submission := range s.Queue {
		if submission.State == "queued" || submission.State == "running" {
			queued = true
		}
	}
	if queued && !s.Conversation.QueueHeld {
		return StateQueued
	}
	if s.Run != nil {
		switch s.Run.Outcome {
		case "failed":
			return StateFailed
		case "aborted":
			return StateAborted
		}
	}
	if s.Conversation.QueueHeld {
		return StateFailed
	}
	return StateReady
}

// RuntimeStatus is the subset of runtime.status hrdx uses.
type RuntimeStatus struct {
	Version     string          `json:"version"`
	BlockedRuns []string        `json:"blocked_runs"`
	Caps        map[string]bool `json:"capabilities"`
}

// Host wraps a client with typed operations.
type Host struct{ *Client }

func (h Host) Status(ctx context.Context) (RuntimeStatus, error) {
	var out RuntimeStatus
	return out, h.CallInto(ctx, "runtime.status", nil, &out)
}

// OpenRoot opens or finds the root conversation for a workspace key.
func (h Host) OpenRoot(ctx context.Context, workspace string, config *AgentConfig) (Conversation, error) {
	params := map[string]any{"workspace": workspace}
	if config != nil {
		params["config"] = config
	}
	var out Conversation
	return out, h.CallInto(ctx, "conversation.create", params, &out)
}

// List returns one page of conversations.
func (h Host) List(ctx context.Context, after string, limit int) ([]Conversation, string, error) {
	var out struct {
		Conversations []Conversation `json:"conversations"`
		After         string         `json:"after"`
	}
	err := h.CallInto(ctx, "conversation.list", map[string]any{"after": after, "limit": limit}, &out)
	return out.Conversations, out.After, err
}

func (h Host) Snapshot(ctx context.Context, id string) (Snapshot, error) {
	var out Snapshot
	// One entry is the smallest page the host accepts; hrdx does not render
	// transcripts, the attached zot view does.
	return out, h.CallInto(ctx, "conversation.snapshot", map[string]any{"id": id, "limit": 1}, &out)
}

// FileAttachment is one file sent with a prompt. The host expands UTF-8
// text into labelled context and sends supported images as image blocks;
// it rejects other content. Data is base64 on the wire.
type FileAttachment struct {
	Name string `json:"name"`
	Data []byte `json:"data"`
}

// Attachment limits enforced by zot hosts; checked early to fail fast.
const (
	MaxAttachments     = 16
	MaxAttachmentBytes = 1 << 20
)

// CheckAttachments applies the host's count and size limits locally.
func CheckAttachments(files []FileAttachment) error {
	if len(files) > MaxAttachments {
		return fmt.Errorf("at most %d attachments per submission", MaxAttachments)
	}
	total := 0
	for _, file := range files {
		if strings.TrimSpace(file.Name) == "" {
			return fmt.Errorf("attachment name required")
		}
		total += len(file.Data)
	}
	if total > MaxAttachmentBytes {
		return fmt.Errorf("attachments exceed %d bytes", MaxAttachmentBytes)
	}
	return nil
}

// Submit admits text with a caller-chosen request ID so an uncertain
// outcome can be retried or queried without duplicating work.
func (h Host) Submit(ctx context.Context, id, content, requestID, policy string) (Submission, error) {
	return h.SubmitFiles(ctx, id, content, requestID, policy, nil)
}

// SubmitFiles is Submit with file attachments. Callers must check the
// host's attachments capability first; older hosts would drop the files.
func (h Host) SubmitFiles(ctx context.Context, id, content, requestID, policy string, files []FileAttachment) (Submission, error) {
	params := map[string]any{"id": id, "content": content, "request_id": requestID}
	if policy != "" {
		params["policy"] = policy
	}
	if len(files) > 0 {
		params["files"] = files
	}
	var out Submission
	return out, h.CallInto(ctx, "conversation.submit", params, &out)
}

func (h Host) Rename(ctx context.Context, id string, revision uint64, title string) (Conversation, error) {
	var out Conversation
	return out, h.CallInto(ctx, "conversation.rename", map[string]any{"id": id, "expected_revision": revision, "title": title}, &out)
}

func (h Host) Fork(ctx context.Context, id string) (Conversation, error) {
	var out Conversation
	return out, h.CallInto(ctx, "conversation.fork", map[string]any{"id": id}, &out)
}

func (h Host) Abort(ctx context.Context, id string) error {
	return h.CallInto(ctx, "conversation.abort", map[string]any{"id": id}, nil)
}

func (h Host) Approvals(ctx context.Context, conversation string) ([]Approval, error) {
	var out struct {
		Approvals []Approval `json:"approvals"`
	}
	return out.Approvals, h.CallInto(ctx, "approval.list", map[string]any{"conversation": conversation}, &out)
}

// Decide records an explicit decision for one pending approval. The host
// refuses decisions for approvals that are no longer pending.
func (h Host) Decide(ctx context.Context, id string, allow bool, scope, reason string) (Approval, error) {
	var out Approval
	return out, h.CallInto(ctx, "approval.decide", map[string]any{"id": id, "allow": allow, "scope": scope, "reason": reason}, &out)
}

func (h Host) RecoveryPreview(ctx context.Context) ([]RecoveryAction, error) {
	var out struct {
		Actions []RecoveryAction `json:"actions"`
	}
	return out.Actions, h.CallInto(ctx, "recovery.preview", nil, &out)
}

func (h Host) Unblock(ctx context.Context, runID string) error {
	return h.CallInto(ctx, "recovery.unblock", map[string]any{"run_id": runID}, nil)
}

// Tasks lists the conversation's host tasks, including owned subagents.
func (h Host) Tasks(ctx context.Context, conversation string) ([]Task, error) {
	var out struct {
		Tasks []Task `json:"tasks"`
	}
	return out.Tasks, h.CallInto(ctx, "task.list", map[string]any{"conversation": conversation}, &out)
}

// AbortTask explicitly aborts one task. Background tasks are included only
// when asked, matching the host's conversation abort semantics.
func (h Host) AbortTask(ctx context.Context, id string, includeBackground bool) (Task, error) {
	var out Task
	return out, h.CallInto(ctx, "task.abort", map[string]any{"id": id, "include_background": includeBackground}, &out)
}

// RetryCleanup asks the host to retry a task's failed cleanup.
func (h Host) RetryCleanup(ctx context.Context, id string) (Task, error) {
	var out Task
	return out, h.CallInto(ctx, "task.retry-cleanup", map[string]any{"id": id}, &out)
}

// Budget is a host spending limit. Window is in nanoseconds on the wire.
type Budget struct {
	Scope          string  `json:"scope"`
	ConversationID string  `json:"conversation_id,omitempty"`
	LimitUSD       float64 `json:"limit_usd,omitempty"`
	LimitTokens    int     `json:"limit_tokens,omitempty"`
	ReserveUSD     float64 `json:"reserve_usd,omitempty"`
	Window         int64   `json:"window,omitempty"`
	Revision       uint64  `json:"revision"`
}

// BudgetStatus is one budget with its current spend.
type BudgetStatus struct {
	Budget      Budget  `json:"budget"`
	SpentUSD    float64 `json:"spent_usd"`
	ReservedUSD float64 `json:"reserved_usd"`
	Tokens      int     `json:"tokens"`
	Unknown     int     `json:"unknown"`
	Exceeded    string  `json:"exceeded,omitempty"`
}

// Budgets reads the budgets that apply to a conversation.
func (h Host) Budgets(ctx context.Context, conversation string) ([]BudgetStatus, bool, error) {
	var out struct {
		Budgets  []BudgetStatus `json:"budgets"`
		Exceeded bool           `json:"exceeded"`
	}
	err := h.CallInto(ctx, "budget.get", map[string]any{"conversation": conversation}, &out)
	return out.Budgets, out.Exceeded, err
}

// SetBudget writes a budget with an optimistic revision check.
func (h Host) SetBudget(ctx context.Context, budget Budget, expected uint64) (Budget, error) {
	var out Budget
	return out, h.CallInto(ctx, "budget.set", map[string]any{"budget": budget, "expected_revision": expected}, &out)
}

// Usage reads the conversation's usage totals.
func (h Host) Usage(ctx context.Context, conversation string) (Usage, error) {
	var out Usage
	return out, h.CallInto(ctx, "usage.get", map[string]any{"id": conversation}, &out)
}

// Configure changes provider, model, and reasoning explicitly. The host
// replaces the whole configuration, so the current raw configuration is
// read and only the given non-empty fields are changed; instructions, tool
// restrictions, and fields unknown to hrdx are preserved. The revision
// check rejects a configuration that changed in between.
func (h Host) Configure(ctx context.Context, id string, change AgentConfig) (Conversation, error) {
	var snap struct {
		Conversation struct {
			Revision uint64                     `json:"revision"`
			Config   map[string]json.RawMessage `json:"config"`
		} `json:"conversation"`
	}
	if err := h.CallInto(ctx, "conversation.snapshot", map[string]any{"id": id, "limit": 1}, &snap); err != nil {
		return Conversation{}, err
	}
	config := snap.Conversation.Config
	if config == nil {
		config = map[string]json.RawMessage{}
	}
	set := func(key, value string) {
		if value != "" {
			encoded, _ := json.Marshal(value)
			config[key] = encoded
		}
	}
	set("provider", change.Provider)
	set("model", change.Model)
	set("reasoning", change.Reasoning)
	var out Conversation
	return out, h.CallInto(ctx, "conversation.configure", map[string]any{"id": id, "expected_revision": snap.Conversation.Revision, "config": config}, &out)
}

func (h Host) GetSubmission(ctx context.Context, id string) (Submission, error) {
	var out Submission
	return out, h.CallInto(ctx, "submission.get", map[string]any{"id": id}, &out)
}
