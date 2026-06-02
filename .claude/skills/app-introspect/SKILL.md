---
name: app-introspect
description: Inspect a running GoFastr app's shape via MCP introspection tools. Use when the user asks "what routes exist", "is the app ready", "what plugins are loaded", "what's the current config", "what batteries depend on what", or any "describe the running server" question where the app was built with framework.WithMCPIntrospection().
---

# Inspect a live GoFastr app via the framework's MCP introspection

`framework.WithMCPIntrospection()` opts the App into a set of read-only
tools that describe the running server: routes, plugins, batteries,
config, readiness. Use these to orient before reading code or before
issuing requests.

## Prerequisites

The user's app must be built with `framework.WithMCPIntrospection()`
and expose `POST /mcp`. The `examples/site` example does both —
launch it with `./scripts/dev-watch.sh` (port 8082) or
`go run ./examples/site`.

## The five tools

| Tool             | Use                                                                                  |
|------------------|--------------------------------------------------------------------------------------|
| `app_routes`     | Every `(method, pattern)` registered via the router, including from Groups + plugin Init. |
| `app_plugins`    | List plugin names in registration order.                                              |
| `app_batteries`  | List batteries with `deps` and `initialized` state, in dependency-resolved order.      |
| `app_config`     | AppConfig snapshot: `name`, `json_case`, `debug_endpoints`, `no_llmmd`, `request_timeout_ms`, `disable_request_timeout`. |
| `app_readiness`  | Run all registered readiness checks; same as `/readyz` but invokable programmatically. |

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
```

### Is the app ready?

```bash
curl -s http://localhost:8082/mcp \
  -X POST -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/call",
       "params":{"name":"app_readiness","arguments":{}}}' \
  | jq -r '.result.content[0].text' | jq .
```

`ready: false` means at least one check returned non-nil; the
`checks` array has per-check `status` (`ok`/`error`/`timeout`),
`duration_ms`, and optionally `error` (only when the App was
configured with `WithVerboseReadiness()`).

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

## Anti-patterns

- **Don't curl every tool when only one is needed.** Each call is
  a JSON-RPC round-trip — pick the one that answers the question.
- **Don't trust `app_readiness: ready=true` as a SLO signal.**
  Readiness checks only cover what someone registered; "ready" can
  still mean "no real dependencies are wired".
- **Don't read `app_config` for runtime stats.** It's the config
  the App was constructed with, not live behavior. For counters,
  pair with `log_metrics` from the log-debug skill.
