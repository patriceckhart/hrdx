package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/patriceckhart/hrdx/internal/plugin"
	"github.com/patriceckhart/hrdx/internal/state"
)

// runPlugins is deliberately separate from TUI startup. Inventory never loads
// workspace state, connects to a holder, starts an updater, or executes code.
func runPlugins(args []string, stdout, stderr io.Writer) int {
	usage := func() {
		fmt.Fprintln(stderr, "usage: hrdx plugins <list|doctor|approve|inspect|enable|disable|revoke|status|start|stop|restart|reload> [options]")
	}
	if len(args) == 0 {
		usage()
		return 2
	}
	if args[0] == "--help" || args[0] == "-h" {
		usage()
		return 0
	}
	switch args[0] {
	case "status", "start", "stop", "restart", "reload":
		return runPluginControl(args, stdout, stderr)
	case "approve", "inspect", "enable", "disable", "revoke":
		return runPluginManagement(args, stdout, stderr)
	}
	if args[0] != "list" && args[0] != "doctor" {
		usage()
		return 2
	}
	flags := flag.NewFlagSet("plugins "+args[0], flag.ContinueOnError)
	flags.SetOutput(stderr)
	statePath := flags.String("state", state.DefaultPath(), "discover plugins next to this state file (empty disables the default root)")
	asJSON := flags.Bool("json", false, "print the experimental inventory as JSON")
	var directories dirList
	flags.Var(&directories, "dir", "additional directory containing plugin packages (repeatable, no precedence)")
	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 {
		usage()
		return 2
	}
	roots := []plugin.Root{}
	if *statePath != "" {
		roots = append(roots, plugin.Root{Path: filepath.Join(filepath.Dir(*statePath), "plugins"), Optional: true})
	}
	for _, directory := range directories {
		roots = append(roots, plugin.Root{Path: directory})
	}
	inventory := plugin.Discover(roots)
	if args[0] == "doctor" && *statePath != "" {
		_, diagnostics := plugin.LoadRegistrations(filepath.Dir(*statePath))
		inventory.Diagnostics = append(inventory.Diagnostics, diagnostics...)
	}
	var err error
	if *asJSON {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		err = encoder.Encode(inventory)
	} else {
		var output strings.Builder
		fmt.Fprintln(&output, "Read-only plugin inventory. This command does not enable or execute plugins.")
		if len(inventory.Packages) == 0 {
			fmt.Fprintln(&output, "No plugin packages found.")
		}
		for _, entry := range inventory.Packages {
			status := "invalid"
			if entry.Valid {
				status = "valid (inventory only)"
			}
			// Quoted fields prevent untrusted paths and labels from injecting
			// terminal controls into human-readable diagnostics.
			fmt.Fprintf(&output, "%s  id=%q version=%q name=%q path=%q\n", status, entry.ID, entry.Version, entry.Name, entry.Path)
			for _, issue := range entry.Diagnostics {
				fmt.Fprintf(&output, "  %s: %s\n", issue.Code, issue.Message)
			}
		}
		for _, issue := range inventory.Diagnostics {
			fmt.Fprintf(&output, "%s: %s (path=%q)\n", issue.Code, issue.Message, issue.Path)
		}
		_, err = io.WriteString(stdout, output.String())
	}
	if err != nil {
		fmt.Fprintln(stderr, "hrdx plugins: cannot write inventory")
		return 1
	}
	if args[0] == "doctor" && inventory.HasProblems() {
		return 1
	}
	return 0
}
