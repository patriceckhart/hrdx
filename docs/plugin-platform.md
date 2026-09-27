# Experimental plugin platform

Status: experimental implementation of the external-peer boundary discussed in [discussion 23](https://github.com/patriceckhart/hrdx/discussions/23). The manifest and protocol are independently versioned. There is no zot wire-compatibility promise.

## Implemented surfaces

- Read-only manifest discovery and diagnostics.
- Explicit, provenance-bound execution approval and separate capability grants.
- Lazy stdio subprocess activation, handshake, concurrent requests, cancellation, failure isolation, stop, and restart.
- Contextual actions, bounded notifications and footer status.
- Scoped workspace/pane queries and ordinary host-owned pane operations.
- Best-effort scoped snapshot subscriptions.
- Quota-limited namespaced private storage with cross-process locking.
- Separately gated floating and docked plain-text views with normalized content-area input.
- Bounded, ordered pane input injection with the separate `pane.send_input` grant.
- Temporary plugin-owned floating panes that follow the connection lifetime.
- Pull-based search providers merged into the host finder.
- Declarative workspace activation markers and a flat typed configuration schema.
- Host lifecycle menus, a settings section, CLI management, and additive control-socket methods.
- A standard-library-only reference peer in `examples/plugins/hello`.

Outside the implemented runtime: per-tab/per-pane grant scopes (workspace or instance only), automatic reload without explicit reapproval, automatic restart/backoff, durable event replay, and OS sandboxing.

## Trust boundary

Plugins are trusted executable code with the user's OS permissions, inherited environment, and package directory as cwd. They are not sandboxed. Capabilities restrict host-mediated protocol operations, not a same-user program's own file access, process creation, network access, or access to the existing control and holder sockets.

Neither manifests nor wire messages can grant permission or enable feature flags. A valid inventory entry is not execution approval. Normal hrdx startup does not scan or run plugins. Execution requires both a saved enabled approval and `--plugins`. Floating views additionally require `--plugin-views`.

All host UI mutations run through the Bubble Tea update loop. A request carries a host-generated connection generation, cancellation context, and buffered reply channel. Grants and target workspace scope are checked immediately before dispatch. Plugin code never receives live Go model objects, PTY handles, holder frames, or state-file JSON.

Raw peer stderr is drained and discarded. Protocol payloads, terminal contents, environment values, argv, and plugin-private data are not logged. Diagnostics contain fixed messages, plugin IDs, and lifecycle stages. Inventory and approval inspection deliberately show package/workspace paths, which may be sensitive.

## Discovery

`hrdx plugins list` and `hrdx plugins doctor` accept `--json`, `--state PATH`, and repeatable `--dir PATH`. These commands never execute packages, start the TUI or holder, or read workspace state. Doctor also validates enabled approval records and package fingerprints.

The default root is `plugins/` next to the selected state file. `--state=""` omits it for inventory. Each explicit root contains immediate package directories. There is no recursive package discovery, automatic repository-local scan, PATH probing, installer hook, or download.

Roots are inspected in caller order after the default root. Filesystem identity deduplicates repeated roots, without assuming case sensitivity. Package directories are sorted lexically. Ordering gives no precedence: duplicate IDs invalidate every claimant. An approved package is selected by its exact canonical path, never by project-over-global shadowing.

A missing default root is harmless. Missing explicit roots, inaccessible directories, duplicate IDs, invalid manifests, and unsupported versions produce diagnostics. Inventory limits are 16 supplied roots and 256 entries per root. Oversized roots are rejected as a whole. Ordinary root files are ignored. Symlink entries are not package directories, and `plugin.json` must be a regular file.

Inventory accepts package-contained relative entrypoint symlinks, but execution approval is deliberately stricter and rejects all package symlinks. Entrypoints must be regular package-relative files, with executable permission bits on Unix or `.exe`/`.com` extensions on native Windows. Validation cannot certify executable format, effective ACLs, interpreter availability, or runtime dependencies without executing code.

List returns zero after writing its inventory even when problems exist. Doctor returns one for any diagnostic, zero for a clean or empty inventory. Invalid command usage returns two. Both return one on output failure. JSON output has `packages` and `diagnostics` arrays, with fixed-code messages. It omits argv and full manifest payloads.

## Manifest schema 1

```json
{
  "schema": 1,
  "id": "example.hello",
  "version": "1.0.0",
  "name": "Hello plugin",
  "entrypoint": "hello.exe",
  "protocol": {"min": 1, "max": 1},
  "requests": ["ui.action.contribute", "ui.notification", "ui.view.contribute", "ui.view.input"],
  "contributes": {
    "actions": [
      {"id": "example.hello.greet", "label": "Hello from plugin", "targets": ["workspace", "pane"]}
    ],
    "views": ["example.hello.panel"]
  }
}
```

| Field | Rules |
|---|---|
| `schema` | Required, currently `1` |
| `id` | Required, at most 128 ASCII bytes, at least two dot-separated segments matching `[a-z][a-z0-9_-]*` |
| `version` | Required full semantic version without a `v` prefix, at most 128 bytes |
| `name` | Optional single-line display name, at most 80 Unicode characters |
| `description` | Optional single-line description, at most 512 Unicode characters |
| `entrypoint` | Required package-relative path, at most 1024 bytes, using `/` separators |
| `args` | Optional array of at most 64 arguments, each at most 4096 bytes without NUL |
| `protocol` | Positive integer `min` and `max`, ordered and including protocol `1` |
| `requests` | At most 64 unique dotted capability names |
| `contributes.actions` | At most 32 contextual actions |
| `contributes.views` | At most eight declared view IDs |
| `contributes.providers` | At most eight search providers, each `{id, kind: "search", label}`, requires `ui.provider.contribute` |
| `activation.markers` | At most 16 unique single-component workspace-relative names such as `.git` or `package.json` |
| `config` | At most 32 flat options `{key, type, default, label}` with type `bool`, `string`, or `int` |

Entrypoints cannot be absolute, traverse upward, contain `.` components, backslashes, Windows-special filename characters, reserved device names, trailing dots/spaces, or control/format characters. There is no shell, environment, tilde, or PATH expansion. Unix shebang executables are possible with executable permission and a usable interpreter. Windows batch files and MSYS-only paths are not native executable entrypoints. WSL, Git Bash, and native Windows remain distinct environments.

Plugin IDs must also be portable filenames because they name approval records and storage directories. Windows device-name prefixes such as `con.tools` and `com1.tools` are rejected on every platform. Ordinary reverse-domain IDs such as `com.example` are valid.

Action and view IDs must be unique within their contribution kind and begin with the plugin ID followed by a dot. The entire ID is at most 128 bytes. Action labels contain 1 to 80 nonblank Unicode characters. Targets are unique values from `workspace`, `tab`, and `pane`. Declaring actions requires requesting `ui.action.contribute`, and declaring views requires requesting `ui.view.contribute`.

A manifest is at most 64 KiB and one UTF-8 JSON object. Duplicate keys, including case-folded aliases, multiple JSON values, and nesting deeper than 32 are rejected. Known fields use Go JSON semantics, including case-insensitive field matching and null as a zero value. Use the documented lowercase spelling and omit unused optional fields. Unknown optional fields are ignored. Required extensions must use a new schema version, not an ignored field. Unknown capability requests confer no rights and cannot be approved.

### Activation and configuration

Activation markers gate contextual actions and providers per workspace. A plugin with markers is offered only in workspaces where at least one marker exists as a direct child entry. Each check is one `Lstat`, never a recursive scan, executable probe, or symlink evaluation. A plugin without markers applies to every in-scope workspace. Opening a repository never approves or starts a plugin.

Configuration options are flat, typed, and declared by the manifest. Approved values are stored in the approval record and merged with manifest defaults into the `config` object of `hello_ack`, so peers receive their settings before `ready`. Values must match the declared type. Configuration lives in ordinary user-readable state: do not declare secrets as options. Plugins needing secrets must obtain them through their own means.

## Approval, persistence, and revocation

Approve an explicit directory with `hrdx plugins approve --package PATH --trust`. Add one `--grant CAPABILITY` for each requested supported capability you want to permit. Add `--set key=value` for manifest-declared configuration; bool and int values are JSON literals. Workspace operations additionally require repeatable `--workspace PATH` or explicit `--instance`, but not both. Workspace scopes are exact absolute paths as stored by hrdx, not recursive prefixes or filesystem aliases. Use the spelling shown in the control API's workspace status, including case. Symlinks are preserved in workspace scope paths to match existing CWD and holder-session behavior. No workspace scope means no workspace data or mutation authority.

Approval records live under `plugin-approvals/<plugin-id>.json` next to the state file. They contain schema, ID, canonical package path, digest, enabled state, grants, workspace roots, and instance-scope choice. They are separate from workspace snapshots so the TUI cannot overwrite concurrent CLI approval changes. Writes use private temporary files and replacement. Existing `state.json` files are unchanged.

The digest covers regular package files, relative names, permission bits, and contents, including the manifest, executable, scripts, and assets. Runtime approval limits the package to 128 MiB, 1024 entries including directories, and 32 directory levels, with no symlinks. Any changed package needs explicit reapproval. Keep mutable data outside the package. The digest does not bind system interpreters, dynamically imported external dependencies, or inherited environment. It is not a signature, sandbox, or defense against adversarial same-user filesystem races.

Enabled records are loaded at TUI startup only when `--plugins` is present. The runtime rechecks package validation and fingerprint before every launch. A watcher compares saved approvals every 500 ms. A missing, disabled, unreadable, or changed record revokes the live registration, cancels pending access, removes contributions, and stops the process. Filesystem latency can extend that interval. An in-progress operation cannot necessarily be undone.

Additional grants and new approvals are never silently loaded into an existing process. Restart hrdx to load them. `plugins enable` requires the package to still match its saved approval. `plugins inspect` prints the approval. `plugins disable` persists disabled state and is observed by running instances even when their control socket is disabled. `plugins revoke` removes the approval record entirely and triggers the same live revocation, while retaining separately stored private plugin data.

## Lifecycle and process ownership

The runtime uses `approved`, `starting`, `handshaking`, `ready`, `stopping`, `stopped`, `failed`, and `blocked` states. Processes activate lazily on action invocation or explicit start. Failed, blocked, and stopped plugins require explicit restart. There are no automatic restart loops. Development reload is explicit: reapprove changed files at the same package path, then use `plugins reload --id ID` or the lifecycle menu. Reload rejects grant or scope changes, stops and reaps the old generation, revalidates current provenance, and starts a new generation. Approval changes still require a full hrdx restart.

`ctrl+b P` opens lifecycle controls. Workspace context menus expose start/restart and stop. The settings window (`ctrl+b ,`) gains a `plugins` section when the runtime is enabled: it shows each approved plugin's state, offers restart and stop, and can disable a plugin by writing the same approval record the CLI uses. Re-enabling is CLI-only because it revalidates the package. The trusted control socket adds:

```json
{"id":"1","method":"plugins.status"}
{"id":"2","method":"plugins.control","params":{"plugin":"example.hello","action":"restart"}}
```

The corresponding CLI commands are `hrdx plugins status` and `hrdx plugins start|stop|restart --id ID`, with optional `--state PATH`. They require the instance's control API. Control replies acknowledge acceptance, not readiness. Status reports package path, display name, plugin version, selected protocol, requested and granted capabilities, workspace or instance scope, declared action/view IDs, lifecycle state, connection generation, and a sanitized failure when present. Package paths and workspace scopes are potentially sensitive, so this detail remains on the existing trusted control interface. These methods cannot create approvals or grants and are not available through the restricted peer protocol.

Plugin processes belong to the TUI, never the holder. Shutdown asks the peer to stop, waits up to 500 ms, then cleans up its managed process group on Unix or job object on Windows. Windows starts the child suspended, assigns a kill-on-close job, and resumes only after successful assignment. Unix uses a separate process group. A trusted Unix executable can deliberately escape its process group, which is another reason this is not sandboxing. Peers must exit on stdin EOF. On Unix, uncatchable termination of hrdx prevents its cleanup code from running, so a peer that ignores EOF can outlive a crashed host. Windows job handles provide stronger parent-exit cleanup.

Split and tab panes created by a plugin become host-owned and survive plugin disconnect. They use existing PTY, split-tree, and holder persistence paths. Floating panes created by a plugin are temporary resources owned by that connection generation: they are excluded from persisted state, never leave a detached holder session, and are closed through the normal pane cleanup path when the owning generation stops, fails, reloads, or the TUI quits. Explicit pane/workspace close still uses normal model cleanup.

## Protocol 1

Every frame is one UTF-8 JSON object terminated by LF. CRLF is accepted. A partial final line at EOF is rejected. Stdout is protocol-only. Limits are 256 KiB per frame excluding LF, nesting depth 32, IDs up to 128 bytes without control/format characters, 32 concurrent requests per direction, 32 queued output frames, and 256 incoming frames per second. The host event bridge is bounded to 64 entries. Backpressure fails calls or disconnects the peer rather than blocking the update loop.

The peer starts with:

```json
{"type":"hello","plugin":"example.hello","protocol":1}
```

The host validates identity and replies:

```json
{"type":"hello_ack","protocol":1,"instance":"host-generated-generation","grants":["ui.action.contribute","ui.notification"]}
```

The peer then sends `{"type":"ready"}`. The total handshake deadline is five seconds. Contributions are declared in the manifest rather than dynamically registered during handshake. Unknown or out-of-order runtime frames fail the session.

Bidirectional calls use:

```json
{"type":"request","id":"1","method":"ui.notify","params":{"text":"Completed"}}
{"type":"response","id":"1","result":{"ok":true}}
{"type":"response","id":"2","error":{"code":"denied","message":"operation or scope is not granted"}}
{"type":"cancel","id":"3"}
```

IDs are directional and must be unique among outstanding calls in that direction. Host calls and peer calls have separate pending maps. Calls have a ten-second host deadline. Cancellation is best effort and does not roll back completed side effects. Do not automatically retry pane/workspace mutations after ambiguous timeouts. Late responses to completed calls are ignored, and UI command results are checked against their originating connection generation.

Resource IDs are connection/instance-local. Pane IDs remain valid only within the running TUI and must be discarded after a new handshake generation. Workspaces are addressed by exact paths, not display names. Tabs are represented in snapshots, not independently grantable durable IDs.

Shutdown is `{"type":"shutdown"}` followed by optional `{"type":"shutdown_ack"}` and process exit. Stop revokes protocol access before waiting for graceful exit.

## Host-to-peer actions

A declared menu action invokes:

```json
{"type":"request","id":"7","method":"command.invoke","params":{"action_id":"example.hello.greet","target":"pane"}}
```

The captured `workspace` path is included only with a matching workspace scope and `workspace.read`. `pane_id` requires matching scope and `pane.read_metadata`. Without those grants, the action receives only its ID and target kind. Context is a snapshot, not a live object. Later host operations re-resolve targets and scopes.

Action activation is asynchronous. The footer shows activation in progress. After the peer becomes ready, hrdx rechecks that the captured workspace, tab, or pane still exists and that the same connection generation remains authorized before sending `command.invoke`. Closed targets produce a host error and are never redirected to the newly focused resource or a replacement plugin process.

A command may return `{"notification":"Completed"}`. The UI displays it only with a current `ui.notification` grant and matching live generation. Results are not arbitrary executable host actions. Use explicit brokered methods for mutations.

### Providers

When the host finder (`ctrl+b /`) query has at least two characters, each ready plugin with `ui.provider.contribute`, a matching workspace scope, and matching activation receives:

```json
{"type":"request","id":"9","method":"provider.query","params":{"provider_id":"example.hello.search","query":"tests","limit":32}}
```

`workspace` is included with `workspace.read`. The peer answers `{"rows":[{"label":"...","detail":"...","id":"opaque","pane_id":0}]}` within two seconds. Rows are plain text up to 120 characters, at most 32 per provider, and are rendered by the host below the ordinary workspace/tab/pane matches. Answers for a query that has since changed, or from a stopped generation, are discarded. Selecting a row that names an in-scope `pane_id` jumps to it. Otherwise the host sends `provider.select` with the row `id`, and the result may carry a notification like a command.

## Peer-to-host methods

Every method below requires its separately named grant. Screen access is never implied by metadata grants. Pane input goes through an ordered per-pane queue drained by a background goroutine, so a stalled child never blocks the UI loop. Keyboard input is always accepted. Plugin and socket automation input is bounded: at most 16 KiB per call, and `busy` when the pane already has 256 KiB unread. Ordering between keyboard and automation input is preserved because they share one FIFO. Input is raw bytes for the child, not sanitized text, so a plugin with `pane.send_input` can run arbitrary commands in that pane. NUL bytes are rejected.

| Method | Grant | Parameters and behavior |
|---|---|---|
| `ui.notify` | `ui.notification` | `text`, up to 240 plain-text characters, optional `severity` of `info` or `error`, transient footer notice |
| `ui.status` | `ui.status.contribute` | `text`, up to 240 characters, optional `priority` 0 to 100, retained in the footer while connected, empty text clears |
| `status` | `workspace.read` | Scoped snapshot using the control API status shape |
| `pane.read` | `pane.read_screen` | `pane_id`, visible plain screen for an in-scope pane |
| `pane.create` | `pane.create` | Explicit workspace path, existing `kind`, and `split` of right, down, tab, or float. Floats need `width_pct`/`height_pct` (10 to 100), optional `anchor`, are limited to four, and are temporary |
| `pane.send_text` | `pane.send_input` | `pane_id`, `text` up to 16 KiB, optional `enter`, queued behind keyboard input, `busy` when the pane is stalled |
| `pane.close` | `pane.close` | `pane_id`, normal explicit pane cleanup |
| `workspace.create` | `workspace.create` | Absolute `path` in scope, optional existing `agent` kind, `group_path` array and opt-in `group_from_git` default |
| `workspace.move` | `workspace.move` | Exact in-scope workspace path in `workspace`, required `group_path` array (`[]` for standalone); no PTY or selection changes |
| `group.list` | `workspace.read` | Occupied group paths and ancestors from the scoped snapshot only |
| `workspace.close` | `workspace.close` | Exact workspace path in `workspace` |
| `events.subscribe` | `host.events.subscribe` and `workspace.read` | `events:["snapshot.changed"]` |
| `events.unsubscribe` | `host.events.subscribe` | Remove this connection's subscription |
| `storage.get` | `storage.plugin_private` | `key` |
| `storage.set` | `storage.plugin_private` | `key`, JSON `value` |
| `storage.delete` | `storage.plugin_private` | `key` |
| `ui.view.open` / `ui.view.update` | `ui.view.contribute` | Full view representation described below |
| `ui.view.close` | `ui.view.contribute` | Declared view `id` |

`status` omits unapproved workspaces. Pane entries require the additional `pane.read_metadata` grant. Process failure strings are omitted so argv or OS errors cannot leak through metadata. Oversized results fail with `too_large`. Notifications are limited to one per second per plugin regardless of severity, and error notices expire like host errors. Status retains the latest value for the normal renderer cadence, so a rapid final transition is not lost. Footer status entries are ordered by priority, then plugin ID, and clipped by display width with the rest of the footer. Transport limits bound status-update volume. Ordinary terminal keys are never redirected to plugins.

Errors include `denied`, `disabled`, `invalid_params`, `invalid_result`, `unknown_method`, `not_found`, `timeout`, `canceled`, `busy`, `unavailable`, `too_large`, `quota`, and `storage_error`. Process, handshake, and approval failures appear in lifecycle diagnostics. Peer-provided error details are not copied into the host footer.

## Snapshot subscriptions

Subscribe returns an initial scoped snapshot and sequence zero. At most once per 200 ms, the UI checks for a changed scoped snapshot and pushes:

```json
{"type":"event","method":"snapshot.changed","params":{"sequence":1,"snapshot":{"type":"status","version":"0.0.0","workspaces":[]}}}
```

Snapshots reflect workspace, group membership, tab, pane, selection, and busy-state changes, including keyboard operations. Workspace entries carry optional `group_path` metadata; group membership uses the [control API contract](../README.md#workspace-groups). Groups do not grant access to their other members. The host sidebar remains host-rendered; there is no sidebar template or replacement API. Intermediate states can be coalesced. No raw keyboard input, terminal bytes, screen content, or terminal titles are emitted. Scope and grants are checked at publication. Delivery never blocks rendering. Dropped attempts leave sequence gaps and are retried while the snapshot differs. Recover by querying `status`. Subscriptions disappear on stop, revocation, restart, or disconnect.

## Private storage

Storage lives under `plugin-data/<plugin-id>/values.json` beside the state file, not inside the package or `state.json`. Keys match `[a-zA-Z0-9_-]{1,64}`. Values are JSON, each at most 32 KiB. Quotas are 64 keys and 1 MiB total encoded data per plugin. A missing value returns null. Delete is idempotent.

Storage operations run outside the UI loop. An OS lock serializes independent instances, and private temporary files plus replacement prevent torn writes. Waiting for a lock honors request cancellation. Data remains after disable or package removal. Plugins own their value schemas and migrations. Namespacing prevents cross-plugin access through this API but does not isolate files from other same-user executables. Unix file modes are private, while Windows uses the configuration directory's inherited ACLs.

## Floating views

Views are optional, ephemeral, plain-text surfaces, not PTYs or split-tree leaves. At most eight may be open across the instance. Example:

```json
{"type":"request","id":"8","method":"ui.view.open","params":{"id":"example.hello.panel","title":"Example","lines":["Hello","A second row"],"width_pct":60,"height_pct":50,"focus":true,"dock":""}}
```

IDs must be manifest-declared and plugin-namespaced. Titles contain 1 to 80 plain-text characters. There are at most 128 lines of at most 512 characters each. Controls, ANSI, Unicode format characters, and embedded newlines are rejected. Blank/padded rows are allowed. Width and height percentages are from 10 to 100. The host centers and sizes the frame, clips by display width, owns borders/z-order, and returns content `width` and `height`. Updates replace the full representation.

`focus:true` is honored only with `ui.view.input` and only when no host modal, selection, or drag is active. Clicking an input-enabled view focuses it and raises it. Content input is a best-effort event:

```json
{"type":"event","method":"view.input","params":{"id":"example.hello.panel","key":"a","paste":false}}
```

Mouse events contain content-relative `x`, `y`, boolean `shift`/`alt`/`ctrl`, a `button` name (`none`, `left`, `middle`, `right`, `wheel_up`, `wheel_down`, `wheel_left`, `wheel_right`, `backward`, or `forward`), and an `action` of `press`, `release`, or `motion`. Keyboard events use normalized key strings, not raw terminal input encoding. `view.resize` reports new content dimensions. Escape closes the focused view. The normal host prefix opens host commands, and host modals always take precedence. Click a view again to return focus after leaving a host mode. Non-input views consume clicks over their frame without forwarding them to obscured terminals. The frame's `x` always closes it.

### Docked views

A view may set `dock` to `right` or `bottom` instead of floating. A docked view reserves a strip of the terminal area (at most 50 percent of its width or height, never shrinking terminals below their minimum size) and the split tree lays out the remaining area. The split tree itself is not modified: every persistent pane still appears exactly once in it, PTYs receive their new inner dimensions through the ordinary resize path, and persisted state is untouched. Multiple docks on the same edge stack outward in open order. A view cannot change between floating and docked in place; close it and reopen it. Docked views keep their strip order when focused instead of raising. Views close on disconnect/revocation and never enter saved workspace state or holder sessions. Arbitrary ANSI rendering is not supported.

## Budgets

With `--plugins` absent, no runtime, goroutine, channel, watcher, or socket method is created, so a fresh install has no plugin cost. With the runtime enabled, each ready plugin adds at most eight goroutines (reader, writer, waiter, watcher share, and bounded request workers), a 32-frame output queue, an 8-frame input buffer, and one shared 64-entry event bridge. A host-to-peer round trip on the stdio transport measures roughly 60 microseconds and 3.4 KiB of allocation on an Apple M1 Pro (`go test ./internal/plugin -run xxx -bench RoundTrip -benchmem`). Frame limits (256 KiB, 256 per second inbound), call limits (32 per direction, 10 s deadline, 2 s for providers), pane input limits (16 KiB per call, 256 KiB queued), and storage quotas (64 keys, 1 MiB) are fixed constants rather than adaptive budgets. Rendering never waits on a plugin: every UI mutation arrives as a Bubble Tea message and every publication is non-blocking.

## Resource identity

Workspaces are addressed by exact public path. Tabs are positional within snapshots and have no independent identity. Pane IDs are integers allocated by the running TUI, stable for that process lifetime including rename and reorder, and invalid after a TUI restart or a new plugin generation. Plugin views are addressed by declared ID within one connection. Temporary panes and views never outlive their connection. Grants are scoped to workspaces or the instance; there is no per-tab or per-pane grant, so a plugin allowed in a workspace may act on every pane in it that the named operation permits.

## Compatibility matrix

| Host version | Manifest schema | Wire protocol | Approval schema |
|---|---|---|---|
| this checkout | 1 | 1 | 1 |

The full Go suite with the race detector has been executed natively on macOS arm64 and, through a `golang:1.25` container, on Linux amd64. Windows is cross-compiled and vetted only; CI runs the native suite.

Manifests and peers declare the protocol range they support. Only protocol 1 exists. Unknown optional manifest fields are ignored, and unknown capability requests are inert. A future incompatible change increments the schema or protocol and older records fail closed with a diagnostic rather than a silent downgrade. No zot compatibility is claimed or tested.

## Ownership and compatibility

`internal/plugin` owns discovery, approvals, transport, supervision, and private storage. `internal/state` owns serializable approval records. `internal/ui` owns contribution state, rendering, target resolution, subscriptions, and host mutations. `cmd/hrdx` assembles flags and lifetime wiring. `internal/api` keeps its trusted control protocol, adding only explicit lifecycle methods.

Existing state files, harness registration, control methods, PTY dimensions, split trees, and holder persistence remain unchanged. No new dependency is required. All runtime flags default off. Tests use temporary synthetic packages, Go helper processes, bounded deadlines, and fixture manifests rather than installed agents or external services.
