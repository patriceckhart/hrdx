// Command hrdx-desktop is the native window frontend for hrdx. It shares
// state.json, the session holder, and the socket API with the hrdx TUI.
package main

import (
	"context"
	"crypto/sha256"
	"embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/patriceckhart/hrdx/desktop/internal/bridge"
	"github.com/patriceckhart/hrdx/desktop/internal/core"
	"github.com/patriceckhart/hrdx/desktop/internal/holdermode"
	"github.com/patriceckhart/hrdx/desktop/internal/prefs"
	"github.com/patriceckhart/hrdx/desktop/internal/startup"
	"github.com/patriceckhart/hrdx/desktop/internal/updater"
	"github.com/patriceckhart/hrdx/internal/api"
	"github.com/patriceckhart/hrdx/internal/holder"
	"github.com/patriceckhart/hrdx/internal/paths"
	"github.com/patriceckhart/hrdx/internal/sound"
	"github.com/patriceckhart/hrdx/internal/state"
)

//go:embed all:frontend/out
var assets embed.FS

//go:embed build/appicon.png
var appIcon []byte

// Injected at build time with -ldflags "-X main.version=...".
var version = "0.0.0"

func resolvedVersion() string {
	if version != "" && version != "0.0.0" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return strings.TrimPrefix(info.Main.Version, "v")
	}
	return version
}

func main() {
	// Holder mode first: a holder spawned by this binary must never touch
	// the GUI.
	if code, handled := holdermode.Run(os.Args[1:], resolvedVersion(), os.Stderr); handled {
		os.Exit(code)
	}
	flags, err := startup.Parse(os.Args[1:], os.Stderr)
	if err != nil {
		os.Exit(2)
	}
	if flags.Version {
		fmt.Println("hrdx-desktop " + resolvedVersion())
		return
	}
	if err := run(flags); err != nil {
		fmt.Fprintln(os.Stderr, "hrdx-desktop:", err)
		os.Exit(1)
	}
}

// session owns the holder connection and the API server, which may start
// late when the app launched blocked and the user retried.
type session struct {
	mu      sync.Mutex
	flags   startup.Options
	sockets paths.Sockets
	app     *core.Core
	events  *api.Broadcaster
	blocked bool
	server  *api.Server
}

// connect attaches to or spawns the holder. busy reports that another
// frontend holds it.
func (s *session) connect() (launcher core.Launcher, busy bool, problem string) {
	if !s.flags.Persist || s.flags.StatePath == "" {
		return bridge.LocalLauncher{}, false, ""
	}
	client, err := holder.ConnectOrSpawn(s.sockets.Holder, resolvedVersion())
	switch {
	case errors.Is(err, holder.ErrHolderBusy):
		return nil, true, ""
	case err != nil:
		return bridge.LocalLauncher{}, false, "session holder unavailable, panes will not persist: " + err.Error()
	}
	client.SetExitHandler(s.app.HolderExited)
	return bridge.HolderLauncher{Client: client}, false, ""
}

// startAPI serves the socket API once state is owned.
func (s *session) startAPI() {
	if !s.flags.API || s.flags.StatePath == "" {
		return
	}
	server := api.NewServer(s.sockets.API, s.app.Send, s.events)
	if err := server.Start(); err != nil {
		s.app.Problem("api disabled: " + err.Error())
		return
	}
	s.server = server
}

// retry is the blocked dialog's Retry action.
func (s *session) retry() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.blocked {
		return nil
	}
	launcher, busy, problem := s.connect()
	if busy {
		return errors.New("hrdx is still running in another terminal")
	}
	saved, problems := loadState(s.flags)
	if err := s.app.Activate(s.flags.StatePath, saved, launcher, s.flags.CWDs); err != nil {
		launcher.Close()
		return err
	}
	for _, message := range append(problems, problem) {
		if message != "" {
			s.app.Problem(message)
		}
	}
	s.blocked = false
	s.startAPI()
	return nil
}

func (s *session) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.server != nil {
		s.server.Close()
	}
	if s.blocked {
		s.app.Stop()
		return
	}
	s.app.Shutdown()
}

// updateCacheDir holds the shared release-check cache, next to state.json
// like the TUI's.
func updateCacheDir(statePath string) string {
	if statePath != "" {
		return filepath.Dir(statePath)
	}
	dir, _ := os.UserCacheDir()
	return dir
}

func loadState(flags startup.Options) (state.State, []string) {
	if flags.Fresh {
		return state.State{}, nil
	}
	loaded, err := state.Load(flags.StatePath)
	if err != nil {
		return state.State{}, []string{"ignoring unreadable state: " + err.Error()}
	}
	return loaded, nil
}

func run(flags startup.Options) error {
	startup.ApplyLoginPath(flags.Shell)
	desktopPrefs := prefs.Load(prefs.Path(flags.StatePath))

	sockets := paths.Legacy(flags.StatePath)
	if flags.StatePath != "" && (flags.Persist || flags.API) {
		sockets = paths.Select(flags.StatePath, runtime.GOOS, os.Getenv("XDG_RUNTIME_DIR"), paths.ProbeUnixSocket)
	}

	platform := &wailsPlatform{}
	hub := bridge.NewHub(platform.Emit)
	sess := &session{flags: flags, sockets: sockets, events: api.NewBroadcaster()}

	config := core.Config{
		Version: resolvedVersion(), DefaultAgent: flags.Agent, AgentBins: flags.AgentBins,
		ZotArgs: flags.ZotArgs(), Shell: flags.Shell, ShellArgs: startup.ShellArgs(runtime.GOOS),
		StatePath: flags.StatePath, Fresh: flags.Fresh,
	}
	hooks := core.Hooks{
		Emit:     func(view core.View) { platform.Emit("state", view) },
		Output:   hub.Write,
		PaneGone: hub.Drop,
		Notify: func(n core.Notification) {
			platform.Emit("notify", n)
			if title, body, show := core.BannerText(n); show {
				showNotification(title, body, n.PaneID)
			}
		},
		Attention: platform.RequestAttention,
		Badge:     setBadge,
		PlaySound: sound.Play,
	}

	// The core starts empty without a launcher; the holder connection
	// decides whether it becomes active now or after a retry.
	blockedConfig := config
	blockedConfig.StatePath = "" // never write state.json while blocked
	sess.app = core.New(blockedConfig, state.State{}, nil, hooks, sess.events, desktopPrefs, nil)
	go sess.app.Run()
	// Clicking a banner brings the window up and focuses that pane.
	setupNotifications(func(paneID int) {
		platform.Focus()
		_ = sess.app.PaneFocus(paneID)
	})
	go hub.Run()
	defer hub.Stop()

	launcher, busy, problem := sess.connect()
	sess.blocked = busy
	if busy {
		sess.app.SetBlocked("hrdx is already running in another terminal. Quit it first.")
	} else {
		saved, problems := loadState(flags)
		if err := sess.app.Activate(flags.StatePath, saved, launcher, flags.CWDs); err != nil {
			return err
		}
		for _, message := range append(problems, problem) {
			if message != "" {
				sess.app.Problem(message)
			}
		}
		sess.startAPI()
	}

	bound := bridge.NewApp(sess.app, hub, platform, resolvedVersion(), sess.retry)
	bound.SetUpdater(bridge.Updater{
		Check: func(ctx context.Context) updater.Info {
			return updater.Check(ctx, updateCacheDir(flags.StatePath), resolvedVersion())
		},
		Install:  updater.Install,
		Relaunch: updater.Relaunch,
	})

	width, height := 1280, 820
	window := desktopPrefs.Window
	if window != nil {
		width, height = window.Width, window.Height
	}
	startState := options.Normal
	if window != nil && window.Maximized {
		startState = options.Maximised
	}
	lockID := "dev.hrdx.desktop"
	if flags.StatePath != "" {
		if absolute, err := filepath.Abs(flags.StatePath); err == nil {
			sum := sha256.Sum256([]byte(absolute))
			lockID = fmt.Sprintf("dev.hrdx.desktop.%x", sum[:8])
		}
	}

	return wails.Run(&options.App{
		Title:            "hrdx",
		Width:            width,
		Height:           height,
		MinWidth:         480,
		MinHeight:        320,
		WindowStartState: startState,
		BackgroundColour: &options.RGBA{R: 17, G: 17, B: 19, A: 255},
		AssetServer:      &assetserver.Options{Assets: assets},
		// Dropped files resolve to absolute paths; the webview never
		// navigates to them.
		DragAndDrop: &options.DragAndDrop{EnableFileDrop: true, DisableWebViewDrop: true},
		Bind:        []interface{}{bound},
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId:               lockID,
			OnSecondInstanceLaunch: func(options.SecondInstanceData) { platform.Focus() },
		},
		Mac: &mac.Options{
			// Content runs under the transparent title bar; the sidebar and tab
			// bar leave room for the traffic lights and act as drag regions.
			TitleBar:                     mac.TitleBarHiddenInset(),
			About:                        &mac.AboutInfo{Title: "hrdx", Icon: appIcon, Message: "Version " + resolvedVersion()},
			DisableEscapeExitsFullscreen: true,
		},
		OnStartup: func(ctx context.Context) {
			platform.SetContext(ctx)
			if window != nil && !window.Maximized {
				wailsruntime.WindowSetPosition(ctx, window.X, window.Y)
			}
		},
		OnBeforeClose: func(ctx context.Context) bool {
			x, y := wailsruntime.WindowGetPosition(ctx)
			w, h := wailsruntime.WindowGetSize(ctx)
			sess.app.SetWindow(prefs.Window{X: x, Y: y, Width: w, Height: h, Maximized: wailsruntime.WindowIsMaximised(ctx)})
			return false
		},
		OnShutdown: func(context.Context) { sess.close() },
	})
}
