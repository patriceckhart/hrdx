package bridge

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/patriceckhart/hrdx/desktop/internal/core"
	"github.com/patriceckhart/hrdx/desktop/internal/prefs"
	"github.com/patriceckhart/hrdx/desktop/internal/updater"
	"github.com/patriceckhart/hrdx/internal/state"
)

type fakePlatform struct {
	opened []string
	files  []string
	quits  int
}

func (p *fakePlatform) Emit(string, any)               {}
func (p *fakePlatform) PickDirectory() (string, error) { return "", nil }
func (p *fakePlatform) ClipboardWrite(string) error    { return nil }
func (p *fakePlatform) ClipboardRead() (string, error) { return "", nil }
func (p *fakePlatform) OpenURL(url string)             { p.opened = append(p.opened, url) }
func (p *fakePlatform) OpenFile(path string) error     { p.files = append(p.files, path); return nil }
func (p *fakePlatform) Quit()                          { p.quits++ }
func (p *fakePlatform) AttentionSupported() bool       { return false }
func (p *fakePlatform) RequestAttention()              {}

func newTestApp(t *testing.T) (*App, *Hub, *fakePlatform) {
	t.Helper()
	rec := &recorder{}
	hub := NewHub(rec.emit)
	c := core.New(core.Config{DefaultAgent: "zot", Shell: "/bin/sh", FrameInterval: time.Millisecond},
		state.State{}, nil, core.Hooks{Output: hub.Write}, nil, prefs.Prefs{}, []string{t.TempDir()})
	go c.Run()
	t.Cleanup(c.Stop)
	platform := &fakePlatform{}
	return NewApp(c, hub, platform, "test", nil), hub, platform
}

func TestUpdateInstallRelaunchesThenQuits(t *testing.T) {
	app, _, platform := newTestApp(t)
	var relaunched string
	app.SetUpdater(Updater{
		Check: func(context.Context) updater.Info { return updater.Info{Latest: "9.9.9", Available: true} },
		Install: func(_ context.Context, info updater.Info) (string, error) {
			return "/Applications/hrdx.app", nil
		},
		Relaunch: func(target string) error { relaunched = target; return nil },
	})
	if err := app.UpdateInstall(); err != nil {
		t.Fatal(err)
	}
	if relaunched != "/Applications/hrdx.app" || platform.quits != 1 {
		t.Fatalf("relaunched %q, quits %d", relaunched, platform.quits)
	}
}

func TestUpdateInstallFailureKeepsRunning(t *testing.T) {
	app, _, platform := newTestApp(t)
	relaunched := false
	app.SetUpdater(Updater{
		Check:    func(context.Context) updater.Info { return updater.Info{Latest: "9.9.9", Available: true} },
		Install:  func(context.Context, updater.Info) (string, error) { return "", errors.New("checksum mismatch") },
		Relaunch: func(string) error { relaunched = true; return nil },
	})
	if err := app.UpdateInstall(); err == nil {
		t.Fatal("want error")
	}
	if relaunched || platform.quits != 0 {
		t.Fatal("failed install must not restart")
	}
	app.SetUpdater(Updater{
		Check: func(context.Context) updater.Info { return updater.Info{} },
		Install: func(context.Context, updater.Info) (string, error) {
			t.Fatal("installed without update")
			return "", nil
		},
		Relaunch: func(string) error { return nil },
	})
	if err := app.UpdateInstall(); err == nil {
		t.Fatal("up to date should report an error")
	}
}

func TestUpdateWithoutUpdater(t *testing.T) {
	app, _, _ := newTestApp(t)
	if info := app.UpdateCheck(); info.Available {
		t.Fatal("no updater should report no update")
	}
	if err := app.UpdateInstall(); err == nil {
		t.Fatal("want error")
	}
}

func TestOpenExternalAllowsOnlyWebSchemes(t *testing.T) {
	app, _, platform := newTestApp(t)
	for _, bad := range []string{"file:///etc/passwd", "javascript:alert(1)", "https://", "hrdx://x", "not a url"} {
		if err := app.OpenExternal(bad); err == nil {
			t.Fatalf("%q accepted", bad)
		}
	}
	if err := app.OpenExternal("https://example.com/a?b=c"); err != nil {
		t.Fatal(err)
	}
	if len(platform.opened) != 1 {
		t.Fatalf("opened = %v", platform.opened)
	}
}

func TestPaneAttachIncludesReplay(t *testing.T) {
	app, hub, _ := newTestApp(t)
	view, err := app.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	id := view.Workspaces[0].Tabs[0].Panes[0].ID
	hub.Write(id, []byte("prompt$ "))
	got, err := app.PaneAttach(id)
	if err != nil {
		t.Fatal(err)
	}
	replay, _ := base64.StdEncoding.DecodeString(got.Replay)
	if string(replay) != "prompt$ " || got.Seq != 1 {
		t.Fatalf("attach = %+v", got)
	}
	if _, err := app.PaneAttach(9999); err == nil {
		t.Fatal("unknown pane attached")
	}
}

func TestCompletePathNeverNil(t *testing.T) {
	app, _, _ := newTestApp(t)
	if got := app.CompletePath("/definitely/missing/x"); got == nil {
		t.Fatal("nil completions serialize as null")
	}
}

func TestReviewOpenHandsOnlyPanelFilesToTheSystem(t *testing.T) {
	app, _, platform := newTestApp(t)
	dir, err := app.ReviewDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := app.ReviewOpen("notes.txt"); err != nil {
		t.Fatal(err)
	}
	if len(platform.files) != 1 || platform.files[0] != filepath.Join(dir, "notes.txt") {
		t.Fatalf("opened = %q", platform.files)
	}
	for _, bad := range []string{"../outside.txt", "/etc/hosts", "missing.txt", ""} {
		if err := app.ReviewOpen(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if len(platform.files) != 1 {
		t.Fatalf("a refused path reached the system: %q", platform.files)
	}
}
