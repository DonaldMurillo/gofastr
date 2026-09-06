// Command desktop-focus is the desktop battery's feature-complete
// dogfood app: a local-first pomodoro timer that exercises the whole
// battery/desktop surface (hidden-title main window, floating widget,
// settings window, tray countdown, notifications, deep links,
// cross-window messages, a plugin capability, the updater) and runs
// unchanged over plain HTTP behind --serve / --addr / $PORT.
//
//	go run ./examples/desktop-focus              # opens the window
//	go run ./examples/desktop-focus --serve :8080
//	gofastr desktop run   --pkg ./examples/desktop-focus
//	gofastr desktop build --id dev.gofastr.desktop-focus --name Focus --scheme gofastr-focus --pkg ./examples/desktop-focus
package main

import (
	"context"
	_ "embed"
	"errors"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"net/url"
	"os"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/framework"
	fwimage "github.com/DonaldMurillo/gofastr/framework/image"
	"github.com/DonaldMurillo/gofastr/framework/isolation"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// pageJS is the page-side behaviour (countdown, timer buttons, widget
// controls, toasts). Embedded, not served from a cwd-relative static
// dir: a bundle launched from Finder runs with cwd "/", and `go run`
// from the repo root would not find examples/desktop-focus/static
// either.
//
//go:embed static/desktop-focus.js
var pageJS []byte

// appID is the reverse-DNS identity: it names the data directory under
// the OS user config dir and the bundle identifier of a built .app.
const appID = "dev.gofastr.desktop-focus"

var serveFlag = flag.String("serve", "", "serve the same app over HTTP at this address instead of opening a desktop window")

// addrFlag is what `gofastr dev` passes to every child (`--addr`), so
// the dev loop and its example sweep can drive this app over HTTP the
// same way --serve does.
var addrFlag = flag.String("addr", "", "alias of --serve, the flag gofastr dev passes to the app it launches")

func main() {
	flag.Parse()

	app, d, _, err := buildApp(nil)
	if err != nil {
		log.Fatal(err)
	}

	// --serve, --addr (what `gofastr dev` passes), or $PORT (what PaaS
	// runtimes inject) runs the identical app over HTTP: the
	// host-independence proof. The battery stays registered; its boot
	// gate is armed only by Run, so an HTTP-served instance never needs
	// the window handshake (and the focus capability is unreachable
	// there: the chokepoint answers 404 until Run freezes the registry).
	addr := *serveFlag
	if addr == "" {
		addr = *addrFlag
	}
	if addr == "" {
		addr = os.Getenv("PORT")
	}
	if addr != "" {
		resolved, err := isolation.ListenAddr(".", addr)
		if err != nil {
			log.Fatal(err)
		}
		if err := app.Start(resolved); err != nil {
			log.Fatal(err)
		}
		return
	}
	if err := d.Run(app); err != nil {
		log.Fatal(err)
	}
}

// buildApp assembles the app. shell is nil in production (the OS
// shell); tests pass battery/desktop/desktoptest's double. The error
// path covers AppOptions, which opens the data dir and database before
// NewApp runs.
func buildApp(shell desktop.Shell) (*framework.App, *desktop.Battery, *Engine, error) {
	opts, err := desktop.AppOptions(appID)
	if err != nil {
		return nil, nil, nil, err
	}
	app := framework.NewApp(append(opts,
		framework.WithConfig(framework.AppConfig{Name: "desktop-focus", APIPrefix: "api"}),
	)...)

	// The data half: the owner-scoped entities.
	registerTasksEntity(app)
	registerSessionsEntity(app)

	// The engine's clock. Tests replace it before any call.
	eng := &Engine{app: app, now: time.Now}

	// The desktop half comes before the screens: the settings screen
	// is PreferencesScreen(d), so the battery must exist when
	// buildSite mounts it. The menu and tray handlers capture d and
	// eng; both only run after Run.
	var d *desktop.Battery
	d = desktop.New(desktop.Config{
		ID:    appID,
		Title: "Focus",
		Width: 1000,
		Shell: shell,
		// Hidden title bar: the page paints under it, the traffic
		// lights stay.
		Style: desktop.WindowStyle{Chrome: desktop.ChromeHiddenTitle},
		// The settings window: the app menu's own item (cmd+,), the
		// File menu's row, the tray's row, and the page's
		// windows.openSettings all reach it. The screen it opens is
		// the battery's own preferences form (see buildSite).
		Settings: &desktop.WindowSpec{Path: "/settings", Title: "Settings", Width: 480, Height: 600},
		// The app's settings, declared once: stored in the app state
		// under "settings", read by the engine through
		// d.Preferences(), and rendered by desktop.PreferencesScreen
		// at /settings. The sound preference is the choice-kind demo;
		// nothing plays it.
		Preferences: []desktop.Preference{
			{Key: "work_minutes", Label: "Work minutes", Help: "Length of one work session.", Kind: desktop.PreferenceInt, Default: 25, Min: intPtr(1), Max: intPtr(180)},
			{Key: "break_minutes", Label: "Break minutes", Kind: desktop.PreferenceInt, Default: 5, Min: intPtr(1), Max: intPtr(60)},
			{Key: "notify_on_done", Label: "Notify when a session ends", Kind: desktop.PreferenceBool, Default: true},
			{Key: "tray_countdown", Label: "Countdown in the menu bar", Kind: desktop.PreferenceBool, Default: true},
			{Key: "sound", Label: "Session sound", Kind: desktop.PreferenceChoice, Default: "chime", Choices: []string{"none", "chime", "bell"}},
		},
		// gofastr-focus://start?task=<id> starts that task; anything
		// else maps like the default (host+path, query kept).
		DeepLink: &desktop.DeepLinkConfig{
			Scheme: "gofastr-focus",
			OnDeepLink: func(u *url.URL) (string, bool) {
				return onDeepLink(u, eng, d)
			},
		},
		Tray: &desktop.Tray{
			// A title next to the icon: macOS places a new status item
			// at the LEFT of the existing ones and hides whatever no
			// longer fits, so an icon-only item disappears on a full
			// menu bar. The title doubles as the countdown the engine
			// writes while a session runs.
			Title:   "Focus",
			Tooltip: "Focus timer",
			Icon:    appIconPNG(),
			Menu: &desktop.Menu{Items: []desktop.MenuItem{
				{Title: "Show Focus", Role: desktop.RoleShow},
				{Title: "Start / Pause", Handler: func(ctx context.Context) error {
					return toggleTimer(ctx, eng)
				}},
				{Title: "Timer widget", Handler: func(ctx context.Context) error {
					spec := desktop.Widget("/widget", 320, 300)
					spec.Style.AllSpaces = true
					if _, err := d.OpenWindow(spec); err != nil {
						return fmt.Errorf("open timer widget: %w", err)
					}
					return nil
				}},
				{Title: "Settings…", Role: desktop.RoleSettings},
				{Role: desktop.RoleSeparator},
				{Role: desktop.RoleQuit},
			}},
			CloseHidesWindow: true,
		},
		Menu: &desktop.Menu{Items: []desktop.MenuItem{
			{Title: "File", Children: []desktop.MenuItem{
				{Title: "New task", Key: "cmd+n", Navigate: "/tasks/new"},
				{Title: "Start / Pause", Key: "cmd+shift+s", Handler: func(ctx context.Context) error {
					return toggleTimer(ctx, eng)
				}},
				{Title: "Settings…", Role: desktop.RoleSettings},
				{Role: desktop.RoleSeparator},
				{Title: "Check for updates…", Handler: func(ctx context.Context) error {
					return checkForUpdates(ctx, d)
				}},
				{Role: desktop.RoleQuit},
			}},
			{Title: "View", Children: []desktop.MenuItem{
				{Title: "Timer widget", Key: "cmd+t", Handler: func(ctx context.Context) error {
					spec := desktop.Widget("/widget", 320, 300)
					spec.Style.AllSpaces = true
					if _, err := d.OpenWindow(spec); err != nil {
						return fmt.Errorf("open timer widget: %w", err)
					}
					return nil
				}},
				{Title: "History", Navigate: "/history"},
			}},
		}},
		// The updater is opt-in through the environment so the example
		// runs without a release feed: FOCUS_UPDATE_FEED names the
		// signed manifest, FOCUS_UPDATE_KEY its ed25519 public key
		// (gofastr desktop keygen / feed).
		Update: updateConfigFromEnv(),
		// The window remembers where it was and what it was showing:
		// a relaunch reopens on the last screen at the last size.
		RememberWindows: true,
	})
	eng.d = d

	// The screens half: framework/ui only, mounted before the battery
	// (its Init looks for the UIHost among the mountables).
	site, err := buildSite(app, eng, d)
	if err != nil {
		return nil, nil, nil, err
	}
	// Same-origin external script (never inline): the page-side
	// behaviour for the countdown, the timer buttons, and the widget,
	// served from the embedded bytes with the hash-versioned URL.
	app.Router().Get("/desktop-focus.js", uihost.ScriptHandler(pageJS))
	app.Mount(uihost.New(site,
		uihost.WithExtraScripts(uihost.ScriptURL("/desktop-focus.js", pageJS)),
		uihost.WithAppIcon(appIconPNG()),
	))
	app.RegisterBattery(d)
	app.RegisterPlugin(focusPlugin{eng: eng})

	// The tick loop: started when the app is ready, stopped when the
	// app drains. One Tick per second drives the tray countdown, the
	// focus_tick events, and the end-of-session transitions.
	loopCtx, stopLoop := context.WithCancel(context.Background())
	app.OnStop(func() error { stopLoop(); return nil })
	app.OnReady(func(string) { go eng.Run(loopCtx) })

	return app, d, eng, nil
}

// intPtr hands a preference declaration a bound by value.
func intPtr(n int) *int { return &n }

// onDeepLink routes a gofastr-focus:// link. start?task=<id> starts
// that task and lands on the dashboard; anything else maps through the
// default rule the battery would have applied (scheme host + path,
// query kept, fragment dropped), returned here so the same code path
// runs for both.
func onDeepLink(u *url.URL, eng *Engine, d *desktop.Battery) (string, bool) {
	if u.Host == "start" {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := eng.Start(localUserCtx(ctx, d), u.Query().Get("task")); err != nil {
			slog.Warn("desktop-focus: deep-link start", "error", err)
		}
		return "/", true
	}
	path := "/" + u.Host + u.Path
	if u.RawQuery != "" {
		path += "?" + u.RawQuery
	}
	return path, true
}

// toggleTimer is the tray and menu "Start / Pause" item: the engine's
// own phase-based toggle under the local identity.
func toggleTimer(ctx context.Context, eng *Engine) error {
	return eng.Toggle(localUserCtx(ctx, eng.d))
}

// updateConfigFromEnv reads the optional feed settings; nil (no
// updater) when either is absent.
func updateConfigFromEnv() *desktop.UpdateConfig {
	feed, key := os.Getenv("FOCUS_UPDATE_FEED"), os.Getenv("FOCUS_UPDATE_KEY")
	if feed == "" || key == "" {
		return nil
	}
	return &desktop.UpdateConfig{FeedURL: feed, PublicKey: key}
}

// checkForUpdates is the File > Check for updates handler: one check,
// then an event the page turns into a toast. With no feed configured
// the check answers unsupported, which the page reports as such.
func checkForUpdates(ctx context.Context, d *desktop.Battery) error {
	if d == nil {
		return nil
	}
	res, err := d.CheckForUpdates(ctx)
	var de *desktop.Error
	switch {
	case err == nil && res.Available:
		return d.Emit("update_available", map[string]string{"version": res.Version, "notes": res.Notes})
	case err == nil:
		return d.Emit("update_none", map[string]string{"version": res.Version})
	case errors.As(err, &de) && de.Code == desktop.CodeUnsupported:
		return d.Emit("update_none", map[string]string{"version": ""})
	default:
		return fmt.Errorf("check for updates: %w", err)
	}
}

// appIconPNG generates the icon source in code (framework/image), the
// same "no binary assets in the repo" posture as examples/meridian.
func appIconPNG() []byte {
	img, err := fwimage.NewGradient(512, 512, "#B45309", "#7C3AED")
	if err != nil {
		return nil
	}
	b, err := img.PNG().Bytes()
	if err != nil {
		return nil
	}
	return b
}
