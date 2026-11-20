package agent

import (
	"errors"
	"strings"
)

// SplitArgs splits a line of extra agent arguments the way a POSIX shell
// would split words: whitespace separates, single and double quotes group,
// and a backslash escapes the next character outside single quotes. Nothing
// is expanded and no shell runs: the result goes straight into the agent's
// argument list.
func SplitArgs(line string) ([]string, error) {
	var args []string
	var current strings.Builder
	inWord := false
	var quote rune
	escaped := false
	for _, r := range line {
		switch {
		case escaped:
			current.WriteRune(r)
			escaped = false
		case r == '\\' && quote != '\'':
			escaped, inWord = true, true
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				current.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote, inWord = r, true
		case r == ' ' || r == '\t' || r == '\n' || r == '\r':
			if inWord {
				args = append(args, current.String())
				current.Reset()
				inWord = false
			}
		default:
			current.WriteRune(r)
			inWord = true
		}
	}
	if quote != 0 {
		return nil, errors.New("arguments: missing closing quote")
	}
	if escaped {
		return nil, errors.New("arguments: trailing backslash")
	}
	if inWord {
		args = append(args, current.String())
	}
	return args, nil
}
