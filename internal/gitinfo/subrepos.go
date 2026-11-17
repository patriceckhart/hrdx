package gitinfo

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Sub-repositories are git repositories nested below a workspace directory.
// Both frontends scan them off their update loop with these helpers.

const (
	// MaxSubrepoDepth is the deepest folder level a scan may search.
	MaxSubrepoDepth = 3
	// SubrepoLimit caps how many repositories one scan reports.
	SubrepoLimit = 24
	// SubrepoInterval is how often frontends refresh a scan.
	SubrepoInterval = 15 * time.Second

	subrepoWorkers    = 4
	subrepoGitTimeout = 3 * time.Second
)

// Dependency trees can contain thousands of vendored repositories.
var subrepoSkipDirs = map[string]bool{"node_modules": true, "vendor": true}

// RepoStatus is the git status of one repository.
type RepoStatus struct {
	Branch    string
	Ahead     int
	Behind    int
	Changed   int // modified, staged, renamed, or untracked entries
	Conflicts int
}

// Subrepo is one repository found below a workspace.
type Subrepo struct {
	Path string // absolute
	Rel  string // slash-separated, relative to the workspace
	RepoStatus
}

// ScanSubrepos lists repositories below root up to depth folder levels, in
// sorted depth-first order. It does not descend into found repositories,
// skips hidden relative paths and everything below them, and does not count
// skipped directories toward the limit. Symlinks are not followed, so cycles
// cannot occur. The root itself is never listed.
func ScanSubrepos(root string, depth, limit int, hidden map[string]bool) []string {
	var found []string
	var walk func(dir string, level int)
	walk = func(dir string, level int) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, entry := range entries {
			if len(found) >= limit {
				return
			}
			name := entry.Name()
			if !entry.IsDir() || strings.HasPrefix(name, ".") || subrepoSkipDirs[name] {
				continue
			}
			path := filepath.Join(dir, name)
			if rel, err := filepath.Rel(root, path); err == nil && hidden[filepath.ToSlash(rel)] {
				continue
			}
			if _, err := os.Lstat(filepath.Join(path, ".git")); err == nil {
				found = append(found, path)
				continue
			}
			if level < depth {
				walk(path, level+1)
			}
		}
	}
	if depth > 0 && limit > 0 {
		walk(root, 1)
	}
	return found
}

// CollectSubrepos scans root and reads each repository's status with up to
// four concurrent calls to status. Pass ReadStatus in production.
func CollectSubrepos(root string, depth int, hidden map[string]bool, status func(string) RepoStatus) []Subrepo {
	paths := ScanSubrepos(root, depth, SubrepoLimit, hidden)
	repos := make([]Subrepo, len(paths))
	slots := make(chan struct{}, subrepoWorkers)
	var wait sync.WaitGroup
	for index, path := range paths {
		rel, err := filepath.Rel(root, path)
		if err != nil {
			rel = filepath.Base(path)
		}
		repos[index] = Subrepo{Path: path, Rel: filepath.ToSlash(rel)}
		wait.Add(1)
		slots <- struct{}{}
		go func(target *Subrepo) {
			defer wait.Done()
			defer func() { <-slots }()
			target.RepoStatus = status(target.Path)
		}(&repos[index])
	}
	wait.Wait()
	return repos
}

// ReadStatus runs `git status` in dir with a timeout. Without git, or when
// it fails, the branch still comes from .git/HEAD.
func ReadStatus(dir string) RepoStatus {
	fallback := RepoStatus{Branch: Branch(dir)}
	gitPath, err := exec.LookPath("git")
	if err != nil {
		return fallback
	}
	ctx, cancel := context.WithTimeout(context.Background(), subrepoGitTimeout)
	defer cancel()
	command := exec.CommandContext(ctx, gitPath, "-C", dir, "--no-optional-locks", "status", "--porcelain=v2", "--branch")
	output, err := command.Output()
	if err != nil {
		return fallback
	}
	status := ParseStatus(string(output))
	if status.Branch == "" {
		status.Branch = fallback.Branch
	}
	return status
}

// ParseStatus reads `git status --porcelain=v2 --branch` output.
func ParseStatus(output string) RepoStatus {
	var status RepoStatus
	oid := ""
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "# branch.oid "):
			oid = strings.TrimPrefix(line, "# branch.oid ")
		case strings.HasPrefix(line, "# branch.head "):
			status.Branch = strings.TrimPrefix(line, "# branch.head ")
		case strings.HasPrefix(line, "# branch.ab "):
			for _, field := range strings.Fields(strings.TrimPrefix(line, "# branch.ab ")) {
				value, err := strconv.Atoi(field[1:])
				if err != nil || value < 0 {
					continue
				}
				if field[0] == '+' {
					status.Ahead = value
				} else if field[0] == '-' {
					status.Behind = value
				}
			}
		case strings.HasPrefix(line, "u "):
			status.Conflicts++
		case strings.HasPrefix(line, "1 "), strings.HasPrefix(line, "2 "), strings.HasPrefix(line, "? "):
			status.Changed++
		}
	}
	if status.Branch == "(detached)" {
		status.Branch = ""
		if len(oid) >= 7 {
			status.Branch = oid[:7]
		}
	}
	return status
}

// CleanSubrepoRel normalizes a saved hidden path and rejects paths that
// could point outside the workspace. It returns "" when rel is unusable.
func CleanSubrepoRel(rel string) string {
	rel = strings.TrimSpace(filepath.ToSlash(rel))
	if rel == "" || filepath.IsAbs(rel) || strings.HasPrefix(rel, "/") {
		return ""
	}
	rel = filepath.ToSlash(filepath.Clean(filepath.FromSlash(rel)))
	if rel == "." || rel == ".." || strings.HasPrefix(rel, "../") {
		return ""
	}
	return rel
}
