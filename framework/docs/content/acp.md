# ACP: Agent Client Protocol server (`core/acp`)

`core/acp` implements the agent side of the Agent Client Protocol v1 — newline-delimited JSON-RPC 2.0 over stdio — so editors and agent harnesses can drive a Go agent session the way LSP drives language servers. Kiln uses it via `kiln acp` (`kiln/acp`).

## Protocol surface

Client → server methods:
- `initialize` — negotiates protocol version, `loadSession` (when `SessionLoader` is implemented), explicit `promptCapabilities` (text + `resource_link`), `mcpCapabilities` (`http: false, sse: false`), and optional `authMethods`.
- `authenticate` — runs `Options.Authenticate` (when `AuthMethods` is non-empty, session methods return `-32000` until authentication succeeds).
- `session/new` — creates a session via `Agent.NewSession(ctx, cwd)` (bounded by `Options.MaxSessions`, default `1024`, with `SessionOverflowRefuse` or `SessionOverflowEvictOldest`).
- `session/load` — replays a stored session as `user_message_chunk` / `agent_message_chunk` updates (refused mid-turn with `-32600`).
- `session/prompt` — executes one turn via `Session.Prompt` and returns a stop reason (`end_turn`, `refusal`, `cancelled`, …).
- `session/cancel` — notification cancelling the active prompt's `ctx`.

Server → client updates during a turn:
- `session/update` notifications: `agent_message_chunk`, `user_message_chunk`, `plan`, `tool_call`, `tool_call_update`.
- `session/request_permission` request: blocks until the user approves or rejects an action; malformed answers always resolve as `OutcomeCancelled`.

## Embedding `core/acp`

Implement `Agent` and `Session`, then serve stdio:

```go
type echoAgent struct {
	seq atomic.Uint64
}

func (a *echoAgent) Info() acp.Implementation {
	return acp.Implementation{Name: "my-agent", Version: "1.0.0"}
}

func (a *echoAgent) NewSession(ctx context.Context, cwd string) (acp.Session, error) {
	return &echoSession{id: fmt.Sprintf("sess_%d", a.seq.Add(1))}, nil
}

type echoSession struct{ id string }

func (s *echoSession) ID() string { return s.id }

func (s *echoSession) Prompt(ctx context.Context, prompt []acp.ContentBlock, out *acp.Client) (string, error) {
	_ = out.Update(acp.AgentMessageChunk("m1", "you said: "+acp.PromptText(prompt)))
	return acp.StopEndTurn, nil
}

func main() {
	srv := acp.NewServer(&echoAgent{}, nil)
	if err := srv.Serve(context.Background(), os.Stdin, os.Stdout); err != nil {
		log.Fatal(err)
	}
}
```

Key runtime guarantees:
- **Panic & error safety:** panics in `Authenticate`, `NewSession`, `LoadSession`, or `Prompt` recover into a generic internal-error JSON-RPC response and log server-side without crashing the process or leaking panic values over the wire.
- **Strict capability boundaries:** unsupported prompt blocks (images/audio), client `mcpServers`, `additionalDirectories`, and unadvertised methods fail cleanly with standard JSON-RPC errors rather than being silently ignored.
- **Kiln adapter (`kiln/acp`):** journals prompts into the session, streams tool calls as `tool_call`/`tool_call_update`, gates destructive tools and `approve_plan` behind `session/request_permission`, and accepts a model via `kiln/acp.WithProvider`.

## Common mistakes

- **Sending `session/*` before `initialize`:** rejected with `-32600`; clients must negotiate first.
- **Sending loose JSON-RPC envelopes:** frames missing `"jsonrpc": "2.0"` or carrying invalid IDs are rejected with `-32600`.
- **Ignoring `ctx` inside `Session.Prompt`:** `session/prompt` cannot reply with `stopReason: "cancelled"` until `Prompt` returns.
- **Treating cancellation as a JSON-RPC error:** cancellation resolves normally with `stopReason: "cancelled"`.
- **Expecting the server to invoke client `fs/*` or `terminal/*` methods:** `core/acp` never calls them and advertises none.
