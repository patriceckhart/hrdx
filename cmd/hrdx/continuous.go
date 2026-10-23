package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/patriceckhart/hrdx/internal/continuous"
	"github.com/patriceckhart/hrdx/internal/state"
)

const continuousUsage = `usage: hrdx continuous <command> [options]

Manage zot continuous host profiles used by durable agent panes. These
commands never start the TUI and never delete conversation history.

commands:
  hosts                       list host profiles and whether they answer
  add --id ID --address ADDR  register an external host: a socket path, or
      [--name NAME]           host:port (remote hosts need --tls-ca and
      [--token-file FILE]     --token-file; plaintext only on loopback)
      [--tls-ca FILE]
  remove --id ID              forget a profile (the host and its store are untouched)
  start --id ID               start a managed host
  stop --id ID                stop a managed host (running work stays recoverable)

common options:
  --state PATH                state file whose directory holds the profiles
`

// runContinuous handles "hrdx continuous". It is separate from "hrdx stop",
// which keeps its holder-only meaning.
func runContinuous(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		fmt.Fprint(stdout, continuousUsage)
		if len(args) == 0 {
			return 2
		}
		return 0
	}
	command := args[0]
	flags := flag.NewFlagSet("continuous "+command, flag.ContinueOnError)
	flags.SetOutput(stderr)
	statePath := flags.String("state", state.DefaultPath(), "state file location")
	id := flags.String("id", "", "profile id")
	address := flags.String("address", "", "host socket path")
	name := flags.String("name", "", "display name")
	tokenFile := flags.String("token-file", "", "file holding the host token")
	tlsCA := flags.String("tls-ca", "", "PEM CA bundle for a remote host")
	jsonOut := flags.Bool("json", false, "print JSON")
	zotBin := flags.String("zot-bin", "", "path to the zot binary")
	if err := flags.Parse(args[1:]); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "hrdx continuous: unexpected arguments:", strings.Join(flags.Args(), " "))
		return 2
	}
	if *statePath == "" {
		fmt.Fprintln(stderr, "hrdx continuous: a state location is required")
		return 2
	}
	dir := filepath.Dir(*statePath)
	profiles, problems := continuous.LoadProfiles(dir)
	for _, problem := range problems {
		fmt.Fprintln(stderr, "hrdx continuous:", problem)
	}
	find := func() (continuous.Profile, bool) {
		for _, profile := range profiles {
			if profile.ID == *id {
				return profile, true
			}
		}
		fmt.Fprintf(stderr, "hrdx continuous: no host profile %q\n", *id)
		return continuous.Profile{}, false
	}

	switch command {
	case "hosts":
		type row struct {
			ID      string `json:"id"`
			Name    string `json:"name,omitempty"`
			Address string `json:"address"`
			Managed bool   `json:"managed"`
			Stopped bool   `json:"stopped,omitempty"`
			Status  string `json:"status"`
			Error   string `json:"error,omitempty"`
		}
		rows := []row{}
		for _, profile := range profiles {
			current := row{ID: profile.ID, Name: profile.Name, Address: profile.Address, Managed: profile.Managed,
				Stopped: continuous.IntentionallyStopped(profile), Status: "running"}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			if err := continuous.Probe(ctx, profile); err != nil {
				current.Status, current.Error = "unreachable", err.Error()
			}
			cancel()
			rows = append(rows, current)
		}
		if *jsonOut {
			_ = json.NewEncoder(stdout).Encode(map[string]any{"hosts": rows})
			return 0
		}
		if len(rows) == 0 {
			fmt.Fprintln(stdout, "No continuous host profiles.")
			return 0
		}
		for _, current := range rows {
			kind := "external"
			if current.Managed {
				kind = "managed"
			}
			line := fmt.Sprintf("%s\t%s\t%s\t%s", current.ID, kind, current.Status, current.Address)
			if current.Stopped {
				line += "\t(stopped intentionally)"
			}
			fmt.Fprintln(stdout, line)
		}
		return 0
	case "add":
		if *id == "" || *address == "" {
			fmt.Fprintln(stderr, "hrdx continuous add: --id and --address are required")
			return 2
		}
		addr := *address
		var err error
		if !continuous.IsRemoteAddress(addr) && !strings.HasPrefix(addr, `\\.\pipe\`) {
			if addr, err = filepath.Abs(addr); err != nil {
				fmt.Fprintln(stderr, "hrdx continuous add:", err)
				return 1
			}
		}
		profile := continuous.Profile{ID: *id, Name: *name, Address: addr}
		for _, file := range []struct {
			value string
			into  *string
		}{{*tokenFile, &profile.TokenFile}, {*tlsCA, &profile.TLSCA}} {
			if file.value == "" {
				continue
			}
			if *file.into, err = filepath.Abs(file.value); err != nil {
				fmt.Fprintln(stderr, "hrdx continuous add:", err)
				return 1
			}
		}
		if err := profile.Validate(); err != nil {
			fmt.Fprintln(stderr, "hrdx continuous add:", err)
			return 2
		}
		for _, existing := range profiles {
			if existing.ID == *id && existing.Managed {
				fmt.Fprintf(stderr, "hrdx continuous add: %q is a managed profile and cannot be replaced\n", *id)
				return 1
			}
		}
		if err := continuous.UpsertProfile(dir, profile); err != nil {
			fmt.Fprintln(stderr, "hrdx continuous add:", err)
			return 1
		}
		fmt.Fprintf(stdout, "Added host profile %s. Restart hrdx to use it.\n", *id)
		return 0
	case "remove":
		if *id == "" {
			fmt.Fprintln(stderr, "hrdx continuous remove: --id is required")
			return 2
		}
		if err := continuous.RemoveProfile(dir, *id); err != nil {
			fmt.Fprintln(stderr, "hrdx continuous remove:", err)
			return 1
		}
		fmt.Fprintf(stdout, "Removed host profile %s. Its host and conversation history are untouched.\n", *id)
		return 0
	case "start":
		profile, ok := find()
		if !ok {
			return 1
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := continuous.StartManaged(ctx, profile, *zotBin); err != nil {
			fmt.Fprintln(stderr, "hrdx continuous start:", err)
			return 1
		}
		fmt.Fprintf(stdout, "Host %s is running.\n", profile.ID)
		return 0
	case "stop":
		profile, ok := find()
		if !ok {
			return 1
		}
		if err := continuous.StopManaged(profile); err != nil {
			fmt.Fprintln(stderr, "hrdx continuous stop:", err)
			return 1
		}
		fmt.Fprintf(stdout, "Stopped host %s. Interrupted work stays recoverable in its store.\n", profile.ID)
		return 0
	}
	fmt.Fprint(stderr, continuousUsage)
	return 2
}
