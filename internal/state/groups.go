package state

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ValidateGroupPath checks a workspace group path: every name holds 1 to 80
// nonblank characters without controls, and the joined path stays within
// 4096 bytes. An empty path means a standalone workspace and is valid.
func ValidateGroupPath(path []string) error {
	size := 0
	for _, name := range path {
		size += len(name) + 1
		if !utf8.ValidString(name) || strings.TrimSpace(name) == "" || len([]rune(name)) > 80 || strings.IndexFunc(name, unicode.IsControl) >= 0 {
			return fmt.Errorf("group names must contain 1 to 80 nonblank characters and no controls")
		}
	}
	if size > 4096 {
		return fmt.Errorf("group_path must be at most 4096 bytes including separators")
	}
	return nil
}
