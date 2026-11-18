package review

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestListDirSortsAndHidesGit(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "b.txt"), "")
	write(t, filepath.Join(root, "A.txt"), "")
	write(t, filepath.Join(root, "src", "x.go"), "")
	write(t, filepath.Join(root, ".git", "HEAD"), "")
	write(t, filepath.Join(root, ".env"), "")

	entries, limited, err := ListDir(root, "")
	if err != nil || limited {
		t.Fatal(err, limited)
	}
	var got []string
	for _, entry := range entries {
		name := entry.Name
		if entry.Dir {
			name += "/"
		}
		got = append(got, name)
	}
	if strings.Join(got, " ") != "src/ .env A.txt b.txt" {
		t.Fatalf("entries = %v", got)
	}
	if sub, _, err := ListDir(root, "src"); err != nil || len(sub) != 1 || sub[0].Name != "x.go" {
		t.Fatalf("src = %v %v", sub, err)
	}
}

func TestListDirLimit(t *testing.T) {
	root := t.TempDir()
	for index := range PerFolder + 3 {
		write(t, filepath.Join(root, "f"+strings.Repeat("x", index%7)+string(rune('a'+index%26))+string(rune('a'+index/26))), "")
	}
	entries, limited, err := ListDir(root, "")
	if err != nil || !limited || len(entries) != PerFolder {
		t.Fatalf("len %d limited %v err %v", len(entries), limited, err)
	}
}

func TestResolveRefusesEscapes(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	write(t, filepath.Join(outside, "secret"), "x")
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	for _, rel := range []string{"../x", "a/../../x", "/etc/passwd", "link/secret", "link"} {
		if _, err := Resolve(root, rel); err == nil {
			t.Errorf("%q resolved", rel)
		}
	}
	if _, err := Resolve(root, "a/../b"); err != nil {
		t.Errorf("a/../b: %v", err)
	}
	if preview := ReadPreview(root, "../"+filepath.Base(outside)+"/secret"); len(preview.Lines) != 0 {
		t.Fatal("preview escaped the root")
	}
}

func TestReadPreview(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "main.go"), "package main\n\n// hi\nfunc main() {}\n")
	write(t, filepath.Join(root, "blob.bin"), "a\x00b")
	write(t, filepath.Join(root, "tabs.txt"), "\tx\n")

	preview := ReadPreview(root, "main.go")
	if len(preview.Lines) != 4 || preview.Note != "" {
		t.Fatalf("preview = %+v", preview)
	}
	if first := preview.Lines[0][0]; first.Text != "package" || first.Class != "keyword" {
		t.Fatalf("first span = %+v", first)
	}
	if note := ReadPreview(root, "blob.bin").Note; !strings.Contains(note, "Binary") {
		t.Fatalf("binary note = %q", note)
	}
	if line := ReadPreview(root, "tabs.txt").Lines[0][0].Text; line != "    x" {
		t.Fatalf("tab expansion = %q", line)
	}
	if note := ReadPreview(root, "missing").Note; note == "" {
		t.Fatal("missing file has no note")
	}
}

func classes(spans []Span) map[string]string {
	out := map[string]string{}
	for _, span := range spans {
		out[strings.TrimSpace(span.Text)] = span.Class
	}
	return out
}

func TestHighlight(t *testing.T) {
	lines := HighlightLines("x.go", []string{
		`func Run(name string) error { return fmt.Errorf("x %d", 42) } // done`,
		"/* open",
		"still comment */ var x = `raw",
		"raw end` + 1",
	})
	got := classes(lines[0])
	for text, want := range map[string]string{"func": "keyword", "Run": "func", "string": "type", "error": "type", "return": "keyword", `"x %d"`: "string", "42": "number", "// done": "comment"} {
		if got[text] != want {
			t.Errorf("%q class %q, want %q (spans %+v)", text, got[text], want, lines[0])
		}
	}
	if lines[1][0].Class != "comment" || lines[2][0].Class != "comment" || lines[2][0].Text != "still comment */" {
		t.Errorf("block comment spans = %+v / %+v", lines[1], lines[2])
	}
	if last := lines[2][len(lines[2])-1]; last.Class != "string" || lines[3][0].Text != "raw end`" {
		t.Errorf("raw string spans = %+v / %+v", lines[2], lines[3])
	}

	py := HighlightLines("a.py", []string{"def f(): return None  # note"})
	if got := classes(py[0]); got["def"] != "keyword" || got["# note"] != "comment" || got["None"] != "keyword" {
		t.Errorf("python spans = %+v", py[0])
	}
	sh := HighlightLines("run.sh", []string{`echo "$#" ${#x}`})
	for _, span := range sh[0] {
		if span.Class == "comment" {
			t.Errorf("shell $# read as a comment: %+v", sh[0])
		}
	}
	plain := HighlightLines("notes.unknown", []string{"func x"})
	if len(plain[0]) != 1 || plain[0][0].Class != "" {
		t.Errorf("unknown type highlighted: %+v", plain[0])
	}
	// Text survives highlighting unchanged.
	for _, spans := range lines {
		var text strings.Builder
		for _, span := range spans {
			text.WriteString(span.Text)
		}
		if text.Len() == 0 {
			t.Error("lost text")
		}
	}
}

func TestParseDiffNumbersLines(t *testing.T) {
	diff := ParseDiff("a.go", "diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n@@ -3,3 +3,4 @@ func x\n ctx\n-old\n+new\n+more\n\\ No newline at end of file\n")
	var got []string
	for _, line := range diff.Lines {
		got = append(got, line.Kind+":"+itoa(line.Number))
	}
	want := "@:0  :3 -:4 +:4 +:5 :0"
	if strings.Join(got, " ") != want {
		t.Fatalf("rows = %v, want %s", got, want)
	}
	if note := ParseDiff("a.go", "Binary files differ\n").Note; note == "" {
		t.Fatal("no hunk should give a note")
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for ; n > 0; n /= 10 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
	}
	return string(digits)
}

func TestParseStatusAndNumstat(t *testing.T) {
	stats := parseNumstat([]byte("3\t1\ta.go\x00-\t-\timg.png\x00"))
	files, limited := parseStatus([]byte(" M a.go\x00?? new.txt\x00D  gone.go\x00UU both.go\x00M  img.png\x00"), stats)
	if limited || len(files) != 5 {
		t.Fatalf("files = %+v", files)
	}
	want := []Change{
		{Path: "a.go", Status: "M", Add: 3, Del: 1},
		{Path: "new.txt", Status: "A", Fresh: true},
		{Path: "gone.go", Status: "D"},
		{Path: "both.go", Status: "U"},
		{Path: "img.png", Status: "M", Binary: true},
	}
	for index, file := range files {
		if file != want[index] {
			t.Errorf("file %d = %+v, want %+v", index, file, want[index])
		}
	}
}

func TestMarks(t *testing.T) {
	changes := Changes{Root: "/repo", Files: []Change{{Path: "pkg/a/x.go", Status: "A"}, {Path: "top.go", Status: "D"}}}
	marks := Marks(changes, "/repo")
	if marks["pkg/a/x.go"] != "A" || marks["pkg/a"] != "M" || marks["pkg"] != "M" || marks["top.go"] != "D" {
		t.Fatalf("marks = %v", marks)
	}
	sub := Marks(changes, "/repo/pkg")
	if sub["a/x.go"] != "A" || sub["a"] != "M" || len(sub) != 2 {
		t.Fatalf("sub marks = %v", sub)
	}
}

// End to end against a real repository, skipped without git.
func TestReadChangesAndDiff(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		command := exec.Command("git", append([]string{"-C", root}, args...)...)
		command.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if out, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	run("init", "-q")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "test")
	write(t, filepath.Join(root, "a.go"), "package a\n\nvar x = 1\n")
	run("add", "a.go")
	run("commit", "-q", "-m", "init")
	write(t, filepath.Join(root, "a.go"), "package a\n\nvar x = 2\n")
	write(t, filepath.Join(root, "sub", "new.py"), "print(1)\n")

	ctx := context.Background()
	changes := ReadChanges(ctx, filepath.Join(root, "sub"))
	if changes.Error != "" || len(changes.Files) != 2 {
		t.Fatalf("changes = %+v", changes)
	}
	if add, del := Totals(changes.Files); add != 2 || del != 1 {
		t.Fatalf("totals = +%d -%d", add, del)
	}
	var modified, fresh Change
	for _, file := range changes.Files {
		if file.Path == "a.go" {
			modified = file
		} else {
			fresh = file
		}
	}
	diff := ReadDiff(ctx, changes.Root, modified)
	kinds := ""
	for _, line := range diff.Lines {
		kinds += line.Kind
	}
	if kinds != "@  -+" {
		t.Fatalf("diff kinds = %q (%+v)", kinds, diff)
	}
	added := ReadDiff(ctx, changes.Root, fresh)
	if len(added.Lines) != 2 || added.Lines[1].Kind != "+" || added.Lines[1].Number != 1 {
		t.Fatalf("new file diff = %+v", added)
	}
	// Reads never touch the index.
	if _, err := os.Stat(filepath.Join(root, ".git", "index.lock")); err == nil {
		t.Fatal("index.lock left behind")
	}

	if outside := ReadChanges(ctx, t.TempDir()); outside.Error != "Not a git repository" {
		t.Fatalf("outside = %+v", outside)
	}
}
