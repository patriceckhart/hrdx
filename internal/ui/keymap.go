package ui

import "github.com/patriceckhart/hrdx/internal/keymap"

const keymapFile = keymap.File

var defaultPrefixKeys = keymap.Defaults

func buildPrefixKeys(overrides map[string]string) map[string]string {
	return keymap.BuildPrefixKeys(overrides)
}

func loadKeymap(dir string) (map[string]string, string) { return keymap.Load(dir) }

func buildNavigationKeys(overrides map[string]string) map[string]string {
	return keymap.BuildNavigationKeys(overrides)
}
