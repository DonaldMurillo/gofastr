# Dev-mode livereload and inspector

When you run `gofastr dev`, GoFastr watches your source files, rebuilds and restarts on save, refreshes open browser tabs over SSE, enables the visual layout inspector (`Alt+Shift+L`), and activates dev-mode MCP tools automatically — zero app code required.

## Environment gates

| Variable | Default | Effect |
| --- | --- | --- |
| `GOFASTR_DEV` | unset | Set to `1` by `gofastr dev`; activates livereload, dev MCP, and uncached static assets |
| `GOFASTR_ENV` | unset | `production` acts as a hard kill switch disabling dev features even if `GOFASTR_DEV=1` |
| `GOFASTR_DEV_LIVERELOAD` | unset (on) | Set to `0` to disable browser auto-reload while keeping rebuild-on-save |
| `GOFASTR_DEV_LAYOUT` | unset (on) | Set to `0` to disable the layout overlay script |
| `GOFASTR_DEV_MCP` | unset (on) | Set to `0` to disable dev-mode `/mcp` auto-mount |

## How livereload works

- `framework.NewApp()` registers `GET /__livereload` (SSE stream sending a build ID on connect) and `GET /__livereload.js` (CSP-safe external script injected automatically into `uihost` and full HTML responses).
- Open tabs reload only when a reconnect reports a **new** build ID; transient disconnects preserve unsaved form state.
- **Hold page while building/failing:** during builds or compile/a11y failures, `gofastr dev` holds the port and answers `503` with compiler/lint output (localhost only) so tabs show the error and reload automatically once your fix compiles.
- **Loopback-only by default:** if you bind a non-loopback address (`0.0.0.0:8080`), livereload and dev MCP turn off unless you opt in with `GOFASTR_DEV_LIVERELOAD_EXPOSE=1` or `GOFASTR_DEV_MCP_EXPOSE=1` (exposing dev MCP grants unauthenticated remote access).
- **Uncached project assets:** static files serve with `Cache-Control: no-store` in dev mode so CSS/JS edits never stick in browser cache.

## Layout inspector (`Alt+Shift+L` / `Option+Shift+L`)

Press **Alt+Shift+L** on any dev page to toggle an overlay outlining `data-cui-layout` (purple), `data-cui-layout-slot` (blue), outlets/areas (teal), style scopes (amber), islands/widgets (green/pink), scroll boxes (grey), and **horizontal viewport overflows** (red, with pixel count). Resize to mobile widths to spot layout overflow instantly. Programmatic API: `window.__gofastrDevLayout` (`show()`, `hide()`, `toggle()`, `findings`).

## Dev loop + agents

Under `gofastr dev`, GoFastr mounts `/mcp` automatically with introspection tools (`app_routes`, `app_readiness`, `app_config`), module toggles, all CRUD entity data tools, and `battery/log` debug tools (`log_recent`, `log_filter`, `log_metrics`, `log_set_level`). See [Agent-ready](agent-ready.md).

## Common mistakes

- **Setting `GOFASTR_ENV=production` locally and wondering why livereload stopped:** `production` overrides `GOFASTR_DEV=1`.
- **Registering `/__livereload` or adding `/__livereload.js` to `WithExtraScripts` manually:** already wired automatically in dev mode; doing it by hand causes route collisions or duplicate script tags.
- **Using `GOFASTR_DEV_MCP_EXPOSE=1` on a shared network:** exposes unauthenticated dev MCP tools to anyone who can reach the port.
