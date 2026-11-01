# Dev-mode livereload

`framework.NewApp()` and `uihost.New()` auto-wire a tiny SSE-based
livereload pair when the process is running under `gofastr dev`. Edit
a watched file (`.go`, `.js`, `.css`, `.html`, or `.md`, the
extensions Go embeds), the binary restarts, and every open browser tab
refreshes on its own. No host-app code required.

The rebuilt server runs with the project directory (`--dir`) as its
working directory, the same cwd it gets when run by hand, so relative
paths like a sqlite `db_url` or a static dir resolve against the
project, and worktree isolation keys off the project's location rather
than wherever `gofastr dev` was launched.

## How it turns on

The first three env vars gate livereload, and `GOFASTR_DEV_LAYOUT`
adds an opt-out for the layout layer on top of the same gate. All
defaults are dev-friendly; production is always safe:

| Var | Default | Meaning |
|---|---|---|
| `GOFASTR_DEV` | unset | `gofastr dev` sets this to `1` on the child process. Without it, livereload stays dormant. |
| `GOFASTR_ENV` | unset | Set to `production` to force livereload off even if `GOFASTR_DEV` slips through. Belt-and-suspenders for accidental dev binaries in prod. |
| `GOFASTR_DEV_LIVERELOAD` | unset (= on) | Set to `0` to opt out while keeping the rest of dev mode (`gofastr dev`'s rebuild loop) running. |
| `GOFASTR_DEV_LAYOUT` | unset (= on) | Set to `0` to stop serving the [layout layer](#layout-layer) script. |

Predicate (single source of truth in `framework/dev/livereload.go`):

```
GOFASTR_ENV != "production"  AND
GOFASTR_DEV truthy           AND
GOFASTR_DEV_LIVERELOAD != "0"
```

## What it does

When enabled:

- `framework.NewApp()` registers two routes on the App router:
  - `GET /__livereload`: `text/event-stream`. Fires one `event: ready`
    on connect, then idles with a 25s SSE-comment heartbeat. Closes
    when the request context cancels (browser tab close, or the start
    of `App.Shutdown`, which ends every open event stream rather than
    wait for it; see [deploy](deploy.md)).
  - `GET /__livereload.js`: ~16 lines of JS. Opens an `EventSource`
    and remembers the build ID the first `ready` event carries. A
    `ready` with a different ID (the reconnect after a rebuild, or
    `gofastr dev`'s hold page below) calls `location.reload()`; a
    reconnect to the same process does not.
- `uihost.New()` auto-appends `/__livereload.js` to the `extraScripts`
  list so every rendered page links to the client script before
  `</body>`. CSP-safe: it's a `<script src="...">`, no inline JS.
  `UIHost.Mount` registers the same routes on the router it mounts on,
  so a host served on its own, or mounted on a router you built, answers
  the scripts its pages link. Registration is idempotent: a router
  `framework.NewApp` already wired is left alone.
- For every OTHER way an app serves a page, `static.Handler` file
  serving (SPA shells, exported static pages), widget-server pages,
  and hand-rolled handlers, `framework.NewApp` mounts dev-only middleware
  that splices the same `<script src>` into responses that declare
  `Content-Type: text/html` (no sniffing, so set the type) and are full
  documents (contain `</body>`) and don't already carry the tag.
  Fragments (island RPC swaps, SPA-nav partials), compressed bodies,
  HEAD/Range requests, and non-HTML responses (JSON, SSE, streams) pass
  through untouched and unbuffered; a handler that Flushes mid-HTML
  streams from that point on, uninjected.

One persistent SSE connection per tab, near-zero idle traffic, no
polling.

### Loopback only, unless you opt in

The stream has no seat and no cap: it is meant for the developer at
this machine. When `App.Start` binds an address other machines can
reach (`0.0.0.0:8080`, `:8080`, a LAN IP), it turns the stream off and
logs a warning: `/__livereload` answers `404`, which an `EventSource`
treats as final, so open pages stop asking. The script still loads.
Set `GOFASTR_DEV_LIVERELOAD_EXPOSE=1` (`dev.LiveReloadExposeEnv`) to keep
it on such a bind, say to reload a phone on your network. The dev MCP
follows the same rule with its own opt-in, `GOFASTR_DEV_MCP_EXPOSE`.

An app that registered its own `/__livereload` before the framework's
keeps it: the framework installed nothing there and does not touch the
host's handler. On an exposed bind the warning then says so (the app's
stream stays on) instead of claiming it was turned off.
`dev.WithdrawLiveReload` reports which case it hit: `dev.Withdrawn`,
`dev.HostOwned`, or `dev.NotRegistered` when the router has no
livereload at all.

### Project static assets are served uncached

Framework assets are content-versioned (`/__gofastr/runtime.js?v=…`),
but project files serve at plain URLs (`/nav.js`, `/app.css`), where a
bare `Last-Modified` invites heuristic caching: the browser keeps
executing the old bytes after an edit even though a fresh fetch returns
the new ones. Under `gofastr dev`, files served from the static dir or
the embedded static FS go out with `Cache-Control: no-store`, so the
browser asks the server for them instead of replaying what it cached.

That is the whole of what the header buys, and what it buys differs by
where the file comes from. A file in the static dir is read from disk
on every request, so editing it and reloading is enough. A file in an
embedded static FS is compiled into the binary, so an edit there still
needs a rebuild; `no-store` only guarantees you are looking at what the
running binary holds rather than at a stale copy of it.

Production caching is unchanged: when dev mode is off, no header is
set at all.

## Layout layer

Under the same gate, every page also links `/__devlayout.js`, a
script that draws the page's structure over it. Press **Alt+Shift+L**
(Option+Shift+L on a Mac) to show or hide it. The choice is kept in
`localStorage`, so it stays on across livereload's reloads until you
press the keys again. Nothing is drawn, and nothing is added to the
page, while it is off.

It outlines, with a label at each box's top-left corner:

| Outline | What it marks |
|---|---|
| layout (purple, solid) | a layout layer, `data-cui-layout`, with its `data-cui-layout-key` |
| slot (blue, dashed) | a layer's content cell, `data-cui-layout-slot`, the client router's swap target |
| outlet, area (teal) | `data-cui-outlet` and `data-cui-area` cells |
| scope (amber, dotted) | an owned style's root, `data-cui-scope` |
| island (green), widget (pink) | `data-island` regions and `data-cui-widget` surfaces |
| scrolls (grey, dotted) | a box whose `overflow` is `auto` or `scroll` and whose content is larger than it, labelled `x`, `y` or both |
| overflows (red, filled) | an element that runs past the viewport's left or right edge, labelled with how far |

An element counts as overflowing only when nothing above it scrolls,
clips or fixes it in place: a table inside a scroll box, a carousel
inside `overflow: hidden` and an off-canvas `position: fixed` drawer
are all deliberate. Only the outermost such element is labelled, not
each of its children. A legend in the bottom-left corner counts each
kind and says when the whole page scrolls sideways, and by how many
pixels. Resize the window to a phone width to find what widens the
page there.

The layer sits in a shadow root on a fixed element outside `<body>`,
so app CSS does not reach it and client navigation does not remove it.
That element is a manual popover, so it paints in the top layer, and
it moves back on top whenever a popover or modal dialog opens: the
outlines stay visible over an open phone sheet or confirm.
It sets every style through the CSSOM, which the framework's default
CSP allows. It takes no pointer events, so the page under it works as
usual. It redraws as you scroll, and walks the page again after DOM
changes, a resize, or the end of a transition or animation.

`window.__gofastrDevLayout` exposes `show()`, `hide()`, `toggle()`,
`on`, and `findings` (the overflow labels), for a browser test or an
agent driving the page.

Pages that uihost does not render get the script through the same
middleware that adds the livereload client.

## How `gofastr dev` wires it

`cmd/gofastr/internal/devcmd/dev.go` injects `GOFASTR_DEV=1` into the child binary's
environment when it launches the rebuilt server. The host app doesn't
need to forward, set, or check the flag. It's transparent.

Every rebuild (the initial one and each change-triggered one) first
runs the same static accessibility lint `gofastr build` enforces:
findings are printed with fix hints and the rebuild is treated exactly
like a compile failure: the server doesn't start, the watcher keeps
running, fixing and saving retries. `gofastr dev --no-a11y` skips the
gate with the same "escape hatch, not a setting" caveat as build's
flag (see the accessibility doc).

On each save `gofastr dev` sends the running app `SIGTERM` and gives it
up to five seconds to exit before `SIGKILL`, so shutdown hooks, the
process-module drain and outbox flushes finish. It moves on as soon as
the app exits, which with livereload's streams ended at the start of
the drain is usually at once. Windows has no `SIGTERM`, so there the app
is killed at once, and `gofastr dev` and `gofastr desktop` still wait
up to the five seconds for it to exit before going on, so shutdown can
remove the temp directory holding the binary (a running `.exe` cannot
be deleted). A `SIGINT` or `SIGTERM` to `gofastr dev`
during a build kills the build and exits; it does not wait for the build
and the app it would start. The build runs in a process group of its
own, so the kill reaches the compiler, the linker and cgo's C toolchain
as well as `go` (on Windows, `taskkill /T` ends the tree).

The watcher walks the project for `.go`, `.js`, `.css`, `.html` and
`.md` changes. It skips every dot-directory (`.git`, `.gofastr`, editor
state, agent worktrees under `.claude/worktrees`), `vendor` and
`node_modules` at any depth, and `dist`, `gen` and `tmp` at the project
root, so a save in a sibling worktree or a build writing to `dist/` does
not restart the app. A directory the built package imports from is
always walked, whatever its name: an `internal/gen` package, app spec
output in `gen/` that `main` imports, or `--pkg ./gen` itself. The
import graph comes from `go list -deps`, run with the build's
environment (`gofastr desktop` builds with `CGO_ENABLED=0`, and so does
its query), read at start and again after each rebuild. A query that
succeeds replaces the graph, so an import you remove stops keeping its
directory walked; one that fails, because a save left the tree
mid-edit, keeps the last graph. A directory a new graph brings into
the walk, or takes out of it, is not itself a change: the save that
added the first import of `gen/` restarts the app once, and a later
save inside `gen/` restarts it again. Ctrl-C ends a query still waiting
on a module proxy.

A build that runs longer than ten seconds prints a progress line every
ten seconds (`Building <dir> (20s)...`). `go build` itself is silent
however long it takes, and the startup banner has already named an
address, so the heartbeat is what tells "still building" from "stuck",
both to a person reading the terminal and to anything waiting on the
dev process programmatically.

## While the app is down

Nothing from the app answers during the first build, after a failed
build, or after the app exits. `gofastr dev` holds the dev address
itself in those windows, so a request gets an answer instead of
"connection refused":

- Every page request gets status `503` with `Retry-After: 1` and an
  `X-Gofastr-Dev` header naming the state: `building`, `build-failed`
  or `exited`.
- A plain-text body for `curl` and other clients that don't ask for
  HTML: what is happening, and for a failed build the compiler, lint
  or sandbox output. A browser gets the same as a page. On an address
  other machines can reach, the output is left out (it carries absolute
  paths and quoted source) and the page points at the terminal.
- `/__livereload` answers too. An open tab reloads into the page when a
  build fails or the app exits, but not for an ordinary rebuild, and
  reloads back into the app when its new build comes up. On an address
  other machines can reach it answers `404`, unless
  `GOFASTR_DEV_LIVERELOAD_EXPOSE=1`, the same rule as the app's stream.

The hold never proxies. It closes its listener right before the app
starts, and the app binds the address itself, so the app runs the same
as it does by hand. Between that close and the app's own bind, which
lasts as long as the app takes to boot, the address refuses
connections. The page for an app that exited gives its exit status
when it was nonzero; the app's own output stays in the terminal running
`gofastr dev`.

Because the hold binds the address at startup, a second `gofastr dev`
on a busy address stops at once with an error naming the address,
instead of building first and then failing to bind. An address with
port `0` gets no hold. In a linked worktree with isolation on, the
second run does not stop: isolation skips a busy port and picks the next
free one, so it starts on another address.

## The dev loop is also livereload for agents

The same `GOFASTR_DEV` gate turns on MCP for agents: `framework.NewApp`
mounts `/mcp` (skipping, with a warning, if the host hand-mounted one),
registers read-only tools for reading the running app's state
(`app_routes`, `app_readiness`, `app_config`, …) and tools
that change it (`app_module_enable` / `app_module_disable`); every
CRUD-enabled entity serves its MCP data tools without per-entity
`mcp: true`; and battery/log, when registered, enables its
`log_recent` / `log_filter` / `log_metrics` / `log_set_level` debug
tools. The `framework_docs_*` tools are the one surface the dev loop
does not imply: the app passes `framework.WithMCPTools(mcptools.Register)`
(generated apps do). A connected agent can check what's running, read recent
requests and errors, read and write app data, and turn modules on or
off on the running dev app without extra setup. See
[agent-ready](agent-ready.md). Opt out with `GOFASTR_DEV_MCP=0`.

## Opting out

```bash
# Keep rebuild-on-save but disable browser refresh:
GOFASTR_DEV_LIVERELOAD=0 gofastr dev

# Keep rebuild + refresh but disable the dev MCP tools:
GOFASTR_DEV_MCP=0 gofastr dev
```

## Forcing it on outside `gofastr dev`

For a custom watcher or `air`-style tool, set the env yourself:

```bash
GOFASTR_DEV=1 ./my-app
```

The framework will auto-register the routes and inject the script.
Browser tabs already pointing at the app reload as soon as the new
binary accepts the SSE connection.

## Common mistakes

- Setting `GOFASTR_ENV=production` while expecting livereload to work
  in dev. Production is a hard kill switch; clear the var first.
- Wiring `uihost.WithExtraScripts("/__livereload.js")` by hand. The
  framework already does it when env says so, and your manual call
  becomes a duplicate `<script>` tag.
- Registering `/__livereload` routes by hand. The framework already
  does it, and a manual `Router().Get` will collide.
