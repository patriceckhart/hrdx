package api

import (
	"fmt"
	"strings"
	"unicode"
)

// MaxMenus caps process-local menu registrations.
const MaxMenus = 64

// RegisterMenu validates registration and returns menus with it added.
// Re-registering an action id replaces its entry without changing menu
// order. menus is never modified in place when an error is returned.
func RegisterMenu(menus []MenuRegister, registration MenuRegister) ([]MenuRegister, error) {
	registration.Target = strings.TrimSpace(registration.Target)
	registration.Label = strings.TrimSpace(registration.Label)
	registration.ActionID = strings.TrimSpace(registration.ActionID)
	if registration.Target != "pane" && registration.Target != "tab" && registration.Target != "sidebar" {
		return menus, fmt.Errorf("target must be pane, tab, or sidebar")
	}
	if registration.Label == "" {
		return menus, fmt.Errorf("label is required")
	}
	if len([]rune(registration.Label)) > 80 || strings.IndexFunc(registration.Label, unicode.IsControl) >= 0 {
		return menus, fmt.Errorf("label must be at most 80 characters and contain no control characters")
	}
	if registration.ActionID == "" {
		return menus, fmt.Errorf("action_id is required")
	}
	if len([]rune(registration.ActionID)) > 128 || strings.IndexFunc(registration.ActionID, unicode.IsControl) >= 0 {
		return menus, fmt.Errorf("action_id must be at most 128 characters and contain no control characters")
	}
	for index, current := range menus {
		if current.ActionID == registration.ActionID {
			menus[index] = registration
			return menus, nil
		}
	}
	if len(menus) >= MaxMenus {
		return menus, fmt.Errorf("at most 64 menu entries may be registered")
	}
	return append(menus, registration), nil
}
