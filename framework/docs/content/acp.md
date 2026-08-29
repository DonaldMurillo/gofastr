# ACP: Agent Client Protocol server

`core/acp` implements the agent side of the Agent Client Protocol v1
(agentclientprotocol.com): the session-based, stdio JSON-RPC 2.0 protocol
editors and agent harnesses use to drive a coding agent. ACP is to agents
what LSP is to language servers; frames are newline-delimited JSON-RPC 2.0,
one object per line.

Kiln speaks it through `kiln acp` (the adapter lives in `kiln/acp`). The
package itself is domain-free: you supply the conversation through two
interfaces and it owns everything on the wire.

## What the package speaks

Methods the server accepts from the client:

| Method | Behavior |
|---|---|
| `initialize` | Version + capability negotiation. Responds with the agent's info, `loadSession` (true only if the agent implements `SessionLoader`), explicit `promptCapabilities` and `mcpCapabilities` (all false), and the configured `authMethods`. |
| `authenticate` | Runs the configured `Options.Authenticate` hook for an advertised method ID. With no methods advertised (the default), every `methodId` is rejected with `-32602`. |
| `session/new` | Creates a session via `Agent.NewSession(cwd)`. Returns `{sessionId}`. |
| `session/load` | Only when `loadSession` was advertised. Replays the conversation as `user_message_chunk` / `agent_message_chunk` updates, then returns. Unknown IDs answer `-32002`. |
| `session/prompt` | Runs one turn via `Session.Prompt`. The response carries the stop reason (`end_turn`, `refusal`, `cancelled`, …). |
| `session/cancel` | Notification. Cancels the session's in-flight prompt context; the server then answers the original `session/prompt` with stop reason `cancelled`. |

What the server sends to the client during a turn:

| Frame | Direction | Purpose |
|---|---|---|
| `session/update` (`agent_message_chunk`) | notify | Streams assistant text. Chunks sharing a `messageId` form one message. |
| `session/update` (`user_message_chunk`) | notify | Replays user history during `session/load`. |
| `session/update` (`plan`) | notify | Replaces the session's execution plan (complete entry list every time). |
| `session/update` (`tool_call`) | notify | Reports a tool invocation starting (`pending`). |
| `session/update` (`tool_call_update`) | notify | Patches a tool call: `in_progress`, then `completed`/`failed` with content. |
| `session/request_permission` | request | Asks the user to approve an operation (allow once / reject once …). |

## What is deliberately not implemented

Every absence below is declared at `initialize` or enforced with an error,
never silently dropped:

- **Prompt content beyond text and `resource_link`.** `initialize` returns
  `promptCapabilities: {image: false, audio: false, embeddedContext: false}`
  with the false values explicit. A client that sends an image block anyway
  gets `-32602` naming the block type.
- **Client MCP servers.** `mcpCapabilities` is `{http: false, sse: false}`
  explicit, and `session/new` / `session/load` reject a non-empty
  `mcpServers` list with `-32602` instead of accepting servers it would
  never connect to.
- **Client filesystem (`fs/read_text_file`, `fs/write_text_file`) and
  terminals (`terminal/*`).** These are client-side methods an agent may
  call; this server never calls them, so a client advertising them changes
  nothing. There is no agent-side capability for them to appear in — the
  agent's `initialize` response claims none.
- **`additionalDirectories`** on session setup: rejected, capability not
  advertised.
- **`logout`, session modes, config options, slash commands, elicitation,
  `session/resume` / `session/close` / `session/list` / `session/delete`,
  `$/cancel_request`.** Not advertised, not handled. Unknown requests get
  `-32601`; unknown notifications are ignored per JSON-RPC.

## Embedding

Implement `Agent` (and optionally `SessionLoader`), then serve stdio:

```go
type echoAgent struct{}

func (echoAgent) Info() acp.Implementation {
	return acp.Implementation{Name: "my-agent", Version: "1.0.0"}
}

func (echoAgent) NewSession(ctx context.Context, cwd string) (acp.Session, error) {
	return &echoSession{}, nil
}

type echoSession struct{}

func (s *echoSession) ID() string { return "sess_1" }

func (s *echoSession) Prompt(ctx context.Context, prompt []acp.ContentBlock, out *acp.Client) (string, error) {
	_ = out.Update(acp.AgentMessageChunk("m1", "you said: "+acp.PromptText(prompt)))
	return acp.StopEndTurn, nil
}

func main() {
	srv := acp.NewServer(echoAgent{}, nil)
	if err := srv.Serve(context.Background(), os.Stdin, os.Stdout); err != nil {
		log.Fatal(err)
	}
}
```

`Session.Prompt` is the whole contract:

- Stream progress with `out.Update(...)`; the constructors
  (`AgentMessageChunk`, `NewToolCall`, `ToolCallUpdateFrame`, `PlanUpdate`)
  produce the spec-shaped frames.
- Gate an operation on the human with `out.RequestPermission(ctx, toolCall,
  options)`; it blocks until the client answers and returns the selected
  option.
- Honor `ctx`: it is cancelled when the client sends `session/cancel`, and
  the server can only answer the client once `Prompt` returns.

`Options` has two fields: `AuthMethods` (advertised in `initialize`) and
`Authenticate` (the hook run for `authenticate`). The zero value advertises
no auth, which is right for local, unauthenticated agents like Kiln.

## Kiln's adapter (`kiln/acp`)

`kiln acp` runs `kiln/acp.NewServer(tools)` over stdio. The adapter:

- journals every prompt as chat so the HTTP panel stays in sync;
- runs turns through an in-process `kiln/agent.Provider` when one is
  attached (`kiln/acp.WithProvider`), streaming text and tool calls as
  `session/update` frames — tool invocations surface as `tool_call` /
  `tool_call_update`, replacing the pre-v0.x bespoke `tools/call` method;
- gates `approve_plan` on `session/request_permission`: the model may
  propose plans freely, but only the user at the client can approve one,
  and a rejection is fed back to the model as a `needs_plan` result;
- without a provider, refuses prompts loudly (one message chunk plus
  stop reason `refusal`) — the `kiln` CLI attaches no model today; drive
  Kiln's tools through `kiln mcp` or the HTTP panel, or embed
  `kiln/acp` with a provider.

Kiln sessions are process-lifetime: `session/load` replays a session minted
by the current process (from the journal's chat) and reports any other ID
as resource-not-found.

## Common mistakes

- **Sending prompts before `initialize` or `session/new`.** Both are
  enforced: `session/*` before `initialize` answers `-32600`, and an unknown
  session ID answers `-32002`. ACP clients negotiate first; a test client
  must too.
- **Expecting the agent to call client fs/terminal methods.** It never
  does. Capabilities like `fs.readTextFile` in the client's `initialize`
  are offers to the agent, not features of it; this agent declares and uses
  none of them.
- **Blocking `Prompt` after `session/cancel`.** The server answers the
  client with stop reason `cancelled` only after `Prompt` returns; an
  implementation that ignores its context leaves the client hanging.
- **Reading `stopReason` from the error path.** Cancellation is not an
  error: `session/prompt` resolves normally with `stopReason: "cancelled"`
  even when the underlying provider call failed while being cancelled.
