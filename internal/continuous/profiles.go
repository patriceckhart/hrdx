package continuous

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// ProfilesFile holds host connection profiles next to the hrdx state file.
// It is separate from workspace snapshots so host definitions, credentials
// references, and layouts have independent lifetimes.
const ProfilesFile = "continuous-hosts.json"

// Profile describes how hrdx reaches one continuous host. Secrets are never
// stored here; TokenFile only references a file the user controls.
type Profile struct {
	ID        string `json:"id"`
	Name      string `json:"name,omitempty"`
	Address   string `json:"address"`
	Managed   bool   `json:"managed,omitempty"`
	Store     string `json:"store,omitempty"` // managed only: store directory
	CWD       string `json:"cwd,omitempty"`   // managed only: host working directory
	TokenFile string `json:"token_file,omitempty"`
	// TLSCA is a PEM CA bundle for remote host:port profiles. Remote hosts
	// require TLS and a token; plaintext TCP is accepted only on loopback.
	TLSCA string `json:"tls_ca,omitempty"`
}

// Validate checks the fields every profile needs.
func (p Profile) Validate() error {
	if strings.TrimSpace(p.ID) == "" {
		return errors.New("profile id is required")
	}
	if strings.ContainsAny(p.ID, " \t\r\n/\\") {
		return fmt.Errorf("profile id %q must not contain spaces or slashes", p.ID)
	}
	if strings.TrimSpace(p.Address) == "" {
		return fmt.Errorf("profile %s: address is required", p.ID)
	}
	if p.Managed && (p.Store == "" || p.CWD == "") {
		return fmt.Errorf("profile %s: managed hosts need store and cwd", p.ID)
	}
	if IsRemoteAddress(p.Address) {
		if p.Managed {
			return fmt.Errorf("profile %s: managed hosts are local", p.ID)
		}
		if !IsLoopbackAddress(p.Address) && (p.TLSCA == "" || p.TokenFile == "") {
			return fmt.Errorf("profile %s: remote hosts need tls_ca and token_file", p.ID)
		}
	}
	return nil
}

// Token reads the profile's token file. An empty result means the host uses
// local admin access without a token.
func (p Profile) Token() (string, error) {
	if p.TokenFile == "" {
		return "", nil
	}
	data, err := os.ReadFile(p.TokenFile)
	if err != nil {
		return "", fmt.Errorf("profile %s: read token file: %w", p.ID, err)
	}
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return "", fmt.Errorf("profile %s: token file is empty", p.ID)
	}
	return fields[0], nil
}

// LoadProfiles reads the profile file in dir. A missing file is empty.
// Invalid entries are skipped and reported, never fatal, so one broken
// profile cannot prevent hrdx from starting.
func LoadProfiles(dir string) ([]Profile, []string) {
	if dir == "" {
		return nil, nil
	}
	data, err := os.ReadFile(filepath.Join(dir, ProfilesFile))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, []string{ProfilesFile + ": " + err.Error()}
	}
	var raw struct {
		Hosts []Profile `json:"hosts"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, []string{ProfilesFile + ": " + err.Error()}
	}
	var problems []string
	seen := map[string]bool{}
	var out []Profile
	for _, profile := range raw.Hosts {
		if err := profile.Validate(); err != nil {
			problems = append(problems, ProfilesFile+": "+err.Error())
			continue
		}
		if seen[profile.ID] {
			problems = append(problems, ProfilesFile+": duplicate id "+profile.ID)
			continue
		}
		seen[profile.ID] = true
		out = append(out, profile)
	}
	return out, problems
}

// SaveProfiles writes profiles atomically with private permissions.
func SaveProfiles(dir string, profiles []Profile) error {
	if dir == "" {
		return errors.New("profiles need a state directory")
	}
	sorted := append([]Profile(nil), profiles...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })
	data, err := json.MarshalIndent(struct {
		Hosts []Profile `json:"hosts"`
	}{sorted}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, ProfilesFile)
	temp := path + ".tmp"
	if err := os.WriteFile(temp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(temp, path)
}

// UpsertProfile replaces or appends one profile in dir's file.
func UpsertProfile(dir string, profile Profile) error {
	if err := profile.Validate(); err != nil {
		return err
	}
	profiles, problems := LoadProfiles(dir)
	if len(problems) > 0 && len(profiles) == 0 {
		if _, err := os.Stat(filepath.Join(dir, ProfilesFile)); err == nil {
			// Refuse to overwrite an unreadable file and lose the user's hosts.
			return errors.New(problems[0])
		}
	}
	replaced := false
	for index := range profiles {
		if profiles[index].ID == profile.ID {
			profiles[index] = profile
			replaced = true
		}
	}
	if !replaced {
		profiles = append(profiles, profile)
	}
	return SaveProfiles(dir, profiles)
}

// RemoveProfile deletes a profile definition. It never touches the host,
// its store, or conversation history.
func RemoveProfile(dir, id string) error {
	profiles, problems := LoadProfiles(dir)
	if len(problems) > 0 && len(profiles) == 0 {
		return errors.New(problems[0])
	}
	kept := profiles[:0]
	found := false
	for _, profile := range profiles {
		if profile.ID == id {
			found = true
			continue
		}
		kept = append(kept, profile)
	}
	if !found {
		return fmt.Errorf("no host profile %q", id)
	}
	return SaveProfiles(dir, kept)
}

// ManagedProfile returns the default managed profile for a project directory:
// one private store per project, below the hrdx state directory.
func ManagedProfile(stateDir, cwd string) Profile {
	sum := sha256.Sum256([]byte(cwd))
	id := "local-" + hex.EncodeToString(sum[:])[:12]
	store := filepath.Join(stateDir, "continuous", id)
	return Profile{
		ID:      id,
		Name:    filepath.Base(cwd),
		Address: filepath.Join(store, "host.sock"),
		Managed: true,
		Store:   store,
		CWD:     cwd,
	}
}

// maxSocketPath is the conservative portable limit for Unix socket paths
// (104 bytes on macOS including the terminator).
const maxSocketPath = 103

// CheckAddress reports a local socket path that the platform cannot bind.
func CheckAddress(address string) error {
	if runtime.GOOS == "windows" {
		return nil // named pipes, no socket path limit
	}
	if len(address) > maxSocketPath {
		return fmt.Errorf("socket path is %d bytes, longer than the %d byte limit; move the hrdx state directory or use an external host profile", len(address), maxSocketPath)
	}
	return nil
}
