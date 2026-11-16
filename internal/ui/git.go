package ui

import (
	"time"

	"github.com/patriceckhart/hrdx/internal/gitinfo"
)

type branchInfo struct {
	value   string
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
	entry := branchInfo{value: readGitBranch(cwd), checked: time.Now()}
	if entry.value != "" {
		entry.ahead, entry.behind = gitinfo.AheadBehind(cwd)
	}
	m.branches[cwd] = entry
	return entry
}

func readGitBranch(cwd string) string { return gitinfo.Branch(cwd) }
