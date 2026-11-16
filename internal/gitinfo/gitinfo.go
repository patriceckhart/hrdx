// Package gitinfo reads the checked-out branch of a directory and its
// ahead and behind counts, shared by every hrdx frontend.
package gitinfo

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Branch resolves .git/HEAD without running git. Worktrees and submodules
// use a ".git" file pointing at the real git dir. A detached HEAD returns
// the short commit, a directory outside git returns "".
func Branch(cwd string) string {
	gitPath := filepath.Join(cwd, ".git")
	info, err := os.Stat(gitPath)
	if err != nil {
		return ""
	}
	gitDir := gitPath
	if !info.IsDir() {
		content, err := os.ReadFile(gitPath)
		if err != nil {
			return ""
		}
		line := strings.TrimSpace(string(content))
		if !strings.HasPrefix(line, "gitdir:") {
			return ""
		}
		gitDir = strings.TrimSpace(strings.TrimPrefix(line, "gitdir:"))
		if !filepath.IsAbs(gitDir) {
			gitDir = filepath.Join(cwd, gitDir)
		}
	}
	head, err := os.ReadFile(filepath.Join(gitDir, "HEAD"))
	if err != nil {
		return ""
	}
	line := strings.TrimSpace(string(head))
	if strings.HasPrefix(line, "ref: ") {
		ref := strings.TrimPrefix(line, "ref: ")
		return strings.TrimPrefix(ref, "refs/heads/")
	}
	if len(line) >= 7 {
		return line[:7]
	}
	return ""
}

// AheadBehind returns how many commits HEAD is ahead of and behind its
// upstream. Zeroes when there is no upstream or git is unavailable. The call
// is bounded so a hung git can never stall rendering for long.
func AheadBehind(cwd string) (ahead, behind int) {
	gitPath, err := exec.LookPath("git")
	if err != nil {
		return 0, 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	command := exec.CommandContext(ctx, gitPath, "-C", cwd, "rev-list", "--left-right", "--count", "HEAD...@{upstream}")
	output, err := command.Output()
	if err != nil {
		return 0, 0
	}
	fields := strings.Fields(strings.TrimSpace(string(output)))
	if len(fields) != 2 {
		return 0, 0
	}
	ahead, _ = strconv.Atoi(fields[0])
	behind, _ = strconv.Atoi(fields[1])
	return ahead, behind
}
