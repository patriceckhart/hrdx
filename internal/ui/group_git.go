package ui

import (
	"os"
	"path/filepath"
	"strings"
)

// Git supplies a default label only when requested, never a group identity.
// Resolve linked worktrees through commondir; branch names remain metadata.
func gitGroupPath(cwd string) []string {
	gitDir := filepath.Join(cwd, ".git")
	info, err := os.Stat(gitDir)
	if err != nil {
		return nil
	}
	if !info.IsDir() {
		data, err := os.ReadFile(gitDir)
		if err != nil {
			return nil
		}
		line := strings.TrimSpace(string(data))
		if !strings.HasPrefix(line, "gitdir:") {
			return nil
		}
		gitDir = strings.TrimSpace(strings.TrimPrefix(line, "gitdir:"))
		if gitDir == "" {
			return nil
		}
		if !filepath.IsAbs(gitDir) {
			gitDir = filepath.Join(cwd, gitDir)
		}
		if common, err := os.ReadFile(filepath.Join(gitDir, "commondir")); err == nil {
			path := strings.TrimSpace(string(common))
			if path == "" {
				return nil
			}
			if !filepath.IsAbs(path) {
				path = filepath.Join(gitDir, path)
			}
			gitDir = path
		}
	}
	if filepath.Base(filepath.Clean(gitDir)) != ".git" {
		return nil
	}
	if _, err := os.Stat(filepath.Join(gitDir, "HEAD")); err != nil {
		return nil
	}
	name := filepath.Base(filepath.Dir(filepath.Clean(gitDir)))
	path := []string{name}
	if validateGroupPath(path) != nil {
		return nil
	}
	return path
}
