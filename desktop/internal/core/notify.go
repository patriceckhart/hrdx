package core

// BannerText returns the title and body of a native notification, and
// whether one should be shown at all. Banners appear only for panes the user
// was not looking at and only when the notification setting is on.
func BannerText(n Notification) (title, body string, show bool) {
	if !n.Banner || !n.Unseen {
		return "", "", false
	}
	title = n.Workspace
	if title == "" {
		title = "hrdx"
	}
	switch n.Kind {
	case "attention":
		body = n.Pane + " needs your input"
	default:
		body = n.Pane + " finished"
	}
	return title, body, true
}
