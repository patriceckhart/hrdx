package api

import "strings"

// GroupsFor lists every group and parent group referenced by a status
// snapshot, once each, in first-workspace order.
func GroupsFor(status Status) Groups {
	result := Groups{Type: "groups", Groups: []GroupStatus{}}
	seen := map[string]bool{}
	for _, ws := range status.Workspaces {
		for depth := 1; depth <= len(ws.GroupPath); depth++ {
			path := ws.GroupPath[:depth]
			key := strings.Join(path, "\x00")
			if !seen[key] {
				seen[key] = true
				result.Groups = append(result.Groups, GroupStatus{GroupPath: append([]string(nil), path...)})
			}
		}
	}
	return result
}
