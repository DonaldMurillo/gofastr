# Desktop host (experimental)

`battery/desktop` runs a GoFastr app as a local-first desktop
application inside the operating system's own WebView, WKWebView on
macOS, from a single `CGO_ENABLED=0` binary. The web rendering path is
reused untouched: the same `framework/ui` screens, the same SSR +
island model, the same `main.go` wiring minus `app.Start`.

**EXPERIMENTAL.** This battery is a proof of concept: the capability
set, the `Shell`/`Window` contracts, and the generated bridge may
change or be removed without a deprecation window. Pin a version if you
depend on it. The native `Shell` ships for darwin/arm64 only (AppKit +
WKWebView through a pure-Go Objective-C bridge); Windows and Linux are
designed but not built, and every other platform gets a named
`unsupported` error from `Run`.

## What a desktop app is

The same App. Nothing about the SSR, hydration, island, poll, or SSE
model changes, and every route, battery, and plugin the web app has is
available. The one difference is who owns the process: `d.Run(app)`
replaces `app.Start(addr)`. `Run` starts the app on a loopback port,
opens the native window on a one-shot boot URL, and blocks until the
window closes, then drains the app.

## The shape of a `main.go`

<!-- gofastr:compile
import (
	"log"

	uiapp "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

var site = uiapp.NewApp("notes")
-->
```go
opts, err := desktop.AppOptions("dev.gofastr.notes")
if err != nil {
    log.Fatal(err)
}
app := framework.NewApp(append(opts,
    framework.WithConfig(framework.AppConfig{Name: "notes"}))...)
app.Mount(uihost.New(site)) // BEFORE RegisterBattery
d := desktop.New(desktop.Config{ID: "dev.gofastr.notes", Title: "Notes"})
app.RegisterBattery(d)
if err := d.Run(app); err != nil {
    log.Fatal(err)
}
```
- `Run` forces `GOFASTR_ISOLATION=off` process-wide (opt out with
  `Config.KeepIsolation`): a desktop app never runs from a linked git
  worktree, and the isolation remap would move the loopback port the
  window is about to load.
- An app can also serve itself over HTTP and skip `Run`: set
  `--serve :8080` or `$PORT` and call `app.Start` instead.
  `examples/desktop-notes` does both behind one flag. In that mode the
  battery stays registered but its boot gate stays unarmed, so there is
  no window handshake and this battery installs **no identity at all**
  and serves **no** native capability calls (the chokepoint answers 404
  until `Run` freezes the registry). Every anonymous browser would
  otherwise become the local admin, and a `$PORT` deployment binds a
  public interface. An app that needs a signed-in user in this mode
  brings `battery/auth`; otherwise its requests are anonymous.

## The data dir

`os.UserConfigDir()/<id>` (`~/Library/Application Support/<id>` on
macOS), created 0700. Three files live there:

| File | What it is |
|---|---|
| `app.db` | The app's SQLite database, opened by `AppOptions`. |
| `secret` | The 32-byte session secret (`WithSecret`), 0600. |
| `identity` | The per-installation local user id (see below). |

`GOFASTR_DESKTOP_DATA_DIR` overrides the base directory (absolute
paths only); tests and CI use it to avoid touching a real profile.
`Config.DataDir` overrides the whole path per battery.

## The local identity

A desktop app ships without `battery/auth`, but owner-scoped CRUD
still needs an owner (hard rule 6). The battery's `LocalUser`
middleware sets a stable per-installation identity (the `identity`
file) into every request context, and installs the process-wide owner
extractor only when nothing else has. Both are fallbacks: an app that
also imports `battery/auth` keeps auth's user and auth's extractor.
`Battery.LocalUserID()` returns the id for code outside a request, such
as a menu handler scoping its own query.

## How the window authenticates

Any local process can find the loopback port, so the page must present
a credential nothing else has:

1. `New` mints a 32-byte boot token (crypto/rand, never logged).
2. `Run` navigates the fresh window to
   `/__gofastr/desktop/enter?t=<token>`, single use, constant-time
   compared.
3. The handler sets the `__gofastr_desktop` cookie (HttpOnly,
   SameSite=Strict, session lifetime) and 302s to `/`.
4. A gate middleware, installed app-wide in `Init` and armed by `Run`
   before the listener opens, then requires the pinned `Host` header
   and that cookie for every route, including pages and the SSE bus.

The `Host` pin closes DNS rebinding from a browser tab; the cookie
closes every other local process. Neither value is ever logged. An app
that never calls `Run` never arms the gate.

## Capabilities

The page reaches native surfaces through ONE chokepoint,
`POST /__gofastr/desktop/call/{cap}/{method}`. It answers a uniform
404 until `Run` freezes the registry, so an app serving itself over
plain HTTP exposes no native surface and no capability is enumerable.
Then: POST with `application/json` only; a `Sec-Fetch-Site` that is
present and cross-site is refused (a non-browser local client sends
none, and the boot cookie is the control for those); bodies capped at
1 MiB and refused outright if any string in them carries a NUL, which
cannot cross into the native layer; responses `{ok:true,result}` or
`{ok:false,error:{code,message}}` with a closed code set (`denied`,
`unsupported`, `invalid_input`, `cancelled`, `not_found`,
`internal`).

The core capabilities:

| Capability | Methods | Permission |
|---|---|---|
| `window` | `title`, `setTitle`, `snapshot` | none (the page lives in that window) |
| `windows` | `open`, `openSettings`, `focus`, `close`, `list` | none (a page may only open the app's own screens; `close` refuses `"main"`) |
| `dialogs` | `openFile`, `saveFile`, `openFolder` | none (the user mediates every dialog; returned paths join the `fs` allow-list) |
| `clipboard` | `readText`, `writeText` | `clipboard:read`, `clipboard:write` |
| `notifications` | `show` | `notifications:show` |
| `fs` | `readText`, `writeText`, `stat` | `fs:read`, `fs:write` |
| `tray` | `setTitle` | none (the tray label is the app's own) |

`fs` only touches paths a `dialogs` call returned during this process
(`Battery.AllowPath` for in-process grants): the cleaned absolute path
AND its symlink resolution must both be on the list, so a symlink
already in place when the request arrives cannot widen access. It is
not open-then-verify: a local process running as the same user could
still win the race between the resolution and the open, but such a
process can read the file directly anyway, so the bridge is not a
privilege boundary against it. Writes are temp-file + rename, mode
0600 (rename does not follow a final symlink); reads cap at 8 MiB.

## Permissions and grants

A method with a `Permission` ("resource:verb") is gated. The
`desktop_grants` table in the app's own SQLite decides; `allow` and
`deny` persist across restarts. A miss hops to the UI thread and shows
the OS alert with Allow, Allow once, and Deny; only the first and the
last persist (so a page cannot re-prompt in a loop, and deny is
sticky). With no database on the app, grants fall back to memory with
a startup Warn.

A grant is keyed on the **capability and the permission**, not the
permission alone: the alert names the capability, so a plugin that
declares an existing permission string still gets its own prompt. The
decision is written on a context detached from the request, because a
page that aborts its own fetch must not be able to make the user's
Deny unwritable, which is the whole point of persisting it. The alert
itself is bounded by `modalTimeout` (10 minutes), not by the default
main-thread deadline.

## The JS bridge

`core-ui/runtime/src/desktop.js` (the `desktop` demand module) owns
the transport, name validation, and the listener map. The per-host
`bridge.js`, generated at freeze time from the same registry as the
manifest, layers the typed namespaces on top:

```js
await __gofastr.desktop.clipboard.writeText({ text: "hi" })
const { title } = await __gofastr.desktop.window.title()
__gofastr.desktop.on("notes_exported", (payload) => { ... })
__gofastr.desktop.manifest
__gofastr.desktop.available // false in a plain browser
```

The generated module is served at `/__gofastr/desktop/bridge.js` and
put on the script rail; no inline script and no new `data-fui-*`
attribute. A capability absent from this host simply has no namespace;
calling it through `call` rejects `not_found`. The module forwards
`X-CSRF-Token` when the app ships a `csrf-token` meta (for example
with `battery/auth.WithBFFPosture`); the chokepoint also defends
itself without one.

`gofastr desktop types --pkg .` builds the app, runs it once with
`GOFASTR_DESKTOP_MANIFEST=1` (the app prints its frozen manifest and
exits before opening a window), and writes a starting-point
`desktop.d.ts` from that manifest. Regenerate it when you change a
capability's methods.

## Writing a capability in a plugin

A plugin registers capabilities from its `Init` through the same seam
the core uses. `examples/desktop-notes`'s `systeminfo` is the whole
example:

<!-- gofastr:compile
import (
	"context"
	"encoding/json"
	"runtime"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/framework"
)

type systemInfoPlugin struct{}

func (systemInfoPlugin) Name() string { return "systeminfo" }
-->
```go
func (systemInfoPlugin) Init(app *framework.App) error {
	d, err := desktop.FromApp(app)
	if err != nil {
		return nil // no desktop battery mounted; the plugin is inert
	}
	return d.Register(desktop.Capability{
		Name:    "systeminfo",
		Version: 1,
		Methods: []desktop.Method{{
			Name: "cpuCount",
			Output: json.RawMessage(
				`{"type":"object","properties":{"count":{"type":"integer"}}}`),
			Handler: func(ctx context.Context, in json.RawMessage) (any, error) {
				return map[string]any{"count": runtime.NumCPU()}, nil
			},
		}},
	})
}
```

The method then appears in `/__gofastr/desktop/manifest.json`, in the
generated `bridge.js`, and in the `.d.ts`, with no host change.
Registration is open until `Run` freezes the registry; a duplicate
name is refused naming the first registration site. Bump
`Capability.Version` when you remove a method or change an input
shape.

## Menus

`Config.Menu` declares the native menu bar in Go. The shell wraps it
with the standard app menu (About, Quit) and an Edit menu; do not
declare your own Quit beyond the `Role: "quit"` item. Each item is
exactly one of:

- `Navigate`: a same-origin absolute path, dispatched as
  `window.__gofastr.navigate(path)`, a client-side nav with no reload.
- `Handler`: a Go `func(ctx context.Context) error`, run on a goroutine
  with a 30 s deadline; errors are logged with the item id.
- `Role`: `quit`, `about`, or `separator`, implemented by the shell.

<!-- gofastr:compile
import (
	"context"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
)

func exportAll(ctx context.Context) error { return nil }
stmt: _ = menu
-->
```go
menu := &desktop.Menu{Items: []desktop.MenuItem{
	{Title: "File", Children: []desktop.MenuItem{
		{Title: "New note", Key: "cmd+n", Navigate: "/notes/new"},
		{Title: "Export all…", Key: "cmd+e", Handler: exportAll},
		{Role: desktop.RoleSeparator},
		{Role: desktop.RoleQuit},
	}},
}}
```

## Settings window

`Config.Settings` describes a secondary window showing one of the
app's own screens:

```go
d := desktop.New(desktop.Config{
    ID:       "dev.gofastr.notes",
    Title:    "Notes",
    Settings: &desktop.WindowSpec{Path: "/settings", Title: "Settings", Width: 520, Height: 460},
})
```

Setting it gives the app three entry points at once: the shell
synthesizes the app menu's "Settings…" item (cmd+, on macOS), the
`RoleSettings` menu role works in `Config.Menu` and in the tray menu,
and the page can call `__gofastr.desktop.windows.openSettings()`.

On the Go side, `d.OpenSettings()` opens (or focuses) that window and
`d.OpenWindow(desktop.WindowSpec{Path: "/notes/new"})` opens any other
screen; a window with the same path that is already open is focused,
so the call is idempotent per path. `d.Windows()` lists them, the main
window first. Secondary windows share the main window's cookie store
(the same web view configuration), so the boot cookie applies and no
second handshake happens. Closing a secondary window does not quit the
app; only the main window's close does (or hides it, with the tray
below).

## Tray icon

`Config.Tray` puts the app in the menu bar (a status item on macOS):

```go
Tray: &desktop.Tray{
    Title: "Notes",             // text in the bar; may be empty with Icon
    Tooltip: "Notes",
    Icon: appIconPNG(),          // PNG, rendered as an 18x18 template image
    Menu: &desktop.Menu{Items: []desktop.MenuItem{
        {Title: "Show Notes", Role: desktop.RoleShow},
        {Title: "New note", Navigate: "/notes/new"},
        {Title: "Settings…", Role: desktop.RoleSettings},
        {Role: desktop.RoleSeparator},
        {Role: desktop.RoleQuit},
    }},
    CloseHidesWindow: true,      // the red button hides, RoleShow returns
```

Where the item lands is the system's call: macOS inserts a new status
item at the left of the existing ones and hides whatever no longer
fits on a full menu bar, and there is no API to choose a position. Give
the tray a `Title` so it is wide and findable, and tell users they can
cmd-drag items to reorder them.

```go
    HideDock: false,             // true removes the Dock icon
},
```

The tray menu uses the same `MenuItem` model as `Config.Menu`
(Navigate, Handler, or Role) and is validated together with it, so
item ids stay unique across both. `RoleShow` orders the main window
front and activates the app. `CloseHidesWindow` keeps the app alive in
the menu bar after the main window closes; `HideDock` runs the app as
an accessory (no Dock icon, menu bar only). `d.SetTrayTitle` and
`__gofastr.desktop.tray.setTitle` change the bar title at runtime.

The tray is macOS-only for now. On a host without a tray surface, Run
logs a Warn naming the host error and the rest of the app works.

## Window styles, frameless windows, widgets

A desktop window is not always a titled rectangle. A launcher bar, a
color picker, a "quick note" pad: these want no title bar, want to
float above the app, and want to let the page paint its own surface.
`WindowSpec.Style` (and `Config.Style` for the main window) describes
that chrome; `desktop.Widget` builds the common shape.

```go
d := desktop.New(desktop.Config{
    ID:    "dev.gofastr.notes",
    Title: "Notes",
    Widgets: []desktop.WindowSpec{
        desktop.Widget("/quick", 320, 280), // opened at launch
    },
})
```

### The style model

```go
type WindowStyle struct {
    Chrome      WindowChrome // ChromeDefault | ChromeHiddenTitle | ChromeNone
    Float       bool         // NSFloatingWindowLevel on darwin
    Panel       bool         // non-activating panel; implies Float
    Transparent bool         // clear window background
    Resizable   *bool        // nil means true
    AllSpaces   bool         // visible on every Space
    X, Y        *int         // top-left origin in screen points; both or neither
}
```

- `ChromeDefault` is the host's standard titled window.
- `ChromeHiddenTitle` keeps the traffic lights and lets the page paint
  under the title bar (`fullSizeContentView` with a transparent,
  title-hidden title bar on darwin).
- `ChromeNone` is borderless: no title bar, no close button, no resize
  box. A borderless window is moved by dragging the page (below).
- `Panel` creates an `NSPanel` with the non-activating style on
  darwin: clicking the window does not steal focus from the frontmost
  app, and the shell never makes it the key window. A panel implies
  `Float`.
- `Transparent` clears the window background AND stops the web view
  drawing its own (`drawsBackground` through KVC), so the page's own
  surface (a `ui.Card`, say) is the whole visual. Note the page still
  needs something to be that surface; an unpainted transparent window
  shows the desktop.
- `X` and `Y` position the window's top-left corner in screen points
  on the primary screen, measured from its top-left. Set both or
  neither: one alone is a configuration error (the bridge answers
  `invalid_input`; the Go side ignores it and centers).

The zero `WindowStyle` is the standard window; nothing changes for
apps that never mention styles.

### The page can style its own windows

`windows.open` accepts a `style` object, validated the same way:

```js
await __gofastr.desktop.windows.open({
  path:  "/widget",
  title: "Quick note",
  width: 320, height: 280,
  style: { chrome: "none", panel: true, transparent: true },
})
```

The style applies when the window is CREATED. An already-open path is
only focused; its style is whatever it was opened with. `chrome` is
one of `default`, `hiddenTitle`, `none`; `resizable` is a boolean
(omit for the default true); `x`/`y` must be sent together and are
bounded to ±100000.

### Dragging a borderless window

A borderless window has no title bar to drag, and the web view covers
the window's background, so the drag starts in the PAGE: put
`data-fui-window-drag` on the element that is the handle (a header
strip, the whole card, anything).

```go
html.Div(html.DivConfig{ExtraAttrs: html.Attrs{"data-fui-window-drag": ""}},
    render.Text("Quick note")),
```

The runtime's `desktop` module listens for `mousedown` on such an
element (or inside one) and calls
`__gofastr.desktop.window.startDrag()`, which any page code can call
directly. The request rides the WebView's script message channel
(`window.webkit.messageHandlers.gofastr`), not the HTTP bridge:
`performWindowDragWithEvent:` needs the mouse-down that is still the
current event on the UI thread, and by the time an HTTP round-trip
came back it no longer would be. In a plain browser (`--serve`) there
is no message handler and the call is a no-op, so screens carrying the
attribute render unchanged there.

### The desktop-notes widget

`examples/desktop-notes` ships the full recipe: a "Quick note…" tray
row opens `desktop.Widget("/widget", 320, 280)`, and `/widget`
renders a `ui.Card` whose header is the drag handle and whose form
posts to the same `/api/notes` route the editor uses, resetting after
each save. The Close button reads its own window id
(`window.__gofastr_desktop.window`) and calls
`__gofastr.desktop.windows.close({id})`: only the page knows which
window it lives in.

### Host support

The style contracts are OS-neutral; today only the darwin/arm64 shell
implements them. The mask bits, the floating level (3), the
non-activating panel bit, and the canJoinAllSpaces behavior were
verified against the SDK headers (`NSWindow.h`, `CGWindowLevel.h`)
and the live window is proven by the native e2e step
(`shell_darwin_e2e_test.go`, tag `desktop_e2e`): styleMask without the
titled bit, floating level, `isOpaque` NO, the `NSPanel` class, the
panel never becoming key, and the page learning its own window id
(every window carries its own `WKUserContentController` with its own
`BootstrapJS(id)` user script while sharing the main window's
`WKWebsiteDataStore`, so the cookie still crosses).

## Native events

`Battery.Emit(name, payload)` delivers an event to page listeners
registered with `__gofastr.desktop.on(name, fn)`. The name must match
`^[a-z][a-z0-9_]{0,63}$` (underscores, not dots). The payload is
marshaled to JSON and handed to the page as a quoted string parsed by
`JSON.parse`, never spliced in as an object literal. A listener that
throws is reported with the event name only. Island refreshes after a
native event stay server-driven (`island.Manager.PushUpdate`), the
same as in the web app.

## Cross-window events and messages

A desktop app can have several windows: the main window, the settings
window, any window `windows.open` created. Three rules hold across
them: every window knows which one it is, a native event reaches every
window, and pages message each other through the server, never window
to window.

### Every window knows its id

The one script a native shell injects at document start
(`desktop.BootstrapJS(windowID)`) carries the id of the window the page
lives in; every window gets its own copy of the script with its own id
("main" for the first window, "settings" for the settings window,
"w2", "w3", ... in opening order). The runtime exposes it:

```js
__gofastr.desktop.windowID // "main", "settings", "w2", ...
```

In a plain browser (`--serve`) there is no marker and `windowID` is
`"main"`.

### Native events reach every window

`Battery.Emit(name, payload)` delivers to EVERY open window, the main
window first: a native event (a menu action, a deep link, an update
tick) is app-wide, and every page that registered a listener with
`__gofastr.desktop.on(name, fn)` hears it. An eval failure in one
window is logged server-side and does not stop the others; `Emit`
returns the first error after trying them all.

`Battery.EmitTo(windowID, name, payload)` delivers to one window. A
window id that is not open (never was, or the user closed it, or it
closed between the lookup and the delivery) is `not_found`, not
`internal`: closing a window mid-delivery is normal window life.

Both deliver with the same `_dispatch` eval shape: the name and the
payload's JSON travel as quoted strings and the page parses the payload
with `JSON.parse`, never an object literal.

### Pages message each other through the bridge

Three methods on the `windows` capability, all ungated (only the app's
own same-origin windows exist):

```js
await __gofastr.desktop.windows.post({ to: "w2", name: "note_saved", payload: {id: 9} })
const { delivered } = await __gofastr.desktop.windows.broadcast({ name: "theme_changed", payload: {dark: true} })
const { id } = await __gofastr.desktop.windows.self()
```

- `post` delivers to the window `to` names. `to` must be an open
  window id (`not_found` otherwise). Output: none.
- `broadcast` delivers to every open window EXCEPT the caller's own;
  the caller knows what it sent. Output: `{delivered}`.
- `self` returns `{id}`: the caller's window id as the page reported
  it.

Both `post` and `broadcast` check the same two rules before anything
is delivered: `name` must match the event grammar
`^[a-z][a-z0-9_]{0,63}$` (underscores, not dots, the same grammar
`Emit` enforces), and the payload's compact JSON must be at most
64 KiB. Violations answer `invalid_input` and deliver nothing. The
cap is measured on the compact form, so whitespace cannot smuggle the
difference; the whole request body is separately capped at 1 MiB by
the chokepoint like every bridge call. The payload crosses unchanged,
including its key order.

Delivery is the same `_dispatch` eval `Emit` uses, so a listener
registered with `on` cannot tell a native event from a posted one.

### The caller is a claim, not an identity

Every bridge call carries the header `X-Gofastr-Window`, which the
runtime module sets from the host marker's window field. The header is
set by the page, so it is a claim: the server cannot verify which
window a request came from, and it does not try. The value only
selects which window is "the caller" for `broadcast` exclusion and the
`self` answer. A claim that is absent or off the window id grammar
(`^[a-z][a-z0-9]{0,15}$`) reads as `"main"`. Nothing about
permissions, grants, or window control (`close`, `focus`) keys on it:
those take an explicit `id` and check it against the open windows.

### Testing cross-window flows

The harness records events per window: `h.Events()` stays the main
window's, and `h.Window(id).Events()` is any window's.
`h.Call(cap, method, input)` plays the main window's page and now
sends `X-Gofastr-Window: main`; `h.CallFrom(windowID, cap, method,
input)` plays any other window's page, id included. To run the pages'
JavaScript for real, open one Chrome tab per window: each tab gets the
session cookie, its own `desktop.BootstrapJS(id)` document-start
script, and its fake window's eval hook
(`battery/desktop/browser_harness_e2e_test.go`'s two-window test is
the reference).

## Deep links

`Config.DeepLink` makes the app answer URLs on its own scheme. A
desktop notes app claims `gofastr-notes`, and `gofastr-notes://notes/123` opens (or
focuses) it on that note. The scheme is registered with the OS at
bundle-build time, the URL itself arrives as an Apple Event while the
app runs, and the battery turns it into the same client-side
navigation a menu item performs.

```go
d := desktop.New(desktop.Config{
    ID:       "dev.gofastr.notes",
    Title:    "Notes",
    DeepLink: &desktop.DeepLinkConfig{Scheme: "gofastr-notes"},
})
```

`Scheme` is required, lowercase-first (`[a-z][a-z0-9+.-]*`, at most 32
characters); `New` panics on a bad one the same way it panics on a bad
menu. Pick a scheme nobody else claims: Apple's own Notes app already
owns `notes:`, so a link on that scheme opens Apple Notes, never yours.
Prefix it with your product name (`gofastr-notes`).

Register the scheme in the bundle or the OS will hand the URL to
somebody else:

```sh
gofastr desktop build --id dev.gofastr.notes --name Notes --scheme gofastr-notes
```

`--scheme` writes `CFBundleURLTypes` into the bundle's Info.plist. With
no flag, the plist carries no URL types and the app claims nothing.
Development runs (`go run`, `gofastr desktop run`) are never
registered; the mapping and the queue still work, only the OS handoff
is missing.

### What a link becomes

The default mapping rewrites the scheme's host and path into the app's
path space, keeps the query, and drops the fragment:

| Link | Navigates to |
|---|---|
| `gofastr-notes://notes/123` | `/notes/123` |
| `gofastr-notes://notes/123?q=1` | `/notes/123?q=1` |
| `gofastr-notes://notes/123#sec` | `/notes/123` |
| `gofastr-notes://` | `/` |

The result must pass the same grammar a menu `Navigate` path passes
(leading `/`, no scheme, no control characters, no `.` or `..`
segments); a link that cannot pass is dropped with a Warn. These are
also dropped, each with a Warn and a scrubbed copy in the log: links on
any other scheme (the match folds case, so `NOTES://` is the app's
scheme), links carrying userinfo (`gofastr-notes://user:pass@host/`), and
links longer than 2048 bytes.

`OnDeepLink` replaces the mapping for links the app wants to route
itself:

```go
DeepLink: &desktop.DeepLinkConfig{
    Scheme: "gofastr-notes",
    OnDeepLink: func(u *url.URL) (string, bool) {
        if u.Host == "new" {
            return "/notes/new", true
        }
        return "", false // decline: no navigation, no event
    },
},
```

The returned path passes the same navigate-path grammar; `ok == false`
declines the link silently (a decline is the app's decision, not a
Warn).

### Delivery

A link that arrives while the window is up:

1. Focuses the main window.
2. Navigates it with `window.__gofastr.navigate(path)`, the identical
   client-side call a menu Navigate item makes (no reload).
3. Emits `deep_link` to every open window's page listeners with
   `{url, path}`: the raw link and the path it became.

```js
__gofastr.desktop.on('deep_link', ({ url, path }) => { ... })
```

Links that arrive before the window is up (a cold launch: the OS may
deliver the URL event before the first window finished its boot
navigation) are queued, at most 16, oldest dropped first with a Warn,
and replayed in arrival order right after the boot navigation.

### The macOS handoff

On macOS the URL arrives as a GetURL Apple Event (class `'GURL'`, id
`'GURL'`) whose direct object is the URL string. The darwin shell
registers the bridge object as the handler with the shared
`NSAppleEventManager` before the run loop starts, so a cold-launch URL
is not missed. The handler reads the direct object, hands the raw URL
to the battery on a goroutine, and the battery does everything above.
The scheme check runs before anything else: an event for a scheme the
config did not claim is dropped, so two GoFastr apps with different
schemes stay independent even though both register the same event
class.

Other hosts answer `unsupported` for now; the portable half (mapping,
queue, the event) is host-independent and runs against the test double
on every OS.

### Testing

The harness plays the OS: `h.OpenURL("gofastr-notes://notes/123")` fires the
deep-link callback exactly as the shell would, before or after the
window is up (`battery/desktop/deeplink_test.go`). On a Mac, the
untagged suite exercises the real Apple Event handler: the test
builds an `NSAppleEventDescriptor`, registers through
`installDeepLinkHandler`, and invokes the handler IMP the way the
manager does (`battery/desktop/shell_darwin_deeplink_test.go`); the
tagged e2e drives the whole chain in a live window
(`battery/desktop/shell_darwin_e2e_test.go`, step 12).

## Notifications and signing

`Notification` carries `Title`, `Subtitle`, and `Body`; from Go call
`d.Notify(ctx, desktop.Notification{...})`, from the page
`__gofastr.desktop.notifications.show({title, subtitle, body})`
(gated by `notifications:show`).

On macOS, `UNUserNotificationCenter` refuses an app whose bundle has
no signature at all: the answer comes back as `UNErrorDomain` code 1,
which the host maps to `unsupported`, and `go run` therefore never
notifies. An ad-hoc signature is enough for the centre to accept the
app: a bundle signed with `codesign --force --deep --sign -` gets the
system permission prompt on its first request (observed 2026-09-04 on
macOS from `dist/Notes.app`). `gofastr desktop build` applies exactly
that by default when `codesign` is on PATH, so a freshly built bundle
can notify.

macOS shows nothing for the app that is frontmost unless the app's
notification delegate asks for it, and a save happens while the app is
frontmost. The shell installs itself as that delegate and answers
"present as banner, keep in the list, play the sound", so a
notification posted from the foreground appears as a banner. Override with
`--sign=<identity>` for a real (Developer ID) signature or `--no-sign`
to skip; on a host without `codesign` the build prints that the bundle
is unsigned, and a signing failure never fails the build.

The first `notifications.show` triggers the system prompt: macOS asks
"<AppName> would like to send you notifications" with Don't Allow and
Allow. Allow once and notifications work from then on; Don't Allow
lands in System Settings > Notifications, and the capability answers
`unsupported` until the user reverses it there. Notarization is still
required only for distribution outside a direct download, not for
notifications.

## Notarization and hardened-runtime signing

`gofastr desktop build --notarize` takes a bundle from "signed" to
"distributable". It signs with your Developer ID Application identity
under the hardened runtime with a secure timestamp, zips the bundle in
pure Go, submits the zip to Apple's notary service and waits, staples
the notary ticket to the bundle, and re-zips so the archive you hand
out carries the staple.

```bash
gofastr desktop build --id dev.gofastr.notes --name Notes \
  --sign "Developer ID Application: Your Name (TEAM12345)" \
  --notarize
```

### What the build runs, in order

| Step | Command |
|---|---|
| Sign | `codesign --force --deep --sign <identity> --options runtime --timestamp --entitlements <plist> <Name>.app` |
| Zip | `<Name>.zip` written in pure Go next to the `.app` |
| Submit | `xcrun notarytool submit <Name>.zip --keychain-profile <profile> --wait` |
| Staple | `xcrun stapler staple <Name>.app` |
| Re-zip | the same pure-Go zip, overwriting `<Name>.zip` so the archive carries the staple |

Every step prints like the build verb's other steps. Under `--notarize`
any step failing fails the build, with the tool's own output in the
message (Apple's tools put the reason there, not in the exit status).
That is deliberately different from the default signing path, where an
ad-hoc signing failure is printed and the build still succeeds.

### The flags

- `--notarize` requires `--sign <identity>`, and the identity must not
  be `-`: Apple's notary service rejects anything not signed by a real
  certificate, and an ad-hoc signature is exactly that. The build
  refuses `--notarize` without a real identity before any tool runs.
- `--notary-profile <name>` names the keychain profile you created
  with `xcrun notarytool store-credentials`. Default: `gofastr`.
- `--entitlements <plist>` uses your plist. Without it the build writes
  a generated plist to a temp file, passes it to codesign, and removes
  it after. The generated plist is an empty `<dict/>`, and that is the
  correct hardened-runtime baseline for these apps: a Go binary hosting
  a WKWebView needs no JIT entitlement, because the JavaScript compiler
  runs inside WebKit's own processes, which hold that entitlement
  themselves. The app process never JITs. If you load plugins or
  frameworks that need more, bring your own plist.

### The zip

Pure Go (`archive/zip`), no `ditto` or `zip` on PATH. Entry names are
relative to the output dir (`Notes.app/Contents/...`), directories are
their own entries, file modes are preserved (the executable bit under
`Contents/MacOS` is what LaunchServices launches), mtimes are zeroed so
two builds of the same tree produce identical bytes, and symlinks are
refused with an error naming the path: a bundle should not contain one,
and a zipper that follows it can read outside the tree.

### What you need before the first run

- An Apple Developer Program membership (the paid tier; notarization
  is not available on the free tier).
- A "Developer ID Application" certificate in your keychain. Check with
  `security find-identity -v -p codesigning`; the `--sign` value is
  that line's title in quotes.
- A notary profile stored once on the machine:

  ```bash
  xcrun notarytool store-credentials gofastr \
    --apple-id you@example.com \
    --team-id TEAM12345 \
    --password <app-specific password>
  ```

  (The password is an app-specific password from
  appleid.apple.com, not your Apple ID password.)
- `xcrun`, `codesign` on PATH: install Xcode's command line tools with
  `xcode-select --install`.

### What was tested and what was not

Nothing in this pipeline has been run against Apple's notary service:
that needs the paid account and a real identity above. What the tests
do prove: every external command goes through one seam, and the tests
assert the exact argv of codesign, notarytool, and stapler for the
ad-hoc, identity, and notarize paths; each step's failure fails the
build with the tool's output in the message; `--notarize` without a
real identity is refused before any tool runs; and the zip round-trips
through `archive/zip` with the executable bit intact. The first real
submission is yours.

## Auto-update

Apps built with `gofastr desktop build` can update themselves from a
signed feed: no Sparkle, no external updater, pure Go on the client
side. The updater lives in `battery/desktop` and its engine in
`battery/desktop/internal/update` (feed verification, semver,
capped downloads, zip extraction, and the OS apply step behind one
interface, implemented for darwin and refusing everywhere else).

**EXPERIMENTAL**, like the rest of `battery/desktop`: darwin/arm64
only, and the contracts below may change. An unbundled run (a
development `go run`, `gofastr desktop run`) never updates: there is no
bundle to swap.

### The feed

One directory served over HTTPS holds three files: `manifest.json`, a
detached `manifest.json.sig`, and the release archive the manifest
names. The signature is a raw ed25519 signature over the exact
manifest bytes, base64-encoded in the `.sig` file.

```json
{
  "version": "1.2.0",
  "notes": "What changed",
  "platforms": {
    "darwin-arm64": {
      "url": "https://example.com/notes/Notes-1.2.0.zip",
      "sha256": "<64 hex chars>",
      "size": 12345678
    }
  }
}
```

- The platform key is `GOOS-GOARCH` of the target machine
  (`darwin-arm64` today).
- The version is semver (`major.minor.patch`, an optional
  `-prerelease`; build metadata is refused).
- A feed may carry a `channels` object whose entries have the same
  shape as the top level; `UpdateConfig.Channel` selects
  `channels[channel]` when present, and the top level otherwise.
- URLs in the feed are https. Plain http is accepted only for
  127.0.0.1 so tests can serve a feed locally.

### Configuring the app

```go
d := desktop.New(desktop.Config{
    ID:    "dev.gofastr.notes",
    Title: "Notes",
    Update: &desktop.UpdateConfig{
        FeedURL:   "https://example.com/notes/manifest.json",
        PublicKey: "<64 hex chars>",  // the .pub file's contents
        Interval:  0,                 // default 6 h; negative = manual only
    },
})
```

`FeedURL` names the manifest itself; the signature is fetched from
`FeedURL + ".sig"`. `New` panics on a bad config (a non-https feed URL,
a public key that is not 64 hex chars) the same way it panics on a bad
menu. `PublicKey` is what `gofastr desktop keygen` wrote to the `.pub`
file.

The battery checks the feed on a schedule: the first check 30 seconds
after the window opens, then every `Interval` (6 hours by default;
negative disables scheduled checks while `CheckForUpdates` and
`updates.install` keep working). When a scheduled check finds a newer
version, every open page gets the `update_available` event:

```js
__gofastr.desktop.on("update_available", ({ version, notes }) => {
  // show "Restart to install 1.2.0"
})
```

Failures are logged (Warn) and retried next interval; an unbundled or
unsupported host logs once and stops. Log lines never carry the feed
URL or a path.

### The updates capability

The page reaches the updater through two methods:

| Method | Permission | Result |
|---|---|---|
| `check` | none | `{available, version, notes}` |
| `install` | `updates:install` | `{}` (the app relaunches) |

`check` is ungated because it reads only public release metadata.
`install` is gated, so the OS permission prompt asks the user once and
the decision persists in `desktop_grants` like every other grant.

`install` runs the full pipeline and then relaunches the new bundle:

1. Fetch and verify the feed (signature first: a feed whose signature
   does not verify is rejected before anything is parsed).
2. Compare semver against the running bundle's
   `CFBundleShortVersionString`. Unbundled runs never update.
3. Download the archive to a 0700 directory under the app's data dir,
   capped at the declared size and 256 MiB absolute; verify sha256 and
   exact size. Any failure removes the download.
4. Extract in pure Go. The archive must contain exactly one top-level
   `<Name>.app` directory matching the running bundle's name; every
   zip entry is path-cleaned and refused if it could escape the
   extraction root; symlinks and devices are refused outright.
5. Run `codesign --verify --deep --strict` on the extracted bundle and
   refuse on failure.
6. Swap: move the current bundle to `<Name>.app.old`, move the new one
   into place, `open -n` the new bundle, and quit. The next launch
   removes a leftover `.old`.

Every failure surfaces as one of the bridge's closed error codes with
a fixed message; no path or URL ever reaches the page.

From Go, the same two steps are `d.CheckForUpdates(ctx)` and
`d.InstallUpdate(ctx)` (the latter is what a File menu
"Check for updates…" handler calls after showing the result).

### Release tooling

The CLI mints the signing pair and writes the feed:

```
gofastr desktop keygen -o path
    writes path (private, 0600, hex seed + hex public key)
    and path.pub (hex public key)

gofastr desktop feed --key path --version 1.2.0 --notes "..." \
    --platform darwin-arm64 --archive Notes.zip \
    --url https://example.com/notes/Notes-1.2.0.zip [-o dir]
    writes manifest.json and manifest.json.sig into dir (-o defaults
    to .), computing sha256 and size from the archive file
```

`feed` refuses a version that is not semver, a platform key outside
the `GOOS-GOARCH` grammar, a non-https archive URL, and an archive
that is not a zip with exactly one top-level `.app` directory. The
writer and the updater's verifier share one implementation
(`desktop.SignUpdateFeed`), so the format cannot drift between them.

Keep the private key out of the repository (it is written 0600 for a
reason) and ship only the `.pub` contents in the app's
`UpdateConfig.PublicKey`.

### Testing an app's update flow

`battery/desktop/update_harness_test.go` is the reference: an
`httptest.Server` serves the signed feed on 127.0.0.1, the battery's
update platform (the OS seam: running version, bundle location,
codesign, relaunch) is replaced through a test seam before `Run`, and
the harness drives the page side (`updates.check`, the permission
gate on `updates.install`) and observes the apply step without any
native code.

## Remembering window state

A desktop app remembers where its windows were. With
`Config.RememberWindows: true` the battery persists every window's
frame (position and size) and the main window's last path, so a user
who drags a window to another monitor, resizes it, navigates to a
screen, and quits finds all of it back on the next launch.

```go
d := desktop.New(desktop.Config{
    ID:               "dev.gofastr.notes",
    Title:            "Notes",
    RememberWindows:  true,
})
```

`examples/desktop-focus` ships with the flag on.

### What is stored

One file, `windows.json` (0600), in the app's data dir next to
`app.db`:

```json
{
  "windows": {
    "main":     {"frame": {"X": 140, "Y": 90,  "Width": 800, "Height": 600}, "path": "/notes/42"},
    "settings": {"frame": {"X": 40,  "Y": 300, "Width": 500, "Height": 400}}
  }
}
```

The file is the battery's, never the OS's autosave (`NSWindow`
frame autosave does not follow the data dir and cannot store the
path). Frames are screen points, top-left based, the same convention
`WindowStyle.X/Y` uses. Writes are debounced 500 ms, so a drag does
not hammer the disk, and the last write happens on quit. A corrupt or
unreadable file is a Warn and a default launch, never a crash.

### What happens on relaunch

- The main window opens at its remembered frame; a secondary window
  opened under the same id ("settings", a widget) gets its remembered
  frame through the same `WindowSpec.Frame` path. An explicit `Frame`
  on the spec wins over the store.
- A remembered frame that no longer overlaps any screen's visible area
  by at least 100x50 points (the monitor was unplugged, the resolution
  changed) is dropped and the window centers.
- The boot handshake's redirect goes to the main window's remembered
  path instead of `/`, validated against the navigate grammar both
  when the page reports it and when the redirect uses it. Any doubt
  means `/`.

### How the path is tracked

The page tells the server where it is. The desktop runtime module
(`core-ui/runtime/src/desktop.js`) calls `window.setPath({path})` once
at load and after every client-side navigation (the router's
`gofastr:navigate` event). The call is ungated and is a claim, exactly
like the `X-Gofastr-Window` header it rides: the server keeps only the
MAIN window's report (the header says which window is calling) and
validates the path before storing it.

### The contracts

```go
type Frame struct{ X, Y, Width, Height int }

WindowConfig.Frame *Frame                 // initial main-window frame
WindowConfig.OnWindowFrame func(id string, f Frame)
WindowSpec.Frame  *Frame                  // initial secondary frame
Window.Frame() (Frame, error)             // live frame, top-left points
Window.SetFrame(Frame) error
```

`OnWindowFrame` fires on a goroutine whenever the user moves or
resizes a window, unthrottled; the battery debounces. On macOS the
window delegate's `windowDidMove:` and `windowDidEndLiveResize:` (the
resize reports once, at the end of the drag) feed it, and a
programmatic `SetFrame` reports through the same path. The top-left to
bottom-left coordinate flip happens once, in the darwin shell, against
the primary screen's height: `[NSScreen mainScreen]` is whichever
screen holds the key window and moves under the user, so the origin
screen (`[NSScreen screens]` index 0) is the only stable base.

Hosts without a native layer ignore `Frame` and never fire
`OnWindowFrame`; the flag is then inert.

### Testing seams

- `desktoptest.Harness.MoveWindow(id, frame)` plays the user dragging a
  window: the fake window's frame changes and `OnWindowFrame` fires
  the way the native delegate does. `Harness.BootRedirect()` is the
  Location the boot handshake answered with.
- `desktoptest.NativeHarness.MoveWindow` and `WindowFrame` drive the
  real shell; the delegate reports by itself.
- The harness suites cover restore across two batteries on one data
  dir, the redirect, the off flag, the 0600 mode, and the corrupt
  file; a darwin unit suite covers the flip and the screen-gone check
  as pure functions; native e2e steps (`battery/desktop` and
  `examples/desktop-focus`, tag `desktop_e2e`) prove the delegate, the
  file, and the page's own `setPath` report on the real window.

## The desktop-focus example

`examples/desktop-focus` is the second dogfood app of
`battery/desktop`. Where `desktop-notes` proves the host (one window,
one entity, the bridge), desktop-focus uses every app-quality feature
the battery ships: window styles, secondary windows, the tray, deep
links, notifications, cross-window messages, a plugin capability, and
the updater. It is a pomodoro timer because a timer exercises all of
them at once: a countdown wants the menu bar, an ending session wants
a notification, a floating widget wants window styles, and "start on
this task" wants a deep link.

### The shape worth copying

The timer's state is in the database, never in server RAM. The current
session is the newest `sessions` row with `completed == false`; the
tick loop recomputes everything from that row once a second. A
crashed or restarted run therefore resumes to the same answer, and the
interactive layer stays stateless (the architecture's rule) with no
extra work. Every engine read and write goes through the entities'
CrudHandlers with the owner's identity context, never raw SQL, so
owner scoping (hard rule 6) covers the engine the same as the screens.

The loop is started from `app.OnReady` and bounded by a context that
`app.OnStop` cancels, and each tick is recover-guarded: one panicking
tick is a Warn, never the end of the timer. In `--serve` mode there is
no battery window, and every native call in the engine (tray title,
notification, event emit, widget open) checks `Battery.Window()` first
and skips itself, so the same binary serves a browser without errors.

### The page contract

The page reaches the engine through exactly one door: the `focus`
capability a plugin registers from its `Init`
(`start {taskId}`, `pause`, `resume`, `skip`, `state`). Every mutation
answer carries the resulting state, and the page script re-renders its
countdown, phase label, and button visibility from that answer instead
of guessing locally; `focus_tick` events (one per second while a
session runs) keep it live afterwards. The server renders the same
visibility rules into the initial HTML, so the page is correct before
any script runs.

The floating widget posts `show_task` to the main window through
`windows.post` and the main page's listener navigates with
`__gofastr.navigate`: pages never talk to each other directly, only
through the bridge.

### Where each feature is wired

| Feature | Where |
|---|---|
| Hidden-title main window | `Config.Style{Chrome: ChromeHiddenTitle}` in `main.go` |
| Floating widget | `desktop.Widget("/widget", 320, 300)` + `Style.AllSpaces`, opened by `Engine.Start` and the View menu |
| Settings window | `Config.Settings` (app menu, File menu, tray row, `windows.openSettings`) |
| Tray countdown | `Engine.Tick` calls `SetTrayTitle` with `mm:ss` while a session runs, back to the app name when idle (the `tray_countdown` preference) |
| Notifications | `Engine.completeSession` through `d.Notify`, gated by the `notify` preference |
| Deep links | `DeepLinkConfig{Scheme: "gofastr-focus", OnDeepLink: ...}`: `start?task=<id>` starts the task, everything else maps through the default rule |
| Cross-window | the widget's Open task button (`windows.post`) and the main page's `show_task` listener |
| Plugin capability | `focusPlugin.Init` registers `focus` v1, ungated |
| Updater | `Update` from `FOCUS_UPDATE_FEED` / `FOCUS_UPDATE_KEY`, plus the File menu's Check for updates item |

### Testing the app shape

`engine_test.go` drives the engine with an injected clock against the
real entities. `harness_test.go` runs the real `Run` flow with the
fake shell: `focus.start` through the chokepoint opens the widget with
the widget style, a tick sets the tray title and reaches every
window's events, a completed session notifies and emits `focus_done`,
`h.OpenURL` plays the OS deep link, and the preferences silence the
notification and the tray countdown. `native_e2e_test.go` runs the
dashboard's Start button, the ticked countdown, the widget's post
into the main window, and the settings checkboxes inside the real
WKWebView through `desktoptest.NativeMain`.

## Escape hatches

- `Config.Shell` accepts any `Shell` implementation: the test double
  (`battery/desktop/desktoptest`), or a sidecar process of your own.
- `Window.Native()` returns the raw native web view handle
  (`WKWebView*`) for the one platform tweak the model does not cover.
- `Window.Eval(js)` runs a one-off script in the page.
- `Register(Capability)` exposes any Go function to the page.
- `app.Router()` is untouched: any route, any Go library.

## The `gofastr desktop` verbs

| Verb | What it does |
|---|---|
| `build --id=<reverse.dns> [--name] [--icon=<png>] [--pkg] [--version] [--sign=<identity>|--no-sign] [--notarize] [--notary-profile] [--entitlements] [--scheme] [-o=dist]` | Cross-compiles darwin/arm64 (`-trimpath -ldflags "-s -w"`) and writes `<o>/<Name>.app`: Info.plist, the binary, PkgInfo, and an icon.icns built in pure Go from the PNG (no iconutil; the default icon is a generated flat square). Signing: ad-hoc (`codesign --force --deep --sign -`) by default when `codesign` is on PATH, `--sign` for a real identity, `--no-sign` to skip; a signing error is printed, never fatal. Notarize before distributing. |
| `types [--pkg] [--out=desktop.d.ts]` | One headless manifest run of the built app, written out as a `.d.ts`. |
| `keygen -o=<path>` | Mints the auto-update signing pair: the private key (0600) and `<path>.pub`. |
| `feed --key --version --platform --archive --url [--notes] [-o]` | Writes the signed `manifest.json` and `manifest.json.sig` the updater verifies. |

`--id` must be reverse-DNS (`[A-Za-z0-9.-]+` with a dot), `--name`
printable with no path separator and at most 64 characters, and every
plist value is XML-escaped.

## What does not work yet

Windows, Linux, and amd64 macOS (Rosetta included) have no native
shell: `Run` returns the named `unsupported` error, and the tray,
window styles, deep links, and the updater are macOS-only (the
contracts are OS-neutral; another host answers `unsupported`, and a
`Config.Tray` there logs a Warn). macOS notifications need a signed
`.app` bundle (ad-hoc is enough, see above), so they never fire from
`go run`. Notarization and the update pipeline have not been run
against Apple's service or a real release feed. No drag-and-drop from
the file manager.

## Testing a desktop app

`battery/desktop/desktoptest` is the test harness. `desktoptest.Run`
runs the battery's real `Run` flow with the fake shell in place of the
OS: the loopback listener, the boot token, the session cookie, the
Host pin, the frozen registry, the menus, the tray, and the chokepoint
all run as they do in a window, and the harness plays the two sides
the OS normally plays. No native code, any OS, `go test`.

```go
func TestExport(t *testing.T) {
    t.Setenv("GOFASTR_DESKTOP_DATA_DIR", t.TempDir()) // before AppOptions
    shell := desktoptest.NewShell()
    app, d := buildApp(shell)                          // desktop.Config{Shell: shell, ...}
    h := desktoptest.Run(t, app, d)

    // The page side: requests carry the window's session and the
    // local identity, the way the WebView's do.
    h.Post("/api/notes", map[string]any{"title": "A"}).AssertStatus(t, 201)
    var out struct{ Title string }
    h.Call("window", "title", nil).MustResult(t, &out)
    h.Call("clipboard", "readText", nil).AssertCode(t, desktop.CodeDenied)

    // The native side: the user's hands on the menus, the tray, the
    // window chrome, and the permission alerts.
    h.Answer(desktop.DecisionAllow)
    h.Shell.SetSaveFile("/tmp/notes.md", nil)
    h.ClickMenu("File", "Export all…")
    ev := h.WaitEvent("notes_exported")
    h.OpenSettings()                 // the app menu's own item (cmd+,)
    h.ClickTray("Show Notes")
    h.PressKey("cmd+n")
    h.CloseWindow("settings")
    h.CloseMainWindow()              // hides under Tray.CloseHidesWindow

    // What crossed the shell.
    h.Shell.Notifications(); h.Navigations(); h.Events(); h.WindowIDs()
}
```

`h.Stranger()` is a client with no session, for asserting that another
local process on the port is refused. The harness quits the app in
`t.Cleanup`; `h.Quit()` returns `Run`'s error when a test ends the app
itself (the quit role, `CloseMainWindow` with no tray).

To run the page's JavaScript for real, test inside the app shell
itself: `desktoptest.NativeMain` boots the app once for the whole test
binary with the REAL shell (the WKWebView the shipped app runs in),
and `desktoptest.Native(t)` hands each test the harness. Nothing
stands in for the browser; every page-side step runs in the actual
web view, and every native step goes through the shell's `NativeDriver`
(`ClickMenu`, `ClickTray`, `OpenSettings`, `Answer`, `ClickPrompt`,
`OpenURL`, `CloseWindow`, `WindowState`, `Notifications`).

```go
func TestMain(m *testing.M) {
    os.Exit(desktoptest.NativeMain(m, buildApp)) // buildApp(nil): the real shell
}

func TestSettingsSaves(t *testing.T) {
    h := desktoptest.Native(t) // skips on a host with no native shell
    h.Navigate("/settings")
    h.Click("#f-notify_on_save")
    h.Click(`form button[type="submit"]`)
    var out struct{ Title string }
    json.Unmarshal(h.Call("window", "title", nil).Result, &out)
}
```

The harness's page side is the window's own JavaScript. `Eval` and
`EvalInto` run an async function body in the page and hand back its
JSON-encoded result; `Call` drives `window.__gofastr.desktop.call`,
the transport the typed bridge namespaces wrap, so a rejection's
`code` and `message` arrive in the same `CallResult` the fake harness
uses; `Get` and `Post` fetch from inside the page with the window's
session; `Click`, `Fill`, and `Submit` drive the real DOM; `Snapshot`
decodes the window's real pixels. A page that never answers times out
(the caller's context bounds the wait, 20 s at most), and a thrown
error surfaces as `internal` carrying the error's message.

One process, one app: the tests share the windows, the database, and
the persisted grants of a single run. A test that denies a permission
persists that denial for every later test in the binary, the same way
it would for a user; `Battery.ResetGrantsForTest` (exported in test
builds) clears the table between phases that need fresh prompts.

`battery/desktop/native_e2e_test.go` and
`examples/desktop-notes/native_e2e_test.go` are the references.

## How tests run

Everything that opens a real WebView is tagged `//go:build desktop_e2e`
and run by hand on a Mac with a display:

```bash
CGO_ENABLED=0 go test -count=1 -tags desktop_e2e ./battery/desktop/ ./examples/desktop-notes/
```

The tagged suites run the page half inside the real WKWebView through
`desktoptest.NativeMain` (the battery's own suite runs its unit tests
first, in the pre-Run world its lazy-loading pin needs, then drives
the same machinery through `desktoptest.NativePhase`). CI, the
pre-commit hook, and `scripts/test-all.sh` never run that tag; the
unit tests against the fake shell in `battery/desktop/desktoptest`
carry the coverage floor, and the chromedp suite in `core-ui/runtime`
still tests the browser runtime itself.

## Common mistakes

- **Calling `app.Start` and `d.Run`.** `Run` owns start and shutdown;
  calling both serves two ports and the window loads the wrong one.
  (Serving instead of running, behind a flag, is the documented
  exception: then `Run` is never called.)
- **Mounting the UI host after `RegisterBattery`.** `Init` snapshots
  the mountables when it runs; a late mount gets the named error
  instead of a window.
- **Relaxing the gate for a "helper" tool.** Any local process that
  finds the port must stay refused; that is the security model. Give a
  helper an explicit route on the app with its own auth instead.
- **Registering capabilities after `Run`.** The registry freezes when
  the window opens; a plugin that registers late gets an error, not a
  silent miss. Register from `Init`.
- **Reading files the dialogs did not return.** `fs` paths must be on
  the session allow-list; do not "fix" a denied read by widening it,
  that is the whole point of the list.
- **Declaring your own Quit item with a Handler.** Use `Role:
  "quit"`; the shell's app menu already owns quit, and a synthesized
  duplicate confuses the menu planner.
