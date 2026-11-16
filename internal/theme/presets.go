package theme

// Preset is a bundled palette adapted from the preset collection in the zot
// theme builder. hrdx maps each palette onto its smaller set of UI roles:
// chrome, status, borders, highlights, and text on accent backgrounds.
type Preset struct {
	Name   string
	Colors Palette
}

func newPreset(name, background, foreground, muted, accent, barBackground, good, bad, busy, faint, alt string) Preset {
	return Preset{
		Name: name,
		Colors: Palette{
			"accent": accent,
			"alt":    alt,
			"muted":  muted,
			"faint":  faint,
			"good":   good,
			"busy":   busy,
			"bad":    bad,
			"bar_bg": barBackground,
			"bar_fg": foreground,
			"ink":    background,
		},
	}
}

// Presets lists the bundled themes in their curated order.
var Presets = []Preset{
	newPreset("violet dusk", "#171322", "#e6e1f2", "#8a7fb0", "#b675f1", "#2b2440", "#7ee0c0", "#ff6b9d", "#f5c97b", "#6b6088", "#f05b8d"),
	newPreset("terminal moss", "#0f1714", "#d6e4d6", "#6f8a72", "#8be28b", "#1a2820", "#9ee493", "#e88a6a", "#d7ba7d", "#5a7a5e", "#d7ba7d"),
	newPreset("ember", "#1a1210", "#f0e0d6", "#9a7a6a", "#ff9e64", "#2e1f1a", "#d7d787", "#ff5f5f", "#ffaf5f", "#7a5f54", "#ff7b72"),
	newPreset("nord ice", "#2e3440", "#e5e9f0", "#7b88a1", "#88c0d0", "#3b4252", "#a3be8c", "#bf616a", "#ebcb8b", "#616e88", "#81a1c1"),
	newPreset("rose pine", "#191724", "#e0def4", "#6e6a86", "#c4a7e7", "#26233a", "#31748f", "#eb6f92", "#f6c177", "#6e6a86", "#c4a7e7"),
	newPreset("mono slate", "#1c1c1c", "#dadada", "#767676", "#bcbcbc", "#303030", "#a8a8a8", "#d75f5f", "#d7af5f", "#5f5f5f", "#bcbcbc"),
	newPreset("dracula", "#282a36", "#f8f8f2", "#6272a4", "#bd93f9", "#343746", "#50fa7b", "#ff5555", "#f1fa8c", "#6272a4", "#ff79c6"),
	newPreset("gruvbox", "#282828", "#ebdbb2", "#928374", "#fabd2f", "#3c3836", "#b8bb26", "#fb4934", "#fe8019", "#928374", "#fb4934"),
	newPreset("tokyo night", "#1a1b26", "#c0caf5", "#565f89", "#7aa2f7", "#24283b", "#9ece6a", "#f7768e", "#e0af68", "#565f89", "#bb9af7"),
	newPreset("catppuccin", "#1e1e2e", "#cdd6f4", "#7f849c", "#cba6f7", "#313244", "#a6e3a1", "#f38ba8", "#f9e2af", "#6c7086", "#cba6f7"),
	newPreset("solarized", "#002b36", "#93a1a1", "#586e75", "#268bd2", "#073642", "#859900", "#dc322f", "#b58900", "#586e75", "#859900"),
	newPreset("miami vice", "#1b1033", "#f5e6ff", "#8a6ab0", "#ff5fd2", "#2c1a4d", "#5fffc7", "#ff4f87", "#ffd75f", "#6b5a8a", "#ff5fd2"),
	newPreset("iron man", "#1a0e0e", "#ffffff", "#9c6b5a", "#e8b53a", "#3a1414", "#ffc24a", "#ff3b30", "#ff8c1a", "#8a5040", "#e23b2e"),
	newPreset("spider-man", "#090d1f", "#f4f7ff", "#6f7fa8", "#e62429", "#24070b", "#44a3ff", "#ff3045", "#ffcf4a", "#5e6b8f", "#ff3045"),
	newPreset("black panther", "#08070d", "#eeeaff", "#77708f", "#9b5cff", "#171123", "#7df9ff", "#ff5f8f", "#d6b8ff", "#5b526d", "#b56cff"),
	newPreset("gotham", "#080a0d", "#d7dde5", "#69717d", "#ffd400", "#171b22", "#ffd400", "#ff5c57", "#ffb000", "#555f6b", "#ffd400"),
	newPreset("matrix", "#020805", "#d6ffd6", "#2f7d4c", "#00ff66", "#06150c", "#00d45a", "#ff4d4d", "#d8ff4d", "#24663d", "#00ff66"),
	newPreset("cyberpunk", "#12071f", "#fff7d6", "#8b6bb0", "#fcee0a", "#26103a", "#00ff9f", "#ff006e", "#fcee0a", "#6d5a8f", "#ff006e"),
	newPreset("tron", "#030914", "#e6fbff", "#416f8f", "#00d9ff", "#071a2a", "#2d7dff", "#ff426d", "#ffb84d", "#315a77", "#00d9ff"),
	newPreset("jedi temple", "#101622", "#efe6d2", "#7f8795", "#48b8ff", "#182437", "#d9b56f", "#ff5f5f", "#d9b56f", "#5d6878", "#48b8ff"),
	newPreset("sith forge", "#110607", "#f7ded8", "#8c5550", "#ff2a2a", "#260a0c", "#ff9f1a", "#ff2a2a", "#ff9f1a", "#74413c", "#ff2a2a"),
	newPreset("wolverine", "#0b0c0f", "#f4e7b0", "#78746a", "#ffd21f", "#1f1b10", "#9fb6c7", "#ff4040", "#ffd21f", "#686868", "#ffd21f"),
	newPreset("cap", "#071226", "#f4f8ff", "#6f829c", "#c41f32", "#111f38", "#d8dde6", "#ff4a5e", "#f4f8ff", "#53677f", "#c41f32"),
	newPreset("dune", "#17100b", "#ead8b8", "#95785b", "#d98b3a", "#2a1c12", "#c8a45d", "#d75f43", "#e2a64a", "#76614c", "#d98b3a"),
	newPreset("laser grid", "#090015", "#f1e8ff", "#755a9b", "#ff2bd6", "#1a0830", "#8a5cff", "#ff3d7f", "#ffd166", "#5a4778", "#ff2bd6"),
	newPreset("toxic waste", "#061006", "#e6ffb8", "#617a35", "#b6ff00", "#14220a", "#d6ff4d", "#ff4d3d", "#ffb000", "#4d642e", "#b6ff00"),
	newPreset("midnight oil", "#071016", "#efe7d0", "#5c7380", "#2f9eb5", "#0d1d26", "#c47c4a", "#d75f5f", "#d99a4e", "#48606b", "#2f9eb5"),
	newPreset("arctic fox", "#0d1620", "#eef7ff", "#6c8192", "#8bdcff", "#162637", "#8cffc1", "#ff6f91", "#ffd98a", "#536a7c", "#8bdcff"),
	newPreset("blood moon", "#12080b", "#f3d7d0", "#7a4b4f", "#c1121f", "#251014", "#d96b2b", "#ff3b3b", "#d96b2b", "#683d40", "#c1121f"),
	newPreset("rainforest", "#06140f", "#d8f3dc", "#5f8069", "#52b788", "#10251b", "#95d5b2", "#ff6b6b", "#ffd166", "#4f6f5a", "#b388eb"),
	newPreset("desert radio", "#1b1410", "#ead7b7", "#8f765f", "#c96f37", "#2a1e17", "#9fd356", "#d95d39", "#f2b84b", "#6f5b49", "#c96f37"),
	newPreset("obsidian", "#07090c", "#dce3ea", "#5d6772", "#5bc0eb", "#111820", "#ff9f1c", "#ff4d6d", "#ff9f1c", "#4c5661", "#5bc0eb"),
	newPreset("neon noir", "#0b0b0f", "#eeeeee", "#6d6d78", "#ff2d95", "#1a1a22", "#00d9ff", "#ff2d55", "#ffd166", "#555560", "#ff2d95"),
	newPreset("plasma storm", "#0b0618", "#f5edff", "#6e5a91", "#7a5cff", "#1a0f33", "#ff4fd8", "#ff3d7f", "#ffd166", "#574577", "#ff4fd8"),
	newPreset("coffee", "#231a15", "#e8d8c8", "#9c8472", "#d8a657", "#33261e", "#a9b665", "#ea6962", "#e78a4e", "#7a6553", "#e78a4e"),
}

func preset(name string) (Preset, bool) {
	for _, current := range Presets {
		if current.Name == name {
			return current, true
		}
	}
	return Preset{}, false
}
