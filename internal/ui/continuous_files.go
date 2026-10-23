package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/patriceckhart/hrdx/internal/continuous"
)

// readAttachments reads local files for a durable submission. Relative
// paths resolve against base (the workspace directory). Directories are
// refused: attachments never upload trees. Errors name the path the user
// gave, never file contents.
func readAttachments(base string, paths []string) ([]continuous.FileAttachment, error) {
	var files []continuous.FileAttachment
	total := 0
	for _, raw := range paths {
		path := strings.TrimSpace(raw)
		if path == "" {
			continue
		}
		if path == "~" || strings.HasPrefix(path, "~/") {
			if home, err := os.UserHomeDir(); err == nil {
				path = filepath.Join(home, strings.TrimPrefix(path, "~"))
			}
		}
		if !filepath.IsAbs(path) {
			path = filepath.Join(base, path)
		}
		info, err := os.Stat(path)
		if err != nil {
			return nil, fmt.Errorf("attachment %s: %w", raw, innerError(err))
		}
		if info.IsDir() {
			return nil, fmt.Errorf("attachment %s is a directory", raw)
		}
		total += int(info.Size())
		if total > continuous.MaxAttachmentBytes {
			return nil, fmt.Errorf("attachments exceed %d bytes", continuous.MaxAttachmentBytes)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("attachment %s: %w", raw, innerError(err))
		}
		files = append(files, continuous.FileAttachment{Name: filepath.Base(path), Data: data})
	}
	return files, continuous.CheckAttachments(files)
}

// innerError drops the resolved path from a PathError, keeping the cause.
func innerError(err error) error {
	if inner := errors.Unwrap(err); inner != nil {
		return inner
	}
	return err
}

// splitPromptFiles extracts @path tokens from a native prompt. "@@" escapes
// a literal @ at the start of a word. Only tokens naming an existing file
// count, so ordinary text such as email addresses stays in the prompt.
func splitPromptFiles(base, prompt string) (string, []string) {
	var kept, paths []string
	for _, word := range strings.Split(prompt, " ") {
		if strings.HasPrefix(word, "@@") {
			kept = append(kept, word[1:])
			continue
		}
		if candidate, ok := strings.CutPrefix(word, "@"); ok && candidate != "" {
			path := candidate
			if !filepath.IsAbs(path) && !strings.HasPrefix(path, "~") {
				path = filepath.Join(base, path)
			}
			if strings.HasPrefix(candidate, "~") {
				if home, err := os.UserHomeDir(); err == nil {
					path = filepath.Join(home, strings.TrimPrefix(candidate, "~"))
				}
			}
			if info, err := os.Stat(path); err == nil && !info.IsDir() {
				paths = append(paths, candidate)
				continue
			}
		}
		kept = append(kept, word)
	}
	return strings.TrimSpace(strings.Join(kept, " ")), paths
}

// conversationFlagSupport caches, per zot binary, whether its attached view
// accepts --continuous-conversation. Tests replace probeConversationFlag.
var (
	conversationFlagMu      sync.Mutex
	conversationFlagSupport = map[string]bool{}
	probeConversationFlag   = func(binary string) bool {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		out, _ := exec.CommandContext(ctx, binary, "--help").CombinedOutput()
		return strings.Contains(string(out), "--continuous-conversation")
	}
)

func zotSupportsConversationFlag(binary string) bool {
	conversationFlagMu.Lock()
	defer conversationFlagMu.Unlock()
	supported, ok := conversationFlagSupport[binary]
	if !ok {
		supported = probeConversationFlag(binary)
		conversationFlagSupport[binary] = supported
	}
	return supported
}

// submitWithFiles checks the host's attachment capability before sending
// files, so an older host never silently drops them.
func submitWithFiles(ctx context.Context, host continuous.Host, conv, content, requestID, policy string, files []continuous.FileAttachment) (continuous.Submission, error) {
	if len(files) > 0 {
		status, err := host.Status(ctx)
		if err != nil {
			return continuous.Submission{}, err
		}
		if !status.Caps["attachments"] {
			return continuous.Submission{}, fmt.Errorf("this host does not accept attachments: %w", continuous.ErrUnsupported)
		}
	}
	return host.SubmitFiles(ctx, conv, content, requestID, policy, files)
}
