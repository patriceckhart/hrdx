<div align="center">
  <a href="https://www.hrdx.dev">
    <img src="assets/hrdx.png" alt="hrdx - run all your coding agents in one terminal" width="80" height="35" />
  </a>
</div>
<br>
<p align="center">
  <a href="LICENSE"><img alt="license" src="https://img.shields.io/badge/license-MIT-blue"></a>
  <img alt="Go 1.25+" src="https://img.shields.io/badge/Go-1.25+-00ADD8?logo=go&logoColor=white">
</p>
<p align="center">
  <a href="https://www.hrdx.dev">hrdx.dev</a>
</p>

## What is it?

hrdx is a experimental, minimal and lightweight terminal multiplexer built for the agent era: your projects as workspaces in a sidebar, tabs per workspace, and real terminal panes running [Codex CLI](https://learn.chatgpt.com/docs/codex/cli), [Claude Code](https://code.claude.com/docs/en/quickstart), [pi](https://www.pi.dev), [zot](https://www.zot.sh) or plain shells side by side. Kick off an agent in one project, switch to the next, and let the sidebar spinners tell you who is still working.

- **Real terminals, not wrappers.** Every pane is a genuine PTY session with a full terminal emulator behind it. Agent TUIs run exactly as they do standalone: streaming, slash commands, sessions, mouse support, all of it. Panes present a clean terminal identity so capability-sniffing TUIs pick rendering paths that work inside a multiplexer, and `HRDX=1` lets tools detect they run inside hrdx.
- **Everything in view.** The sidebar shows one hierarchy of workspaces, Git branches, and panes, adding tab rows only when a workspace has multiple tabs. Every pane has a shared status circle: agents become animated braille spinners while working, and an unfocused agent turns orange when it finishes. Focusing the pane acknowledges it and restores green.
- **Feels like your terminal.** Scrollback, mouse selection with clipboard copy, drag-to-resize splits, drag-to-reorder workspaces, right-click context menus, and kitty keyboard protocol handling so ordinary navigation (including with Caps Lock or Num Lock active) and exotic chords like ctrl+1 reach the focused process correctly.
- **Picks up where you left off.** Quit and relaunch: shells and agents keep running in a lightweight session holder and reattach exactly where they were, running commands and all. Workspaces, tabs, splits, and ratios come back too, and if a session is truly gone, agents resume their latest conversation from their own session store.
- **Yours to tune.** A settings window (`ctrl+b ,` or the gear in the sidebar) lets you switch individual agents on or off, control automatic copying of selected text, pick a notification sound for finished turns (including your own audio files), and change the color theme, with user themes as simple JSON files. All persisted. See [Themes](#themes).
- **Bring your own agent.** Register any agent CLI as a custom harness via a small JSON file, including its own busy detection for the sidebar spinner and finish sound. It shows up in pickers, cycling, and settings like the built-ins. See [Custom harnesses](#custom-harnesses).
- **Scriptable from outside.** A JSON socket API lets scripts and editors inspect workspaces and pane states, open projects, spawn panes, type into agents, wait for them to finish, read their screens, and subscribe to live events. See [Socket API](#socket-api).

## Install

```sh
curl -fsSL https://www.hrdx.dev/install.sh | bash
```

```powershell
irm https://www.hrdx.dev/install.ps1 | iex
```

macOS, Linux, or Windows (10 1809+ / 11, via [ConPTY](https://learn.microsoft.com/en-us/windows/console/creating-a-pseudoconsole-session)), plus at least one agent CLI on your PATH: `codex`, `claude`, `pi` or `zot`. Update on any supported platform with `hrdx update`.

## Run

```sh
hrdx
```

Open several projects at once, or pick your default agent:

```sh
hrdx --cwd ~/Developer/api --cwd ~/Developer/web
hrdx --agent claude
```

### Flags

| Flag | Purpose |
|---|---|
| `--cwd PATH` | Open a project as a workspace, repeatable |
| `--agent ID` | Default agent for new panes: `zot`, `pi`, `claude`, `codex` (default `zot`) |
| `--provider ID` | Pass a provider to every zot pane (zot only) |
| `--model ID` | Pass a model to every zot pane (zot only) |
| `--reasoning LEVEL` | Set the reasoning level (zot only) |
| `--continue` | Resume each project's latest session |
| `--codex-bin PATH` | Use a specific codex binary |
| `--claude-bin PATH` | Use a specific claude binary |
| `--pi-bin PATH` | Use a specific pi binary |
| `--zot-bin PATH` | Use a specific zot binary |
| `--shell PATH` | Shell for shell panes (default `$SHELL`; on Windows only when resolvable, otherwise `%COMSPEC%`/`powershell.exe`) |
| `--state PATH` | State file for workspace persistence (empty disables) |
| `--fresh` | Ignore saved workspaces and start clean |
| `--api` | Serve the control API on a unix socket (default on, `--api=false` disables) |
| `--persist` | Keep pane processes alive across restarts via the session holder (default on) |
| `--plugins` | Enable the experimental runtime for explicitly approved plugins (default off) |
| `--plugin-views` | Allow approved floating plugin views, requires `--plugins` (default off) |

A native Windows `hrdx.exe` launched from Git Bash ignores an MSYS-only `SHELL` value such as `/usr/bin/bash`, which Windows cannot resolve, and falls back to `%COMSPEC%`. To use Git Bash for panes, pass a native path explicitly, for example `hrdx --shell "C:/Program Files/Git/bin/bash.exe"`.

On Windows, hrdx reads VT input from the console so bracketed multi-line paste reaches an agent pane as one paste instead of separate Enter presses. This requires a terminal that supports bracketed paste, such as Windows Terminal. Console size changes are detected by polling, so pane resizing may lag by up to 100 ms.

## Keys

All keys go to the focused terminal, except the `ctrl+b` prefix (tmux style):

| After `ctrl+b` | Action |
|---|---|
| `c` or `C` | Split right / below (opens a picker: installed agents or shell) |
| `a` or `A` | Split right / below with the default agent directly |
| `s` or `S` | Split with a new shell pane directly (right / below), also `%`/`\"` and `\|`/`-` |
| `w` | New workspace (directory prompt with tab completion, then agent/shell picker) |
| `t` | New tab in the current workspace (opens the agent/shell picker) |
| `n` or `p` | Next / previous tab |
| `]` or `[` | Next / previous workspace |
| `tab` or `shift+tab` | Next / previous pane; stays in prefix mode for repeated jumps, `esc` exits |
| `/` | Fuzzy finder over every workspace, tab, and pane: type to filter, arrows select, enter jumps |
| `b` | Collapse or expand the workspace sidebar |
| `P` | Open experimental plugin lifecycle controls |
| `r` | Rename the focused pane |
| `m` | Open the pane context menu |
| `=` | Equalize all splits |
| `u` or `d` (or `pgup`/`pgdown`) | Scroll the focused pane's history |
| `esc` / `G` | Back to live output, clear selection |
| `,` | Settings window: enable / disable agents, notifications |
| `x` | Close pane (sibling takes its room) |
| `X` | Close workspace |
| `ctrl+b` | Send a literal ctrl+b to the pane |
| `q` | Quit |
| `left` / `right` | Scroll the hint row in the footer (narrow terminals) |

Panes whose process exits (for example `exit` in a shell) close automatically; the sibling pane takes the room. Panes that fail to start stay visible with the error.

### Custom keys

Keys are configurable via a `keys.json` next to the state file (`~/Library/Application Support/hrdx/keys.json` on macOS, `$XDG_CONFIG_HOME/hrdx/keys.json` on Linux, `%AppData%\hrdx\keys.json` on Windows). It maps action names to a single key. A prefix-action override replaces that action's default keys. The `prefix` action remaps the `ctrl+b` trigger itself, not just an action inside it. `navigate-up` and `navigate-down` add navigation keys for pickers, settings, and find while arrows and j/k remain available:

```json
{
  "find": "f",
  "quit": "Q",
  "agent-cycle": "g",
  "sidebar-toggle": "B",
  "navigate-up": "home",
  "navigate-down": "end"
}
```

Actions: `prefix`, `literal`, `quit`, `picker-right`, `picker-down`, `agent-right`, `agent-down`, `agent-cycle` (unbound by default), `shell-right`, `shell-down`, `workspace`, `tab-new`, `tab-next`, `tab-prev`, `space-next`, `space-prev`, `pane-next`, `pane-prev`, `find`, `sidebar-toggle`, `close-pane`, `close-space`, `equalize`, `rename`, `menu`, `settings`, `plugins`, `scroll-up`, `scroll-down`, `live`, `navigate-up`, `navigate-down`.

## Mouse

Everything is clickable: workspace, tab, and pane rows in the sidebar, the collapse arrow beside `WORKSPACES`, the main tab bar, menus, and the settings entry at the bottom. The compact sidebar shortens workspace and branch names after six display cells and pane names after two, hides the `WORKSPACES` heading and new-workspace entry, and shows only the left-aligned expand arrow and settings gear. Drag workspaces to reorder them, drag pane borders to resize, right-click for context menus, and drag with the left button to select text. Completed selections are copied straight to your clipboard by default; turn this off under the terminal tab in settings when you only want the highlight. Wheel events go to the pane under the cursor: agent TUIs scroll themselves, shells scroll their local history, and `shift+pgup` / `shift+pgdn` do the same from the keyboard.

Local scrollback stops at the oldest available line; further upward wheel events do not wrap back to live output. hrdx reassembles mouse reports fragmented by its input decoder so rapid wheel input is not mistaken for typing. A literal `alt+[` may wait up to 30 ms to distinguish it from a fragmented mouse report; ordinary Escape, other keys, and paste keep their normal behavior.

Sidebar context menus follow the clicked row: workspace names and Git branch rows offer workspace actions; the first pane row of each tab in a multi-tab workspace offers tab actions; other pane rows offer pane actions. Closing a tab leaves the workspace and its other tabs intact. The final tab cannot be closed through the tab menu, and pane menus omit Close for a tab's last pane. To close the entire workspace, use its workspace menu or the workspace-close key binding. Blank rows and dividers do not change focus. Custom socket menu entries follow the same scopes (`sidebar`, `tab`, or `pane`). If automation removes a menu or tab-picker target before selection, its stale action is dismissed instead of acting on the newly focused workspace or creating an invisible pane.

## Remote and container panes

Every pane is a real PTY, so a shell pane can connect to a remote host, Docker container, or Kubernetes workload. For an interactive shell:

```sh
ssh user@host
docker exec -it -w /workspace container-name sh
kubectl exec -it -n namespace deploy/app -- sh
```

An agent installed at the target can be launched directly instead:

```sh
ssh -tt user@host 'cd /path/to/project && exec codex'
docker exec -it -w /workspace container-name codex
kubectl exec -it -n namespace deploy/app -- codex
```

Use the same pattern for any supported or custom agent. Authentication and the agent executable, configuration, credentials, and project files must be available at the target.

To make remote and container agents appear in pickers, agent cycling, settings, and the sidebar, register their client command as a custom harness:

```json
[
  {
    "kind": "remote-codex",
    "binary": "ssh",
    "args": ["-tt", "user@host", "cd /path/to/project && exec codex"]
  },
  {
    "kind": "docker-codex",
    "binary": "docker",
    "args": ["exec", "-it", "-w", "/workspace", "container-name", "codex"]
  },
  {
    "kind": "k8s-codex",
    "binary": "kubectl",
    "args": ["exec", "-it", "-n", "namespace", "deploy/app", "--", "codex"]
  }
]
```

Wrapper scripts are useful when the host, container, namespace, pod, working directory, or authentication setup is dynamic. Set the harness `binary` to the wrapper path and put any fixed parameters in `args`.

The session holder keeps the local SSH, Docker, or Kubernetes client process alive when hrdx restarts. It cannot keep an agent alive when its remote host, container, pod, or network connection ends, and it does not automatically reconnect. Workspace Git details are read from the local workspace path, not from the remote filesystem.

## Custom harnesses

Any agent CLI beyond the built-ins can be registered by dropping a `harness.json` next to the state file (`~/Library/Application Support/hrdx/` on macOS, `$XDG_CONFIG_HOME/hrdx/` on Linux, `%AppData%\hrdx\` on Windows). Registered harnesses appear everywhere the built-ins do: in the pickers, in agent cycling, as agent panes in the sidebar hierarchy, and in the settings window for enabling and disabling.

```json
[
  {
    "kind": "aider",
    "binary": "aider",
    "args": ["--no-auto-commits"],
    "resume": ["--restore-chat-history"],
    "busy": "Waiting for the model"
  },
  {
    "kind": "goose",
    "idle_title": "goose idle",
    "attention_title": "goose waiting"
  }
]
```

| Field | Purpose |
|---|---|
| `kind` | Identifier used in pickers and pane names (required, must not collide with built-ins) |
| `binary` | Executable to launch (default: same as `kind`) |
| `args` | Extra arguments passed on every launch |
| `resume` | Arguments that resume the latest session when a restored pane relaunches |
| `resume_first` | Put the resume args before `args` (for subcommands like `resume --last`) |
| `busy` | A substring visible on screen only while the harness is working; drives the busy spinner and the finish sound. Empty: braille spinner detection, like the built-ins |
| `idle_title` | Terminal-title substring emitted when the harness is idle; overrides a stale visible spinner |
| `attention_title` | Terminal-title substring emitted while waiting for user input; overrides the spinner and shows an orange dot when unfocused |

Both title fields are optional and have no defaults, since every harness
publishes its own markers. Leave them out and the harness is detected purely
from the screen, exactly as `busy` describes. Set them when the harness keeps a
spinner on screen while it is really idle or blocked on a prompt: a matching
title always outranks the screen scrape. Check what your harness emits with
`printf '\e]2;...\a'`-style OSC titles before picking a substring.

## Experimental plugins

Plugins are opt-in external processes communicating through bounded NDJSON over stdin/stdout. They can contribute context-menu actions, finder search providers, notifications, footer status, scoped metadata subscriptions, approved pane operations and input, private storage, and separately enabled floating views. Manifests may declare workspace activation markers and typed configuration options. Normal startup runs no plugins. Custom harnesses and holder sessions are unchanged.

**Plugins are trusted executable code, not sandboxed code.** Approving a plugin permits it to run with your OS permissions and inherited environment. Host grants restrict protocol operations only. They cannot prevent a same-user executable from reading accessible files, spawning processes, or using the existing control socket. Inspect packages before approving them.

### Discover and approve

```sh
hrdx plugins list
hrdx plugins doctor --json
hrdx plugins list --state="" --dir ./plugin-packages
```

Discovery never executes code. The default root is `plugins/` next to the state file, with `plugin.json` in each immediate package directory. Repeatable `--dir PATH` adds roots without precedence. Duplicate IDs invalidate every claimant. There is no automatic project scan, recursive discovery, or download. `--state PATH` selects another configuration location, while `--state=""` disables the default inventory root. `list` returns zero after writing its inventory. `doctor` also checks enabled approvals and returns 1 for problems. Invalid command usage returns 2.

Try the standard-library-only reference plugin from a checkout:

```sh
go build -o examples/plugins/hello/hello.exe ./examples/plugins/hello
hrdx plugins approve --package ./examples/plugins/hello --trust --grant ui.action.contribute --grant ui.notification --grant ui.provider.contribute --set greeting=Hi
hrdx --plugins
```

Right-click a pane or workspace and select **Hello from plugin**. Open the finder with `ctrl+b /` and type at least two characters to see the example search provider's rows below the ordinary matches. The process starts lazily when invoked, with activation progress in the footer. hrdx rechecks the captured target after activation, so a pane, tab, or workspace closed while the plugin starts is not redirected to another resource. `ctrl+b P` opens plugin lifecycle controls, also available in workspace menus and in a `plugins` settings section that can also disable a plugin. Menus scroll with keyboard selection when their entries exceed the available height.

Manifests can declare activation markers (`.git`, `package.json`) so actions and providers appear only in workspaces containing one of them, and flat typed configuration options set with `--set key=value` at approval time and delivered to the plugin at startup. Configuration is ordinary state, not a secret store. Approval is bound to the canonical package path and a SHA-256 digest of all package files. Changing a binary, manifest, or asset requires explicit reapproval. Runtime packages cannot contain symlinks and are limited to 128 MiB, 1024 entries, and 32 directory levels. Write mutable data outside the package. Grants must be explicitly listed and requested by the manifest. To allow workspace operations, also specify repeatable `--workspace PATH` scopes or explicitly choose `--instance`. Workspace scopes use the exact absolute path spelling shown in hrdx's status, preserving symlinks and case. Without those scopes, no workspace data or pane operations are allowed.

```sh
hrdx plugins inspect --id example.hello
hrdx plugins disable --id example.hello
hrdx plugins enable --id example.hello
hrdx plugins revoke --id example.hello
hrdx plugins status
hrdx plugins start --id example.hello
hrdx plugins stop --id example.hello
hrdx plugins restart --id example.hello
hrdx plugins reload --id example.hello
```

Management commands accept `--state PATH`. `revoke` removes the approval record but retains separately stored private plugin data. Approvals live in `plugin-approvals/` next to the state file, separate from workspace snapshots. A running instance checks approval changes every 500 ms and revokes changed, removed, or disabled records. New approvals and changed grants require restarting hrdx. `start`, `stop`, `restart`, and `status` use the running instance's control socket and require the API to be enabled. Status includes package path, version, negotiated protocol, requested and granted capabilities, scopes, declared action/view IDs, lifecycle state, generation, and sanitized failure. Package paths and workspace scopes can be sensitive. Lifecycle commands acknowledge acceptance, not successful startup. Check `status` for handshake or launch failures. After changing development package files, run `approve --trust` again and then `reload --id ID`. Reload accepts only a current approval for the same package path, stops the old generation, and revalidates the package before launch. A stopped or crashed plugin never automatically restarts.

### Views, events, and process lifetime

Plugin views additionally require `--plugin-views`, a `ui.view.contribute` grant, and a declared view ID. A view floats over the terminals by default, or docks at the right or bottom edge of the terminal area (at most half of it), in which case the split layout uses the remaining space without changing the saved layout. Focused keyboard/mouse input needs the separate `ui.view.input` grant. Views use bounded plain-text rows rather than raw ANSI. Host borders, clipping, stacking, menus, settings, and footer remain host-owned. Escape or the frame's `x` closes a view, and the normal prefix opens host commands. Views have no PTY, holder session, or persisted layout identity.

To enable the reference plugin's optional panel, reapprove it with `--grant ui.view.contribute --grant ui.view.input` in addition to its action and notification grants, then launch `hrdx --plugins --plugin-views`.

Subscriptions deliver scoped `snapshot.changed` events at most five times per second. Delivery is best effort, changes may be coalesced, and clients recover through a fresh scoped `status` query. Terminal output and ordinary terminal keystrokes are not event streams. Notifications are rate-limited, status keeps the latest value, and stopped sessions lose their contributions and subscriptions.

Plugins stop with the TUI using bounded graceful shutdown and platform-specific process cleanup. Split and tab panes created by approved plugin operations become host-owned and retain normal holder persistence. Floating panes created by a plugin are temporary: they belong to that plugin connection, never enter saved state, and close when the plugin stops, crashes, reloads, or hrdx quits. At most four exist at once. With the separate `pane.send_input` grant a plugin may type into in-scope panes. Pane input, from keyboard, socket, or plugin, is queued in order and written by a background goroutine, so a child that stops reading its PTY cannot freeze the UI. Plugin input is bounded to 16 KiB per call and 256 KiB queued per pane and reports `busy` when a child is stalled. Keyboard input is never dropped. Private storage is kept separately under `plugin-data/`, with per-plugin quotas and cross-process locking. Neither approvals nor plugin data are sandboxed from other same-user processes. Protocol payloads and stderr are not logged.

The [plugin protocol and manifest reference](docs/plugin-platform.md) documents methods, grants, limits, compatibility, and remaining work. The platform is experimental, independently versioned, and does not claim zot wire compatibility.

## Socket API

While hrdx runs it serves a control API on a unix socket next to the state file (`hrdx.sock`), so scripts, editors, and coding agents can inspect and drive a running session. Disable with `--api=false`.

The protocol is newline-delimited JSON: send one request per line, receive one response line with the same `id`.

```sh
SOCK="$HOME/Library/Application Support/hrdx/hrdx.sock"   # macOS default
# SOCK="$XDG_CONFIG_HOME/hrdx/hrdx.sock"                  # Linux, or macOS with XDG_CONFIG_HOME set
# hrdx.sock is a native Windows AF_UNIX socket too (%AppData%\hrdx\hrdx.sock).
# WSL has a separate socket namespace and cannot connect to it directly; Git
# Bash does not ship a compatible `nc -U`. Use a native Windows client, such
# as .NET UnixDomainSocketEndPoint or Go's net.DialUnix.

echo '{"id": "1", "method": "status"}' | nc -U "$SOCK"
echo '{"id": "2", "method": "workspace.create", "params": {"path": "~/Developer/api", "agent": "claude"}}' | nc -U "$SOCK"
echo '{"id": "3", "method": "pane.create", "params": {"workspace": "api", "kind": "shell", "split": "down"}}' | nc -U "$SOCK"
echo '{"id": "3b", "method": "pane.create", "params": {"workspace": "api", "kind": "shell", "split": "float", "anchor": "center", "width_pct": 40, "height_pct": 30}}' | nc -U "$SOCK"
echo '{"id": "4", "method": "pane.send_text", "params": {"pane_id": 3, "text": "run the tests", "enter": true}}' | nc -U "$SOCK"
echo '{"id": "5", "method": "pane.wait", "params": {"pane_id": 3, "until": "idle"}}' | nc -U "$SOCK"
echo '{"id": "6", "method": "pane.read", "params": {"pane_id": 3}}' | nc -U "$SOCK"
echo '{"id": "7", "method": "menu.register", "params": {"target": "pane", "label": "Run linter", "action_id": "custom.run_linter"}}' | nc -U "$SOCK"
```

| Method | Effect |
|---|---|
| `ping` | Liveness check, returns `pong` |
| `status` | Workspaces, tabs, and panes with id, kind, running, and busy state |
| `workspace.create` | Open a directory as a workspace (`path`, optional `agent`) |
| `workspace.close` | Close a workspace by name or path |
| `pane.create` | Add a pane (`workspace` name or path, `kind`, `split`: `right`, `down`, `tab`, `float`) |
| `pane.send_text` | Type into a pane (`pane_id`, `text`, optional `enter`) |
| `pane.read` | The pane's visible screen as plain text |
| `pane.wait` | Block until a pane's agent is `idle` or `busy` (`until`, optional `timeout_ms`) |
| `pane.close` | Close a pane by id |
| `menu.register` | Add a process-local context-menu entry (`target`: `pane`, `tab`, `sidebar`; `label`; unique `action_id`) |
| `events.subscribe` | Keep the connection open and push events |
| `plugins.status` | Inspect experimental plugin lifecycle states, requires `--plugins` |
| `plugins.control` | Request `start`, `stop`, `restart`, or explicitly reapproved `reload` for a plugin (`plugin`, `action`), requires `--plugins` |

Successful responses are `{"id": "...", "result": {...}}`; failures are `{"id": "...", "error": {"code": "not_found", "message": "..."}}` with codes `not_found`, `invalid_params`, `unknown_method`, `timeout`, and `error`.

After `events.subscribe` the connection stays open and hrdx pushes lines like `{"event": "pane.busy_changed", "data": {"pane_id": 3, "busy": false}}`. Events: `workspace.created`, `workspace.closed`, `pane.created`, `pane.closed`, `pane.busy_changed`, and `menu.action`, so a script can react the moment an agent finishes instead of polling.

A `menu.register` entry appears after the built-in actions in the requested context menu. Selecting it publishes `{"event":"menu.action","data":{"action_id":"custom.run_linter","target":"pane","pane_id":3,"workspace":"api","path":"/path/to/api","tab_index":0}}`. Registrations are ephemeral, re-registering an `action_id` replaces it, and events use the API's existing best-effort delivery for slow subscribers.

With `split: "float"`, `width_pct` and `height_pct` are required integers from 1 through 100. `anchor` defaults to `center` and also accepts `top`, `bottom`, `left`, or `right`. Floating panes belong to the active tab, render above its split layout without changing pane ratios, and are omitted from the sidebar and persisted state. Multiple floats stack in creation or focus order. Close one with its title-bar `x` or the existing `pane.close` method. They remain discoverable in `status` through `floating`, `anchor`, `width_pct`, and `height_pct` fields.

Every request is answered by the TUI's own update loop, so the API always sees exactly what is on screen. `pane.wait` plus `pane.send_text` is enough to build simple agent pipelines: prompt an agent, wait until it is idle, read the screen, move on.

## Themes

hrdx includes a collection of selectable themes such as Dracula, Gruvbox, Tokyo Night, Catppuccin, Solarized, Matrix, and the original default. Pick one in the settings window's theme section; long lists scroll with the arrow keys or mouse wheel. The change applies immediately and persists.

Custom themes are JSON files that override any subset of the default colors; missing values inherit the original look. Drop them into a `themes/` directory next to the state file (`~/Library/Application Support/hrdx/themes/` on macOS, `$XDG_CONFIG_HOME/hrdx/themes/` on Linux, `%AppData%\hrdx\themes\` on Windows). Custom theme names must not collide with a bundled theme.

```json
{
  "name": "neon",
  "description": "Pink accent, near-black bars.",
  "colors": {
    "accent": 201,
    "bar_bg": "#101010"
  }
}
```

Values are ANSI 256 color numbers or `"#rrggbb"` strings.

| Color | Used for |
|---|---|
| `accent` | Focused pane frames, highlights, logo, selected items |
| `alt` | Prefix badge, behind-count in the sidebar |
| `muted` | Secondary text, hints, idle pane names |
| `faint` | Inactive pane borders, sidebar divider |
| `good` | Running dots, input badge |
| `busy` | Busy spinner and completed-work attention dot |
| `bad` | Errors, exited dots |
| `bar_bg` / `bar_fg` | Header and footer bars |
| `ink` | Text on accent backgrounds, tab bar strip |

See `examples/themes/` for a full example.

## Notifications

The notification section of the settings window has two independent toggles for finished agent turns: play a sound (built-in `ding` and `chime`, or your own audio files) and a system notification, which rings the terminal bell so your platform's native attention indicator fires: dock badge and bounce on macOS, the window manager's urgency hint on Linux, the taskbar/window attention flash on Windows Terminal (depends on its `bellStyle` setting). No notification daemon or permission required. Because it is the terminal bell, your terminal may also play its own alert sound for it even with hrdx's sound toggle off. To get the badge without any audio, set the terminal's bell to visual or silent (Terminal.app: Settings, Profiles, Advanced, uncheck Audible bell; iTerm2: Profiles, Terminal, Silence bell; Ghostty: `bell-features`; Windows Terminal: `bellStyle`). Add custom sounds with a `sounds.json` next to the state file; they appear as choices and are previewed when selected:

```json
[
  { "name": "sheep", "file": "~/sounds/maehhh.wav" },
  { "name": "gong", "file": "/Users/me/sounds/gong.aiff" }
]
```

`name` is the label in settings (must not collide with built-ins), `file` any audio file your OS player understands (`afplay` on macOS, `paplay`/`aplay` on Linux, PowerShell's `SoundPlayer` on Windows — WAV only there). Missing files are reported in the footer and skipped.

## Persistence

Quitting hrdx does not kill your sessions. Pane processes live in a small background process (the session holder) that hrdx starts on demand and talks to over a local socket. Close the TUI, reopen it, and every shell and agent reattaches exactly where it was: running commands keep running, scrollback and screen state are replayed, nothing restarts. The holder is the same `hrdx` binary, uses no resources worth mentioning, and goes away when you kill its sessions.

Workspaces, panes, split layout, ratios, selection, sidebar collapsed state, and holder session ids are saved automatically (default: `~/Library/Application Support/hrdx/state.json` on macOS, `$XDG_CONFIG_HOME/hrdx/state.json` on Linux, `%AppData%\hrdx\state.json` on Windows). An absolute `XDG_CONFIG_HOME` takes precedence on macOS too, so everything hrdx stores next to the state file (keys, harnesses, themes, plugins, sockets) follows it. An existing `~/Library/Application Support/hrdx` keeps being used until you move it to `$XDG_CONFIG_HOME/hrdx`, so setting the variable never orphans running sessions. Windows ignores the variable. On the next launch the layout is restored and each pane reattaches to its held session. When a held session is gone (rebooted machine, killed holder), the pane starts fresh instead: shell panes get a new shell, and agent panes relaunch resuming their latest session for that directory via the agent's own session store.

`--persist=false` disables the holder (panes die with the TUI, like a plain terminal). `--fresh` skips restoring and cleans up now-unreferenced held sessions; `--state ""` disables persistence entirely.

## Development

```sh
make check
```

Windows without `make` on `PATH`: `go vet ./... && gofmt -l . && go test ./...`.

## License

MIT
