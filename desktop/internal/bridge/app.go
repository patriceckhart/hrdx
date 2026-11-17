package bridge

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"sync/atomic"
	"time"

	"github.com/patriceckhart/hrdx/desktop/internal/core"
	"github.com/patriceckhart/hrdx/desktop/internal/updater"
	"github.com/patriceckhart/hrdx/internal/paths"
	"github.com/patriceckhart/hrdx/internal/sound"
	"github.com/patriceckhart/hrdx/internal/theme"
)

// Platform is the webview runtime surface the bridge needs. The Wails
// implementation lives in main; tests pass a fake.
type Platform interface {
	Emit(event string, payload any)
	PickDirectory() (string, error)
	ClipboardWrite(text string) error
	ClipboardRead() (string, error)
	OpenURL(url string)
	Quit()
	// AttentionSupported reports whether RequestAttention does anything.
	AttentionSupported() bool
	RequestAttention()
}

// App is the single struct bound to the webview. Every method posts to the
// core and returns its reply; errors become promise rejections.
type App struct {
	core     *core.Core
	hub      *Hub
	platform Platform
	version  string
	retry    func() error
	updates  Updater
	updating atomic.Bool
}

// Updater checks for and installs new app releases. Install returns the
// path that Relaunch opens once the app has quit.
type Updater struct {
	Check    func(ctx context.Context) updater.Info
	Install  func(ctx context.Context, info updater.Info) (string, error)
	Relaunch func(target string) error
}

// SetUpdater enables UpdateCheck and UpdateInstall. Without it both report
// no update.
func (a *App) SetUpdater(u Updater) { a.updates = u }

// NewApp wires the bound methods. retry reattempts the holder connection
// after a blocked start; it may be nil. Only methods meant for the webview
// are exported, because Wails binds every exported method.
func NewApp(c *core.Core, hub *Hub, platform Platform, version string, retry func() error) *App {
	return &App{core: c, hub: hub, platform: platform, version: version, retry: retry}
}

// Snapshot returns the full view model.
func (a *App) Snapshot() (core.View, error) { return a.core.Snapshot() }

// Version returns the app version.
func (a *App) Version() string { return a.version }

// WorkspaceCreate opens a directory as a workspace.
func (a *App) WorkspaceCreate(path, agent string, groupPath []string) error {
	return a.core.WorkspaceCreate(path, agent, groupPath)
}

// WorkspacePickDirectory opens the native directory dialog.
func (a *App) WorkspacePickDirectory() (string, error) {
	if a.platform == nil {
		return "", nil
	}
	return a.platform.PickDirectory()
}

// WorkspaceSelect selects a workspace.
func (a *App) WorkspaceSelect(index int) error { return a.core.WorkspaceSelect(index) }

// WorkspaceClose closes a workspace and kills its panes.
func (a *App) WorkspaceClose(index int) error { return a.core.WorkspaceClose(index) }

// WorkspaceReorder moves a workspace in the sidebar.
func (a *App) WorkspaceReorder(from, to int) error { return a.core.WorkspaceReorder(from, to) }

// WorkspaceMove changes a workspace's group.
func (a *App) WorkspaceMove(index int, groupPath []string) error {
	return a.core.WorkspaceMove(index, groupPath)
}

// SubrepoOpen starts a pane in a nested repository: where is tab, right, or down.
func (a *App) SubrepoOpen(workspace int, path, kind, where string) error {
	return a.core.SubrepoOpen(workspace, path, kind, where)
}

// SubrepoHide hides a nested repository from a workspace's list.
func (a *App) SubrepoHide(workspace int, path string) error {
	return a.core.SubrepoHide(workspace, path)
}

// SubrepoUnhide shows a workspace's hidden folders again.
func (a *App) SubrepoUnhide(workspace int) error { return a.core.SubrepoUnhide(workspace) }

// WorkspaceSetFolded folds a workspace's sidebar rows.
func (a *App) WorkspaceSetFolded(index int, folded bool) error {
	return a.core.WorkspaceSetFolded(index, folded)
}

// WorkspaceRename renames a workspace.
func (a *App) WorkspaceRename(index int, name string) error {
	return a.core.WorkspaceRename(index, name)
}

// CompletePath completes a directory prefix for the workspace prompt.
func (a *App) CompletePath(prefix string) []string {
	matches := paths.CompleteDir(prefix)
	if matches == nil {
		return []string{}
	}
	return matches
}

// Find returns finder candidates for a fuzzy query.
func (a *App) Find(query string) ([]core.FindCandidate, error) { return a.core.Find(query) }

// TabCreate adds a tab in the selected workspace.
func (a *App) TabCreate(kind string) error { return a.core.TabCreate(kind) }

// TabSelect activates a tab.
func (a *App) TabSelect(workspace, tab int) error { return a.core.TabSelect(workspace, tab) }

// TabClose closes a tab; the last one is refused.
func (a *App) TabClose(workspace, tab int) error { return a.core.TabClose(workspace, tab) }

// TabRename names a tab.
func (a *App) TabRename(workspace, tab int, name string) error {
	return a.core.TabRename(workspace, tab, name)
}

// PaneCreate splits the focused pane: right, down, tab, primary, secondary.
func (a *App) PaneCreate(kind, split string) error { return a.core.PaneCreate(kind, split) }

// PaneCreateFloat opens an ephemeral floating pane.
func (a *App) PaneCreateFloat(kind, anchor string, widthPct, heightPct int) error {
	return a.core.PaneCreateFloat(kind, anchor, widthPct, heightPct)
}

// PaneFocus focuses a pane.
func (a *App) PaneFocus(id int) error { return a.core.PaneFocus(id) }

// PaneClose kills a pane.
func (a *App) PaneClose(id int) error { return a.core.PaneClose(id) }

// PaneMove rearranges two panes: zone center swaps, an edge splits there.
func (a *App) PaneMove(paneID, targetID int, zone string) error {
	return a.core.PaneMove(paneID, targetID, zone)
}

// PaneRename renames a pane.
func (a *App) PaneRename(id int, name string) error { return a.core.PaneRename(id, name) }

// PaneRestart relaunches an exited pane.
func (a *App) PaneRestart(id int) error { return a.core.PaneRestart(id) }

// PaneAttachResult is everything a mounting terminal needs.
type PaneAttachResult struct {
	Cols     int    `json:"cols"`
	Rows     int    `json:"rows"`
	Seq      uint64 `json:"seq"`
	Preamble string `json:"preamble"`
	Replay   string `json:"replay"`
	Running  bool   `json:"running"`
}

// PaneAttach returns the mode preamble, replay ring, and the seq after
// which live pane:output events continue.
func (a *App) PaneAttach(id int) (PaneAttachResult, error) {
	info, err := a.core.PaneAttachInfo(id)
	if err != nil {
		return PaneAttachResult{}, err
	}
	replay := a.hub.Snapshot(id)
	return PaneAttachResult{Cols: info.Cols, Rows: info.Rows, Seq: replay.Seq,
		Preamble: info.Preamble, Replay: replay.Replay, Running: info.Running}, nil
}

// PaneInput forwards UTF-8 input. Fire and forget.
func (a *App) PaneInput(id int, data string) { a.core.PaneInput(id, []byte(data)) }

// PanePaste writes pasted text, bracketed for agents and programs that ask
// for it, as the TUI does. Fire and forget.
func (a *App) PanePaste(id int, text string) { a.core.PanePaste(id, text) }

// PaneInputBinary forwards base64 binary input. Fire and forget.
func (a *App) PaneInputBinary(id int, encoded string) {
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err == nil {
		a.core.PaneInput(id, data)
	}
}

// PaneResize resizes a pane's PTY to its content box.
func (a *App) PaneResize(id, cols, rows int) { a.core.PaneResize(id, cols, rows) }

// PaneScrollReset is reserved; scrollback lives in xterm.js in v1.
func (a *App) PaneScrollReset(int) {}

// SplitSetRatio sets a split ratio by tree path.
func (a *App) SplitSetRatio(path []int, ratio float64) error {
	return a.core.SplitSetRatio(path, ratio)
}

// SplitEqualize evens the active tab, or one split when path is given.
func (a *App) SplitEqualize(path []int) error { return a.core.SplitEqualize(path) }

// SplitDirectionCycle advances the default split direction.
func (a *App) SplitDirectionCycle() error { return a.core.SplitDirectionCycle() }

// Navigate runs a keyboard action such as pane-next or tab-3.
func (a *App) Navigate(action string) error { return a.core.Navigate(action) }

// MenuAction publishes a custom menu entry selection.
func (a *App) MenuAction(actionID, target string, paneID, workspace, tab int) error {
	return a.core.MenuAction(actionID, target, paneID, workspace, tab)
}

// SettingsGet returns the settings dialog content.
func (a *App) SettingsGet() (core.SettingsView, error) {
	supported := a.platform != nil && a.platform.AttentionSupported()
	return a.core.Settings(supported)
}

// SettingsSet applies a partial settings update.
func (a *App) SettingsSet(patch core.SettingsPatch) error { return a.core.SettingsSet(patch) }

// SoundPreview plays a sound.
func (a *App) SoundPreview(name string) error {
	if !sound.IsKind(name) {
		return fmt.Errorf("unknown sound %q", name)
	}
	sound.Play(name)
	return nil
}

// ThemeList returns built-in and user themes.
func (a *App) ThemeList() []string { return theme.Names() }

// ClipboardWrite copies text to the system clipboard.
func (a *App) ClipboardWrite(text string) error {
	if a.platform == nil {
		return nil
	}
	return a.platform.ClipboardWrite(text)
}

// ClipboardRead reads the system clipboard.
func (a *App) ClipboardRead() (string, error) {
	if a.platform == nil {
		return "", nil
	}
	return a.platform.ClipboardRead()
}

// OpenExternal opens an http or https URL in the system browser.
func (a *App) OpenExternal(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return fmt.Errorf("only http and https links can be opened")
	}
	if a.platform != nil {
		a.platform.OpenURL(parsed.String())
	}
	return nil
}

// WindowFocus reports webview window focus changes.
func (a *App) WindowFocus(focused bool) { a.core.SetWindowFocused(focused) }

// DismissProblems clears banner messages.
func (a *App) DismissProblems() { a.core.DismissProblems() }

// RetryHolder reattempts the holder connection after a blocked start.
func (a *App) RetryHolder() error {
	if a.retry == nil {
		return nil
	}
	return a.retry()
}

// UpdateCheck reports whether a newer release is available.
func (a *App) UpdateCheck() updater.Info {
	if a.updates.Check == nil {
		return updater.Info{}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return a.updates.Check(ctx)
}

// UpdateInstall downloads and verifies the latest release, installs it over
// the running app, and restarts. Panes detach from the session holder on
// quit, so the new version reattaches them.
func (a *App) UpdateInstall() error {
	if a.updates.Check == nil || a.updates.Install == nil || a.updates.Relaunch == nil {
		return errors.New("updates are not supported in this build")
	}
	if !a.updating.CompareAndSwap(false, true) {
		return errors.New("an update is already in progress")
	}
	defer a.updating.Store(false)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	info := a.updates.Check(ctx)
	if !info.Available {
		return errors.New("hrdx is already up to date")
	}
	target, err := a.updates.Install(ctx, info)
	if err != nil {
		return err
	}
	if err := a.updates.Relaunch(target); err != nil {
		return fmt.Errorf("update %s installed, restart manually: %w", info.Latest, err)
	}
	a.Quit()
	return nil
}

// Quit persists, detaches from the holder, and exits. Sessions keep running.
func (a *App) Quit() {
	if a.platform != nil {
		a.platform.Quit()
	}
}
