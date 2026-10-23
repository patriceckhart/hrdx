package ui

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/patriceckhart/hrdx/internal/api"
)

// Sub-repositories are scanned and inspected off the update loop; the
// sidebar only reads the cached result, so a slow git never stalls
// rendering.

const (
	maxSubrepoDepth   = 3
	subrepoLimit      = 24
	subrepoWorkers    = 4
	subrepoInterval   = 15 * time.Second
	subrepoGitTimeout = 3 * time.Second
)

// Dependency trees can contain thousands of vendored repositories.
var subrepoSkipDirs = map[string]bool{"node_modules": true, "vendor": true}

type subrepo struct {
	path      string
	rel       string // slash-separated, relative to the workspace
	branch    string
	ahead     int
	behind    int
	changed   int // modified, staged, renamed, or untracked entries
	conflicts int
}

type subrepoCache struct {
	repos   []subrepo
	pending bool
}

// gen invalidates ticks and scans started before the setting last changed.
type subrepoTickMsg struct{ gen int }

type subreposMsg struct {
	gen   int
	root  string
	repos []subrepo
}

// Replaceable in tests so they never depend on git.
var readGitStatusFunc = readGitStatus

func subrepoTick(gen int, delay time.Duration) tea.Cmd {
	if delay <= 0 {
		return func() tea.Msg { return subrepoTickMsg{gen: gen} }
	}
	return tea.Tick(delay, func(time.Time) tea.Msg { return subrepoTickMsg{gen: gen} })
}

func (m *Model) setSubrepoDepth(depth int) tea.Cmd {
	depth = clampInt(depth, 0, maxSubrepoDepth)
	if depth == m.subrepoDepth {
		return nil
	}
	m.subrepoDepth = depth
	m.subrepoGen++
	clear(m.subrepos)
	m.persist()
	if depth == 0 {
		return nil
	}
	return subrepoTick(m.subrepoGen, 0)
}

func (m Model) handleSubrepoTick(msg subrepoTickMsg) (tea.Model, tea.Cmd) {
	if msg.gen != m.subrepoGen || m.subrepoDepth == 0 {
		return m, nil
	}
	live := map[string]bool{}
	commands := []tea.Cmd{subrepoTick(m.subrepoGen, subrepoInterval)}
	for _, currentSpace := range m.spaces {
		root := currentSpace.cwd
		if live[root] {
			continue
		}
		live[root] = true
		entry := m.subrepos[root]
		if entry.pending {
			continue
		}
		entry.pending = true
		m.subrepos[root] = entry
		commands = append(commands, refreshSubrepos(m.subrepoGen, root, m.subrepoDepth, m.hiddenSubreposFor(root)))
	}
	for root := range m.subrepos {
		if !live[root] {
			delete(m.subrepos, root)
		}
	}
	return m, tea.Batch(commands...)
}

func (m Model) handleSubrepos(msg subreposMsg) (tea.Model, tea.Cmd) {
	if msg.gen != m.subrepoGen {
		return m, nil
	}
	// A scan started before a folder was hidden may still report it.
	hidden := m.hiddenSubreposFor(msg.root)
	repos := make([]subrepo, 0, len(msg.repos))
	for _, repo := range msg.repos {
		if !hidden[repo.rel] {
			repos = append(repos, repo)
		}
	}
	m.subrepos[msg.root] = subrepoCache{repos: repos}
	return m, nil
}

// hidden must be a private copy: the command runs off the update loop.
func refreshSubrepos(gen int, root string, depth int, hidden map[string]bool) tea.Cmd {
	return func() tea.Msg {
		return subreposMsg{gen: gen, root: root, repos: collectSubrepos(root, depth, hidden)}
	}
}

func collectSubrepos(root string, depth int, hidden map[string]bool) []subrepo {
	paths := scanSubrepos(root, depth, subrepoLimit, hidden)
	repos := make([]subrepo, len(paths))
	slots := make(chan struct{}, subrepoWorkers)
	var wait sync.WaitGroup
	for index, path := range paths {
		rel, err := filepath.Rel(root, path)
		if err != nil {
			rel = filepath.Base(path)
		}
		repos[index] = subrepo{path: path, rel: filepath.ToSlash(rel)}
		wait.Add(1)
		slots <- struct{}{}
		go func(target *subrepo) {
			defer wait.Done()
			defer func() { <-slots }()
			status := readGitStatusFunc(target.path)
			status.path, status.rel = target.path, target.rel
			*target = status
		}(&repos[index])
	}
	wait.Wait()
	return repos
}

// scanSubrepos does not descend into found repositories, and skipped
// directories do not count toward the limit. Symlinks are not followed, so
// cycles cannot occur.
func scanSubrepos(root string, depth, limit int, hidden map[string]bool) []string {
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

// Without git, or when it fails, the branch still comes from .git/HEAD.
func readGitStatus(dir string) subrepo {
	fallback := subrepo{branch: readGitBranch(dir)}
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
	status := parseGitStatus(string(output))
	if status.branch == "" {
		status.branch = fallback.branch
	}
	return status
}

// parseGitStatus reads `git status --porcelain=v2 --branch`.
func parseGitStatus(output string) subrepo {
	var status subrepo
	oid := ""
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "# branch.oid "):
			oid = strings.TrimPrefix(line, "# branch.oid ")
		case strings.HasPrefix(line, "# branch.head "):
			status.branch = strings.TrimPrefix(line, "# branch.head ")
		case strings.HasPrefix(line, "# branch.ab "):
			for _, field := range strings.Fields(strings.TrimPrefix(line, "# branch.ab ")) {
				value, err := strconv.Atoi(field[1:])
				if err != nil || value < 0 {
					continue
				}
				if field[0] == '+' {
					status.ahead = value
				} else if field[0] == '-' {
					status.behind = value
				}
			}
		case strings.HasPrefix(line, "u "):
			status.conflicts++
		case strings.HasPrefix(line, "1 "), strings.HasPrefix(line, "2 "), strings.HasPrefix(line, "? "):
			status.changed++
		}
	}
	if status.branch == "(detached)" {
		status.branch = ""
		if len(oid) >= 7 {
			status.branch = oid[:7]
		}
	}
	return status
}

func (m Model) subrepoRows(spaceIndex int, rail string, width int) []sidebarRow {
	if m.subrepoDepth == 0 || m.sideCollapsed {
		return nil
	}
	repos := m.subrepos[m.spaces[spaceIndex].cwd].repos
	nodes := subrepoTree(repos)
	rows := make([]sidebarRow, 0, len(nodes))
	for nodeIndex, node := range nodes {
		marker := "├ "
		if subrepoLastSibling(nodes, nodeIndex) {
			marker = "└ "
		}
		indent := strings.Repeat("  ", node.level)
		if node.repo < 0 {
			name := truncate(node.name+"/", max(1, width-3-len(indent)-lipgloss.Width(marker)))
			rows = append(rows, sidebarRow{
				label: rail + " " + indent + stylePaneDim.Render(marker+name),
				kind:  "subrepo", space: spaceIndex, tab: -1, pane: -1, subrepo: -1,
			})
			continue
		}
		repo := repos[node.repo]
		suffix, suffixWidth := "", 0
		add := func(text string, style lipgloss.Style) {
			suffix += style.Render(text)
			suffixWidth += lipgloss.Width(text)
		}
		if repo.conflicts > 0 {
			add(fmt.Sprintf(" !%d", repo.conflicts), styleDotOff)
		}
		if repo.changed > 0 {
			add(fmt.Sprintf(" ●%d", repo.changed), styleDotBusy)
		}
		if repo.ahead > 0 {
			add(fmt.Sprintf(" ↑%d", repo.ahead), styleDotOn)
		}
		if repo.behind > 0 {
			add(fmt.Sprintf(" ↓%d", repo.behind), lipgloss.NewStyle().Foreground(colorAlt))
		}
		available := max(1, width-3-len(indent)-lipgloss.Width(marker)-suffixWidth)
		name := truncate(node.name, available)
		branch := ""
		if repo.branch != "" && available-lipgloss.Width(name) > 2 {
			branch = " " + truncate(repo.branch, available-lipgloss.Width(name)-1)
		}
		var text string
		if m.subrepoTargeted(repo.path) {
			text = styleSubrepoTarget.Render(name + branch)
		} else {
			text = stylePaneRun.Render(name) + stylePaneDim.Render(branch)
		}
		rows = append(rows, sidebarRow{
			label: rail + " " + indent + stylePaneDim.Render(marker) + text + suffix,
			kind:  "subrepo", space: spaceIndex, tab: -1, pane: -1, subrepo: node.repo,
		})
	}
	return rows
}

type subrepoNode struct {
	level int
	name  string
	repo  int // -1 for a folder that only leads to deeper repositories
}

// subrepoTree relies on the depth-first, sorted scan order to emit each
// shared parent folder once.
func subrepoTree(repos []subrepo) []subrepoNode {
	var nodes []subrepoNode
	var previous []string
	for index, repo := range repos {
		parts := strings.Split(repo.rel, "/")
		parents := parts[:len(parts)-1]
		shared := 0
		for shared < len(parents) && shared < len(previous) && parents[shared] == previous[shared] {
			shared++
		}
		for level := shared; level < len(parents); level++ {
			nodes = append(nodes, subrepoNode{level: level, name: parents[level], repo: -1})
		}
		nodes = append(nodes, subrepoNode{level: len(parents), name: parts[len(parts)-1], repo: index})
		previous = parents
	}
	return nodes
}

func subrepoLastSibling(nodes []subrepoNode, index int) bool {
	level := nodes[index].level
	for _, next := range nodes[index+1:] {
		if next.level < level {
			return true
		}
		if next.level == level {
			return false
		}
	}
	return true
}

// subrepoTargeted also covers the agent picker that follows the location
// picker.
func (m Model) subrepoTargeted(path string) bool {
	if m.mode != modeMenu || m.pickPath == "" || filepath.Clean(m.pickPath) != filepath.Clean(path) {
		return false
	}
	return m.pickAction == "subrepo" || strings.HasPrefix(m.pickAction, "subrepo-") ||
		(m.pickAction == "space" && m.pickFromSubrepo)
}

// subrepoTextHit excludes the rail, indent, tree marker, and trailing
// blanks, so left clicks only act on visible text.
func subrepoTextHit(label string, x int) bool {
	plain := ansi.Strip(label)
	marker := strings.IndexAny(plain, "├└")
	if marker < 0 {
		return false
	}
	start := ansi.StringWidth(plain[:marker]) + 2
	end := ansi.StringWidth(strings.TrimRight(plain, " "))
	return x >= start && x < end
}

func (m Model) subrepoAt(row sidebarRow) (*space, subrepo, bool) {
	if row.kind != "subrepo" || row.space < 0 || row.space >= len(m.spaces) {
		return nil, subrepo{}, false
	}
	owner := m.spaces[row.space]
	repos := m.subrepos[owner.cwd].repos
	if row.subrepo < 0 || row.subrepo >= len(repos) {
		return nil, subrepo{}, false
	}
	return owner, repos[row.subrepo], true
}

func (m *Model) openSubrepoPicker(owner *space, path string, at rect) {
	m.mode = modeMenu
	m.menuPane = nil
	m.menuTab = nil
	m.menuSpace = nil
	m.pickItems = []menuItem{
		{"New tab here", "subrepo:tab"},
		{"Split right here", "subrepo:split-right"},
		{"Split down here", "subrepo:split-down"},
		{"Open as workspace", "subrepo:space"},
		{"Hide folder", "subrepo:hide"},
	}
	m.pickAction = "subrepo"
	m.pickFromSubrepo = true
	m.pickSpace = owner
	m.pickPath = path
	m.openMenuBox(at)
}

// cleanSubrepoRel rejects saved paths that could point outside the
// workspace.
func cleanSubrepoRel(rel string) string {
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

// hiddenSubreposFor merges every workspace sharing root, since they share
// one scan result.
func (m Model) hiddenSubreposFor(root string) map[string]bool {
	var hidden map[string]bool
	for _, currentSpace := range m.spaces {
		if currentSpace.cwd != root {
			continue
		}
		for rel := range currentSpace.hiddenSubrepos {
			if hidden == nil {
				hidden = map[string]bool{}
			}
			hidden[rel] = true
		}
	}
	return hidden
}

func (m *Model) hideSubrepo(target *space, path string) {
	rel, err := filepath.Rel(target.cwd, path)
	if err != nil {
		return
	}
	rel = cleanSubrepoRel(rel)
	if rel == "" {
		return
	}
	if target.hiddenSubrepos == nil {
		target.hiddenSubrepos = map[string]bool{}
	}
	target.hiddenSubrepos[rel] = true
	if entry, ok := m.subrepos[target.cwd]; ok {
		repos := make([]subrepo, 0, len(entry.repos))
		for _, repo := range entry.repos {
			if repo.rel != rel {
				repos = append(repos, repo)
			}
		}
		entry.repos = repos
		m.subrepos[target.cwd] = entry
	}
	m.persist()
}

// unhideSubrepos resets every workspace sharing the directory, since they
// share one scan result.
func (m *Model) unhideSubrepos(target *space) tea.Cmd {
	if len(target.hiddenSubrepos) == 0 {
		return nil
	}
	for _, currentSpace := range m.spaces {
		if currentSpace.cwd == target.cwd {
			currentSpace.hiddenSubrepos = nil
		}
	}
	m.persist()
	if m.subrepoDepth == 0 {
		return nil
	}
	// A new generation drops in-flight scans that still skip the folders.
	m.subrepoGen++
	clear(m.subrepos)
	return subrepoTick(m.subrepoGen, 0)
}

func sortedKeys(set map[string]bool) []string {
	if len(set) == 0 {
		return nil
	}
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

func (m Model) apiSubrepos(cwd string) []api.SubrepoStatus {
	repos := m.subrepos[cwd].repos
	if m.subrepoDepth == 0 || len(repos) == 0 {
		return nil
	}
	out := make([]api.SubrepoStatus, 0, len(repos))
	for _, repo := range repos {
		out = append(out, api.SubrepoStatus{Path: repo.path, Name: repo.rel, Branch: repo.branch,
			Ahead: repo.ahead, Behind: repo.behind, Changed: repo.changed, Conflicts: repo.conflicts})
	}
	return out
}
