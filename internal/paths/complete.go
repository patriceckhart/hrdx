package paths

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ResolveDir expands ~, makes the path absolute, and requires a directory.
func ResolveDir(value string) (string, error) {
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

// CompleteDir returns directory completions for a partially typed path.
// The result values are full input strings (not just the missing suffix),
// with ~ preserved when the input used it.
func CompleteDir(input string) []string {
	if input == "" {
		input = "~/"
	}

	display := input
	expanded := input
	home, homeErr := os.UserHomeDir()
	if homeErr == nil && (input == "~" || strings.HasPrefix(input, "~/")) {
		expanded = filepath.Join(home, strings.TrimPrefix(input, "~"))
		if strings.HasSuffix(input, "/") && !strings.HasSuffix(expanded, "/") {
			expanded += "/"
		}
	}

	dir := expanded
	prefix := ""
	if !strings.HasSuffix(expanded, "/") {
		dir = filepath.Dir(expanded)
		prefix = filepath.Base(expanded)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	displayDir := display
	if !strings.HasSuffix(display, "/") {
		if idx := strings.LastIndex(display, "/"); idx >= 0 {
			displayDir = display[:idx+1]
		} else {
			displayDir = ""
		}
	}

	var matches []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if prefix != "" && !strings.HasPrefix(strings.ToLower(name), strings.ToLower(prefix)) {
			continue
		}
		if prefix == "" && strings.HasPrefix(name, ".") {
			continue
		}
		matches = append(matches, displayDir+name)
	}
	sort.Strings(matches)
	if len(matches) > 8 {
		matches = matches[:8]
	}
	return matches
}

// CommonPrefix returns the longest shared prefix of the candidates.
func CommonPrefix(values []string) string {
	if len(values) == 0 {
		return ""
	}
	shortest := values[0]
	for _, value := range values[1:] {
		for !strings.HasPrefix(value, shortest) {
			shortest = shortest[:len(shortest)-1]
			if shortest == "" {
				return ""
			}
		}
	}
	return shortest
}
