---
name: app-introspect
description: Inspect a running GoFastr app's shape via MCP introspection tools. Use when the user asks "what routes exist", "is the app ready", "what plugins are loaded", "what's the current config", "what batteries depend on what", "what does this framework doc say", or any "describe the running server" question where the app was built with framework.WithMCPIntrospection().
---

# Inspect a live GoFastr app via the framework's MCP introspection

`framework.WithMCPIntrospection()` opts the App into a set of read-only
tools that describe the running server: routes, plugins, batteries,
modules, config, readiness — plus the framework's own embedded docs.
Use these to orient before reading code or before issuing requests.

## Prerequisites

Any GoFastr app running under `gofastr dev` has the full surface
automatically (mount + introspection + control + log debug tools;
opt-out `GOFASTR_DEV_MCP=0`). Outside the dev loop the app must be
built with `framework.WithMCPIntrospection()` and expose `/mcp` (via
`framework.WithMCP()`) — both `examples/site` and blueprint-generated
apps wire both. Launch the site with `./scripts/dev-watch.sh` (port
8082) or `go run ./examples/site` (port 8083 — plain go-run has no
`PORT` set). The curl examples below use 8082; adjust to how the app
was launched.

## The ten tools

| Tool                    | Use                                                                                  |
|-------------------------|--------------------------------------------------------------------------------------|
| `app_routes`            | Every `(method, pattern)` registered via the router, including from Groups + plugin Init. |
| `app_plugins`           | List plugin names in registration order.                                              |
| `app_batteries`         | List batteries with `deps` and `initialized` state, in dependency-resolved order.      |
| `app_modules`           | List modules with manifest metadata (version, deps, migration group), enabled state, and owned surface counts. |
| `app_config`            | AppConfig snapshot: `name`, `json_case`, `debug_endpoints`, `no_llmmd`, `request_timeout_ms`, `disable_request_timeout`. |
| `app_readiness`         | Run all registered readiness checks; same set `/readyz` consults, invokable programmatically. |
| `app_routines`          | Every registered stored routine: name, declared dialect, sha256 checksum of the Up body, ledger state (present/drifted/missing/unknown), and best-effort liveness in `pg_proc`/`pg_views` (Postgres; unknown on SQLite). Use to confirm a routine body change propagated, or spot one the boot skipped. |
| `framework_docs_list`   | Every framework doc topic embedded in the binary (name, title, summary).              |
| `framework_docs_get`    | Full markdown of one topic by name (e.g. `entity-declarations`).                      |
| `framework_docs_search` | Substring search across all topics (min 3 chars, `limit` caps hits).                  |

The `framework_docs_*` tools answer "how does the framework work?"
questions against the exact framework version the binary was built
with — prefer them over fetching docs from the repo when a live app
is available.

## The control tools (mutating)

`framework.WithMCPControl()` — or any app running under `gofastr dev`,
where the whole MCP surface auto-enables — adds runtime state control:

| Tool                 | Use                                                                     |
|----------------------|--------------------------------------------------------------------------|
| `app_module_enable`  | Enable a registered module at runtime (persisted; dependency-checked).  |
| `app_module_disable` | Disable a module (fails closed while enabled modules depend on it).     |

Call `app_modules` first to see names + current state. These mutate the
running app — confirm with the user before toggling anything they
didn't ask about.

## How to invoke

Curl the MCP endpoint with a JSON-RPC `tools/call` payload. Result is
wrapped in MCP content format: `result.content[0].text` is a
JSON-encoded string of the actual return value.

### What endpoints does this app have?

```bash
curl -s http://localhost:8082/mcp \
  -X POST -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/call",
       "params":{"name":"app_routes","arguments":{}}}' \
  | jq -r '.result.content[0].text' | jq '.routes'
```

### What's loaded?

```bash
curl -s http://localhost:8082/mcp \
  -X POST -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/call",
       "params":{"name":"app_plugins","arguments":{}}}' \
  | jq -r '.result.content[0].text' | jq .

curl -s http://localhost:8082/mcp \
  -X POST -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/call",
       "params":{"name":"app_batteries","arguments":{}}}' \
  | jq -r '.result.content[0].text' | jq .

curl -s http://localhost:8082/mcp \
  -X POST -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":3,"method":"tools/call",
       "params":{"name":"app_modules","arguments":{}}}' \
  | jq -r '.result.content[0].text' | jq .
```

### Is the app ready?

```bash
curl -s http://localhost:8082/mcp \
  -X POST -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/call",
       "params":{"name":"app_readiness","arguments":{}}}' \
  | jq -r '.result.content[0].text' | jq .
```

`ready: false` means at least one check failed — the `checks` array
has per-check `status` (`ok`/`error`/`timeout`) and `duration_ms` —
OR that zero checks are registered (then `reason` says
`"no readiness checks registered"` and `checks` is empty: unconfirmed
is not ready). The tool never includes raw error text, even when the
App was configured with `WithVerboseReadiness()`: `/readyz` and `/mcp`
can sit on different trust boundaries, so verbose applies only to
`/readyz`.

### What's the config?

```bash
curl -s http://localhost:8082/mcp \
  -X POST -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/call",
       "params":{"name":"app_config","arguments":{}}}' \
  | jq -r '.result.content[0].text' | jq .
```

`request_timeout_ms == 0` with `disable_request_timeout: false`
means the framework default (30s) is in effect. A non-zero value
is the explicit override.

### What does the framework doc say?

```bash
curl -s http://localhost:8082/mcp \
  -X POST -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/call",
       "params":{"name":"framework_docs_search","arguments":{"term":"owner_field","limit":10}}}' \
  | jq -r '.result.content[0].text' | jq '.hits'
```

Then `framework_docs_get` with the winning topic name for the full
markdown.

## Anti-patterns

- **Don't curl every tool when only one is needed.** Each call is
  a JSON-RPC round-trip — pick the one that answers the question.
- **Don't trust `app_readiness: ready=true` as a SLO signal.**
  Readiness checks only cover what someone registered; "ready" can
  still mean "no real dependencies are wired".
- **Don't read `app_config` for runtime stats.** It's the config
  the App was constructed with, not live behavior. For counters,
  pair with `log_metrics` from the log-debug skill.
