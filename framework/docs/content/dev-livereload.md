# Dev-mode livereload

`framework.NewApp()` and `uihost.New()` auto-wire a tiny SSE-based
livereload pair when the process is running under `gofastr dev`. Edit
a `.go` file, the binary restarts, and every open browser tab refreshes
on its own — no host-app code required.

## How it turns on

Three env vars decide. All defaults are dev-friendly; production is
always safe:

| Var | Default | Meaning |
|---|---|---|
| `GOFASTR_DEV` | unset | `gofastr dev` sets this to `1` on the child process. Without it, livereload stays dormant. |
| `GOFASTR_ENV` | unset | Set to `production` to force livereload off even if `GOFASTR_DEV` slips through. Belt-and-suspenders for accidental dev binaries in prod. |
| `GOFASTR_DEV_LIVERELOAD` | unset (= on) | Set to `0` to opt out while keeping the rest of dev mode (`gofastr dev`'s rebuild loop) running. |

Predicate (single source of truth in `framework/dev/livereload.go`):

```
GOFASTR_ENV != "production"  AND
GOFASTR_DEV truthy           AND
GOFASTR_DEV_LIVERELOAD != "0"
```

## What it does

When enabled:

- `framework.NewApp()` registers two routes on the App router:
  - `GET /__livereload` — `text/event-stream`. Fires one `event: ready`
    on connect, then idles with a 25s SSE-comment heartbeat. Closes
    when the request context cancels (server shutdown / browser tab
    close).
  - `GET /__livereload.js` — ~16 lines of JS. Opens an `EventSource`,
    treats the **second** `onopen` (i.e. the reconnect after the
    server drop) as the reload signal, calls `location.reload()`.
- `uihost.New()` auto-appends `/__livereload.js` to the `extraScripts`
  list so every rendered page links to the client script before
  `</body>`. CSP-safe — it's a `<script src="...">`, no inline JS.
- For every OTHER way an app serves a page — `static.Handler` file
  serving (SPA shells, exported static pages), widget-server pages,
  hand-rolled handlers — `framework.NewApp` mounts dev-only middleware
  that splices the same `<script src>` into responses that declare
  `Content-Type: text/html` (no sniffing — set the type) and are full
  documents (contain `</body>`) and don't already carry the tag.
  Fragments (island RPC swaps, SPA-nav partials), compressed bodies,
  HEAD/Range requests, and non-HTML responses (JSON, SSE, streams) pass
  through untouched and unbuffered; a handler that Flushes mid-HTML
  streams from that point on, uninjected.

One persistent SSE connection per tab, near-zero idle traffic, no
polling.

## How `gofastr dev` wires it

`cmd/gofastr/dev.go` injects `GOFASTR_DEV=1` into the child binary's
environment when it launches the rebuilt server. The host app doesn't
need to forward, set, or check the flag — it's transparent.

## The dev loop is also livereload for agents

The same `GOFASTR_DEV` gate auto-enables the MCP agent surface:
`framework.NewApp` mounts `/mcp` (skipping, with a warning, if the host
hand-mounted one), registers the read-only introspection tools
(`app_routes`, `app_readiness`, `framework_docs_search`, …) and the
mutating control tools (`app_module_enable` / `app_module_disable`);
every CRUD-enabled entity serves its MCP data tools without per-entity
`mcp: true`; and battery/log — when registered — enables its
`log_recent` / `log_filter` / `log_metrics` / `log_set_level` debug
tools. A connected agent can orient, read recent requests and errors,
read and write app data, and toggle modules on the running dev app
with zero wiring. See [agent-ready](agent-ready.md). Opt out with
`GOFASTR_DEV_MCP=0`.

## Opting out

```bash
# Keep rebuild-on-save but disable browser refresh:
GOFASTR_DEV_LIVERELOAD=0 gofastr dev

# Keep rebuild + refresh but disable the dev MCP agent surface:
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
  framework already does it when env says so — your manual call
  becomes a duplicate `<script>` tag.
- Registering `/__livereload` routes by hand. The framework already
  does it — a manual `Router().Get` will collide.
