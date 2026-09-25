package plugin

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/patriceckhart/hrdx/internal/state"
)

// SupportedGrants is intentionally smaller than the proposal. An unknown grant
// cannot accidentally become authority when a newer plugin requests it.
func SupportedGrants() []string {
	return []string{"ui.action.contribute", "ui.notification", "ui.status.contribute", "ui.provider.contribute", "workspace.read", "pane.read_metadata", "pane.read_screen", "pane.create", "pane.close", "pane.send_input", "workspace.create", "workspace.close", "workspace.move", "storage.plugin_private", "host.events.subscribe", "ui.view.contribute", "ui.view.input"}
}

func ApprovalPath(base, id string) string {
	if base == "" || !validID(id) {
		return ""
	}
	return filepath.Join(base, "plugin-approvals", id+".json")
}

// InspectPackage inspects an explicit package without scanning its siblings.
func InspectPackage(path string) Package {
	canonical, err := filepath.Abs(path)
	if err == nil {
		canonical, err = filepath.EvalSymlinks(canonical)
	}
	if err != nil {
		return Package{Path: path, Diagnostics: []Problem{*problem("package_unreadable", "cannot resolve package directory")}}
	}
	return inspect(canonical)
}

// Fingerprint binds approval to all regular package files, including scripts
// and assets. Mutable plugin data must live outside the package. This detects
// ordinary upgrades, not adversarial same-user filesystem races or dependencies
// imported from outside the package. It is not a sandbox or a signature.
func Fingerprint(path string) (string, error) {
	root, err := os.OpenRoot(path)
	if err != nil {
		return "", problem("package_unreadable", "cannot open package")
	}
	defer root.Close()
	hash := sha256.New()
	remaining := int64(128 << 20)
	count := 1
	var walk func(string, int) error
	walk = func(directory string, depth int) error {
		if depth > 32 {
			return fmt.Errorf("package exceeds 32 directory levels")
		}
		dir, err := root.Open(directory)
		if err != nil {
			return fmt.Errorf("cannot read package directory")
		}
		entries, readErr := dir.ReadDir(max(1, 1025-count))
		dir.Close()
		if (readErr != nil && readErr != io.EOF) || count+len(entries) > 1024 {
			return fmt.Errorf("package cannot be inspected or exceeds 1024 entries")
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
		for _, entry := range entries {
			count++
			if count > 1024 {
				return fmt.Errorf("package exceeds 1024 entries")
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("runtime approval does not allow package symlinks")
			}
			name := filepath.Join(directory, entry.Name())
			if entry.IsDir() {
				if err := walk(name, depth+1); err != nil {
					return err
				}
				continue
			}
			info, err := entry.Info()
			if err != nil || !info.Mode().IsRegular() {
				return fmt.Errorf("package contains an unreadable or non-regular file")
			}
			if info.Size() > remaining {
				return fmt.Errorf("package exceeds 128 MiB")
			}
			portable := filepath.ToSlash(name)
			fmt.Fprintf(hash, "%d:%s:%d:%d:", len(portable), portable, info.Mode().Perm(), info.Size())
			file, err := root.Open(name)
			if err != nil {
				return fmt.Errorf("cannot read package file")
			}
			n, copyErr := io.Copy(hash, io.LimitReader(file, remaining+1))
			file.Close()
			remaining -= n
			if copyErr != nil || remaining < 0 || n != info.Size() {
				return fmt.Errorf("package changed or could not be read")
			}
		}
		return nil
	}
	err = walk(".", 0)
	if err != nil {
		return "", problem("invalid_package", err.Error())
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func Approve(entry Package, grants, workspaces []string, instance bool, config ...map[string]any) (state.PluginApproval, error) {
	if !entry.Valid || entry.Manifest == nil {
		return state.PluginApproval{}, problem("invalid_package", "package must pass validation")
	}
	approval := state.PluginApproval{Schema: 1, ID: entry.ID, Path: entry.Path, Enabled: true, Instance: instance}
	if len(config) > 0 && len(config[0]) > 0 {
		approval.Config = make(map[string]any)
		for key, value := range config[0] {
			option, ok := configOption(entry.Manifest, key)
			if !ok || !validConfigValue(option.Type, value) {
				return state.PluginApproval{}, problem("invalid_config", "configuration keys must be declared by the manifest with matching types")
			}
			approval.Config[key] = value
		}
	}
	for _, grant := range grants {
		if !slices.Contains(SupportedGrants(), grant) || !slices.Contains(entry.Manifest.Requests, grant) || slices.Contains(approval.Grants, grant) {
			return state.PluginApproval{}, problem("invalid_grant", "grants must be unique, supported, and requested by the manifest")
		}
		approval.Grants = append(approval.Grants, grant)
	}
	if instance && len(workspaces) > 0 {
		return state.PluginApproval{}, problem("invalid_scope", "choose instance scope or explicit workspaces, not both")
	}
	for _, path := range workspaces {
		// Scope is the workspace's public absolute path, not an alternate
		// filesystem spelling. hrdx intentionally preserves symlinks in CWD
		// for holder compatibility, so resolving them here would deny valid
		// workspaces (notably macOS /var versus /private/var).
		absolute, err := filepath.Abs(path)
		if err != nil || path == "" {
			return state.PluginApproval{}, problem("invalid_scope", "cannot resolve approved workspace")
		}
		info, err := os.Stat(absolute)
		if err != nil || !info.IsDir() {
			return state.PluginApproval{}, problem("invalid_scope", "workspace must be an existing directory")
		}
		if !slices.Contains(approval.Workspaces, absolute) {
			approval.Workspaces = append(approval.Workspaces, absolute)
		}
	}
	var err error
	approval.Digest, err = Fingerprint(entry.Path)
	return approval, err
}

func VerifyApproval(entry Package, approval state.PluginApproval) error {
	if !entry.Valid || entry.Manifest == nil || !approval.Enabled || approval.Schema != 1 || approval.ID != entry.ID || approval.Path != entry.Path || len(approval.Digest) != 64 {
		return problem("not_approved", "package is disabled or not approved at this path")
	}
	for _, grant := range approval.Grants {
		if !slices.Contains(SupportedGrants(), grant) || !slices.Contains(entry.Manifest.Requests, grant) {
			return problem("not_approved", "package grants need review")
		}
	}
	for key, value := range approval.Config {
		option, ok := configOption(entry.Manifest, key)
		if !ok || !validConfigValue(option.Type, value) {
			return problem("not_approved", "configuration no longer matches the manifest, reapprove the package")
		}
	}
	digest, err := Fingerprint(entry.Path)
	if err != nil || digest != approval.Digest {
		return problem("package_changed", "package contents changed, explicit reapproval is required")
	}
	return nil
}

func configOption(manifest *Manifest, key string) (ConfigOption, bool) {
	for _, option := range manifest.Config {
		if option.Key == key {
			return option, true
		}
	}
	return ConfigOption{}, false
}

// EffectiveConfig merges manifest defaults with approved values. The result is
// what the peer receives in hello_ack and what plugins.status reports.
func EffectiveConfig(manifest *Manifest, approval state.PluginApproval) map[string]any {
	if manifest == nil || len(manifest.Config) == 0 {
		return nil
	}
	values := make(map[string]any, len(manifest.Config))
	for _, option := range manifest.Config {
		value := option.Default
		if approved, ok := approval.Config[option.Key]; ok && validConfigValue(option.Type, approved) {
			value = approved
		}
		if value == nil {
			switch option.Type {
			case "bool":
				value = false
			case "string":
				value = ""
			case "int":
				value = 0
			}
		}
		values[option.Key] = value
	}
	return values
}

// ActivatesFor reports whether a plugin declares interest in a workspace. It
// performs one Lstat per marker and no recursive scan, executable probe, or
// symlink evaluation. A plugin without markers is relevant everywhere.
func ActivatesFor(manifest *Manifest, workspace string) bool {
	if manifest == nil {
		return false
	}
	if len(manifest.Activation.Markers) == 0 {
		return true
	}
	for _, marker := range manifest.Activation.Markers {
		if _, err := os.Lstat(filepath.Join(workspace, filepath.FromSlash(marker))); err == nil {
			return true
		}
	}
	return false
}

// LoadRegistrations reads only explicit host approvals, never project discovery.
// Invalid and changed packages are isolated and reported rather than started.
func LoadRegistrations(base string) ([]Registration, []Diagnostic) {
	var registrations []Registration
	var diagnostics []Diagnostic
	if base == "" {
		return registrations, diagnostics
	}
	directory := filepath.Join(base, "plugin-approvals")
	entries, err := rootEntries(directory)
	if os.IsNotExist(err) {
		return registrations, diagnostics
	}
	if err != nil || len(entries) > 64 {
		return nil, []Diagnostic{{Path: directory, Problem: Problem{Code: "approval_unreadable", Message: "cannot read approval directory or it exceeds 64 entries"}}}
	}
	for _, file := range entries {
		id := strings.TrimSuffix(file.Name(), ".json")
		if !strings.HasSuffix(file.Name(), ".json") || !validID(id) {
			continue
		}
		path := ApprovalPath(base, id)
		approval, err := state.LoadPluginApproval(path)
		if err != nil || approval.ID != id {
			diagnostics = append(diagnostics, Diagnostic{Path: path, Problem: Problem{Code: "invalid_approval", Message: "cannot load plugin approval"}})
			continue
		}
		if !approval.Enabled {
			continue
		}
		entry := InspectPackage(approval.Path)
		if err := VerifyApproval(entry, approval); err != nil {
			diagnostics = append(diagnostics, Diagnostic{Path: path, Problem: Problem{Code: "blocked", Message: err.Error()}})
			continue
		}
		registrations = append(registrations, Registration{Package: entry, Approval: approval})
	}
	return registrations, diagnostics
}

// AllowsWorkspace compares public workspace paths without filesystem I/O on
// the UI loop. Scopes are exact absolute paths, not filesystem aliases or
// directory prefixes. Use the spelling shown in the control API status.
func AllowsWorkspace(approval state.PluginApproval, path string) bool {
	if approval.Instance {
		return true
	}
	return slices.Contains(approval.Workspaces, filepath.Clean(path))
}

func HasGrant(approval state.PluginApproval, grant string) bool {
	return approval.Enabled && slices.Contains(approval.Grants, grant)
}

// SafeText is for host-rendered contributions, never terminal input payloads.
func SafeText(text string, limit int) bool {
	return plainText(text, limit, true) && strings.TrimSpace(text) == text
}
