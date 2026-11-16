# Contributing to hrdx

Contributions are welcome, including bug reports, design feedback, documentation, tests, and code. This guide explains how to make a contribution easy to evaluate and safe to merge.

## Choose the right starting point

### Reporting a defect

Search existing issues first, then include enough information for someone else to reproduce the problem:

- the hrdx version
- operating system and architecture
- terminal application and shell
- agent or custom harness involved, when relevant
- the smallest sequence of actions that triggers the problem
- expected and actual behavior
- sanitized logs, screenshots, or state excerpts when useful

Never post credentials, private agent conversations, shell history, access tokens, or an unredacted state file. For terminal problems, mention whether the behavior involves resize, mouse input, clipboard access, Unicode, persistence, or a particular full-screen program.

### Suggesting a capability

Start with a discussion or feature request and describe the workflow that is difficult today before proposing an interface. Useful proposals explain:

- who encounters the problem
- why existing keys, socket methods, harnesses, or configuration do not solve it
- how the behavior should appear in the TUI or socket API
- persistence and compatibility expectations
- platform-specific concerns
- reasonable alternatives

Wait for design feedback before implementing changes that alter the socket API, persisted state, terminal emulation, holder protocol, key behavior, or several packages.

### Small corrections

Focused typo, documentation, and test improvements can usually go directly to a pull request. Keep them separate from behavioral changes.

## Design boundaries

hrdx is intended to remain an experimental, minimal, lightweight terminal multiplexer distributed as one portable binary. Changes should preserve:

- real PTYs rather than agent-specific wrappers
- a compact dependency and operational footprint
- predictable behavior on macOS, Linux, and Windows
- a single-threaded Bubble Tea model for UI state
- backward-compatible persisted state and socket contracts
- clean separation between terminal emulation, process ownership, and UI rendering

The major ownership boundaries are:

- `cmd/hrdx`: command-line parsing, startup wiring, process modes, and platform launch behavior
- `internal/api`: unix-socket protocol, request decoding, replies, subscriptions, and public API types
- `internal/holder`: long-lived PTY session ownership, attach and detach behavior, replay buffering, and holder protocol
- `internal/state`: serialized workspace, tab, pane, split, and preference state
- `internal/term`: PTY lifecycle, terminal input encoding, scrollback, selection, and the UI-facing terminal pane
- `internal/ui`: Bubble Tea model, workspace and split behavior, rendering, input, menus, settings, persistence coordination, and API request handling
- `internal/vt`: terminal escape parsing and emulator state
- `internal/update`: update discovery and self-update behavior
- `internal/winproc`: Windows process-tree and foreground-process support
- `internal/agent`, `internal/layout`, `internal/paths`, `internal/theme`, `internal/sound`, `internal/keymap`, `internal/gitinfo`: logic shared by the TUI and the desktop app
- `desktop/`: hrdx Desktop, a nested Go module with its own `go.mod`

Keep socket framing out of UI code and UI mutation out of the socket server. Keep ANSI parsing in `internal/vt`, PTY mechanics in `internal/term` or `internal/holder`, and persisted representations in `internal/state`.

Prefer the standard library and existing dependencies. New dependencies need a clear benefit, especially when they affect binary size, terminal behavior, or cross-platform builds.

## Preparing a development checkout

Use the Go version declared in `go.mod`, then build from the repository root:

```sh
go mod download
make build
./hrdx --version
```

Useful commands:

```sh
make build       # compile ./hrdx
make run         # run hrdx from source
make test        # run go test ./...
make check       # format Go files, run go vet, then run tests
make install     # install cmd/hrdx with go install
```

### Desktop app

`desktop/` builds hrdx Desktop and needs more tools than the CLI:

- Go 1.26 or newer (Wails dependencies require it, the root module does not)
- [bun](https://bun.sh)
- the Wails v2 CLI: `go install github.com/wailsapp/wails/v2/cmd/wails@v2.15.0`
- the platform webview: WebKit on macOS 12+, WebView2 on Windows 10+ (bundled with Windows 11), and on Linux `libgtk-3-dev` and `libwebkit2gtk-4.1-dev`

```sh
cd desktop
make check       # go vet, go test -race, frontend typecheck, lint, tests, build
make build       # app bundle in build/bin
```

On Linux, pass `-tags webkit2_41` to Go commands in `desktop/`. Do not run the Next.js dev server in automated checks.

Tests must not depend on installed coding agents, real credentials, a developer's hrdx state, a live network service, or a particular terminal. Use temporary directories, local sockets, fixtures, synthetic terminal data, and fake process boundaries where practical.

## Making a change

Keep each change focused. Avoid bundling unrelated refactors, broad formatting churn, generated files, or cleanup with a behavioral contribution.

For a bug fix:

1. Reproduce the failure with a focused test when practical.
2. Find the earliest package where an invariant breaks.
3. Fix that layer rather than compensating in rendering or another downstream consumer.
4. Exercise neighboring cases that share the parser, layout, process, persistence, or event path.

For new behavior:

1. Identify every user-visible and integration surface involved.
2. Preserve existing state files, holder sessions, key mappings, and socket clients unless a breaking change has been agreed upon.
3. Add tests in the package that owns the behavior.
4. Update `README.md` when users or API clients can observe the change.

Follow ordinary Go conventions. Format changed Go files with `gofmt`, keep errors operationally useful, and avoid exposing terminal contents or private paths unnecessarily.

The repository's `AGENTS.md` contains additional implementation guidance for automated tools and also serves as an architecture reference for human contributors.

## Areas requiring additional checks

### Terminal emulation and rendering

Terminal changes must account for ANSI state, Unicode display width, wide and combining characters, wrapping, alternate screens, cursor visibility, scrollback, mouse protocols, and narrow windows. Prefer deterministic emulator and rendering tests over snapshots tied to one terminal application.

Rendering code composes ANSI-styled fragments by display width, not byte or rune count. Preserve clipping and reset behavior so one pane cannot visually corrupt adjacent panes.

### PTYs and session persistence

A pane may be owned directly by hrdx or by the session holder. Test startup, attach, detach, replay, resize, process exit, close, and cleanup paths when changing lifecycle code. Quitting the TUI must not kill persistent panes, while explicitly closing a pane must not leak its process.

Changes to the holder protocol or ring buffer should cover partial reads, disconnects, slow clients, exited sessions, and concurrent events. Avoid timing-only synchronization in tests.

### State compatibility

Existing state files should continue to load. New JSON fields should normally be optional, and missing or invalid data should have a safe fallback. Check snapshot and restore together, including legacy fields, selected indexes, split-tree completeness, and holder session IDs.

### Socket API

The socket API is consumed outside this repository. Prefer additive fields and methods. Validate parameters at the model boundary, preserve request IDs and established error codes, and keep newline-delimited JSON free of human diagnostics.

The server must not mutate UI state directly. Requests are forwarded into the Bubble Tea update loop so keyboard and API operations share the same ordering and invariants. Subscription delivery is intentionally best effort and must not block the UI.

### Operating systems

CI runs on Linux, macOS, and Windows. Process handling, PTYs, signals, shells, filesystem paths, clipboard access, foreground-process detection, socket behavior, and self-update logic can be platform-specific. Inspect every build-tagged counterpart before changing one platform implementation.

Do not assume a Unix shell or path. Windows Git Bash, native Windows terminals, WSL, and ConPTY have distinct behavior described in the README.

## Verification

Run focused package tests while developing. Before opening a pull request that changes Go code, run:

```sh
gofmt -w <changed-go-files>
go vet ./...
go test ./...
go test -race ./...
git diff --check
```

CI performs `go vet`, a gofmt check, and `go test -race ./...` on Linux, macOS, and Windows.

`make check` formats all Go files before running vet and tests. Review its resulting diff rather than using it when unrelated Go changes are present.

Documentation-only pull requests do not need the Go suite unless they alter executable examples. They should still be checked for accurate commands, links, schemas, and formatting.

If a command cannot pass because of an unrelated repository or environment problem, include the exact command and failure in the pull request. Do not silently omit it.

## Pull request shape

Open the pull request against `main` and include:

- the problem being addressed
- the chosen solution and why it fits hrdx
- tests added or updated
- verification commands run
- user-visible, compatibility, persistence, and platform effects
- a linked issue or discussion when one exists

Keep commits understandable and use concise, imperative subjects. A reviewer may ask for a contribution to be reduced in scope or split into independent changes to keep the terminal and process model maintainable.

Every push to `main` that passes CI normally creates a patch release. Maintainers may use `[release=skip]` in an appropriate docs-only or CI-only commit message when no release should be cut.

## Documentation locations

Choose the document that owns the public surface:

- `README.md` for installation, flags, keys, mouse behavior, harnesses, socket API, themes, notifications, persistence, and development
- `CONTRIBUTING.md` for contributor workflow and review expectations
- `AGENTS.md` for automated-tool operating guidance and architecture contracts
- `.goreleaser.yaml` and `.github/workflows/` for release and CI behavior

Examples should be runnable, JSON should match the implemented schema, and limitations should be stated directly.

## Sensitive security reports

Do not open a public issue containing an exploitable vulnerability, credential, private terminal content, or sensitive local state. Use GitHub's private vulnerability reporting when available. Include a minimal reproduction, affected versions, impact, and suggested mitigation without using production secrets.

## License

By submitting a contribution, you agree that it may be distributed under the repository's [MIT License](LICENSE).
