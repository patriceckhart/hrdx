//go:build !windows

package main

import tea "github.com/charmbracelet/bubbletea"

// platformInputOptions keeps Bubble Tea's default stdin reader on Unix,
// which already understands bracketed paste.
func platformInputOptions() []tea.ProgramOption { return nil }

// watchPlatformResize is a no-op on Unix; Bubble Tea handles SIGWINCH.
func watchPlatformResize(*tea.Program) {}
