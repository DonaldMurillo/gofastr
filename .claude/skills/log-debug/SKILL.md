---
name: log-debug
description: Investigate a running GoFastr app by querying its log MCP tools. Use when the user asks "why is /path slow", "what's the last error", "check the access logs", "is logging healthy", "what just happened on the server", or any debug-the-running-server question where the app exposes /mcp.
---

# Debug a live GoFastr app via the log MCP tools

The `battery/log` plugin (with `Config.EnableMCP: true`) registers
four JSON-RPC tools on the App's MCP server. Use these to investigate
a running app — agent live-debugging, not log greps from the user.

## Prerequisites

The user's app must be:
1. Running.
2. Built with `log.Config{EnableMCP: true}` (or wired to mount the
   plugin's MCP tools manually).
3. Exposing `POST /mcp` (the canonical mount; the website example
   does this — `examples/site/main.go`).

Quickest way to spin up a known-good target in this repo:

```bash
./scripts/dev-watch.sh   # examples/site on :8082, auto-rebuilds
# or
go run ./examples/site
```

Then curl `http://localhost:8082/mcp` (or whatever port your app uses).

## The four tools

| Tool            | Use                                                                                  |
|-----------------|--------------------------------------------------------------------------------------|
| `log_recent`    | Last N entries in chrono order. Optional `limit` (default 50) + `level` filter.       |
| `log_filter`    | Match by `msg`/`path`/`request_id`/`since_ts`/`until_ts`/`level`. `historical=true` tails the file sink for entries evicted from the ring. |
| `log_metrics`   | Counter snapshot — `post_stop_drops`, `sink_write_failures`, `webhook_dropped`, `webhook_gave_up`. |
| `log_set_level` | Flip the runtime log level (DEBUG/INFO/WARN/ERROR). Returns the previous value. **Only registered when `log.Config.AllowMCPMutation` is `true`.** |

## How to invoke

Use the Bash tool to curl the MCP endpoint with a JSON-RPC payload.
The MCP URL is `http://localhost:<PORT>/mcp` (ask the user if you
don't know the port — common defaults are 8082 / 8088).

### Last 5 access entries

```bash
curl -s http://localhost:8082/mcp \
  -X POST -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/call",
       "params":{"name":"log_recent","arguments":{"limit":5,"level":"INFO"}}}' \
  | jq -r '.result.content[0].text' | jq .
```

The double `jq` is intentional: MCP wraps the tool result in
`content[0].text` as a JSON-encoded string, so we parse that text
back into structured JSON for readability.

### All errors in the last 5 minutes

Use `since_ts` with an RFC3339 timestamp:

```bash
# GNU date (Linux containers): use this form first.
SINCE=$(date -u -d '5 minutes ago' +%Y-%m-%dT%H:%M:%SZ 2>/dev/null \
       || date -u -v-5M +%Y-%m-%dT%H:%M:%SZ)    # macOS fallback
curl -s http://localhost:8082/mcp \
  -X POST -H 'Content-Type: application/json' \
  -d "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"tools/call\",
       \"params\":{\"name\":\"log_filter\",
                   \"arguments\":{\"level\":\"ERROR\",\"since_ts\":\"$SINCE\",\"limit\":50}}}" \
  | jq -r '.result.content[0].text' | jq .
```

### Trace a specific request by ID

The framework's `middleware.RequestID` stamps every request with
`X-Request-ID`. Pull it from the user's curl response (or browser
network panel), then:

```bash
curl -s http://localhost:8082/mcp \
  -X POST -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/call",
       "params":{"name":"log_filter",
                 "arguments":{"request_id":"<paste-id-here>","historical":true}}}' \
  | jq -r '.result.content[0].text' | jq .
```

`historical=true` extends the search into the file sink so you can
trace IDs older than the ring buffer's window (default 1000 entries).

### Temporarily switch to DEBUG

If the agent needs verbose output for an investigation:

```bash
# Flip to DEBUG
curl -s http://localhost:8082/mcp \
  -X POST -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/call",
       "params":{"name":"log_set_level","arguments":{"level":"DEBUG"}}}' \
  | jq -r '.result.content[0].text' | jq .

# ...reproduce the bug, then call log_recent / log_filter...

# Restore INFO (always restore — leaving DEBUG on in prod is rude)
curl -s http://localhost:8082/mcp \
  -X POST -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/call",
       "params":{"name":"log_set_level","arguments":{"level":"INFO"}}}' \
  | jq -r '.result.content[0].text' | jq .
```

### Health of the logging system

Before trusting the log queries, check the counters are zero:

```bash
curl -s http://localhost:8082/mcp \
  -X POST -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/call",
       "params":{"name":"log_metrics","arguments":{}}}' \
  | jq -r '.result.content[0].text' | jq .
```

Non-zero `sink_write_failures` or `post_stop_drops` means the
plugin lost entries — anything queried might be incomplete.

## Anti-patterns

- **Don't reach for the file directly.** The ring buffer is faster
  and structured; only fall through to file (via `historical=true`)
  when the ring window's been exhausted.
- **Don't leave DEBUG on after an investigation.** Other observers
  see the firehose. Call `log_set_level INFO` when done.
- **Don't treat `remote` as authoritative.** Unless the app set
  `Config.TrustForwardedFor`, `remote` is just `r.RemoteAddr`;
  `forwarded_for` is the raw client header — attacker-controlled.
- **Don't filter for what isn't structured.** The plugin's
  `http.access` entries carry `method`/`path`/`status`/`bytes`/
  `dur_ms`/`request_id`/`remote`/`forwarded_for`. Anything else
  is a substring search via `msg` or post-filtering in your head.
