package ui

// The default split direction decides where the primary prefix split keys
// (c, a, s) put the new pane; their shifted variants (C, A, S) always take
// the other direction. tmux-style keys (% and | right, " and - down) are
// explicit and never follow the setting.

const (
	splitDirRight = ""
	splitDirDown  = "down"
	splitDirAuto  = "auto"
)

var splitDirections = []string{splitDirRight, splitDirDown, splitDirAuto}

var explicitSplitKeys = map[string]bool{"%": true, "|": true, "\"": false, "-": false}

func validSplitDirection(value string) string {
	switch value {
	case splitDirDown, splitDirAuto:
		return value
	}
	return splitDirRight
}

func splitDirectionLabel(value string) string {
	switch value {
	case splitDirDown:
		return "below"
	case splitDirAuto:
		return "auto (longer side)"
	}
	return "right"
}

// prefixSplitVertical resolves a prefix split action to a side-by-side
// (true) or stacked (false) split.
func (m Model) prefixSplitVertical(action, key string) bool {
	if vertical, ok := explicitSplitKeys[key]; ok {
		return vertical
	}
	primary := action == "picker-right" || action == "agent-right" || action == "shell-right"
	preferred := m.preferredSplitVertical()
	if primary {
		return preferred
	}
	return !preferred
}

// preferredSplitVertical is the direction of the primary split keys. Auto
// splits the focused pane along its longer side; terminal cells are about
// twice as tall as wide, so the width counts half.
func (m Model) preferredSplitVertical() bool {
	switch m.splitDirection {
	case splitDirDown:
		return false
	case splitDirAuto:
		if currentSpace := m.currentSpace(); currentSpace != nil {
			focused := m.currentPane()
			for _, pr := range m.allPaneLayout(currentSpace.tab()) {
				if pr.pane == focused && pr.r.w > 0 && pr.r.h > 0 {
					return pr.r.w >= 2*pr.r.h
				}
			}
		}
	}
	return true
}

func (m *Model) cycleSplitDirection() {
	for index, value := range splitDirections {
		if value == m.splitDirection {
			m.splitDirection = splitDirections[(index+1)%len(splitDirections)]
			m.persist()
			return
		}
	}
	m.splitDirection = splitDirRight
	m.persist()
}
