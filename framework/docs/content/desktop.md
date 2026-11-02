# Desktop host (experimental)

`battery/desktop` runs a GoFastr app as a local-first desktop
application inside the operating system's own WebView: WKWebView on
macOS, WebView2 on Windows, and WebKitGTK on Linux. The shell builds with `CGO_ENABLED=0`.
It also works in CGO-enabled applications: those builds use Go's standard
runtime support while the desktop bindings remain Go and assembly.
The web rendering path is
reused untouched: the same `framework/ui` screens, the same SSR +
island model, the same `main.go` wiring minus `app.Start`.

**EXPERIMENTAL.** This battery is a proof of concept: the capability
set, the `Shell`/`Window` contracts, and the generated bridge may
change or be removed without an upgrade note. Pin a version if you
depend on it. Native shells ship for darwin/arm64 (AppKit + WKWebView)
windows/amd64 (Win32 + WebView2), and linux/amd64 or linux/arm64
(GTK 3 + WebKitGTK 4.1); other targets get a named
`unsupported` error from `Run`.

## Package layout

The battery is split by platform: `battery/desktop` is the contract plus
everything OS-neutral; the native layer is one package per platform, each
compiling on every GOOS so a host cross-compiles with no build tags of
its own.

| Package | What it is |
|---|---|
| `battery/desktop` | The contract (`Shell`, `Window`, `NativeDriver`) and the OS-neutral half: capabilities, grants, handshake, app state, preferences, deep links, updates, and the unsupported shell. |
| `battery/desktop/macos` | The real shell on darwin/arm64 (AppKit + WKWebView through `internal/objc`); the unsupported shell elsewhere. |
| `battery/desktop/windows` | The Windows/amd64 shell: Win32, WebView2, native capabilities, custom titlebar support, and DWM Mica/Mica Alt/Acrylic. |
| `battery/desktop/linux` | GTK 3 / WebKitGTK 4.1 shell on linux/amd64 and linux/arm64; unsupported elsewhere. |
| `battery/desktop/native` | `Shell()` picks the platform package by GOOS; `New(cfg)` is `desktop.New` with that shell as the nil-`Shell` default. The `gofastr_nodesktop` build tag leaves every platform package out (see [A server build that never opens a window](#a-server-build-that-never-opens-a-window)). |
| `battery/desktop/desktoptest` | The fake shell and the app-shell test harness. |
| `battery/desktop/internal/*` | `objc`, `ffi`, `fakecgo`, `gtk`, `win32`, `update`: one package per OS seam. |

`desktop.New` with a nil `Config.Shell` answers the unsupported shell on
every platform: the platform-aware default lives in `native`, so the
contract package never imports the platform packages and nothing
registers itself through `init`.

## What a desktop app is

The same App. Nothing about the SSR, hydration, island, poll, or SSE
model changes, and every route, battery, and plugin the web app has is
available. The one difference is who owns the process: `d.Run(app)`
replaces `app.Start(addr)`. `Run` starts the app on a loopback port,
opens the native window on a one-shot boot URL, and blocks until the
window closes, then drains the app. If the server stops while the window
is open (a listener failure, a `Shutdown` from elsewhere), `Run` closes
the window and returns.

## The shape of a `main.go`

<!-- gofastr:compile
import (
	"log"

	uiapp "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/battery/desktop/native"
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
d := native.New(desktop.Config{ID: "dev.gofastr.notes", Title: "Notes"})
app.RegisterBattery(d)
if err := d.Run(app); err != nil {
    log.Fatal(err)
}
```
- `native.New(cfg)` is `desktop.New(cfg)` with the platform shell as
  the nil-`Shell` default; a host may keep `desktop.New` and pass
  `Shell: native.Shell()` itself, or hand a `Shell` of its own (the
  test double). See the package layout below.
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
macOS), created 0700. Four files live there:

| File | What it is |
|---|---|
| `app.db` | The app's SQLite database, opened by `AppOptions`. |
| `secret` | The 32-byte session secret (`WithSecret`), 0600. |
| `identity` | The per-installation local user id (see below). |
| `state.json` | The app state store (see "App state"), 0600. |

`GOFASTR_DESKTOP_DATA_DIR` overrides the base directory (absolute
paths only); tests and CI use it to avoid touching a real profile.
`Config.DataDir` overrides the whole path per battery.

### Persistent browser profiles on Linux

Set `desktop.Config.PersistentProfile: true` to retain a Linux app's browser
cookies and website data across launches. This is an opt-in **browser profile**,
not a cookies-only store. Linux's default stays ephemeral, shared among the
windows of that running shell. The option is supported by the Linux native
shell; other shells return `ErrUnsupported` before starting the app rather than
silently ignoring it. It does not change other platforms' existing defaults.

The profile lives at `<resolved DataDir>/browser-profiles/<Config.ID>/`, never
under a window title. Even apps that explicitly share a `DataDir` have separate
browser profiles by ID. The data directory and profile root must be owned by
the current user with mode `0700`; missing directories are created privately,
but existing permissions are not changed. Symlinked paths, foreign-owned or
unsafe writable ancestors, non-regular profile files, malformed origin metadata
and invalid cookie databases are refused, not reset. So is a hard link to a file
outside the profile, or a second name for the cookie, origin or lock files.
WebKit's own cache links inside the profile are accepted. Startup
inspects a bounded profile tree (100,000 entries, depth32); these checks are not
a defense against a hostile process running as the same user.

**The origin is stable.** On the first launch, `Run` binds an unused IPv4
loopback port and records the actual port before opening WebKit. Later launches
bind exactly that port. This retains the origin needed by `localStorage` and
IndexedDB instead of silently abandoning them at a new random port. A busy
port, port-remapping configuration, or invalid origin record stops startup
before a window opens. Stop the conflicting process or restore the profile;
there is no automatic port migration. Failed first app startup can retry the
pending profile. Packaged secondary deep-link launches forward to the primary
before attempting to lock its profile.

Only non-session cookies with an expiry persist; session cookies are not
persisted, and expired or deleted cookies are not restored as live cookies.
Expired rows may remain in the native database until WebKit removes them. Each launch still mints fresh boot-token/session
credentials, and old credentials cannot authorize the next process. The Host
pin, same-site checks and permission grants are unchanged. The profile does
not implement login/logout: application auth determines whether its cookies
persist and clears them on logout. Turning this option off starts an ephemeral
browser but does not erase the saved profile, app database or grants.

Two intentional exceptions keep browser persistence from bypassing the native
boot contract: stored service-worker registrations are cleared and their native
completion awaited **before any view opens** (a failure/timeout aborts startup),
and persistent HTTP-auth credential storage is disabled. Applications can use
workers during a launch, but cannot rely on a worker surviving the next launch.

One process owns a profile at a time. Once WebKit has used it, its exclusive
lock is retained until process exit, not merely until the GTK loop returns:
WebKit's network process shuts down asynchronously. Do not delete the lock file
to force access. Kernel locks release when the owning process exits, including
a crash; this is not an acknowledgement that every native child has flushed or
that storage survives a crash. Filesystem write preflight and SQLite cookie
integrity and known native schema checks catch some startup failures, but
WebKit's cookie-storage setter
returns no durable-write acknowledgement; later disk-full/native write errors
cannot be promised synchronously by `Run`. Other website stores retain WebKit's
own format and recovery behavior. The cookie preflight accepts the ordered
9-column legacy and 10-column `moz_cookies` layouts used by libsoup 3; native
coverage uses WebKitGTK 2.54 / libsoup 3.6.5. An unknown future layout is refused
without migration or deletion: use a compatible runtime/framework version or
restore a backup, rather than discarding the database to make startup succeed.

To reset browser data, exit every process using the profile, then move aside or
remove the **whole AppID profile directory**, not just `origin.json` or the lock.
The next launch creates a new origin and empty browser data. Keep the separate
application database, identity and grants unless you also intend to reset those.

## Single-user mode

A desktop app is one user on one machine: every request the window
makes maps to a single per-installation identity, and that identity is
the owner for every owner-scoped row. The battery ships this by
itself; `battery/auth` is not part of the picture.

The identity is the `identity` file in the data dir: 16 bytes of
crypto/rand as hex, minted on first run, 0600, stable across
relaunches. A value read back off disk is revalidated against the
grammar that minted it before it becomes a security principal: a
trailing newline or a truncated file would silently be a different
owner and hide every row the app had written, so a corrupt file is a
named error at `Init`: the battery will not guess. Restore the file to
keep the rows, or delete it to mint a new identity (the old rows stay
with the old id).

The `LocalUser` middleware sets that identity into the request
context, and the battery installs the process-wide owner extractor
only when nothing else has. The middleware applies only while the boot
gate is armed ("How the window authenticates" below), which `Run` does
before the listener opens: a request that reached a handler then came
from the desktop window and carries the boot cookie, so it is the
local user. The identity's roles are `["admin"]` and its email is
`local@<id>`, satisfying `battery/auth`'s `User` interface
structurally without this package importing it.

What `Scope.OwnerField` means here: the same as anywhere else (hard
rule 6). Create stamps the owner column with the local user's id and
every query scopes to it. There is one user, so no cross-user read can
happen, but keep the field: it is what keeps the CRUD layer closed the
day the same binary also serves over `$PORT`, where requests are
anonymous.

What a host must NOT do:

- Wire `battery/auth` on top to give the window an owner. The desktop
  battery installs its own extractor. When auth's package `init()` has
  installed one, the local identity never applies: auth owns identity
  end to end, a signed-out desktop window stays anonymous, and the
  local identity's admin roles are exactly what must not shadow that
  decision.
- Expect an identity in `--serve` mode. The gate stays unarmed there,
  so `LocalUser` never applies: every anonymous browser would
  otherwise become the local admin. Such an app brings `battery/auth`
  or stays anonymous.
- Mint or edit the `identity` file by hand.

`Battery.LocalUserID()` returns the id for code outside a request, such
as a menu handler scoping its own query. A second desktop battery in
one process (a test binary with two apps) recognizes the first one's
extractor as its own and keeps its local identity
(`localuser_second_battery_test.go`).

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

### Local callers with no window: `OpenPrefixes`

A CLI or script on the same machine has no window and no cookie, so
every call gets 403. List the paths it uses in `Config.OpenPrefixes`
and protect them with the app's own auth (an API key, a bearer token,
`battery/auth`):

```go
d := desktop.New(desktop.Config{ID: "dev.gofastr.notes", OpenPrefixes: []string{"/api"}})
```

A path under an open prefix skips only the cookie check. The `Host`
pin stays, a forged cross-site write (a form-shaped request with no
CORS preflight) gets 403, a preflighted cross-origin JSON call is left
to CORS and the app's auth, and a request without the
cookie reaches the app with no user: the local identity goes only on
the window's requests, so the window's own `fetch("/api/...")` is still
the local user. The window keeps its JS bridge, because it never leaves
the app's origin. The listener stays on `127.0.0.1`; a caller in a
container needs `battery/service` and its `BindAddress`.

The match is on whole segments of the clean, unescaped path, so
`/apix`, `/api//x`, `/api/../admin` and `/api%2Fx` stay gated. `New`
panics on `/`, on anything under `/__gofastr/` (the bridge, the runtime,
the handshake) and on a malformed entry. `Run` logs the open prefixes
as a warning.

## Where a window may navigate

A window's main frame stays on the app's own origin. Each shell asks
the battery before a main-frame navigation commits, and the battery
allows only `http://<the loopback address Run bound>` and the
`about:blank` page a web view starts on. Anything else is cancelled:
an external link, `location.href = "https://…"`, a redirect to another
host, a `data:` URL. The page stays where it was and the shell logs a
warning naming the refused host (`desktop.NavigationHost`, which drops
the path and query so an OAuth code in the URL is never logged).
Iframes are not checked; the page's own CSP decides what it frames.

A foreign page in the app's window looks like the app to the user, and
native events are evaluated into whatever the window shows, so the
default is to refuse. An app that wants the old behaviour (an in-window
OAuth flow, say) sets `Config.DangerouslyAllowForeignNavigation: true`.
There is no "open in the system browser" hand-off yet: a refused link
does nothing. A request for a new window (`window.open`, a
`target="_blank"` link) opens nothing on any shell, the app's own pages
included.

How each shell enforces it:

| Shell | Hook | When it runs |
|-------|------|--------------|
| macOS | `WKNavigationDelegate` `decidePolicyForNavigationAction` | Before the request, main frame only |
| Windows | `ICoreWebView2::add_NavigationStarting` | Before the request and on each redirect; WebView2 raises it for the main frame only |
| Windows | `ICoreWebView2::add_NewWindowRequested` | Every new-window request is marked handled. WebView2 would otherwise open a second web view with no navigation policy |
| Linux | `decide-policy` on the response (`webkit_response_policy_decision_is_main_frame_main_resource`) | After the response arrives, before the page commits. WebKitGTK's navigation action has no main-frame flag, so a refused request still reaches the other host; nothing it returns renders |

A custom `Shell` gets the policy as `WindowConfig.AllowNavigation` and
must call it, on the UI thread, before a main-frame navigation commits;
a shell that ignores the field lets every navigation through.

A test checks the policy without a shell: `desktoptest.Harness`'s
`AllowsNavigation(url)` runs the battery's check with the harness's
bound address (`battery/desktop/navigation_policy_security_test.go`).
The macOS e2e (`phaseForeignNavigation`) and the Linux graphical test
(`TestNativeGTKWebKit`) drive a real web view at a second origin. The
Windows hooks have no test runner yet; `TestWindowsShellRefusesNewWindows`
checks the shell's source.

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

A call can wait on a person (a file picker, a permission alert), so the
call route runs on its own 11-minute budget instead of the app's
`RequestTimeout` and `HTTPServerTimeouts.WriteTimeout` (30 s and 60 s
by default). The macOS shell bounds each alert and panel at 10 minutes.
A plugin method that can hang should bound itself on its context.

The core capabilities:

| Capability | Methods | Permission |
|---|---|---|
| `window` | `title`, `setTitle`, `snapshot` | none (the page lives in that window; each method acts on the calling window, and a closed one answers `not_found`) |
| `windows` | `open`, `openSettings`, `focus`, `close`, `list`, `post`, `broadcast`, `self` | none (a page may only open the app's own screens; `close` refuses `"main"`) |
| `dialogs` | `openFile`, `saveFile`, `openFolder` | none (the user mediates every dialog; returned paths join the `fs` allow-list) |
| `clipboard` | `readText`, `writeText` | `clipboard:read`, `clipboard:write` |
| `notifications` | `show` | `notifications:show` |
| `fs` | `readText`, `writeText`, `stat` | `fs:read`, `fs:write` |
| `tray` | `setTitle` | none (the tray label is the app's own) |
| `state` | `get`, `set`, `delete`, `keys` | none (page keys live under the `page.` prefix only; see "App state") |
| `preferences` | `get`, `set` | none (only declared keys; the values live in the battery's own `settings` entry; see "Preferences") |

The macOS, Linux and Windows shells check a dialog's input the same way
(`desktop.CheckFileDialog`) and answer `invalid_input` instead of
opening a dialog it was not asked for: a filter extension (`png` or
`.png`; `*` alone matches every file) may not be empty or hold `/`,
`\`, `*`, `?`, `[`, `]` or `;`, so a filter named Text cannot also
match `*.exe`, and `saveFile`'s `defaultName` is a file name, with no
`/` or `\`. Filter names and the default name may not hold a NUL.

`fs` only touches paths a `dialogs` call returned during this process
(`Battery.AllowPath` for in-process grants). The path must arrive in
the exact cleaned absolute form the dialog returned: `a/link/../x` or
`a//x` is refused even when it cleans to a grant, because the OS opens
the raw string and walks `link/..` through the link. Its live symlink
resolution must be on the list too, and for a file that does not exist
yet that means its deepest existing parent's, so a symlink already in
place when the request arrives, or a missing parent replaced by one
after the grant, cannot widen access. It is
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
sticky). The alert speaks the calling page's language: its words are
the `desktop.permission_*` phrases, rendered under the bridge
request's locale. With no database on the app, grants fall back to memory with
a startup Warn.

A grant is keyed on the **capability and the permission**, not the
permission alone: the alert names the capability, so a plugin that
declares an existing permission string still gets its own prompt. The
decision is written on a context detached from the request, because a
page that aborts its own fetch must not be able to make the user's
Deny unwritable, which is the whole point of persisting it. The alert
itself is bounded by `modalTimeout` (10 minutes), not by the default
main-thread deadline. One alert shows at a time. A first-time call that
arrives while an alert is up waits for it and then reads the stored
answer; if its own request ends first, it returns `cancelled` without
prompting.

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
put on the script rail; no inline script and no new `data-cui-*`
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
name is refused naming the first registration site. The names
`available`, `call`, `on`, `off` and `manifest` are refused too:
`bridge.js` assigns each capability onto `NS.desktop`, where the
runtime already keeps those members. Bump
`Capability.Version` when you remove a method or change an input
shape.

## Menus

`Config.Menu` declares the native menu bar in Go. The shell wraps it
with the standard app menu (About, Quit) and an Edit menu; do not
declare your own Quit beyond the `Role: "quit"` item. The shell's own
words (those two menus, a role item's default title, the Windows
About box and accelerator names) are `desktop.menu_*`,
`desktop.about_*` and `desktop.accelerator_*` phrases. They speak the
first of the OS's preferred languages your catalog has (System
Settings on macOS, the display languages on Windows), else the one
`LANG` names. Your own item titles are `i18n.Phrase` values the shell
renders in that same language, so a catalog entry for the key
translates your menu with no code change. Define them at package
level, like every phrase. Each item is exactly one of:

- `Navigate`: a same-origin absolute path, dispatched as
  `window.__gofastr.navigate(path)`, a client-side nav with no reload.
- `Handler`: a Go `func(ctx context.Context) error`, run on a goroutine
  with a 30 s deadline; errors are logged with the item id.
- `Role`: `quit`, `about`, or `separator`, implemented by the shell.

`Key` is the item's accelerator: modifiers (`cmd`/`command`/`meta`,
`ctrl`/`control`, `alt`/`option`/`opt`, `shift`) joined by `+`, then
one key, as in `cmd+n` or `ctrl+shift+t`. Command maps to Control on
Windows and Linux. At least one modifier other than `shift` is
required, and `New` panics on a bare key such as `n` or `shift+n`:
every shell matches accelerators before the page sees the keystroke,
so that character could never be typed into the page.

<!-- gofastr:compile
import (
	"context"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/core/i18n"
)

func exportAll(ctx context.Context) error { return nil }
stmt: _ = menu
-->
```go
var (
	menuFile      = i18n.Define("notes.menu.file", "File")
	menuNewNote   = i18n.Define("notes.menu.new_note", "New note")
	menuExportAll = i18n.Define("notes.menu.export_all", "Export all…")
)

var menu = &desktop.Menu{Items: []desktop.MenuItem{
	{Title: menuFile, Children: []desktop.MenuItem{
		{Title: menuNewNote, Key: "cmd+n", Navigate: "/notes/new"},
		{Title: menuExportAll, Key: "cmd+e", Handler: exportAll},
		{Role: desktop.RoleSeparator},
		{Role: desktop.RoleQuit},
	}},
}}
```

## Settings window

`Config.Settings` describes a secondary window showing one of the
app's own screens:

```go
d := native.New(desktop.Config{
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
    Tooltip: trayTooltip,        // an i18n.Phrase, in the OS language
    Icon: appIconPNG(),          // PNG, rendered as an 18x18 template image
    Menu: &desktop.Menu{Items: []desktop.MenuItem{
        {Title: menuShow, Role: desktop.RoleShow},
        {Title: menuNewNote, Navigate: "/notes/new"},
        {Title: menuSettings, Role: desktop.RoleSettings},
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

Linux uses an app-owned local session-bus connection and the
StatusNotifierItem/DBusMenu protocols. It requires a compatible status host
(for example, a desktop panel with SNI support); it does not install a host,
enable desktop extensions, or implement an XEmbed fallback. Title/icon placement
and whether text is shown are the host's choice. `HideDock` has no Linux effect.
Linux accepts full-color PNGs up to 1 MiB and 256x256 pixels; an omitted icon gets
a small built-in fallback. Menus are bounded to 128 entries and eight levels;
text is valid UTF-8, NUL-free and at most 4096 bytes. Top-level Show and Quit
recovery actions are always added, even with a custom menu.

`CloseHidesWindow` on Linux hides only after a watcher has registered the item
and reports a live status host. Without that proof, close behaves normally.
Host loss/replacement, removal of this item from the watcher's registry, or bus
disconnection marks the tray unavailable and
restores a hidden window; a busy GTK thread delays restoration, which is retried
rather than forgotten. A functioning main loop is still required to show it.
`Window.Close` follows the close-button behavior, while explicit Quit always
exits. Linux tray application callbacks run one at a time with at most 16 queued
actions. Handler contexts retain the normal 30-second deadline, but a handler
that ignores cancellation can occupy that one worker until it returns, even
after shutdown; the tray does not wait indefinitely for it or spawn a replacement. Show,
host-loss recovery and Quit use independent bounded/coalesced paths, so that
handler does not trap a hidden window. Custom shells can opt into this contract
with `WindowConfig.OnTrayMenu`: invoke it off the UI thread on a bounded worker;
it returns only after the handler completes. `OnMenu` preserves the existing
asynchronous main-menu handler behavior. Broker requests are also bounded;
overload can reject an action rather than grow memory without limit. No protocol can prove that a
third-party host actually painted the icon: a visible tray still depends on
the desktop's host. On a host without a tray surface, Run logs a Warn naming
the host error and the rest of the app works.

## Window styles, frameless windows, widgets

A desktop window is not always a titled rectangle. A launcher bar, a
color picker, a "quick note" pad: these want no title bar, want to
float above the app, and want to let the page paint its own surface.
`WindowSpec.Style` (and `Config.Style` for the main window) describes
that chrome; `desktop.Widget` builds the common shape.

```go
d := native.New(desktop.Config{
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
`data-cui-window-drag` on the element that is the handle (a header
strip, the whole card, anything).

```go
html.Div(html.DivConfig{ExtraAttrs: html.Attrs{"data-cui-window-drag": ""}},
    render.Text("Quick note")),
```

The runtime's `desktop` module listens for `mousedown` on such an
element (or inside one) and calls
`__gofastr.desktop.window.startDrag()`, which any page code can call
directly. The request rides the WebView's script message channel
(`window.webkit.messageHandlers.gofastr`), not the HTTP bridge:
`performWindowDragWithEvent:` needs the mouse-down that is still the
current event on the UI thread, and by the time an HTTP round-trip
came back it no longer would be. WebKitGTK exposes the same handler
name, but its message arrives from the web process after GTK has moved
past the press, so the Linux shell starts the move from the last
primary-button press the web view saw and ignores the message once that
button is released. In a plain browser (`--serve`) there
is no message handler and the call is a no-op, so screens carrying the
attribute render unchanged there.

Only a primary-button press starts the drag, and a press on a control
inside the handle keeps its click: `button`, `input`, `select`,
`textarea`, `a[href]` and editable `[contenteditable]` elements never
drag, because once the native drag starts the host owns the mouse and
the click never lands. Mark any other clickable element inside the
handle (a menu trigger, a `role="button"` div) with
`data-cui-window-no-drag`:

```go
html.Div(html.DivConfig{ExtraAttrs: html.Attrs{"data-cui-window-drag": ""}},
    render.Text("Quick note"),
    html.Div(html.DivConfig{ExtraAttrs: html.Attrs{"data-cui-window-no-drag": "", "role": "button"}},
        render.Text("More")),
),
```

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

The style contracts are OS-neutral. macOS/arm64 and Windows/amd64
implement native chrome (see the platform package layout). Linux supports
standard GTK decorations, borderless windows, resizability and window-manager
placement/keep-above hints. Unified and hidden title bars fall back to GTK's
own decorations. `Transparent` works when a compositing manager is running and
the screen has an RGBA visual: the shell gives the window that visual, clears
the GTK window background and WebKit's page background, and the desktop shows
where the page paints nothing. A material needs more than that. The shell asks
for blur behind the window only from KWin on X11 with its blur effect on
(`_KDE_NET_WM_BLUR_BEHIND_REGION`), and tints the window with the GTK theme's
background color at 88% (`MaterialWindow`), 80% (`MaterialSidebar`) or 62%
(`MaterialGlass`) opacity. Without blur a material window stays opaque, since
a tint over a sharp desktop is harder to read than a solid window; a material
window that also sets `Transparent` gets plain transparency instead. GNOME has
no blur protocol, and the shell does not probe Wayland compositors for one.
`[data-cui-window-drag]` regions move a Linux window: the page posts its drag
message, and the shell starts a window-manager move from the primary-button
press the web view saw, only while that button is still held. On Windows 11 the shell asks DWM for dark frame colors,
rounded corners, and system materials. `MaterialWindow` maps to Mica,
`MaterialSidebar` maps to whole-window Mica Alt, and `MaterialGlass`
maps to whole-window Desktop Acrylic. DWM materials require Windows 11
build 22621 or newer; older systems fall back to the ordinary window
surface. The mask bits, the floating
non-activating panel bit, and the canJoinAllSpaces behavior were
verified against the SDK headers (`NSWindow.h`, `CGWindowLevel.h`)
and the live window is proven by the native e2e step
(`shell_darwin_e2e_test.go`, tag `desktop_e2e`): styleMask without the
titled bit, floating level, `isOpaque` NO, the `NSPanel` class, the
panel never becoming key, and the page learning its own window id
(every window carries its own `WKUserContentController` with its own
`BootstrapJS(id)` user script while sharing the main window's
`WKWebsiteDataStore`, so the cookie still crosses).

On Windows, `ChromeHiddenTitle` and `ChromeUnified` use a custom frame
while DWM keeps its native caption controls. Without a native menu, the
WebView can fill the title bar area. With a menu, the shell preserves the
caption and menu bands above the WebView so the page cannot cover them.
WebView2 browser data uses a profile derived from `Config.ID`, so apps with
the same title keep separate browser state.
The shell uses per-monitor DPI awareness so WebView2 text, menus, and
controls render at the display scale. WebView2 accepts CSS
`-webkit-app-region: drag` in this mode; the shared
`data-cui-window-drag` handle also starts a native drag.
`WebView2Loader.dll` and the Evergreen WebView2 Runtime are required to
launch the built executable.

## The macOS look: native chrome and the desktop theme

A web page in a default window looks like a website in a window. Two
layers together make it read as a Mac app: the window chrome is
native (materials, a unified title bar, focus state), and the page
wears a theme sized to the platform. Both are opt-in; an app that
never mentions them keeps the standard window and its own theme.

### Native chrome

```go
d := native.New(desktop.Config{
    ID:    "dev.gofastr.focus",
    Title: "Focus",
    Style: desktop.WindowStyle{
        Chrome:   desktop.ChromeUnified,
        Material: desktop.MaterialSidebar,
    },
    SidebarWidth: desktopui.DefaultSidebarWidth,
})
```

- `ChromeUnified` is the Notes and Finder shape: a transparent title
  bar, a hidden title, an empty toolbar attached so the unified
  toolbar style takes effect, and full-size content. The traffic
  lights sit over the page.
- `WindowStyle.Material` picks the native effect under the page:
  `MaterialNone` (the zero value, an opaque window),
  `MaterialSidebar` (the sidebar material under the sidebar zone
  only), `MaterialWindow` (under the whole window: `NSGlassEffectView`
  on macOS 26, `NSVisualEffectView` below it), and `MaterialGlass`
  (macOS 26 glass; below 26 it degrades to `MaterialWindow` with one
  Info log line per window). The macOS 26 check reads
  `NSAppKitVersionNumber` from the AppKit actually loaded.
- `Config.SidebarWidth` (and `WindowSpec.SidebarWidth` for another
  window) is the initial sidebar zone in points, `0..MaxSidebarWidth`
  (4096); 0 means no zone. The page reports later widths through
  `__gofastr.desktop.window.setChrome({sidebarWidth})`, which applies
  to the calling window only; a ResizeObserver on the sidebar element
  is the intended caller. Go code calls `Window.SetSidebarWidth`.
- `WindowStyle.TrafficLightInset` (`*desktop.Inset`, points from the
  system position, both values `>= 0`) moves the three window buttons.
  nil keeps the system position.
- Every titled chrome carries the miniaturizable mask bit, so the
  yellow button renders enabled. `ChromeNone` stays borderless.

`New` panics on an unknown material, a negative inset, or a sidebar
width out of range; `windows.open` refuses the same values with
`invalid_input`.

A material only shows where the page paints transparent. The desktop
layout below paints `html` and `body` transparent; a page that paints
its own background gets an ordinary opaque window over the effect.

Turning the web view's own background off goes through the KVC form of
the private `drawsBackground` setting, the same path
`WindowStyle.Transparent` uses and the one Tauri gates behind its
`macOSPrivateApi` flag. There is no public switch. App Review
tolerance of the KVC form is observed, not promised.

### Focus and Reduce Transparency reach the page

WebKit has no CSS signal for window activity and implements no
`prefers-reduced-transparency` query, so the shell pushes both states:

- `window_focus {"id"}` and `window_blur {"id"}` go to every window
  when any window becomes or stops being key
  (`windowDidBecomeKey:` / `windowDidResignKey:`).
- `reduce_transparency {"on": bool}` goes to every window on each
  change of the system setting. When it is on, the shell hides the
  effect views and restores an opaque window background, the same
  window `MaterialNone` gives. `Shell.Appearance()` answers the
  current state at any time, including before `Run`.
- `desktop.BootstrapJS(windowID, reduceTransparency)` carries the
  state at load, so the first paint is right.

The `desktop` runtime module turns those into two classes on
`<html>`: `desktop-inactive` while this window is not key (only for
events whose `id` is this window's), and `desktop-reduce-transparency`
while the setting is on. CSS reads the classes; no component ships
JavaScript for them. Go hosts get the same signals through
`WindowConfig.OnWindowFocus`, `OnWindowBlur`, and `OnAppearance`,
which the battery fills.

### The desktop theme

`battery/desktop/ui` (package `desktopui`) is the page half: one theme,
three layouts, and a few components, registered through the design
system's own machinery (`style.Theme`, `registry.RegisterStyle`,
`app.NewLayout`). Its token values are per platform; Windows and Linux
will swap values, never fields.

```go
import (
    appui "github.com/DonaldMurillo/gofastr/core-ui/app"
    desktopui "github.com/DonaldMurillo/gofastr/battery/desktop/ui"
)

site := appui.NewApp("focus")
site.WithTheme(desktopui.ThemeFor(runtime.GOOS))
layout := desktopui.Layout(
    appui.NewStaticComponent(desktopui.SourceList(desktopui.SourceListConfig{
        Label: "Focus",
        Sections: []desktopui.SourceSection{{
            Title: "Views",
            Items: []desktopui.SourceItem{
                {Label: "Today", Href: "/"},
                {Label: "History", Href: "/history"},
            },
        }},
        CurrentPath: "/",
    })))
site.Register("/", screen, layout)
```

What the theme changes from the framework theme, and nothing else:

| Token | Value |
|---|---|
| Fonts | `-apple-system, system-ui, ui-sans-serif, sans-serif`; mono `ui-monospace, 'SF Mono', Menlo, monospace` |
| Type scale | 10, 11, 13 (base), 15, 17, 22, 26 px, from the HIG type table (one CSS px is one point in WKWebView) |
| Radii | 0, 4, 8, 12, 16, capsule |
| Light background / text | `#FFFFFF` / `#000000` |
| Accent | `#007AFF`, the system blue (focus rings, selection, links) |
| Primary | `#0071E3` under white (4.7:1) in both schemes: `#FFFFFF` on `#007AFF` is 4.0:1, below the AA floor `Theme.Validate` enforces for filled buttons |
| Dark palette | background `#1E1E1E`, surface `#2A2A2A`, text `#F5F5F7`, accent `#0A84FF` |
| Touch target | 24 (the WCAG 2.5.8 floor; the web default is 44) |

The system color keywords (`Canvas`, `AccentColor`) would track the
user's settings, but the design system's color grammar refuses them
(`TestColorGrammarRefusesSystemKeywords`), so the nearest values stand
in. Values marked `// measured, unverified` in `theme.go` came from
captures, not from Apple sources.

The primary stays `#0071E3` on purpose. A filled button's label is 13px
regular, and WCAG only relaxes the floor to 3:1 for large text (24px, or
18.66px bold), so a large-text exception would not cover desktop buttons.
The system blue stays on Accent, where no white text sits on it.
`TestPrimaryStaysAAUnderWhite` fails if a theme swaps `#007AFF` into
primary.

`desktopui.Layout(sidebar)` is an `app.NewLayout` layout named
`desktop`. Its build renders the desktop frame (`.desktopui-frame`):
the sidebar component inside a `<nav aria-label="Sidebar">`, then the
primary. A nil sidebar renders the content column alone.

- `html` and `body` paint nothing, so the material shows. Under
  `--serve` in a browser the page falls back to the browser's
  background.
- The sidebar zone (`.desktopui-frame__sidebar`) is `--desktop-sidebar-width` wide (default
  `desktopui.DefaultSidebarWidth`, 220) and reserves
  `--desktop-sidebar-top-inset` (default `desktopui.SidebarTopInset`,
  52) above its first row for the traffic lights. The zone is
  transparent in light mode and a 2% wash of the text color in dark
  mode, where the dark sidebar material and the content background
  otherwise land on the same near-black.
- `<main>` is the one opaque region, on `--color-background`, with its
  leading top corner rounded into the window frame.
- The sidebar stays beside the content at every window width. The
  frame is the desktop's own rather than `ui.ContentRow`, whose
  narrow-screen stacking would put the source list above the page.

`desktopui.WindowLayout()` (named `desktop-window`) is for a window
with no sidebar: a settings window, an about panel. The page and the
content column paint nothing, so a whole-window material is the
surface, and the content column reserves the same 52-point
traffic-light zone at its top. Register a screen that lives in a small
window on this layout; `examples/desktop-focus` mounts `/settings` on
it and keeps Settings out of its sidebar, the macOS shape (the app
menu's Settings… item opens the window).

`desktopui.WidgetLayout()` (named `desktop-widget`) is for a floating
widget window (`desktop.Widget`): no chrome, a transparent page behind
the screen's own surface, no viewport-height floor, and
`--desktop-widget-padding` (default 8px) between the window edge and
that surface.

Every desktop frame sets `--ui-control-padding-y` to `4px` on the
page. With the theme's 24-point touch target (which comfortable
density turns into the controls' minimum height), buttons and text
fields come out about 28 points tall instead of 44 (see `gofastr docs
theming` for the `--ui-*` knobs).

Components:

- `SourceList`: the source-list sidebar. 28 px rows, gray section
  headers, optional leading icon and trailing count, and a selection
  fill that is a 5% mix of the text color; the accent is kept for the
  keyboard focus ring. The current row carries `aria-current="page"`
  and the runtime's `active` class in the server-rendered markup, so
  the runtime's active-link module can clear it on client-side
  navigation. `ui.Sidebar` stays the web sidebar; its look is fixed in
  its stylesheet and a desktop window never needs its drawer.
- `Glass`: a translucent surface (`backdrop-filter` blur and
  saturation, a translucent fill, a 1 px inset rim, a hairline edge,
  and a drop shadow; over a flat page the blur has nothing to show,
  and the edge and shadow are what keep the surface visible).
  `GlassConfig{Thick: true}` is the sheet and popover weight. It uses
  no SVG reference filters (WebKit bug 245510).
- `FloatingToolbar`: `ui.Toolbar` in a glass capsule, sticky over the
  content. The toolbar's roles and behavior are unchanged.
- `Inspector`: a labelled side panel on glass whose rows are
  `ui.DetailList`, with the label column capped so values keep their
  line. `InspectorSplit(content, inspector)` puts it in the trailing
  column at `--desktop-inspector-width` (260px) and wraps it below the
  content when the content would drop under 20rem.
- `Sheet` and `Popover`: the modal and non-modal overlay surfaces on
  thick glass. A preset widget paints its own opaque panel, so a glass
  sheet goes in as the widget's skeleton:
  `preset.Modal("confirm").Skeleton(desktopui.SheetSkeleton(desktopui.SheetConfig{Title: "Discard draft?"}))`.
- For segmented controls, use `ui.SegmentedControl`; the theme's
  tokens restyle it.

Under `desktop-inactive`, glass fills flatten, `--color-accent` dims
to the muted text color inside glass, and the selected source-list row
grays. Under `desktop-reduce-transparency`, glass goes to the opaque
surface color with no filter.

### Known gaps

Captured in `examples/desktop-focus` on macOS 26.5, light and dark,
active and inactive:

- Web idioms the framework components carry are unchanged: uppercase
  letter-spaced table and stat labels, a rule under the page header,
  filled destructive buttons.
- In light mode the whole-window material under a settings window
  renders as a flat mid-gray on macOS 26.5.
- Styled buttons keep the accent in an inactive window, where native
  default buttons go gray. WebKit's own form controls (checkboxes,
  selects) do follow the window's active state.

### Testing the chrome

- The fake shell (`battery/desktop/desktoptest`) records each window's
  sidebar width (`SidebarWidthOf(id)`), fires the appearance callback
  from `SetReduceTransparency`, and fires the focus callbacks from
  `FocusWindow` and `BlurWindow` on a goroutine, the way the native
  delegates do.
- `desktop.WindowState` reads the applied chrome back off the live
  window: `Material`, `TitlebarTransparent`, `TitleVisibility`,
  `ToolbarStyle`, `SidebarWidth`, the style mask, and `CGWindowID`
  (what `screencapture -l` takes).
- `NativeDriver.MakeKey(id)` makes one of the app's windows key, which
  fires the focus callbacks. Activating the app does not: a test
  process that is not frontmost never gets a key window back.
- The `desktop_e2e` step `ChromeContractOnTheOS`
  (`battery/desktop/native_e2e_test.go`) walks the real shell.

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

Every script the battery sends a page (events, posted messages, menu
and deep-link navigations) is wrapped in
`if (location.origin === "<app origin>") { ... }`, and the page checks
it when the script runs. A window that has followed an external link
to another site gets nothing: a page there that defines
`window.__gofastr.desktop._dispatch` or `window.__gofastr.navigate`
would otherwise receive every event, message and deep link, an OAuth
callback's code included. A script sent before `Run` binds its port
runs on no page.

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

`Config.DeepLink` makes the app answer URLs on its own scheme. A notes
app claims `gofastr-notes`, and `gofastr-notes://notes/123` opens or
focuses it on that note. The battery turns the URL into the same
client-side navigation a menu item performs.

```go
d := native.New(desktop.Config{
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

Build with `--scheme` to declare the packaged app's activation scheme.
macOS and Windows install or claim an OS handler as described below;
Linux builds embed command-line and session-bus handoff support; the separate
`desktop install` command advertises an OS handler for a tar bundle:

```sh
gofastr desktop build --id dev.gofastr.notes --name Notes --scheme gofastr-notes
```

On macOS, `--scheme` writes `CFBundleURLTypes` into the bundle's
Info.plist. On Windows, it embeds the scheme in the executable, and the
app registers it under the current user's `HKCU\Software\Classes`
whenever it starts. Windows passes each URL to the executable.
If the app is already running, the new process forwards the URL to its
open window. Windows refuses to replace a scheme owned by another
per-user or machine-wide handler. The embedded scheme supplies a default
`DeepLinkConfig` on Windows and Linux when the app has none; a custom
config must use the same scheme. Linux builds do not register a handler;
`desktop install` advertises it without changing saved default-handler
preferences. Without `--scheme`, these builds do not
enable native launch handoff. Development runs (`go run`, `gofastr
desktop run`) do not register a scheme.

### What a link becomes

The default mapping rewrites the scheme's host and path into the app's
path space, keeps the query, and drops the fragment:

| Link | Navigates to |
|---|---|
| `gofastr-notes://notes/123` | `/notes/123` |
| `gofastr-notes://notes/123?q=1` | `/notes/123?q=1` |
| `gofastr-notes://notes/123#sec` | `/notes/123` |
| `gofastr-notes://` | `/` |
| `gofastr-notes://notes/a%3Fb` | `/notes/a%3Fb` (the path stays escaped, so `%3F` and `%23` never start a query or fragment) |

The result must pass the same grammar a menu `Navigate` path passes
(leading `/`, no scheme, no control characters, no `.` or `..`
segments, percent-encoded ones such as `%2e%2e` included); a link
that cannot pass is dropped with a Warn. The grammar decodes each path
segment and also refuses one that decodes to a `/`, a `\` or a control
character. Go's `ServeMux` decodes a `{name}` wildcard, so
`gofastr-notes://files/..%2f..%2fsecret` would otherwise reach a
`GET /files/{name}` handler as `PathValue("name") == "../../secret"`,
and `%00` would reach it as a NUL byte. The query is not checked this
way: it is the page's to decode. A link that reaches an app with
no `DeepLink` config is dropped with a Warn as well. These are
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

Both scripts run only while the window shows the app's own origin
(the guard in "Native events reach every window" above); a window on
another site is skipped.

```js
__gofastr.desktop.on('deep_link', ({ url, path }) => { ... })
```

Links that arrive before the window is up (a cold launch: the OS may
deliver the URL event before the first window finished its boot
navigation) are queued, at most 16, oldest dropped first with a Warn,
and replayed in arrival order right after the boot navigation. A
link that arrives while the window is coming up waits behind them:
the battery checks for the window and writes its queue under the lock
the replay takes, so no link lands in the queue after the replay has
emptied it, and none overtakes a link queued before it. A
queued link is validated and mapped when it arrives, so `OnDeepLink`
runs once per link, never again at replay. Once the window is up, the
pending delivery queue is also capped at 16 (in addition to the link
currently being delivered); a busy page drops the oldest pending link
first with a scrubbed Warn instead of allowing an unbounded backlog.

### Platform handoff

On macOS the URL arrives as a GetURL Apple Event (class `'GURL'`, id
`'GURL'`). The darwin shell installs its handler before the run loop
starts, so it receives cold-launch links too. One goroutine hands the
URLs to the battery in the order the events arrived, at most 16
waiting.

On Windows, the URL arrives as a command-line argument. A named
per-app mutex keeps one process running; a second activation forwards
the URL to the existing window. Every window the app opens (main,
Settings, `OpenWindow`, `Config.Widgets`) shares one window class, and
the second process finds the first of them in Z-order, so any of them
accepts the forwarded URL. The main window is the one that comes
forward and navigates. A forward of several URLs reaches the battery
in order, through one goroutine, at most 16 waiting. A cold-launch URL waits until the first
page has completed its boot navigation. The battery validates the
scheme before it navigates or emits the event.

Linux's launch broker handles cold command-line URLs and forwards running-instance
activations over an app-owned local D-Bus connection, only when the binary has an
embedded scheme from `gofastr desktop build --scheme=...`. Both plain ELF and
tar.gz builds support this opt-in. Pass a URL as one argument to the executable
or archive's `launch.sh`; setting `Config.DeepLink` alone does not enable native
launch handoff. Building alone installs no OS handler or desktop association.
The separate per-user installer advertises the handler without changing saved
default-handler preferences. Neither command installs a D-Bus activation service. The mapping, queue and event remain host-independent.

The Linux broker requires `DBUS_SESSION_BUS_ADDRESS` to name an **existing local**
`unix:path=...` or `unix:abstract=...` session bus. Escaped address fields and an
optional GUID are supported; remote/TCP, autolaunch and missing buses are refused.
Launch from a supported desktop session with its existing bus address; the app
never creates a session bus as a fallback. If the bus disconnects after startup,
URL activation stops until the app restarts, but the broker does not terminate the
normal application or alter another GIO user's connection settings. Single-instance
coordination is scoped to that session bus, not globally across desktop sessions.
If a replacement bus starts while the original app remains open, a new launch can
become primary on the replacement bus; the old app must restart to regain URL
activation. Session-bus peers are not authenticated as trusted applications:
URLs still pass the normal validator and boot/session gates. A successful handoff acknowledges queue
acceptance, not completed navigation. URI input is limited to 2048 bytes, with at
most 16 pending native deliveries (plus one callback in flight); oldest pending
items are dropped on overflow. Shutdown discards queued work; a callback already
running is not forcibly interrupted.

Darwin/amd64 has no native shell; the mapping, queue and event run against the
test double on every OS.

### Testing

The harness plays the OS: `h.OpenURL("gofastr-notes://notes/123")` fires the
deep-link callback exactly as the shell would, before or after the
window is up (`battery/desktop/deeplink_test.go`). The race between a
link and the window coming up is pinned by a test hook in
`battery/desktop/deeplink_queue_test.go`. On Windows,
`battery/desktop/windows/activation_deeplink_windows_test.go` hands
`receiveDeepLink` the `WM_COPYDATA` payload a second instance sends, on
the main, Settings and widget handles. On a Mac, the
untagged suite exercises the real Apple Event handler: the test
builds an `NSAppleEventDescriptor`, registers through
`installDeepLinkHandler`, and invokes the handler IMP the way the
manager does (`battery/desktop/macos/shell_darwin_deeplink_test.go`); the
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
When archiving a macOS bundle on Windows, the zipper assigns executable modes
under `Contents/MacOS` because Windows does not expose Unix execute bits.
Failed archive writes close the output before removing the incomplete file.

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
  127.0.0.1 so tests can serve a feed locally. Every redirect target
  is held to the same rule: when an https URL redirects to plain http
  elsewhere, the fetch fails before the target is asked, and the Warn
  in the log names the scheme rule.

### Configuring the app

```go
d := native.New(desktop.Config{
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
unsupported host logs once and stops. So does a bundle whose
`CFBundleShortVersionString` is not semver (`1.0`): the check answers
`unsupported` with "this app's version is not major.minor.patch"
instead of blaming the feed. Log lines never carry the feed URL or a
filesystem path. A failed file operation logs the operation and the OS
error (`rename: permission denied`), and `codesign` or `open` output
names files relative to the bundle (`Notes.app: invalid signature`).

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
5. Read the extracted bundle's `Contents/Info.plist` (XML, the format
   `gofastr desktop build` writes) and refuse unless its
   `CFBundleShortVersionString` is exactly the feed's version and its
   `CFBundleIdentifier` is the running app's. A mislabelled archive
   would otherwise reinstall on every check or downgrade silently.
6. Run `codesign --verify --deep --strict` on the extracted bundle and
   refuse on failure.
7. Swap: move the current bundle to `<Name>.app.old`, move the new one
   into place, `open -n` the new bundle, and quit. The next launch
   removes a leftover `.old`. When the second move fails, the old
   bundle is moved back. If that rollback fails too, `<Name>.app.old`
   is the only copy of the app, and a later `install` refuses rather
   than clear it; the log says to rename it back to `<Name>.app`.

Every failure surfaces as one of the bridge's closed error codes with
a fixed message; no path or URL ever reaches the page.

One install runs at a time. A second `install` while one is running
(a double-clicked button) is refused with `invalid_input` ("an update
install is already running"), and once the swap has happened every
later call is refused too ("the update is installed; the app is
restarting"), so two runs never share `<Name>.app.new` or `.old`.

From Go, the same two steps are `d.CheckForUpdates(ctx)` and
`d.InstallUpdate(ctx)` (the latter is what a File menu
"Check for updates…" handler calls after showing the result).

### Release tooling

The CLI mints the signing pair and writes the feed:

```
gofastr desktop keygen -o path
    writes path (private, 0600, hex seed + hex public key)
    and path.pub (hex public key); refuses when either exists,
    and never writes through a symlink

gofastr desktop feed --key path --version 1.2.0 --notes "..." \
    --platform darwin-arm64 --archive Notes.zip \
    --url https://example.com/notes/Notes-1.2.0.zip [-o dir]
    writes manifest.json and manifest.json.sig into dir (-o defaults
    to .), computing sha256 and size from the archive file
```

`feed` refuses a version that is not semver, a platform key outside
the `GOOS-GOARCH` grammar, a non-https archive URL, an archive
that is not a zip with exactly one top-level `.app` directory, and an
archive whose `Contents/Info.plist` does not declare `--version` as its
`CFBundleShortVersionString` (build with the same `--version` you
publish). It also extracts the archive into a temporary directory with
the updater's own extractor, so anything a client would refuse is
refused at release time instead: a stray top-level file (`README.txt`,
`.DS_Store`), a zip with no directory entry for the bundle root, a
symlink. `gofastr desktop build` writes archives that pass. The
writer and the updater's verifier share one implementation
(`desktop.SignUpdateFeed`), so the format cannot drift between them.

Keep the private key out of the repository (it is written 0600 for a
reason) and ship only the `.pub` contents in the app's
`UpdateConfig.PublicKey`.

### Testing an app's update flow

`battery/desktop/update_harness_test.go` is the reference: an
`httptest.Server` serves the signed feed on 127.0.0.1, the battery's
update platform (the OS seam: running version and identifier, bundle
location, codesign, relaunch) is replaced through a test seam before `Run`, and
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
d := native.New(desktop.Config{
    ID:               "dev.gofastr.notes",
    Title:            "Notes",
    RememberWindows:  true,
})
```

`examples/desktop-focus` ships with the flag on.

### What is stored

One entry in the app state store (the next section): the `windows`
key of `state.json` (0600) in the app's data dir, next to `app.db`:

```json
{
  "entries": {
    "windows": {
      "main":     {"frame": {"X": 140, "Y": 90,  "Width": 800, "Height": 600}, "path": "/notes/42"},
      "settings": {"frame": {"X": 40,  "Y": 300, "Width": 500, "Height": 400}}
    }
  }
}
```

The window state is a typed client over that one entry, so the
remembered frames and every other state key land in one debounced
write and one quit flush. There is no separate `windows.json` anymore:
the feature was unreleased, so there is no migration. The state is the
battery's, never the OS's autosave (`NSWindow` frame autosave does not
follow the data dir and cannot store the path). Frames are screen
points, top-left based, the same convention `WindowStyle.X/Y` uses.
A corrupt or unreadable file is a Warn and a default launch, never a
crash.

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

## App state

A desktop app needs small durable state that is not worth a table:
which theme the page picked, the sidebar width, a value two windows
want to agree on. The battery ships one store for that,
`battery/desktop/appstate`, and serves a slice of it to the page.

### The store

`appstate.Store` is a durable key/value store backed by one JSON file,
`state.json` in the app's data dir:

```json
{
  "entries": {
    "page.theme": "\"dark\"",
    "windows": {"main": {"frame": {"X": 140, "Y": 90, "Width": 800, "Height": 600}, "path": "/notes/42"}},
    "settings": {"work_minutes": 50, "sound": "bell"}
  }
}
```

Values are stored as raw JSON: a value written through one Go type
reads back through another without ever being re-encoded through
`any`. Keys match `^[a-z][a-z0-9_.-]{0,63}$` (at most 64 characters);
the leading dotted segments are namespaces. The battery owns `windows`
(the remembered window state) and `settings` (the preferences); the
page owns `page.`. One value is capped at 64 KiB of encoded JSON
(`appstate.MaxValueBytes`).

Writes are debounced 500 ms (one timer, rescheduled per write, so a
burst lands once), the file is written whole with sorted keys at mode
0600, and `Flush` forces a write now and waits for any in-flight one,
so a clean exit never loses the last change (`Run` flushes on quit).
Each write lands through a fresh temp file renamed onto `state.json`:
a crash mid-write leaves the previous whole file, never a torn one,
and a symlink planted at the path is replaced, not followed. A
missing file is an empty store; a corrupt or unreadable one is a Warn
and an empty store, and the next write replaces it.

`Watch(fn)` calls `fn` with the key of every `Set` and `Delete`, on
`fn`'s own goroutine and never under the store's lock. The returned
`stop` removes the watcher.

### The battery accessor

`Run` opens the store after the app reports ready (`Init` resolves the
data dir inside `app.Start`) and before the shell opens the window,
always, not only when `RememberWindows` is set. The same store serves
the battery, the remembered windows, the preferences, and the page, so
one flush on quit carries all of it. `Battery.State()` returns it:

<!-- gofastr:compile
import (
	"log"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/battery/desktop/appstate"
	"github.com/DonaldMurillo/gofastr/battery/desktop/native"
)

var d = native.New(desktop.Config{ID: "dev.gofastr.notes", Title: "Notes"})

func applyZoom(z float64) {}
-->
```go
// A menu handler, a capability of your own, a goroutine:
if s := d.State(); s != nil {
    if err := s.Set("ui.zoom", 1.25); err != nil {
        log.Fatal(err)
    }
    z, ok, _ := appstate.Get[float64](s, "ui.zoom")
    if ok {
        applyZoom(z)
    }
}
```

`State()` is nil before `Run`: the data dir is not resolved until
`Init` runs, so there is no store to hand out yet.

### The state capability

The page gets the same store through the `state` capability, ungated
like `window.setPath`: the page is the app, and the window id is a
claim.

```js
await __gofastr.desktop.state.set({key: "page.theme", value: "dark"})
const {value} = await __gofastr.desktop.state.get({key: "page.theme"})
// value === "dark"; an absent key answers null
const {keys} = await __gofastr.desktop.state.keys({prefix: "theme"})
await __gofastr.desktop.state.delete({key: "page.theme"})
```

- `get {key}` answers `{value}`: the stored JSON, or `null` when the
  key is absent.
- `set {key, value}` stores any JSON value (a missing `value` stores
  `null`), capped at 64 KiB of compact JSON, measured on the compact
  form so whitespace cannot smuggle the difference.
- `delete {key}` removes the key.
- `keys {prefix}` answers the stored page keys that start with
  `page.` + `prefix`, sorted.

Page keys are confined to the `page.` prefix: every method refuses any
other key with `invalid_input`, so the battery's own entries
(`windows`, `settings`) are never reachable from the page. The store's
key grammar and its length cap apply on top.

### The state_changed event

After every successful `set` or `delete` the battery emits
`state_changed` with `{key}` to every open window. That is the
cross-window sync a settings widget wants: one window changes the
theme, every window hears it.

```js
__gofastr.desktop.on("state_changed", async ({key}) => {
    if (key !== "page.theme") return
    const {value} = await __gofastr.desktop.state.get({key: "page.theme"})
    applyTheme(value)
})
```

- `battery/desktop/appstate/appstate_test.go` overrides the package's
  unexported debounce delay, so the debounce, the flush, the key
  grammar, the size cap, the 0600 mode, the corrupt-file posture, and
  a flush racing the timer under `-race` are all tested without
  waiting real half-seconds.
- `battery/desktop/appstate/flushrace_test.go` pins the quit-time
  ordering (a flush waits for an in-flight write; the debounce write
  snapshots under the write lock), and
  `persist_security_test.go` pins the atomic replace, the
  symlink-not-followed rule, and readers never seeing a partial file.
- `battery/desktop/cap_state_test.go` drives the capability from the
  page side through `desktoptest.Run`: the round trip
  (`TestStateRoundTripThroughPage`), the `page.` rule
  (`TestStateKeysConfinedToPagePrefix`), the size cap
  (`TestStateCapsValueSize`), a stranger's 403
  (`TestStateStrangerRefused`), the event reaching a second window
  (`TestStateChangedReachesSecondWindow`), and a value surviving a
  second battery on the same data dir
  (`TestStateSurvivesSecondBattery`).
- The quit flush is observable the way the window store's was: set a
  value, `h.Quit()` before the debounce elapses, and read `state.json`
  (0600) in the data dir.
- The native e2e phase step `PageStateRoundTrips`
  (`battery/desktop/native_e2e_test.go`, tag `desktop_e2e`) runs the
  round trip inside the real WKWebView through `EvalAsync` and reads
  `state.json` from the data dir.

## Preferences

A desktop app needs a handful of settings that are not worth a table
and not worth hand-building a screen for: the work and break minutes,
whether a save notifies, where exports go. The battery takes those as
declarations on `Config.Preferences`, stores them in the app state,
reads them back typed, and renders them as a form.

### The declaration

`examples/desktop-focus` declares its five preferences on `Config`:

<!-- gofastr:compile
import (
	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/battery/desktop/native"
	"github.com/DonaldMurillo/gofastr/core/i18n"
)

func intPtr(n int) *int { return &n }

var (
	prefWorkMinutes    = i18n.Define("focus.pref.work_minutes", "Work minutes")
	prefWorkMinutesTip = i18n.Define("focus.pref.work_minutes.help", "Length of one work session.")
	prefBreakMinutes   = i18n.Define("focus.pref.break_minutes", "Break minutes")
	prefNotifyOnDone   = i18n.Define("focus.pref.notify_on_done", "Notify when a session ends")
	prefTrayCountdown  = i18n.Define("focus.pref.tray_countdown", "Countdown in the menu bar")
	prefSound          = i18n.Define("focus.pref.sound", "Session sound")
)
stmt: _ = d
-->
```go
d := native.New(desktop.Config{
    ID:    "dev.gofastr.focus",
    Title: "Focus",
    Preferences: []desktop.Preference{
        {Key: "work_minutes", Label: prefWorkMinutes, Help: prefWorkMinutesTip, Kind: desktop.PreferenceInt, Default: 25, Min: intPtr(1), Max: intPtr(180)},
        {Key: "break_minutes", Label: prefBreakMinutes, Kind: desktop.PreferenceInt, Default: 5, Min: intPtr(1), Max: intPtr(60)},
        {Key: "notify_on_done", Label: prefNotifyOnDone, Kind: desktop.PreferenceBool, Default: true},
        {Key: "tray_countdown", Label: prefTrayCountdown, Kind: desktop.PreferenceBool, Default: true},
        {Key: "sound", Label: prefSound, Kind: desktop.PreferenceChoice, Default: "chime", Choices: []string{"none", "chime", "bell"}},
    },
})
```

`Key` matches `^[a-z][a-z0-9_]{0,63}$` and is unique (dots are
refused: they would collide with the app state's namespace
separators). `Label` and `Help` are `i18n.Phrase` values
(`examples/desktop-focus/phrases.go` defines them at package level);
the screen and the capability render them in the request's language.
`Label` is required. `Default` must match `Kind`: bool,
int, string, or (choice) a string listed in `Choices`. `Min` and `Max`
bound an int (both optional, but `Min` must not exceed `Max`) and are
refused on every other kind; `Choices` belongs to choice and is
refused everywhere else. `New` panics on a declaration that breaks any
of that, naming the key: it is a wiring error, the same posture as a
bad menu. `New` also takes a defensive copy of the list (`Choices`,
`Min`, `Max` included), so mutating the Config afterwards changes
nothing.

Storage is one entry in the app state store: key `settings`, value one
JSON object mapping preference keys to typed values
(`{"work_minutes":50,"sound":"bell"}`). The state capability confines
page keys to `page.`, so this entry is never reachable from the page.
A stored key the app no longer declares is left in the file and simply
never read; a stored value that fails its own declaration (a
hand-edited `state.json`) falls back to the default with a Warn.

### The typed reads

<!-- gofastr:compile
import (
	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/battery/desktop/native"
)

var d = native.New(desktop.Config{ID: "dev.gofastr.focus"})
-->
```go
p := d.Preferences()
p.Int("work_minutes")    // 25
p.Bool("notify_on_done") // true
p.String("sound")        // "chime"
p.All()                  // every declared key, stored-or-default
p.Declared()             // the validated declarations, in order
```

`Preferences()` is usable from `New` on. Reads consult the store `Run`
opened; before `Run` there is no store, so they answer the declared
defaults. That is also the whole behavior in `--serve` mode (no `Run`,
no window, no store): the app reads its defaults and cannot save new
ones. `Bool`, `Int`, and `String` panic on an undeclared key or a kind
mismatch: like a bad Config, a programming error you want loudly.
`Set(key, value)` validates the kind, range, and choice, stores, and
returns an error naming the key on refusal; before `Run` it answers
`unsupported`. It accepts the JSON forms of the kind's Go type (bool,
whole number, string) plus the string spellings the runtime's form
serializer produces (`"true"`, `"30"`): app code passes the typed
value, the form path passes the string. JSON `null` is refused on
every kind: no rendered control produces it, and silently storing the
zero value is exactly what the validation exists to stop.

### The preferences capability

The page gets the same surface through the `preferences` capability,
ungated like `state` (the page is the app, and only declared keys
exist):

```js
const {values, declared} = await __gofastr.desktop.preferences.get()
const out = await __gofastr.desktop.preferences.set({values: {work_minutes: 50, sound: "bell"}})
```

- `get {}` answers `{values, declared}`: every declared key's
  stored-or-default value, plus the declarations (key, label, help,
  kind, default, choices, min, max). Label and help arrive as text in
  the calling page's language.
- `set {values}` applies every entry through the same validation as
  `Preferences.Set`. The whole call is all-or-nothing: every entry is
  validated first, the first invalid one refuses the call with
  `invalid_input` naming the key, and nothing is stored. A success
  answers `{values}`: every declared key with its value after the
  call.

After every successful set the battery emits `preferences_changed`
with `{keys}` (sorted) to every open window, the same cross-window
sync `state_changed` gives page keys:

```js
__gofastr.desktop.on("preferences_changed", ({keys}) => {
    if (keys.includes("work_minutes")) rerender()
})
```

### The screen

The battery cannot reach the render pipeline, so the screen is a
builder the host mounts:

```go
site.Register("/settings",
    desktop.PreferencesScreen(d, desktop.PreferencesScreenPath("/settings")),
    layout)
```

The screen's title is "Settings". It renders one `framework/ui` form
with one field per declared preference: bool as the hidden+checkbox
pair the entity screens use (the runtime's serializer collapses the
pair to one scalar), int as a number input carrying Min and Max (the
range hint joins the field's help), string as a text input, choice as
a select. Field ids follow the `f-<key>` shape the screen has always
used.
Zero CSS, zero hand-rolled structural markup; a missing primitive
would be a gap to fix upstream, never a local div. Mount it where
`Config.Settings.Path` points (the settings window opens that path) or
anywhere else; `PreferencesScreenPath` names the mount path and
panics on one that is not a same-origin absolute path, the grammar
`Config.Settings.Path` is held to.

Saving goes through the runtime's form intercept (`data-cui-rpc` on the
form, the entity screens' shape) to the battery's own route,
`POST /__gofastr/desktop/preferences`, which sits behind the same
session gate as every other `/__gofastr/desktop/*` route. The route
reads the serializer's JSON body (bools and numbers arrive as the
strings the controls carry; a blank int means "not provided", so it is
skipped and the stored value stands; the CSRF token `ui.Form` stamps
is skipped too), applies every entry through the same all-or-nothing
path the capability uses, and answers the shapes the runtime
understands:

- a refusal is status 400 with the validation envelope
  `{"error": ..., "fields": {key: [msg]}}`, which the `formerrors`
  module renders into the named fields;
- a success is a plain 2xx, and the form's `data-cui-rpc-navigate`
  back to the mount path re-renders the page with the saved values,
  the same landing a saved resource form gives.

In `--serve` mode the route answers 503 naming what is missing (the
desktop host's app state store); the screen still renders the declared
defaults.

### The examples

`examples/desktop-focus` declares the five preferences above, mounts
the screen at `/settings`, and reads them through `d.Preferences()`
(`Engine.Start` sizes a session from `work_minutes`,
`completeSession` gates the notification on `notify_on_done`, the tray
countdown on `tray_countdown`). `examples/desktop-notes` declares
`notify_on_save` and `export_folder` the same way. Both dropped the
hand-built settings entity, resource config, screen, and
`/settings/{id}` route they carried before.

### Testing seams

- `battery/desktop/preferences_test.go` covers the declaration
  validation panics, the defensive copy, the typed reads before and
  after a store exists, the reader panics, `Set`'s coercion and
  refusal table, the all-or-nothing batch, the undeclared-key and
  corrupt-value fallbacks, and persistence across a second battery on
  the same data dir.
- `battery/desktop/cap_preferences_test.go` drives `get`/`set` through
  `desktoptest.Run`: the values, the declarations, the event's keys,
  and the refusals naming the key.
- `battery/desktop/preferences_screen_test.go` mounts the screen on a
  harness app and walks the form: the rendered fields, the form-shaped
  POST, the 400 envelope on a bad int, the blank-int rule, and a
  stranger's 403 on the form route.
- The examples' suites walk the user's flow end to end; their
  `desktop_e2e` tests (tag `desktop_e2e`) flip the real checkboxes,
  change the work minutes, click Save in the real WKWebView, reload,
  and assert the form shows what `d.Preferences()` answers.
  The focus suite also submits a value over the declared Max and
  asserts the route's validation envelope renders into the field (a
  visible role=alert message), not just a status code.

## The desktop-focus example
`examples/desktop-focus` is the second dogfood app of
`battery/desktop`. Where `desktop-notes` proves the host (one window,
one entity, the bridge), desktop-focus uses every app-quality feature
the battery ships: window styles, secondary windows, the tray, deep
links, notifications, cross-window messages, a plugin capability, and
the updater. It is a
pomodoro timer because a timer exercises all of them at once: a
countdown wants the menu bar, an ending session wants a notification,
a floating widget wants window styles, and "start on this task" wants
a deep link.

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
| Preferences | `Config.Preferences` (five keys) rendered by `desktop.PreferencesScreen` at `/settings`, read by the engine through `d.Preferences()` |
| Tray countdown | `Engine.Tick` calls `SetTrayTitle` with `mm:ss` while a session runs, back to the app name when idle (the `tray_countdown` preference) |
| Notifications | `Engine.completeSession` through `d.Notify`, gated by the `notify_on_done` preference |
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
  `native.Shell()` is the platform default a host passes (nil selects
  the unsupported shell in `desktop.New`); `native.New(cfg)` passes it
  for you.
- `Window.Native()` returns the raw native web view handle
  (`WKWebView*`) for the one platform tweak the model does not cover.
- `Window.Eval(js)` runs a one-off script in the page.
- `Register(Capability)` exposes any Go function to the page.
- `app.Router()` is untouched: any route, any Go library.

## The `gofastr desktop` verbs

| Verb | What it does |
|---|---|
| `build ...` on macOS | Cross-compiles for darwin/arm64 with `CGO_ENABLED=0` and writes `<o>/<Name>.app` (a `GOARCH` other than arm64 is refused; on an Intel Mac the build warns that the bundle will not launch there): Info.plist, the binary, PkgInfo, and an icon.icns built in pure Go from the PNG. Signing is ad-hoc by default when `codesign` is on PATH; `--sign` selects an identity, and `--no-sign` skips signing. `--notarize` submits and staples the bundle. `--scheme` writes the URL scheme to Info.plist. |
| `build ...` on Windows/amd64 | Cross-compiles with `CGO_ENABLED=0` and writes `<o>/<Name>.exe` beside `WebView2Loader.dll`. The executable includes file and product version details, a multi-size icon from `--icon` (PNG; generated by default), and no console window. `--scheme` embeds the URL scheme; the app registers it for the current user at startup. The target machine needs the Evergreen WebView2 Runtime. |
| `build ...` on Linux/amd64 or Linux/arm64 | Writes a plain ELF executable by default. `--format=tar.gz` opts into a release archive with an icon, launcher, desktop-entry template, runtime README and checksum manifest. `--scheme` embeds command-line/session-bus activation, not an OS handler. It does not install the app or bundle system libraries. |
| `install --bundle=<extracted-directory>` on Linux | Validates and installs a bundle for the current user, including a desktop entry and any embedded URL handler. No default-handler or autostart preference changes. |
| `uninstall --id=<reverse.dns>` on Linux | Removes an unchanged owned installation; preserves application and browser-profile data. |
| `types [--pkg] [--out=desktop.d.ts]` | One headless manifest run of the built app, written out as a `.d.ts`. |
| `keygen -o=<path>` | Mints the auto-update signing pair: the private key (0600) and `<path>.pub`. Refuses when either path already exists, a symlink included, and leaves no half pair behind. |
| `feed --key --version --platform --archive --url [--notes] [-o]` | Writes the signed `manifest.json` and `manifest.json.sig` the updater verifies; refuses a `--version` that differs from the bundle's `CFBundleShortVersionString`. |

`--id` must be reverse-DNS (`[A-Za-z0-9.-]+` with a dot), `--name`
printable with no path separator and at most 64 characters, and every
plist value is XML-escaped. On macOS `--version` must be semver
(`major.minor.patch` with an optional `-pre-release`, no build metadata,
no leading zeros, at most 64 characters), the form the updater compares;
`desktop.ValidateAppVersion` is the same check.

### A binary whose desktop mode is a subcommand

`build` links the package's `main` and launches it with no arguments
when the user double-clicks it. When the binary's no-argument mode is a
web server and its window lives behind a subcommand (`myapp desktop`), a
double-click starts that server with no window, and on Windows with no
console either. Bake the subcommand in with `--launch-args`, and read it
in `main` through `desktop.LaunchArgs`:

```sh
gofastr desktop build --id=dev.example.app --name=MyApp \
  --launch-args=desktop --ldflags="-X main.version=1.2.3"
```

<!-- gofastr:compile
import (
	"os"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
)

func runDesktop([]string) {}
func runServer([]string)  {}
-->
```go
func main() {
	args := desktop.LaunchArgs(os.Args[1:])
	if len(args) > 0 && args[0] == "desktop" {
		runDesktop(args[1:])
		return
	}
	runServer(args)
}
```

`LaunchArgs` puts the baked arguments in front when the binary got none,
only the `-psn_...` serial number older macOS passes, or only deep links
on the scheme `--scheme` embedded (a cold-launch link arrives as the
argument). Any other arguments, and every argument of a build made
without `--launch-args`, come back unchanged, so `myapp serve` from a
terminal still serves. `--launch-args` takes up to 16
whitespace-separated words; one argument holding a space is not
expressible. `--ldflags` appends linker flags to the build's own.

### Building with plain `go build`

A release script can skip `gofastr desktop build`. Link with the same
flags it uses (`-trimpath -ldflags "-s -w"`, plus `-H windowsgui` on
Windows so no console opens; add `-X github.com/DonaldMurillo/gofastr/battery/desktop.builtDeepLinkScheme=<scheme>`
for a deep-link scheme). On Windows, put `WebView2Loader.dll` beside the
`.exe`: take `runtimes/win-x64/native/WebView2Loader.dll` from the
Microsoft.Web.WebView2 NuGet package, version 1.0.4258.31, the one
`build` pins, after checking the package's SHA-256 is
`56f7f4b8bf9aee4b8efefbbdd4f67d5f74ebd1b100ed0806da71bf76af481aa9`.
Without the DLL the window cannot open. A plain
`go build` has no version resource or icon; those come from `build`.

### A server build that never opens a window

Linking `battery/desktop/native` links the Linux GTK shell, which loads
GTK through the glibc loader, so a `CGO_ENABLED=0` Linux binary comes out
dynamically linked and does not start on Alpine or `scratch`. Build the
server image with the `gofastr_nodesktop` tag:

```sh
CGO_ENABLED=0 GOOS=linux go build -tags gofastr_nodesktop -o app ./cmd/app
```

The tag leaves every platform shell out, on every OS. `native.Shell()`
then answers the unsupported shell, so `Run` returns its unsupported-platform
error instead of opening a window. Everything else in
`battery/desktop` stays.

## Linux runtime and native tests

Linux menus and the About box use the language named by
`LANGUAGE`, `LC_ALL`, `LC_MESSAGES`, or `LANG`. Permission prompts and
file chooser labels use the calling request's language.

On Linux, `gofastr desktop build --id=dev.example.app --name=MyApp`
writes `dist/MyApp`, a plain ELF executable. It uses the current architecture;
set `GOARCH=amd64` or `GOARCH=arm64` to select the other supported target.
This default (also selected by `--format=executable`) remains unchanged:
no installer or desktop entry, and `--icon` is rejected. `--version` is
accepted but does not add Linux executable metadata. Both Linux formats
accept `--scheme` to embed opt-in activation support and reject macOS
signing/notarization options rather than ignoring them. An explicit
`--format` is supported on Linux only.

For artifact-only packaging, opt into a tar archive:

```sh
gofastr desktop build --id=dev.example.app --name=MyApp \
  --version=1.2.3 --format=tar.gz --icon=app.png
```

This writes `dist/dev.example.app-1.2.3-linux-amd64.tar.gz` (or `arm64`),
with a same-named root directory containing:

- `bin/dev.example.app`: the CGO-free executable; its filename uses the
  stable app ID, not the display name.
- `launch.sh`: an executable launcher that forwards arguments unchanged
  and preserves the working directory. Keep it beside `bin/`; it is not
  designed to be moved alone or invoked through a symlink elsewhere.
- `icon.png`: a validated, re-encoded PNG from `--icon`, or the same
  generated flat-square default used by the other desktop builders.
  Input PNGs are limited to 16 MiB and 4096 pixels per dimension.
- `dev.example.app.desktop.in`: an **inert template**, not an installable
  desktop entry. Its `@BUNDLE_ROOT@` paths require a packager to render
  the final absolute installation path. `desktop install` instead generates
  its own validated desktop entry; do not install or trust the template as-is.
  Other packagers must
  apply the Desktop Entry Specification's string escaping and separate
  `Exec` argument quoting/field-code escaping, not naive text replacement.
  In particular, literal `%` in `Exec` becomes `%%`, and executable paths
  containing `=` are unsupported by the specification.
- `README.txt`: runtime prerequisites and the packaging limitations.
- `manifest.json`: schema version 1, app identity/version/platform,
  `entry_point`, required runtime descriptions, and a sorted `files` list
  with each payload's relative `path`, octal `mode`, byte `size` and
  `sha256`. The manifest does not list itself; checksums are not signatures
  or proof of publisher identity. An opted-in scheme adds `embedded_scheme`
  and the existing-local-session-bus runtime prerequisite.

Extract the full archive and run its `launch.sh`. This does not register a
menu entry, URL scheme, default handler or updater. No GTK, WebKit or other
shared libraries are copied into the archive; retain the application's own
licenses and notices when distributing it. Archive entry order, modes and
timestamps are deterministic for identical payload bytes. Reproducible Go
executables additionally require pinned source, modules, toolchain, build
settings and VCS metadata. Archive versions use `[0-9]+(\.[0-9A-Za-z]+)*`
(at most 64 characters); the complete archive filename must fit 255 bytes.

For explicit URL handoff, build with a scheme and pass the URL as one argument:

```sh
gofastr desktop build --id=dev.example.app --name=MyApp --scheme=my-app
./dist/MyApp 'my-app://notes/123'
```

This requires an existing local session bus even on the first launch. A second
launch on the same bus forwards to the primary before starting another app or
GTK shell. It does not make the scheme clickable in a browser: OS handler
installation is a separate `desktop install` operation; default-association
policy remains with the desktop/user.

### Per-user Linux installation

Build a `--format=tar.gz` bundle, extract its whole root directory, then run:

    gofastr desktop install --bundle=/absolute/path/dev.example.app-1.2.3-linux-amd64
    gofastr desktop uninstall --id=dev.example.app

Install accepts an extracted schema-version-1 GoFastr bundle for the current
architecture, not an arbitrary directory or a tar filename. It validates the
fixed inventory, SHA-256/size metadata, ELF platform/entry point and PNG before
installation, and checks each payload again while copying. Checksums detect
changes, not publisher identity: install only applications you trust. It does
not run the supplied launcher or application during installation.

The bundle goes under `$XDG_DATA_HOME/gofastr/apps/<ID>` (default
`$HOME/.local/share/gofastr/apps/<ID>`), with a real desktop entry at
`$XDG_DATA_HOME/applications/<ID>.desktop` and an argument-preserving launcher
at `$HOME/.local/bin/<ID>`. A nondefault XDG_DATA_HOME does not move the bin
launcher. Empty/relative XDG_DATA_HOME falls back to the default. Add
`$HOME/.local/bin` to PATH yourself if your desktop does not already include it.
New directories/executables are owner-only 0700; other installed files are 0600.
Existing directory permissions are not changed.

Only a bundle carrying `embedded_scheme` gets `MimeType=x-scheme-handler/...`
and `%u`. Installation advertises an available URL handler and refreshes only
that applications directory with `update-desktop-database` when available.
Missing/failing desktop-file-utils produces a warning and an explicit refresh
command; installed files remain. No default-handler preferences or autostart
entries are written. A desktop may choose the new handler when no saved default
exists; choosing between handlers is the desktop/user's policy. The native
activation path still requires the existing local Unix session bus described
above. This is not a D-Bus activation service.

The installer generates a real desktop entry; it never trusts or naively
substitutes the archive's inert `.desktop.in` template. Paths are escaped for
the desktop-entry and argument layers. Resolved installation paths must be
absolute UTF-8 and contain neither '=' nor control bytes. Symlinked destination components,
foreign-owned ancestors, and unsafe writable directories are refused (trusted
sticky temp roots are permitted for isolated tests). This does not protect
against another malicious process running as the same UID.

Existing destinations are refused, not overwritten. Quit the app, uninstall
its unchanged installation, then install a new bundle. Uninstall verifies the
recorded payload/launcher/desktop-entry contents and ownership/modes, refusing
changed, missing, or extra files rather than deleting them. It never removes
application data, cookie/profile data, preferences or unrelated launchers.
It does not stop already-running instances. This is not an atomic upgrade or
signed updater; an interrupted operation can require manual inspection.

### Runtime prerequisites

Linux builds require no C compiler or GTK development headers:

```sh
CGO_ENABLED=0 go build -o dist/my-app ./cmd/my-app
```

Install GTK 3 and WebKitGTK 4.1 runtime libraries on the machine running the
app. Debian/Ubuntu package names are `libgtk-3-0` (or `libgtk-3-0t64`) and
`libwebkit2gtk-4.1-0`; Fedora uses `gtk3` and `webkit2gtk4.1`. WebKitGTK 2.40
or newer and glibc 2.34 or newer are required. Run from an X11 or Wayland
desktop session; missing libraries and missing displays produce actionable
errors from `Run`. System notifications also need optional `libnotify` and a
notification daemon. The shell queries the current daemon's capabilities for
each notification: plain-body daemons receive literal text; markup-capable
or unknown daemons receive escaped body text. Empty or failed capability
queries retain the conservative escaped fallback.

libnotify performs synchronous native calls on the GTK thread. A caller on
another goroutine can receive `ErrCancelled` while a daemon reply is still
pending, but cancellation does not preempt that native call or retract a
notification already sent. GTK work and shutdown wait for the reply or the
native proxy's default timeout; calls made on the GTK thread also wait there.
The bounded test below demonstrates recovery after releasing a held reply,
not cancellation of an unresponsive daemon. Existing GTK window-manager limits apply under Wayland,
including requests to place a window at an absolute screen position.

The shell supports native menus (Command accelerators map to Control), file
and folder pickers, permission dialogs, clipboard text, secondary windows,
JavaScript evaluation and PNG snapshots. It uses the same document-start
bootstrap and authenticated loopback bridge as the other native shells.

An opt-in test exercises real GTK and WebKit, including shared window cookies,
async JavaScript, native permission dialogs, clipboard, cancellation and PNG
capture. A separate-process test covers the real battery boot/session gate
and authenticated clipboard bridge. Neither requires an external browser:

```sh
CGO_ENABLED=0 GOFASTR_LINUX_NATIVE=1 go test ./battery/desktop/linux -run '^TestNativeGTKWebKit$' -count=1 -v
CGO_ENABLED=0 GOFASTR_LINUX_NATIVE=1 go test ./battery/desktop/linux -run '^TestNativeGoFastrBridge$' -count=1 -v
```

GTK and WebKit bind to the first thread that initializes them, so every shell
lifetime in a process runs its GTK work on one toolkit thread, started on the
first `Run` and locked to its OS thread for the life of the process. A
lifetime releases ownership when `Run` returns, and the next `Run` starts a
new window, a new ephemeral cookie store and the same thread. A `Run` while
another lifetime is live returns an error instead of sharing GTK's main loop.
A shell callback that calls `runtime.Goexit` ends the toolkit thread for good,
and every later `Run` returns an error. `TestNativeGTKLifetimes` runs three
lifetimes in one process, each started from a goroutine whose thread then
exits, and checks the page, the cookie store, the concurrent refusal and that
all three ran on one thread:

```sh
CGO_ENABLED=0 GOFASTR_LINUX_NATIVE=1 go test ./battery/desktop/linux -run '^TestNativeGTKLifetimes$' -count=1 -v
```

`TestNativeWindowEffects` reads screen pixels. Before a compositing manager
runs, a `Transparent` window must stay opaque. With `xcompmgr` running it must
show the root window's gray through the page, a glass window without blur must
stay opaque, a drag message with no held press must not move the window, and
an `xdotool` press-and-drag on a `data-cui-window-drag` handle must move it by
the dragged distance. It needs `xcompmgr` and `xdotool`;
`GOFASTR_LINUX_COMPOSITOR=1` turns a missing tool into a failure instead of a
skip:

```sh
CGO_ENABLED=0 GOFASTR_LINUX_NATIVE=1 GOFASTR_LINUX_COMPOSITOR=1 GDK_BACKEND=x11 xvfb-run -a   go test ./battery/desktop/linux -run '^TestNativeWindowEffects$' -count=1 -v
```

`TestNativePickersAutomated` drives the real GTK file choosers with `xdotool`.
With no portal on the bus, GTK shows them in process, so the test finds each
one among the process's own windows. It opens a file through a filter,
selects a folder, picks a new save destination and checks nothing was created,
then saves over an existing file: Cancel on the overwrite confirmation must
keep the chooser open with no result, and Replace on the second confirmation
must return the existing path. It opens the choosers on the working directory
through a private GSettings keyfile, because a folder chooser opened on Recent
has no folder to select:

```sh
CGO_ENABLED=0 GOFASTR_LINUX_NATIVE=1 GOFASTR_LINUX_PICKERS=1 GDK_BACKEND=x11 xvfb-run -a   go test ./battery/desktop/linux -run '^TestNativePickersAutomated$' -count=1 -v
```

Two further opt-in tests cover the private activation transport and actual
cold/warm GTK navigation, including the boot/session gate. They require a
test-only embedded scheme; the graphical check also needs a display:

```sh
CGO_ENABLED=0 GOFASTR_LINUX_ACTIVATION=1 dbus-run-session -- \
  go test ./battery/desktop/linux -run '^TestNativeActivation$' -count=1 -timeout=120s -v \
  -ldflags '-X github.com/DonaldMurillo/gofastr/battery/desktop.builtDeepLinkScheme=gofastr605test'
CGO_ENABLED=0 GOFASTR_LINUX_NATIVE=1 GOFASTR_LINUX_ACTIVATION=1 dbus-run-session -- \
  go test ./battery/desktop/linux -run '^TestNativeActivationGraphical$' -count=1 -timeout=150s -v \
  -ldflags '-X github.com/DonaldMurillo/gofastr/battery/desktop.builtDeepLinkScheme=gofastr605test'
```

The persistent-profile regression runs fresh writer/reader and isolated/default
processes. It verifies stable origins, cookies and localStorage, expiry/session
semantics, rejected stale boot credentials, profile ownership and busy ports.
It also proves a worker was persisted before checking that the next launch
cleared it. It requires both opt-ins and an unembedded test binary:

```sh
CGO_ENABLED=0 GOFASTR_LINUX_NATIVE=1 GOFASTR_LINUX_PROFILE=1 dbus-run-session -- \
  go test ./battery/desktop/linux -run '^TestNativeProfile$' -count=1 -timeout=240s -v
```

The tray tests use a disposable private bus and test watcher, never the real
desktop's watcher. `TestNativeTray` checks protocol/property/menu contracts;
`TestNativeTrayGraphical` runs separate GTK processes for hide/restore, delayed
recovery, bus loss, explicit Quit and ordinary no-host/no-bus closing, including
a real WebKit snapshot. They do **not** prove a panel rendered an icon:

```sh
CGO_ENABLED=0 GOFASTR_LINUX_TRAY=1 \
  go test ./battery/desktop/linux -run '^TestNativeTray$' -count=1 -timeout=120s -v
CGO_ENABLED=0 GOFASTR_LINUX_NATIVE=1 GOFASTR_LINUX_TRAY=1 \
  go test ./battery/desktop/linux -run '^TestNativeTrayGraphical$' -count=1 -timeout=180s -v
```

Two notification tests use real GTK and libnotify without creating a WebKit
view. `TestNativeNotifications` exports a fixture daemon on its own private bus
and checks native payloads with and without markup support, failed capability
queries, Unicode, daemon refusal, loss/restart and operation cleanup. A bounded
held reply verifies caller cancellation and GTK recovery after release. The
fixture then ends its GTK loop, and a second lifetime on a new daemon must show
a notification under its own app name: libnotify keeps the first name it was
initialized with, so the shell sets the name on every `Show`. This is not a test
of production `Run` teardown. `TestNativeNotificationGraphical` starts an actual
`dunst` daemon on another private bus, verifies displayed/dismissed counts and
finds the daemon's unique mapped X11 window with `xdotool`. It captures that
window as `notification.png`, checks its frame/text pixels, and requires it to
unmap on dismissal. Inspect that PNG for text
and layout; daemon counts alone are not pixel evidence. These fixtures never
send notifications to the user's desktop session:

```sh
CGO_ENABLED=0 GOFASTR_LINUX_NATIVE=1 GOFASTR_LINUX_NOTIFICATIONS=1 GDK_BACKEND=x11 xvfb-run -a \
  go test ./battery/desktop/linux -run '^TestNativeNotifications$' -count=1 -timeout=90s -v
CGO_ENABLED=0 GOFASTR_LINUX_NATIVE=1 GOFASTR_LINUX_NOTIFICATIONS=1 GDK_BACKEND=x11 xvfb-run -a \
  go test ./battery/desktop/linux -run '^TestNativeNotificationGraphical$' -count=1 -timeout=90s -v
```

For a headless Linux worker with GTK/WebKitGTK, `libnotify`, **dunst 1.7.0 or newer**
(including its matching `dunstctl`), `xdotool`, `xcompmgr`, `dbus` (including
`dbus-daemon`), `xvfb` and `xauth` installed, use the repository's runner below.

The notification fixture needs `dunstctl count` (added in 1.6) and the
`width`/`height`/`origin` geometry options (added in 1.7). The runner checks the
daemon version before running any suites and refuses older or unrecognized
versions. Ubuntu 22.04's stock dunst 1.5 package is insufficient for this test;
use a worker with dunst 1.7 or newer. This test dependency does not raise the
application shell's GTK/WebKitGTK runtime minimum.

Run all twelve automated tests in separate invocations:

```sh
./scripts/test-linux-desktop.sh
```

The blocking Linux CI job runs this script with `CGO_ENABLED=0`, a private
session bus per invocation and an authenticated Xvfb for graphical cases.
Only the activation tests get the test-only embedded scheme; every other suite
keeps the unembedded defaults. Logs and
PNG captures go to `dist/ci/linux-native/`. A skipped or missing test fails the runner. Xvfb
coverage validates notification rendering with dunst on X11, transparency under
`xcompmgr`, and the in-process GTK choosers. It does not validate other
notification daemons, Wayland, KWin blur, or portal-backed file choosers.

Do not disable WebKit's process sandbox to run the tests. Ordinary `go test`
runs the no-display tests and skips these opt-in graphical tests. When running
them directly, set `GOFASTR_LINUX_TEST_OUTPUT` to a writable directory to retain
the PNG capture.

### Interactive picker QA

`TestNativePickersAutomated` covers the same four steps under Xvfb. This
operator-driven test is for a real desktop session, with its own theme, window
manager and possibly a portal. Run it by itself in a visible session (not Xvfb).
Both opt-ins are required; it is excluded from the automated runner:

```sh
CGO_ENABLED=0 GOFASTR_LINUX_NATIVE=1 GOFASTR_LINUX_INTERACTIVE=1 \
  go test ./battery/desktop/linux -run '^TestNativeInteractivePickers$' \
  -count=1 -timeout=6m -v
```

The test prints paths to private disposable fixtures and has a five-minute
limit. Follow its four steps: open the input file, select its directory, choose
a new save destination, then choose an existing destination. For the last
step, refuse the first overwrite confirmation with that confirmation's Cancel
button; verify the chooser remains open without returning a result, press Save
again, and accept the second confirmation with Replace. Do not cancel the main
chooser or close the parent window between steps. Only use the printed fixture
paths, not personal files.

Assertions check exact returned paths, unchanged existing fixture contents,
and that selecting a new destination does not create a file. Seeing the
confirmation and verifying Cancel→retry are manual observations: a passing
final-path assertion alone does not prove either. Record those observations
with the GTK version/session type when reporting QA. These checks exercise the
native shell, not the authenticated browser bridge or every desktop's portal.

## What does not work yet

darwin/amd64 has no native shell yet. Linux draws GTK's own title bar for
unified and hidden chrome, and blurs behind materials only under KWin on X11.
Linux tray icons require a compatible StatusNotifierItem host; no-host operation
keeps ordinary visible-window and close behavior. Linux defaults to an ephemeral WebKit
store shared by the windows of one shell lifetime; `Config.PersistentProfile` opts
into the private persistent browser profile described above. The explicit Linux
installer advertises a per-user OS handler but does not choose default handlers,
install system-wide packages or provide signing/native updates. Windows currently
supports amd64 and needs the Evergreen WebView2 Runtime. Windows builds
do not include code signing or an installer. The updater is macOS-only.
macOS notifications need a signed `.app` bundle (ad-hoc is
enough, see above), so they never fire from `go run`. Notarization and
the update pipeline have not been run against Apple's service or a
real release feed. No drag-and-drop from the file manager.

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

`ClickMenu` and `ClickTray` match an item by its title phrase's
English, so the same test passes on a machine set to any language.

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

The macOS app-shell suites are tagged `//go:build desktop_e2e`
and run by hand on a Mac with a display. The Linux native tests use the
separate environment opt-ins described above:

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
