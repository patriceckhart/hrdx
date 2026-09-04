package ui

import (
	"encoding/json"
	"os"
	"path/filepath"
)

const worktreeConfigFile = "worktree.json"

type worktreeConfig struct {
	Create string `json:"create,omitempty"`
}

func loadWorktreeConfig(dir string) (worktreeConfig, string) {
	if dir == "" {
		return worktreeConfig{}, ""
	}
	data, err := os.ReadFile(filepath.Join(dir, worktreeConfigFile))
	if err != nil {
		if os.IsNotExist(err) {
			return worktreeConfig{}, ""
		}
		return worktreeConfig{}, "worktree config: " + err.Error()
	}
	var config worktreeConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return worktreeConfig{}, "worktree config: " + err.Error()
	}
	return config, ""
}
