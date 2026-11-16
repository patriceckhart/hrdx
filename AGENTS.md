# Working Agreement for hrdx

This file defines how automated coding assistants should work in this repository. Treat it as a practical operating manual, not a substitute for reading the code.

## Product intent

hrdx is an experimental, minimal, lightweight terminal multiplexer for coding agents and shells. Changes should preserve its defining properties:

- one portable Go binary
- every pane backed by a real PTY
- projects represented as workspaces with tabs and split layouts
- terminal behavior compatible with ordinary agent TUIs and shells
- persistent sessions through a lightweight holder process
- predictable behavior on macOS, Linux, and Windows
- a small dependency and operational footprint

Prefer a narrow, explicit implementation over a generalized subsystem. New abstractions must earn their cost through a real ownership boundary or repeated use.

## Starting a task

Build context before editing:

1. Inspect `git status --short --branch`. Existing modifications may belong to the user or another agent.
2. Locate and read every `AGENTS.md` that governs the target path.
3. Read the owning implementation, nearby tests, and user-facing documentation for the behavior.
4. Reproduce reported failures when feasible. Keep expected behavior distinct from observed behavior.
5. For GitHub work, inspect the issue, discussion, or pull request before implementing it. Do not change branches merely to review a pull request.

Do not use one search result or one function as a complete model of a feature. Follow the path through UI state, terminal or holder behavior, persistence, API representations, tests, and platform-specific files where relevant.

## Code ownership map

Put behavior in the package that owns the concern:

| Area | Responsibility |
|---|---|
| `cmd/hrdx` | CLI flags, configuration assembly, startup, process modes, and platform wiring |
| `internal/api` | Socket server, newline-delimited JSON protocol, public request and response types, subscriptions |
| `internal/holder` | Persistent PTY ownership, attach and detach, replay ring, holder protocol |
| `internal/state` | Serializable workspaces, tabs, panes, layouts, and preferences |
| `internal/term` | PTY pane lifecycle, input encoding, resize, scrollback, selection, terminal-facing helpers |
| `internal/ui` | Bubble Tea model, rendering, layout, input, menus, settings, persistence coordination, API handling |
| `internal/vt` | Escape parsing, terminal state, glyphs, colors, history, and screen model |
| `internal/update` | Update checks and self-update |
| `internal/winproc` | Windows foreground-process and process-tree behavior |
| `internal/agent` | Agent registry, `harness.json` harnesses, binary resolution, launch arguments, busy and attention classification |
| `internal/layout` | Split tree operations, completeness validation, fallback layout, conversion to and from `state.Node` |
| `internal/paths` | Socket path selection, default shell, directory resolution and completion |
| `internal/theme` | Theme files and built-in presets as plain color strings |
| `internal/sound` | Notification sounds, `sounds.json`, playback |
| `internal/keymap` | Prefix actions, defaults, and `keys.json` overrides |
| `internal/gitinfo` | Branch, ahead and behind counts, and default git group labels |
| `desktop/` | hrdx Desktop, a nested Go module (Wails v2 and a static Next.js bundle) sharing the packages above |

The API server must not mutate UI state directly. Terminal escape parsing must not leak into UI layout code. Persisted structures belong in `internal/state`, while conversion between live and saved state belongs in `internal/ui/persist.go` for the TUI and `desktop/internal/core/persist.go` for the desktop app.

Logic both frontends need lives in the shared packages, with thin wrappers in `internal/ui`. Do not reimplement it in `desktop/`. The desktop core loop in `desktop/internal/core` owns its live state exactly as the Bubble Tea loop owns the TUI's, and answers the socket API with the same result shapes. `desktop/internal/core/testdata/api_parity.json` is recorded from the TUI (see `internal/ui/api_parity_test.go`) and must be re-recorded when TUI answers change.

## Correctness contracts

### UI state and API ordering

- Bubble Tea's update loop owns live UI state. Keep mutations on that loop.
- Socket requests must round-trip through `api.Request` and a buffered reply channel.
- API and keyboard actions should use the same model operations and preserve the same invariants.
- Event publication must not block rendering. Best-effort subscribers may drop events when slow.
- Modal input modes must consume only their own keys. Terminal mode should continue forwarding ordinary input to the focused PTY.

### Layout and rendering

- Every persistent pane in a tab must appear exactly once in its split tree.
- Removing a split leaf must promote its sibling without corrupting ratios.
- Floating or otherwise ephemeral panes must not enter persisted split trees unless the contract explicitly changes.
- PTYs receive the content dimensions inside pane borders, not the outer rectangle.
- ANSI-styled rows must be clipped and padded by display width. Byte length and rune count are insufficient.
- Account for wide glyphs, combining characters, cursor visibility, narrow windows, and stale PTY sizes.
- Overlays must preserve ANSI reset behavior and mouse hit testing must follow visual stacking order.

### PTYs and holder sessions

- A directly owned PTY dies with its pane or TUI. A holder-backed persistent PTY detaches when the TUI quits and reattaches on restart.
- Explicit pane, tab, or workspace close must clean up the corresponding process and model references.
- Ephemeral panes must not leave detached holder sessions behind.
- Resize, output replay, process exit, attach failure, holder restart, and stale session cleanup are separate paths and should remain testable.
- Avoid goroutine leaks and blocking sends in process and socket paths.

### Terminal emulation and input

- Preserve valid VT state across partial escape sequences and arbitrary feed boundaries.
- Input encoding must respect application cursor mode, bracketed paste, mouse capture, and kitty keyboard behavior.
- Escape and control keys are application input in terminal mode. Do not reserve them globally without an explicit mode or prefix contract.
- Selection, scrollback, alternate-screen behavior, and child mouse capture should match normal terminal expectations.

### Persistence

- Keep existing state files loadable unless migration is explicitly part of the task.
- Additive JSON fields should be optional and have safe zero-value behavior.
- Validate restored indexes and split trees. Fall back safely when saved layout data is incomplete or corrupt.
- Test snapshot and restore together when changing workspaces, tabs, panes, layouts, settings, or holder session references.
- Do not persist process-local registrations or ephemeral UI state unless the public contract requires it.

### Cross-platform behavior

- CI targets Linux, macOS, and Windows.
- Inspect build-tagged counterparts when changing PTYs, process handling, foreground detection, detach behavior, shells, paths, clipboard access, sockets, signals, or updates.
- Do not assume `/bin/sh`, Unix signals, Unix path syntax, or a shared socket namespace.
- Keep Windows ConPTY and native-shell behavior distinct from WSL and Git Bash behavior.

### Private data

- Treat terminal contents, shell history, workspace paths, state files, custom harness arguments, and socket payloads as potentially sensitive.
- Never add real credentials, tokens, private transcripts, or personal state to fixtures, logs, errors, or commits.
- Use synthetic values and temporary directories in tests.

## Implementation approach

For fixes, locate the earliest broken invariant and test it there. A visual symptom may originate in VT parsing, PTY sizing, holder replay, input encoding, or restored state. Repair the owning layer rather than masking the result in rendering.

For features, identify all public surfaces before coding: keys, mouse behavior, context menus, settings, socket methods and events, status output, persisted state, holder lifecycle, platform variants, and README documentation. Implement only the surfaces needed for a complete first version.

While editing:

- Preserve unrelated working-tree changes.
- Use idiomatic Go and standard-library facilities when sufficient.
- Keep functions and interfaces focused.
- Avoid introducing an interface solely to mock one call unless it improves the production boundary.
- Return errors with enough context to identify the operation without exposing private terminal content.
- Avoid time-based synchronization in tests. Prefer channels, explicit hooks, or polling with a deadline.
- Use `t.TempDir()` and `t.Setenv()` for isolated tests.
- Restore mutated package globals with `t.Cleanup()`.
- Do not perform opportunistic refactors outside the requested change.

If requirements conflict with terminal pass-through, process persistence, state compatibility, or cross-platform behavior, ask before choosing silently.

## Testing guidance

Tests should be deterministic and independent of installed agent CLIs, real shells where avoidable, external network services, global hrdx state, and terminal capabilities.

Useful ownership examples:

- protocol decoding and routing tests in `internal/api`
- holder attach, replay, ring, and process tests in `internal/holder`
- JSON load and save tests in `internal/state`
- PTY-independent rendering and input tests in `internal/term` and `internal/vt`
- model, layout, mouse, key, API integration, persistence, settings, and theme tests in `internal/ui`
- startup and flag tests in `cmd/hrdx`

When changing a shared contract, test both success and failure paths. Include missing, malformed, boundary, cleanup, and compatibility cases where applicable.

## Validation ladder

Match validation effort to the change, then finish with repository-wide checks when Go code changed.

1. Add or update a focused test in the owning package.
2. During iteration, run the smallest relevant test, for example:

   ```sh
   go test ./internal/ui -run TestName
   ```

3. Format every changed Go file with `gofmt`.
4. Run the complete suite:

   ```sh
   go test ./...
   ```

5. Run the race detector for concurrency, process, holder, socket, or broad runtime changes:

   ```sh
   go test -race ./...
   ```

6. Run `go vet ./...` for Go changes.
7. Before reporting completion, inspect:

   ```sh
   git diff --check
   git status --short --branch
   git diff
   ```

CI runs vet, a gofmt check, and the race-enabled suite on Linux, macOS, and Windows.

The desktop app is a nested module that root `go vet ./...` and `go test ./...` skip. When `desktop/` or any shared `internal/` package changes, also run its ladder (CI runs it in the `desktop` job):

```sh
cd desktop
go vet ./...
go test -race ./...
cd frontend
bun install --frozen-lockfile
bun run typecheck
bun run lint
bun test
bun run build
```

On Linux add `-tags webkit2_41` to the Go commands. Never start the Next.js dev server in automation.

`make check` formats every Go file before running vet and tests. Do not use it blindly when unrelated Go changes are present, because formatting them would modify user-owned work.

Do not hide failures, weaken assertions, or claim checks that were not run. If a check cannot run, state the command and exact reason.

Documentation-only changes do not require the Go suite unless they alter executable examples. They still require diff and status review.

## Documentation duties

Update `README.md` in the same change whenever users or socket clients observe different behavior. It owns installation, flags, keys, mouse behavior, remote panes, custom harnesses, socket API, themes, notifications, persistence, and development commands.

Keep JSON examples synchronized with `internal/api` types. State process lifetime, persistence, event delivery, platform support, and terminal-input limitations directly.

`CONTRIBUTING.md` owns contributor-facing workflow. This file owns automated-tool behavior and architecture contracts.

## Dependency policy

hrdx's single-binary design and small footprint are product requirements.

- Prefer the standard library or existing dependencies.
- Explain why a new dependency is necessary before adding it.
- Review transitive dependencies, binary-size effects, and platform support.
- Keep `go.mod` and `go.sum` changes limited to the requested work.
- Do not add a second terminal emulator, process supervisor, or UI framework for behavior the current packages can support.
- Desktop work must not add dependencies to the root `go.mod`. Wails and frontend packages belong to `desktop/go.mod` and `desktop/frontend/package.json` only. New frontend runtime dependencies beyond `next`, `react`, `react-dom`, and the `@xterm` packages need a stated reason.

## Source-control and release safety

The working directory may contain changes from other people or agents.

Allowed inspection commands include `git status`, `git diff`, `git log`, `git show`, and `git blame`. Avoid destructive or broad commands such as `git reset --hard`, `git clean`, `git checkout .`, and blanket staging.

Do not commit, create or switch branches, rebase, push, tag, publish a release, or open a pull request unless the user explicitly requests that operation. Permission to edit files is not permission to commit them.

When a commit is requested:

- include only files changed for the current task
- stage paths explicitly
- review the staged diff before committing
- use a concise, imperative subject
- never bypass hooks without explicit approval

Every push to `main` that passes CI normally creates and pushes a patch tag, then publishes a release with the CLI archives and the desktop app builds (`.dmg`, Windows `.zip`, Linux `.tar.gz`). Treat a push to `main` as a release operation. Do not add `[release=skip]` unless the user or maintainer explicitly requests a release skip.

When a pull request is requested, run the full validation suite first, summarize compatibility and user-visible effects, and mention failed or skipped checks.

## Completion report

Keep the final response brief and factual. Include:

- what changed and where
- which checks ran and their results
- any unresolved risk, skipped validation, or follow-up

Do not report success while tests are failing. Do not include unrelated cleanup suggestions unless they materially affect the requested work.
