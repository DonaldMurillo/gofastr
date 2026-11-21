# Desktop apps (`battery/desktop`, experimental)

> **Experimental:** `battery/desktop` targets macOS (arm64/amd64), Windows (amd64), and Linux (amd64/arm64) with pure-Go (`CGO_ENABLED=0`) native webview shells. APIs may still change before v1.

`battery/desktop` wraps a normal GoFastr web app in a native desktop window (WKWebView on macOS, WebView2 on Windows, WebKitGTK on Linux) over an authenticated loopback server — no Electron, no Node, no CGO.

## Quickstart

```go
import (
	"log"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/battery/desktop/native"
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

func main() {
	opts, err := desktop.AppOptions("dev.gofastr.notes")
	if err != nil {
		log.Fatal(err)
	}
	site := app.NewApp("Notes")
	site.Register("/", &HomeScreen{}, nil)

	fwApp := framework.NewUIHostApp(uihost.New(site), opts...)
	// Register entities and routes BEFORE RegisterBattery.
	d := native.New(desktop.Config{
		ID:              "dev.gofastr.notes",
		Title:           "Notes",
		RememberWindows: true,
	})
	fwApp.RegisterBattery(d)
	log.Fatal(d.Run(fwApp)) // Run owns app.Start and graceful Shutdown
}
```

Use `desktop.AppOptions(id)` to wire the per-app data directory (`app.db`, `secret`, `identity` via `framework/localapp`). Never call both `app.Start` and `d.Run` (`d.Run` owns the full lifecycle).

For a server-only Linux image (Alpine / `scratch`) that shares code with a desktop app, build with `-tags gofastr_nodesktop` so no GTK dynamic loader dependency is linked.

## How the window authenticates

The native bootstrap and typed bridge (`window.__gofastr_desktop`) inject only into the top frame of main and secondary windows — never iframes.

Any local process can discover a loopback port, so every request must carry a credential only the launched window holds:

1. `New` mints a 32-byte random boot token (never logged).
2. `Run` navigates the new window to `/__gofastr/desktop/enter?t=<token>` (single-use, constant-time compared).
3. The handler sets the `__gofastr_desktop_<session value>` cookie (`HttpOnly`, `SameSite=Strict`, session lifetime) and redirects to `/`. Using a unique cookie name per running instance prevents two WebView2 instances sharing loopback cookies from overwriting each other.
4. An app-wide gate middleware armed by `Run` requires both the pinned `Host` header (blocking DNS rebinding from browser tabs) and that session cookie on every route, including pages and `/__gofastr/sse`.

When `battery/auth` is not imported, `battery/desktop` installs the local owner identity from `localapp` (`["admin"]`) on authenticated window requests so `Scope.OwnerField` works seamlessly for single-user desktop apps.

### Local callers with no window: `OpenPrefixes`

Other local processes (a CLI or helper script) have no window cookie and receive `403`. List paths that should bypass the window gate in `Config.OpenPrefixes` and protect them with your own auth (API key, bearer token, or `battery/auth`):

```go
d := desktop.New(desktop.Config{
	ID:           "dev.gofastr.notes",
	OpenPrefixes: []string{"/api"},
})
```

## Capabilities and permissions

Pages call native capabilities over the authenticated bridge (`__gofastr.desktop.<cap>.<method>(...)`). Sensitive capabilities prompt the user via a native OS dialog once and persist grants in `desktop_grants`.

| Capability | Methods | Permission |
| --- | --- | --- |
| `window` | `title`, `setTitle`, `snapshot`, `startDrag`, `setChrome`, `setPath` | none |
| `windows` | `open`, `openSettings`, `focus`, `close`, `list`, `post`, `broadcast`, `self` | none (`close` refuses `"main"`) |
| `dialogs` | `openFile`, `saveFile`, `openFolder` | none (user-mediated) |
| `clipboard` | `readText`, `writeText` | `clipboard:read`, `clipboard:write` |
| `notifications` | `show` | `notifications:show` |
| `fs` | `readText`, `writeText`, `stat`, `list` | `fs:read`, `fs:write` |
| `tray` | `setTitle` | none |
| `state` | `get`, `set`, `delete`, `keys` | none (`page.` prefix only) |
| `preferences` | `get`, `set` | none (declared keys only) |
| `updates` | `check`, `install` | none / `updates:install` |

### File picker grants

`fs` can only touch paths returned by a `dialogs` call during the current session (or explicitly granted in Go via `Battery.AllowPath` / `Battery.AllowFolder`):
- `openFile` and `saveFile` grant only the exact returned file paths for `fs.stat`, `fs.readText`, and `fs.writeText` — never siblings or parent directories.
- `openFolder` grants `fs.stat`, `fs.readText` and `fs.list` on the picked folder and everything under it, and never `fs.writeText` (saving requires a `saveFile` dialog). Reads inside an allowed folder execute through an `os.Root` pinned at pick time so symlinks or `..` cannot escape.

Register custom Go capabilities before `Run` with `Register(Capability)` (the registry freezes when the window opens).

## Chrome, menus, widgets, and deep links

- **Native chrome & `desktopui`:** set `WindowStyle.Chrome` (`ChromeUnified`, `ChromeHiddenTitle`, `ChromeNone`) and `Material` (`MaterialSidebar`, `MaterialWindow`, `MaterialGlass` on macOS/Windows 11). Mark borderless drag regions with `data-cui-window-drag` (and clickable controls inside them with `data-cui-window-no-drag`). Pair with `battery/desktop/ui` (`desktopui.ThemeFor`, `desktopui.Layout`, `WindowLayout`, `WidgetLayout`).
- **Menus & tray:** declare native menu bars and tray menus in `Config`. Use `Role: "quit"` for Quit rather than writing a custom quit handler.
- **Cross-window events:** `Battery.Emit(name, payload)` broadcasts to every open window (guarded to the app's own origin); `Battery.EmitTo(id, name, payload)` targets one window. Pages listen with `__gofastr.desktop.on(name, fn)` and know their own ID via `__gofastr.desktop.windowID`.
- **Deep links:** set `Config.DeepLink` (`Scheme: "my-app"`, optional `OnDeepLink`) and pass `--scheme=my-app` to `gofastr desktop build` to route custom URL schemes into client-side navigation and `deep_link` events.
- **App state & preferences:** `Battery.State()` (`battery/desktop/appstate`) stores debounced JSON key-value data in `state.json` (`0600`). Declare typed settings in `Config.Preferences`, read them via `d.Preferences()`, and mount `desktop.PreferencesScreen` at `/settings`.
- **Auto-update (macOS & Windows):** configure `Config.Update` (`FeedURL`, ed25519 `PublicKey`). Updates verify feed signature, expiry, SHA-256, bundle version/ID, and code signature before swapping and relaunching.

## The `gofastr desktop` verbs

| Verb | Purpose |
| --- | --- |
| `gofastr desktop run` | Build and launch the desktop app in dev mode |
| `gofastr desktop build --id <rev.dns> --name <Name>` | Package a `.app` bundle (macOS, ad-hoc signed by default; `--sign`, `--notarize`, `--scheme`), `.exe` + `WebView2Loader.dll` (Windows; `--sign`, `--zip`), or ELF / `--format=tar.gz` (Linux). Use `--launch-args` + `desktop.LaunchArgs` when desktop mode is a subcommand. |
| `gofastr desktop install / uninstall` | Per-user Linux install/uninstall of an extracted `tar.gz` bundle |
| `gofastr desktop types` | Generate `desktop.d.ts` TypeScript definitions for registered capabilities |
| `gofastr desktop keygen -o <path>` | Mint an ed25519 keypair for signed update feeds |
| `gofastr desktop feed --key ... --version ... --platform ... --archive ... --url ...` | Build and sign `manifest.json` + `manifest.json.sig` |

## Testing

Use `battery/desktop/desktoptest` (`desktoptest.Run(t, app, d)`) to test the real boot handshake, cookie gate, Host pin, menus, tray, permissions, and capabilities in pure Go on any OS with no native window (`h.Call`, `h.ClickMenu`, `h.Answer`, `h.Stranger()`). For full WebView e2e runs on macOS, use `desktoptest.NativeMain` with `-tags desktop_e2e`.

## Common mistakes

- **Calling both `app.Start` and `d.Run`:** `d.Run` owns startup and shutdown; calling both starts two servers.
- **Mounting `uihost` after `app.RegisterBattery(d)`:** register entities and mount `uihost` before registering the desktop battery.
- **Registering capabilities after `Run`:** the capability registry freezes when the window opens; register inside `Init` or before `Run`.
- **Widening `fs` access instead of using `dialogs`:** the session allow-list is the core sandbox boundary between the webview and the user's disk.
- **Declaring a custom Quit menu handler instead of `Role: "quit"`:** let the shell own quit so menus and shutdown hooks run once cleanly.
- **Linking `battery/desktop/native` into a static Alpine/scratch server image:** pass `-tags gofastr_nodesktop` for headless Linux server builds.
