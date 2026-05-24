# Kiln — agent-driven build mode

Kiln is a separate binary that lets an AI agent (Claude Code, pi,
Codex, any CLI with `KILN_URL`) build a GoFastr app live by mutating
an in-memory IR over HTTP. The world re-renders, the schema migrates,
and a chat panel streams the conversation — all in-process. Freeze
the journal when done to emit canonical `entities/*.json` and graduate
to regular Go source you commit.

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

## Freezing

When the build is done:

```bash
kiln freeze --dir build/
```

This reads the journal and emits:

- `build/entities/*.json` — one declaration per entity, ready to load
  with `app.EntitiesFromDir`.
- `build/world.json` — the canonical world IR snapshot.

You commit these files; the running Kiln process is no longer needed.
Switch your app to load `build/entities/` via `EntitiesFromDir`.

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

## Common mistakes

- **Treating Kiln as a runtime.** It's a build-time tool. Once you
  freeze, the running Kiln binary is not part of your app.
- **Editing `entities/*.json` and then re-running Kiln on top.**
  Kiln expects to own the world while it's running. Hand-edits should
  happen post-freeze, after Kiln has exited.
- **Storing credentials in `cmd/kiln/adapters.go`.** Adapters spawn
  CLIs; credentials live wherever those CLIs already keep them.
