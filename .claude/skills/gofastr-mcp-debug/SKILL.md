---
name: gofastr-mcp-debug
description: Live debug a running GoFastr app via its MCP endpoint — combined entry point covering both the log_* tools (battery/log) and the app_* introspection tools (framework). Use when the user wants to "debug the live app", "see what's going on right now", "tell me about the running server", or any general "go look at the running app" prompt.
---

# Debug a running GoFastr app live (one-page guide)

This skill is the **starting point** for any agent investigation of a
running GoFastr app via MCP. For deep recipes, jump to:

- `.claude/skills/log-debug/SKILL.md` — the four `log_*` tools (recent
  entries, structured filter, metrics, level mutation).
- `.claude/skills/app-introspect/SKILL.md` — the five `app_*` tools
  (routes, plugins, batteries, config, readiness).

## When to use which

| User asks…                                  | Reach for                                                |
|---------------------------------------------|----------------------------------------------------------|
| "What just happened?" / "Why is X failing?" | `log_recent`, `log_filter` (log-debug)                   |
| "Trace request ID abc-123"                  | `log_filter` with `request_id` + `historical=true`       |
| "Is the app ready?"                         | `app_readiness` (app-introspect)                         |
| "What endpoints exist?"                     | `app_routes` (app-introspect)                            |
| "What plugins / batteries are loaded?"      | `app_plugins`, `app_batteries` (app-introspect)          |
| "Are the logs even working?"                | `log_metrics` — non-zero counters = lost entries          |
| "Crank up DEBUG for 30 seconds, then back"  | `log_set_level DEBUG` → reproduce → `log_set_level INFO` |

## Getting started

The user's app must be running with these flags wired:

```go
fwApp := framework.NewApp(
    framework.WithMCPIntrospection(),       // app_* tools
)
fwApp.RegisterPlugin(log.New(log.Config{
    EnableMCP:   true,                       // log_* tools
    MCPRingSize: 2000,
}))
fwApp.Router().Handle("POST", "/mcp", fwApp.MCP)
```

`examples/website` already has this wired. Spin it up with the
repo's normal dev workflow:

```bash
./scripts/dev-watch.sh         # examples/website on :8082, auto-rebuild
# or
go run ./examples/website
```

Then point curl at `http://localhost:8082/mcp` and use the log-debug
+ app-introspect skills' recipes.

## The JSON-RPC envelope

All tool calls are `POST /mcp` with body:

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "tools/call",
  "params": {
    "name": "<tool_name>",
    "arguments": { ... }
  }
}
```

Response wraps the tool's return value as a JSON-encoded string in
`.result.content[0].text` — pipe through `jq -r '.result.content[0].text' | jq .`
to get back to structured JSON. List available tools via
`"method": "tools/list"` (no params).

## Anti-patterns

- **Don't reach for MCP when grepping the file directly is easier.** If
  the user knows exactly what they're looking for and has shell access,
  `tail -f ~/.local/state/<app>/server.log | grep …` may be the right
  move. MCP wins when the agent (not the user) is the consumer.
- **Don't curl tools you don't know the shape of.** `tools/list` first
  if you're not sure what's registered; the descriptions are written
  for agents.
- **Don't forget to revert state mutations.** `log_set_level` is the
  only mutating tool right now, but treat it like a temporary debug
  toggle: flip on, reproduce, flip off.
