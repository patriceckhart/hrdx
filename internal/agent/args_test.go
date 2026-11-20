package agent

import (
	"slices"
	"testing"
)

func TestSplitArgs(t *testing.T) {
	cases := []struct {
		line string
		want []string
	}{
		{"", nil},
		{"   ", nil},
		{"--codemode", []string{"--codemode"}},
		{"  --model  gpt-5 \t--fast ", []string{"--model", "gpt-5", "--fast"}},
		{`--prompt "fix the bug" --tag 'a b'`, []string{"--prompt", "fix the bug", "--tag", "a b"}},
		{`--path my\ dir --q "say \"hi\""`, []string{"--path", "my dir", "--q", `say "hi"`}},
		{`--empty "" ''`, []string{"--empty", "", ""}},
		{`'keeps \ backslash'`, []string{`keeps \ backslash`}},
		{`a"b c"d`, []string{"ab cd"}},
		// Nothing is expanded: no shell runs.
		{"$HOME ~ * ; rm -rf", []string{"$HOME", "~", "*", ";", "rm", "-rf"}},
	}
	for _, tc := range cases {
		got, err := SplitArgs(tc.line)
		if err != nil || !slices.Equal(got, tc.want) {
			t.Errorf("SplitArgs(%q) = %q, %v, want %q", tc.line, got, err, tc.want)
		}
	}
	for _, bad := range []string{`"open`, `'open`, `trailing\`} {
		if _, err := SplitArgs(bad); err == nil {
			t.Errorf("SplitArgs(%q) should fail", bad)
		}
	}
}
