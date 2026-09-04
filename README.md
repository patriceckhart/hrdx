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
| `--worktree-create-cmd CMD` | Override the worktree creation command template |
| `--state PATH` | State file for workspace persistence (empty disables) |
| `--fresh` | Ignore saved workspaces and start clean |
| `--api` | Serve the control API on a unix socket (default on, `--api=false` disables) |
| `--persist` | Keep pane processes alive across restarts via the session holder (default on) |

A native Windows `hrdx.exe` launched from Git Bash ignores an MSYS-only `SHELL` value such as `/usr/bin/bash`, which Windows cannot resolve, and falls back to `%COMSPEC%`. To use Git Bash for panes, pass a native path explicitly, for example `hrdx --shell "C:/Program Files/Git/bin/bash.exe"`.

## Keys

All keys go to the focused terminal, except the `ctrl+b` prefix (tmux style):

| After `ctrl+b` | Action |
|---|---|
| `c` or `C` | Split right / below (opens a picker: installed agents or shell) |
| `a` or `A` | Split right / below with the default agent directly |
| `s` or `S` | Split with a new shell pane directly (right / below), also `%`/`\"` and `\|`/`-` |
| `w` | New workspace (directory prompt with tab completion, then agent/shell picker) |
| *(unbound)* | Open a Git worktree in the current workspace group (configure `worktree-add` in `keys.json`) |
| `t` | New tab in the current workspace (opens the agent/shell picker) |
| `n` or `p` | Next / previous tab |
| `]` or `[` | Next / previous workspace |
| `tab` or `shift+tab` | Next / previous pane; stays in prefix mode for repeated jumps, `esc` exits |
| `/` | Fuzzy finder over every workspace, tab, and pane: type to filter, arrows select, enter jumps |
| `b` | Collapse or expand the workspace sidebar |
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
  "worktree-add": "w",
  "navigate-up": "home",
  "navigate-down": "end"
}
```

Actions: `prefix`, `literal`, `quit`, `picker-right`, `picker-down`, `agent-right`, `agent-down`, `agent-cycle` (unbound by default), `shell-right`, `shell-down`, `workspace`, `worktree-add` (unbound by default), `tab-new`, `tab-next`, `tab-prev`, `space-next`, `space-prev`, `pane-next`, `pane-prev`, `find`, `close-pane`, `close-space`, `equalize`, `rename`, `menu`, `settings`, `scroll-up`, `scroll-down`, `live`, `navigate-up`, `navigate-down`.
Actions: `prefix`, `literal`, `quit`, `picker-right`, `picker-down`, `agent-right`, `agent-down`, `agent-cycle` (unbound by default), `shell-right`, `shell-down`, `workspace`, `worktree-add` (unbound by default), `tab-new`, `tab-next`, `tab-prev`, `space-next`, `space-prev`, `pane-next`, `pane-prev`, `find`, `sidebar-toggle`, `close-pane`, `close-space`, `equalize`, `rename`, `menu`, `settings`, `scroll-up`, `scroll-down`, `live`, `navigate-up`, `navigate-down`.

## Mouse

Everything is clickable: workspace, tab, and pane rows in the sidebar, the collapse arrow beside `WORKSPACES`, the main tab bar, menus, and the settings entry at the bottom. Git worktrees from the same repository are grouped together; use the workspace context menu's **Open worktree** action to add an existing worktree, or **Create worktree...** to create one. The compact sidebar shortens names and hides the workspace heading and new-workspace entry.

Creation prompts for a branch/name and runs `worktree.json` next to the state file. The default command is `git worktree add -b {name} {path}`, with `{path}` set to `.worktrees/<name>` in the repository. Configure a custom shell command with `{name}`, `{path}`, and `{repo}` placeholders, for example:

```json
{"create": "git worktree add -b {name} {path}"}
```

The same template can be supplied with `--worktree-create-cmd`; that flag takes precedence over `worktree.json`. Paths and values are shell-quoted before interpolation. Drag workspaces to reorder them, drag pane borders to resize, right-click for context menus, and drag with the left button to select text. Wheel events go to the pane under the cursor: agent TUIs scroll themselves, shells scroll their local history, and `shift+pgup` / `shift+pgdn` do the same from the keyboard.

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

## Socket API

While hrdx runs it serves a control API on a unix socket next to the state file (`hrdx.sock`), so scripts, editors, and coding agents can inspect and drive a running session. Disable with `--api=false`.

The protocol is newline-delimited JSON: send one request per line, receive one response line with the same `id`.

```sh
SOCK="$HOME/Library/Application Support/hrdx/hrdx.sock"   # macOS
# SOCK="$XDG_CONFIG_HOME/hrdx/hrdx.sock"                  # Linux
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

The notification section of the settings window has two independent toggles for finished agent turns: play a sound (built-in `ding` and `chime`, or your own audio files) and a system notification, which rings the terminal bell so your platform's native attention indicator fires: dock badge and bounce on macOS, the window manager's urgency hint on Linux, the taskbar/window attention flash on Windows Terminal (depends on its `bellStyle` setting). No notification daemon or permission required. Add custom sounds with a `sounds.json` next to the state file; they appear as choices and are previewed when selected:

```json
[
  { "name": "sheep", "file": "~/sounds/maehhh.wav" },
  { "name": "gong", "file": "/Users/me/sounds/gong.aiff" }
]
```

`name` is the label in settings (must not collide with built-ins), `file` any audio file your OS player understands (`afplay` on macOS, `paplay`/`aplay` on Linux, PowerShell's `SoundPlayer` on Windows — WAV only there). Missing files are reported in the footer and skipped.

## Persistence

Quitting hrdx does not kill your sessions. Pane processes live in a small background process (the session holder) that hrdx starts on demand and talks to over a local socket. Close the TUI, reopen it, and every shell and agent reattaches exactly where it was: running commands keep running, scrollback and screen state are replayed, nothing restarts. The holder is the same `hrdx` binary, uses no resources worth mentioning, and goes away when you kill its sessions.

Workspaces, panes, split layout, ratios, selection, sidebar collapsed state, and holder session ids are saved automatically (default: `~/Library/Application Support/hrdx/state.json` on macOS, `$XDG_CONFIG_HOME/hrdx/state.json` on Linux, `%AppData%\hrdx\state.json` on Windows). On the next launch the layout is restored and each pane reattaches to its held session. When a held session is gone (rebooted machine, killed holder), the pane starts fresh instead: shell panes get a new shell, and agent panes relaunch resuming their latest session for that directory via the agent's own session store.

`--persist=false` disables the holder (panes die with the TUI, like a plain terminal). `--fresh` skips restoring and cleans up now-unreferenced held sessions; `--state ""` disables persistence entirely.

## Development

```sh
make check
```

Windows without `make` on `PATH`: `go vet ./... && gofmt -l . && go test ./...`.

## License

MIT
