package api

import "time"

// Durable execution through zot continuous hosts. These methods are
// additive: existing pane methods keep their meaning, and "submit work to a
// conversation" is deliberately separate from "type into a pane".

// Continuous method names.
const (
	MethodContinuousHosts      = "continuous.hosts"
	MethodContinuousHostStart  = "continuous.host.start"
	MethodContinuousHostStop   = "continuous.host.stop"
	MethodContinuousList       = "continuous.list"
	MethodContinuousAttach     = "continuous.attach"
	MethodContinuousSnapshot   = "continuous.snapshot"
	MethodContinuousSubmit     = "continuous.submit"
	MethodContinuousSubmission = "continuous.submission"
	MethodContinuousRename     = "continuous.rename"
	MethodContinuousAbort      = "continuous.abort"
	MethodContinuousApprovals  = "continuous.approvals"
	MethodContinuousDecide     = "continuous.decide"
	MethodContinuousRecovery   = "continuous.recovery"
	MethodContinuousUnblock    = "continuous.unblock"
	MethodContinuousSetHost    = "continuous.workspace_host"
	MethodContinuousTasks      = "continuous.tasks"
	MethodContinuousTaskAbort  = "continuous.task.abort"
	MethodContinuousTaskRetry  = "continuous.task.retry_cleanup"
	MethodContinuousBudgets    = "continuous.budgets"
	MethodContinuousBudgetSet  = "continuous.budget.set"
	MethodContinuousConfigure  = "continuous.configure"
	EventContinuousBound       = "continuous.bound"
	EventContinuousState       = "continuous.state_changed"
	EventContinuousHost        = "continuous.host_changed"
)

// MaxContinuousSubmitBytes bounds one submitted prompt.
const MaxContinuousSubmitBytes = 256 << 10

// continuousRequestTimeout bounds durable requests, which wait for a host
// and, for attach or host start, for a managed host to come up.
const continuousRequestTimeout = 45 * time.Second

// continuousParams returns a fresh params value for each durable method.
var continuousParams = map[string]func() any{
	MethodContinuousHosts:      func() any { return &struct{}{} },
	MethodContinuousHostStart:  func() any { return &ContinuousHostRef{} },
	MethodContinuousHostStop:   func() any { return &ContinuousHostRef{} },
	MethodContinuousList:       func() any { return &ContinuousList{} },
	MethodContinuousAttach:     func() any { return &ContinuousAttach{} },
	MethodContinuousSnapshot:   func() any { return &ContinuousTarget{} },
	MethodContinuousSubmit:     func() any { return &ContinuousSubmit{} },
	MethodContinuousSubmission: func() any { return &ContinuousSubmissionRef{} },
	MethodContinuousRename:     func() any { return &ContinuousRename{} },
	MethodContinuousAbort:      func() any { return &ContinuousTarget{} },
	MethodContinuousApprovals:  func() any { return &ContinuousTarget{} },
	MethodContinuousDecide:     func() any { return &ContinuousDecide{} },
	MethodContinuousRecovery:   func() any { return &ContinuousHostRef{} },
	MethodContinuousUnblock:    func() any { return &ContinuousUnblock{} },
	MethodContinuousSetHost:    func() any { return &ContinuousWorkspaceHost{} },
	MethodContinuousTasks:      func() any { return &ContinuousTarget{} },
	MethodContinuousTaskAbort:  func() any { return &ContinuousTaskRef{} },
	MethodContinuousTaskRetry:  func() any { return &ContinuousTaskRef{} },
	MethodContinuousBudgets:    func() any { return &ContinuousTarget{} },
	MethodContinuousBudgetSet:  func() any { return &ContinuousBudgetSet{} },
	MethodContinuousConfigure:  func() any { return &ContinuousConfigure{} },
}

// ContinuousTaskRef names one host task.
type ContinuousTaskRef struct {
	Profile           string `json:"profile"`
	Task              string `json:"task"`
	IncludeBackground bool   `json:"include_background,omitempty"`
}

// ContinuousBudgetSet writes one budget. Scope is runtime or conversation;
// a conversation budget needs Conversation. ExpectedRevision is the
// budget's current revision (0 for a new one).
type ContinuousBudgetSet struct {
	Profile          string  `json:"profile"`
	Scope            string  `json:"scope"`
	Conversation     string  `json:"conversation,omitempty"`
	LimitUSD         float64 `json:"limit_usd,omitempty"`
	LimitTokens      int     `json:"limit_tokens,omitempty"`
	ReserveUSD       float64 `json:"reserve_usd,omitempty"`
	WindowSeconds    int64   `json:"window_seconds,omitempty"`
	ExpectedRevision uint64  `json:"expected_revision"`
}

// ContinuousConfigure changes the model selection of one conversation.
// Empty fields keep their current value.
type ContinuousConfigure struct {
	Profile      string `json:"profile"`
	Conversation string `json:"conversation"`
	Provider     string `json:"provider,omitempty"`
	Model        string `json:"model,omitempty"`
	Reasoning    string `json:"reasoning,omitempty"`
}

// IsContinuousMethod reports whether method belongs to the durable surface.
func IsContinuousMethod(method string) bool {
	_, ok := continuousParams[method]
	return ok
}

// DurableStatus is a pane's or workspace's durable binding in status replies.
// Connection is connected, disconnected, or unknown. Execution is empty until
// the first successful observation and is the last observed value while
// disconnected, never a guess.
type DurableStatus struct {
	Profile      string `json:"profile"`
	WorkspaceKey string `json:"workspace_key"`
	Conversation string `json:"conversation"`
	Title        string `json:"title,omitempty"`
	Connection   string `json:"connection"`
	Execution    string `json:"execution,omitempty"`
	Revision     uint64 `json:"revision,omitempty"`
	ObservedAt   string `json:"observed_at,omitempty"`
	Queued       int    `json:"queued,omitempty"`
	Approvals    int    `json:"approvals,omitempty"`
	Views        int    `json:"views,omitempty"`
	Error        string `json:"error,omitempty"`
}

// ContinuousTarget names a conversation on a host profile.
type ContinuousTarget struct {
	Profile      string `json:"profile"`
	Conversation string `json:"conversation"`
}

// ContinuousHostRef names a host profile.
type ContinuousHostRef struct {
	Profile string `json:"profile"`
}

// ContinuousList lists conversations on a host.
type ContinuousList struct {
	Profile string `json:"profile"`
	After   string `json:"after,omitempty"`
	Limit   int    `json:"limit,omitempty"`
}

// ContinuousAttach opens a durable view. Without a conversation it creates a
// new independent durable agent in the workspace. With one, it opens another
// view of that exact conversation, which must already be associated with the
// workspace or be a workspace root on the host.
type ContinuousAttach struct {
	Workspace    string `json:"workspace,omitempty"` // name or path; empty: selected workspace
	Profile      string `json:"profile,omitempty"`   // empty: the workspace's host
	Conversation string `json:"conversation,omitempty"`
	WorkspaceKey string `json:"workspace_key,omitempty"`
	Split        string `json:"split,omitempty"` // right (default), down, tab
}

// ContinuousSubmit admits text. RequestID makes retries safe: a repeated
// request with the same ID returns the original submission.
type ContinuousSubmit struct {
	Profile      string `json:"profile"`
	Conversation string `json:"conversation"`
	Content      string `json:"content"`
	RequestID    string `json:"request_id"`
	Policy       string `json:"policy,omitempty"` // queue (default) or steer
	// Files are read by hrdx from these local paths and sent to the host,
	// so the host never needs the client's filesystem.
	Files []string `json:"files,omitempty"`
}

// ContinuousSubmissionRef reads one submission.
type ContinuousSubmissionRef struct {
	Profile    string `json:"profile"`
	Submission string `json:"submission"`
}

// ContinuousRename sets a conversation title.
type ContinuousRename struct {
	Profile          string  `json:"profile"`
	Conversation     string  `json:"conversation"`
	ExpectedRevision uint64  `json:"expected_revision"`
	Title            *string `json:"title"`
}

// ContinuousDecide decides one pending approval explicitly.
type ContinuousDecide struct {
	Profile  string `json:"profile"`
	Approval string `json:"approval"`
	Allow    bool   `json:"allow"`
	Scope    string `json:"scope,omitempty"` // call (default) or tool
	Reason   string `json:"reason,omitempty"`
}

// ContinuousUnblock releases one recovery hold.
type ContinuousUnblock struct {
	Profile string `json:"profile"`
	RunID   string `json:"run_id"`
}

// ContinuousWorkspaceHost selects the host for new durable panes. An empty
// profile restores the project's managed host.
type ContinuousWorkspaceHost struct {
	Workspace string `json:"workspace,omitempty"`
	Profile   string `json:"profile"`
}

// ContinuousHostEvent reports a host connection change.
type ContinuousHostEvent struct {
	Profile   string `json:"profile"`
	Connected bool   `json:"connected"`
	Error     string `json:"error,omitempty"`
}

// ContinuousStateEvent reports an execution state change or a new binding.
type ContinuousStateEvent struct {
	Profile      string `json:"profile"`
	Conversation string `json:"conversation"`
	WorkspaceKey string `json:"workspace_key,omitempty"`
	Workspace    string `json:"workspace,omitempty"`
	State        string `json:"state,omitempty"`
	Previous     string `json:"previous,omitempty"`
}
