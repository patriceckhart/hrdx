package ui

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type branchInfo struct {
	value   string
	common  string
	ahead   int
	behind  int
	checked time.Time
}

// gitBranch returns the checked-out branch plus ahead/behind counts for a
// directory, cached for a few seconds so the sidebar can query it on every
// render.
func (m Model) gitBranch(cwd string) branchInfo {
	if entry, ok := m.branches[cwd]; ok && time.Since(entry.checked) < 10*time.Second {
		return entry
	}
	entry := branchInfo{value: readGitBranch(cwd), common: readGitCommonDir(cwd), checked: time.Now()}
	if entry.value != "" {
		entry.ahead, entry.behind = readGitAheadBehind(cwd)
	}
	m.branches[cwd] = entry
	return entry
}

// readGitBranch resolves .git/HEAD without running git. Worktrees and
// submodules use a ".git" file pointing at the real git dir.
func readGitBranch(cwd string) string {
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

func readGitCommonDir(cwd string) string {
	output, err := runGit(cwd, "rev-parse", "--git-common-dir")
	if err != nil {
		return ""
	}
	common := strings.TrimSpace(string(output))
	if common == "" {
		return ""
	}
	if !filepath.IsAbs(common) {
		common = filepath.Join(cwd, common)
	}
	if resolved, err := filepath.EvalSymlinks(common); err == nil {
		common = resolved
	}
	if filepath.Base(filepath.Clean(common)) != ".git" {
		return ""
	}
	return filepath.Clean(filepath.Dir(common))
}

// readGitAheadBehind returns how many commits HEAD is ahead of and behind
// its upstream. Zeroes when there is no upstream or git is unavailable. The
// call is bounded so a hung git can never stall rendering for long.
func readGitAheadBehind(cwd string) (ahead, behind int) {
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
