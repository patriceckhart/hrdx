package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/patriceckhart/hrdx/internal/api"
	"github.com/patriceckhart/hrdx/internal/holder"
	"github.com/patriceckhart/hrdx/internal/state"
	"github.com/patriceckhart/hrdx/internal/ui"
	"github.com/patriceckhart/hrdx/internal/update"
)

// Injected at build time via -ldflags "-X main.version=...". See
// .goreleaser.yaml for the release build. 0.0.0 is the pre-release
// placeholder for local / untagged builds.
var (
	version = "0.0.0"
	commit  = ""
	date    = ""
)

// resolvedVersion falls back to the module version embedded by Go when
// hrdx is installed with "go install ...@version". Release archives
// still use the version injected by GoReleaser.
func resolvedVersion() string {
	if version != "" && version != "0.0.0" {
		return version
	}
	info, ok := debug.ReadBuildInfo()
	if !ok || info.Main.Version == "" || info.Main.Version == "(devel)" {
		return version
	}
	return strings.TrimPrefix(info.Main.Version, "v")
}

// defaultShell picks the shell for shell panes when --shell is unset.
func defaultShell() string {
	return defaultShellFor(runtime.GOOS, os.Getenv, exec.LookPath)
}

// defaultShellFor takes its platform dependencies as arguments so the Windows
// selection rules can be tested on every host.
func defaultShellFor(goos string, getenv func(string) string, lookPath func(string) (string, error)) string {
	shell := getenv("SHELL")
	if goos != "windows" {
		if shell != "" {
			return shell
		}
		return "/bin/sh"
	}

	// Git Bash exports SHELL=/usr/bin/bash, but that MSYS path cannot be
	// launched by a native Windows hrdx process. Keep SHELL when Windows can
	// actually resolve it (including an absolute Git for Windows path).
	if shell != "" {
		if _, err := lookPath(shell); err == nil {
			return shell
		}
	}
	if comspec := getenv("COMSPEC"); comspec != "" {
		return comspec
	}
	return "powershell.exe"
}

func fullVersion() string {
	v := resolvedVersion()
	if commit != "" {
		short := commit
		if len(short) > 7 {
			short = short[:7]
		}
		v += " (" + short
		if date != "" {
			v += ", " + date
		}
		v += ")"
	}
	return v
}

type paths []string

func (p *paths) String() string { return fmt.Sprint([]string(*p)) }
func (p *paths) Set(value string) error {
	*p = append(*p, value)
	return nil
}

func main() {
	// Subcommand routing before flag parsing: `hrdx update` and
	// `hrdx --version` never start the TUI.
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "update":
			var err error
			switch {
			case len(os.Args) > 2 && os.Args[2] == "--check":
				err = update.RunCheck(resolvedVersion())
			case len(os.Args) > 2:
				fmt.Fprintln(os.Stderr, "usage: hrdx update [--check]")
				os.Exit(2)
			default:
				err = update.Run(resolvedVersion())
			}
			if err != nil {
				fmt.Fprintln(os.Stderr, "hrdx:", err)
				os.Exit(1)
			}
			return
		case "--version", "-v", "version":
			fmt.Println("hrdx " + fullVersion())
			return
		case "--holder":
			// Session holder mode: a background process owning every
			// pane's PTY so sessions survive TUI restarts.
			holderFlags := flag.NewFlagSet("holder", flag.ExitOnError)
			holderSocket := holderFlags.String("holder-socket", "", "unix socket to serve on")
			_ = holderFlags.Parse(os.Args[2:])
			if *holderSocket == "" {
				fmt.Fprintln(os.Stderr, "hrdx: --holder requires --holder-socket")
				os.Exit(2)
			}
			if err := holder.NewServer(*holderSocket, resolvedVersion()).Run(); err != nil {
				fmt.Fprintln(os.Stderr, "hrdx holder:", err)
				os.Exit(1)
			}
			return
		}
	}

	var cwd paths
	var agent, provider, model, reasoning, shell, statePath string
	var zotBin, piBin, claudeBin, codexBin, worktreeCmd string
	var resume, fresh, apiOn, persistOn bool
	flag.Var(&cwd, "cwd", "project directory to open as a workspace (repeatable)")
	flag.StringVar(&agent, "agent", "zot", "default agent for new panes: zot, pi, claude, codex, or a custom harness kind")
	flag.StringVar(&provider, "provider", "", "zot provider (zot panes only)")
	flag.StringVar(&model, "model", "", "zot model (zot panes only)")
	flag.StringVar(&reasoning, "reasoning", "", "zot reasoning level (zot panes only)")
	flag.StringVar(&zotBin, "zot-bin", "", "path to the zot binary")
	flag.StringVar(&piBin, "pi-bin", "", "path to the pi binary")
	flag.StringVar(&claudeBin, "claude-bin", "", "path to the claude binary")
	flag.StringVar(&codexBin, "codex-bin", "", "path to the codex binary")
	flag.StringVar(&shell, "shell", defaultShell(), "shell for shell panes")
	flag.StringVar(&worktreeCmd, "worktree-create-cmd", "", "command template for creating worktrees ({name}, {path}, {repo})")
	flag.StringVar(&statePath, "state", state.DefaultPath(), "state file for workspace persistence (empty disables)")
	flag.BoolVar(&resume, "continue", false, "resume each agent's latest session")
	flag.BoolVar(&fresh, "fresh", false, "ignore saved workspaces and start clean")
	flag.BoolVar(&apiOn, "api", true, "serve the control API on a unix socket next to the state file")
	flag.BoolVar(&persistOn, "persist", true, "keep pane processes alive across restarts via the session holder")
	flag.Parse()

	saved := state.State{}
	if !fresh {
		loaded, err := state.Load(statePath)
		if err != nil {
			fmt.Fprintln(os.Stderr, "hrdx: ignoring unreadable state:", err)
		} else {
			saved = loaded
		}
	}

	if len(cwd) == 0 && len(saved.Workspaces) == 0 {
		current, err := os.Getwd()
		if err != nil {
			fmt.Fprintln(os.Stderr, "hrdx:", err)
			os.Exit(1)
		}
		cwd = append(cwd, current)
	}

	var zotArgs []string
	if provider != "" {
		zotArgs = append(zotArgs, "--provider", provider)
	}
	if model != "" {
		zotArgs = append(zotArgs, "--model", model)
	}
	if reasoning != "" {
		zotArgs = append(zotArgs, "--reasoning", reasoning)
	}
	if resume {
		zotArgs = append(zotArgs, "--continue")
	}

	config := ui.Config{
		DefaultAgent: agent,
		AgentBins: map[string]string{
			"zot":    zotBin,
			"pi":     piBin,
			"claude": claudeBin,
			"codex":  codexBin,
		},
		ZotArgs:           zotArgs,
		Shell:             shell,
		WorktreeCreateCmd: worktreeCmd,
		Version:           resolvedVersion(),
	}
	if statePath != "" {
		config.CacheDir = filepath.Dir(statePath)
	}
	modelUI := ui.New(config, cwd, statePath, saved)
	events := api.NewBroadcaster()
	modelUI.SetEventBroadcaster(events)

	// Session holder: pane processes live in a small background process
	// and survive TUI restarts. Attach to a running holder or spawn one.
	var holderClient *holder.Client
	if persistOn && statePath != "" {
		holderSocket := filepath.Join(filepath.Dir(statePath), "holder.sock")
		client, err := holder.ConnectOrSpawn(holderSocket, resolvedVersion())
		if err != nil {
			fmt.Fprintln(os.Stderr, "hrdx: session holder unavailable, panes will not persist:", err)
		} else {
			holderClient = client
			modelUI.SetHolder(client)
		}
	}
	// WithReportFocus: after system sleep the terminal's screen contents
	// and the renderer's cache can disagree; the focus-regained event on
	// wake triggers a full repaint (see FocusMsg in the update loop).
	// The cursor sink keeps the hardware cursor at the focused input point
	// so IME and dead-key composition previews appear where the user types.
	cursorSink := ui.NewCursorSink()
	modelUI.SetCursorSink(cursorSink)
	program := tea.NewProgram(modelUI, tea.WithAltScreen(), tea.WithMouseCellMotion(), tea.WithReportFocus(),
		tea.WithOutput(ui.NewCursorOutput(os.Stdout, cursorSink)))

	if apiOn && statePath != "" {
		socket := filepath.Join(filepath.Dir(statePath), "hrdx.sock")
		server, err := ui.StartAPIServer(socket, func(request api.Request) {
			program.Send(request)
		}, events)
		if err != nil {
			fmt.Fprintln(os.Stderr, "hrdx: api disabled:", err)
		} else {
			defer server.Close()
		}
	}

	if holderClient != nil {
		holderClient.SetExitHandler(func(session int64) {
			program.Send(ui.HolderExitMsg{Session: session})
		})
	}

	if _, err := program.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "hrdx:", err)
		os.Exit(1)
	}
}
