package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/patriceckhart/hrdx/internal/plugin"
	"github.com/patriceckhart/hrdx/internal/state"
)

func runPluginManagement(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("plugins "+args[0], flag.ContinueOnError)
	flags.SetOutput(stderr)
	statePath := flags.String("state", state.DefaultPath(), "state file location used to select the approval directory")
	packagePath := flags.String("package", "", "explicit package directory for approval")
	id := flags.String("id", "", "plugin ID for inspect, enable, disable, or revoke")
	trust := flags.Bool("trust", false, "approve executing trusted code with your OS permissions (not a sandbox)")
	instance := flags.Bool("instance", false, "allow granted workspace operations across the entire instance")
	var grants, workspaces, settings dirList
	flags.Var(&grants, "grant", "explicit capability grant (repeatable)")
	flags.Var(&workspaces, "workspace", "exact workspace directory scope (repeatable)")
	flags.Var(&settings, "set", "manifest-declared configuration value as key=value (repeatable, JSON for bool and int)")
	if err := flags.Parse(args[1:]); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 || *statePath == "" {
		fmt.Fprintln(stderr, "hrdx plugins: a nonempty --state location and no positional arguments are required")
		return 2
	}
	base := filepath.Dir(*statePath)
	var approval state.PluginApproval
	var err error
	if args[0] == "approve" {
		if !*trust || *packagePath == "" || *id != "" {
			fmt.Fprintln(stderr, "usage: hrdx plugins approve --package PATH --trust [--grant CAPABILITY ...] [--workspace PATH ... | --instance]")
			fmt.Fprintln(stderr, "Trust permits arbitrary executable code with your OS permissions. Capability grants are not sandboxing.")
			return 2
		}
		entry := plugin.InspectPackage(*packagePath)
		var config map[string]any
		config, err = parsePluginSettings(entry, settings)
		if err == nil {
			approval, err = plugin.Approve(entry, grants, workspaces, *instance, config)
		}
	} else {
		if *id == "" || plugin.ApprovalPath(base, *id) == "" || *packagePath != "" || *trust || *instance || len(grants) != 0 || len(workspaces) != 0 || len(settings) != 0 {
			fmt.Fprintln(stderr, "hrdx plugins: use --id with inspect, enable, disable, or revoke. Use approve to change grants or scopes.")
			return 2
		}
		approval, err = state.LoadPluginApproval(plugin.ApprovalPath(base, *id))
		if err == nil && approval.ID != *id {
			err = fmt.Errorf("approval identity does not match")
		}
		if err == nil && args[0] == "inspect" {
			encoder := json.NewEncoder(stdout)
			encoder.SetIndent("", "  ")
			if encoder.Encode(approval) != nil {
				return 1
			}
			return 0
		}
		if err == nil && args[0] == "revoke" {
			if err := os.Remove(plugin.ApprovalPath(base, *id)); err != nil {
				fmt.Fprintln(stderr, "hrdx plugins: cannot remove approval")
				return 1
			}
			if _, err := fmt.Fprintf(stdout, "Plugin %q approval revoked. Running instances stop it on their next 500ms check. Private plugin data was retained.\n", *id); err != nil {
				return 1
			}
			return 0
		}
		if err == nil {
			approval.Enabled = args[0] == "enable"
			if approval.Enabled {
				err = plugin.VerifyApproval(plugin.InspectPackage(approval.Path), approval)
			}
		}
	}
	if err != nil {
		var issue *plugin.Problem
		if errors.As(err, &issue) {
			fmt.Fprintf(stderr, "hrdx plugins: %s: %s\n", issue.Code, issue.Message)
		} else {
			fmt.Fprintln(stderr, "hrdx plugins: cannot read the existing approval")
		}
		return 1
	}
	if err := state.SavePluginApproval(plugin.ApprovalPath(base, approval.ID), approval); err != nil {
		fmt.Fprintln(stderr, "hrdx plugins: cannot save approval")
		return 1
	}
	_, err = fmt.Fprintf(stdout, "Plugin %q approval saved (enabled=%t). Running instances revoke changed approvals on their next 500ms check. Restart hrdx to load new approvals.\n", approval.ID, approval.Enabled)
	if err != nil {
		return 1
	}
	return 0
}

// parsePluginSettings decodes key=value pairs against the manifest's declared
// option types. Strings are taken literally, bool and int values are JSON.
func parsePluginSettings(entry plugin.Package, settings []string) (map[string]any, error) {
	if len(settings) == 0 {
		return nil, nil
	}
	if entry.Manifest == nil {
		return nil, &plugin.Problem{Code: "invalid_config", Message: "package must pass validation before configuration"}
	}
	config := make(map[string]any, len(settings))
	for _, setting := range settings {
		key, raw, ok := strings.Cut(setting, "=")
		if !ok {
			return nil, &plugin.Problem{Code: "invalid_config", Message: "use --set key=value"}
		}
		var value any = raw
		for _, option := range entry.Manifest.Config {
			if option.Key != key || option.Type == "string" {
				continue
			}
			if err := json.Unmarshal([]byte(raw), &value); err != nil {
				return nil, &plugin.Problem{Code: "invalid_config", Message: "bool and int values must be JSON literals"}
			}
		}
		config[key] = value
	}
	return config, nil
}
