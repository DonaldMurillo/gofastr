# Kiln: agent-driven build mode

> **Experimental.** Kiln is a separate binary for shaping a GoFastr app live.
> When you graduate, it writes the same `gofastr.yml` app spec the generator
> already reads: a scaffolder, not the thing you ship. The generated Go code
> is what you keep; it's the source of truth, not the app spec.

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

`kiln agent` writes its journal to `.kiln.session.jsonl` in the current
directory. If the serve it starts exits before answering (a journal it cannot
open, a bad flag), `kiln agent` prints the serve's error and exits 1 without
launching OMP.

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

The three read routes (`/kiln/world`, `/kiln/status`, `/.kiln/events`)
refuse a browser request whose `Origin` is cross-site, and one whose
`Host` is not a loopback address when an `Origin` is present (a
rebound page and the listener agree on the attacker-named host, so only
the Host pin catches it). Write routes pin `Host` to loopback whenever
the listener bound a loopback address, `Origin` header or not.
Requests without an `Origin` header, the agent transports and `curl`,
pass untouched. `/.kiln/events` seats each peer host at 16 concurrent
streams (429 past it), and the JSON GETs answer `Cache-Control:
no-store`. `Live.ServeHTTP` sets an `X-Kiln-Server` header; the
`waitReady` probe requires it and fails when serve exits, instead of
hanging on a port held by something else.

`kiln mcp` exposes the same tools over stdio MCP. `kiln acp` speaks the
published Agent Client Protocol v1 instead (see
[ACP](/docs/acp)): `initialize` → `session/new` → `session/prompt`, with
tool invocations streamed as `session/update` `tool_call` frames and
`approve_plan` gated on `session/request_permission`. The CLI attaches no
model to ACP turns, so prompts are journaled and refused with a pointer at
`kiln mcp` / the panel; embedders attach one with `kiln/acp.WithProvider`.
Chat messages an ACP turn journals carry `acp_session` (also in the
`/kiln/chat` JSON), and `session/load` replays only that session's own
messages. A cancelled prompt journals no further text and runs no
further tool calls. Pass `--no-http` when a parent process owns the UI.

## Agent adapters

Built-ins are `omp`, `claude-code`, `pi`, and `codex`. `auto` chooses the first
installed adapter in that order. `none` keeps chat journaled without spawning
an agent. A free-form command is parsed as an executable plus arguments and
receives the prompt as its final argument, after `--`. The `omp` and
`claude-code` adapters put `--` before the prompt too, so a chat message
that starts with a dash (`--fix the header`) reaches the agent as text
instead of being parsed as a flag. A custom CLI has to accept `--` as the
end of its options, as most argument parsers do. A command that already
ends in `--` gets no second one.

Kiln injects `KILN_URL`, installs the current embedded skill, and enforces an
HTTP boundary for built-in adapters: they must mutate the app by calling
Kiln's tools, not by editing the repository.

Each turn's adapter runs in its own process group on Unix. Cancelling,
superseding or switching away from a turn kills the whole group, and so does
the turn ending: a shell the adapter left in the background does not keep
calling Kiln. On Windows only the adapter process itself is killed.
Stopping `kiln serve` (Ctrl-C or SIGTERM) cancels a running turn the same
way and waits, up to a few seconds, for the adapter to be killed before the
process exits.

## Tools

Read and configuration:

- `world_get`: the whole world, or one part of it by `path`
  (`entities.<name>`, `pages.<path>`, `_chat`, `_plans`); the whole
  world comes back with `jwt_secret`, `seed_password` and a credentialed
  `db_url` masked as `[redacted]`, the same masking `GET /kiln/world`
  applies (the in-process world keeps the real values for freeze)
- `set_app_config`: current app spec app config; HTTP calls use
  `{"config":{...}}`, a non-empty `config.name` is required, and an
  omitted/empty API prefix resolves to `api`
- `set_theme`: semantic light theme token overrides. Both config tools
  write the config they read; when another config edit lands in between
  they answer `kind: "conflict"`, and the call is safe to send again
- `set_scaffold`: navigation plus owned-Go endpoint, middleware, plugin, and
  helper stubs

Entities and data:

- `add_entity`, `update_entity`, `delete_entity`: an entity whose CRUD
  mount (`/<api_prefix>/<table>`) is an existing page's path is refused
  with `kind: "conflict"`; `delete_entity` removes the entity's seeds
  and hooks in the same edit, and `undo` brings them back; a
  `delete_entity` whose drop SQLite refuses (another table's rows still
  point at it) fails and leaves every table and row as it was
- `add_field`, `delete_field`: deleting a field, or an `update_entity`
  that leaves one out, removes that column from the entity's seed rows
  and weights in the same edit, keeps a row left with only its `id`
  (another seed may name it), and `undo` brings them back. The column also leaves the live table. A
  UNIQUE, key, indexed or foreign-key column, which SQLite's
  `DROP COLUMN` refuses, and an `update_entity` that makes a field
  optional or no longer unique, rebuild the table from the declared
  entity with its rows copied across. Rows the rebuilt table refuses (a
  NULL in a required column `add_field` could only add as nullable)
  refuse the edit, and the table stays as it was
- `add_seed`: rows go into the entity's table, its declared `table` or
  else the framework default (`blogPosts` → `blog_posts`); a seed for an
  entity the world does not hold is refused, and so is a row key that is
  neither a declared field nor `id` (`kind: "validation"`, naming the row
  and the key). A seed for an entity that already has one adds rows; the
  freeze merges them into one block. A row that leaves out `id` (or sets
  it to null) gets a generated UUID, and one that leaves out `created_at`
  or `updated_at` on an entity with timestamps gets the current time, as a
  CRUD create would. A generated id is journaled with the seed, so boot,
  reset and undo rebuild the row with the same id and another seed can
  refer to it; the implicit `created_at`/`updated_at` are stamped again on
  each rebuild. A `json` field's object, array,
  number or boolean is stored as its JSON text, as a CRUD create stores
  it; a string is stored as written, so it must be JSON text already

Screens and behavior:

- `add_page`, `update_page_element`, `delete_page`: a page path starts
  with `/` and holds no parameter (`:id`, `{id}`); an element's actions
  are kinds the effect runtime supports; a page on an entity's CRUD
  mount (`/api/posts`) is refused with `kind: "conflict"`
- `add_hook`, `delete_hook`: `when` is one of `before_create`,
  `after_create`, `before_update`, `after_update`, `before_delete`,
  `after_delete`, `before_list`, `after_list`; a hook action is `noop`,
  `validate`, `set_field`, `audit` or `emit_event`
- `add_route`, `delete_route`: the method and path parse as a
  `net/http` pattern; a route action is `noop` or `respond_json`; a route
  that overlaps one the app already serves (`GET /x/{id}` and
  `GET /{name}/y` both match `/x/y`, neither more specific) is refused
  with `kind: "conflict"` naming the other pattern

These are refused at the call with `kind: "validation"`. Replay keeps
the looser rules earlier builds journaled under (any supported action
kind on a hook or route, a relative page path), so an old session
still loads.

Safety and session:

- `propose_plan`, `approve_plan`, `reject_plan`
- `undo`, `reset_session`, `chat`

Every transport uses the same typed dispatcher. Destructive deletes
and `update_entity` require an approved plan naming the exact
operation and target (`update_entity` carries the `PlanID` in its
journal entry; journals written before the gate existed fail replay
with a pointer at the entry). An approval is
single-use, and only an edit that lands spends it: a delete refused
because its target is absent leaves the plan unspent. `undo` reverts the
most recent world edit and deterministically rebuilds: the journal is cut
at that edit, with the `tool_call` envelope the HTTP tool API journals
before it, so the edit's `tool_result` and anything journaled after it
go too; a journal with no world edit has nothing to undo.
`reset_session` clears the journal and ephemeral schema. The runtime
database is derived from the journal: boot, `undo`, and `reset_session`
drop every table, re-migrate, and re-apply the world's seeds, and an
approved `delete_entity` drops the entity's table, so rows from a reset
or undone world cannot resurrect through the CRUD surface. That includes
rows written through the CRUD API: after an `undo`, only seed rows
remain. Tables linked by foreign keys drop together whatever rows link
them. A rebuild that fails anyway (an unwritable database) fails the
`undo` or `reset_session` with an internal error, and fails boot,
instead of serving rows the journal does not hold. A seed's
rows are inserted in one transaction before its journal entry is
durable; a seed the database refuses part way (a unique conflict, say)
lands none of its rows and leaves the rows already in the database,
CRUD writes included, where they were. A journal whose last line was
torn by a crash loses exactly that in-flight entry at open. An append
that fails while kiln runs (a full disk, a failed fsync) cuts its bytes
back off, so the next append starts a clean line; if the cut fails too,
kiln refuses further appends until it restarts, and the restart heals
the fragment.

Two dispatch-side postures: an internal failure of a tool (a panic, a
filesystem error) answers the caller with a generic "internal error"
(`Kind: internal`) while the real error is logged server-side — a
gate refusal still names `invalid_params`, and a malformed hook, route
or page (an unknown event or action kind, a bad method, a relative path)
answers `Kind: validation` with the reason — and journaled tool args
mask credentialed values (`db_url`, `jwt_secret`, `seed_password`)
the same way `world.Redacted` does (`dsnredact.HasSecret` decides
what counts), so the journal file never gains a secret the world
itself would not carry.

On ACP, every `Destructive` tool call round-trips a
`session/request_permission` before it runs, the same gate
`approve_plan` uses; `undo` is not classed Destructive and stays
ungated.

Those rules are enforced during **replay**, not only at the tool call. The
journal is the authorization record, so a destructive entry carries the
`plan_id` that authorized it and replay re-checks that the plan exists, was
approved, lists that exact target, and has not already been spent on it.
Consumption is derived from the log too. It used to live in a per-process
map that replay never rebuilt, so a restart re-armed every spent approval.
The `multi_tenant` refusal runs on the same path.

This matters because the journal is replayed at boot and by `kiln freeze`,
which turns the replayed world into an app spec. A hand-authored
`.kiln.session.jsonl` used to install world state the tool API refuses. It
needs a local filesystem write, so it is an integrity property rather than a
remote one, but a guard only one of two readers applies is not a guard.

## Current world contract

The world mirrors the current app spec where a live representation is safe:

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
registry. Every live world edit and an `undo` refresh the current screen
through a forced SPA navigation: the build-mode client at
`/.kiln/reload.js` listens on `/.kiln/events` for `world_edit`, `undo`
and `session_reset`. A same-route navigation keeps the head and the
shared layout, so `set_app_config` (theme), `set_scaffold` (nav), an
undo of either, and `reset_session` reload the document instead, as
`framework/dev`'s livereload does on a new build. So does the first
connection after a dropped one: `/.kiln/events` does not replay missed
events, so the client cannot tell whether it missed a shell edit. A
page's first connection checks too: the `w` query on its
`/.kiln/reload.js` names the world it was rendered from, the stream's
`ready` event carries `{"world": …}` for the world served now, and the
page reloads when they differ.

Prefer design-system kinds:

- `page_header`, `hero`, `section`, `card`
- `stack`, `cluster`, `grid`, `stat_row`, `stat_grid`, `stat_card`
- `link_button`, `callout`, `divider`

Semantic leaf kinds such as `heading`, `paragraph`, `text`, `link`, `form`,
`input`, `button`, `list`, and table elements remain available. `class`,
`style`, and `on*` props are rejected: pages use the shared design system
instead of a second way to style things. Form actions that target CRUD
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
`update_page_element` performs optimistic edits to that tree using those IDs
and an optional `if_match` version. The version is checked again when the
patch applies, with or without `if_match`: a patch that another edit of the
same page beat to the apply answers `kind: "conflict"` with a refetch hint,
so concurrent patches never overwrite each other's edits. A page deleted
and added again also starts at version `1`, so each `add_page` gives the
page a new `gen`, and a patch read from the earlier page is a conflict
too, even when the versions match. `if_match` compares only the version.
Re-read the page and send the patch again. A session journal written before this check may
hold two patches built from one version; it still loads, and the later
patch wins.

## Graduate to owned Go

```bash
kiln freeze --diff
kiln freeze --dir build
gofastr validate build/gofastr.yml
gofastr generate app build/gofastr.yml --out=app
cd app
go test ./...
```

Freeze only reads the journal, so it is safe to run while `kiln serve` is
still running: it opens the file read-only, skips a last line that has no
newline yet (an append in flight), and never truncates anything.

Freeze writes exactly:

- `build/gofastr.yml`: deterministic current app spec, ready for the
  one-shot generator;
- `build/world.json`: lossless authoring snapshot.

A database URL with an embedded password never lands in either file:
freeze writes a `${DATABASE_URL}` reference instead, and the generated
app refuses to boot until the variable is supplied (credential-free DSNs,
like SQLite file paths, stay verbatim).

Declarative Kiln hook actions remain exact in `world.json`. Where the
current app spec requires a Go function, freeze emits an owned-Go handler
stub with a description naming the declarative action; implement that behavior
after generation. Endpoints graduate the same way, into the app spec's
top-level `endpoints:` list: each under its entity, its path relative to the
entity's route, linking `entities/<entity>_endpoints.go#<Name>`. Entity
endpoints are part of the world IR but are not served by the live build-mode
app (rendering one needs run-then-respond semantics kiln/effect does not
provide). Freeze refuses a world route, a scaffold endpoint with no entity,
and an endpoint whose path sits outside its entity's route: none has an app
spec spelling, so delete it and register the route in owned Go after
generation. The removed pre-v0.1 `entities/*.json`
format is not emitted.

Each page freezes to one screen. A page without a `name` takes one from
its path (`/blog/posts` → `blog_posts`, `/` → `home`); when a named page
or an earlier path already holds that name, it gains a suffix, so `/`
and `/home` freeze to `home` and `home_2`.

Freeze writes one `seed:` block per entity: several seeds for one entity
merge into the first one's place, rows in order, counts summed and weights
joined. Two seeds that weight the same column differently are refused,
naming the entity and the column.

Freeze fails loudly, naming the offending key, when world data cannot
round-trip through the app spec's YAML subset: a seed row whose every value
is a nested object, or a props/row key containing a colon, comment marker, or
brackets. Reshape the data (or drop the row) and re-run; a freeze that
succeeded is guaranteed to re-parse into the same values. An empty object
(a JSON column holding `{}`) freezes as a bare `key:` line, the block-style
empty map the YAML subset reads back.

`FreezeAndGenerate(ctx, world, dir, out)` performs the freeze and invokes
`gofastr generate app gofastr.yml` when `gofastr` is on `PATH`. The
generator runs under `ctx`, and its output is collected for at most two
seconds after it exits.

## Security boundary

Kiln's HTTP endpoints are intentionally unauthenticated and local-development
only. The default loopback bind is the primary boundary. Unsafe cross-origin
browser requests are rejected; non-browser clients without an `Origin` header
and same-origin requests are allowed. Binding `--addr 0.0.0.0:8765` is an
explicit decision to expose those endpoints and should only be done behind an
appropriate network/auth boundary.

Three further postures hold on every surface. JSON request bodies decode
strictly: a body that fails to parse, or carries duplicate or case-folded
keys, is refused with `400` rather than resolved last-wins. Every response
Kiln serves carries the framework's security headers (`nosniff`, frame and
CSP policy included). And `.kiln.session.jsonl` is created owner-only
(`0600` in a `0700` directory), because its entries embed the same secrets
freeze writes owner-only. Windows journal writes protect the open file's ACL
before writing any entries. Built-in agent adapters run each turn in a fresh
`0700` working directory under their registered temp path; a directory Kiln
does not own is refused, not adopted.

## Verification

From the framework checkout on Windows, ensure an MSYS2 GCC is on `PATH` so
`go-sqlite3` can run, then:

```bash
go test ./cmd/kiln ./kiln/...
go test ./cmd/gofastr -run TestKilnFreezeAppSpecParsesAndValidates
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
  suite validates adapters and protocol behavior; build with `-tags kilnlive`,
  set `KILN_LIVE=1`, and run a named `TestLive_*` case to prove the selected
  external driver actually edits, renders, freezes, and generates the current
  world.
