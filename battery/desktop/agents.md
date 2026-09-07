# battery/desktop

EXPERIMENTAL desktop host: run a GoFastr app inside the OS's own
WebView (WKWebView / WebView2 / WebKitGTK) from a `CGO_ENABLED=0`
binary, with a typed JS bridge to native capabilities.

**Use this when** the prompt mentions: desktop app, native window,
WebView, menu bar, menubar app, file dialog, local-first app,
`__gofastr.desktop`, clipboard/file-dialog/notification capability,
`gofastr desktop`.

**Import:** `github.com/DonaldMurillo/gofastr/battery/desktop` (the
contract; `.../desktop/native` for the platform-picking constructor)

**Layout:** the contract plus the OS-neutral half lives here; the native
layer is one package per platform, each compiling on every GOOS:

| Package | What it is |
|---|---|
| `battery/desktop` | Contract (`Shell`, `Window`, `NativeDriver`) + capabilities, grants, handshake, app state, preferences, deep links, updates, the unsupported shell |
| `battery/desktop/macos` | Real shell on darwin/arm64 (AppKit + WKWebView via `internal/objc`); unsupported elsewhere |
| `battery/desktop/windows` | Unsupported today; WebView2 + DWM Mica/Acrylic when it lands |
| `battery/desktop/linux` | Unsupported today; WebKitGTK when it lands |
| `battery/desktop/native` | `Shell()` picks by GOOS; `New(cfg)` is `desktop.New` with that shell as the nil-`Shell` default |
| `battery/desktop/desktoptest` | Fake shell + app-shell harness |
| `battery/desktop/internal/*` | `objc`, `ffi`, `fakecgo`, `gtk`, `update` |

`desktop.New` with a nil `Config.Shell` answers the unsupported shell on
every platform; nothing registers through `init`.

**Shape:**
```go
opts, _ := desktop.AppOptions("dev.gofastr.notes") // data dir + app.db + secret
app := framework.NewApp(append(opts, framework.WithConfig(...))...)
app.Mount(uihost.New(site))                         // BEFORE RegisterBattery
d := native.New(desktop.Config{ID: "dev.gofastr.notes", Title: "Notes"})
app.RegisterBattery(d)
err := d.Run(app)                                   // replaces app.Start; blocks
```
Page side: `__gofastr.desktop.clipboard.writeText({text})`, events via
`__gofastr.desktop.on(name, fn)`. One POST chokepoint
(`/__gofastr/desktop/call/{cap}/{method}`), permission-gated with a
persisted grant store + OS prompt.

**Rules that will bite you if ignored:**
- `New` PANICS on a bad Config (invalid app id, invalid menu, invalid
  `Settings.Path`, invalid `Preferences` entry). The core capabilities
  (`window`, `windows`, `dialogs`, `clipboard`, `notifications`, `fs`,
  `tray`, `state`, `preferences`, `updates`) are registered by `New`;
  plugins add theirs from `Init` via `desktop.FromApp(app).Register(...)`
  before `Run` freezes the registry.
- `Run` requires a mounted `*uihost.UIHost` and forces
  `GOFASTR_ISOLATION=off` process-wide (opt out: `Config.KeepIsolation`).
- The boot token/cookie and Host pin refuse every non-window request;
  do not relax them, and never log the token or session value.
- `fs` only touches paths a `dialogs` call returned this process
  (`AllowPath`); writes are temp-file + rename, 0600.
- `state` page keys live under `page.` only (the battery's own
  `windows`/`settings` entries are never page-reachable); every
  `set`/`delete` broadcasts `state_changed`.
- Everything goes through the chokepoint's closed error codes
  (`denied`, `unsupported`, `invalid_input`, `cancelled`, `not_found`,
  `internal`); 5xx bodies never carry internal error text.
- Never make `battery/desktop` import `macos`/`windows`/`linux`/
  `native` (cycle: they import the contract). A platform need from the
  contract gets exported here (`MenuPlan`, `MainWindowID`,
  `NewUnsupportedShell`), not worked around.

**Testing:** `desktoptest.Run(t, app, d)` (with
`desktop.Config{Shell: desktoptest.NewShell()}`) runs the real `Run`
flow with no native code: `h.Get/Post/Call` as the page,
`h.ClickMenu/ClickTray/PressKey/OpenSettings/CloseWindow/Answer` as the
user, `h.Shell.Notifications()/h.Events()/h.Navigations()` as the
record. Set `GOFASTR_DESKTOP_DATA_DIR` before `AppOptions`.

Full doc: `framework/docs/content/desktop.md` (`gofastr docs desktop`).
