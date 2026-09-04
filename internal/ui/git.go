package ui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

type branchInfo struct {
	value   string
	ahead   int
	behind  int
	checked time.Time
}

func workspacePathKey(path string) string {
	if absolute, err := filepath.Abs(path); err == nil {
		path = absolute
	}
	path = filepath.Clean(path)
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	return filepath.Clean(path)
}

// gitCommonDir identifies the repository shared by a worktree. It reads the
// worktree's .git metadata directly, so rendering does not need to invoke git.
func gitCommonDir(cwd string) (string, bool) {
	gitPath := filepath.Join(cwd, ".git")
	info, err := os.Stat(gitPath)
	if err != nil {
		return "", false
	}
	if info.IsDir() {
		return workspacePathKey(gitPath), true
	}
	content, err := os.ReadFile(gitPath)
	if err != nil {
		return "", false
	}
	line := strings.TrimSpace(string(content))
	if !strings.HasPrefix(line, "gitdir:") {
		return "", false
	}
	gitDir := strings.TrimSpace(strings.TrimPrefix(line, "gitdir:"))
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(cwd, gitDir)
	}
	gitDir = filepath.Clean(gitDir)
	if common, err := os.ReadFile(filepath.Join(gitDir, "commondir")); err == nil {
		commonDir := strings.TrimSpace(string(common))
		if !filepath.IsAbs(commonDir) {
			commonDir = filepath.Join(gitDir, commonDir)
		}
		return workspacePathKey(commonDir), true
	}
	// A non-worktree .git file (for example a submodule) has no commondir;
	// its git directory is still the best available repository identity.
	return workspacePathKey(gitDir), true
}

// gitBranch returns the checked-out branch plus ahead/behind counts for a
// directory, cached for a few seconds so the sidebar can query it on every
// render.
func (m Model) gitBranch(cwd string) branchInfo {
	if entry, ok := m.branches[cwd]; ok && time.Since(entry.checked) < 10*time.Second {
		return entry
	}
	entry := branchInfo{value: readGitBranch(cwd), checked: time.Now()}
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

// readGitAheadBehind returns how many commits HEAD is ahead of and behind
// its upstream. Zeroes when there is no upstream or git is unavailable. The
// call is bounded so a hung git can never stall rendering for long.
const defaultWorktreeCreateCmd = "git worktree add -b {name} {path}"

func resolveWorktreePath(cwd, name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("worktree name is required")
	}
	if filepath.IsAbs(name) || strings.ContainsAny(name, `/\\`) || name == "." || name == ".." {
		return "", fmt.Errorf("worktree name must be a relative directory name")
	}
	path, err := filepath.Abs(filepath.Join(cwd, ".worktrees", name))
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(path); err == nil {
		return "", fmt.Errorf("%s already exists", path)
	} else if !os.IsNotExist(err) {
		return "", err
	}
	return path, nil
}

func shellQuote(value, shell string) string {
	return shellQuoteFor(runtime.GOOS, value, shell)
}

func shellQuoteFor(goos, value, shell string) string {
	if goos == "windows" {
		switch strings.ToLower(shellBaseFor(goos, shell)) {
		case "cmd.exe":
			return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
		case "powershell.exe", "pwsh.exe":
			return "'" + strings.ReplaceAll(value, "'", "''") + "'"
		}
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func shellCommandArgs(shell, command string) []string {
	return shellCommandArgsFor(runtime.GOOS, shell, command)
}

func shellCommandArgsFor(goos, shell, command string) []string {
	if goos == "windows" {
		switch strings.ToLower(shellBaseFor(goos, shell)) {
		case "cmd.exe":
			return []string{"/C", command}
		case "powershell.exe", "pwsh.exe":
			return []string{"-Command", command}
		}
	}
	return []string{"-c", command}
}

func shellBaseFor(goos, shell string) string {
	if goos == "windows" {
		shell = strings.TrimRight(shell, `/\\`)
		if index := strings.LastIndexAny(shell, `/\\`); index >= 0 {
			return shell[index+1:]
		}
	}
	return filepath.Base(shell)
}

func createWorktreeCommand(cwd, name, path string, target *space, template, shell string) tea.Cmd {
	return func() tea.Msg {
		if template == "" {
			template = defaultWorktreeCreateCmd
		}
		if shell == "" {
			shell = "sh"
		}
		command := strings.NewReplacer("{name}", shellQuote(name, shell), "{path}", shellQuote(path, shell), "{repo}", shellQuote(cwd, shell)).Replace(template)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return worktreeCreateMsg{space: target, path: path, err: err}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, shell, shellCommandArgs(shell, command)...)
		cmd.Dir = cwd
		if output, err := cmd.CombinedOutput(); err != nil {
			if len(output) > 0 {
				err = fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
			}
			return worktreeCreateMsg{space: target, path: path, err: err}
		}
		return worktreeCreateMsg{space: target, path: path}
	}
}

func listWorktreeItems(cwd string, spaces []*space) ([]menuItem, error) {
	gitPath, err := exec.LookPath("git")
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, gitPath, "-C", cwd, "worktree", "list", "--porcelain").Output()
	if err != nil {
		return nil, err
	}
	open := make(map[string]bool, len(spaces))
	for _, current := range spaces {
		open[workspacePathKey(current.cwd)] = true
	}
	return parseWorktreePorcelain(string(output), open), nil
}

func parseWorktreePorcelain(output string, open map[string]bool) []menuItem {
	var items []menuItem
	var path, branch, head string
	flush := func() {
		if path == "" || open[workspacePathKey(path)] {
			path, branch, head = "", "", ""
			return
		}
		if branch == "" {
			branch = head
		}
		if branch == "" {
			branch = filepath.Base(path)
		}
		items = append(items, menuItem{branch + "  " + path, "worktree:" + path})
		path, branch, head = "", "", ""
	}
	for _, line := range strings.Split(output, "\n") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			flush()
			path = strings.TrimSpace(strings.TrimPrefix(line, "worktree "))
		case strings.HasPrefix(line, "branch "):
			branch = strings.TrimPrefix(line, "branch refs/heads/")
		case strings.HasPrefix(line, "HEAD "):
			head = strings.TrimSpace(strings.TrimPrefix(line, "HEAD "))
			if len(head) > 7 {
				head = head[:7]
			}
		case line == "":
			flush()
		}
	}
	flush()
	return items
}

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
