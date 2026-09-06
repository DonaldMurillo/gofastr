// Command desktop-notes is the desktop battery's dogfood app: a
// local-first notes app that runs unchanged inside the OS WebView
// (desktop.New + Run) or over plain HTTP behind --serve / $PORT.
//
//	go run ./examples/desktop-notes              # opens the window
//	go run ./examples/desktop-notes --serve :8080
//	gofastr desktop run   --pkg ./examples/desktop-notes
//	gofastr desktop build --id dev.gofastr.desktop-notes --name Notes --scheme gofastr-notes --pkg ./examples/desktop-notes
package main

import (
	"context"
	_ "embed"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/filter"
	fwimage "github.com/DonaldMurillo/gofastr/framework/image"
	"github.com/DonaldMurillo/gofastr/framework/isolation"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// pageJS is the page-side behaviour (copy link, export toast, window
// title). Embedded, not served from a cwd-relative static dir: a bundle
// launched from Finder runs with cwd "/", and `go run` from the repo
// root would not find examples/desktop-notes/static either.
//
//go:embed static/desktop-notes.js
var pageJS []byte

// appID is the reverse-DNS identity: it names the data directory under
// the OS user config dir and the bundle identifier of a built .app.
const appID = "dev.gofastr.desktop-notes"

var serveFlag = flag.String("serve", "", "serve the same app over HTTP at this address instead of opening a desktop window")

// addrFlag is what `gofastr dev` passes to every child (`--addr`), so
// the dev loop and its example sweep can drive this app over HTTP the
// same way --serve does.
var addrFlag = flag.String("addr", "", "alias of --serve, the flag gofastr dev passes to the app it launches")

func main() {
	flag.Parse()

	app, d, err := buildApp(nil)
	if err != nil {
		log.Fatal(err)
	}

	// --serve, --addr (what `gofastr dev` passes), or $PORT (what PaaS runtimes inject),
	// runs the identical app over HTTP: the host-independence proof.
	// The battery stays registered; its boot gate is armed only by Run,
	// so an HTTP-served instance never needs the window handshake.
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
func buildApp(shell desktop.Shell) (*framework.App, *desktop.Battery, error) {
	opts, err := desktop.AppOptions(appID)
	if err != nil {
		return nil, nil, err
	}
	app := framework.NewApp(append(opts,
		framework.WithConfig(framework.AppConfig{Name: "desktop-notes", APIPrefix: "api"}),
	)...)

	// The data half: the owner-scoped entities + their hooks.
	registerNotesEntity(app)
	registerSettingsEntity(app)

	// The screens half: framework/ui only, mounted before the battery
	// (its Init looks for the UIHost among the mountables).
	site, err := buildSite(app)
	if err != nil {
		return nil, nil, err
	}
	// Same-origin external script (never inline): the page-side
	// behaviour for copy-link, export, and the window title, served
	// from the embedded bytes with the hash-versioned URL.
	app.Router().Get("/desktop-notes.js", uihost.ScriptHandler(pageJS))
	app.Mount(uihost.New(site,
		uihost.WithExtraScripts(uihost.ScriptURL("/desktop-notes.js", pageJS)),
		uihost.WithAppIcon(appIconPNG()),
	))

	// The desktop half. d is captured by the menu handler below before
	// any menu item can fire.
	var d *desktop.Battery
	d = desktop.New(desktop.Config{
		ID:     appID,
		Title:  "Notes",
		Width:  960,
		Height: 680,
		Shell:  shell,
		// Settings gets three entry points: the app menu's own
		// Settings item (synthesized by the shell, cmd+,), this File
		// menu row, and the tray's Settings row below.
		Settings: &desktop.WindowSpec{Path: "/settings", Title: "Settings", Width: 520, Height: 460},
		// gofastr-notes://notes/<id> opens the app on that note; the bundle
		// registers the scheme with --scheme gofastr-notes.
		DeepLink: &desktop.DeepLinkConfig{Scheme: "gofastr-notes"},
		Tray: &desktop.Tray{
			// A title next to the icon: macOS places a new status item
			// at the LEFT of the existing ones and hides whatever no
			// longer fits, so an icon-only item disappears on a full
			// menu bar. There is no API to choose the position; the
			// user reorders items with cmd-drag.
			Title:   "Notes",
			Tooltip: "Notes",
			Icon:    appIconPNG(),
			Menu: &desktop.Menu{Items: []desktop.MenuItem{
				{Title: "Show Notes", Role: desktop.RoleShow},
				{Title: "New note", Navigate: "/notes/new"},
				{Title: "Quick note…", Handler: func(ctx context.Context) error {
					// The widget is a styled window, not a navigation:
					// a Navigate row would move the MAIN window.
					if _, err := d.OpenWindow(desktop.Widget("/widget", 320, 280)); err != nil {
						return fmt.Errorf("open quick note: %w", err)
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
				{Title: "New note", Key: "cmd+n", Navigate: "/notes/new"},
				{Title: "Export all…", Key: "cmd+e", Handler: func(ctx context.Context) error {
					return exportAll(ctx, app, d)
				}},
				{Title: "Settings…", Role: desktop.RoleSettings},
				{Role: desktop.RoleSeparator},
				{Title: "Check for updates…", Handler: func(ctx context.Context) error {
					return checkForUpdates(ctx, d)
				}},
				{Role: desktop.RoleQuit},
			}},
		}},
		// The updater is opt-in through the environment so the example
		// runs without a release feed: NOTES_UPDATE_FEED names the
		// signed manifest, NOTES_UPDATE_KEY its ed25519 public key
		// (gofastr desktop keygen / feed).
		Update: updateConfigFromEnv(),
	})
	app.RegisterBattery(d)
	app.RegisterPlugin(systemInfoPlugin{})
	registerNotesHooks(app, d)
	return app, d, nil
}

// exportAll is the File > Export handler: one native save dialog, one
// Markdown file of every note the local identity owns, one native
// event the page turns into a toast. The dialog returns the only path
// this process may write (the fs allow-list posture); the write is
// 0600.
func exportAll(ctx context.Context, app *framework.App, d *desktop.Battery) error {
	if d == nil {
		return nil
	}
	path, err := d.Shell().Dialogs().SaveFile(ctx, desktop.SaveOptions{DefaultName: "notes.md"})
	if err == desktop.ErrCancelled {
		return nil
	}
	if err != nil {
		return fmt.Errorf("save dialog: %w", err)
	}
	if path == "" {
		return nil
	}
	rows, err := app.MustCrudHandler("notes").ListAll(localUserCtx(ctx, d), crud.ListOptions{
		Limit: 1000,
		Sorts: []filter.ParsedSort{{Field: "updated_at", Desc: true}},
	})
	if err != nil {
		return fmt.Errorf("load notes: %w", err)
	}
	var b strings.Builder
	for _, row := range rows {
		title, _ := row["title"].(string)
		body, _ := row["body"].(string)
		b.WriteString("# " + title + "\n\n")
		if body != "" {
			b.WriteString(body + "\n\n")
		}
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		return fmt.Errorf("write export: %w", err)
	}
	return d.Emit("notes_exported", map[string]any{"path": path})
}

// updateConfigFromEnv reads the optional feed settings; nil (no
// updater) when either is absent.
func updateConfigFromEnv() *desktop.UpdateConfig {
	feed, key := os.Getenv("NOTES_UPDATE_FEED"), os.Getenv("NOTES_UPDATE_KEY")
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
	img, err := fwimage.NewGradient(512, 512, "#0E7C86", "#4338CA")
	if err != nil {
		return nil
	}
	b, err := img.PNG().Bytes()
	if err != nil {
		return nil
	}
	return b
}
