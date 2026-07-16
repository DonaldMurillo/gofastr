# Kiln — agent-driven build mode

> **Experimental.** Kiln is a separate binary for shaping a GoFastr app live.
> Its durable graduation artifact is the same `gofastr.yml` blueprint consumed
> by the current generator; generated Go remains the source of truth.

Kiln gives an agent a small, journaled application IR. Each accepted edit is
replayed into a fresh framework app, SQLite is migrated, and the browser sees
the result through GoFastr's normal SSR + hydration host. REST CRUD defaults to
`/api/<entity>` so an HTML screen may safely use `/<entity>`.

## Start it

```bash
# Turnkey path: starts the server, installs the Kiln skill, and runs OMP/GLM-5.2.
kiln agent -p "build a small issue tracker"

# Panel-driven mode with the same built-in adapter.
kiln serve --agent omp

# Other installed adapters or a custom command remain available.
kiln serve --agent claude-code
kiln serve --agent pi
kiln serve --agent codex
kiln serve --agent auto
kiln serve --agent "omp -p --model glm-5.2"
```

The `omp` adapter runs OMP in an isolated temporary working directory with
`--model glm-5.2`, the Bash tool, auto-approval, no session persistence, and
Kiln's system prompt. Authentication stays with the installed CLI; Kiln does
not store provider credentials.

The server binds to `127.0.0.1:8765` by default. Open any current screen or the
empty-state host and use the floating panel. Important endpoints are:

| Endpoint | Purpose |
|---|---|
| `GET /kiln/world` | Current lossless world IR. |
| `GET /kiln/status` | Runtime status. |
| `POST /kiln/tool/<name>` | JSON tool dispatch. |
| `POST/GET /mcp` | Kiln authoring tools over Streamable HTTP MCP. |
| `POST/GET /mcp/app` | Rebuilt app's entity MCP tools. |
| `GET /.kiln/events` | Journal/build SSE stream. |

`kiln mcp` and `kiln acp` expose the same tool surface over stdio. Pass
`--no-http` when a parent process owns the UI.

## Agent adapters

Built-ins are `omp`, `claude-code`, `pi`, and `codex`. `auto` chooses the first
installed adapter in that order. `none` keeps chat journaled without spawning
an agent. A free-form command is parsed as an executable plus arguments and
receives the prompt as its final argument.

Kiln injects `KILN_URL`, installs the current embedded skill, and enforces an
HTTP boundary for built-in adapters: they must mutate the app through the tool
surface, not by editing the repository.

## Tool surface

Read and configuration:

- `world_get`
- `set_app_config` — current blueprint app config; HTTP calls use
  `{"config":{...}}`, a non-empty `config.name` is required, and an
  omitted/empty API prefix resolves to `api`
- `set_theme` — semantic light theme token overrides
- `set_scaffold` — navigation plus owned-Go endpoint, middleware, plugin, and
  helper stubs

Entities and data:

- `add_entity`, `update_entity`, `delete_entity`
- `add_field`, `delete_field`
- `add_seed`

Screens and behavior:

- `add_page`, `update_page_element`, `delete_page`
- `add_hook`, `delete_hook`
- `add_route`, `delete_route`

Safety and session:

- `propose_plan`, `approve_plan`, `reject_plan`
- `undo`, `reset_session`, `chat`

Every transport uses the same typed dispatcher. Destructive deletes require an
approved plan naming the exact operation and target. An approval is
single-use. `undo` truncates one journal entry and deterministically rebuilds;
`reset_session` clears the journal and ephemeral schema.

## Current world contract

The world mirrors the current blueprint where a live representation is safe:

- app module/database/static/output/API prefix, auth/admin/PWA, light and dark
  semantic theme tokens;
- current entity fields, relations, indices, access, owner scoping, search,
  cursor fields, properties, hooks, declarative routes, and seeds;
- screen metadata, layouts, access declarations, node trees, and navigation;
- owned-Go endpoint/middleware/plugin/helper stubs.

`multi_tenant: true` is rejected by authoring and freeze because neither Kiln
nor the generator can guess the app-specific tenant resolver. Use
`owner_field` for per-user CRUD, or wire tenant middleware in owned Go after
graduation. Auth/admin declarations are preserved into the scaffold; Kiln's
live preview is not a production auth certification environment.

## UI composition

Kiln pages run through `framework/uihost`: full SSR on first load, hydration of
the existing DOM, SPA navigation across screens, and component CSS from the
registry. Live world edits refresh the current screen through a forced SPA
navigation, never `location.reload()`.

Prefer design-system kinds:

- `page_header`, `hero`, `section`, `card`
- `stack`, `cluster`, `grid`, `stat_row`, `stat_grid`, `stat_card`
- `link_button`, `callout`, `divider`

Semantic leaf kinds such as `heading`, `paragraph`, `text`, `link`, `form`,
`input`, `button`, `list`, and table elements remain available. `class`,
`style`, and `on*` props are rejected: pages compose the shared design system
instead of inventing a second styling surface. Form actions that target CRUD
must use the API prefix, for example `/api/notes`.

Example:

```json
{
  "page": {
    "path": "/dashboard",
    "name": "dashboard",
    "title": "Dashboard",
    "layout": {"name": "app"},
    "tree": {
      "kind": "stack",
      "props": {"gap": "lg"},
      "children": [
        {"kind": "page_header", "props": {
          "title": "Dashboard", "subtitle": "Current project health"
        }},
        {"kind": "stat_row", "children": [
          {"kind": "stat_card", "props": {"label": "Open", "value": "12"}},
          {"kind": "stat_card", "props": {"label": "Closed", "value": "48"}}
        ]}
      ]
    }
  }
}
```

`add_page` assigns stable node `_id` values and version `1`.
`update_page_element` performs optimistic, surgical tree edits using those IDs
and an optional `if_match` version.

## Graduate to owned Go

```bash
kiln freeze --diff
kiln freeze --dir build
gofastr validate build/gofastr.yml
gofastr generate --from=build/gofastr.yml --out=app
cd app
go test ./...
```

Freeze writes exactly:

- `build/gofastr.yml` — deterministic current blueprint, ready for the
  one-shot generator;
- `build/world.json` — lossless authoring snapshot.

Declarative Kiln hook/route actions remain exact in `world.json`. Where the
current blueprint requires a Go function, freeze emits an owned-Go handler
stub with a description naming the declarative action; implement that behavior
after generation. The removed pre-v0.1 `entities/*.json` format is not emitted.

Freeze fails loudly, naming the offending key, when world data cannot
round-trip through the blueprint's YAML subset — a seed row whose every value
is a nested object, or a props/row key containing a colon, comment marker, or
brackets. Reshape the data (or drop the row) and re-run; a freeze that
succeeded is guaranteed to re-parse into the same values.

`FreezeAndGenerate` performs the freeze and invokes
`gofastr generate --from=gofastr.yml` when `gofastr` is on `PATH`.

## Security boundary

Kiln's mutation surface is intentionally unauthenticated and local-development
only. The default loopback bind is the primary boundary. Unsafe cross-origin
browser requests are rejected; non-browser clients without an `Origin` header
and same-origin requests are allowed. Binding `--addr 0.0.0.0:8765` is an
explicit decision to expose the tool surface and should only be done behind an
appropriate network/auth boundary.

## Verification

From the framework checkout on Windows, ensure an MSYS2 GCC is on `PATH` so
`go-sqlite3` can run, then:

```bash
go test ./cmd/kiln ./kiln/...
go test ./cmd/gofastr -run TestKilnFreezeBlueprintParsesAndValidates
```

The integration suite covers journal replay, live migrations, CRUD, hooks,
browser form submission, UI hosting, and freeze artifacts. Live provider tests
remain opt-in because they consume external model credentials.

## Common mistakes

- **Passing flat app fields.** `set_app_config` requires
  `{"config":{"name":"My App",...}}`. A flat payload is rejected so it cannot
  appear to succeed while silently discarding configuration.
- **Using bare CRUD paths.** Entities mount below `app.api_prefix`, which
  defaults to `/api`; point forms and clients at `/api/<entity>` unless the
  world explicitly changes that prefix.
- **Adding page-local classes or styles.** Kiln page nodes reject `class`,
  `style`, and `on*` props. Compose an existing typed component, or add a
  missing component/token to the design system before using it in Kiln.
- **Treating `world.json` as generated application code.** It is the lossless
  authoring snapshot. `gofastr.yml` is the generator input, and declarative
  behaviors that require Go graduate as explicit owned stubs to implement.
- **Exposing the mutation server.** Keep Kiln on loopback. Binding a public
  interface without an external authentication/network boundary exposes every
  world-editing tool.
- **Calling a provider run verified without using it live.** The deterministic
  suite validates adapters and protocol behavior; set `KILN_LIVE=1` and run a
  named `TestLive_*` case to prove the selected external driver actually edits,
  renders, freezes, and generates the current world.
