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

hrdx is an experimental, minimal and lightweight agent workbench: your projects as workspaces in a sidebar, tabs per workspace, and real terminal panes running [Codex CLI](https://learn.chatgpt.com/docs/codex/cli), [Claude Code](https://code.claude.com/docs/en/quickstart), [pi](https://www.pi.dev), [zot](https://www.zot.sh) or plain shells side by side. Kick off an agent in one project, switch to the next, and let the sidebar spinners tell you who is still working.

- **Real terminals, not wrappers.** Every pane is a genuine PTY session with a full terminal emulator behind it. Agent TUIs run exactly as they do standalone: streaming, slash commands, sessions, mouse support, all of it. Panes present a clean terminal identity so capability-sniffing TUIs pick rendering paths that work inside a multiplexer, and `HRDX=1` lets tools detect they run inside hrdx. Color queries (`OSC 10`, `11`, and `4`) are answered with your terminal's real colors, which hrdx asks for once at startup (dark defaults when the terminal does not answer, and on Windows), so tools such as GitHub Copilot CLI pick readable colors.
- **Everything in view.** The sidebar shows one hierarchy of workspaces, Git branches, and panes, adding tab rows only when a workspace has multiple tabs. Every pane has a shared status circle: agents become animated braille spinners while working, and an unfocused agent turns orange when it finishes. Focusing the pane acknowledges it and restores green.
- **Feels like your terminal.** Scrollback, mouse selection with clipboard copy, drag-to-resize splits, drag-to-reorder workspaces and tabs, right-click context menus, and kitty keyboard protocol handling so ordinary navigation (including with Caps Lock or Num Lock active) and exotic chords like ctrl+1 reach the focused process correctly.
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

Quitting hrdx leaves your shells and agents running in the background; reattach by relaunching `hrdx`, or stop the background holder with `hrdx stop` (see [Persistence](#persistence)).

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
| `c` or `C` | Split in the default direction / the other direction (opens a picker: installed agents or shell) |
| `a` or `A` | Split in the default / other direction with the default agent directly |
| `s` or `S` | Split with a new shell pane directly (default / other direction); `%` and `\|` always split right, `\"` and `-` always below |
| `v` | Cycle the default split direction: right, below, auto |
| `w` | New workspace (directory prompt with tab completion, then agent/shell picker, then optional agent arguments, see [Agent arguments](#agent-arguments)) |
| `t` | New tab in the current workspace (opens the agent/shell picker) |
| `n` or `p` | Next / previous tab |
| `<` or `>` | Move the current tab one place left / right |
| `1` through `9`, or `0` | Select that numbered tab in the current workspace (`0` selects tab 10); missing tabs leave the selection unchanged |
| `]` or `[` | Next / previous workspace |
| `tab` or `shift+tab` | Next / previous pane; stays in prefix mode for repeated jumps, `esc` exits |
| `/` | Fuzzy finder over groups, workspaces, tabs, and panes: type to filter, arrows select, enter jumps |
| `e` or `g` | Open or close the read-only Files / Changes panel (see [Files and Changes](#files-and-changes)) |
| `b` | Collapse or expand the workspace sidebar |
| `P` | Open experimental plugin lifecycle controls |
| `D` | Open the durable agents panel for the current workspace (see [Durable agents](#durable-agents)) |
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

The default split direction is right, so `c`, `a`, and `s` keep their usual meaning. Change it with `v` or under the terminal tab in settings: **below** swaps the lowercase and uppercase keys, and **auto** splits the focused pane along its longer side (side by side when it is at least twice as wide as tall in cells, otherwise below). The choice is saved as the optional `split_direction` (`"down"` or `"auto"`; empty means right). The `-right` / `-down` action names in `keys.json` keep naming the lowercase and uppercase keys; context menus and the socket API keep their explicit directions.

Panes whose process exits (for example `exit` in a shell) close automatically; the sibling pane takes the room. Panes that fail to start stay visible with the error.

`alt+enter` reaches the focused pane with its Alt modifier when the host terminal reports it, so apps such as zot can use it to insert a newline. `shift+enter` requires the host terminal to report Shift separately from ordinary Enter; hrdx cannot recover a modifier the terminal did not send.

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

Actions: `prefix`, `literal`, `quit`, `picker-right`, `picker-down`, `agent-right`, `agent-down`, `agent-cycle` (unbound by default), `shell-right`, `shell-down`, `split-cycle`, `workspace`, `tab-new`, `tab-next`, `tab-prev`, `tab-move-left`, `tab-move-right`, `tab-1` through `tab-9`, `tab-0`, `space-next`, `space-prev`, `pane-next`, `pane-prev`, `find`, `files`, `changes`, `sidebar-toggle`, `close-pane`, `close-space`, `equalize`, `rename`, `menu`, `settings`, `plugins`, `scroll-up`, `scroll-down`, `live`, `navigate-up`, `navigate-down`.

## Mouse

Everything is clickable: workspace, tab, and pane rows in the sidebar, the collapse arrow beside `WORKSPACES`, the main tab bar, menus, and the settings entry at the bottom. The compact sidebar shortens workspace and branch names after six display cells and pane names after two, hides the `WORKSPACES` heading and new-workspace entry, and shows only the left-aligned expand arrow and settings gear. Drag workspaces to reorder them, and drag tabs along the tab bar to reorder them (the tab's right-click menu also offers Move left and Move right). Drag the sidebar's right border to widen or narrow it (20 to 80 columns, never more than half the window; the width is remembered and the compact sidebar keeps its fixed width), drag pane borders to resize, right-click for context menus, and drag with the left button to select text. Completed selections are copied straight to your clipboard by default; turn this off under the terminal tab in settings when you only want the highlight. Wheel events go to the pane under the cursor: agent TUIs scroll themselves, shells scroll their local history, and `shift+pgup` / `shift+pgdn` do the same from the keyboard.

Local scrollback stops at the oldest available line; further upward wheel events do not wrap back to live output. hrdx reassembles mouse reports fragmented by its input decoder so rapid wheel input is not mistaken for typing. A literal `alt+[` may wait up to 30 ms to distinguish it from a fragmented mouse report; ordinary Escape, other keys, and paste keep their normal behavior.

Sidebar context menus follow the clicked row: workspace names and Git branch rows offer workspace actions; the first pane row of each tab in a multi-tab workspace offers tab actions; other pane rows offer pane actions. Closing a tab leaves the workspace and its other tabs intact. The final tab cannot be closed through the tab menu, and pane menus omit Close for a tab's last pane. To close the entire workspace, use its workspace menu or the workspace-close key binding. Blank rows and dividers do not change focus. Custom socket menu entries follow the same scopes (`sidebar`, `tab`, or `pane`). If automation removes a menu or tab-picker target before selection, its stale action is dismissed instead of acting on the newly focused workspace or creating an invisible pane.

### Folding workspaces

Turn on **allow folding workspaces in the sidebar** under the terminal tab in settings (off by default, saved as `workspace_folding`). Each workspace name in the expanded sidebar then ends in a `▾` toggle. Clicking it, or choosing **Collapse** from the workspace context menu, folds the workspace to its name row; `▸` and **Expand** bring the branch, tab, and pane rows back. A folded row summarizes its panes: a spinner while any agent is working, an orange `●` when one finished unseen, and the pane count. Folding is purely visual: panes keep running, and keys, the finder, and the socket API still reach them. The fold is saved per workspace as the optional `folded` field. Turning the setting off shows every workspace expanded and removes the toggles and menu entries, but keeps each workspace's fold for when it is turned on again. The compact sidebar has no fold toggles and always shows one row per workspace.

### Sub-repositories

A workspace can also list the Git repositories nested inside its directory. Enable it with the **show nested git repositories in the sidebar** checkbox under the terminal tab in settings (off by default); once checked, the rows below it choose how deep to search (1 to 3 folder levels). Unchecking and checking again restores the previous depth. Repositories appear below the workspace's branch row as an indented tree: folders that only lead to deeper repositories are shown once as dimmed `name/` rows, which do not react to clicks. Each repository shows its name, branch, and status: `●n` changed or untracked files, `!n` merge conflicts, `↑n`/`↓n` commits ahead of or behind its upstream. Hidden directories, `node_modules`, and `vendor` are skipped, repositories inside a found repository are not searched, and at most 24 are listed per workspace. Status is refreshed in the background every 15 seconds with at most four `git status` processes at a time, so rendering never waits for Git; without Git only the branch is shown. The compact sidebar hides these rows.

Clicking a repository row's text (or right-clicking anywhere on the row) asks where to open it: a new tab, a split right or down of the focused pane, or a separate workspace; the row stays highlighted while you choose. **Hide folder** removes a repository from that workspace's list; hidden folders, and everything below them, are no longer scanned. The workspace context menu offers **Show hidden folders (n)** to bring them all back. The hidden list is saved per workspace as optional `hidden_subrepos` paths relative to the workspace directory. A pane opened in a repository keeps that start directory across restarts (saved as the pane's optional `cwd`); if the directory has disappeared, the pane falls back to the workspace directory. With the setting enabled, `status` adds an optional `subrepos` array to each workspace once a scan has completed.

### Files and Changes

Two read-only panels dock right of the panes, like herdr's. They follow the focused pane's folder (its start folder, or the workspace's) and refresh every 5 seconds while open. Nothing in them writes to disk or to git: there is no editor and no commit box.

- **Files** (`ctrl+b e`) is a folder tree. Folders open lazily, `.git` is hidden, and each folder lists at most 500 entries. Files and folders with uncommitted changes are colored and marked `M`, `A`, `D`, or `U`. Opening a file shows a syntax-highlighted preview with line numbers (up to 512 KiB and 5,000 lines; binary files show a note).
- **Changes** (`ctrl+b g`) lists the repository's uncommitted changes against `HEAD`, untracked files included, with each file's added and deleted line counts and the totals. Opening a file shows its highlighted diff with line numbers. Git runs with `--no-optional-locks` and without index refreshes, so the panel never touches the index or takes a lock.

In the TUI, opening a panel moves the keyboard into it: arrows or `j`/`k` move, `enter` or `l` opens a folder or file, `h` or `left` goes back up, `tab` switches between the panels, `esc` steps back from a preview or diff and then closes the panel, and the prefix key still works. Clicking a row opens it, the wheel scrolls, `x` in the header closes, and clicking a pane returns the keyboard to it. The panes shrink while a panel is open and their PTYs resize accordingly. In the desktop app, **Files** and **Changes** buttons sit at the right of the tab bar. There, the text of a preview or diff can be selected with the mouse and copied with `cmd+c` (`ctrl+shift+c` on Linux and Windows), and double-clicking a file in the tree, or the **Open** button above its preview, opens it in the system's default app. Only files inside the panel's folder can be opened.

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

## Agent arguments

A new workspace can start its agent with extra arguments, for example `--codemode`. After you pick an agent in the new workspace flow, the footer asks for arguments: type them and press `enter`, press `enter` on the empty line to start without any, or `esc` to cancel the workspace. The desktop app has an **Arguments** field in the New workspace dialog. Shell panes take no arguments and start right away.

Arguments are split like shell words: spaces separate, single and double quotes group (`--prompt "fix the bug"`), and a backslash escapes the next character. No shell runs, so `$HOME`, `~`, and `*` are passed as written. They are added after the agent's own arguments and the `--provider`, `--model`, and `--reasoning` flags, on every launch of that pane, including a restart or a resume after hrdx restarts. They are saved with the pane in the state file as an optional `args` list, so treat that file as you would shell history. Other panes, new tabs, and splits start their agents without them.

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

### Program status reports

Agents that report their state through the Program Status Protocol (OSC 7501)
are tracked from those reports instead of the screen: `working` is busy,
`blocked` shows the orange attention dot, and `idle`, `done`, or `error` are
idle. Reports outrank both title markers and the screen scrape, so custom
spinner themes and braille characters in transcript text do not affect the busy
state. Only the state is used; report messages are ignored. Agents that never
report, or clear their status on exit, fall back to screen detection.

hrdx answers the protocol's support query, but ConPTY does not forward that
acknowledgement on Windows, so agents that wait for it are handled per agent:

| Agent | Busy detection |
|---|---|
| codex | OSC 7501 reports, sent without a query |
| zot, pi | OSC 7501 reports; panes start with `ZOT_PROGRAM_STATUS=1` and `PI_PROGRAM_STATUS=1` unless already set (set `0` before launching hrdx to opt out) |
| claude | Terminal title: `◐`/`◑` while working, `✳` when idle; OSC 7501 reports win when they arrive (for example on macOS and Linux) |

## Workspace groups

Groups are optional organisational labels, independent of directories, branches, tabs, and PTY lifetime. Socket clients and approved plugins can place any workspace, including non-Git directories, at an arbitrarily nested `group_path`:

```json
{"id":"1","method":"workspace.create","params":{"path":"/work/game-feature","agent":"shell","group_path":["game","reviews"]}}
{"id":"2","method":"workspace.move","params":{"workspace":"/work/game-feature","group_path":["game","reviews","ready"]}}
{"id":"3","method":"group.list"}
```

`workspace.move` requires an array; `[]` makes a workspace standalone. Names are exact, case-sensitive labels, not filesystem paths: a slash inside a name is literal. Each name must be nonblank, contain no control characters, and have at most 80 Unicode characters; the path is limited to 4096 UTF-8 bytes including one separator byte per component. Missing groups and ancestors appear implicitly and disappear when their last workspace moves or closes. There are no separately stored group objects or group IDs.

`group.list` returns `{"type":"groups","groups":[{"group_path":["game"]},{"group_path":["game","reviews"]},{"group_path":["game","reviews","ready"]}]}` inside the usual `result`. Paths are unique, in first-workspace order, ancestors before descendants. Status keeps the existing flat `workspaces` array and adds optional `group_path`; branch information stays in `branch`. Workspace created, closed, and moved events carry group metadata (omitted for standalone workspaces). Moving replies with `workspace`, `path`, and optional `group_path`. Events remain best-effort; query status or group.list to recover. Separate group lifecycle events are not emitted.

The sidebar adds plain nested headings while keeping the existing workspace, branch, tab and pane rows. Headings do not select, close, or drag anything. Groups and their children follow first-workspace order; dragging a workspace still reorders the underlying workspace list, not its membership. Keyboard workspace cycling and flat status retain that list order. Deep indentation is capped to keep labels visible in narrow or compact sidebars. The selection rail stays at the left edge; only row content is indented. Group placement is RPC/plugin-only; existing keys and context menus are unchanged. The fuzzy finder includes group paths in pane labels and matches group names, so identically named workspaces in different groups remain distinguishable.

Groups persist with the workspace. Old state and clients without group fields remain standalone, and an invalid saved group path falls back to standalone without discarding panes. Moving never recreates a tab, pane, holder session, or split layout.

For an optional Git default, pass `"group_from_git":true` to `workspace.create` without `group_path` (or with null). A repository root or linked worktree uses the primary repository directory basename as its suggested single-component group. An explicit array, including `[]`, always wins. Detection does not execute Git, traverse parent directories, or change branch metadata; unavailable or unrecognised metadata leaves the workspace standalone. Equal repository basenames intentionally share a label: supply an explicit path to distinguish them. The chosen label is persisted, not recomputed on restore or branch changes. Git defaults are opt-in, preserving this revision's existing flat sidebar and Git branch display.

Plugins can use existing scoped status snapshots, actions, and right/bottom docked views to provide alternative organisation controls. `workspace.create` accepts the same group options; `workspace.move` requires its own grant and an exact in-scope workspace path; `group.list` uses `workspace.read` and exposes only groups containing in-scope workspaces. This version adds no template language or sidebar replacement API: host rendering and mouse hit testing remain host-owned.

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

## Durable agents

Experimental. Durable agents run on a [zot continuous](https://www.zot.sh) host instead of inside the pane. The pane is a real PTY running zot's attached TUI as a view of one conversation. The host owns execution, queues, approvals, and recovery, so the work continues when the view closes, when hrdx quits, or when the holder restarts. Everything else keeps working as before: ordinary agents and shells, holder persistence, and resume behavior are unchanged, and durable and ordinary panes can share a workspace, tab, and split layout.

Create one by picking `zot (durable)` at the end of any agent picker (`ctrl+b c`, `ctrl+b t`, the tab bar `+`, or a split menu). It is never the preselected entry, and no existing default turns durable on its own. The first durable agent in a project starts a managed host for that project in the background, with a private store under `continuous/` next to the state file. Each further durable agent in the workspace gets its own conversation. "Open another view" in the pane menu, or the panel, shows the same conversation again.

`ctrl+b D` opens the durable agents panel for the current workspace: every associated conversation with its state and number of open views, even with no pane open, plus actions to open a view, queue or steer a prompt, rename, review pending approvals (allow this call, allow this tool for the conversation, or deny), review recovery holds, show usage, list tasks with abort or cleanup retry, list forks and owned subagents with open view, prompt, and abort actions, show and set the conversation cost budget, change the model (`provider/model`, other configuration such as instructions and tool restrictions is preserved), explicitly abort work, forget a conversation locally (host history is kept), and start or stop managed hosts. Destructive actions ask first. The settings window gains a `durable` section to mute individual notification categories (finished, failed, approval, recovery, host) and pick the host for new durable agents in the current workspace; muting silences sound and notices but keeps the attention dot. The sidebar shows authoritative host state for durable panes: the busy spinner while the host runs work, `queued`, `approve`, `recover`, `failed`, or `offline` when the host cannot be reached. A disconnected host never reads as idle or finished. Finished turns, failures, approval requests, recovery holds, and lost hosts use the notification settings, once per transition, never for history seen at startup.

Lifetime rules:

- Closing a durable pane, tab, or workspace ends only the attached view. It never aborts host work or stops a host. Aborting is an explicit panel or API action.
- Quitting hrdx detaches views like any held pane. Managed hosts keep running independently of the TUI and the holder; `hrdx stop` still manages only the holder.
- On restart each durable pane reattaches to its exact conversation. If the host is missing, the conversation is gone, or the host workspace key now resolves to a different conversation (another store), the pane stays visible with the error instead of starting a fresh agent. "Retry durable binding" in the pane menu tries again.
- A managed host stopped on purpose is not restarted when hrdx reopens; start it from the panel or with `hrdx continuous start`. hrdx never restarts a crashed host in a loop; zot performs its own recovery when the host starts.
- Prompts submitted from hrdx carry a stable request ID. A lost response is reported as unknown, never resubmitted under a new ID.

Manage host profiles without starting the TUI. Profiles live in `continuous-hosts.json` next to the state file; tokens are referenced by file and never copied into state, status output, or logs.

```sh
hrdx continuous hosts                                   # profiles and whether they answer (--json)
hrdx continuous add --id team --address /path/to/host.sock [--token-file FILE]
hrdx continuous add --id remote --address host.example:7000 --tls-ca ca.pem --token-file token
hrdx continuous start --id local-1a2b3c4d5e6f           # managed hosts only
hrdx continuous stop --id local-1a2b3c4d5e6f            # interrupted work stays recoverable
hrdx continuous remove --id team                        # the host and its history are untouched
```

An added external host can be selected per workspace from the panel ("Use host for new durable agents") or with `continuous.workspace_host`. Existing panes keep their binding when the selection changes.

Remote hosts (`host:port`, served by `zot continuous serve --listen`) require TLS 1.3 against the profile's CA bundle and a token; plaintext TCP is accepted only on loopback. Managed hosts are always local. On native Windows the profile's socket path maps to the same private named pipe zot derives, and a stopped managed host is terminated rather than interrupted (zot keeps the interrupted work recoverable either way).

Limitations: durable agents need zot with `zot continuous` and a state location (not `--state ""`). Forks and owned subagents have no workspace key; their views attach by conversation ID, which needs a zot whose `--help` lists `--continuous-conversation`. With an older zot such a view reports "update zot" and nothing else changes. Native prompts attach local files with `@path` tokens (relative to the workspace; `@@` keeps a literal `@`): UTF-8 text and PNG, JPEG, GIF, or WebP images, at most 16 files and 1 MiB per prompt, sent only to hosts that advertise attachment support. Older hrdx releases ignore the durable fields and would relaunch such panes as ordinary zot, so back up the state file before downgrading.

## Socket API

While hrdx runs it serves a control API on a Unix socket next to the state file (`hrdx.sock`), so scripts, editors, and coding agents can inspect and drive a running session. Disable with `--api=false`. The session holder uses `holder.sock` in the same directory.

On macOS and Linux, if the state directory cannot create Unix sockets and `XDG_RUNTIME_DIR` is an absolute, usable directory, sockets fall back to a private `hrdx/` directory under `XDG_RUNTIME_DIR`. The names are `hrdx-<id>.sock` and `holder-<id>.sock`, where `<id>` is the first 16 hex digits of the SHA-256 hash of the absolute state-file path. Different `--state` files therefore get different runtime sockets. Existing socket paths remain the default whenever they work, and an existing live holder is reused even if the API must move. `hrdx plugins` control commands try the legacy API socket first, then the runtime socket, and `hrdx stop` does the same for the holder socket. Runtime sockets last only as long as `XDG_RUNTIME_DIR`, which may be cleared at logout. Native Windows keeps its existing socket paths; WSL uses its own runtime and socket namespace.

The protocol is newline-delimited JSON: send one request per line, receive one response line with the same `id`.

```sh
SOCK="$HOME/Library/Application Support/hrdx/hrdx.sock"   # macOS default
# SOCK="$XDG_CONFIG_HOME/hrdx/hrdx.sock"                  # Linux, or macOS with XDG_CONFIG_HOME set
# If the state directory cannot host sockets, use $XDG_RUNTIME_DIR/hrdx/hrdx-<id>.sock
# on macOS or Linux, with <id> computed from the absolute state-file path as above.
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
| `workspace.create` | Open a directory as a workspace (`path`, optional `agent`, `group_path`, `group_from_git`) |
| `workspace.move` | Set a workspace's organisational `group_path` without restarting panes (`workspace`: name or path) |
| `group.list` | List occupied group paths and their ancestors |
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

Durable agent methods are separate from pane input: `pane.send_text` types into a view, `continuous.submit` admits work to a conversation. They answer after a host round trip that never blocks the UI (up to 45 seconds when a managed host must start). Host error codes such as `conflict`, `busy`, `queue_full`, `not_found`, and `unauthorized` pass through unchanged; `unavailable` means the host could not be reached and `unsupported` means the platform lacks local hosts.

| Method | Effect |
|---|---|
| `continuous.hosts` | Host profiles with `managed`, `connection`, and intentional `stopped` state |
| `continuous.host.start` / `continuous.host.stop` | Start or stop a managed host (`profile`) |
| `continuous.workspace_host` | Host profile for new durable panes in a workspace (`workspace`, `profile`; empty restores the managed host) |
| `continuous.attach` | Open a durable view (`workspace`, optional `profile`, `split`: `right`, `down`, `tab`). Without `conversation` it creates a new durable agent; with one, another view of it (unassociated conversations also need `profile`; without `workspace_key` the view attaches by conversation ID, for forks and subagents). Answers once bound, with `pane_id` |
| `continuous.list` | Conversations on a host (`profile`, optional `after`, `limit`) |
| `continuous.snapshot` | Execution state, queue, run, approvals, recovery, tasks, and usage (`profile`, `conversation`) |
| `continuous.submit` | Admit a prompt (`profile`, `conversation`, `content`, required `request_id`, `policy`: `queue` or `steer`, optional `files`: local paths read by hrdx, relative to the selected workspace). Retrying with the same `request_id` returns the original submission |
| `continuous.submission` | Read one submission (`profile`, `submission`) |
| `continuous.rename` | Set a title (`profile`, `conversation`, `expected_revision`, `title`) |
| `continuous.abort` | Explicitly abort active work and withdraw queued prompts (`profile`, `conversation`) |
| `continuous.approvals` / `continuous.decide` | List pending approvals, or decide one (`approval`, `allow`, `scope`: `call` or `tool`, optional `reason`) |
| `continuous.recovery` / `continuous.unblock` | Preview interrupted runs, or release one hold (`run_id`) so the host applies its recovery plan |
| `continuous.tasks` | Host tasks of a conversation (`profile`, `conversation`) |
| `continuous.task.abort` / `continuous.task.retry_cleanup` | Abort one task (`task`, optional `include_background`), or retry its failed cleanup |
| `continuous.budgets` | Budgets that apply to a conversation, with spend and `exceeded` |
| `continuous.budget.set` | Write a budget (`scope`: `runtime` or `conversation`, `conversation`, `limit_usd`, `limit_tokens`, `reserve_usd`, `window_seconds`, `expected_revision`) |
| `continuous.configure` | Change `provider`, `model`, or `reasoning` of a conversation; empty fields and all other configuration are kept |

```sh
echo '{"id": "8", "method": "continuous.attach", "params": {"workspace": "api", "split": "tab"}}' | nc -U "$SOCK"
echo '{"id": "9", "method": "continuous.submit", "params": {"profile": "local-1a2b3c4d5e6f", "conversation": "<id>", "content": "run the tests", "request_id": "ci-42"}}' | nc -U "$SOCK"
```

`status` marks durable panes with `"mode": "durable"` and a `durable` object (`profile`, `workspace_key`, `conversation`, `title`, `connection`: `connected`, `disconnected`, or `unknown`, `execution`: `ready`, `queued`, `running`, `approval`, `recovery`, `failed`, or `aborted`, `revision`, `observed_at`, `queued`, `approvals`, and `error`). While disconnected, `execution` is the last observed value. Workspaces add `continuous_host` and a `continuous` list of associated conversations with their open `views`. Ordinary panes omit these fields, and `busy` keeps its meaning.

Successful responses are `{"id": "...", "result": {...}}`; failures are `{"id": "...", "error": {"code": "not_found", "message": "..."}}` with codes `not_found`, `invalid_params`, `unknown_method`, `timeout`, and `error`.

After `events.subscribe` the connection stays open and hrdx pushes lines like `{"event": "pane.busy_changed", "data": {"pane_id": 3, "busy": false}}`. Events: `workspace.created`, `workspace.closed`, `workspace.moved`, `pane.created`, `pane.closed`, `pane.busy_changed`, `menu.action`, `continuous.bound` (a durable pane was bound to a conversation), `continuous.state_changed` (`profile`, `conversation`, `state`, `previous`), and `continuous.host_changed` (`profile`, `connected`, `error`), so a script can react the moment an agent finishes instead of polling.

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

Quitting hrdx does not kill your sessions. Pane processes live in a small background process (the session holder) that hrdx starts on demand and talks to over a local socket. Close the TUI, reopen it, and every shell and agent reattaches exactly where it was: running commands keep running, scrollback and screen state are replayed, nothing restarts. The holder is the same `hrdx` binary and uses no resources worth mentioning. When [hrdx Desktop](#desktop-app) starts it, the holder is the `hrdx-desktop` binary in holder mode instead. Both speak the same protocol, so either frontend reattaches to either holder.

The holder serves one hrdx terminal at a time. Starting hrdx while another terminal is attached exits with `already running in another terminal` instead of opening a second, unpersisted session; quit the other terminal first, or pass a different `--state` file for an independent instance. Only a socket with nothing listening is treated as stale and replaced.

Workspaces, panes, split layout, ratios, selection, sidebar collapsed state, and holder session ids are saved automatically (default: `~/Library/Application Support/hrdx/state.json` on macOS, `$XDG_CONFIG_HOME/hrdx/state.json` on Linux, `%AppData%\hrdx\state.json` on Windows). An absolute `XDG_CONFIG_HOME` takes precedence on macOS too, so everything hrdx stores next to the state file (keys, harnesses, themes, plugins, sockets) follows it. An existing `~/Library/Application Support/hrdx` keeps being used until you move it to `$XDG_CONFIG_HOME/hrdx`, so setting the variable never orphans running sessions. Windows ignores the variable. On the next launch the layout is restored and each pane reattaches to its held session. When a held session is gone (rebooted machine, killed holder), the pane starts fresh instead: shell panes get a new shell, and agent panes relaunch resuming their latest session for that directory via the agent's own session store.

`--persist=false` disables the holder (panes die with the TUI, like a plain terminal). `--fresh` skips restoring and cleans up now-unreferenced held sessions; `--state ""` disables persistence entirely.

Stop the holder to restart it or reclaim everything it holds:

```sh
hrdx stop
```

With no held sessions it stops the holder and exits 0, and it exits 0 when no holder is running. If sessions are still held, it lists them (`id`, command, cwd, running state) and asks `Stop them too? [y/N]`. Only `y`/`Y` kills them; anything else, including a bare Enter or end of input, leaves the holder running and exits 1, so a stray key never discards running shells and agents. `hrdx stop` takes no options and targets the default state location. The holder serves one TUI client at a time, so `stop` works while hrdx is closed; with a TUI attached it reports the holder as busy rather than waiting, so quit the TUI (`ctrl+b q`) first. The prompt does not block a TUI from reconnecting. If held sessions change while you decide, `stop` refuses to shut down and asks you to review them again. Socket access errors are reported as failures rather than as an absent holder. Stopping the holder does not edit saved state: the next launch restores the layout and starts fresh panes for the killed sessions.

## Desktop app

Experimental. hrdx Desktop is a second frontend: the same workspaces, groups, tabs, splits, floating panes, agents, and shells in a native window, with terminals rendered by xterm.js. It lives in `desktop/` as a separate Go module and is built with [Wails v2](https://wails.io) and a static Next.js bundle. The `hrdx` CLI, its binary size, and its build requirements are unchanged.

It shares everything with the TUI: `state.json`, the session holder, `keys.json`, `harness.json`, `sounds.json`, and `themes/`. Quit the TUI and open the desktop app, and every pane reattaches with its running process. Quitting the app and starting `hrdx` works the same in reverse. While it runs, the app serves the same socket API on the same path the TUI would.

Only one frontend can use a state file at a time. If the TUI is attached, the app shows `hrdx is already running in another terminal` with Retry and Quit, starts no panes, and writes no state. A second app window for the same state file focuses the existing one.

Build prerequisites: Go 1.26, [bun](https://bun.sh), the Wails v2 CLI (`go install github.com/wailsapp/wails/v2/cmd/wails@v2.15.0`), and the platform webview (WebKit on macOS 12+, WebView2 on Windows 10+, `libwebkit2gtk-4.1-dev` and `libgtk-3-dev` on Linux, where builds need `-tags webkit2_41`).

```sh
cd desktop
make build          # build/bin/hrdx.app on macOS, build/bin/hrdx-desktop elsewhere
make check          # go vet, go test -race, frontend typecheck, lint, tests, and build
```

`hrdx-desktop` accepts `--cwd` (repeatable), `--agent`, `--provider`, `--model`, `--reasoning`, `--continue`, `--zot-bin`, `--pi-bin`, `--claude-bin`, `--codex-bin`, `--shell`, `--state`, `--fresh`, `--api`, `--persist`, and `--version`, with the same meanings and defaults as `hrdx`. Without `--cwd` and saved workspaces it opens empty with a New workspace prompt. That dialog has an Arguments field for the first pane's agent (see [Agent arguments](#agent-arguments)). On macOS it reads `PATH` from your login shell once at startup, so agents installed through Homebrew or `~/.local/bin` resolve as they do in a terminal.

Keys follow the TUI: the prefix (`ctrl+b`, remappable in `keys.json`) and every prefix action work, including `tab` and `shift+tab` repeats until `esc`. Native shortcuts are layered on top: `cmd+q` quit, `cmd+,` settings, `cmd+w` close pane, `cmd+t` new tab, `cmd+k` find, `cmd+c` and `cmd+v` copy and paste (`ctrl+shift+` on Linux and Windows, so plain control keys stay terminal input). Escape and control keys are never reserved. Right-click opens the same scoped context menus, with `menu.register` entries appended. Dividers drag, and a double-click resets one to an even split.

The Files and Changes buttons at the right of the tab bar open the same read-only panels as `ctrl+b e` and `ctrl+b g` (see [Files and Changes](#files-and-changes)): a folder tree with highlighted previews, and the uncommitted changes with highlighted diffs.

Nested repositories work as in the TUI and share its settings: turn on **Nested repositories** under Behavior in settings and pick a search depth of 1 to 3 levels (saved as `subrepo_depth`). Each workspace then lists the git repositories below it with branch, changed files (`●`), ahead (`↑`), behind (`↓`), and conflicts (`!`), refreshed every 15 seconds. Clicking or right-clicking a repository offers New tab here, Split right here, Split down here, Open as workspace, and Hide folder. The workspace menu offers Show hidden folders (n). Hidden folders are the same `hidden_subrepos` list the TUI uses, and `status` reports the same optional `subrepos` array.

Desktop-only preferences live in `desktop.json` beside `state.json`: window geometry, `font_family`, `font_size`, `line_height`, `cursor_blink`, `scrollback_lines`, and the sidebar width in pixels (the TUI's `sidebar_width` in columns is left alone). A missing or corrupt file means defaults.

Limits in this version:

- Plugins and durable agents are not available. `plugins.*` and `continuous.*` socket methods answer `unknown_method`. Durable panes from a TUI state file show a placeholder and are saved back unchanged, along with every other field the app does not use.
- Only the visible tab has live terminals. Switching back to a tab, or a webview reload, rebuilds its terminals from a 2 MiB per-pane replay buffer, which bounds the scrollback you get back.
- xterm.js has no kitty keyboard protocol. When a program enables it, `shift+enter`, `ctrl+enter`, `alt+enter`, modified `tab`, and `ctrl+` punctuation are sent as CSI u sequences. `ctrl+0` to `ctrl+9` are always sent as CSI u, as in the TUI, so agent shortcuts such as zot's quick model slots work. Other keys use legacy encodings. On macOS, `cmd+backspace` deletes to the start of the line, `cmd+left` and `cmd+right` jump to the start and end, and `option+backspace`, `option+left`, and `option+right` work by word, as in Terminal and Ghostty.
- Color queries are answered with the colors the app draws: xterm.js's default ANSI palette, `#e4e4e7` text, and the `#18181b` pane background.
- Pasted and dropped text is wrapped in bracketed paste markers whenever the program enabled bracketed paste, and always for agents, as in the TUI. A multi-line paste into an agent arrives as one input instead of one submitted line at a time, even after the pane reattached.
- On macOS, the notification setting shows a native banner when an agent you are not looking at finishes or needs input ("website: Claude Code needs your input"). Clicking it focuses that pane. The Dock icon shows the number of panes waiting for you and bounces while the app is in the background. macOS asks for permission on first launch. Linux and Windows keep the sound and in-app markers only.
- Every release also attaches desktop builds: `hrdx-desktop_<version>_macos_universal.dmg`, `hrdx-desktop_<version>_windows_amd64.zip`, and `hrdx-desktop_<version>_linux_amd64.tar.gz` (needs WebKitGTK 4.1). Builds are unsigned unless signing secrets are configured, so macOS asks to confirm the first launch (right-click the app, then Open).
- The desktop app checks GitHub for a newer release at launch, every hour, and when its window regains focus (at most every ten minutes). When one exists, a notice offers "Update now and restart": it downloads the release asset for your platform, verifies it against `desktop-checksums-<platform>.txt`, replaces the installed app (the whole `hrdx.app` bundle on macOS, the executable on Windows and Linux), and relaunches. Panes detach before the restart and reattach afterwards, so sessions keep running. The app must live in a folder you can write to, such as `/Applications`. Dev builds (version `0.0.0`) never offer updates.

## Development

```sh
make check
```

Windows without `make` on `PATH`: `go vet ./... && gofmt -l . && go test ./...`.

The desktop app is a nested module: `go vet ./...` and `go test ./...` at the repository root skip it. Run `make check` in `desktop/` when changing it or any shared `internal/` package it uses.

## License

MIT
