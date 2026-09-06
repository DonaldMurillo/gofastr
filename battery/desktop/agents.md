# battery/desktop

EXPERIMENTAL desktop host: run a GoFastr app inside the OS's own
WebView (WKWebView / WebView2 / WebKitGTK) from a `CGO_ENABLED=0`
binary, with a typed JS bridge to native capabilities.

**Use this when** the prompt mentions: desktop app, native window,
WebView, menu bar, menubar app, file dialog, local-first app,
`__gofastr.desktop`, clipboard/file-dialog/notification capability,
`gofastr desktop`.

**Import:** `github.com/DonaldMurillo/gofastr/battery/desktop`

**Shape:**
```go
opts, _ := desktop.AppOptions("dev.gofastr.notes") // data dir + app.db + secret
app := framework.NewApp(append(opts, framework.WithConfig(...))...)
app.Mount(uihost.New(site))                         // BEFORE RegisterBattery
d := desktop.New(desktop.Config{ID: "dev.gofastr.notes", Title: "Notes"})
app.RegisterBattery(d)
err := d.Run(app)                                   // replaces app.Start; blocks
```
Page side: `__gofastr.desktop.clipboard.writeText({text})`, events via
`__gofastr.desktop.on(name, fn)`. One POST chokepoint
(`/__gofastr/desktop/call/{cap}/{method}`), permission-gated with a
persisted grant store + OS prompt.

**Rules that will bite you if ignored:**
- `New` PANICS on a bad Config (invalid app id, invalid menu, invalid
  `Settings.Path`). The seven core capabilities are registered by
  `New`; plugins add theirs
  from `Init` via `desktop.FromApp(app).Register(...)` before `Run`
  freezes the registry.
- `Run` requires a mounted `*uihost.UIHost` and forces
  `GOFASTR_ISOLATION=off` process-wide (opt out: `Config.KeepIsolation`).
- The boot token/cookie and Host pin refuse every non-window request;
  do not relax them, and never log the token or session value.
- `fs` only touches paths a `dialogs` call returned this process
  (`AllowPath`); writes are temp-file + rename, 0600.
- Everything goes through the chokepoint's closed error codes
  (`denied`, `unsupported`, `invalid_input`, `cancelled`, `not_found`,
  `internal`); 5xx bodies never carry internal error text.

**Testing:** `desktoptest.Run(t, app, d)` (with
`desktop.Config{Shell: desktoptest.NewShell()}`) runs the real `Run`
flow with no native code: `h.Get/Post/Call` as the page,
`h.ClickMenu/ClickTray/PressKey/OpenSettings/CloseWindow/Answer` as the
user, `h.Shell.Notifications()/h.Events()/h.Navigations()` as the
record. Set `GOFASTR_DESKTOP_DATA_DIR` before `AppOptions`.

Full doc: `framework/docs/content/desktop.md` (`gofastr docs desktop`).
