package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"runtime"
	"time"

	"github.com/patriceckhart/hrdx/internal/paths"
	"github.com/patriceckhart/hrdx/internal/plugin"
	"github.com/patriceckhart/hrdx/internal/state"
)

func runPluginControl(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("plugins "+args[0], flag.ContinueOnError)
	flags.SetOutput(stderr)
	statePath := flags.String("state", state.DefaultPath(), "state location of the running hrdx instance")
	id := flags.String("id", "", "approved plugin ID")
	if err := flags.Parse(args[1:]); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 || *statePath == "" || (args[0] != "status" && !plugin.ValidIdentifier(*id)) || (args[0] == "status" && *id != "") {
		fmt.Fprintln(stderr, "hrdx plugins: use status, or start/stop/restart/reload --id ID, with a nonempty --state location")
		return 2
	}
	connection, err := net.DialTimeout("unix", paths.Legacy(*statePath).API, 5*time.Second)
	if err != nil {
		if fallback, pathErr := paths.Runtime(*statePath, runtime.GOOS, os.Getenv("XDG_RUNTIME_DIR")); pathErr == nil {
			connection, err = net.DialTimeout("unix", fallback.API, 5*time.Second)
		}
	}
	if err != nil {
		fmt.Fprintln(stderr, "hrdx plugins: cannot connect to the running instance's control socket")
		return 1
	}
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(5 * time.Second))
	method := "plugins.control"
	params := map[string]string{"plugin": *id, "action": args[0]}
	if args[0] == "status" {
		method = "plugins.status"
		params = nil
	}
	if json.NewEncoder(connection).Encode(map[string]any{"id": "plugins", "method": method, "params": params}) != nil {
		return 1
	}
	var response struct {
		ID     string          `json:"id"`
		Result json.RawMessage `json:"result"`
		Error  *plugin.Problem `json:"error,omitempty"`
	}
	if json.NewDecoder(io.LimitReader(connection, 1<<20)).Decode(&response) != nil || response.ID != "plugins" {
		fmt.Fprintln(stderr, "hrdx plugins: invalid or timed-out control response")
		return 1
	}
	if json.NewEncoder(stdout).Encode(response) != nil {
		return 1
	}
	if response.Error != nil {
		return 1
	}
	return 0
}
