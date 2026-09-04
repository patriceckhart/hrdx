package ui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

type worktreeInfo struct {
	path   string
	branch string
}

func gitWorktreeRoot(cwd string) string {
	if absolute, err := filepath.Abs(cwd); err == nil {
		cwd = absolute
	}
	if resolved, err := filepath.EvalSymlinks(cwd); err == nil {
		cwd = resolved
	}
	gitPath := filepath.Join(cwd, ".git")
	info, err := os.Stat(gitPath)
	if err != nil {
		return ""
	}
	gitDir := gitPath
	if !info.IsDir() {
		data, err := os.ReadFile(gitPath)
		if err != nil || !strings.HasPrefix(strings.TrimSpace(string(data)), "gitdir:") {
			return ""
		}
		gitDir = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(data)), "gitdir:"))
		if !filepath.IsAbs(gitDir) {
			gitDir = filepath.Join(cwd, gitDir)
		}
	}
	if data, err := os.ReadFile(filepath.Join(gitDir, "commondir")); err == nil {
		common := strings.TrimSpace(string(data))
		if !filepath.IsAbs(common) {
			common = filepath.Join(gitDir, common)
		}
		gitDir = common
	}
	if filepath.Base(gitDir) != ".git" {
		return ""
	}
	root := filepath.Dir(gitDir)
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	return filepath.Clean(root)
}

func runGit(cwd string, args ...string) ([]byte, error) {
	git, err := exec.LookPath("git")
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, git, args...)
	cmd.Dir = cwd
	return cmd.Output()
}

func listWorktrees(cwd string) ([]worktreeInfo, error) {
	output, err := runGit(cwd, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, fmt.Errorf("git worktree list: %w", err)
	}
	var result []worktreeInfo
	var current *worktreeInfo
	for _, line := range strings.Split(string(output), "\n") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			if current != nil {
				result = append(result, *current)
			}
			current = &worktreeInfo{path: strings.TrimPrefix(line, "worktree ")}
		case strings.HasPrefix(line, "branch ") && current != nil:
			current.branch = strings.TrimPrefix(line, "branch refs/heads/")
		case strings.HasPrefix(line, "HEAD ") && current != nil && current.branch == "":
			head := strings.TrimPrefix(line, "HEAD ")
			current.branch = "(detached) " + head[:min(7, len(head))]
		}
	}
	if current != nil {
		result = append(result, *current)
	}
	return result, nil
}

func (m Model) openWorktreePicker(base *space, at rect) (tea.Model, tea.Cmd) {
	if base == nil || gitWorktreeRoot(base.cwd) == "" {
		return m, m.flashStatus("workspace is not a Git worktree")
	}
	worktrees, err := listWorktrees(base.cwd)
	if err != nil {
		return m, m.flashStatus(err.Error())
	}
	var items []menuItem
	for _, worktree := range worktrees {
		alreadyOpen := false
		for _, current := range m.spaces {
			if samePath(current.cwd, worktree.path) {
				alreadyOpen = true
				break
			}
		}
		if !alreadyOpen {
			items = append(items, menuItem{worktree.branch + "  " + worktree.path, "worktree-open:" + worktree.path})
		}
	}
	if len(items) == 0 {
		return m, m.flashInfo("no unopened worktrees")
	}
	m.mode = modeMenu
	m.menuPane, m.menuTab, m.menuSpace = nil, nil, nil
	m.pickItems, m.pickAction, m.pickSpace = items, "worktree-open", base
	m.openMenuBox(at)
	return m, nil
}

// Keep the destination as one interpolated argument. Besides being simpler,
// this avoids relying on the shell to concatenate separately quoted path
// components (which is not portable to cmd.exe when the repository path has
// spaces).
const defaultWorktreeCommand = `git worktree add {{path}} -b {{name}}`

func shellQuote(value string) string {
	if runtime.GOOS == "windows" {
		// cmd.exe does not concatenate quoted and unquoted fragments, so only
		// quote values that need protection. This keeps ../custom-{{name}} a
		// valid single argument while still handling paths with spaces.
		if !strings.ContainsAny(value, " \t&()[]{}^=;!'+,`~") {
			return value
		}
		return `"` + strings.ReplaceAll(value, `"`, `\"`) + `"`
	}
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func interpolateWorktreeCommand(command string, values map[string]string) string {
	for key, value := range values {
		command = strings.ReplaceAll(command, "{{"+key+"}}", shellQuote(value))
		command = strings.ReplaceAll(command, "{"+key+"}", shellQuote(value))
	}
	return command
}

func runShell(cwd, command string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if runtime.GOOS == "windows" {
		shell := os.Getenv("COMSPEC")
		if shell == "" {
			shell = "cmd.exe"
		}
		cmd := exec.CommandContext(ctx, shell, "/d", "/c", command)
		cmd.Dir = cwd
		return cmd.CombinedOutput()
	}
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = cwd
	return cmd.CombinedOutput()
}

func (m Model) worktreeCommand() string {
	if strings.TrimSpace(m.config.WorktreeCommand) == "" {
		return defaultWorktreeCommand
	}
	return m.config.WorktreeCommand
}

func (m Model) createWorktree(base *space, name string) (string, error) {
	if base == nil || gitWorktreeRoot(base.cwd) == "" {
		return "", fmt.Errorf("workspace is not a Git worktree")
	}
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\\`) {
		return "", fmt.Errorf("worktree name must be one path component")
	}
	root := gitWorktreeRoot(base.cwd)
	path := filepath.Join(root, ".worktrees", name)
	before, beforeErr := listWorktrees(base.cwd)
	command := interpolateWorktreeCommand(m.worktreeCommand(), map[string]string{
		"name": name, "path": path, "base": base.cwd, "base_worktree_path": root,
	})
	output, err := runShell(base.cwd, command)
	if err != nil {
		detail := strings.TrimSpace(string(output))
		if detail == "" {
			detail = err.Error()
		}
		return "", fmt.Errorf("create worktree: %s", detail)
	}
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	worktrees, listErr := listWorktrees(base.cwd)
	if listErr == nil {
		for _, worktree := range worktrees {
			if samePath(worktree.path, path) {
				return worktree.path, nil
			}
		}

		// Some worktree managers, such as worktrunk's `wt switch --create`,
		// choose the worktree path themselves. In that case the command can
		// succeed without creating the conventional .worktrees/name path.
		// Resolve the newly-created worktree by its branch name instead of
		// rejecting a successful command.
		if beforeErr == nil {
			for _, worktree := range worktrees {
				if worktree.branch != name || containsWorktreePath(before, worktree.path) {
					continue
				}
				return worktree.path, nil
			}
		}
	}
	return "", fmt.Errorf("create worktree: command did not create %s", path)
}

func containsWorktreePath(worktrees []worktreeInfo, path string) bool {
	for _, worktree := range worktrees {
		if samePath(worktree.path, path) {
			return true
		}
	}
	return false
}

func samePath(a, b string) bool {
	for _, path := range []string{a, b} {
		if absolute, err := filepath.Abs(path); err == nil {
			if resolved, err := filepath.EvalSymlinks(absolute); err == nil {
				if path == a {
					a = resolved
				} else {
					b = resolved
				}
			}
		}
	}
	a, b = filepath.Clean(a), filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func (m Model) runWorktreePick(path string) (tea.Model, tea.Cmd) {
	base := m.pickSpace
	m.closeMenu()
	if base == nil {
		return m, nil
	}
	newSpace := m.addSpaceKind(path, m.config.DefaultAgent)
	m.selected = len(m.spaces) - 1
	m.persist()
	return m, m.startPane(newSpace, newSpace.tab().panes[0])
}
