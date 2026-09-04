package ui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// keymapFile declares key overrides in the state directory. The format maps
// action names to a single key: {"find": "f", "navigate-up": "home"}.
const keymapFile = "keys.json"

// defaultPrefixKeys maps every prefix action to its default keys. The
// first key of each action is the one shown in hints and docs.
var defaultPrefixKeys = map[string][]string{
	"prefix":          {"ctrl+b"},
	"literal":         {"ctrl+b"},
	"quit":            {"q"},
	"picker-right":    {"c"},
	"picker-down":     {"C"},
	"agent-right":     {"a"},
	"agent-down":      {"A"},
	"agent-cycle":     {},
	"shell-right":     {"s", "%", "|"},
	"shell-down":      {"S", "\"", "-"},
	"workspace":       {"w"},
	"worktree-add":    {},
	"worktree_open":   {},
	"worktree_create": {},
	"tab-new":         {"t"},
	"tab-next":        {"n"},
	"tab-prev":        {"p"},
	"space-next":      {"]"},
	"space-prev":      {"["},
	"pane-next":       {"tab"},
	"pane-prev":       {"shift+tab"},
	"find":            {"/"},
	"close-pane":      {"x"},
	"close-space":     {"X"},
	"equalize":        {"="},
	"rename":          {"r"},
	"menu":            {"m"},
	"settings":        {","},
	"sidebar-toggle":  {"b"},
	"scroll-up":       {"u", "pgup"},
	"scroll-down":     {"d", "pgdown"},
	"live":            {"esc", "G"},
}

var navigationActions = map[string]bool{
	"navigate-up":   true,
	"navigate-down": true,
}

// buildPrefixKeys resolves the key -> action table from the defaults and
// the user's overrides. An override replaces all default keys of its
// action and shadows whichever action held the key before. "prefix" is
// excluded: it is the trigger that enters prefix mode in the first place
// (resolved separately via primaryKey), not an action dispatched once
// already inside it, and by default shares its key with "literal".
func buildPrefixKeys(overrides map[string]string) map[string]string {
	keys := map[string]string{}
	overridden := map[string]bool{}
	for action := range overrides {
		if _, ok := defaultPrefixKeys[action]; ok {
			overridden[action] = true
		}
	}
	for action, actionKeys := range defaultPrefixKeys {
		if action == "prefix" || overridden[action] {
			continue
		}
		for _, key := range actionKeys {
			keys[key] = action
		}
	}
	for action, key := range overrides {
		if action == "prefix" {
			continue
		}
		if _, ok := defaultPrefixKeys[action]; ok {
			keys[key] = action
		}
	}
	return keys
}

// loadKeymap reads keys.json from the state directory. A missing file is
// fine; a broken one returns a problem string for the status line.
func loadKeymap(dir string) (map[string]string, string) {
	data, err := os.ReadFile(filepath.Join(dir, keymapFile))
	if err != nil {
		return nil, ""
	}
	var overrides map[string]string
	if err := json.Unmarshal(data, &overrides); err != nil {
		return nil, keymapFile + ": " + err.Error()
	}
	var unknown []string
	for action, key := range overrides {
		_, prefixAction := defaultPrefixKeys[action]
		if !prefixAction && !navigationActions[action] {
			unknown = append(unknown, action)
			delete(overrides, action)
			continue
		}
		if strings.TrimSpace(key) == "" {
			delete(overrides, action)
		}
	}
	if len(unknown) > 0 {
		return overrides, keymapFile + ": unknown actions: " + strings.Join(unknown, ", ")
	}
	return overrides, ""
}

// buildNavigationKeys resolves extra local navigation keys from keys.json.
// Arrows and j/k remain available when custom keys are configured.
func buildNavigationKeys(overrides map[string]string) map[string]string {
	keys := map[string]string{}
	if key := overrides["navigate-up"]; key != "" {
		keys[key] = "navigate-up"
	}
	if key := overrides["navigate-down"]; key != "" {
		keys[key] = "navigate-down"
	}
	return keys
}
