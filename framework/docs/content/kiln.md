# Kiln — agent-driven build mode


> **Experimental.** Kiln is the framework's most provisional surface.
> The in-memory IR, journal-freeze format, and blueprint graduation
> flow may still change between releases. Build with it; don't pin a
> production path on it yet.

Kiln is a separate binary that lets an AI agent (Claude Code, pi,
Codex, any CLI with `KILN_URL`) build a GoFastr app live by mutating
an in-memory IR over HTTP. The world re-renders, the schema migrates,
and a chat panel streams the conversation — all in-process. Freeze
the journal when done to emit a canonical snapshot of the built world,
then graduate to a `gofastr.yml` blueprint (or hand-written Go) that you
commit and generate from.

This page is an overview. Source of truth: read the package docs in
`kiln/` and the CLI help (`kiln serve -h`).

## Install

```bash
go install ./cmd/kiln
```

The binary builds independently of the main `gofastr` CLI; install
both if you use both.

## Subcommands

| Subcommand    | Effect                                                          |
|---------------|-----------------------------------------------------------------|
| `kiln serve`  | HTTP server only: panel + SSE + REST tool dispatch + MCP at `/mcp`. |
| `kiln mcp`    | HTTP + MCP on stdio (for subprocess harnesses).                 |
| `kiln acp`    | HTTP + ACP on stdio (for ACP-attached harnesses).               |
| `kiln agent`  | Run an embedded agent loop against a remote Kiln instance.      |
| `kiln freeze` | Read the journal and emit canonical `entities/*.json` + `world.json`. |

In stdio modes the HTTP panel keeps running so you can watch the world
build live. Logging goes to stderr; stdout is reserved for JSON-RPC.

## Picking an agent

```bash
kiln serve --agent claude-code   # uses ~/.claude/.credentials.json
kiln serve --agent pi            # uses pi's installed config
kiln serve --agent auto          # picks the first installed CLI on PATH
kiln serve --agent "<freeform>"  # any command; the prompt is appended
```

Kiln subscribes to its own SSE bus: every `chat_user` event spawns the
configured CLI as a subprocess with `KILN_URL` injected. The CLI reads
the `~/.claude/skills/kiln/SKILL.md` file (auto-installed) and drives
the build with `curl` against HTTP. Stdout is journaled back as
`chat_assistant` so the panel renders the reply.

## Bring-your-own auth

Kiln does **not** manage credentials. Each adapter spawns its CLI
which manages its own login (`claude` reads `~/.claude/.credentials.json`,
`pi` reads its own config, etc.). Adding a new agent is a one-entry
change in `cmd/kiln/adapters.go`.

## Mutation surface & loopback binding

The tool API (`POST /kiln/tool/{name}`, `/kiln/agent`, `/mcp`) mutates the
in-memory world **without authentication** — Kiln is local build-mode
tooling. The primary control is the **loopback bind** (`--addr` defaults to
`127.0.0.1:8765`); pass `--addr 0.0.0.0:8765` only when you deliberately
want it reachable off-box, and put your own auth in front of it.

As defense-in-depth, an **same-origin guard** rejects cross-origin
browser-driven state changes (a malicious web page or DNS-rebinding POSTing
to `localhost:8765`). Non-browser clients (the agent, `curl`, MCP/ACP) send
no `Origin` and are unaffected.

## Plan-gated destructive ops

Destructive tools (`delete_entity`, `delete_field`, `delete_page`,
`delete_hook`, `delete_route`) are enforced at the protocol layer:

1. The agent calls `propose_plan` listing each destructive op in
   `targets`:

   ```json
   { "plan_id": "p1", "steps": ["drop posts"], "targets": [{"op":"delete_entity","name":"posts"}] }
   ```

2. The panel renders a plan card with **Approve** / **Reject** buttons.
3. After Approve, the agent retries the destructive call with `plan_id`
   set.

Without an approved plan whose `targets` list matches, `delete_*`
returns `{"ok":false,"kind":"needs_plan"}`. Each `(plan, target)` is
single-use; reuse needs a new plan.

## Tool surface

`world_get`, `set_app_config`, `add_entity`, `update_entity`,
`delete_entity`, `add_field`, `delete_field`, `add_page`, `delete_page`,
`add_hook`, `delete_hook`, `add_route`, `delete_route`, `add_seed`,
`propose_plan`, `approve_plan`, `reject_plan`, `undo`, `chat`. See
`kiln/protocol/descriptors.go` for the full JSON schemas. A tool call over
HTTP is a plain POST against the loopback server:

```bash
kiln serve --agent none &   # loopback 127.0.0.1:8765 by default; unauthenticated
curl -X POST http://localhost:8765/kiln/tool/add_entity \
  -H 'Content-Type: application/json' \
  -d '{"entity":{"name":"posts","fields":[{"name":"title","type":"string","required":true}]}}'
curl http://localhost:8765/posts          # CRUD live
curl http://localhost:8765/kiln/world     # current IR
```

## Wire into Claude Code as an MCP server

```json
{
  "mcpServers": {
    "kiln": { "command": "kiln", "args": ["mcp", "--no-http"] }
  }
}
```

## Generated apps don't carry Kiln

`gofastr generate --from <blueprint>` emits a plain framework app. Node
trees render through the leaf package **`kiln/noderender`** (which imports
only `core-ui/html`, `core/render`, and the zero-dependency `kiln/world`
IR) — **not** `kiln/render`, which pulls Kiln's authoring engine
(`kiln/expr`, `kiln/effect`, `framework`). So a shipped, frozen app does
not link the build-mode evaluator. A codegen build test asserts this:
the generated screens package compiles and its dependency graph excludes
`kiln/expr` / `kiln/effect` / `kiln/render`.

## Freezing

When the build is done:

```bash
kiln freeze --dir build/
```

This reads the journal and emits:

- `build/entities/*.json` — one declaration per entity, as a readable
  snapshot of the frozen world's entities.
- `build/world.json` — the canonical world IR snapshot.

You commit these files; the running Kiln process is no longer needed.
To graduate to a running framework app, declare the frozen entities in a
`gofastr.yml` blueprint (the snapshot makes a faithful starting point — see
[Blueprints](blueprints.md)) or write them in Go with `app.Entity(...)`, then
run `gofastr generate --from=gofastr.yml`. There is no file-based runtime
loader; the generated `entities.RegisterAll(app)` wires them in.

## Free-order authoring & durability

Build mode is **free-order**: you can add `posts` (with a `BelongsTo users`
relation) before `users` exists. The live rebuild defers a `BelongsTo`
whose target entity isn't registered yet and re-derives it once the target
is added — the durable world (and `kiln freeze`) always keep the full
relation. The framework's strict `AutoMigrate` still rejects a dangling
`BelongsTo` outside build mode; only the kiln live runtime defers.

Every mutation is **validated by a trial rebuild before it is journaled**.
An entry that can't be rebuilt is rejected and never written to the
durable log — so a poison entry can't survive a restart and brick the
session. On any failure the in-memory session is restored by replaying the
journal.

## Architecture

Kiln is bigger than a single doc page; the layout under `kiln/`:

- `kiln/world` — in-memory IR for entities, fields, relations, pages.
- `kiln/journal` — append-only event log; the basis for replay and
  freeze.
- `kiln/effect` — typed effects the agent fires; the world applies them.
- `kiln/expr` — small expression language for hooks/computed fields.
- `kiln/freeze` — IR → canonical declarations.
- `kiln/render` — live UI render of the current world.
- `kiln/live` — SSE bus + state subscription.
- `kiln/protocol` — wire formats for HTTP + MCP + ACP.
- `kiln/agent/mcp` — MCP server exposing kiln tools.
- `kiln/agent/acp` — ACP server exposing kiln tools.
- `kiln/integration` — end-to-end tests against a real subprocess agent.

## Forms in the kiln world

Kiln-rendered `form` nodes default `enctype="application/json"`
because they target the world's CRUD endpoints (which decode JSON, not
urlencoded). This is the **opposite** of the framework default — bare
`<form>` elements in hand-written HTML submit browser-native, kiln
forms intercept.

To opt out per-form via the world API, set `enctype` explicitly:

```yaml
- kind: form
  props:
    method: POST
    action: /notes
    enctype: application/x-www-form-urlencoded  # browser-native submit
```

For RPC-island form submission (no navigation, JSON response signal),
use `data-fui-rpc` via `attrs`:

```yaml
- kind: form
  props:
    action: /api/notes
    attrs:
      data-fui-rpc: "/api/notes"
      data-fui-rpc-signal: notes-state
```

## Common mistakes

- **Treating Kiln as a runtime.** It's a build-time tool. Once you
  freeze, the running Kiln binary is not part of your app.
- **Editing `entities/*.json` and then re-running Kiln on top.**
  Kiln expects to own the world while it's running. Hand-edits should
  happen post-freeze, after Kiln has exited.
- **Storing credentials in `cmd/kiln/adapters.go`.** Adapters spawn
  CLIs; credentials live wherever those CLIs already keep them.
