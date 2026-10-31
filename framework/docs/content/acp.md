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
| `authenticate` | Runs the configured `Options.Authenticate` hook for an advertised method ID. Requires `initialize` first, exactly like the session methods. With no methods advertised (the default), every `methodId` is rejected with `-32602`. |
| `session/new` | Creates a session via `Agent.NewSession(cwd)`. Returns `{sessionId}`. An agent that returns an ID this connection already holds, live or draining, gets `-32603` and the held session is kept. |
| `session/load` | Only when `loadSession` was advertised. Replays the conversation as `user_message_chunk` / `agent_message_chunk` updates, then returns. Unknown IDs answer `-32002`; a session whose prompt turn is still running answers `-32600` (reloading it mid-turn would let a second prompt run beside the first and lose the turn's cancel handle). A loader that returns a session with a different ID answers `-32603`. |
| `session/prompt` | Runs one turn via `Session.Prompt`. The response carries the stop reason (`end_turn`, `refusal`, `cancelled`, …). The next prompt on a session waits until the previous turn's response is written, so a client that stops reading holds at most one parked response per session. |
| `session/cancel` | Notification. Cancels the session's in-flight prompt context; the server then answers the original `session/prompt` with stop reason `cancelled`. |

What the server sends to the client during a turn:

| Frame | Direction | Purpose |
|---|---|---|
| `session/update` (`agent_message_chunk`) | notify | Streams assistant text. Chunks sharing a `messageId` form one message. |
| `session/update` (`user_message_chunk`) | notify | Replays user history during `session/load`. |
| `session/update` (`plan`) | notify | Replaces the session's execution plan (complete entry list every time). |
| `session/update` (`tool_call`) | notify | Reports a tool invocation starting (`pending`). |
| `session/update` (`tool_call_update`) | notify | Patches a tool call: `in_progress`, then `completed`/`failed` with content. |
| `session/request_permission` | request | Asks the user to approve an operation (allow once / reject once …). An answer that carries neither a valid selection nor a cancellation is treated as cancelled — a malformed approval never reads as approval. An answer without `"jsonrpc": "2.0"` is not a JSON-RPC response; it settles the request as an error. |

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
  produce the spec-shaped frames. A `ToolCallUpdate` field left nil is
  omitted (unchanged); a title, content, `rawInput` or `rawOutput` set to
  an empty value is sent empty, which clears it on the client.
- Gate an operation on the human with `out.RequestPermission(ctx, toolCall,
  options)`; it blocks until the client answers and returns the selected
  option. A client answer that is neither a valid selection nor a
  cancellation comes back as `OutcomeCancelled`, so a malformed or
  hostile answer never reads as approval. It works from every handler,
  `LoadSession` included: handlers run beside the reader that delivers
  the answer. Other frames that arrive while a handler waits are held,
  up to 256 frames or 16 MiB; past that a request is refused with an
  internal error. A `session/cancel` still reaches a running turn at
  once. Input EOF cancels the waiting handler's `ctx` and drops the
  frames held behind it. The call returns when `ctx` ends or the
  connection closes even while its request is still being written to a
  client that stopped reading output; that write finishes, or breaks the
  stream, on its own, so the next frame never lands inside it.
- Honor `ctx`: it is cancelled when the client sends `session/cancel` and
  when `Serve` returns (input EOF or the caller's ctx ending), and the
  server can only answer the client once `Prompt` returns.

`Serve` returns as soon as its ctx ends, even with no input arriving. The
read in flight runs on in a goroutine until `in` yields a line, EOF or an
error; close `in` to end it sooner. `PlanUpdate(nil)` sends `"entries": []`,
which tells the client the plan is now empty.

`Options` has four fields: `AuthMethods` (advertised in `initialize`) and
`Authenticate` (the hook run for `authenticate`; the connection must have
sent `initialize` first) — the zero value advertises no auth, which is right
for local, unauthenticated agents like Kiln — plus the session bounds
`MaxSessions` (live sessions one connection may hold; 0 = 1024, negative =
unlimited) and `SessionOverflow`
(`acp.SessionOverflowRefuse`, the default: `session/new` past the cap is
answered `-32602` before the embedder mints anything, mirroring the 4 MiB
frame cap; `acp.SessionOverflowEvictOldest`: the connection's oldest
session is dropped, its in-flight prompt turn canceled (a `session/load` of its id is refused until that turn returns), and the new one
seated). Without the bound, `session/new` is a frame-cheap unbounded write
into per-connection state that lives until the connection closes. A panic
in any embedder hook — `Authenticate`, `NewSession`, `LoadSession`,
`Prompt` — is recovered into an internal-error response for that one
request; it never kills the process. An error `Prompt` returns is
answered `session/prompt: internal error`; its text stays on the server.

## Kiln's adapter (`kiln/acp`)

`kiln acp` runs `kiln/acp.NewServer(tools)` over stdio. The adapter:

- journals every prompt as chat so the HTTP panel stays in sync;
- runs turns through an in-process `kiln/agent.Provider` when one is
  attached (`kiln/acp.WithProvider`), streaming text and tool calls as
  `session/update` frames — tool invocations surface as `tool_call` /
  `tool_call_update`, replacing the pre-v0.x bespoke `tools/call` method;
- keeps each session's conversation with the provider across prompts: a
  prompt's request carries the session's earlier prompts, replies, tool
  calls and results. A tool call that a cancelled prompt never ran is
  answered with a failed result of kind `cancelled`, so no call is left
  without a result. A session runs one prompt at a time, also when a
  second connection loads it; a prompt cancelled while it waits answers
  `cancelled` and is not journaled;
- gates `approve_plan` on `session/request_permission`: the model may
  propose plans freely, but only the user at the client can approve one,
  and a rejection is fed back to the model as a `needs_plan` result;
- without a provider, refuses prompts loudly (one message chunk plus
  stop reason `refusal`) — the `kiln` CLI attaches no model today; drive
  Kiln's tools through `kiln mcp` or the HTTP panel, or embed
  `kiln/acp` with a provider. `kiln acp --agent X` still runs the panel's
  agent on panel messages, never on an ACP session's: the refused prompt
  stays refused.

Kiln sessions are process-lifetime: `session/load` replays a session minted
by the current process (from the journal's chat) and reports any other ID
as resource-not-found.

## Common mistakes

- **Loose JSON-RPC envelopes.** A frame naming a method must carry
  `"jsonrpc": "2.0"` and an id that is a string, a number, or null (or no
  id, for a notification). Anything else is answered `-32600` with a null
  id before dispatch, per JSON-RPC 2.0 §4 and §5.
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
