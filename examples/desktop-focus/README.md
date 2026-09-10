# desktop-focus

A local-first pomodoro timer and the `battery/desktop` dogfood app that
uses every desktop feature: unified-chrome main window,
floating always-on-top timer widget, settings window, tray countdown,
notifications, deep links, cross-window messages, a plugin capability,
and the updater. One `main.go` runs it unchanged inside the OS WebView
or over plain HTTP.

## What it shows

- The timer state lives in the database, never in server RAM: the
  current session is the newest `sessions` row with
  `completed == false`, so a restart (or a second replica) resumes to
  the same answer. All engine reads and writes go through the
  entities' CrudHandlers with the owner's identity context.
- A once-a-second tick loop, started from `app.OnReady` and stopped on
  app shutdown: while a session runs it updates the tray title to
  `mm:ss` (when the owner's `tray_countdown` preference is on) and
  emits `focus_tick` with the state to every open window; when the
  time is up it completes the session, counts the task's pomodoro,
  notifies through the OS (`notify` preference), emits `focus_done`,
  auto-starts the break after work, and goes idle after a break.
- The `focus` plugin capability: `start {taskId}`, `pause`, `resume`,
  `skip`, `state`. The page never talks to the engine any other way;
  every mutation answer carries the new state so the page syncs from
  the engine instead of guessing.
- Window styles: the main window is `ChromeUnified` over the sidebar
  material (transparent title bar, hidden title, an empty toolbar so
  the style takes effect, vibrancy under the source list, the zone at
  `SidebarWidth`); the settings window is `ChromeUnified` over the
  whole-window material; the timer widget is
  `desktop.Widget("/widget", 320, 300)` with `AllSpaces` (borderless,
  non-activating, transparent, visible on every Space), opened
  automatically when a session starts. The widget's card header is the
  drag handle (`data-fui-window-drag`).
- Cross-window messages: the widget's "Open task" button posts
  `show_task` to the main window through `windows.post`; the main
  page's listener navigates to the task with `__gofastr.navigate`.
- Deep links: `gofastr-focus://start?task=<id>` starts that task and
  lands on the dashboard; anything else maps through the default rule
  (`gofastr-focus://history` opens the history screen). Register the
  scheme at bundle-build time with `--scheme gofastr-focus`.
- A tray menu (Show, Start / Pause, Timer widget, Settings, quit) and
  a native menu bar (File, View) sharing the same item model; the
  settings window has four entry points (app menu, File menu, tray,
  `windows.openSettings`). Its screen is `desktop.PreferencesScreen`:
  the battery renders the declared preferences (`work_minutes`,
  `break_minutes`, `notify_on_done`, `tray_countdown`, and `sound`,
  the choice-kind demo) as one form saved through the battery's own
  route, with no settings entity behind it.
- The updater wired from `FOCUS_UPDATE_FEED` / `FOCUS_UPDATE_KEY`
  (`gofastr desktop keygen` / `feed`), with the File menu's
  "Check for updates…" item.

## The macOS look

The app opts into the phase 13 desktop theme and layout:
`site.WithTheme(desktopui.Theme())` and every screen but the widget
mounts on `desktopui.Layout()` with a `SourceList` sidebar (Dashboard,
Tasks, History, Settings; the active row follows the path). The timer
controls float in a `FloatingToolbar`, the task detail shows its facts
in an `Inspector`, and the sidebar zone's width is reported to the
shell through `window.setChrome` from a `ResizeObserver` in the page
script. The example itself ships no CSS: the theme, the layout, and
the `battery/desktop/ui` components carry all of it. Capture findings
and the exact commands live in `docs/desktop-sections/13-focus.md`.

## Run it

```bash
go run ./examples/desktop-focus                    # native window (darwin/arm64)
go run ./examples/desktop-focus --serve :8080      # same app in a browser
gofastr desktop run  --pkg ./examples/desktop-focus         # build + run, dev tools on
gofastr desktop build --id dev.gofastr.desktop-focus \
  --name Focus --scheme gofastr-focus --pkg ./examples/desktop-focus  # dist/Focus.app
```

`--serve` mode is also what `gofastr dev` drives (it sets `$PORT`), so
rebuild-on-save and livereload work on this example. In that mode
there is no window, so every native call in the engine is skipped and
the timer buttons explain themselves with a toast; the screens and
entities all run, and the settings screen renders the declared
defaults (saving preferences needs the desktop host's app state
store, which only Run opens).

## Where the data lives

`os.UserConfigDir()/dev.gofastr.desktop-focus/` (macOS:
`~/Library/Application Support/dev.gofastr.desktop-focus/`):
`app.db` (SQLite), `secret` (session key), `identity` (the local user
the owner scoping keys on). Delete the directory to reset the app.
`GOFASTR_DESKTOP_DATA_DIR` overrides the base directory (tests use it).

The tests come in three shapes. `engine_test.go` drives the engine
with an injected clock through the real entities. `harness_test.go`
drives the window shape through `desktoptest.Run`: the bridge, the
tray, the menus, the deep links, the notifications, and the settings
window the way a user reaches them. `native_e2e_test.go` (tag
`desktop_e2e`, a Mac with a display) runs the app inside the real
WKWebView through `desktoptest.NativeMain`: the dashboard's Start
button, the ticked countdown, the widget's post into the main window,
and the settings window's checkboxes, with `FOCUS_SHOTS=<dir>` writing
the windows' real pixels as PNGs. `main_test.go` covers the `--serve`
shape.

## What needs a bundle

Notifications require a signed `.app`: macOS refuses
`UNUserNotificationCenter` from `go run`. An ad-hoc signature is
enough, and `gofastr desktop build` applies one by default when
`codesign` is on PATH. Everything else works unbundled. On hosts
without a native shell, `Run` reports the named `unsupported` error,
and a configured tray logs a Warn.
