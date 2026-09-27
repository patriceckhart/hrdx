// Package state persists hrdx workspaces to disk so a restart restores the
// same workspaces, panes, and layout. Running processes cannot survive a
// restart; agent panes are relaunched with their resume flag so each agent
// resumes its latest session for that directory.
package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
)

type Pane struct {
	Kind string `json:"kind"`
	Name string `json:"name"`

	// Session is the holder session id owning this pane's PTY; 0 when
	// the pane was not holder-backed. On restore hrdx reattaches to the
	// session instead of starting a new process.
	Session int64 `json:"session,omitempty"`
}

// Node mirrors the split tree. Leaf nodes reference a pane by its index in
// the workspace's pane list.
type Node struct {
	Pane     *int    `json:"pane,omitempty"`
	Vertical bool    `json:"vertical,omitempty"`
	Ratio    float64 `json:"ratio,omitempty"`
	A        *Node   `json:"a,omitempty"`
	B        *Node   `json:"b,omitempty"`
}

// Tab is one tabbed layout inside a workspace.
type Tab struct {
	Name     string `json:"name,omitempty"`
	Panes    []Pane `json:"panes"`
	Layout   *Node  `json:"layout,omitempty"`
	Selected int    `json:"selected"`
}

type Workspace struct {
	GroupPath []string `json:"group_path,omitempty"`
	Name      string   `json:"name"`
	CWD       string   `json:"cwd"`
	Tabs      []Tab    `json:"tabs,omitempty"`
	Active    int      `json:"active,omitempty"`

	// Legacy single-layout fields, read for states written before tabs.
	Panes    []Pane `json:"panes,omitempty"`
	Layout   *Node  `json:"layout,omitempty"`
	Selected int    `json:"selected,omitempty"`
}

type State struct {
	Workspaces []Workspace `json:"workspaces"`
	Selected   int         `json:"selected"`

	// DisabledAgents lists agent kinds the user switched off in the
	// settings window; they are hidden from pickers and cycling.
	DisabledAgents []string `json:"disabled_agents,omitempty"`

	// Sound plays an audio notification when an agent finishes a turn;
	// SoundKind selects which one. Notify rings the terminal bell so
	// the terminal can surface its own notification (badge, bounce).
	Sound     bool   `json:"sound,omitempty"`
	SoundKind string `json:"sound_kind,omitempty"`
	Notify    bool   `json:"notify,omitempty"`

	// Theme selects the color theme: "default" or a themes/*.json name.
	Theme string `json:"theme,omitempty"`

	// DisableAutoCopy prevents completed text selections from being copied to
	// the system clipboard. Its zero value preserves the legacy auto-copy behavior.
	DisableAutoCopy bool `json:"disable_auto_copy,omitempty"`

	// SidebarCollapsed remembers whether the workspace sidebar is compact.
	// Its zero value keeps older state files expanded.
	SidebarCollapsed bool `json:"sidebar_collapsed,omitempty"`
}

// DefaultPath returns the state file location under the user config dir.
// Everything else hrdx stores (keys, harnesses, themes, plugins, sockets)
// lives next to it, so this is the single place that decides the directory.
func DefaultPath() string {
	base := configBase(runtime.GOOS, os.Getenv("XDG_CONFIG_HOME"), platformConfigDir())
	if base == "" {
		return ""
	}
	return filepath.Join(base, "hrdx", "state.json")
}

// platformConfigDir is the OS convention: Application Support on macOS,
// XDG_CONFIG_HOME or ~/.config on Linux, %AppData% on Windows.
func platformConfigDir() string {
	base, err := os.UserConfigDir()
	if err != nil {
		home, herr := os.UserHomeDir()
		if herr != nil {
			return ""
		}
		base = filepath.Join(home, ".config")
	}
	return base
}

// configBase prefers an absolute XDG_CONFIG_HOME on every Unix-like system,
// including macOS, where os.UserConfigDir ignores it. Windows keeps its
// native directory and a relative XDG value is ignored per the spec.
//
// Existing installations keep working: when hrdx already has a directory
// under the platform location and none under XDG yet, the platform location
// stays in use so sessions, keys, and approvals are not silently orphaned.
// Moving that directory under XDG_CONFIG_HOME switches over.
func configBase(goos, xdg, platform string) string {
	if goos == "windows" || xdg == "" || !filepath.IsAbs(xdg) {
		return platform
	}
	xdg = filepath.Clean(xdg)
	if platform == "" || platform == xdg {
		return xdg
	}
	if !dirExists(filepath.Join(xdg, "hrdx")) && dirExists(filepath.Join(platform, "hrdx")) {
		return platform
	}
	return xdg
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// Load reads the state file. A missing file returns an empty state, nil.
func Load(path string) (State, error) {
	var loaded State
	if path == "" {
		return loaded, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return loaded, nil
		}
		return loaded, err
	}
	if err := json.Unmarshal(data, &loaded); err != nil {
		return State{}, err
	}
	return loaded, nil
}

// Save writes the state file atomically (write temp, rename).
func Save(path string, current State) error {
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(current, "", "  ")
	if err != nil {
		return err
	}
	temp := path + ".tmp"
	if err := os.WriteFile(temp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(temp, path)
}
