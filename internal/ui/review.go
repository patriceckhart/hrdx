package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/patriceckhart/hrdx/internal/review"
)

// The read-only Files and Changes panels dock right of the panes. Folder
// listings, previews, git status, and diffs load through tea.Cmds so a
// large folder or slow git never stalls the update loop. A generation
// number drops results that arrive after the panel moved on.

const (
	reviewNone    = ""
	reviewFiles   = "files"
	reviewChanges = "changes"

	reviewRefresh = 5 * time.Second
	reviewWidth   = 42 // percent of the terminal area
)

type reviewRow struct {
	path  string // relative to the panel folder (files) or repository (changes)
	name  string
	dir   bool
	depth int
	mark  string
}

type reviewPanel struct {
	kind   string // reviewFiles or reviewChanges, "" when closed
	gen    int
	dir    string // folder the panel follows
	focus  bool   // keyboard goes to the panel
	cursor int    // selected row
	scroll int    // first visible row of the list or code

	// Files tree.
	listings map[string][]review.Entry
	open     map[string]bool
	marks    map[string]string
	limited  bool
	errText  string

	// Changes list.
	changes review.Changes

	// Code view: a preview (files) or diff (changes). code == nil shows the
	// list.
	codePath string
	code     []string // rendered lines
	codeNote string
}

type reviewTickMsg struct{ gen int }

type reviewListingMsg struct {
	gen     int
	dir     string
	folder  string
	entries []review.Entry
	limited bool
	marks   map[string]string
	err     string
}

type reviewChangesMsg struct {
	gen     int
	dir     string
	changes review.Changes
}

type reviewCodeMsg struct {
	gen   int
	dir   string
	path  string
	lines []string
	note  string
}

// reviewTickFunc schedules the panel refresh. Tests replace it so they
// never wait for the timer.
var reviewTickFunc = func(gen int) tea.Cmd {
	return tea.Tick(reviewRefresh, func(time.Time) tea.Msg { return reviewTickMsg{gen: gen} })
}

func reviewTick(gen int) tea.Cmd { return reviewTickFunc(gen) }

// reviewDir is the folder the panels follow: the focused pane's start
// folder, or the workspace's.
func (m Model) reviewDir() string {
	current := m.currentSpace()
	if current == nil {
		return ""
	}
	if focused := m.currentPane(); focused != nil && focused.cwd != "" {
		return focused.cwd
	}
	return current.cwd
}

// toggleReview opens kind, switches to it, or closes it when it is open.
// Opening focuses the panel so the keyboard browses it.
func (m *Model) toggleReview(kind string) tea.Cmd {
	if m.review.kind == kind {
		m.closeReview()
		return nil
	}
	wasOpen := m.review.kind != reviewNone
	m.review = reviewPanel{kind: kind, gen: m.review.gen + 1, focus: true}
	if !wasOpen {
		m.relayoutForDock()
	}
	return m.reloadReview(true)
}

func (m *Model) closeReview() {
	if m.review.kind == reviewNone {
		return
	}
	m.review = reviewPanel{gen: m.review.gen + 1}
	if m.mode == modeReview {
		m.mode = modeTerminal
	}
	m.relayoutForDock()
}

// followReview reloads the panel when the folder it should show changed,
// after focus moved to another pane, tab, or workspace.
func (m *Model) followReview() tea.Cmd {
	if m.review.kind == reviewNone || m.reviewDir() == m.review.dir {
		return nil
	}
	return m.reloadReview(true)
}

// reloadReview refetches the panel's data. reset starts fresh, for a new
// folder; otherwise open folders and the open file refresh in place.
func (m *Model) reloadReview(reset bool) tea.Cmd {
	panel := &m.review
	dir := m.reviewDir()
	if reset || dir != panel.dir {
		gen := panel.gen + 1
		*panel = reviewPanel{kind: panel.kind, gen: gen, dir: dir, focus: panel.focus,
			listings: map[string][]review.Entry{}, open: map[string]bool{}}
	}
	if dir == "" {
		panel.errText = "No folder"
		return nil
	}
	gen := panel.gen
	commands := []tea.Cmd{reviewTick(gen)}
	switch panel.kind {
	case reviewFiles:
		commands = append(commands, loadListing(gen, dir, ""))
		for folder := range panel.open {
			commands = append(commands, loadListing(gen, dir, folder))
		}
		if panel.codePath != "" {
			commands = append(commands, loadPreview(gen, dir, panel.codePath))
		}
	case reviewChanges:
		commands = append(commands, loadChanges(gen, dir))
	}
	return tea.Batch(commands...)
}

func loadListing(gen int, dir, folder string) tea.Cmd {
	return func() tea.Msg {
		entries, limited, err := review.ListDir(dir, folder)
		msg := reviewListingMsg{gen: gen, dir: dir, folder: folder, entries: entries, limited: limited}
		if err != nil {
			msg.err = err.Error()
		}
		if folder == "" {
			msg.marks = review.Marks(review.ReadChanges(context.Background(), dir), dir)
		}
		return msg
	}
}

func loadChanges(gen int, dir string) tea.Cmd {
	return func() tea.Msg {
		return reviewChangesMsg{gen: gen, dir: dir, changes: review.ReadChanges(context.Background(), dir)}
	}
}

func loadPreview(gen int, dir, path string) tea.Cmd {
	return func() tea.Msg {
		preview := review.ReadPreview(dir, path)
		lines := make([]string, len(preview.Lines))
		for index, spans := range preview.Lines {
			lines[index] = styleLineNumber.Render(fmt.Sprintf("%4d ", index+1)) + renderSpans(spans)
		}
		return reviewCodeMsg{gen: gen, dir: dir, path: path, lines: lines, note: preview.Note}
	}
}

func loadDiff(gen int, dir string, change review.Change, root string) tea.Cmd {
	return func() tea.Msg {
		diff := review.ReadDiff(context.Background(), root, change)
		lines := make([]string, len(diff.Lines))
		for index, line := range diff.Lines {
			lines[index] = renderDiffLine(line)
		}
		return reviewCodeMsg{gen: gen, dir: dir, path: change.Path, lines: lines, note: diff.Note}
	}
}

// updateReview handles the panel's async results. It reports whether msg
// belonged to the panel.
func (m Model) updateReview(message tea.Msg) (bool, tea.Model, tea.Cmd) {
	panel := &m.review
	switch msg := message.(type) {
	case reviewTickMsg:
		if msg.gen != panel.gen || panel.kind == reviewNone {
			return true, m, nil
		}
		return true, m, m.reloadReview(false)
	case reviewListingMsg:
		if msg.gen != panel.gen || msg.dir != panel.dir {
			return true, m, nil
		}
		if msg.folder != "" && !panel.open[msg.folder] {
			return true, m, nil // closed while loading
		}
		panel.listings[msg.folder] = msg.entries
		if msg.folder == "" {
			panel.errText, panel.marks, panel.limited = msg.err, msg.marks, msg.limited
		} else if msg.limited {
			panel.limited = true
		}
		panel.clampCursor(len(m.reviewRows()))
		return true, m, nil
	case reviewChangesMsg:
		if msg.gen != panel.gen || msg.dir != panel.dir {
			return true, m, nil
		}
		panel.changes = msg.changes
		panel.clampCursor(len(msg.changes.Files))
		// An open diff refreshes, or closes when its file has no changes.
		if panel.codePath != "" {
			for _, change := range msg.changes.Files {
				if change.Path == panel.codePath {
					return true, m, loadDiff(panel.gen, panel.dir, change, msg.changes.Root)
				}
			}
			panel.codePath, panel.code, panel.codeNote = "", nil, ""
		}
		return true, m, nil
	case reviewCodeMsg:
		if msg.gen != panel.gen || msg.dir != panel.dir || msg.path != panel.codePath {
			return true, m, nil
		}
		panel.code, panel.codeNote = msg.lines, msg.note
		if len(panel.code) == 0 && panel.codeNote == "" {
			panel.codeNote = "Empty file"
		}
		return true, m, nil
	}
	return false, m, nil
}

func (p *reviewPanel) clampCursor(rows int) {
	p.cursor = clampInt(p.cursor, 0, max(0, rows-1))
}

// reviewRows flattens the Files tree, or lists the changes.
func (m Model) reviewRows() []reviewRow {
	panel := m.review
	var rows []reviewRow
	if panel.kind == reviewChanges {
		for _, change := range panel.changes.Files {
			rows = append(rows, reviewRow{path: change.Path, name: change.Path, mark: change.Status})
		}
		return rows
	}
	var walk func(folder string, depth int)
	walk = func(folder string, depth int) {
		for _, entry := range panel.listings[folder] {
			path := entry.Name
			if folder != "" {
				path = folder + "/" + entry.Name
			}
			rows = append(rows, reviewRow{path: path, name: entry.Name, dir: entry.Dir, depth: depth, mark: panel.marks[path]})
			if entry.Dir && panel.open[path] && depth < 40 {
				walk(path, depth+1)
			}
		}
	}
	walk("", 0)
	return rows
}

// reviewBox is the panel's strip in body coordinates (below the header).
func (m Model) reviewBox() rect {
	if m.review.kind == reviewNone {
		return rect{}
	}
	area := m.fullTerminalArea()
	w := m.reviewStripWidth(area)
	if w < 3 {
		return rect{}
	}
	return rect{x: m.sidebarContentWidth() + 1 + area.w - w, y: 1, w: w, h: area.h}
}

// reviewStripWidth reserves the panel's columns, leaving the panes their
// minimum width.
func (m Model) reviewStripWidth(area rect) int {
	if m.review.kind == reviewNone {
		return 0
	}
	w := min(area.w-minPaneCols, max(30, area.w*reviewWidth/100))
	if w < 20 {
		return 0
	}
	return w
}

// reviewContentRows is the number of rows below the panel's header.
func (m Model) reviewContentRows() int {
	return max(1, m.reviewBox().h-3)
}

// updateReviewKey handles keys while the panel has focus. Escape returns
// to the code list or closes the panel, the prefix still works, and every
// other key stays with the panel.
func (m Model) updateReviewKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	panel := &m.review
	key := msg.String()
	if key == m.prefixTrigger {
		m.mode = modePrefix
		return m, nil
	}
	page := m.reviewContentRows()
	if panel.codePath != "" {
		switch key {
		case "esc", "left", "h", "backspace":
			panel.codePath, panel.code, panel.codeNote, panel.scroll = "", nil, "", 0
		case "up", "k":
			panel.scroll--
		case "down", "j":
			panel.scroll++
		case "pgup", "ctrl+u":
			panel.scroll -= page
		case "pgdown", "ctrl+d", " ":
			panel.scroll += page
		case "home", "g":
			panel.scroll = 0
		case "end", "G":
			panel.scroll = len(panel.code)
		case "q":
			m.closeReview()
			return m, nil
		}
		panel.scroll = clampInt(panel.scroll, 0, max(0, len(panel.code)-page))
		return m, nil
	}
	rows := m.reviewRows()
	switch key {
	case "esc", "q":
		m.closeReview()
		return m, nil
	case "up", "k":
		panel.cursor--
	case "down", "j":
		panel.cursor++
	case "pgup":
		panel.cursor -= page
	case "pgdown":
		panel.cursor += page
	case "home", "g":
		panel.cursor = 0
	case "end", "G":
		panel.cursor = len(rows) - 1
	case "tab":
		other := reviewChanges
		if panel.kind == reviewChanges {
			other = reviewFiles
		}
		return m, m.toggleReview(other)
	case "left", "h":
		if panel.kind == reviewFiles && panel.cursor < len(rows) {
			row := rows[panel.cursor]
			if row.dir && panel.open[row.path] {
				m.collapse(row.path)
			} else if parent := parentPath(row.path); parent != "" {
				for index, candidate := range rows {
					if candidate.path == parent {
						panel.cursor = index
					}
				}
			}
		}
	case "enter", "right", "l", " ":
		if panel.cursor < len(rows) {
			return m, m.openReviewRow(rows[panel.cursor], key == "right" || key == "l")
		}
	}
	panel.clampCursor(len(rows))
	panel.scroll = scrollIntoView(panel.scroll, panel.cursor, page)
	return m, nil
}

// openReviewRow expands a folder, or shows a file's preview or diff.
// expandOnly keeps right arrow from opening files.
func (m *Model) openReviewRow(row reviewRow, expandOnly bool) tea.Cmd {
	panel := &m.review
	if row.dir {
		if panel.open[row.path] {
			if !expandOnly {
				m.collapse(row.path)
			}
			return nil
		}
		panel.open[row.path] = true
		return loadListing(panel.gen, panel.dir, row.path)
	}
	if expandOnly {
		return nil
	}
	panel.codePath, panel.code, panel.codeNote, panel.scroll = row.path, nil, "", 0
	if panel.kind == reviewChanges {
		for _, change := range panel.changes.Files {
			if change.Path == row.path {
				return loadDiff(panel.gen, panel.dir, change, panel.changes.Root)
			}
		}
		return nil
	}
	return loadPreview(panel.gen, panel.dir, row.path)
}

func (m *Model) collapse(folder string) {
	for path := range m.review.open {
		if path == folder || strings.HasPrefix(path, folder+"/") {
			delete(m.review.open, path)
			delete(m.review.listings, path)
		}
	}
}

func parentPath(path string) string {
	if index := strings.LastIndexByte(path, '/'); index >= 0 {
		return path[:index]
	}
	return ""
}

func scrollIntoView(scroll, cursor, page int) int {
	if cursor < scroll {
		return cursor
	}
	if cursor >= scroll+page {
		return cursor - page + 1
	}
	return scroll
}

// reviewMouse handles clicks and wheel events inside the panel. It reports
// whether the panel took the event.
func (m *Model) reviewMouse(msg tea.MouseMsg) (bool, tea.Cmd) {
	box := m.reviewBox()
	if box.w == 0 || m.mode != modeTerminal && m.mode != modeReview {
		return false, nil
	}
	x, y := msg.X-box.x, msg.Y-1-box.y
	if x < 0 || y < 0 || x >= box.w || y >= box.h {
		if msg.Action == tea.MouseActionPress && m.mode == modeReview {
			m.review.focus = false
			m.mode = modeTerminal
		}
		return false, nil
	}
	panel := &m.review
	page := m.reviewContentRows()
	switch msg.Button {
	case tea.MouseButtonWheelUp, tea.MouseButtonWheelDown:
		delta := 3
		if msg.Button == tea.MouseButtonWheelUp {
			delta = -3
		}
		if panel.codePath != "" {
			panel.scroll = clampInt(panel.scroll+delta, 0, max(0, len(panel.code)-page))
		} else {
			panel.scroll = clampInt(panel.scroll+delta, 0, max(0, len(m.reviewRows())-page))
		}
		return true, nil
	}
	if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		return true, nil
	}
	panel.focus = true
	m.mode = modeReview
	if y == 1 { // header row
		switch {
		case x >= box.w-4:
			m.closeReview()
		case panel.codePath != "" && x <= 4:
			panel.codePath, panel.code, panel.codeNote, panel.scroll = "", nil, "", 0
		}
		return true, nil
	}
	if panel.codePath != "" || y < 3 || y >= box.h-1 {
		return true, nil
	}
	index := panel.scroll + y - 3
	rows := m.reviewRows()
	if index >= 0 && index < len(rows) {
		panel.cursor = index
		return true, m.openReviewRow(rows[index], false)
	}
	return true, nil
}

var (
	styleLineNumber = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	styleDiffAdd    = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	styleDiffDel    = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	styleDiffMod    = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	styleDiffHunk   = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))

	// Syntax colors in the terminal's own ANSI palette, so they follow the
	// user's terminal theme.
	syntaxStyles = map[string]lipgloss.Style{
		"keyword": lipgloss.NewStyle().Foreground(lipgloss.Color("5")),
		"string":  lipgloss.NewStyle().Foreground(lipgloss.Color("2")),
		"comment": lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Italic(true),
		"number":  lipgloss.NewStyle().Foreground(lipgloss.Color("3")),
		"type":    lipgloss.NewStyle().Foreground(lipgloss.Color("6")),
		"func":    lipgloss.NewStyle().Foreground(lipgloss.Color("4")),
		"punct":   lipgloss.NewStyle().Foreground(lipgloss.Color("245")),
	}
)

func renderSpans(spans []review.Span) string {
	var out strings.Builder
	for _, span := range spans {
		text := sanitizeCode(span.Text)
		if style, ok := syntaxStyles[span.Class]; ok {
			out.WriteString(style.Render(text))
		} else {
			out.WriteString(text)
		}
	}
	return out.String()
}

// sanitizeCode drops control characters, so file contents can never send
// escape sequences to the host terminal.
func sanitizeCode(text string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) {
			return -1
		}
		return r
	}, text)
}

func renderDiffLine(line review.DiffLine) string {
	if line.Kind == "@" {
		var text strings.Builder
		for _, span := range line.Spans {
			text.WriteString(sanitizeCode(span.Text))
		}
		return styleDiffHunk.Render(text.String())
	}
	number := "     "
	if line.Number > 0 {
		number = fmt.Sprintf("%4d ", line.Number)
	}
	sign := " "
	signStyle := styleLineNumber
	switch line.Kind {
	case "+":
		sign, signStyle = "+", styleDiffAdd
	case "-":
		sign, signStyle = "-", styleDiffDel
	}
	return styleLineNumber.Render(number) + signStyle.Render(sign) + " " + renderSpans(line.Spans)
}

func markStyle(mark string) lipgloss.Style {
	switch mark {
	case "A":
		return styleDiffAdd
	case "D", "U":
		return styleDiffDel
	}
	return styleDiffMod
}

// renderReview draws the panel into its strip of the body rows.
func (m Model) renderReview(rows []string) {
	box := m.reviewBox()
	if box.w < 3 || box.h < 4 {
		return
	}
	panel := m.review
	borderColor := colorFaint
	if m.mode == modeReview {
		borderColor = colorAccent
	}
	border := lipgloss.NewStyle().Foreground(borderColor)
	inner := box.w - 2
	title := "Files"
	if panel.kind == reviewChanges {
		title = "Changes"
	}
	lines := make([]string, 0, box.h)
	lines = append(lines, border.Render("╭"+strings.Repeat("─", inner)+"╮"))

	// Header: back arrow, title, details, close button.
	header := " " + styleSpaceDim.Render(title)
	info := ""
	switch {
	case panel.codePath != "":
		header = " " + stylePaneDim.Render("←") + " " + styleSpaceDim.Render(title)
		info = panel.codePath
	case panel.kind == reviewFiles:
		info = lastElement(panel.dir)
	default:
		add, del := review.Totals(panel.changes.Files)
		count := len(panel.changes.Files)
		noun := "files"
		if count == 1 {
			noun = "file"
		}
		info = fmt.Sprintf("%d %s %s %s", count, noun, styleDiffAdd.Render(fmt.Sprintf("+%d", add)), styleDiffDel.Render(fmt.Sprintf("-%d", del)))
	}
	closeButton := stylePaneDim.Render(" x ")
	space := inner - lipgloss.Width(header) - lipgloss.Width(closeButton)
	if panel.codePath != "" || panel.kind == reviewFiles {
		info = stylePaneDim.Render(cutLeft(info, max(0, space-2)))
	}
	gap := max(1, space-lipgloss.Width(info))
	lines = append(lines, border.Render("│")+fit(header+strings.Repeat(" ", gap)+info+closeButton, inner)+border.Render("│"))
	lines = append(lines, border.Render("├"+strings.Repeat("─", inner)+"┤"))

	content := box.h - 4
	body := m.reviewBody(content, inner)
	for _, line := range body {
		lines = append(lines, border.Render("│")+fit(line, inner)+border.Render("│"))
	}
	lines = append(lines, border.Render("╰"+strings.Repeat("─", inner)+"╯"))

	for index, line := range lines {
		y := box.y + index
		if y >= 0 && y < len(rows) {
			rows[y] = overlayAt(rows[y], line, box.x, box.w)
		}
	}
}

// reviewBody renders exactly height lines of list or code.
func (m Model) reviewBody(height, width int) []string {
	panel := m.review
	out := make([]string, 0, height)
	note := func(text string) {
		if len(out) < height {
			out = append(out, stylePaneDim.Render(" "+text))
		}
	}
	if panel.codePath != "" {
		if panel.codeNote != "" {
			note(panel.codeNote)
		}
		if panel.code == nil && panel.codeNote == "" {
			note("Loading…")
		}
		for index := panel.scroll; index < len(panel.code) && len(out) < height; index++ {
			out = append(out, " "+panel.code[index])
		}
	} else {
		rows := m.reviewRows()
		switch {
		case panel.errText != "" && panel.kind == reviewFiles:
			note(panel.errText)
		case panel.kind == reviewChanges && panel.changes.Error != "":
			note(panel.changes.Error)
		case panel.kind == reviewFiles && panel.listings[""] == nil:
			note("Loading files…")
		case panel.kind == reviewChanges && panel.changes.Root == "" && panel.changes.Error == "":
			note("Loading changes…")
		case len(rows) == 0 && panel.kind == reviewChanges:
			note("No uncommitted changes")
		case len(rows) == 0:
			note("No files")
		}
		scroll := clampInt(panel.scroll, 0, max(0, len(rows)-height))
		for index := scroll; index < len(rows) && len(out) < height; index++ {
			out = append(out, m.renderReviewRow(rows[index], index == panel.cursor, width))
		}
		if panel.limited || panel.changes.Limited {
			if len(out) == height {
				out = out[:height-1]
			}
			note("List limited")
		}
	}
	for len(out) < height {
		out = append(out, "")
	}
	return out
}

func (m Model) renderReviewRow(row reviewRow, selected bool, width int) string {
	var line string
	if m.review.kind == reviewChanges {
		folder, name := splitDir(row.path)
		stats := ""
		for _, change := range m.review.changes.Files {
			if change.Path == row.path {
				if change.Add > 0 {
					stats += " " + styleDiffAdd.Render(fmt.Sprintf("+%d", change.Add))
				}
				if change.Del > 0 {
					stats += " " + styleDiffDel.Render(fmt.Sprintf("-%d", change.Del))
				}
			}
		}
		pathWidth := max(1, width-3-lipgloss.Width(stats)-1)
		path := cutLeft(folder+name, pathWidth)
		if strings.HasSuffix(path, name) && len(path) > len(name) {
			path = stylePaneDim.Render(path[:len(path)-len(name)]) + name
		}
		line = " " + markStyle(row.mark).Render(row.mark) + " " + path
		line += strings.Repeat(" ", max(0, width-lipgloss.Width(line)-lipgloss.Width(stats))) + stats
	} else {
		icon := "  "
		if row.dir {
			icon = "▸ "
			if m.review.open[row.path] {
				icon = "▾ "
			}
		}
		name := row.name
		if row.dir {
			name += "/"
		}
		indent := strings.Repeat("  ", row.depth)
		mark := ""
		if row.mark != "" && !row.dir {
			mark = " " + markStyle(row.mark).Render(row.mark)
		}
		styled := name
		if row.mark != "" {
			styled = markStyle(row.mark).Render(name)
		}
		line = " " + indent + stylePaneDim.Render(icon) + styled
		line = ansiCut(line, 0, max(1, width-lipgloss.Width(mark)))
		line += strings.Repeat(" ", max(0, width-lipgloss.Width(line)-lipgloss.Width(mark))) + mark
	}
	if selected {
		focus := lipgloss.NewStyle().Background(colorFaint)
		if m.mode == modeReview {
			focus = lipgloss.NewStyle().Background(lipgloss.Color("237"))
		}
		return focus.Render(fit(line, width))
	}
	return line
}

func splitDir(path string) (string, string) {
	if index := strings.LastIndexByte(path, '/'); index >= 0 {
		return path[:index+1], path[index+1:]
	}
	return "", path
}

func lastElement(path string) string {
	path = strings.TrimRight(strings.ReplaceAll(path, "\\", "/"), "/")
	if index := strings.LastIndexByte(path, '/'); index >= 0 {
		return path[index+1:]
	}
	return path
}

// cutLeft keeps the end of a plain string within width cells, so a long
// path shows its file name.
func cutLeft(text string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(text) <= width {
		return text
	}
	runes := []rune(text)
	for len(runes) > 0 && lipgloss.Width(string(runes))+1 > width {
		runes = runes[1:]
	}
	return "…" + string(runes)
}

// fit clips or pads an ANSI line to exactly width cells.
func fit(line string, width int) string {
	line = ansiCut(line, 0, width)
	if gap := width - lipgloss.Width(line); gap > 0 {
		line += strings.Repeat(" ", gap)
	}
	return line + "\x1b[0m"
}
