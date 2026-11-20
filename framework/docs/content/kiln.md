# Kiln: agent-driven build mode (experimental)

> **Experimental:** Kiln is a standalone local dev binary (`cmd/kiln`) for shaping a GoFastr app live with an AI agent. When you graduate, `kiln freeze` writes a standard `gofastr.yml` app spec and you generate plain Go code you own.

Kiln gives an agent a journaled application IR (`.kiln.session.jsonl`). Every accepted tool call replays into a live GoFastr app, migrates SQLite, and refreshes the browser through GoFastr's normal SSR + hydration host (with entity CRUD mounted under `/api/<entity>`).

## Starting Kiln

```bash
# Turnkey agent loop: starts the server and runs the built-in adapter
kiln agent -p "build a small issue tracker"

# Browser panel mode with an adapter (omp, claude-code, pi, codex, auto, or custom command)
kiln serve --agent auto
```

Default bind: `127.0.0.1:8765` (loopback-only). Important routes:
- `GET /kiln/world` / `GET /kiln/status` — current redacted world IR and status
- `POST /kiln/tool/<name>` — JSON authoring tool dispatch
- `POST/GET /mcp` — Kiln authoring tools over Streamable HTTP MCP (`kiln mcp` speaks stdio; `kiln acp` speaks [ACP](acp.md))
- `POST/GET /mcp/app` — the rebuilt preview app's own entity MCP tools
- `GET /.kiln/events` — journal/rebuild SSE stream

## Authoring tools and safety gates

Agents mutate the app through Kiln's typed tools rather than editing files directly:
- **Config & theme:** `world_get` (masks `jwt_secret`, `seed_password`, and credentialed `db_url`), `set_app_config` (`{"config":{"name":...}}`), `set_theme`, `set_scaffold`.
- **Entities & data:** `add_entity`, `update_entity`, `delete_entity`, `add_field`, `delete_field`, `add_seed` (supports owner scoping via `owner_field`; rejects `multi_tenant: true` because tenant middleware belongs in owned Go).
- **Pages & behavior:** `add_page`, `update_page_element` (optimistic version checks), `delete_page`, `add_hook`, `delete_hook`, `add_route`, `delete_route`. Page nodes use `framework/ui` component kinds and reject `class`, `style`, and `on*` props.
- **Plans & undo:** `propose_plan`, `approve_plan`, `reject_plan`, `undo`, `reset_session`. Destructive deletes and `update_entity` require a single-use approved plan (enforced at both call time and journal replay). `undo` truncates the journal and deterministically rebuilds the preview DB from seeds.

## Graduating to owned Go

Freeze the journal into a deterministic `gofastr.yml` (plus lossless `world.json`) and run the one-shot app generator:

```bash
kiln freeze --diff
kiln freeze --dir build
gofastr validate build/gofastr.yml
gofastr generate app build/gofastr.yml --out=app
cd app && go test ./...
```

Notes on freezing:
- Credentialed database URLs are replaced with `${DATABASE_URL}` rather than written into `gofastr.yml`.
- `app.json_case` must be camelCase (or empty) to freeze for generated clients.
- Declarative hooks and entity endpoints graduate as owned Go handler stubs to implement in your generated app.

## Security boundary

Kiln is **local-development only**. Its loopback bind (`127.0.0.1`) and Host/Origin checks are the security boundary; `.kiln.session.jsonl` is written `0600` inside a `0700` directory, and every agent turn runs in its own isolated temp directory and process group (killed cleanly on cancel or shutdown). Do not bind `--addr 0.0.0.0` without an external auth and network boundary.

## Common mistakes

- **Passing flat fields to `set_app_config`:** wrap them in `{"config":{"name":"My App",...}}`.
- **Targeting bare `/notes` instead of `/api/notes` in preview forms:** entities mount under `app.api_prefix` (default `/api`).
- **Passing `class`, `style`, or `on*` props on Kiln page nodes:** rejected to enforce the single design system.
- **Treating `world.json` as production code:** graduate via `kiln freeze` → `gofastr generate app` and own the Go files.
- **Binding Kiln to a public network interface:** its mutation tools are unauthenticated local dev endpoints.
