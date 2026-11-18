package review

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// MaxChanges bounds the changed-file list.
const MaxChanges = 1000

// gitTimeout bounds every git call so a hung git cannot stall a panel.
const gitTimeout = 10 * time.Second

// readOnly makes git write nothing under .git: no index refresh, no lock,
// no fsmonitor cookie. Panels only look.
var readOnly = []string{"--no-optional-locks", "-c", "core.fsmonitor=false", "-c", "diff.autoRefreshIndex=false"}

// Change is one file with uncommitted changes.
type Change struct {
	Path   string `json:"path"`
	Status string `json:"status"` // M modified, A added or untracked, D deleted, U conflict
	Add    int    `json:"add"`
	Del    int    `json:"del"`
	Binary bool   `json:"binary,omitempty"`
	Fresh  bool   `json:"fresh,omitempty"` // untracked
}

// Changes is the state of a repository's working tree.
type Changes struct {
	Root    string   `json:"root,omitempty"` // repository top level
	Files   []Change `json:"files"`
	Limited bool     `json:"limited,omitempty"`
	Error   string   `json:"error,omitempty"` // "Not a git repository" and other failures
}

// ErrNoGit means git is not installed.
var ErrNoGit = errors.New("git is not installed")

func git(ctx context.Context, dir string, args ...string) ([]byte, error) {
	gitPath, err := exec.LookPath("git")
	if err != nil {
		return nil, ErrNoGit
	}
	ctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()
	command := exec.CommandContext(ctx, gitPath, append([]string{"-C", dir}, args...)...)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	out, err := command.Output()
	if err != nil {
		if message := strings.TrimSpace(stderr.String()); message != "" {
			return out, errors.New(message)
		}
		return out, err
	}
	return out, nil
}

// TopLevel returns the repository root containing dir, or "" outside git.
func TopLevel(ctx context.Context, dir string) string {
	out, err := git(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return ""
	}
	return filepath.Clean(strings.TrimSpace(string(out)))
}

// ReadChanges lists the uncommitted changes of the repository at dir
// against HEAD, with added and deleted line counts.
func ReadChanges(ctx context.Context, dir string) Changes {
	root := TopLevel(ctx, dir)
	if root == "" {
		if _, err := exec.LookPath("git"); err != nil {
			return Changes{Error: ErrNoGit.Error()}
		}
		return Changes{Error: "Not a git repository"}
	}
	run := func(args ...string) ([]byte, error) { return git(ctx, root, append(readOnly, args...)...) }
	status, err := run("status", "--porcelain=v1", "-z", "--no-renames", "--untracked-files=all")
	if err != nil {
		return Changes{Root: root, Error: err.Error()}
	}
	stats := map[string]Change{}
	_, headErr := run("rev-parse", "--verify", "--quiet", "HEAD")
	var numstats [][]byte
	if headErr == nil {
		out, err := run("diff", "HEAD", "--numstat", "-z", "--no-renames")
		if err != nil {
			return Changes{Root: root, Error: err.Error()}
		}
		numstats = append(numstats, out)
	} else {
		// A repository without commits: staged and unstaged diffs.
		for _, args := range [][]string{{"diff", "--cached", "--numstat", "-z", "--no-renames"}, {"diff", "--numstat", "-z", "--no-renames"}} {
			out, err := run(args...)
			if err != nil {
				return Changes{Root: root, Error: err.Error()}
			}
			numstats = append(numstats, out)
		}
	}
	for _, out := range numstats {
		for path, change := range parseNumstat(out) {
			old := stats[path]
			stats[path] = Change{Add: old.Add + change.Add, Del: old.Del + change.Del, Binary: old.Binary || change.Binary}
		}
	}
	files, limited := parseStatus(status, stats)
	// git diff leaves out untracked files: count their lines directly.
	for index, file := range files {
		if file.Fresh {
			files[index].Add, files[index].Binary = countLines(root, file.Path)
		}
	}
	return Changes{Root: root, Files: files, Limited: limited}
}

// countLines counts the lines of an untracked file, reading at most
// PreviewBytes. Binary and unreadable files count as zero.
func countLines(root, rel string) (lines int, binary bool) {
	path, err := Resolve(root, rel)
	if err != nil {
		return 0, false
	}
	handle, err := os.Open(path)
	if err != nil {
		return 0, false
	}
	defer handle.Close()
	data, _ := io.ReadAll(io.LimitReader(handle, PreviewBytes))
	if isBinary(data) {
		return 0, true
	}
	lines = bytes.Count(data, []byte{'\n'})
	if len(data) > 0 && data[len(data)-1] != '\n' {
		lines++
	}
	return lines, false
}

// parseNumstat reads `git diff --numstat -z`: "add\tdel\tpath\0".
func parseNumstat(out []byte) map[string]Change {
	result := map[string]Change{}
	for _, entry := range strings.Split(string(out), "\x00") {
		fields := strings.SplitN(entry, "\t", 3)
		if len(fields) != 3 || fields[2] == "" {
			continue
		}
		add, _ := strconv.Atoi(fields[0])
		del, _ := strconv.Atoi(fields[1])
		result[fields[2]] = Change{Add: add, Del: del, Binary: fields[0] == "-"}
	}
	return result
}

// parseStatus reads `git status --porcelain=v1 -z --no-renames`.
func parseStatus(out []byte, stats map[string]Change) ([]Change, bool) {
	var files []Change
	entries := strings.Split(string(out), "\x00")
	limited := false
	for _, entry := range entries {
		if len(entry) < 4 {
			continue
		}
		if len(files) == MaxChanges {
			limited = true
			break
		}
		xy, path := entry[:2], entry[3:]
		fresh := xy == "??"
		status := "M"
		switch {
		case strings.Contains(xy, "U") || xy == "AA" || xy == "DD":
			status = "U"
		case strings.Contains(xy, "D"):
			status = "D"
		case fresh || strings.Contains(xy, "A"):
			status = "A"
		}
		stat := stats[path]
		files = append(files, Change{Path: path, Status: status, Add: stat.Add, Del: stat.Del, Binary: stat.Binary, Fresh: fresh})
	}
	return files, limited
}

// Marks maps paths relative to dir onto change statuses, for the Files
// tree. Folders containing changes are marked "M".
func Marks(changes Changes, dir string) map[string]string {
	marks := map[string]string{}
	if changes.Root == "" {
		return marks
	}
	// git prints the real path (/private/var on macOS), the folder may use a
	// symlinked one (/var), so compare resolved paths.
	root, folder := changes.Root, dir
	if real, err := filepath.EvalSymlinks(root); err == nil {
		root = real
	}
	if real, err := filepath.EvalSymlinks(folder); err == nil {
		folder = real
	}
	prefix, err := filepath.Rel(root, folder)
	if err != nil || prefix == ".." || strings.HasPrefix(prefix, ".."+string(filepath.Separator)) {
		return marks
	}
	prefix = filepath.ToSlash(prefix)
	for _, change := range changes.Files {
		rel := change.Path
		if prefix != "." {
			var ok bool
			if rel, ok = strings.CutPrefix(rel, prefix+"/"); !ok {
				continue
			}
		}
		marks[rel] = change.Status
		for parent := filepath.ToSlash(filepath.Dir(rel)); parent != "." && parent != "/"; parent = filepath.ToSlash(filepath.Dir(parent)) {
			if _, ok := marks[parent]; !ok {
				marks[parent] = "M"
			}
		}
	}
	return marks
}

// DiffLine is one row of a diff: Kind is "@" for a hunk header, "+", "-",
// " " for context, or "" for other lines. Number is the line number in the
// new file (old file for deletions), 0 when not applicable.
type DiffLine struct {
	Kind   string `json:"k"`
	Number int    `json:"n,omitempty"`
	Spans  []Span `json:"s"`
}

// Diff is the change of one file. Note explains a missing text diff.
type Diff struct {
	Lines []DiffLine `json:"lines"`
	Note  string     `json:"note,omitempty"`
}

// ReadDiff returns the diff of path (relative to the repository root)
// against HEAD, with highlighted code. Untracked and, in a repository
// without commits, added files show as entirely added.
func ReadDiff(ctx context.Context, root string, change Change) Diff {
	if change.Binary {
		return Diff{Note: "Binary file changed"}
	}
	_, headErr := git(ctx, root, append(readOnly, "rev-parse", "--verify", "--quiet", "HEAD")...)
	if change.Fresh || (headErr != nil && change.Status == "A") {
		preview := ReadPreview(root, change.Path)
		if len(preview.Lines) == 0 {
			return Diff{Note: preview.Note}
		}
		lines := make([]DiffLine, 0, len(preview.Lines)+1)
		lines = append(lines, DiffLine{Kind: "@", Spans: []Span{{Text: "@@ -0,0 +1," + strconv.Itoa(len(preview.Lines)) + " @@"}}})
		for index, spans := range preview.Lines {
			lines = append(lines, DiffLine{Kind: "+", Number: index + 1, Spans: spans})
		}
		return Diff{Lines: lines, Note: preview.Note}
	}
	args := append(append([]string{}, readOnly...), "diff")
	if headErr == nil {
		args = append(args, "HEAD")
	}
	args = append(args, "--no-ext-diff", "--no-textconv", "--no-color", "--", change.Path)
	out, err := git(ctx, root, args...)
	if err != nil {
		return Diff{Note: "Cannot read this diff: " + err.Error()}
	}
	return ParseDiff(change.Path, string(out))
}

// ParseDiff turns unified diff text into numbered, highlighted rows. Old
// and new sides are highlighted separately so a block comment opened in a
// removed line does not color the added ones.
func ParseDiff(path, text string) Diff {
	raw := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	first := -1
	for index, line := range raw {
		if strings.HasPrefix(line, "@@") {
			first = index
			break
		}
	}
	if first < 0 {
		return Diff{Note: "No text diff available"}
	}
	raw = raw[first:]
	note := ""
	if len(raw) > MaxLines {
		raw, note = raw[:MaxLines], "Diff truncated"
	}
	lang, highlight := languageFor(path)
	var oldState, newState lexState
	paint := func(code string, state *lexState) []Span {
		code = clipLine(code)
		if !highlight {
			return []Span{{Text: code}}
		}
		return lang.highlight(code, state)
	}
	lines := make([]DiffLine, 0, len(raw))
	oldLine, newLine := 0, 0
	for _, line := range raw {
		if strings.HasPrefix(line, "@@") {
			oldLine, newLine = hunkStart(line)
			oldState, newState = lexState{}, lexState{}
			lines = append(lines, DiffLine{Kind: "@", Spans: []Span{{Text: clipLine(line)}}})
			continue
		}
		if line == "" {
			line = " "
		}
		switch line[0] {
		case '+':
			lines = append(lines, DiffLine{Kind: "+", Number: newLine, Spans: paint(line[1:], &newState)})
			newLine++
		case '-':
			lines = append(lines, DiffLine{Kind: "-", Number: oldLine, Spans: paint(line[1:], &oldState)})
			oldLine++
		case ' ':
			code := line[1:]
			paint(code, &oldState)
			lines = append(lines, DiffLine{Kind: " ", Number: newLine, Spans: paint(code, &newState)})
			oldLine++
			newLine++
		default: // "\ No newline at end of file"
			lines = append(lines, DiffLine{Kind: "", Spans: []Span{{Text: clipLine(line)}}})
		}
	}
	return Diff{Lines: lines, Note: note}
}

// hunkStart reads the first old and new line numbers of "@@ -a,b +c,d @@".
func hunkStart(header string) (oldLine, newLine int) {
	for _, field := range strings.Fields(header) {
		if len(field) < 2 || (field[0] != '-' && field[0] != '+') {
			continue
		}
		number, _, _ := strings.Cut(field[1:], ",")
		value, err := strconv.Atoi(number)
		if err != nil {
			continue
		}
		if field[0] == '-' {
			oldLine = value
		} else {
			newLine = value
		}
	}
	return oldLine, newLine
}

// Totals sums the added and deleted lines of all changes.
func Totals(files []Change) (add, del int) {
	for _, file := range files {
		add += file.Add
		del += file.Del
	}
	return add, del
}
