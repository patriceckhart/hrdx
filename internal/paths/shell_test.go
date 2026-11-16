package paths

import (
	"errors"
	"testing"
)

func TestDefaultShellFor(t *testing.T) {
	tests := []struct {
		name       string
		goos       string
		env        map[string]string
		resolvable map[string]bool
		want       string
	}{
		{
			name: "unix preserves SHELL",
			goos: "linux",
			env:  map[string]string{"SHELL": "/opt/homebrew/bin/fish"},
			want: "/opt/homebrew/bin/fish",
		},
		{
			name: "unix without SHELL uses sh",
			goos: "darwin",
			want: "/bin/sh",
		},
		{
			name:       "Windows preserves resolvable SHELL",
			goos:       "windows",
			env:        map[string]string{"SHELL": `C:\Program Files\Git\bin\bash.exe`, "COMSPEC": `C:\Windows\System32\cmd.exe`},
			resolvable: map[string]bool{`C:\Program Files\Git\bin\bash.exe`: true},
			want:       `C:\Program Files\Git\bin\bash.exe`,
		},
		{
			name: "Windows rejects Git Bash MSYS SHELL",
			goos: "windows",
			env:  map[string]string{"SHELL": "/usr/bin/bash", "COMSPEC": `C:\Windows\System32\cmd.exe`},
			want: `C:\Windows\System32\cmd.exe`,
		},
		{
			name: "Windows without usable environment uses PowerShell",
			goos: "windows",
			env:  map[string]string{"SHELL": "/usr/bin/bash"},
			want: "powershell.exe",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			getenv := func(key string) string { return tt.env[key] }
			lookPath := func(file string) (string, error) {
				if tt.resolvable[file] {
					return file, nil
				}
				return "", errors.New("not found")
			}
			if got := DefaultShellFor(tt.goos, getenv, lookPath); got != tt.want {
				t.Fatalf("defaultShellFor() = %q, want %q", got, tt.want)
			}
		})
	}
}
