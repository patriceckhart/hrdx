// Package state persists hrdx workspaces to disk so a restart restores the
// same workspaces, panes, and layout. Running processes cannot survive a
// restart; agent panes are relaunched with their resume flag so each agent
// resumes its latest session for that directory.
package state

import (
	"encoding/json"
	"os"
	"path/filepath"
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
	Name   string `json:"name"`
	CWD    string `json:"cwd"`
	Tabs   []Tab  `json:"tabs,omitempty"`
	Active int    `json:"active,omitempty"`

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
	SidebarCollapsed bool   `json:"sidebar_collapsed,omitempty"`
	WorktreeCommand  string `json:"worktree_command,omitempty"`
}

// DefaultPath returns the state file location under the user config dir.
func DefaultPath() string {
	base, err := os.UserConfigDir()
	if err != nil {
		home, herr := os.UserHomeDir()
		if herr != nil {
			return ""
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "hrdx", "state.json")
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
