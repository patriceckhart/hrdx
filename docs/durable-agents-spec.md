# Native durable and ordinary agent workspaces

Status: design specification, implemented. The user-facing behavior is documented in the README under Durable agents and Socket API; where this document and the README differ, the README describes what ships. Method and field names here were reconciled during implementation and some differ from the final API.

## 1. Product goal

Make zot continuous a first-class, built-in execution backend in hrdx while preserving hrdx's current role as a minimal terminal multiplexer for ordinary agents and shells.

Users can mix ordinary agents, durable zot conversations, and shells in the same workspace, tab, and split layout. Durable execution is optional. Installing or running zot continuous is never required for existing hrdx workflows.

The integration belongs to hrdx itself. It must not require a plugin, custom harness registration, or terminal-output scraping to access continuous functionality.

hrdx remains one portable Go binary. Zot remains a separately installed executable and execution service. hrdx does not embed a model engine, duplicate zot's scheduler, or become a replacement chat application.

## 2. Non-negotiable compatibility

- Existing workspaces, tabs, panes, splits, grouping, settings, and state files continue to load.
- Existing default agent selection and launch behavior remain unchanged until the user explicitly selects durable execution.
- Standalone zot, Claude Code, Codex, pi, custom harnesses, remote terminal commands, and plain shells remain supported.
- Ordinary agents retain current input forwarding, screen rendering, busy detection, notifications, session resume, and holder behavior.
- Existing CLI flags and socket requests retain their meanings. New behavior uses additive, optional configuration and API fields.
- A continuous host failure must not prevent shell use, ordinary agent launch, or navigation in unrelated workspaces.
- No existing workspace or conversation is automatically converted to a durable one.
- No durable request silently falls back to standalone execution when the host is unavailable.
- Existing plugins remain independent of the native integration.

## 3. Execution modes and identities

### 3.1 Ordinary terminal execution

The current behavior remains a complete, supported mode, not a compatibility afterthought.

Each pane runs an ordinary agent or shell in a real PTY. A holder-backed PTY survives TUI exit. A directly owned PTY ends with its pane or TUI. Explicit pane close cleans up its process. If a held agent session is gone, the existing agent-specific resume behavior applies.

An ordinary agent may maintain its own session history, but hrdx must not describe that as zot continuous durability. Holder persistence is process persistence, not crash-recoverable execution.

### 3.2 Durable execution

A durable agent is a conversation on a zot continuous host. A terminal pane runs the attached zot TUI as a view of that conversation.

The durable identity is the pair of host identity and conversation ID. A pane ID, PTY, holder session ID, workspace display name, socket filename, and conversation title are not substitutes for that identity.

A durable conversation may have zero, one, or multiple attached views. Closing a view does not cancel admitted work. Multiple views of the same conversation are deliberately shared views, not independent agents.

### 3.3 Selection and defaults

- Agent kind and execution mode are separate choices. Selecting zot alone does not implicitly select continuous execution.
- Initial durable support is zot only. Other agents remain ordinary terminal agents.
- New-pane and new-tab flows offer ordinary agent, durable zot, and shell choices without making the ordinary flow longer by default.
- Global and per-workspace defaults may select durable zot explicitly. Existing saved defaults remain ordinary.
- A workspace's default host is only a convenience. An existing pane's binding never changes when the default changes.
- One workspace can contain ordinary zot, durable zot, another agent, and a shell simultaneously.
- Changing execution mode creates a new agent or view. It must not reinterpret a running ordinary process as a durable conversation or migrate history implicitly.

## 4. Native host management

### 4.1 Host registry

Maintain a native registry of connection profiles with stable IDs, display names, ownership mode, endpoint or transport configuration, optional credential references, and startup configuration for managed hosts.

Workspace snapshots reference profiles by ID. Secrets are stored separately and are never embedded in workspace snapshots, logs, API status, or normal process arguments.

Changing an endpoint must not silently bind saved conversations to a different store. Verify host/store identity where supported. If identity cannot be established, require explicit confirmation before rebinding saved durable references.

### 4.2 Managed hosts

hrdx can start a local zot continuous host on explicit request or when creating a durable agent using an explicitly configured managed profile.

Requirements:

- Validate the zot binary, protocol support, store location, and startup configuration.
- Reuse an existing matching host. Prevent duplicate startup across concurrent hrdx instances.
- Respect zot's store ownership and locking. Never bypass a lock or start a second writer to repair a connection failure.
- Do not launch the service as an ordinary pane or an ephemeral child that dies with the TUI.
- Ensure the managed service survives normal hrdx exit using platform-correct process ownership.
- Use bounded startup probes and show actionable, sanitized failures.
- Track ownership independently of reachability. A reachable external host is not thereby managed by hrdx.
- Show whether a managed service is running, starting, stopping, unreachable, or incompatible.
- Support explicit start, reconnect, restart, and stop actions.
- Stop and restart show affected work and require an explicit decision. A disconnected host has unknown activity, not zero activity.
- An intentional stop suppresses automatic startup until the user explicitly starts it or requests new durable execution.
- Never run a hidden crash-restart loop. Offer restart after unexpected host exit, then let zot perform recovery.

The initial default is a separate managed profile/store per local project, created only when durable execution is selected. Explicit profiles may share a host across workspaces. Host working directories and tool permissions must match that mapping. A conversation's workspace label is not proof that tools run in the correct project directory.

### 4.3 External and remote hosts

External profiles connect to independently managed services. hrdx never stops, restarts, or modifies their stores as a side effect of closing local views.

The complete scope includes remote profiles through a supported authenticated transport or an explicitly configured tunnel. Do not treat a local socket path as remotely accessible. Transport capabilities, host filesystem location, and credential handling must be visible.

Local integration ships first. Remote support requires its own platform and security acceptance tests before being advertised.

## 5. Conversation management

Provide native actions to:

- List and search conversations available to the selected profile and workspace.
- Create a root conversation with explicit initial configuration.
- Open an existing conversation in a tab, split, or temporary floating terminal.
- Open another view of the exact same conversation.
- Rename a conversation using the host's revision checks.
- Fork a conversation through the host, then open the resulting identity.
- Inspect conversations with no open panes.
- Explicitly abort active work where authorized.
- Remove a local association without deleting host history.

Use stable IDs for actions. Titles and workspace names are display metadata. Creation and retries must use host-supported idempotency mechanisms where available. Do not assume a workspace root creation method creates multiple independent roots for the same workspace key.

If the supported attached zot CLI cannot target an arbitrary conversation ID exactly, add that capability in zot before shipping the relevant hrdx action. Attaching to the workspace root is not an acceptable substitute for attaching to a selected fork or child.

Deleting conversation history or stores is not part of pane close, workspace close, profile removal, or ordinary host shutdown.

## 6. UI and navigation

### 6.1 Sidebar and status

Keep the current workspace/tab/pane hierarchy. Mark durable views distinctly and show authoritative execution state for their conversations.

Provide a built-in, collapsible workspace activity panel for conversations without open panes, queued work, approvals, recovery holds, tasks, and usage. It is host-rendered UI, not a plugin view or a fake terminal pane.

Panel visibility must not change persisted split topology. Narrow terminals retain usable terminal content and offer a modal or compact alternative.

### 6.2 Actions and input

Expose actions through the existing prefix command system, context menus, agent picker, settings, and named actions for custom key bindings.

- Workspace actions: choose host, open activity, create durable conversation, attach existing conversation.
- Pane actions: identify mode, open another view, inspect conversation, close view.
- Conversation actions: rename, fork, queue prompt, steer, inspect submissions, approvals, recovery, and explicit abort.
- Host actions: inspect, connect, reconnect, and authorized managed lifecycle operations.

Do not reserve new unprefixed terminal keys. Escape, control keys, bracketed paste, mouse capture, kitty keyboard behavior, selection, and scrollback retain their existing contracts.

Closing a control panel returns focus to the previous valid target. Stale menu targets must not operate on a newly focused conversation.

### 6.3 Real terminals

Keep zot's attached TUI as the primary conversation editor and transcript renderer. File/image selection and normal conversation interaction remain zot responsibilities.

hrdx owns layouts, navigation, host connections, status, and explicit control operations. It does not implement a second chat renderer, attachment store, model engine, or tool executor.

Every terminal pane remains a real PTY. Native panels are controls outside the pane model.

## 7. Structured state and notifications

Model connection health separately from execution state. A disconnected view may retain a clearly marked last-known running state, but it cannot claim current knowledge.

Expose at least:

| Dimension | States or information |
| --- | --- |
| Connection | connecting, connected, disconnected, incompatible |
| Execution | ready, queued, running, approval required, recovery blocked, failed, aborted |
| View process | starting, running, exited, attach failed |
| Freshness | current snapshot revision or last successful observation |

A conversation can have running work and queued submissions at once. Pending approvals and recovery holds are not ordinary idle states.

Use host snapshots and subscriptions for durable status. Screen-based detection continues for ordinary agents and must not override authoritative durable state.

Notify about completion, failure, approval requests, recovery holds, and connection loss using existing user notification preferences plus configurable event categories.

Deduplicate by stable event identity and submission outcome across multiple views, reconnects, and history replay. Initial snapshots must not produce notifications for all historical completions. Notifications must not expose tool arguments, prompt text, or terminal contents by default.

## 8. Submissions, queues, and steering

Provide native controls for:

- Submitting work with a stable request ID.
- Queueing a follow-up.
- Steering at the next host-supported boundary.
- Inspecting submission state and outcome.
- Withdrawing and reordering queued submissions when supported and authorized.
- Waiting for a specific submission rather than an inferred pane busy/idle transition.
- Displaying held queues and why they cannot proceed.

The host remains the ordering and execution authority. hrdx must not implement another durable queue.

A transport timeout can mean the host admitted work but the response was lost. Retry only with the same idempotency identity where supported, or query the outcome. Never generate a new request ID automatically for an uncertain submission.

Do not persist a hidden offline prompt queue. On disconnect, preserve unsent editor input where hrdx owns that input and explicitly report that it was not submitted. Attached zot editor behavior remains zot's contract.

Native prompt controls initially submit text. File and image attachments remain available in the attached zot TUI. Any later native attachment controls must use capability-negotiated host transfers and the host's size and validation rules.

## 9. Approvals and recovery

### 9.1 Approval review

Show the host, workspace, conversation, tool, normalized arguments, expiry, and available scopes. Potentially large or sensitive arguments are shown only in the explicit review UI with safe terminal escaping.

Allow and deny require explicit actions. Revalidate the pending identity before deciding. Report expired, stale, or already resolved requests without applying the decision to a replacement request.

Do not invent broader authorization scopes than the host provides. Do not implicitly approve because a user opens a pane or reconnects.

### 9.2 Recovery review

Show interrupted operations, the host's recovery preview, known outcomes, and uncertainty. Explain whether an action releases a recovery hold, reports an interrupted operation, or aborts work.

Never replay tools in hrdx. Never promise exactly-once arbitrary shell execution. Zot's safe, idempotent, reconcile, and never-replay contracts remain authoritative.

Recovery actions require the necessary host role and explicit user intent. Reconnecting, opening a workspace, and queueing a prompt must not unblock held work.

## 10. Tasks, subagents, configuration, and budgets

The activity panel exposes host-supported tasks, their deadlines and status, related conversations, owned subagents, usage, and budgets.

- Open an owned subagent's conversation without creating a new child.
- Show parent/child relationships without requiring a terminal for every child.
- Abort tasks or retry cleanup only when supported and authorized.
- Distinguish a task, submission, conversation, and view in both UI and API.
- Configure provider, model, and reasoning through the host's supported configuration operations.
- Preserve existing conversation configuration on ordinary attachment.
- Display available models using host capabilities rather than assuming local credentials or catalogs apply.
- Show and explicitly change budgets when supported and authorized.

Do not add a second scheduler or infer that killing a subagent terminal cancels its hosted task.

## 11. Lifecycle contract

| Action | Ordinary agent or shell | Durable zot view |
| --- | --- | --- |
| Quit hrdx with holder enabled | Detach PTYs, processes continue | Detach attached TUIs, host work continues |
| Quit with holder disabled | Direct PTYs end | Attached TUI ends, independently owned host work continues |
| Close pane or temporary view | Clean up that PTY/process | Clean up attached TUI only, conversation continues |
| Close tab or workspace | Clean up its PTYs/processes | Clean up local views, no implicit abort or host stop |
| Holder or view process dies | Existing resume rules on relaunch | Reattach exact saved conversation, no prompt resubmission |
| Continuous host dies | No effect on ordinary panes | Mark unavailable, preserve bindings, offer restart/reconnect |
| Abort conversation | Not a generic ordinary-agent action | Explicit host operation, not a terminal kill |
| Stop managed host | No effect on unrelated ordinary panes | Explicit service operation with interruption warning |

`--persist=false` continues to govern PTY holder behavior, not durable host execution. `--fresh` may clean up obsolete held views but must not abort durable conversations or destroy their stores. `hrdx stop` retains its holder-only meaning. New managed-host lifecycle commands are separate.

With `--state ""`, do not save new host associations or create hidden persistent stores automatically. Explicit external connections and explicitly selected managed store locations may still be used, with clear notice that zot's store remains durable.

Floating durable views remain ephemeral local views. Closing them must not leak their holder sessions and must not cancel their hosted conversations.

## 12. Persistence and restoration

Add optional fields for execution mode, profile ID, conversation ID, and any host workspace key required for exact attachment. Missing execution mode means ordinary terminal execution.

Persist host profile definitions separately from conversation history and credentials. Store local workspace associations independently of whether a pane currently views the conversation.

Do not persist host transcripts, approvals, task queues, provider secrets, live subscription handles, or process-local registrations in hrdx workspace snapshots.

Restoration order:

1. Load and validate existing workspace/layout state with current legacy fallbacks.
2. Restore ordinary panes through the existing path without waiting for continuous services.
3. Resolve durable bindings and validate profile and host identity.
4. Reuse surviving attached PTYs when valid, or launch an exact attached view when missing.
5. Fetch current host state and establish subscriptions without gaps.
6. Present unresolved bindings as recoverable unavailable views, never as fresh ordinary agents.

Missing profiles, deleted conversations, incompatible hosts, corrupt fields, and invalid layouts must not discard unrelated panes. Offer explicit reassociation or removal of the unavailable local view.

Older state files remain readable. Older binaries are not promised safe downgrade behavior after saving new durable fields, because they may ignore those fields. Document the downgrade limitation and require a backup before using an older release on new state.

## 13. Connections and protocol compatibility

Implement a bounded native continuous client with handshake, capability discovery, cancellation, deadlines, response correlation, snapshot retrieval, and subscriptions.

- Use supported protocol methods, not private journal reads.
- Negotiate before exposing mutations.
- Disable unsupported actions with an explanation.
- Do not assume hrdx and zot's similarly shaped JSON protocols are wire compatible.
- Reconnect with bounded exponential backoff and jitter. Allow manual retry and stop retrying after explicit disconnect.
- On a dropped watch or cursor expiry, obtain a fresh snapshot and restore the subscription using the host's documented sequencing.
- Detect stale connection generations so delayed replies cannot mutate a replacement binding.
- Never replay mutations simply because a subscription reconnects.
- Bound memory, frame sizes, pending requests, and UI event queues.
- Coalesce high-frequency progress updates, but reconcile from authoritative state after any observation gap.
- Do not send blocking network requests from the Bubble Tea update loop.

Connection loss and execution failure are different conditions. A disconnected host may still be executing work.

## 14. Socket API

Preserve existing pane-oriented methods and their contracts, including terminal input and pane waits. Durable operations are a separate additive API surface.

Proposed method families, to be finalized in implementation:

| Family | Operations |
| --- | --- |
| `continuous.host.*` | list, status, connect, disconnect, start, stop, restart |
| `continuous.conversation.*` | list, snapshot, create, attach, rename, fork, configure, abort |
| `continuous.submission.*` | submit, get, wait, withdraw, reorder |
| `continuous.approval.*` | list, get, decide |
| `continuous.recovery.*` | preview, unblock |
| `continuous.task.*` | list, get, abort, retry cleanup |
| `continuous.usage.*` | get |
| `continuous.budget.*` | get, set |

Requests identify a host profile and stable target IDs. Workspace or pane convenience targets resolve once on the UI loop. Mutations support host revision checks and stable request identities where available.

Status adds optional execution mode, durable binding, connection health, execution state, and freshness fields. Existing `running` and `busy` fields retain their existing meanings and are not sufficient to express approval or disconnected states.

Publish additive host, conversation, submission, approval, recovery, and task events. Preserve existing best-effort nonblocking subscriber delivery. Clients recover missed events by querying snapshots. Never describe the hrdx event socket as a durable event log.

All requests route through `api.Request` and buffered replies. Remote work runs asynchronously and returns completion messages to the UI loop. Slow hosts must not block rendering, unrelated API requests, or shutdown.

The existing local control socket is a trusted same-user interface. Document that allowing continuous approval or abort methods through it delegates the corresponding host authority. Never expose credentials or connection tokens in API responses.

## 15. Configuration and command surfaces

Add native settings and CLI help for host profiles, managed startup, workspace defaults, notification categories, and diagnostics.

Exact flag and subcommand spelling is finalized with implementation. Required operations are host inventory, inspect, connect, managed start/stop/restart, and workspace host selection.

Existing `--agent`, provider/model flags, `--zot-bin`, `--persist`, `--fresh`, and `--state` must retain their existing ordinary-mode behavior. In durable mode, explicitly requested provider/model/reasoning selections apply to creation or explicit configuration, not silent reconfiguration on attachment.

Report missing binary, unsupported CLI attachment targeting, unsupported protocol, unavailable credentials, lock conflicts, wrong host identity, and inaccessible store paths distinctly without printing private payloads.

## 16. Architecture and ownership

- `cmd/hrdx`: CLI, configuration assembly, managed service process wiring, and platform-specific startup.
- New focused `internal/continuous` package: host protocol client, connection lifecycle, typed host state, capability handling, and observation. No live UI mutation or terminal rendering.
- `internal/api`: additive public requests, responses, events, and socket routing.
- `internal/state`: optional serializable bindings and preferences.
- `internal/ui`: model operations, panels, menus, status projection, notifications, and persistence conversion.
- `internal/term`: attached TUI PTY launch, resize, input, and process cleanup using existing boundaries.
- `internal/holder`: existing PTY ownership and replay. No model execution or continuous store ownership.
- `internal/vt`: unchanged terminal emulation responsibilities.
- Platform-specific process code and `internal/winproc`: native service and child cleanup where applicable.

Background workers communicate through typed messages. Bubble Tea remains the owner of live workspace state. Keyboard, mouse, and socket actions share model operations and validation.

Prefer the standard library and existing dependencies. Any imported zot module or new transport dependency needs a binary-size, transitive dependency, API stability, and cross-platform review. Do not pull zot's full agent runtime into hrdx merely to speak its protocol.

## 17. Security and platform requirements

- Managed local sockets and stores use restrictive platform-appropriate permissions.
- Authentication, role restrictions, and capability failures remain visible and enforced by the host.
- Tokens are not written into state snapshots, diagnostic output, or terminal titles.
- Escape untrusted names, titles, tool arguments, and errors before native UI rendering.
- Host filesystem permissions remain the host's responsibility. A workspace mapping is not a sandbox.
- Remote file paths and local file paths are distinguished. No implicit project upload or filesystem synchronization.
- Native Windows, WSL, and Git Bash have distinct process, shell, path, and socket semantics.
- Managed service survival, attachment, reconnect, shutdown, and cleanup are tested on macOS, Linux, and native Windows.
- Preserve ConPTY behavior and inspect build-tagged process counterparts before changes.
- If zot lacks a required transport on a platform, show a capability-specific limitation. Ordinary hrdx must still work normally there.

## 18. Delivery sequence

The complete scope is native. Staging is for implementation safety, not a plugin-based interim architecture.

1. Compatibility foundation: execution mode/binding model, additive persistence, bounded client, fake host tests, and ordinary-mode regression coverage.
2. Exact attachment: external local profile, create/list/attach, multiple views, restoration, structured state, and safe close semantics.
3. Managed local hosts: startup ownership, duplicate prevention, explicit lifecycle, settings, and diagnostics.
4. Native control: activity panel, submissions, queue/steering, notifications, conversation rename/fork/configure, and API parity.
5. Human intervention: approvals, recovery review, stale-action protection, and permission-aware controls.
6. Full workspace operations: tasks, subagents, usage/budgets, authenticated remote profiles, and platform hardening.

Each stage must be a complete tested vertical slice. Do not advertise later-stage behavior as implemented. Update README documentation with each user-visible implementation change. This specification alone does not change runtime behavior.

## 19. Acceptance criteria and validation

### Ordinary workflows

- Existing state restores with identical ordinary agent, shell, holder, layout, and resume behavior.
- hrdx works with zot absent and no host configured.
- Ordinary agents launch and remain interactive while a continuous host is slow, incompatible, or unreachable.
- Custom harnesses, remote terminal panes, plugins, notifications, and existing socket operations retain their contracts.

### Mixed and durable workflows

- One tab contains an ordinary zot, a durable zot, another ordinary agent, and a shell without mode confusion.
- Two views of one conversation observe the same work. Two independent agents use different conversation identities.
- A durable conversation continues after its last view closes and remains discoverable.
- Quit/relaunch restores exact durable bindings and layouts without resubmitting prompts.
- Losing the holder recreates attached views, not conversations.
- Changing profile defaults does not retarget existing views.
- Fork and owned-child attachment targets the requested conversation, never the root by accident.

### Failure and safety

- Lost submission responses do not duplicate admitted work.
- Dropped watches and expired cursors reconcile state without stale status or replayed completion notifications.
- Host restart exposes zot's recovery state without automatic unsafe replay or approval.
- Expired approvals, stale revisions, deleted conversations, wrong stores, and late replies fail safely.
- Explicit close cleans up attached PTYs, including ephemeral views, while preserving host work.
- Managed startup races do not create duplicate services or bypass store locks.
- Managed stop, intentional disconnect, and crash recovery have distinct tested behavior.
- Stalled host reads/writes, cancellation, subscriber backpressure, and shutdown do not freeze the UI or leak workers.

### Test strategy

Use deterministic fake hosts and synthetic data for protocol, state, UI, API, lifecycle, and fault tests. Gate real zot compatibility tests explicitly, pin supported versions, and avoid dependence on live providers or installed agent CLIs in the default suite.

Test capability negotiation, malformed and oversized frames, partial reads, response correlation, permissions, timeouts, connection generations, snapshots, watch gaps, idempotency, and interrupted operations.

Test snapshot/restore pairs, legacy files, corrupt bindings, narrow-window panels, focus restoration, visual stacking, and terminal input pass-through.

Before declaring implementation complete, format changed Go files and run focused tests, `go test ./...`, `go vet ./...`, and `go test -race ./...`. Review the full diff and `git diff --check`. Run platform CI on macOS, Linux, and Windows.

## 20. Boundaries and release gates

Not included: automatic migration of ordinary transcripts, durable execution for non-zot agents, transparent remote filesystem synchronization, a second agent runtime, arbitrary tool replay, automatic approvals, store deletion through close actions, or rewriting zot's chat UI.

Release gates include exact arbitrary-conversation attachment in zot, stable host identity or a safe explicit rebinding flow, demonstrated platform-correct managed process survival, defined credential storage, pinned protocol compatibility tests, and measured binary/dependency impact.

The feature is complete when ordinary hrdx remains as useful as today, durable agents are native rather than plugin-driven, and users can create, observe, control, close, reopen, and recover durable work without confusing terminal lifetime with execution lifetime.
