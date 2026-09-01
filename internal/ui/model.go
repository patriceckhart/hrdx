package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
	"github.com/patriceckhart/hrdx/internal/api"
	"github.com/patriceckhart/hrdx/internal/holder"
	"github.com/patriceckhart/hrdx/internal/state"
	"github.com/patriceckhart/hrdx/internal/term"
	"github.com/patriceckhart/hrdx/internal/update"
)

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

const sidebarWidth = 26

type inputMode int

const (
	modeTerminal inputMode = iota
	modePrefix
	modeNewSpace
	modeRename
	modeMenu
	modeSettings
	modeFind
)

// menuItem is one entry of the right-click context menu.
type menuItem struct {
	label  string
	action string
}

var paneMenuItems = []menuItem{
	{"Rename pane", "rename"},
	{"Split left...", "pick-left"},
	{"Split right...", "pick-right"},
	{"Split up...", "pick-up"},
	{"Split down...", "pick-down"},
	{"Close pane", "close"},
}

var tabMenuItems = []menuItem{
	{"New tab", "tab-new"},
	{"Rename tab", "tab-rename"},
	{"Close tab", "tab-close"},
}

var spaceMenuItems = []menuItem{
	{"Rename", "space-rename"},
	{"Close", "space-close"},
	{"New tab", "space-tab"},
}

// Config carries the launch settings for agent panes.
type Config struct {
	DefaultAgent string            // agent kind used for new panes and splits
	AgentBins    map[string]string // per-agent binary overrides
	Resume       bool              // resume the latest session for new agent panes
	ZotArgs      []string          // extra args passed to zot panes only
	Shell        string
	Version      string // current binary version for the update check
	CacheDir     string // directory for the update check cache
}

type floatPlacement struct {
	anchor              string
	widthPct, heightPct int
}

type pane struct {
	id       int
	name     string
	kind     string // agent kind ("zot", "pi", "claude", "codex") or "shell"
	term     *term.Pane
	running  bool
	failure  string
	resume   bool  // restored agent pane: relaunch resuming its session
	session  int64 // holder session to reattach to on restore, 0 = start fresh
	floating *floatPlacement
}

// tab is one tabbed layout of panes inside a workspace.
type tab struct {
	name     string
	panes    []*pane
	layout   *splitNode
	selected int
}

type space struct {
	name   string
	cwd    string
	tabs   []*tab
	active int
}

// tab returns the active tab, never nil for a live workspace.
func (s *space) tab() *tab {
	if len(s.tabs) == 0 {
		s.tabs = []*tab{{}}
		s.active = 0
	}
	s.active = clampInt(s.active, 0, len(s.tabs)-1)
	return s.tabs[s.active]
}

type Model struct {
	config        Config
	spaces        []*space
	selected      int
	nextID        int
	width         int
	height        int
	mode          inputMode
	input         textinput.Model
	status        string
	statusIsInfo  bool // status is a notice, not an error: accent styling
	kittyPushed   bool
	drag          *splitNode
	dragFull      rect
	statePath     string
	spinFrame     int
	ticking       bool
	selPane       *pane
	selRect       rect
	statusSeq     int
	menuPane      *pane
	menuTab       *tab
	menuSpace     *space
	menuAt        rect // menu box in body coordinates
	menuIndex     int
	customMenus   []api.MenuRegister // ephemeral socket API context-menu entries
	pickItems     []menuItem         // kind picker entries while it is open
	pickAction    string             // "space", "tab", "split-right", "split-down", "settings"
	pickSpace     *space             // tab target for the picker
	pickPath      string             // directory for a pending new workspace
	renamePane    *pane
	renameTab     *tab
	renameSpace   *space
	branches      map[string]branchInfo
	disabled      map[string]bool // agent kinds switched off in settings
	soundOn       bool            // play a sound when an agent finishes a turn
	soundKind     string          // which sound: "ding" or a sounds.json entry
	notifyOn      bool            // post a desktop notification on finish
	themeName     string          // active bundled or user theme name
	wasBusy       map[int]bool    // pane id -> spinner seen, for finish notifications
	soundSeq      map[int]uint64  // invalidates stale agent-finish confirmations
	paneAttention map[int]bool    // completed while unfocused; cleared only by focus
	settingsTab   int             // active tab of the settings window
	settingsIndex int             // selected row of the settings window
	completions   []string
	completion    int
	sideScroll    int
	dragSpace     *space            // sidebar workspace being dragged for reordering
	dragMoved     bool              // the drag moved rows: suppress persist-less release
	hintScroll    int               // first visible hint in the ctrl+b footer row
	updateInfo    update.Info       // populated async; Available drives the notice
	events        *api.Broadcaster  // API event fan-out, nil-safe
	holder        *holder.Client    // session holder connection, nil = local panes
	blurredAt     time.Time         // when the terminal lost focus, for stale detection
	appBlurred    bool              // terminal application currently lacks focus
	cursorSink    *CursorSink       // publishes the hardware cursor position, nil-safe
	prefixKeys    map[string]string // prefix key -> action, defaults plus keys.json
	prefixTrigger string            // key that enters prefix mode from a live terminal
	keyOverrides  map[string]string // action -> key from keys.json, for hints
	findIndex     int               // selected row of the find window
	quitting      bool              // shutting down: exits must not edit the layout
}

// staleAfter is how long the terminal must have been unfocused before a
// focus regain triggers the full repaint. System sleep exceeds this
// easily; alt-tabbing between apps never does.
const staleAfter = 30 * time.Second

// SetHolder wires the session holder client; must be called before the
// program runs. With a holder, pane processes live in the holder and
// survive TUI restarts.
func (m *Model) SetHolder(client *holder.Client) {
	m.holder = client
}

// SetCursorSink wires the hardware cursor publisher; must be called before
// the program runs.
func (m *Model) SetCursorSink(sink *CursorSink) {
	m.cursorSink = sink
}

// HolderExitMsg reports that a holder session's process exited; send it
// into the program from the holder client's exit handler.
type HolderExitMsg struct{ Session int64 }

// gcHolderSessions kills holder sessions no pane references anymore
// (left over from --fresh starts or corrupted state).
func (m *Model) gcHolderSessions() {
	if m.holder == nil {
		return
	}
	referenced := map[int64]bool{}
	for _, currentSpace := range m.spaces {
		for _, currentTab := range currentSpace.tabs {
			for _, currentPane := range currentTab.panes {
				if currentPane.session != 0 {
					referenced[currentPane.session] = true
				}
			}
		}
	}
	sessions, err := m.holder.List()
	if err != nil {
		return
	}
	for _, current := range sessions {
		if !referenced[current.ID] {
			m.holder.Kill(current.ID)
		}
	}
}

// SetEventBroadcaster wires the API event fan-out; must be called before
// the program runs.
func (m *Model) SetEventBroadcaster(events *api.Broadcaster) {
	m.events = events
}

// menuItems returns the entries of the currently open context menu.
func (m Model) menuItems() []menuItem {
	if m.pickAction != "" {
		return m.pickItems
	}
	target := "pane"
	builtins := paneMenuItems
	if m.menuSpace != nil {
		target = "sidebar"
		builtins = spaceMenuItems
	} else if m.menuTab != nil {
		target = "tab"
		builtins = tabMenuItems
		if owner := m.spaceByTab(m.menuTab); owner != nil && len(owner.tabs) == 1 {
			builtins = tabMenuItems[:len(tabMenuItems)-1]
		}
	} else if m.menuPane != nil {
		if owner := m.tabByPaneID(m.menuPane.id); owner != nil && len(owner.panes) == 1 {
			builtins = paneMenuItems[:len(paneMenuItems)-1]
		}
	}

	items := append([]menuItem{}, builtins...)
	for _, registration := range m.customMenus {
		if registration.Target == target {
			items = append(items, menuItem{registration.Label, "custom:" + registration.ActionID})
		}
	}
	return items
}

func (m Model) spaceByTab(target *tab) *space {
	for _, currentSpace := range m.spaces {
		for _, currentTab := range currentSpace.tabs {
			if currentTab == target {
				return currentSpace
			}
		}
	}
	return nil
}

func (m Model) tabByPaneID(id int) *tab {
	for _, currentSpace := range m.spaces {
		for _, currentTab := range currentSpace.tabs {
			for _, currentPane := range currentTab.panes {
				if currentPane.id == id {
					return currentTab
				}
			}
		}
	}
	return nil
}

type statusExpireMsg struct{ seq int }

// flashStatus shows an error message in the footer that clears after two
// seconds.
func (m *Model) flashStatus(text string) tea.Cmd {
	m.status = text
	m.statusIsInfo = false
	m.statusSeq++
	seq := m.statusSeq
	return tea.Tick(2*time.Second, func(time.Time) tea.Msg { return statusExpireMsg{seq: seq} })
}

// flashInfo is flashStatus for non-error notices (copies, agent cycling);
// it renders in the accent color instead of error red.
func (m *Model) flashInfo(text string) tea.Cmd {
	cmd := m.flashStatus(text)
	m.statusIsInfo = true
	return cmd
}

// soundConfirmMsg fires a moment after a pane went from busy to idle; the
// completion only counts when the pane is still idle then, so short redraw
// gaps in the child's spinner do not create attention or ring mid-turn.
type soundConfirmMsg struct {
	id  int
	seq uint64
}

func (m *Model) clearFocusedAttention() {
	if focused := m.currentPane(); focused != nil {
		delete(m.paneAttention, focused.id)
	}
}

// trackBusy watches one pane's agent-busy state. On a busy -> idle
// transition it schedules a debounced completion confirmation and notifies
// API subscribers of the state change.
func (m *Model) trackBusy(target *pane) tea.Cmd {
	if m.paneBusy(target) {
		if !m.wasBusy[target.id] {
			m.soundSeq[target.id]++
			m.publish(api.Event{Event: api.EventPaneBusyChanged,
				Data: api.PaneEvent{Pane: target.id, Name: target.name, Kind: target.kind, Busy: true}})
		}
		m.wasBusy[target.id] = true
		return nil
	}
	if !m.wasBusy[target.id] {
		return nil
	}
	delete(m.wasBusy, target.id)
	m.publish(api.Event{Event: api.EventPaneBusyChanged,
		Data: api.PaneEvent{Pane: target.id, Name: target.name, Kind: target.kind, Busy: false}})
	m.soundSeq[target.id]++
	id, seq := target.id, m.soundSeq[target.id]
	return tea.Tick(1500*time.Millisecond, func(time.Time) tea.Msg {
		return soundConfirmMsg{id: id, seq: seq}
	})
}

// spinnerFrames matches zot's own braille spinner.
var spinnerFrames = []string{"⠋", "⠙", "⠚", "⠞", "⠖", "⠦", "⠴", "⠲", "⠳", "⠓"}

type spinTickMsg struct{}

func spinTick() tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(time.Time) tea.Msg { return spinTickMsg{} })
}

// anyBusy reports whether any zot pane is currently working.
func (m *Model) anyBusy() bool {
	for _, currentSpace := range m.spaces {
		for _, currentTab := range currentSpace.tabs {
			for _, currentPane := range currentTab.panes {
				if m.paneBusy(currentPane) {
					return true
				}
			}
		}
	}
	return false
}

// Kitty keyboard protocol sequences, mirroring zot's own pair. The push must
// happen while the alternate screen is active because the protocol keeps a
// separate mode stack per screen; pushing on the main screen has no effect
// inside the TUI. modifyOtherKeys is included as an xterm fallback.
const (
	seqEnhancedKeysOn  = "\x1b[>1u\x1b[>4;2m"
	seqEnhancedKeysOff = "\x1b[<u\x1b[>4m"
)

type paneUpdateMsg struct {
	id   int
	open bool
}

type paneStartedMsg struct {
	id   int
	term *term.Pane
	err  error
}

// New builds the model. Saved workspaces from statePath are restored first;
// paths not already present are added on top. Pass statePath == "" to
// disable persistence.
func New(config Config, paths []string, statePath string, saved state.State) Model {
	if config.Shell == "" {
		config.Shell = "/bin/zsh"
	}
	// Custom harnesses live next to the state file and must register
	// before any kind validation below.
	harnessProblem := ""
	keymapOverrides := map[string]string{}
	if statePath != "" {
		harnessProblem = loadHarnesses(filepath.Dir(statePath))
		overrides, keymapProblem := loadKeymap(filepath.Dir(statePath))
		keymapOverrides = overrides
		if keymapProblem != "" {
			if harnessProblem != "" {
				harnessProblem += "; "
			}
			harnessProblem += keymapProblem
		}
	}
	if !isAgentKind(config.DefaultAgent) {
		config.DefaultAgent = "zot"
	}
	input := textinput.New()
	input.Placeholder = "directory (e.g. ~/Developer/api)"
	input.Prompt = ""

	model := Model{config: config, input: input, nextID: 1, statePath: statePath,
		branches: map[string]branchInfo{}, disabled: map[string]bool{},
		wasBusy: map[int]bool{}, soundSeq: map[int]uint64{}, paneAttention: map[int]bool{},
		soundOn: saved.Sound, status: harnessProblem,
		soundKind: saved.SoundKind, notifyOn: saved.Notify, themeName: saved.Theme,
		prefixKeys: buildPrefixKeys(keymapOverrides), keyOverrides: keymapOverrides}
	model.prefixTrigger = model.primaryKey("prefix")
	if statePath != "" {
		for _, problem := range []string{
			loadSounds(filepath.Dir(statePath)),
			loadThemes(filepath.Dir(statePath)),
		} {
			if problem == "" {
				continue
			}
			if model.status != "" {
				model.status += "; "
			}
			model.status += problem
		}
	}
	if !isSoundKind(model.soundKind) {
		model.soundKind = defaultSoundKind
	}
	if !isThemeName(model.themeName) {
		model.themeName = defaultThemeName
	}
	applyTheme(model.themeName)
	for _, kind := range saved.DisabledAgents {
		if isAgentKind(kind) {
			model.disabled[kind] = true
		}
	}
	// A disabled default agent silently moves to the first enabled one.
	if model.disabled[model.config.DefaultAgent] {
		if available := model.availableAgents(); len(available) > 0 {
			model.config.DefaultAgent = available[0]
		}
	}
	model.restore(saved)
	for _, path := range paths {
		exists := false
		for _, currentSpace := range model.spaces {
			if currentSpace.cwd == path {
				exists = true
				break
			}
		}
		if !exists {
			model.addSpace(path)
		}
	}
	return model
}

func (m *Model) addSpace(path string) *space {
	return m.addSpaceKind(path, m.config.DefaultAgent)
}

func (m *Model) addSpaceKind(path, kind string) *space {
	newSpace := &space{name: filepath.Base(path), cwd: path, tabs: []*tab{{}}}
	m.spaces = append(m.spaces, newSpace)
	m.addPane(newSpace, kind, true)
	return newSpace
}

// addTab appends a fresh tab with one pane of the given kind, activating it.
func (m *Model) addTab(target *space, kind string) *pane {
	target.tabs = append(target.tabs, &tab{})
	target.active = len(target.tabs) - 1
	return m.addPane(target, kind, true)
}

// addPane creates a pane and inserts it into the active tab's layout by
// splitting the focused pane. vertical means a side-by-side split; before
// puts the new pane left of or above the focused one.
func (m *Model) addPane(target *space, kind string, vertical bool) *pane {
	return m.addPaneSide(target, kind, vertical, false)
}

func (m *Model) addPaneSide(target *space, kind string, vertical, before bool) *pane {
	currentTab := target.tab()
	newPane := m.newPane(target, kind)

	if currentTab.layout == nil {
		currentTab.layout = leafNode(newPane)
	} else {
		focused := currentTab.panes[currentTab.selected]
		if focused.floating != nil {
			currentTab.layout.walk(func(candidate *pane) { focused = candidate })
		}
		insertAtSide(currentTab.layout, focused, newPane, vertical, before)
	}
	currentTab.panes = append(currentTab.panes, newPane)
	currentTab.selected = len(currentTab.panes) - 1
	return newPane
}

func (m *Model) addFloatingPane(target *space, kind, anchor string, widthPct, heightPct int) *pane {
	currentTab := target.tab()
	newPane := m.newPane(target, kind)
	newPane.floating = &floatPlacement{anchor: anchor, widthPct: widthPct, heightPct: heightPct}
	currentTab.panes = append(currentTab.panes, newPane)
	currentTab.selected = len(currentTab.panes) - 1
	return newPane
}

func (m *Model) newPane(target *space, kind string) *pane {
	count := 1
	for _, currentTab := range target.tabs {
		for _, existing := range currentTab.panes {
			if existing.kind == kind {
				count++
			}
		}
	}
	newPane := &pane{
		id: m.nextID, name: fmt.Sprintf("%s %d", kind, count), kind: kind,
		resume: m.config.Resume && isAgentKind(kind),
	}
	m.nextID++
	return newPane
}

// resolveDir expands ~, makes the path absolute, and requires a directory.
func resolveDir(value string) (string, error) {
	if value == "~" || strings.HasPrefix(value, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		value = filepath.Join(home, strings.TrimPrefix(value, "~"))
	}
	path, err := filepath.Abs(value)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("%s does not exist", path)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s is not a directory", path)
	}
	return path, nil
}

// updateInfoMsg delivers the async update check result.
type updateInfoMsg struct{ info update.Info }

func checkForUpdate(cacheDir, version string) tea.Cmd {
	return func() tea.Msg {
		info, ok := <-update.CheckAsync(cacheDir, version)
		if !ok {
			return updateInfoMsg{}
		}
		return updateInfoMsg{info: info}
	}
}

func (m Model) Init() tea.Cmd {
	var commands []tea.Cmd
	commands = append(commands, checkForUpdate(m.config.CacheDir, m.config.Version))
	m.gcHolderSessions()
	for _, currentSpace := range m.spaces {
		for _, currentTab := range currentSpace.tabs {
			for _, currentPane := range currentTab.panes {
				commands = append(commands, m.startPane(currentSpace, currentPane))
			}
		}
	}
	return tea.Batch(commands...)
}

func (m Model) startPane(owner *space, target *pane) tea.Cmd {
	// Before the first WindowSizeMsg the layout collapses to the minimum
	// pane size; starting PTYs at 8x2 makes TUIs render wrapped garbage.
	// Keep the 80x24 default until the real size is known.
	cols, rows := 80, 24
	if m.width > 0 && m.height > 0 {
		for _, currentTab := range owner.tabs {
			for _, pr := range m.allPaneLayout(currentTab) {
				if pr.pane == target {
					inner := pr.r.inner()
					cols, rows = inner.w, inner.h
				}
			}
		}
	}
	command := m.config.Shell
	args := shellArgsFor(runtime.GOOS)
	if spec := agentByKind(target.kind); spec != nil {
		command = m.config.binaryFor(target.kind)
		args = append([]string{}, spec.args...)
		if target.kind == "zot" {
			args = append(args, m.config.ZotArgs...)
		}
		if target.resume && len(spec.resume) > 0 && !contains(args, spec.resume[0]) {
			if spec.resumeFirst {
				args = append(append([]string{}, spec.resume...), args...)
			} else {
				args = append(args, spec.resume...)
			}
		}
	}
	cwd := owner.cwd
	id := target.id

	if m.holder != nil {
		client := m.holder
		reattach := target.session
		return func() tea.Msg {
			started, err := startHolderPane(client, reattach, command, args, cwd, cols, rows)
			return paneStartedMsg{id: id, term: started, err: err}
		}
	}

	return func() tea.Msg {
		started, err := term.Start(command, args, cwd, cols, rows)
		return paneStartedMsg{id: id, term: started, err: err}
	}
}

// shellArgsFor returns the default arguments for an interactive shell.
// Unix shells retain the existing login-shell behavior. Native Windows
// shells such as cmd.exe and powershell.exe do not accept the Unix -l flag.
func shellArgsFor(goos string) []string {
	if goos == "windows" {
		return nil
	}
	return []string{"-l"}
}

// startHolderPane starts (or reattaches to) a session in the holder and
// wires its output into a fresh pane terminal.
func startHolderPane(client *holder.Client, reattach int64, command string, args []string, cwd string, cols, rows int) (*term.Pane, error) {
	session := reattach
	if session != 0 {
		sessions, err := client.List()
		if err != nil {
			return nil, fmt.Errorf("inspect holder sessions: %w", err)
		}
		if !holderSessionMatchesWorkspace(sessions, session, cwd) {
			// State and holder writes are independent. After an interrupted
			// shutdown, a persisted id can refer to a session from another
			// workspace. Never attach that PTY to the wrong workspace.
			session = 0
		}
	}
	if session == 0 {
		started, err := client.Start(command, args, cwd, term.PaneEnv(), cols, rows)
		if err != nil {
			return nil, err
		}
		session = started
	}
	pane := term.NewHolderPane(client, session, cols, rows)
	running, err := client.Attach(session, cols, rows, pane.Feed)
	if err != nil {
		if reattach != 0 {
			// The session vanished (holder restarted): start fresh.
			return startHolderPane(client, 0, command, args, cwd, cols, rows)
		}
		return nil, err
	}
	if !running {
		pane.MarkExited()
	}
	return pane, nil
}

func holderSessionMatchesWorkspace(sessions []holder.SessionInfo, session int64, cwd string) bool {
	wanted := filepath.Clean(cwd)
	for _, current := range sessions {
		if current.ID == session {
			return filepath.Clean(current.CWD) == wanted
		}
	}
	return false
}

// terminalArea is the local rect of the pane region (right of the sidebar,
// below the tab bar, above the footer). Row 0 of the body is the tab bar.
func (m Model) terminalArea() rect {
	return rect{0, 0, max(minPaneCols, m.width-sidebarWidth-1), max(minPaneRows, m.height-3)}
}

func (m Model) layoutFor(target *tab) []paneRect {
	panes, _ := m.layoutAll(target)
	return panes
}

func (m Model) layoutAll(target *tab) ([]paneRect, []divRect) {
	var panes []paneRect
	var divs []divRect
	layoutNode(target.layout, m.terminalArea(), &panes, &divs)
	return panes, divs
}

func (m Model) floatingLayout(target *tab) []paneRect {
	area := m.terminalArea()
	var panes []paneRect
	for _, current := range target.panes {
		placement := current.floating
		if placement == nil {
			continue
		}
		width := clampInt(area.w*placement.widthPct/100, minPaneCols, area.w)
		height := clampInt(area.h*placement.heightPct/100, minPaneRows, area.h)
		x := area.x + (area.w-width)/2
		y := area.y + (area.h-height)/2
		switch placement.anchor {
		case "top":
			y = area.y
		case "bottom":
			y = area.y + area.h - height
		case "left":
			x = area.x
		case "right":
			x = area.x + area.w - width
		}
		panes = append(panes, paneRect{pane: current, r: rect{x: x, y: y, w: width, h: height}})
	}
	return panes
}

func (m Model) allPaneLayout(target *tab) []paneRect {
	panes := m.layoutFor(target)
	return append(panes, m.floatingLayout(target)...)
}

// resizePanes pushes the layout geometry of every tab into its PTYs, not
// just the active one: hidden tabs must track size changes too, otherwise
// switching back shows a stale-size buffer with wrapped leftovers.
// PTYs get the content size inside each pane's border.
func (m *Model) resizePanes(target *space) {
	for _, currentTab := range target.tabs {
		for _, pr := range m.allPaneLayout(currentTab) {
			if pr.pane.term != nil {
				inner := pr.r.inner()
				pr.pane.term.Resize(inner.w, inner.h)
			}
		}
	}
}

func (m Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case tea.WindowSizeMsg:
		if !m.kittyPushed {
			// The first size message arrives after Bubble Tea entered the
			// alt screen, so the push lands on the correct mode stack.
			_, _ = os.Stdout.WriteString(seqEnhancedKeysOn)
			m.kittyPushed = true
		}
		m.width, m.height = msg.Width, msg.Height
		for _, currentSpace := range m.spaces {
			m.resizePanes(currentSpace)
		}
		return m, nil

	case updateInfoMsg:
		m.updateInfo = msg.info
		return m, nil

	case tea.FocusMsg:
		m.appBlurred = false
		m.clearFocusedAttention()
		// Regaining focus after a long absence (system sleep, display
		// reattach) can leave stale artifacts: the renderer's line cache
		// no longer matches what is really on screen. Repaint everything
		// then. Quick app switches skip the clear, which would otherwise
		// flicker the whole UI on every alt-tab.
		blurredAt := m.blurredAt
		m.blurredAt = time.Time{}
		if blurredAt.IsZero() || time.Since(blurredAt) < staleAfter {
			return m, nil
		}
		for _, currentSpace := range m.spaces {
			m.resizePanes(currentSpace)
		}
		return m, tea.ClearScreen

	case tea.BlurMsg:
		m.blurredAt = time.Now()
		m.appBlurred = true
		return m, nil

	case paneStartedMsg:
		target, owner := m.paneByID(msg.id)
		if target == nil {
			if msg.term != nil {
				msg.term.Close()
			}
			return m, nil
		}
		if msg.err != nil {
			target.failure = msg.err.Error()
			return m, nil
		}
		target.term = msg.term
		target.running = msg.term.Running()
		if session := msg.term.HolderSession(); session != 0 && session != target.session {
			// Holder-backed pane: remember the session id so a restart
			// reattaches instead of starting fresh.
			target.session = session
			m.persist()
		}
		m.resizePanes(owner)
		return m, waitForUpdate(msg.id, msg.term.Updates())

	case paneUpdateMsg:
		target, owner := m.paneByID(msg.id)
		if target == nil || target.term == nil {
			return m, nil
		}
		if !msg.open {
			target.running = false
			// The process is gone; the pane follows, like a closing terminal
			// window. Startup failures keep their pane via target.failure so
			// the error stays readable.
			if m.quitting {
				return m, nil
			}
			m.removePane(owner, target)
			return m, nil
		}
		var tick tea.Cmd
		if !m.ticking && m.anyBusy() {
			m.ticking = true
			tick = spinTick()
		}
		return m, tea.Batch(waitForUpdate(msg.id, target.term.Updates()), tick, m.trackBusy(target))

	case spinTickMsg:
		var sounds []tea.Cmd
		for _, currentSpace := range m.spaces {
			for _, currentTab := range currentSpace.tabs {
				for _, currentPane := range currentTab.panes {
					sounds = append(sounds, m.trackBusy(currentPane))
				}
			}
		}
		if m.anyBusy() {
			m.spinFrame = (m.spinFrame + 1) % len(spinnerFrames)
			sounds = append(sounds, spinTick())
			return m, tea.Batch(sounds...)
		}
		m.ticking = false
		return m, tea.Batch(sounds...)

	case soundConfirmMsg:
		if m.soundSeq[msg.id] != msg.seq {
			return m, nil
		}
		target, _ := m.paneByID(msg.id)
		if target == nil {
			return m, nil
		}
		if m.paneBusy(target) {
			// Working again already: the pause was a redraw gap, not a
			// finished turn. trackBusy re-arms the transition.
			m.wasBusy[msg.id] = true
			return m, nil
		}
		if focused := m.currentPane(); m.appBlurred || focused == nil || focused.id != target.id {
			m.paneAttention[target.id] = true
		}
		if m.soundOn {
			playSound(m.soundKind)
		}
		if m.notifyOn {
			systemNotify()
		}
		return m, nil

	case statusExpireMsg:
		if msg.seq == m.statusSeq {
			m.status = ""
		}
		return m, nil

	case tea.KeyMsg:
		return m.updateKey(msg)

	case tea.MouseMsg:
		return m.updateMouse(msg)

	case api.Request:
		return m, m.handleAPI(msg)

	case HolderExitMsg:
		for _, currentSpace := range m.spaces {
			for _, currentTab := range currentSpace.tabs {
				for _, currentPane := range currentTab.panes {
					if currentPane.session == msg.Session && currentPane.term != nil {
						currentPane.term.MarkExited()
					}
				}
			}
		}
		return m, nil
	}

	// Kitty CSI-u chords (ctrl+1, ...) arrive as unexported messages.
	if raw, ok := rawInputBytes(message); ok {
		return m.updateRaw(raw)
	}

	if m.mode == modeNewSpace {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(message)
		return m, cmd
	}
	return m, nil
}

// updateRaw routes raw CSI-u input from the host terminal. Kitty-aware
// children (zot) get the sequence verbatim so chords like ctrl+1 survive;
// legacy children (shells) get the classic encoding instead.
func (m Model) updateRaw(raw []byte) (tea.Model, tea.Cmd) {
	// Shift+PgUp / Shift+PgDn scroll the focused pane's history.
	if m.mode == modeTerminal {
		switch string(raw) {
		case "\x1b[5;2~":
			m.scrollCurrent(m.pageStep())
			return m, nil
		case "\x1b[6;2~":
			m.scrollCurrent(-m.pageStep())
			return m, nil
		}
	}
	code, mods, ok := parseCSIU(raw)
	if ok && m.mode == modeTerminal {
		if legacy := legacyEncode(code, mods); len(legacy) > 0 && keyFromBytes(legacy, code, mods).String() == m.prefixTrigger {
			m.mode = modePrefix
			return m, nil
		}
	}
	if ok && m.mode != modeTerminal {
		// Translate the chord for the local input modes.
		if legacy := legacyEncode(code, mods); len(legacy) > 0 {
			return m.updateKey(keyFromBytes(legacy, code, mods))
		}
		return m, nil
	}
	current := m.currentPane()
	if current == nil || current.term == nil || !current.running {
		return m, nil
	}
	if !ok || current.term.KittyKeys() {
		current.term.Write(raw)
		return m, nil
	}
	if legacy := legacyEncode(code, mods); len(legacy) > 0 {
		current.term.Write(legacy)
	} else {
		// Modified keys such as ctrl+1 have no classic terminal encoding.
		// Preserve their CSI-u sequence even if the child's kitty protocol
		// enable sequence was missed during a session reattach.
		current.term.Write(raw)
	}
	return m, nil
}

// keyFromBytes maps a decoded chord onto the KeyMsg values the local modes use.
func keyFromBytes(legacy []byte, code rune, mods int) tea.KeyMsg {
	switch code {
	case 27:
		return tea.KeyMsg{Type: tea.KeyEsc}
	case 13:
		return tea.KeyMsg{Type: tea.KeyEnter}
	case 9:
		return tea.KeyMsg{Type: tea.KeyTab}
	case 127:
		return tea.KeyMsg{Type: tea.KeyBackspace}
	}
	if mods&modCtrl != 0 && code >= 'a' && code <= 'z' {
		return tea.KeyMsg{Type: tea.KeyType(code - 'a' + 1), Alt: mods&modAlt != 0}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(string(legacy)), Alt: mods&modAlt != 0}
}

func (m Model) updateKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.mode {
	case modeRename:
		switch msg.String() {
		case "esc":
			m.mode = modeTerminal
			m.renamePane = nil
			m.renameTab = nil
			m.renameSpace = nil
			m.input.Blur()
			return m, nil
		case "enter":
			value := strings.TrimSpace(m.input.Value())
			if value != "" {
				if m.renamePane != nil {
					m.renamePane.name = value
				}
				if m.renameTab != nil {
					m.renameTab.name = value
				}
				if m.renameSpace != nil {
					m.renameSpace.name = value
				}
				m.persist()
			}
			m.mode = modeTerminal
			m.renamePane = nil
			m.renameTab = nil
			m.renameSpace = nil
			m.input.Blur()
			m.input.SetValue("")
			return m, nil
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd

	case modeSettings:
		return m.updateSettingsKey(msg)

	case modeMenu:
		items := m.menuItems()
		switch msg.String() {
		case "esc", "q":
			m.closeMenu()
			return m, nil
		case "up", "k":
			m.menuIndex = (m.menuIndex - 1 + len(items)) % len(items)
			return m, nil
		case "down", "j":
			m.menuIndex = (m.menuIndex + 1) % len(items)
			return m, nil
		case "enter":
			return m.runMenuAction(items[m.menuIndex].action)
		}
		return m, nil

	case modeNewSpace:
		switch msg.String() {
		case "esc":
			m.mode = modeTerminal
			m.clearCompletions()
			m.input.Blur()
			return m, nil
		case "tab", "shift+tab":
			delta := 1
			if msg.String() == "shift+tab" {
				delta = -1
			}
			m.advanceCompletion(delta)
			return m, nil
		case "enter":
			value := strings.TrimSpace(m.input.Value())
			if value == "" {
				m.mode = modeTerminal
				m.clearCompletions()
				m.input.Blur()
				return m, nil
			}
			path, err := resolveDir(value)
			if err != nil {
				return m, m.flashStatus(err.Error())
			}
			m.clearCompletions()
			m.input.Blur()
			m.input.SetValue("")
			m.status = ""
			m.openKindPicker("space", nil, path, rect{x: 1, y: max(1, m.height-4-len(m.availableAgents()))})
			return m, nil
		}
		m.clearCompletions()
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd

	case modeFind:
		return m.updateFindKey(msg)

	case modePrefix:
		// Pane cycling and hint scrolling stay in prefix mode so they can be
		// repeated. Escape returns input to the focused terminal.
		switch action := m.prefixKeys[msg.String()]; action {
		case "pane-next", "pane-prev":
			return m.runPrefix(msg)
		}
		switch msg.String() {
		case "right":
			m.hintScroll = min(m.hintScroll+1, len(m.prefixHintEntries())-1)
			return m, nil
		case "left":
			m.hintScroll = max(0, m.hintScroll-1)
			return m, nil
		case "esc":
			m.mode = modeTerminal
			m.hintScroll = 0
			return m, nil
		}
		m.mode = modeTerminal
		m.hintScroll = 0
		return m.runPrefix(msg)

	default: // modeTerminal
		if msg.String() == m.prefixTrigger {
			m.mode = modePrefix
			return m, nil
		}
		if current := m.currentPane(); current != nil && current.term != nil && current.running {
			if msg.Paste {
				// Agent TUIs support bracketed paste. Prefer it for agents even
				// when their one-time DECSET 2004 sequence was missed on reattach.
				bracketed := current.term.BracketedPaste() || m.paneAgentKind(current) != ""
				current.term.Write(term.EncodePaste(string(msg.Runes), bracketed))
			} else if !isSpuriousModifierKey(msg) {
				current.term.Write(term.EncodeKey(msg, current.term.AppCursor()))
			}
		}
		return m, nil
	}
}

// isSpuriousModifierKey reports a bare NUL keystroke with no other
// content. On Windows, Bubble Tea's console-input reader does not
// filter a lone Ctrl or Alt key-down the way it does Shift, so pressing
// either by itself (a fraction of a second before the paired letter's
// own event) surfaces as a phantom KeyRunes event carrying rune 0. No
// real keyboard input produces a literal NUL, so it is always safe to
// drop; forwarding it would otherwise type ^@ into the pane. Unix's
// genuine ctrl+@ arrives as a distinct KeyCtrlAt message and is
// unaffected.
func isSpuriousModifierKey(msg tea.KeyMsg) bool {
	return msg.Type == tea.KeyRunes && len(msg.Runes) == 1 && msg.Runes[0] == 0
}

func (m Model) runPrefix(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.prefixKeys[msg.String()] {
	case "literal":
		if current := m.currentPane(); current != nil && current.term != nil {
			current.term.Write([]byte{0x02})
		}
	case "quit":
		m.closeAll()
		return m, tea.Quit
	case "picker-right":
		m.openKindPicker("split-right", nil, "", rect{x: sidebarWidth + 2, y: 1})
	case "picker-down":
		m.openKindPicker("split-down", nil, "", rect{x: sidebarWidth + 2, y: 1})
	case "agent-right":
		return m.splitCurrent(m.config.DefaultAgent, true)
	case "agent-down":
		return m.splitCurrent(m.config.DefaultAgent, false)
	case "agent-cycle":
		m.cycleAgent()
		return m, m.flashInfo("default agent: " + m.config.DefaultAgent)
	case "shell-right":
		return m.splitCurrent("shell", true)
	case "shell-down":
		return m.splitCurrent("shell", false)
	case "workspace":
		return m.openNewSpaceInput()
	case "tab-new":
		if currentSpace := m.currentSpace(); currentSpace != nil {
			m.openKindPicker("tab", currentSpace, "", rect{x: sidebarWidth + 2, y: 1})
		}
	case "tab-next":
		m.selectTab(1)
	case "tab-prev":
		m.selectTab(-1)
	case "space-next":
		m.selectSpace(1)
	case "space-prev":
		m.selectSpace(-1)
	case "pane-next":
		m.cyclePane(1)
	case "pane-prev":
		m.cyclePane(-1)
	case "find":
		return m.openFind()
	case "close-pane":
		m.closeCurrentPane()
	case "close-space":
		m.closeCurrentSpace()
	case "equalize":
		m.equalizeCurrent()
	case "rename":
		return m.openRenameInput(m.currentPane())
	case "menu":
		if current := m.currentPane(); current != nil {
			m.openMenu(current, rect{x: 2, y: 1})
		}
	case "settings":
		m.openSettings()
	case "scroll-up":
		m.scrollCurrent(m.pageStep())
	case "scroll-down":
		m.scrollCurrent(-m.pageStep())
	case "live":
		if current := m.currentPane(); current != nil && current.term != nil {
			current.term.ResetScroll()
			current.term.ClearSelection()
		}
	}
	return m, nil
}

func (m Model) pageStep() int {
	return max(1, (m.height-2)/2)
}

func (m *Model) scrollCurrent(delta int) {
	if current := m.currentPane(); current != nil && current.term != nil {
		current.term.Scroll(delta)
	}
}

func (m *Model) splitCurrent(kind string, vertical bool) (tea.Model, tea.Cmd) {
	return m.splitCurrentSide(kind, vertical, false)
}

func (m *Model) splitCurrentSide(kind string, vertical, before bool) (tea.Model, tea.Cmd) {
	currentSpace := m.currentSpace()
	if currentSpace == nil {
		return *m, nil
	}
	newPane := m.addPaneSide(currentSpace, kind, vertical, before)
	m.resizePanes(currentSpace)
	m.persist()
	return *m, m.startPane(currentSpace, newPane)
}

func (m *Model) equalizeCurrent() {
	currentSpace := m.currentSpace()
	if currentSpace == nil {
		return
	}
	var walk func(n *splitNode)
	walk = func(n *splitNode) {
		if n == nil || n.pane != nil {
			return
		}
		n.ratio = 0.5
		walk(n.a)
		walk(n.b)
	}
	walk(currentSpace.tab().layout)
	m.resizePanes(currentSpace)
	m.persist()
}

func (m *Model) openNewSpaceInput() (tea.Model, tea.Cmd) {
	m.mode = modeNewSpace
	m.status = ""
	m.input.Placeholder = "directory (tab completes)"
	m.input.SetValue("")
	m.clearCompletions()
	m.input.Focus()
	return *m, textinput.Blink
}

func (m *Model) clearCompletions() {
	m.completions = nil
	m.completion = -1
}

// advanceCompletion completes the typed path. The first tab fills the
// common prefix and shows candidates; further tabs cycle through them.
func (m *Model) advanceCompletion(delta int) {
	if len(m.completions) > 0 {
		m.completion = (m.completion + delta + len(m.completions)) % len(m.completions)
		m.input.SetValue(m.completions[m.completion])
		m.input.CursorEnd()
		return
	}
	matches := completeDir(strings.TrimSpace(m.input.Value()))
	if len(matches) == 0 {
		return
	}
	if len(matches) == 1 {
		m.input.SetValue(matches[0] + "/")
		m.input.CursorEnd()
		return
	}
	m.completions = matches
	m.completion = -1
	if shared := commonPrefix(matches); len(shared) > len(strings.TrimSpace(m.input.Value())) {
		m.input.SetValue(shared)
		m.input.CursorEnd()
	}
}

func (m *Model) openRenameInput(target *pane) (tea.Model, tea.Cmd) {
	if target == nil {
		return *m, nil
	}
	m.mode = modeRename
	m.renamePane = target
	m.input.Placeholder = "pane name"
	m.input.SetValue(target.name)
	m.input.CursorEnd()
	m.input.Focus()
	return *m, textinput.Blink
}

func (m *Model) openRenameTabInput(target *tab, index int) (tea.Model, tea.Cmd) {
	if target == nil {
		return *m, nil
	}
	m.mode = modeRename
	m.renameTab = target
	m.input.Placeholder = "tab name"
	name := target.name
	if name == "" {
		name = fmt.Sprintf("%d", index+1)
	}
	m.input.SetValue(name)
	m.input.CursorEnd()
	m.input.Focus()
	return *m, textinput.Blink
}

// openMenuBox positions the context menu for the current item list. The
// coordinates are body coordinates: the full row below the header,
// spanning sidebar and terminal.
func (m *Model) openMenuBox(at rect) {
	m.menuIndex = 0
	width := 0
	for _, item := range m.menuItems() {
		if w := lipgloss.Width(item.label); w > width {
			width = w
		}
	}
	width += 4 // padding + border
	height := len(m.menuItems()) + 2

	bodyW := max(1, m.width)
	bodyH := max(1, m.height-2)
	x := clampInt(at.x, 0, max(0, bodyW-width))
	y := clampInt(at.y, 0, max(0, bodyH-height))
	m.menuAt = rect{x, y, width, height}
}

func (m *Model) openMenu(target *pane, at rect) {
	m.mode = modeMenu
	m.menuPane = target
	m.menuTab = nil
	m.openMenuBox(at)
}

func (m *Model) openTabMenu(target *tab, at rect) {
	m.mode = modeMenu
	m.menuPane = nil
	m.menuTab = target
	m.menuSpace = nil
	m.openMenuBox(at)
}

func (m *Model) openSpaceMenu(target *space, at rect) {
	m.mode = modeMenu
	m.menuPane = nil
	m.menuTab = nil
	m.menuSpace = target
	m.openMenuBox(at)
}

func (m *Model) openRenameSpaceInput(target *space) (tea.Model, tea.Cmd) {
	if target == nil {
		return *m, nil
	}
	m.mode = modeRename
	m.renameSpace = target
	m.input.Placeholder = "workspace name"
	m.input.SetValue(target.name)
	m.input.CursorEnd()
	m.input.Focus()
	return *m, textinput.Blink
}

func (m *Model) closeMenu() {
	m.mode = modeTerminal
	m.menuPane = nil
	m.menuTab = nil
	m.menuSpace = nil
	m.pickItems = nil
	m.pickAction = ""
	m.pickSpace = nil
	m.pickPath = ""
}

// toggleAgent flips one agent kind in settings. The last enabled agent
// cannot be disabled; the default agent follows when it gets disabled.
func (m *Model) toggleAgent(kind string) tea.Cmd {
	if !isAgentKind(kind) {
		return nil
	}
	if !m.disabled[kind] {
		enabled := 0
		for _, spec := range agentSpecs {
			if !m.disabled[spec.kind] {
				enabled++
			}
		}
		if enabled <= 1 {
			return m.flashStatus("cannot disable the last agent")
		}
		m.disabled[kind] = true
		if m.config.DefaultAgent == kind {
			if available := m.availableAgents(); len(available) > 0 {
				m.config.DefaultAgent = available[0]
			}
		}
	} else {
		delete(m.disabled, kind)
	}
	m.persist()
	return nil
}

// openKindPicker shows a menu with the installed agents plus shell. The
// chosen kind feeds the pending action: new workspace, new tab, or split.
func (m *Model) openKindPicker(action string, target *space, path string, at rect) {
	var items []menuItem
	for _, kind := range m.availableAgents() {
		items = append(items, menuItem{kind, "kind:" + kind})
	}
	items = append(items, menuItem{"shell", "kind:shell"})

	m.mode = modeMenu
	m.menuPane = nil
	m.menuTab = nil
	m.menuSpace = nil
	m.pickItems = items
	m.pickAction = action
	m.pickSpace = target
	m.pickPath = path
	m.openMenuBox(at)
	// Preselect the default agent so enter keeps the old one-key flow.
	for index, item := range items {
		if item.action == "kind:"+m.config.DefaultAgent {
			m.menuIndex = index
		}
	}
}

// runKindPick executes the pending picker action with the chosen kind.
func (m Model) runKindPick(kind string) (tea.Model, tea.Cmd) {
	action, target, path := m.pickAction, m.pickSpace, m.pickPath
	m.closeMenu()
	switch action {
	case "space":
		newSpace := m.addSpaceKind(path, kind)
		m.selected = len(m.spaces) - 1
		m.persist()
		return m, m.startPane(newSpace, newSpace.tab().panes[0])
	case "tab":
		if target == nil {
			target = m.currentSpace()
		}
		if target == nil {
			return m, nil
		}
		for index, currentSpace := range m.spaces {
			if currentSpace == target {
				m.selected = index
			}
		}
		newPane := m.addTab(target, kind)
		m.resizePanes(target)
		m.persist()
		return m, m.startPane(target, newPane)
	case "split-left":
		return m.splitCurrentSide(kind, true, true)
	case "split-right":
		return m.splitCurrent(kind, true)
	case "split-up":
		return m.splitCurrentSide(kind, false, true)
	case "split-down":
		return m.splitCurrent(kind, false)
	}
	return m, nil
}

func (m Model) runMenuAction(action string) (tea.Model, tea.Cmd) {
	if kind, ok := strings.CutPrefix(action, "kind:"); ok {
		return m.runKindPick(kind)
	}
	targetPane := m.menuPane
	targetTab := m.menuTab
	targetSpace := m.menuSpace
	at := m.menuAt
	m.closeMenu()

	if actionID, ok := strings.CutPrefix(action, "custom:"); ok {
		m.publishMenuAction(actionID, targetPane, targetTab, targetSpace)
		return m, nil
	}

	if targetSpace != nil {
		for index, currentSpace := range m.spaces {
			if currentSpace == targetSpace {
				m.selected = index
			}
		}
		switch action {
		case "space-rename":
			return m.openRenameSpaceInput(targetSpace)
		case "space-close":
			m.closeCurrentSpace()
		case "space-tab":
			m.openKindPicker("tab", targetSpace, "", at)
		}
		return m, nil
	}

	if targetTab != nil {
		currentSpace := m.currentSpace()
		if currentSpace == nil {
			return m, nil
		}
		tabIndex := 0
		for index, currentTab := range currentSpace.tabs {
			if currentTab == targetTab {
				tabIndex = index
			}
		}
		switch action {
		case "tab-new":
			m.openKindPicker("tab", currentSpace, "", at)
		case "tab-rename":
			return m.openRenameTabInput(targetTab, tabIndex)
		case "tab-close":
			if len(currentSpace.tabs) > 1 {
				m.closeTab(currentSpace, targetTab)
				m.resizePanes(currentSpace)
				m.persist()
			}
		}
		return m, nil
	}

	if targetPane == nil {
		return m, nil
	}
	if currentSpace := m.currentSpace(); currentSpace != nil {
		m.focusPane(currentSpace, targetPane)
	}
	switch action {
	case "rename":
		return m.openRenameInput(targetPane)
	case "pick-left":
		m.openKindPicker("split-left", nil, "", at)
	case "pick-right":
		m.openKindPicker("split-right", nil, "", at)
	case "pick-up":
		m.openKindPicker("split-up", nil, "", at)
	case "pick-down":
		m.openKindPicker("split-down", nil, "", at)
	case "close":
		m.closeCurrentPane()
	}
	return m, nil
}

func (m Model) updateMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	// A pending ctrl+b prefix is cancelled by any mouse press so clicks
	// never get swallowed silently.
	if m.mode == modePrefix && msg.Action == tea.MouseActionPress {
		m.mode = modeTerminal
	}

	if m.mode == modeSettings {
		return m.updateSettingsMouse(msg)
	}

	localX := msg.X - sidebarWidth - 1
	// Body row 0 is the tab bar; panes start one row below.
	tabRow := msg.Y == 1 && localX >= 0
	localY := msg.Y - 2
	area := m.terminalArea()
	inTerminal := localX >= 0 && localY >= 0 && localX < area.w && localY < area.h

	// Tab bar clicks.
	if tabRow && msg.Action == tea.MouseActionPress && m.mode == modeTerminal {
		currentSpace := m.currentSpace()
		if currentSpace == nil {
			return m, nil
		}
		index, isNew := m.tabHit(currentSpace, localX)
		if msg.Button == tea.MouseButtonRight {
			if index >= 0 {
				currentSpace.active = index
				m.resizePanes(currentSpace)
				m.clearFocusedAttention()
				m.openTabMenu(currentSpace.tabs[index], rect{x: msg.X, y: 1})
			}
			return m, nil
		}
		if msg.Button != tea.MouseButtonLeft {
			return m, nil
		}
		if isNew {
			m.openKindPicker("tab", currentSpace, "", rect{x: msg.X, y: 1})
			return m, nil
		}
		if index >= 0 {
			currentSpace.active = index
			m.resizePanes(currentSpace)
			m.clearFocusedAttention()
			m.persist()
		}
		return m, nil
	}

	// Context menu handling has priority while it is open. The menu lives
	// in body coordinates covering the whole row below the header.
	if m.mode == modeMenu {
		bodyX, bodyY := msg.X, msg.Y-1
		items := m.menuItems()
		if msg.Action == tea.MouseActionMotion && m.menuAt.hit(bodyX, bodyY) {
			index := bodyY - m.menuAt.y - 1
			if index >= 0 && index < len(items) {
				m.menuIndex = index
			}
			return m, nil
		}
		if msg.Action != tea.MouseActionPress {
			return m, nil
		}
		if m.menuAt.hit(bodyX, bodyY) && msg.Button == tea.MouseButtonLeft {
			index := bodyY - m.menuAt.y - 1
			if index >= 0 && index < len(items) {
				m.menuIndex = index
				return m.runMenuAction(items[index].action)
			}
			return m, nil
		}
		m.closeMenu()
		return m, nil
	}

	// A workspace drag in the sidebar reorders the list. Descendant rows
	// resolve to their owning workspace, so they remain useful drag targets.
	if m.dragSpace != nil {
		switch msg.Action {
		case tea.MouseActionMotion:
			if msg.X > sidebarWidth {
				return m, nil
			}
			hit := m.sidebarHit(msg.Y - 1)
			descendant := hit.kind == "space" || hit.kind == "tab" || hit.kind == "pane"
			if descendant && hit.space >= 0 && hit.space < len(m.spaces) &&
				m.spaces[hit.space] != m.dragSpace {
				if m.moveSpaceTo(m.dragSpace, hit.space) {
					m.dragMoved = true
				}
			}
			return m, nil
		case tea.MouseActionRelease:
			if m.dragMoved {
				m.persist()
			}
			m.dragSpace = nil
			m.dragMoved = false
			return m, nil
		}
	}

	// Right-click on a pane opens the context menu. Search in reverse render
	// order so overlapping floating panes receive the click from the top down.
	if inTerminal && msg.Button == tea.MouseButtonRight && msg.Action == tea.MouseActionPress {
		if currentSpace := m.currentSpace(); currentSpace != nil {
			panes := m.allPaneLayout(currentSpace.tab())
			for index := len(panes) - 1; index >= 0; index-- {
				pr := panes[index]
				if pr.r.hit(localX, localY) {
					m.focusPane(currentSpace, pr.pane)
					m.openMenu(pr.pane, rect{x: msg.X, y: msg.Y - 1})
					return m, nil
				}
			}
		}
		return m, nil
	}

	// Divider dragging has priority over everything else.
	if m.drag != nil {
		switch msg.Action {
		case tea.MouseActionMotion:
			m.applyDrag(localX, localY)
			return m, nil
		case tea.MouseActionRelease:
			m.applyDrag(localX, localY)
			m.drag = nil
			m.persist()
			return m, nil
		}
	}

	// Selection drag in progress.
	if m.selPane != nil {
		switch msg.Action {
		case tea.MouseActionMotion:
			if m.selPane.term != nil {
				m.selPane.term.ExtendSelection(
					clampInt(localX-m.selRect.x, 0, m.selRect.w-1),
					clampInt(localY-m.selRect.y, 0, m.selRect.h-1))
			}
			return m, nil
		case tea.MouseActionRelease:
			var flash tea.Cmd
			if m.selPane.term != nil {
				m.selPane.term.FinishSelection()
				text := m.selPane.term.SelectionText()
				if strings.TrimSpace(text) == "" || !strings.Contains(text, "\n") && len([]rune(text)) <= 1 {
					m.selPane.term.ClearSelection()
				} else {
					copyToClipboard(text)
					flash = m.flashInfo(fmt.Sprintf("copied %d chars", len([]rune(text))))
				}
			}
			m.selPane = nil
			return m, flash
		}
	}

	if inTerminal {
		currentSpace := m.currentSpace()
		if currentSpace == nil {
			return m, nil
		}
		panes, divs := m.layoutAll(currentSpace.tab())
		floats := m.floatingLayout(currentSpace.tab())
		panes = append(panes, floats...)
		overFloat := false
		for index := len(floats) - 1; index >= 0; index-- {
			pr := floats[index]
			if !pr.r.hit(localX, localY) {
				continue
			}
			overFloat = true
			if msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress &&
				localY == pr.r.y && localX >= pr.r.x+pr.r.w-4 {
				m.removePane(currentSpace, pr.pane)
				return m, nil
			}
			break
		}

		if !overFloat && msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress {
			for _, dv := range divs {
				if dv.r.hit(localX, localY) {
					m.drag = dv.node
					m.dragFull = dv.full
					return m, nil
				}
			}
		}

		for index := len(panes) - 1; index >= 0; index-- {
			pr := panes[index]
			if !pr.r.hit(localX, localY) {
				continue
			}
			inner := pr.r.inner()
			paneX := clampInt(localX-inner.x, 0, inner.w-1)
			paneY := clampInt(localY-inner.y, 0, inner.h-1)
			terminal := pr.pane.term
			captures := terminal != nil && pr.pane.running && terminal.MouseCapturing()

			// Wheel: forward to capturing children, otherwise local scrollback.
			if msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown {
				if terminal == nil {
					return m, nil
				}
				if captures {
					terminal.Write(encodeSGRMouse(msg, paneX, paneY))
					return m, nil
				}
				if terminal.AltScreen() {
					// Full-screen apps without mouse support get arrow keys,
					// like normal terminals translate wheel events.
					arrow := "\x1b[A"
					if msg.Button == tea.MouseButtonWheelDown {
						arrow = "\x1b[B"
					}
					terminal.Write([]byte(strings.Repeat(arrow, 3)))
					return m, nil
				}
				delta := 3
				if msg.Button == tea.MouseButtonWheelDown {
					delta = -3
				}
				terminal.Scroll(delta)
				return m, nil
			}

			if msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress {
				m.focusPane(currentSpace, pr.pane)
				if terminal != nil {
					terminal.ClearSelection()
				}
				// Shift forces local selection even over capturing children,
				// matching normal terminal convention.
				if terminal != nil && (!captures || msg.Shift) && inner.hit(localX, localY) {
					terminal.StartSelection(paneX, paneY)
					m.selPane = pr.pane
					m.selRect = inner
					return m, nil
				}
			}
			if captures {
				terminal.Write(encodeSGRMouse(msg, paneX, paneY))
			}
			return m, nil
		}
		return m, nil
	}

	// Wheel over the sidebar scrolls it when the content overflows. One
	// row per tick: trackpads emit many wheel events per gesture, and the
	// list is short, so bigger steps feel jumpy compared to terminal
	// scrollback.
	if msg.X <= sidebarWidth && (msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown) {
		delta := -1
		if msg.Button == tea.MouseButtonWheelDown {
			delta = 1
		}
		total := len(m.sidebarRows())
		m.sideScroll = clampInt(m.sidebarOffset(total)+delta, 0, max(0, total-(max(3, m.height-2)-2)))
		return m, nil
	}

	// Right-click anywhere in a workspace hierarchy opens the workspace
	// context menu next to the cursor.
	if msg.Button == tea.MouseButtonRight && msg.Action == tea.MouseActionPress {
		hit := m.sidebarHit(msg.Y - 1)
		descendant := hit.kind == "space" || hit.kind == "tab" || hit.kind == "pane"
		if descendant && hit.space >= 0 && hit.space < len(m.spaces) {
			m.selected = hit.space
			m.clearFocusedAttention()
			m.openSpaceMenu(m.spaces[hit.space], rect{x: msg.X, y: msg.Y - 1})
		}
		return m, nil
	}

	if msg.Button != tea.MouseButtonLeft || msg.Action != tea.MouseActionPress {
		return m, nil
	}
	// The sidebar starts at body row 0 (screen row 1).
	hit := m.sidebarHit(msg.Y - 1)
	switch hit.kind {
	case "space":
		if hit.space >= 0 && hit.space < len(m.spaces) {
			m.selected = hit.space
			m.clearFocusedAttention()
			// Arm a reorder drag; a plain click releases without motion
			// and leaves the order untouched.
			m.dragSpace = m.spaces[hit.space]
			m.dragMoved = false
		}
	case "tab":
		if hit.space >= 0 && hit.space < len(m.spaces) {
			target := m.spaces[hit.space]
			if hit.tab >= 0 && hit.tab < len(target.tabs) {
				m.selected = hit.space
				target.active = hit.tab
				m.resizePanes(target)
				m.clearFocusedAttention()
				m.persist()
			}
		}
	case "pane":
		if hit.space >= 0 && hit.space < len(m.spaces) {
			target := m.spaces[hit.space]
			if hit.tab >= 0 && hit.tab < len(target.tabs) &&
				hit.pane >= 0 && hit.pane < len(target.tabs[hit.tab].panes) {
				m.selected = hit.space
				m.focusPane(target, target.tabs[hit.tab].panes[hit.pane])
				// focusPane moves the active tab and that tab's selected
				// pane, both of which belong to the saved state.
				m.persist()
			}
		}
	case "new":
		return m.openNewSpaceInput()
	case "settings":
		m.openSettings()
	}
	return m, nil
}

// moveSpaceTo moves target to the given index in the workspace list and
// follows it with the selection. Returns true when the order changed.
func (m *Model) moveSpaceTo(target *space, index int) bool {
	from := -1
	for i, currentSpace := range m.spaces {
		if currentSpace == target {
			from = i
			break
		}
	}
	if from < 0 || index < 0 || index >= len(m.spaces) || index == from {
		return false
	}
	m.spaces = append(m.spaces[:from], m.spaces[from+1:]...)
	m.spaces = append(m.spaces, nil)
	copy(m.spaces[index+1:], m.spaces[index:])
	m.spaces[index] = target
	m.selected = index
	return true
}

func (m *Model) applyDrag(localX, localY int) {
	if m.drag == nil {
		return
	}
	full := m.dragFull
	if m.drag.vertical {
		if full.w > 0 {
			m.drag.ratio = clampFloat(float64(localX-full.x)/float64(full.w), 0.1, 0.9)
		}
	} else {
		if full.h > 0 {
			m.drag.ratio = clampFloat(float64(localY-full.y)/float64(full.h), 0.1, 0.9)
		}
	}
	if currentSpace := m.currentSpace(); currentSpace != nil {
		m.resizePanes(currentSpace)
	}
}

func (m *Model) focusPane(owner *space, target *pane) {
	for tabIndex, currentTab := range owner.tabs {
		for index, current := range currentTab.panes {
			if current == target {
				owner.active = tabIndex
				if target.floating != nil && index != len(currentTab.panes)-1 {
					currentTab.panes = append(currentTab.panes[:index], currentTab.panes[index+1:]...)
					currentTab.panes = append(currentTab.panes, target)
					currentTab.selected = len(currentTab.panes) - 1
				} else {
					currentTab.selected = index
				}
				m.clearFocusedAttention()
				return
			}
		}
	}
}

// sidebarRow is one clickable line of the sidebar. kind is "", "space",
// "tab", "pane", or "new". The render and mouse hit test share this layout
// so they can never drift apart.
type sidebarRow struct {
	label string
	kind  string
	space int
	tab   int
	pane  int
}

// paneBusy reports whether a pane running an agent (agent panes, or shell
// panes with an agent in the foreground) is currently working. Built-ins
// are detected by the braille spinner their TUIs render during a turn;
// custom harnesses can declare a busy substring instead.
func (m Model) paneBusy(currentPane *pane) bool {
	return m.paneBusyWithTitle(currentPane, m.paneAgentTitleState(currentPane))
}

// paneBusyWithTitle is paneBusy with the title state already resolved, so the
// sidebar can share one lookup between the busy check and the attention dot.
// Any non-empty title state is authoritative and outranks the screen scrape.
func (m Model) paneBusyWithTitle(currentPane *pane, titleState string) bool {
	kind := m.paneAgentKind(currentPane)
	if kind == "" || !currentPane.running || currentPane.term == nil {
		return false
	}
	if titleState != "" {
		return false
	}
	if spec := agentByKind(kind); spec != nil && spec.busyMatch != "" {
		return currentPane.term.HasScreenText(spec.busyMatch)
	}
	return currentPane.term.HasSpinner()
}

// sidebarPaneIcon uses a shared dot shape for shells and agents while
// color and animation expose pane state. Agents animate only while active;
// an idle agent process stays a dot.
func (m Model) sidebarPaneIcon(currentPane *pane) string {
	titleState := m.paneAgentTitleState(currentPane)
	attention := m.paneAttention[currentPane.id]
	if titleState == "attention" {
		focused := m.currentPane()
		attention = attention || m.appBlurred || focused == nil || focused != currentPane
	}
	return paneTypeStateIcon(
		currentPane,
		m.paneAgentKind(currentPane) != "",
		m.paneBusyWithTitle(currentPane, titleState),
		attention,
		m.spinFrame,
	)
}

func paneIconCell(style lipgloss.Style, glyph string) string {
	if lipgloss.Width(glyph) < 2 {
		glyph += " "
	}
	return style.Render(glyph)
}

func paneTypeStateIcon(currentPane *pane, agent, busy, attention bool, frame int) string {
	runningGlyph, stoppedGlyph := "●", "○"
	switch {
	case currentPane.failure != "":
		return paneIconCell(styleDotOff, stoppedGlyph)
	case agent && busy:
		return paneIconCell(styleDotBusy, spinnerFrames[frame%len(spinnerFrames)])
	case currentPane.running && attention:
		return paneIconCell(styleDotBusy, runningGlyph)
	case currentPane.running:
		return paneIconCell(styleDotOn, runningGlyph)
	case currentPane.term == nil:
		return paneIconCell(stylePaneDim, stoppedGlyph)
	default:
		return paneIconCell(styleDotOff, stoppedGlyph)
	}
}

func sidebarPaneState(currentPane *pane) string {
	switch {
	case currentPane.failure != "":
		return styleDotOff.Render("failed")
	case !currentPane.running && currentPane.term != nil:
		return styleDotOff.Render("exited")
	case currentPane.term == nil:
		return stylePaneDim.Render("starting")
	default:
		return ""
	}
}

func (m Model) sidebarRows() []sidebarRow {
	rows := []sidebarRow{
		{},
		{label: " " + styleSection.Render("WORKSPACES")},
		{},
	}

	// Reserve one shared tab-label column whenever any workspace has tabs.
	// This keeps every pane icon aligned, including panes in workspaces that
	// only have one tab. Named tabs widen the shared column for all rows.
	tabNameWidth := 0
	for _, currentSpace := range m.spaces {
		if len(currentSpace.tabs) <= 1 {
			continue
		}
		for tabIndex, currentTab := range currentSpace.tabs {
			name := truncate(tabDisplayName(currentTab, tabIndex), 6)
			tabNameWidth = max(tabNameWidth, lipgloss.Width(name))
		}
	}

	for spaceIndex, currentSpace := range m.spaces {
		if spaceIndex > 0 {
			rows = append(rows, sidebarRow{
				label: " " + styleDivider.Render(strings.Repeat("─", sidebarWidth-2)),
				kind:  "divider", space: spaceIndex, tab: -1, pane: -1,
			})
		}
		rail := " "
		if spaceIndex == m.selected {
			rail = styleSpaceSel.Render("▍")
		}
		label := rail + styleSpaceDim.Render(truncate(currentSpace.name, sidebarWidth-3))
		rows = append(rows, sidebarRow{
			label: label,
			kind:  "space", space: spaceIndex, tab: -1, pane: -1,
		})
		if branch := m.gitBranch(currentSpace.cwd); branch.value != "" {
			suffixWidth := 0
			suffix := ""
			if branch.ahead > 0 {
				text := fmt.Sprintf(" ↑%d", branch.ahead)
				suffix += styleDotOn.Render(text)
				suffixWidth += lipgloss.Width(text)
			}
			if branch.behind > 0 {
				text := fmt.Sprintf(" ↓%d", branch.behind)
				suffix += lipgloss.NewStyle().Foreground(colorAlt).Render(text)
				suffixWidth += lipgloss.Width(text)
			}
			branchStyle := stylePaneDim
			if spaceIndex == m.selected {
				branchStyle = styleSpaceSel
			}
			rows = append(rows, sidebarRow{
				label: rail + branchStyle.Render(truncate(branch.value, sidebarWidth-3-suffixWidth)) + suffix,
				kind:  "space", space: spaceIndex, tab: -1, pane: -1,
			})
		}

		showTabs := len(currentSpace.tabs) > 1
		for tabIndex, currentTab := range currentSpace.tabs {
			paneIndent := rail + "  "
			paneNameWidth := sidebarWidth - 6
			if tabNameWidth > 0 {
				paneIndent = rail + strings.Repeat(" ", tabNameWidth+3)
				paneNameWidth = sidebarWidth - tabNameWidth - 7
			}

			shownPanes := 0
			for paneIndex, currentPane := range currentTab.panes {
				if currentPane.floating != nil {
					continue
				}
				rowIndent := paneIndent
				if showTabs && shownPanes == 0 {
					tabStyle := stylePaneDim
					tabMarker := "  "
					if spaceIndex == m.selected && tabIndex == currentSpace.active {
						tabStyle = styleSpaceSel
						tabMarker = styleSpaceSel.Render("▸") + " "
					}
					tabName := truncate(tabDisplayName(currentTab, tabIndex), 6)
					tabPadding := strings.Repeat(" ", tabNameWidth-lipgloss.Width(tabName))
					rowIndent = rail + tabMarker + tabStyle.Render(tabName) + tabPadding + " "
				}
				selected := spaceIndex == m.selected &&
					tabIndex == currentSpace.active &&
					paneIndex == currentTab.selected
				state := sidebarPaneState(currentPane)
				stateWidth := lipgloss.Width(state)
				if state != "" {
					state = " " + state
					stateWidth++
				}
				name := truncate(m.paneDisplayName(currentPane), paneNameWidth-stateWidth)
				nameStyle := stylePaneDim
				if currentPane.running {
					nameStyle = stylePaneRun
				}
				if selected {
					nameStyle = stylePaneSel
				}
				nameLabel := nameStyle.Render(name)
				rows = append(rows, sidebarRow{
					label: rowIndent + m.sidebarPaneIcon(currentPane) + nameLabel + state,
					kind:  "pane", space: spaceIndex, tab: tabIndex, pane: paneIndex,
				})
				shownPanes++
			}
		}
	}

	rows = append(rows,
		sidebarRow{},
		sidebarRow{label: " " + styleNewButton.Render("+  new workspace"), kind: "new", space: -1, tab: -1, pane: -1},
	)
	return rows
}

// sidebarHit maps a body row (header already subtracted) to a sidebar
// entry, accounting for the sidebar scroll offset. The bottom row is the
// pinned settings entry.
func (m Model) sidebarHit(y int) sidebarRow {
	height := max(3, m.height-2)
	if y == height-2 {
		return sidebarRow{kind: "settings", space: -1, tab: -1, pane: -1}
	}
	if y == height-1 {
		return sidebarRow{}
	}
	rows := m.sidebarRows()
	y += m.sidebarOffset(len(rows))
	if y < 0 || y >= len(rows) {
		return sidebarRow{}
	}
	return rows[y]
}

// sidebarOffset clamps the scroll offset to the overflow of the row list.
// The two bottom sidebar rows are reserved for the pinned settings entry
// and its trailing blank line (mirroring the blank line at the top).
func (m Model) sidebarOffset(total int) int {
	height := max(3, m.height-2) - 2
	return clampInt(m.sideScroll, 0, max(0, total-height))
}

func (m Model) View() string {
	if m.width == 0 || m.height == 0 {
		return "starting hrdx..."
	}

	header := m.renderHeader()
	sidebar := m.renderSidebar()
	right := m.renderTabBar() + "\n" + m.renderTerminal()
	body := lipgloss.JoinHorizontal(lipgloss.Top, sidebar, right)
	if m.mode == modeMenu {
		rows := strings.Split(body, "\n")
		m.overlayMenu(rows)
		body = strings.Join(rows, "\n")
	}
	if m.mode == modeSettings {
		rows := strings.Split(body, "\n")
		m.overlaySettings(rows)
		body = strings.Join(rows, "\n")
	}
	if m.mode == modeFind {
		rows := strings.Split(body, "\n")
		m.overlayFind(rows)
		body = strings.Join(rows, "\n")
	}
	footer := m.renderFooter()
	m.publishCursor()
	return header + "\n" + body + "\n" + footer
}

// publishCursor reports where the hardware cursor belongs for the current
// frame: the focused pane's cursor cell in terminal mode, or the footer
// input point while typing there. Terminals render IME and dead-key
// composition previews at the hardware cursor, so parking it at the real
// input point makes those previews appear in the right place.
func (m Model) publishCursor() {
	if m.cursorSink == nil {
		return
	}
	switch m.mode {
	case modeTerminal:
		current := m.currentPane()
		currentSpace := m.currentSpace()
		if current == nil || current.term == nil || currentSpace == nil {
			break
		}
		x, y, visible := current.term.CursorCell()
		if !visible {
			break
		}
		for _, pr := range m.allPaneLayout(currentSpace.tab()) {
			if pr.pane != current {
				continue
			}
			inner := pr.r.inner()
			if x >= inner.w || y >= inner.h {
				break
			}
			// Screen: sidebar plus border to the left, header and tab bar above.
			m.cursorSink.Set(sidebarWidth+1+inner.x+x, 2+inner.y+y, true)
			return
		}
	case modeNewSpace, modeRename:
		badge := " NEW WORKSPACE "
		if m.mode == modeRename {
			badge = " RENAME "
		}
		// Footer layout: badge, one space, then the input's value.
		m.cursorSink.Set(len(badge)+1+m.input.Position(), m.height-1, true)
		return
	case modeFind:
		box := m.findBox()
		// Query row: border, " > ", then the input; body starts at row 1.
		m.cursorSink.Set(box.x+4+m.input.Position(), 1+box.y+1, true)
		return
	}
	m.cursorSink.Set(0, 0, false)
}

// tabCell describes one clickable region of the tab bar in local x cells.
type tabCell struct {
	from, to int // inclusive-exclusive
	index    int // tab index, -1 for the + button
}

func (m Model) tabCells(target *space) []tabCell {
	cells := make([]tabCell, 0, len(target.tabs)+1)
	x := 0
	for index, currentTab := range target.tabs {
		label := m.tabLabel(currentTab, index)
		width := lipgloss.Width(label)
		cells = append(cells, tabCell{from: x, to: x + width, index: index})
		x += width
	}
	cells = append(cells, tabCell{from: x, to: x + 3, index: -1})
	return cells
}

func tabDisplayName(target *tab, index int) string {
	if target.name != "" {
		return target.name
	}
	return fmt.Sprintf("%d", index+1)
}

func (m Model) tabLabel(target *tab, index int) string {
	return " " + truncate(tabDisplayName(target, index), 16) + " "
}

// tabHit maps a local x on the tab bar to a tab index or the + button.
func (m *Model) tabHit(target *space, x int) (index int, isNew bool) {
	for _, cell := range m.tabCells(target) {
		if x >= cell.from && x < cell.to {
			if cell.index == -1 {
				return -1, true
			}
			return cell.index, false
		}
	}
	return -1, false
}

func (m Model) renderTabBar() string {
	currentSpace := m.currentSpace()
	width := max(1, m.width-sidebarWidth-1)
	if currentSpace == nil {
		return styleTabBar.Render(strings.Repeat(" ", width))
	}
	var out strings.Builder
	used := 0
	for index, currentTab := range currentSpace.tabs {
		label := m.tabLabel(currentTab, index)
		if index == currentSpace.active {
			out.WriteString(styleTabActive.Render(label))
		} else {
			out.WriteString(styleTabIdle.Render(label))
		}
		used += lipgloss.Width(label)
	}
	out.WriteString(styleTabIdle.Render(" + "))
	used += 3
	if used < width {
		out.WriteString(styleTabBar.Render(strings.Repeat(" ", width-used)))
	}
	return out.String()
}

func (m Model) renderHeader() string {
	logo := styleLogo.Render(" hrdx ")
	title := ""
	if currentSpace := m.currentSpace(); currentSpace != nil {
		title = styleBarText.Render(" " + currentSpace.name)
		if current := m.currentPane(); current != nil {
			title += styleBarMuted.Render("  " + m.paneDisplayName(current))
		}
	}
	right := ""
	if m.updateInfo.Available {
		right = styleBadgeInput.Render(" update "+m.updateInfo.Latest+" ") + styleBarMuted.Render(" ")
	}
	if currentSpace := m.currentSpace(); currentSpace != nil {
		right += styleBarMuted.Render(shortenPath(currentSpace.cwd) + " ")
	}
	gap := m.width - lipgloss.Width(logo) - lipgloss.Width(title) - lipgloss.Width(right)
	if gap < 0 {
		gap = 0
	}
	return logo + title + styleBar.Render(strings.Repeat(" ", gap)) + right
}

func (m Model) renderSidebar() string {
	source := m.sidebarRows()
	offset := m.sidebarOffset(len(source))
	rows := make([]string, 0, len(source))
	for _, current := range source[offset:] {
		rows = append(rows, current.label)
	}

	height := max(3, m.height-2)
	// The two bottom rows are the pinned settings entry and trailing space
	// that keeps it clear of the footer.
	list := height - 2
	for len(rows) < list {
		rows = append(rows, "")
	}
	rows = rows[:list]

	// Overflow markers so hidden rows are discoverable.
	if offset > 0 {
		rows[0] = stylePaneDim.Render(" ↑  more")
	}
	if offset < len(source)-list {
		rows[list-1] = stylePaneDim.Render(" ↓  more")
	}
	// Extra trailing space after the gear: unlike the other sidebar icons
	// (●○↑↓), U+2699 is uncommon enough that some fonts (Windows
	// Terminal's default among them) substitute a wider fallback glyph
	// for it, which then overlaps a single following space.
	rows = append(rows, " "+stylePaneDim.Render("⚙  settings"), "")

	return lipgloss.NewStyle().
		Width(sidebarWidth).
		Height(height).
		Border(lipgloss.ThickBorder(), false, true, false, false).
		BorderForeground(colorFaint).
		Render(strings.Join(rows, "\n"))
}

// renderTerminal composes the active tab's pane screens into one text block
// matching terminalArea.
func (m Model) renderTerminal() string {
	area := m.terminalArea()
	currentSpace := m.currentSpace()
	if currentSpace == nil {
		return lipgloss.NewStyle().Padding(1, 2).Width(area.w).Height(area.h).
			Render(styleMuted.Render("no panes. " + m.prefixTrigger + " " + m.primaryKey("workspace") + " opens a new workspace."))
	}

	panes, _ := m.layoutAll(currentSpace.tab())
	focused := m.currentPane()

	// segments[row] holds (x, text) fragments that tile the row.
	type segment struct {
		x    int
		text string
	}
	segments := make([][]segment, area.h)

	for _, pr := range panes {
		isFocused := pr.pane == focused
		lines := m.paneLines(pr, isFocused, isFocused && m.mode == modeTerminal)
		for i := 0; i < pr.r.h && pr.r.y+i < area.h; i++ {
			line := ""
			if i < len(lines) {
				line = lines[i]
			}
			segments[pr.r.y+i] = append(segments[pr.r.y+i], segment{pr.r.x, line})
		}
	}

	rows := make([]string, area.h)
	for y := 0; y < area.h; y++ {
		row := segments[y]
		for i := 1; i < len(row); i++ {
			for j := i; j > 0 && row[j-1].x > row[j].x; j-- {
				row[j-1], row[j] = row[j], row[j-1]
			}
		}
		var out strings.Builder
		for _, seg := range row {
			out.WriteString(seg.text)
		}
		rows[y] = out.String()
		if lipgloss.Width(rows[y]) < area.w {
			rows[y] += strings.Repeat(" ", area.w-lipgloss.Width(rows[y]))
		}
	}

	for _, pr := range m.floatingLayout(currentSpace.tab()) {
		isFocused := pr.pane == focused
		lines := m.paneLines(pr, isFocused, isFocused && m.mode == modeTerminal)
		for index, line := range lines {
			y := pr.r.y + index
			if y >= 0 && y < len(rows) {
				rows[y] = overlayAt(rows[y], line, pr.r.x, pr.r.w)
			}
		}
	}
	return strings.Join(rows, "\n")
}

// overlayMenu draws the context menu box over the composed rows.
func (m Model) overlayMenu(rows []string) {
	box := m.menuAt
	border := lipgloss.NewStyle().Foreground(colorAccent)
	normal := lipgloss.NewStyle().Background(colorBarBg).Foreground(colorBarFg)
	active := lipgloss.NewStyle().Background(colorAccent).Foreground(colorInk).Bold(true)

	innerW := box.w - 2
	lines := make([]string, 0, box.h)
	lines = append(lines, border.Render("╭"+strings.Repeat("─", innerW)+"╮"))
	for index, item := range m.menuItems() {
		label := " " + item.label + strings.Repeat(" ", max(0, innerW-lipgloss.Width(item.label)-1))
		style := normal
		if index == m.menuIndex {
			style = active
		}
		lines = append(lines, border.Render("│")+style.Render(label)+border.Render("│"))
	}
	lines = append(lines, border.Render("╰"+strings.Repeat("─", innerW)+"╯"))

	for i, line := range lines {
		y := box.y + i
		if y < 0 || y >= len(rows) {
			continue
		}
		rows[y] = overlayAt(rows[y], line, box.x, box.w)
	}
}

// overlayAt replaces the cells [x, x+width) of an ANSI row with overlay.
func overlayAt(row, overlay string, x, width int) string {
	left := ansiCut(row, 0, x)
	right := ansiCut(row, x+width, -1)
	return left + "\x1b[0m" + overlay + "\x1b[0m" + right
}

// ansiCut returns the cells [from, to) of an ANSI string preserving the
// escape state. to == -1 means to the end. Wide runes count as their
// display width; one straddling a boundary is replaced by spaces for
// the cells inside the range, so the result's width is always exact.
func ansiCut(value string, from, to int) string {
	var out strings.Builder
	col := 0
	inEscape := false
	var escape strings.Builder
	for _, r := range value {
		if inEscape {
			escape.WriteRune(r)
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEscape = false
				out.WriteString(escape.String())
				escape.Reset()
			}
			continue
		}
		if r == 0x1b {
			inEscape = true
			escape.WriteRune(r)
			continue
		}
		width := runewidth.RuneWidth(r)
		start := col
		col += width
		if width == 0 {
			// Combining marks attach to the previous rune; keep them
			// when that rune was inside the range.
			if start > from && (to < 0 || start <= to) {
				out.WriteRune(r)
			}
			continue
		}
		if start >= from && (to < 0 || col <= to) {
			out.WriteRune(r)
			continue
		}
		// Partially visible wide rune: pad its in-range cells.
		overlapFrom := max(start, from)
		overlapTo := col
		if to >= 0 {
			overlapTo = min(col, to)
		}
		for i := overlapFrom; i < overlapTo; i++ {
			out.WriteByte(' ')
		}
	}
	return out.String()
}

// paneLines returns exactly r.h display lines for a pane, each r.w cells
// wide, framed by the pane's own border. The focused pane gets an accent
// border.
func (m Model) paneLines(pr paneRect, focused, showCursor bool) []string {
	target := pr.pane
	inner := pr.r.inner()

	var content []string
	switch {
	case target.failure != "":
		content = placeholderLines(inner, styleError.Render(truncate("failed: "+target.failure, inner.w)))
	case target.term == nil:
		content = placeholderLines(inner, styleMuted.Render(truncate("starting "+target.name+"...", inner.w)))
	default:
		content = target.term.RenderLines(showCursor)
	}
	for len(content) < inner.h {
		content = append(content, strings.Repeat(" ", inner.w))
	}
	content = content[:inner.h]

	// The pane's PTY size can lag behind the layout for a frame (splits,
	// tab switches, drags), and wide runes (emoji, CJK) occupy two host
	// cells while the emulator counts one. Clip and pad every line to
	// exactly inner.w cells so the row compositor never drifts.
	for i, line := range content {
		visible := lipgloss.Width(line)
		if visible == inner.w {
			continue
		}
		if visible > inner.w {
			line = ansiCut(line, 0, inner.w) + "\x1b[0m"
			visible = lipgloss.Width(line)
		}
		if visible < inner.w {
			line += strings.Repeat(" ", inner.w-visible)
		}
		content[i] = line
	}

	borderStyle := lipgloss.NewStyle().Foreground(colorFaint)
	if focused {
		borderStyle = lipgloss.NewStyle().Foreground(colorAccent)
	}

	horizontal := strings.Repeat("─", max(0, pr.r.w-2))
	titleLimit := pr.r.w - 6
	closeLabel := ""
	if target.floating != nil {
		titleLimit -= 4
		closeLabel = " x "
	}
	title := " " + truncate(m.paneDisplayName(target), max(0, titleLimit)) + " "
	top := "╭" + title + strings.Repeat("─", max(0, pr.r.w-2-lipgloss.Width(title)-lipgloss.Width(closeLabel))) + closeLabel + "╮"
	bottom := "╰" + horizontal + "╯"
	side := borderStyle.Render("│")

	lines := make([]string, 0, pr.r.h)
	lines = append(lines, borderStyle.Render(top))
	for _, row := range content {
		lines = append(lines, side+row+side)
	}
	lines = append(lines, borderStyle.Render(bottom))
	return lines
}

func placeholderLines(r rect, message string) []string {
	lines := make([]string, r.h)
	blank := strings.Repeat(" ", r.w)
	for i := range lines {
		lines[i] = blank
	}
	if r.h > 0 {
		pad := max(0, r.w-lipgloss.Width(message))
		lines[0] = " " + message + strings.Repeat(" ", max(0, pad-1))
	}
	return lines
}

func (m Model) renderFooter() string {
	right := styleBarMuted.Render(" " + m.agentSummary() + " ")
	var badge, body string
	switch m.mode {
	case modeNewSpace:
		badge = styleBadgeInput.Render(" NEW WORKSPACE ")
		body = styleBarText.Render(" " + m.input.View())
		if len(m.completions) > 0 {
			var hints []string
			for index, candidate := range m.completions {
				name := filepath.Base(candidate)
				if index == m.completion {
					hints = append(hints, styleBarText.Render("["+name+"]"))
				} else {
					hints = append(hints, styleBarMuted.Render(name))
				}
			}
			body += styleBarMuted.Render("  ") + strings.Join(hints, styleBarMuted.Render("  "))
		}
	case modeRename:
		badge = styleBadgeInput.Render(" RENAME ")
		body = styleBarText.Render(" " + m.input.View())
	case modeMenu:
		badge = styleBadgePrefix.Render(" MENU ")
		body = styleBarMuted.Render(" click or arrows + enter, esc closes")
	case modeSettings:
		badge = styleBadgeInput.Render(" SETTINGS ")
		body = styleBarMuted.Render(" enter toggles, tab switches section, esc closes")
	case modeFind:
		badge = styleBadgeInput.Render(" FIND ")
		body = styleBarMuted.Render(" type to filter, arrows select, enter jumps, esc closes")
	case modePrefix:
		badge = styleBadgePrefix.Render(" " + strings.ToUpper(m.prefixTrigger) + " ")
		body = m.prefixHints(m.width - lipgloss.Width(badge) - lipgloss.Width(right))
	default:
		badge = styleBadgeTerm.Render(" TERM ")
		body = styleBarMuted.Render(" " + m.prefixTrigger + " commands")
		if m.updateInfo.Available {
			body += styleBarText.Render("  update "+m.updateInfo.Current+" -> "+m.updateInfo.Latest) +
				styleBarMuted.Render("  run 'hrdx update'")
		}
		if current := m.currentPane(); current != nil && current.term != nil {
			if offset := current.term.ScrollOffset(); offset > 0 {
				badge = styleBadgePrefix.Render(" SCROLL ")
				body = styleBarText.Render(fmt.Sprintf(" %d lines back ", offset)) +
					styleBarMuted.Render(" wheel down or type to return")
			}
		}
	}
	if m.status != "" {
		style := styleBarError
		if m.statusIsInfo {
			style = styleBarInfo
		}
		body += style.Render("  " + m.status)
	}

	// The footer must occupy exactly one terminal row. Prefer the active
	// prompt or status over the summary on narrow windows, then clip any
	// remaining overflow so Bubble Tea never wraps the start of the footer.
	if m.width <= 0 {
		return ""
	}
	badgeWidth := lipgloss.Width(badge)
	if badgeWidth >= m.width {
		return ansiCut(badge, 0, m.width) + "\x1b[0m"
	}
	if badgeWidth+lipgloss.Width(body)+lipgloss.Width(right) > m.width {
		right = ""
	}
	available := m.width - badgeWidth - lipgloss.Width(right)
	if lipgloss.Width(body) > available {
		body = ansiCut(body, 0, available) + "\x1b[0m"
	}
	gap := m.width - badgeWidth - lipgloss.Width(body) - lipgloss.Width(right)
	return badge + body + styleBar.Render(strings.Repeat(" ", max(0, gap))) + right
}

func (m Model) agentSummary() string {
	agents, busy := 0, 0
	for _, currentSpace := range m.spaces {
		for _, currentTab := range currentSpace.tabs {
			for _, currentPane := range currentTab.panes {
				if m.paneAgentKind(currentPane) == "" {
					continue
				}
				agents++
				if m.paneBusy(currentPane) {
					busy++
				}
			}
		}
	}
	label := "agents"
	if agents == 1 {
		label = "agent"
	}
	return fmt.Sprintf("%d %s | %d busy", agents, label, busy)
}

// primaryKey returns the key shown in hints for an action: the user's
// override, or the first default key.
func (m Model) primaryKey(action string) string {
	if key, ok := m.keyOverrides[action]; ok {
		return key
	}
	if defaults := defaultPrefixKeys[action]; len(defaults) > 0 {
		return defaults[0]
	}
	return ""
}

// prefixHintEntries builds the ctrl+b hint row from the active keymap.
// Keys of one entry are space separated; a slash would read as its own
// shortcut (see issue #2).
func (m Model) prefixHintEntries() [][2]string {
	keys := func(actions ...string) string {
		var parts []string
		for _, action := range actions {
			if key := m.primaryKey(action); key != "" {
				parts = append(parts, key)
			}
		}
		return strings.Join(parts, " ")
	}
	entries := [][2]string{
		{keys("picker-right", "picker-down"), "split"},
		{keys("agent-right", "agent-down"), "agent"},
		{keys("shell-right", "shell-down"), "shell"},
		{keys("workspace"), "workspace"},
		{keys("tab-new"), "tab"},
		{keys("tab-next", "tab-prev"), "tabs"},
		{keys("space-next", "space-prev"), "workspaces"},
		{keys("pane-next"), "panes"},
		{keys("find"), "find"},
		{keys("rename"), "rename"},
		{keys("equalize"), "equal"},
		{keys("scroll-up", "scroll-down"), "scroll"},
		{keys("close-pane", "close-space"), "close"},
		{keys("settings"), "settings"},
		{keys("quit"), "quit"},
	}
	out := entries[:0]
	for _, entry := range entries {
		if entry[0] != "" {
			out = append(out, entry)
		}
	}
	return out
}

// prefixHints renders the ctrl+b key hints, fitting as many as the given
// width allows, starting at the hint scroll offset. Ellipses on either
// side mark clipped hints; left/right arrows move the window.
func (m Model) prefixHints(width int) string {
	hints := m.prefixHintEntries()
	start := clampInt(m.hintScroll, 0, len(hints)-1)
	var out strings.Builder
	used := 0
	if start > 0 {
		out.WriteString(styleBarMuted.Render(" ‹"))
		used += 2
	}
	for index := start; index < len(hints); index++ {
		hint := hints[index]
		cellWidth := 1 + len(hint[0]) + 1 + len(hint[1]) + 1
		reserve := 0
		if index < len(hints)-1 {
			reserve = 2 // room for the arrow marker when more hints follow
		}
		if used+cellWidth+reserve > width {
			out.WriteString(styleBarMuted.Render(" ›"))
			break
		}
		out.WriteString(styleBarMuted.Render(" " + hint[0]))
		out.WriteString(styleBarText.Render(" " + hint[1] + " "))
		used += cellWidth
	}
	return out.String()
}

func shortenPath(path string) string {
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(path, home) {
		return "~" + strings.TrimPrefix(path, home)
	}
	return path
}

func (m *Model) selectSpace(delta int) {
	if len(m.spaces) > 0 {
		count := len(m.spaces)
		m.selected = (m.selected + delta + count) % count
		m.clearFocusedAttention()
	}
}

func (m *Model) selectTab(delta int) {
	currentSpace := m.currentSpace()
	if currentSpace == nil || len(currentSpace.tabs) == 0 {
		return
	}
	count := len(currentSpace.tabs)
	currentSpace.active = (currentSpace.active + delta + count) % count
	m.resizePanes(currentSpace)
	m.clearFocusedAttention()
}

// cyclePane moves focus through split panes in layout order, followed by
// floating panes in their current stacking order.
func (m *Model) cyclePane(delta int) {
	currentSpace := m.currentSpace()
	if currentSpace == nil {
		return
	}
	currentTab := currentSpace.tab()
	if len(currentTab.panes) < 2 {
		return
	}

	ordered := make([]*pane, 0, len(currentTab.panes))
	if currentTab.layout != nil {
		currentTab.layout.walk(func(target *pane) { ordered = append(ordered, target) })
	}
	for _, target := range currentTab.panes {
		if target.floating != nil {
			ordered = append(ordered, target)
		}
	}
	current := m.currentPane()
	position := -1
	for index, target := range ordered {
		if target == current {
			position = index
			break
		}
	}
	if position < 0 {
		return
	}
	next := ordered[(position+delta+len(ordered))%len(ordered)]
	m.focusPane(currentSpace, next)
}

func (m *Model) closeCurrentPane() {
	currentSpace := m.currentSpace()
	if currentSpace == nil {
		return
	}
	currentTab := currentSpace.tab()
	if len(currentTab.panes) == 0 {
		return
	}
	m.removePane(currentSpace, currentTab.panes[currentTab.selected])
}

// removePane closes a pane's process and removes it from its tab's layout,
// wherever it lives. The last pane of a tab takes the tab with it, unless
// it is the workspace's only tab.
func (m *Model) removePane(owner *space, target *pane) {
	if owner == nil || target == nil {
		return
	}
	for _, currentTab := range owner.tabs {
		index := -1
		for i, current := range currentTab.panes {
			if current == target {
				index = i
			}
		}
		if index < 0 {
			continue
		}
		delete(m.wasBusy, target.id)
		delete(m.soundSeq, target.id)
		delete(m.paneAttention, target.id)
		if target.term != nil {
			target.term.Close()
		}
		removeAt(&currentTab.layout, target)
		currentTab.panes = append(currentTab.panes[:index], currentTab.panes[index+1:]...)
		if currentTab.selected >= len(currentTab.panes) {
			currentTab.selected = max(0, len(currentTab.panes)-1)
		}
		// Dropping the last pane closes the tab too, unless it is the only one.
		if len(currentTab.panes) == 0 && len(owner.tabs) > 1 {
			m.closeTab(owner, currentTab)
		}
		m.resizePanes(owner)
		m.clearFocusedAttention()
		m.persist()
		return
	}
}

func (m *Model) closeTab(owner *space, target *tab) {
	for _, currentPane := range target.panes {
		delete(m.wasBusy, currentPane.id)
		delete(m.soundSeq, currentPane.id)
		delete(m.paneAttention, currentPane.id)
		if currentPane.term != nil {
			currentPane.term.Close()
		}
	}
	for index, currentTab := range owner.tabs {
		if currentTab == target {
			owner.tabs = append(owner.tabs[:index], owner.tabs[index+1:]...)
			break
		}
	}
	owner.active = clampInt(owner.active, 0, max(0, len(owner.tabs)-1))
	m.clearFocusedAttention()
}

func (m *Model) closeCurrentSpace() {
	if len(m.spaces) == 0 {
		return
	}
	for _, currentTab := range m.spaces[m.selected].tabs {
		for _, currentPane := range currentTab.panes {
			delete(m.wasBusy, currentPane.id)
			delete(m.soundSeq, currentPane.id)
			delete(m.paneAttention, currentPane.id)
			if currentPane.term != nil {
				currentPane.term.Close()
			}
		}
	}
	m.spaces = append(m.spaces[:m.selected], m.spaces[m.selected+1:]...)
	if m.selected >= len(m.spaces) {
		m.selected = max(0, len(m.spaces)-1)
	}
	m.clearFocusedAttention()
	m.persist()
}

func (m *Model) closeAll() {
	m.quitting = true
	m.persist()
	if m.kittyPushed {
		// Pop while the alt screen is still active, before Bubble Tea
		// restores the main screen.
		_, _ = os.Stdout.WriteString(seqEnhancedKeysOff)
		m.kittyPushed = false
	}
	for _, currentSpace := range m.spaces {
		for _, currentTab := range currentSpace.tabs {
			for _, currentPane := range currentTab.panes {
				if currentPane.term == nil {
					continue
				}
				if currentPane.floating == nil && m.holder != nil && currentPane.term.HolderSession() != 0 {
					// Holder-backed persistent panes detach so the next launch
					// can reattach. Floating panes are ephemeral and close instead.
					currentPane.term.Detach()
					continue
				}
				currentPane.term.Close()
			}
		}
	}
	if m.holder != nil {
		m.holder.Close()
	}
}

func (m *Model) currentSpace() *space {
	if len(m.spaces) == 0 || m.selected < 0 || m.selected >= len(m.spaces) {
		return nil
	}
	return m.spaces[m.selected]
}

func (m *Model) currentPane() *pane {
	currentSpace := m.currentSpace()
	if currentSpace == nil {
		return nil
	}
	currentTab := currentSpace.tab()
	if len(currentTab.panes) == 0 {
		return nil
	}
	currentTab.selected = clampInt(currentTab.selected, 0, len(currentTab.panes)-1)
	return currentTab.panes[currentTab.selected]
}

func (m *Model) paneByID(id int) (*pane, *space) {
	for _, currentSpace := range m.spaces {
		for _, currentTab := range currentSpace.tabs {
			for _, currentPane := range currentTab.panes {
				if currentPane.id == id {
					return currentPane, currentSpace
				}
			}
		}
	}
	return nil, nil
}

func waitForUpdate(id int, updates <-chan struct{}) tea.Cmd {
	return func() tea.Msg {
		_, open := <-updates
		return paneUpdateMsg{id: id, open: open}
	}
}

func encodeSGRMouse(msg tea.MouseMsg, x, y int) []byte {
	if x < 0 || y < 0 {
		return nil
	}
	button := 0
	switch msg.Button {
	case tea.MouseButtonLeft:
		button = 0
	case tea.MouseButtonMiddle:
		button = 1
	case tea.MouseButtonRight:
		button = 2
	case tea.MouseButtonWheelUp:
		button = 64
	case tea.MouseButtonWheelDown:
		button = 65
	default:
		return nil
	}
	if msg.Action == tea.MouseActionMotion {
		button += 32
	}
	final := "M"
	if msg.Action == tea.MouseActionRelease && msg.Button != tea.MouseButtonWheelUp && msg.Button != tea.MouseButtonWheelDown {
		final = "m"
	}
	return []byte(fmt.Sprintf("\x1b[<%d;%d;%d%s", button, x+1, y+1, final))
}

func truncate(value string, width int) string {
	runes := []rune(strings.ReplaceAll(value, "\n", " "))
	if width <= 0 {
		return ""
	}
	if len(runes) <= width {
		return string(runes)
	}
	if width <= 1 {
		return string(runes[:width])
	}
	return string(runes[:width-1]) + "…"
}
