package core

import (
	"fmt"
	"path/filepath"
	"slices"

	"github.com/patriceckhart/hrdx/internal/api"
	"github.com/patriceckhart/hrdx/internal/gitinfo"
)

// Sub-repositories mirror the TUI: with the setting on (subrepo_depth in
// state.json), git repositories nested below each workspace are scanned off
// the loop every gitinfo.SubrepoInterval and listed in the sidebar. Scans
// run in goroutines and post their result back, so a slow git never stalls
// the loop.

// SubrepoView is one nested repository in the sidebar.
type SubrepoView struct {
	Path      string `json:"path"`
	Rel       string `json:"rel"`
	Branch    string `json:"branch,omitempty"`
	Ahead     int    `json:"ahead,omitempty"`
	Behind    int    `json:"behind,omitempty"`
	Changed   int    `json:"changed,omitempty"`
	Conflicts int    `json:"conflicts,omitempty"`
}

type subrepoCache struct {
	repos   []gitinfo.Subrepo
	pending bool
}

// readSubrepoStatus is replaceable in tests so they never depend on git.
var readSubrepoStatus = gitinfo.ReadStatus

// refreshSubrepos starts a scan for every distinct workspace directory that
// has none in flight, and drops results for directories no longer open.
func (m *model) refreshSubrepos() {
	if m.subrepoDepth == 0 {
		return
	}
	live := map[string]bool{}
	for _, current := range m.spaces {
		root := current.cwd
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
		gen, depth, hidden := m.subrepoGen, m.subrepoDepth, m.hiddenSubreposFor(root)
		post := m.post
		go func() {
			repos := gitinfo.CollectSubrepos(root, depth, hidden, readSubrepoStatus)
			post(func() { m.applySubrepos(gen, root, repos) })
		}()
	}
	for root := range m.subrepos {
		if !live[root] {
			delete(m.subrepos, root)
		}
	}
}

// applySubrepos stores a scan result unless the setting changed since it
// started. A scan started before a folder was hidden may still report it.
func (m *model) applySubrepos(gen int, root string, repos []gitinfo.Subrepo) {
	if gen != m.subrepoGen {
		return
	}
	hidden := m.hiddenSubreposFor(root)
	kept := make([]gitinfo.Subrepo, 0, len(repos))
	for _, repo := range repos {
		if !hidden[repo.Rel] {
			kept = append(kept, repo)
		}
	}
	m.subrepos[root] = subrepoCache{repos: kept}
}

// setSubrepoDepth changes the search depth (0 turns the feature off) and
// rescans immediately.
func (m *model) setSubrepoDepth(depth int) {
	depth = clamp(depth, 0, gitinfo.MaxSubrepoDepth)
	if depth == m.subrepoDepth {
		return
	}
	m.subrepoDepth = depth
	m.subrepoGen++
	clear(m.subrepos)
	m.persist()
	m.refreshSubrepos()
}

// hiddenSubreposFor merges every workspace sharing root, since they share
// one scan. The map is a private copy for the scanning goroutine.
func (m *model) hiddenSubreposFor(root string) map[string]bool {
	hidden := map[string]bool{}
	for _, current := range m.spaces {
		if current.cwd != root {
			continue
		}
		for _, rel := range current.hiddenSubrepos {
			hidden[rel] = true
		}
	}
	return hidden
}

// hideSubrepo hides path from owner's list and stops scanning below it.
func (m *model) hideSubrepo(owner *space, path string) error {
	rel, err := filepath.Rel(owner.cwd, path)
	if err != nil {
		return fmt.Errorf("%s is not inside the workspace", path)
	}
	rel = gitinfo.CleanSubrepoRel(rel)
	if rel == "" {
		return fmt.Errorf("%s is not inside the workspace", path)
	}
	if !slices.Contains(owner.hiddenSubrepos, rel) {
		owner.hiddenSubrepos = append(owner.hiddenSubrepos, rel)
		slices.Sort(owner.hiddenSubrepos)
	}
	if entry, ok := m.subrepos[owner.cwd]; ok {
		entry.repos = slices.DeleteFunc(slices.Clone(entry.repos), func(repo gitinfo.Subrepo) bool { return repo.Rel == rel })
		m.subrepos[owner.cwd] = entry
	}
	m.persist()
	return nil
}

// unhideSubrepos resets every workspace sharing owner's directory and
// rescans, dropping in-flight scans that still skip the folders.
func (m *model) unhideSubrepos(owner *space) {
	if len(owner.hiddenSubrepos) == 0 {
		return
	}
	for _, current := range m.spaces {
		if current.cwd == owner.cwd {
			current.hiddenSubrepos = nil
		}
	}
	m.persist()
	m.subrepoGen++
	clear(m.subrepos)
	m.refreshSubrepos()
}

// subrepoViews lists the cached repositories of cwd for the sidebar.
func (m *model) subrepoViews(cwd string) []SubrepoView {
	if m.subrepoDepth == 0 {
		return nil
	}
	repos := m.subrepos[cwd].repos
	if len(repos) == 0 {
		return nil
	}
	out := make([]SubrepoView, len(repos))
	for index, repo := range repos {
		out[index] = SubrepoView{Path: repo.Path, Rel: repo.Rel, Branch: repo.Branch,
			Ahead: repo.Ahead, Behind: repo.Behind, Changed: repo.Changed, Conflicts: repo.Conflicts}
	}
	return out
}

// apiSubrepos matches the TUI's status field.
func (m *model) apiSubrepos(cwd string) []api.SubrepoStatus {
	views := m.subrepoViews(cwd)
	if len(views) == 0 {
		return nil
	}
	out := make([]api.SubrepoStatus, len(views))
	for index, repo := range views {
		out[index] = api.SubrepoStatus{Path: repo.Path, Name: repo.Rel, Branch: repo.Branch,
			Ahead: repo.Ahead, Behind: repo.Behind, Changed: repo.Changed, Conflicts: repo.Conflicts}
	}
	return out
}

// subrepoOwner finds a workspace by index and checks that path is one of
// its listed repositories, so the frontend cannot open arbitrary paths.
func (m *model) subrepoOwner(workspace int, path string) (*space, error) {
	owner, err := m.spaceAt(workspace)
	if err != nil {
		return nil, err
	}
	for _, repo := range m.subrepos[owner.cwd].repos {
		if filepath.Clean(repo.Path) == filepath.Clean(path) {
			return owner, nil
		}
	}
	return nil, fmt.Errorf("%s is not a listed repository of %s", path, owner.name)
}
