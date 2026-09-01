# A2A task exchange

`framework.WithA2A` mounts an [Agent2Agent](https://a2a-protocol.org) v1.0
task exchange at `/a2a`: a JSON-RPC endpoint where a remote agent sends a
message, the server routes it to a named skill, and the work comes back as
a task with artifacts. The protocol implementation is `core/a2a`; this page
is the framework wiring.

<!-- gofastr:compile
import (
	coreapp "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/a2a"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)
var site = coreapp.NewApp("Acme")
var summarizeInvoices a2a.Handler
var keys []uihost.AgentCardSigningKey
stmt: _ = host
-->


```go
app := framework.NewApp(
	framework.WithMCP(),
	framework.WithA2A(framework.A2AConfig{
		Skills: []a2a.Skill{{
			ID:          "summarize-invoices",
			Name:        "Summarize invoices",
			Description: "Summarize the caller's invoices for a period.",
			Handler:     summarizeInvoices,
		}},
	}),
)
host := uihost.New(site, uihost.WithAgentReady(uihost.AgentReadyConfig{
	AgentCard: &uihost.AgentCardConfig{
		A2AEndpoint: "/a2a",
		Skills:      app.A2ASkills(),
		SigningKeys: keys,
	},
}))
```

(`site` is the core-ui app your screens are registered on; see
[agent-readiness](agent-ready.md) for the full bundle.)

## A GoFastr agent is deterministic

The one thing an integrator must know: **a skill is invoked by name,
never inferred from prose.** The router reads `message.metadata.skill`,
then the first data part carrying a `"skill"` key, then (only when
exactly one skill is registered) that skill — and anything else is
rejected with a message naming the available skill ids. There is no
model call, no intent classification, no fallback guess on the server
side. A client that sends "please list my invoices" as text gets a
rejected task; a client that sends
`{"skill": "entity.invoices", "operation": "list"}` gets invoices.

That determinism is the point: the calling agent (the LLM, on the other
side of the wire) decides *what* to ask for; this server decides exactly
*how*, and the same request always produces the same work.

## The wire surface

One endpoint, `A2AConfig.Path` (default `/a2a`), POST only, content type
`application/json` (or `application/a2a+json`). JSON-RPC 2.0 methods,
PascalCase per the v1.0 binding:

| Method | Serves |
|---|---|
| `SendMessage` | Run a message; the response is the final task (or the working snapshot with `returnImmediately`). |
| `SendStreamingMessage` | Same, answered as `text/event-stream`. |
| `GetTask` / `ListTasks` | Read a task / page the caller's tasks. |
| `CancelTask` | Cancel a running task. |
| `SubscribeToTask` | SSE stream of a task's updates. |
| `CreateTaskPushNotificationConfig` / `Get…` / `List…` / `Delete…` | Webhook configuration per task. |
| `GetExtendedAgentCard` | The extended card, when `A2AConfig.ExtendedCard` is set. |

Wire facts, pinned against the canonical `a2a.proto`: field names are
camelCase; states serialize as `TASK_STATE_COMPLETED`-style proto enum
names; timestamps are RFC 3339 UTC; a `Part` is a flat object whose set
field (`text`, `raw`, `url`, `data`) is the discriminator. The v0.x
slash-form methods (`message/send`, `tasks/get`, …) are
wire-incompatible, not merely outdated — a v0.x client gets
`-32601` method not found and that is correct.

## Entity skills

Every registered entity with MCP tools (`mcp: true` in its declaration,
see [entity declarations](entity-declarations.md)) gets one derived
skill, `entity.<name>` (`entity.<ns>.<name>` under a routegroup MCP
namespace). Turn this off with `A2AConfig.DisableEntitySkills` — the
zero value is ON.

The skill description is generated from the MCP tool registry, the same
schema walk OpenAPI, the CLI, the SDK, and `tools/list` share, so it
cannot drift from the tools it invokes. It lists one line per operation
with the tool's input-schema keys:

```
CRUD on invoices records through the app's MCP tools. Send one data part carrying skill, operation, and arguments:
list(page, limit, sort, due_at, total, ...)
get(id)
create(due_at, total, ...)
update(id, due_at, total, ...)
delete(id)
```

### The data-part contract

One data part carries `skill`, `operation` (one of the five actions),
and `arguments` (the tool's params object, verbatim — the same keys
`tools/list` advertises). A full round trip:

```json
POST /a2a HTTP/1.1
Content-Type: application/json
Authorization: Bearer gfsk_…

{"jsonrpc":"2.0","id":1,"method":"SendMessage","params":{
  "message":{
    "messageId":"m-1",
    "role":"ROLE_USER",
    "parts":[{"data":{
      "skill":"entity.invoices",
      "operation":"list",
      "arguments":{"limit":10}
    }}]
  }
}}
```

```json
{"jsonrpc":"2.0","id":1,"result":{"task":{
  "id":"8d9e4a02-51cd-435f-a3f0-ecaba310d3c1",
  "contextId":"83d49668-4847-487c-8426-79e1b36d15ce",
  "status":{"state":"TASK_STATE_COMPLETED","timestamp":"2026-09-01T22:43:45.791Z"},
  "artifacts":[{"artifactId":"8c00bbd9-…","name":"invoices.list","parts":[
    {"data":{"data":[{"id":"n1","title":"…","userId":"u-alice"}],"page":1,"perPage":20,"total":2,"totalPages":1}}
  ]}],
  "history":[…],
  "metadata":{"gofastr.skill":"entity.invoices"}
}}}
```

The tool result becomes one artifact named `<entity>.<operation>` with a
data part. Failure mapping:

- **Unknown or missing operation** → `TASK_STATE_REJECTED`, with the
  agent message naming the operations: `unknown operation "purge";
  operations: list, get, create, update, delete`. A rejection is the
  agent's decision not to do the work, not a transport error.
- **Tool error** (bad arguments, a refused write, a gated tool) →
  `TASK_STATE_FAILED`, the agent message carrying the refusal text
  (e.g. `entity mcp request failed: status 401: …`).
- **A skill the router cannot name** → `TASK_STATE_REJECTED`, naming the
  registered skill ids.

## Auth and owner scoping

The exchange is mounted through the app router, so the same
session/bearer middleware that guards the REST API guards `/a2a`. A
caller that resolves to no owner gets HTTP 401 and JSON-RPC `-31401`
before any method runs — there is no anonymous posture, because tasks
are per-user data (the same rule that requires `Scope.OwnerField` on
entities, see [entity declarations](entity-declarations.md) → Per-user
scoping).

Every task is owned by the principal that created it. Reads, resumes,
cancels, and push configs are scoped to that owner in the store; another
owner's task id is indistinguishable from an unknown one
(`-32001` task not found), so ids do not enumerate across users.

Entity-skill calls re-enter the app with the caller's credentials: the
handler passes the original request into `a.MCP.CallTool`, the entity
tool re-dispatches through the app router copying the caller's
`Cookie` / `Authorization` / `X-API-Key` headers, and owner scoping
applies exactly as it does over `/mcp` (see
[agent-readiness](agent-ready.md) → MCP auto-mount). Alice's
`entity.invoices` list returns exactly Alice's rows.

Tool gating carries over too: `CallTool` runs the server's call gate, so
a tool the framework hides from agents (one owned by a disabled module)
is refused through A2A with the same generic `tool unavailable` error it
returns over `/mcp`.

## Task lifecycle

`SendMessage` runs the skill handler to completion and answers with the
final task, unless `configuration.returnImmediately` is set, in which
case the `SUBMITTED`/`WORKING` snapshot comes back at once and the work
continues in the background.

A handler pauses its task with `RequireInput` / `RequireAuth`
(`TASK_STATE_INPUT_REQUIRED` / `TASK_STATE_AUTH_REQUIRED`). The next
`SendMessage` addressed to the task (its `messageId` carrying
`taskId`) resumes it: the handler runs again with the new message and
the task's history. `CancelTask` cancels a running task — the handler's
context is cut, and the state is settled against the store so a late
completion cannot resurrect a task the client was told was canceled.

One handler run is bounded by `A2AConfig.TaskTimeout` (default 5
minutes); a run that exceeds it fails with `task timed out`.

### Streaming

`SendStreamingMessage` and `SubscribeToTask` answer
`text/event-stream`. Each event's data is a complete JSON-RPC response
object whose result is a `StreamResponse` — exactly one of `task`,
`message`, `statusUpdate`, `artifactUpdate`. The stream sends the
current task snapshot first, then every event until the task is terminal
or interrupted, then closes. A comment line (`: keep-alive`) paces idle
streams every 15 seconds so proxies do not time the connection out.

With the SQL store, `SubscribeToTask` on a task running on another
replica falls back to polling the store and emitting a snapshot whenever
the stored version changes — per-event fidelity across a replica
boundary would need a shared bus, which a SQL store cannot give. Events
between polls coalesce into the next snapshot.

## Push notifications

A client registers a webhook per task (in `SendMessage`'s
`configuration.taskPushNotificationConfig`, or via
`CreateTaskPushNotificationConfig` after the task exists). The server
POSTs each `StreamResponse` to the configured URL with the
`A2A-Notification-Token` header carrying the config's token, so the
receiver can check the notification came from the agent it registered
with.

Delivery is best effort by design: one attempt, no retry, a non-2xx or
transport error is logged once. Reliable delivery with backoff is
[queue](queue.md)'s job; task progress must never stall on a receiver
that is down. The SSRF posture: push URLs targeting internal hosts
(loopback, RFC1918, CGNAT, link-local) are refused at registration
unless `A2AConfig.AllowPrivatePush` is set, redirects are not followed,
and the client is dial-time guarded.

## Tasks are not jobs

A2A task state and [`battery/queue`](queue.md) answer different
questions. A task is a conversation unit: who asked, what the agent is
doing, what came back — owned per user, listable, resumable, bounded by
`TaskTimeout`, and worthless as durable infrastructure (the memory
store forgets on restart; the SQL store keeps rows but nothing retries).
A queue job is background work: at-least-once delivery, retries,
dead-letter, workers. Long work behind a skill should `t.Working`,
enqueue a job, and complete (or push-notify) when the job lands. Do not
store business state in tasks, and do not run minutes-long handlers
in-process expecting the task row to act as a job table.

## What the framework already provides

- `/a2a` mounted behind the app's middleware chain — session/bearer
  auth, owner context, recovery, request logging — with no wiring.
- One skill per entity with MCP tools, descriptions generated from the
  live tool registry, plus your hand-written skills.
- `app.A2ASkills()` feeding the agent card, and
  `AgentCardConfig.A2AEndpoint` advertising the exchange as the card's
  JSON-RPC interface with streaming + push capabilities.
- Card signing + JWKS + RFC 9728 metadata (see
  [agent-readiness](agent-ready.md)).
- The SQL task store on the app's DB (tables created if absent), shared
  across replicas; a memory store when there is no DB.
- `GOFASTR_ROLE=agent` serving the exchange on a narrow listener (see
  [scaling](scaling.md)).

## Configuration reference

| `A2AConfig` field | Purpose |
|---|---|
| `Path` | Endpoint path; default `/a2a`. Must start with `/`; a conflicting mount panics at Start. |
| `Skills` | Hand-written skills. At least one skill overall (hand-written or derived) or Start fails. |
| `DisableEntitySkills` | Turn OFF the derived entity skills. Zero value = ON. |
| `Store` | `a2a.Store`. Nil → SQL store on the app's DB, or a memory store with no DB. |
| `ExtendedCard` | Serves `GetExtendedAgentCard` when set. |
| `AllowPrivatePush` | Permit push URLs targeting internal hosts (dev/tests). |
| `TaskTimeout` | Ceiling on one handler run; default 5 minutes. |

`app.A2A()` returns the mounted `*a2a.Server` (nil before Start or when
not configured) for direct registration reads — but note skills the
server runs are fixed at Start, from the same list
`app.A2ASkills()` reports.

## Common mistakes

- **Sending prose and expecting the agent to figure it out.** Skills are
  invoked by name (`metadata.skill` or a data part with `"skill"`).
  A text-only message to a multi-skill agent is rejected, by design.
- **Omitting `"operation"` in an entity skill's data part.** The task is
  REJECTED naming the operations. `"arguments"` may be omitted only when
  the operation needs nothing (`list`); a present non-object
  `"arguments"` is also rejected.
- **Auth middleware that never runs on `/a2a`.** The exchange resolves
  the owner from the request context; a hand-wired mux that bypasses the
  app router leaves every caller at 401. Mount via `WithA2A` and keep
  the router's middleware chain in front of it.
- **Expecting another user's task id to 404 differently.** It answers
  `-32001` exactly like an unknown id, so ids cannot be used to probe
  whose tasks exist.
- **Treating tasks as durable background jobs.** No retries, no
  dead-letter, `TaskTimeout` kills long runs. Use
  [`battery/queue`](queue.md) for work that must survive; see
  [tasks are not jobs](#tasks-are-not-jobs).
- **Pointing the card's `A2AEndpoint` at a path nothing serves.** The
  card field only advertises; `WithA2A` is what mounts. The pair must
  name the same path.
