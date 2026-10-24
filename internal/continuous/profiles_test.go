package continuous

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestProfilesRoundTripAndValidation(t *testing.T) {
	dir := t.TempDir()
	if profiles, problems := LoadProfiles(dir); profiles != nil || problems != nil {
		t.Fatalf("missing file = %v %v, want empty", profiles, problems)
	}
	external := Profile{ID: "ext", Address: "/tmp/ext.sock", TokenFile: "/tmp/token"}
	if err := UpsertProfile(dir, external); err != nil {
		t.Fatal(err)
	}
	managed := ManagedProfile(dir, "/work/project")
	if err := UpsertProfile(dir, managed); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, ProfilesFile))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 && runtime.GOOS != "windows" {
		t.Fatalf("profiles file mode = %o, want private", perm)
	}
	profiles, problems := LoadProfiles(dir)
	if len(problems) != 0 || len(profiles) != 2 {
		t.Fatalf("loaded %v problems %v", profiles, problems)
	}
	if err := RemoveProfile(dir, "ext"); err != nil {
		t.Fatal(err)
	}
	if err := RemoveProfile(dir, "ext"); err == nil {
		t.Fatal("removing a missing profile succeeded")
	}
	profiles, _ = LoadProfiles(dir)
	if len(profiles) != 1 || profiles[0].ID != managed.ID || !profiles[0].Managed {
		t.Fatalf("after remove = %+v", profiles)
	}
}

func TestLoadProfilesSkipsInvalidEntries(t *testing.T) {
	dir := t.TempDir()
	data := `{"hosts":[{"id":"ok","address":"/tmp/a.sock"},{"id":"","address":"/x"},{"id":"ok","address":"/tmp/b.sock"},{"id":"m","address":"/x","managed":true}]}`
	if err := os.WriteFile(filepath.Join(dir, ProfilesFile), []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	profiles, problems := LoadProfiles(dir)
	if len(profiles) != 1 || profiles[0].Address != "/tmp/a.sock" {
		t.Fatalf("profiles = %+v", profiles)
	}
	if len(problems) != 3 {
		t.Fatalf("problems = %v, want 3", problems)
	}
}

func TestUpsertRefusesToOverwriteCorruptFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ProfilesFile)
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := UpsertProfile(dir, Profile{ID: "x", Address: "/tmp/x.sock"}); err == nil {
		t.Fatal("upsert overwrote an unreadable profiles file")
	}
	data, _ := os.ReadFile(path)
	if string(data) != "{not json" {
		t.Fatal("corrupt file was modified")
	}
}

func TestManagedProfileIsStablePerProject(t *testing.T) {
	a := ManagedProfile("/state", "/work/a")
	if again := ManagedProfile("/state", "/work/a"); again != a {
		t.Fatal("managed profile is not deterministic")
	}
	if b := ManagedProfile("/state", "/work/b"); b.ID == a.ID || b.Store == a.Store {
		t.Fatal("different projects share a managed store")
	}
	if !strings.HasPrefix(a.Address, a.Store) || a.CWD != "/work/a" {
		t.Fatalf("managed profile = %+v", a)
	}
	// Windows uses named pipes, which have no socket path length limit.
	if err := CheckAddress(strings.Repeat("x", 200)); (err == nil) != (runtime.GOOS == "windows") {
		t.Fatalf("CheckAddress(200 bytes) = %v on %s", err, runtime.GOOS)
	}
}

func TestTokenFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "token")
	if err := os.WriteFile(path, []byte("  synthetic-token-1234567890 admin\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	token, err := Profile{ID: "p", TokenFile: path}.Token()
	if err != nil || token != "synthetic-token-1234567890" {
		t.Fatalf("token = %q, %v", token, err)
	}
	empty := filepath.Join(dir, "empty")
	_ = os.WriteFile(empty, nil, 0o600)
	if _, err := (Profile{ID: "p", TokenFile: empty}).Token(); err == nil {
		t.Fatal("empty token file accepted")
	}
}

func TestStopManagedRefusesExternalAndUnreachable(t *testing.T) {
	if err := StopManaged(Profile{ID: "ext", Address: "/tmp/x.sock"}); err == nil {
		t.Fatal("stopped an external host")
	}
	dir := t.TempDir()
	profile := Profile{ID: "m", Address: filepath.Join(dir, "h.sock"), Managed: true, Store: dir, CWD: dir}
	if err := os.WriteFile(filepath.Join(dir, pidFile), []byte("999999"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Nothing answers: the recorded pid must not be signalled.
	if err := StopManaged(profile); err == nil {
		t.Fatal("stop signalled a host that does not answer")
	}
	if _, err := os.Stat(filepath.Join(dir, pidFile)); !os.IsNotExist(err) {
		t.Fatal("stale pid file kept")
	}
	if IntentionallyStopped(profile) {
		t.Fatal("failed stop recorded an intentional stop")
	}
}
