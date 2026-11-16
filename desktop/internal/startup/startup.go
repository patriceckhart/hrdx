// Package startup assembles hrdx Desktop's configuration from flags and the
// environment, without touching the GUI.
package startup

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/patriceckhart/hrdx/internal/paths"
	"github.com/patriceckhart/hrdx/internal/state"
)

// Options are the parsed command-line flags. Names and defaults match hrdx.
type Options struct {
	CWDs      []string
	Agent     string
	Provider  string
	Model     string
	Reasoning string
	Resume    bool
	AgentBins map[string]string
	Shell     string
	StatePath string
	Fresh     bool
	API       bool
	Persist   bool
	Version   bool
}

type dirList []string

func (d *dirList) String() string { return fmt.Sprint([]string(*d)) }
func (d *dirList) Set(value string) error {
	*d = append(*d, value)
	return nil
}

// Parse reads the subset of hrdx flags that apply to the desktop app.
func Parse(args []string, stderr io.Writer) (Options, error) {
	flags := flag.NewFlagSet("hrdx-desktop", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var cwd dirList
	var zotBin, piBin, claudeBin, codexBin string
	options := Options{}
	flags.Var(&cwd, "cwd", "project directory to open as a workspace (repeatable)")
	flags.StringVar(&options.Agent, "agent", "zot", "default agent for new panes: zot, pi, claude, codex, or a custom harness kind")
	flags.StringVar(&options.Provider, "provider", "", "zot provider (zot panes only)")
	flags.StringVar(&options.Model, "model", "", "zot model (zot panes only)")
	flags.StringVar(&options.Reasoning, "reasoning", "", "zot reasoning level (zot panes only)")
	flags.StringVar(&zotBin, "zot-bin", "", "path to the zot binary")
	flags.StringVar(&piBin, "pi-bin", "", "path to the pi binary")
	flags.StringVar(&claudeBin, "claude-bin", "", "path to the claude binary")
	flags.StringVar(&codexBin, "codex-bin", "", "path to the codex binary")
	flags.StringVar(&options.Shell, "shell", paths.DefaultShell(), "shell for shell panes")
	flags.StringVar(&options.StatePath, "state", state.DefaultPath(), "state file for workspace persistence (empty disables)")
	flags.BoolVar(&options.Resume, "continue", false, "resume each agent's latest session")
	flags.BoolVar(&options.Fresh, "fresh", false, "ignore saved workspaces and start clean")
	flags.BoolVar(&options.API, "api", true, "serve the control API on a unix socket")
	flags.BoolVar(&options.Persist, "persist", true, "keep pane processes alive across restarts via the session holder")
	flags.BoolVar(&options.Version, "version", false, "print the version and exit")
	// macOS passes -psn_* to apps launched from Finder on older systems.
	filtered := args[:0:0]
	for _, arg := range args {
		if !strings.HasPrefix(arg, "-psn_") {
			filtered = append(filtered, arg)
		}
	}
	if err := flags.Parse(filtered); err != nil {
		return Options{}, err
	}
	options.CWDs = cwd
	options.AgentBins = map[string]string{"zot": zotBin, "pi": piBin, "claude": claudeBin, "codex": codexBin}
	return options, nil
}

// ZotArgs assembles provider flags for zot panes, as hrdx does.
func (o Options) ZotArgs() []string {
	var args []string
	if o.Provider != "" {
		args = append(args, "--provider", o.Provider)
	}
	if o.Model != "" {
		args = append(args, "--model", o.Model)
	}
	if o.Reasoning != "" {
		args = append(args, "--reasoning", o.Reasoning)
	}
	if o.Resume {
		args = append(args, "--continue")
	}
	return args
}

// ShellArgs mirrors the TUI: Unix shells start as login shells.
func ShellArgs(goos string) []string {
	if goos == "windows" {
		return nil
	}
	return []string{"-l"}
}

// LoginPath asks the user's login shell for PATH. GUI apps on macOS do not
// inherit it, so agent binaries in ~/.local/bin or Homebrew would be missing.
// It returns "" on Windows, on timeout, or on any failure.
func LoginPath(goos string, shell string, timeout time.Duration) string {
	if goos == "windows" || shell == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	// A marker isolates PATH from anything the profile prints.
	command := exec.CommandContext(ctx, shell, "-l", "-c", `printf '\n__HRDX_PATH__%s__HRDX_END__' "$PATH"`)
	command.Stdin = nil
	command.Stderr = nil
	output, err := command.Output()
	if err != nil {
		return ""
	}
	return extractPath(string(output))
}

func extractPath(output string) string {
	start := strings.LastIndex(output, "__HRDX_PATH__")
	if start < 0 {
		return ""
	}
	rest := output[start+len("__HRDX_PATH__"):]
	end := strings.Index(rest, "__HRDX_END__")
	if end < 0 {
		return ""
	}
	return strings.TrimSpace(rest[:end])
}

// ApplyLoginPath sets PATH from the login shell when it yields one. Panes
// inherit the process environment through term.PaneEnv, so this also sets
// the PATH that panes see.
func ApplyLoginPath(shell string) {
	if path := LoginPath(runtime.GOOS, shell, 3*time.Second); path != "" {
		_ = os.Setenv("PATH", path)
	}
}
