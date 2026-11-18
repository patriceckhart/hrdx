// Package review backs the read-only Files and Changes panels of every hrdx
// frontend: bounded folder listings, file previews, git changes with their
// diffs, and syntax highlighting. Nothing here writes to the repository.
package review

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Limits keep huge folders and files from stalling a frontend.
const (
	PerFolder    = 500       // entries listed per folder
	PreviewBytes = 512 << 10 // bytes read for a preview
	MaxLines     = 5000      // preview or diff lines shown
	MaxLineLen   = 2000      // characters kept per line
)

// Entry is one item of a folder listing.
type Entry struct {
	Name string `json:"name"`
	Dir  bool   `json:"dir"`
}

// ErrOutside rejects paths that leave the panel's root folder.
var ErrOutside = errors.New("path is outside the folder")

// Resolve joins rel (slash separated) onto root and refuses results that
// leave root, also through symbolic links.
func Resolve(root, rel string) (string, error) {
	if root == "" {
		return "", errors.New("no folder")
	}
	rel = filepath.FromSlash(strings.TrimSpace(rel))
	if filepath.IsAbs(rel) || filepath.VolumeName(rel) != "" {
		return "", ErrOutside
	}
	joined := filepath.Clean(filepath.Join(root, rel))
	if !within(filepath.Clean(root), joined) {
		return "", ErrOutside
	}
	real, err := filepath.EvalSymlinks(joined)
	if err != nil {
		return joined, nil // missing paths fail when opened
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		realRoot = root
	}
	if !within(realRoot, real) {
		return "", ErrOutside
	}
	return joined, nil
}

func within(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// ListDir lists the folder rel below root: folders first, then files, both
// by name. .git is hidden. limited reports that entries were cut at
// PerFolder.
func ListDir(root, rel string) (entries []Entry, limited bool, err error) {
	dir, err := Resolve(root, rel)
	if err != nil {
		return nil, false, err
	}
	handle, err := os.Open(dir)
	if err != nil {
		return nil, false, err
	}
	defer handle.Close()
	items, err := handle.ReadDir(PerFolder + 2) // +1 for .git, +1 to detect the cut
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, false, err
	}
	for _, item := range items {
		if item.Name() == ".git" {
			continue
		}
		isDir := item.IsDir()
		if item.Type()&os.ModeSymlink != 0 {
			if target, err := os.Stat(filepath.Join(dir, item.Name())); err == nil {
				isDir = target.IsDir()
			}
		}
		entries = append(entries, Entry{Name: item.Name(), Dir: isDir})
	}
	if len(entries) > PerFolder {
		entries, limited = entries[:PerFolder], true
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Dir != entries[j].Dir {
			return entries[i].Dir
		}
		a, b := strings.ToLower(entries[i].Name), strings.ToLower(entries[j].Name)
		if a != b {
			return a < b
		}
		return entries[i].Name < entries[j].Name
	})
	return entries, limited, nil
}

// Preview is a highlighted read-only view of a file. Note explains why
// there are no lines (binary, unreadable) or that they were cut.
type Preview struct {
	Lines [][]Span `json:"lines"`
	Note  string   `json:"note,omitempty"`
}

// ReadPreview reads rel below root for display.
func ReadPreview(root, rel string) Preview {
	path, err := Resolve(root, rel)
	if err != nil {
		return Preview{Note: "Cannot open this file: " + err.Error()}
	}
	handle, err := os.Open(path)
	if err != nil {
		return Preview{Note: "Cannot open this file"}
	}
	defer handle.Close()
	data, err := io.ReadAll(io.LimitReader(handle, PreviewBytes+1))
	if err != nil {
		return Preview{Note: "Cannot read this file"}
	}
	if isBinary(data) {
		return Preview{Note: "Binary file, no preview"}
	}
	note := ""
	if len(data) > PreviewBytes {
		data, note = data[:PreviewBytes], "File truncated for preview"
	}
	lines := splitLines(string(data))
	if len(lines) > MaxLines {
		lines, note = lines[:MaxLines], "File truncated for preview"
	}
	return Preview{Lines: HighlightLines(rel, lines), Note: note}
}

func isBinary(data []byte) bool {
	sample := data
	if len(sample) > 8000 {
		sample = sample[:8000]
	}
	return bytes.IndexByte(sample, 0) >= 0
}

// splitLines splits text into lines without their endings, shortening
// overlong lines.
func splitLines(text string) []string {
	text = strings.TrimSuffix(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	if text == "" {
		return []string{""}
	}
	lines := strings.Split(text, "\n")
	for index, line := range lines {
		lines[index] = clipLine(line)
	}
	return lines
}

// clipLine expands tabs to 4-column stops (frontends draw a tab as one
// cell or none) and shortens overlong lines.
func clipLine(line string) string {
	if strings.IndexByte(line, '\t') >= 0 {
		var out strings.Builder
		column := 0
		for _, r := range line {
			if r == '\t' {
				pad := 4 - column%4
				out.WriteString(strings.Repeat(" ", pad))
				column += pad
				continue
			}
			out.WriteRune(r)
			column++
		}
		line = out.String()
	}
	if len(line) <= MaxLineLen {
		return line
	}
	cut := MaxLineLen
	for cut > 0 && !isRuneStart(line[cut]) {
		cut--
	}
	return line[:cut] + "…"
}

func isRuneStart(b byte) bool { return b&0xC0 != 0x80 }
