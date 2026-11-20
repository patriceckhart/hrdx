package core

import (
	"context"
	"errors"
	"os"
	"strings"

	"github.com/patriceckhart/hrdx/internal/review"
)

// Read-only Files and Changes panels. The loop only resolves which folder
// the panels show: the focused pane's start folder, or the workspace's.
// Listing, reading, and git run on the caller's goroutine so a large folder
// or slow git never stalls the loop.

// FolderListing is one folder of the Files tree, with change marks keyed by
// path relative to the panel folder.
type FolderListing struct {
	Entries []review.Entry    `json:"entries"`
	Limited bool              `json:"limited,omitempty"`
	Marks   map[string]string `json:"marks,omitempty"`
	Error   string            `json:"error,omitempty"`
}

// ReviewDir returns the folder the panels show for the current focus.
func (c *Core) ReviewDir() (string, error) {
	var dir string
	err := c.do(func() error {
		owner := c.m.currentSpace()
		if owner == nil {
			return errors.New("no workspace")
		}
		dir = owner.cwd
		if focused := c.m.currentPane(); focused != nil && focused.cwd != "" {
			dir = focused.cwd
		}
		return nil
	})
	return dir, err
}

// ReviewList lists rel below the panel folder. Marks come only with the
// top level (rel == "") because they cover the whole tree.
func (c *Core) ReviewList(rel string) FolderListing {
	dir, err := c.ReviewDir()
	if err != nil {
		return FolderListing{Error: err.Error()}
	}
	entries, limited, err := review.ListDir(dir, rel)
	if err != nil {
		return FolderListing{Error: err.Error()}
	}
	listing := FolderListing{Entries: entries, Limited: limited}
	if rel == "" {
		listing.Marks = review.Marks(review.ReadChanges(context.Background(), dir), dir)
	}
	return listing
}

// ReviewPreview returns a highlighted preview of rel below the panel folder.
func (c *Core) ReviewPreview(rel string) review.Preview {
	dir, err := c.ReviewDir()
	if err != nil {
		return review.Preview{Note: err.Error()}
	}
	return review.ReadPreview(dir, rel)
}

// ReviewFilePath resolves rel below the panel folder to an absolute path of
// an existing regular file, for opening it in the system's default app.
// Paths outside the folder, folders, and missing files are refused, so the
// webview can only open what the Files panel can show.
func (c *Core) ReviewFilePath(rel string) (string, error) {
	dir, err := c.ReviewDir()
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(rel) == "" {
		return "", errors.New("no file")
	}
	path, err := review.Resolve(dir, rel)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", errors.New("file not found")
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("not a file")
	}
	return path, nil
}

// ReviewChanges lists the uncommitted changes of the panel folder's
// repository.
func (c *Core) ReviewChanges() review.Changes {
	dir, err := c.ReviewDir()
	if err != nil {
		return review.Changes{Error: err.Error()}
	}
	return review.ReadChanges(context.Background(), dir)
}

// ReviewDiff returns the diff of path, relative to the repository root.
// The path must be one of the current changes, so the panel cannot be used
// to read arbitrary files.
func (c *Core) ReviewDiff(path string) review.Diff {
	changes := c.ReviewChanges()
	if changes.Error != "" {
		return review.Diff{Note: changes.Error}
	}
	for _, change := range changes.Files {
		if change.Path == path {
			return review.ReadDiff(context.Background(), changes.Root, change)
		}
	}
	return review.Diff{Note: "This file has no changes anymore"}
}
