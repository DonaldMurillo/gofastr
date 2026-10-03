# desktop-notes

The `battery/desktop` dogfood app: a local-first notes app whose one
`main.go` runs unchanged inside the OS WebView or over plain HTTP.

## What it shows

- The same screens, entity, and hooks in both hosts. `Run(app)` opens
  the native window; `--serve :8080` (or `$PORT`, which `gofastr dev`
  injects) serves the identical app to a browser. The desktop battery
  stays registered in both modes; its boot gate is armed only by `Run`.
- One owner-scoped entity (`notes`) with `SearchFields` on title and
  body, so `?q=` free-text search works on the API and the list
  screen's search box.
- The list is an island: sorting and pagination swap the table by RPC,
  no reload. `/notes/{id}` is the engine's detail view (Edit, Delete,
  Back); the editor (`/notes/new`, `/notes/{id}/edit`) is its form,
  saving through the runtime's form intercept to `POST/PUT /api/notes`
  and returning to the detail page.
- Every core capability once: File > New note (menu Navigate),
  File > Export all (save dialog, Markdown file, native event), Copy
  link (clipboard), a saved note fires a notification (gated by the
  owner's `notify_on_save` preference), and the window title follows
  the open note.
- A settings window: `desktop.PreferencesScreen`, the battery's form
  over the declared preferences (`notify_on_save`, `export_folder`,
  stored in the app state; no settings entity). `Config.Settings`
  gives it three entry points: the app menu's own Settings item
  (cmd+,), the File menu's `Settings…` row, and the page's
  `windows.openSettings()`.
- A tray icon: `Config.Tray` puts the app in the menu bar with the
  app icon as a template image, a menu (Show Notes, New note,
  Settings, Quit) sharing the main menu's item model, and
  `CloseHidesWindow` so the red button hides the window instead of
  quitting; `Show Notes` (RoleShow) brings it back.
- A plugin capability: `systeminfo.cpuCount`, registered from a plain
  `framework.Plugin` Init. It appears in the manifest, the generated
  bridge, and `gofastr desktop types` output with no host change.

## Run it

```bash
go run ./examples/desktop-notes                    # native window (darwin/arm64)
go run ./examples/desktop-notes --serve :8080      # same app in a browser
gofastr desktop run  --pkg ./examples/desktop-notes        # build + run, dev tools on
gofastr desktop build --id dev.gofastr.desktop-notes \
  --name Notes --scheme gofastr-notes --pkg ./examples/desktop-notes  # dist/Notes.app; gofastr-notes:// links open it
```

`--serve` mode is also what `gofastr dev` drives (it sets `$PORT`), so
rebuild-on-save and livereload work on this example.

## Where the data lives

`os.UserConfigDir()/dev.gofastr.desktop-notes/` (macOS:
`~/Library/Application Support/dev.gofastr.desktop-notes/`):
`app.db` (SQLite), `secret` (session key), `identity` (the local user
the owner scoping keys on). Delete the directory to reset the app.
`GOFASTR_DESKTOP_DATA_DIR` overrides the base directory (tests use it).

The tests come in two shapes. `main_test.go` drives the `--serve` shape
through `framework.TestHarness`. `harness_test.go` drives the window
shape through `desktoptest.Run`: the real `Run` flow with the fake
shell as the OS, so the File menu, the tray, the settings window, the
export dialog, and the save notification are all exercised the way a
user reaches them.

## What needs a bundle

Notifications require a signed `.app`: macOS refuses
`UNUserNotificationCenter` from `go run`. An ad-hoc signature is
enough, and `gofastr desktop build` applies one by default when
`codesign` is on PATH, so the built bundle notifies (the first save
triggers the system's Allow prompt). Everything else, including the
clipboard, dialogs, and the export, works unbundled. On hosts without
a native shell, `Run` reports the named `unsupported` error, and a
configured tray logs a Warn.
