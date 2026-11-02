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
rejected. The rejection names the skill asked for and lists no others:
the exchange serves entity skills the public card leaves out, so the
card is where a client finds the skills it can call. There is no
model call, no intent classification, no fallback guess on the server
side. A client that sends "please list my invoices" as text gets a
rejected task; a client that sends
`{"skill": "entity.invoices", "operation": "list"}` gets invoices.

That determinism is the point: the calling agent (the LLM, on the other
side of the wire) decides *what* to ask for; this server decides exactly
*how*, and the same request always produces the same work.

## The wire surface

One endpoint, `A2AConfig.Path` (default `/a2a`), POST only, content type
`application/json` (or `application/a2a+json`). A browser `Origin` naming any
authority other than the request's own Host is refused 403 before the body is
read, the same cross-origin gate the MCP transport enforces; an absent Origin
passes, because curl and native A2A clients send none. (`A2AConfig.AllowedOrigins` permits tunnel or split-origin browser
Origins; embedders using `core/a2a` directly have `Config.AllowedOrigins` or
`Server.SetAllowedOrigins`.) JSON-RPC 2.0 methods,
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
`-32601` method not found and that is correct. The JSON-RPC envelope is
decoded with strict top-level keys: a body repeating a key, or spelling
one in two cases (`"method"` and `"Method"`), is refused with a parse
error rather than silently resolved last-key-wins.

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

A task also records the scopes of the credential that created it. A
caller whose API token or embed grant narrows it (`access.HeldScopes`)
sees only the tasks made under the same scope set, in any order; to it,
a task its owner's session or a broader token ran is `-32001` too, and
`ListTasks` leaves it out of the page and `totalSize`. A session sees
every task of its owner. `WithA2A` wires this; a server built by hand
passes `a2a.Config.Scopes`. A custom `a2a.Store` keeps
`TaskRecord.Scope`. One that filters `ListTasks` by `ListQuery.Scoped`
in its query declares the optional `a2a.ScopeFilter` interface (both
built-in stores do); without it, the server lists a scoped caller by
walking every task of the owner and counting the caller's alone, so the
page, `totalSize` and `nextPageToken` never count another tier's tasks.
The server also drops another tier's task from a declaring store's page,
so a store that declares it wrongly hands out no task, though its
`totalSize` is then its own. The SQL store adds
the `scope` column to an older table on open, and its rows read as a
session's.

A task also records the tenant the creating request acted in. With a
tenant hook, a caller reads, lists, resumes, cancels and configures only
the tasks made in the tenant it is in now: a member removed from tenant
A, or switched to B, gets `-32001` for A's tasks, and an open stream on
one ends at the next keep-alive once the refreshed principal resolves
to another tenant. `WithA2A` wires the tenant your tenant middleware
resolved (`tenant.GetTenantID`); a server built by hand passes
`a2a.Config.Tenant`. The owner stays the user id, which erasure deletes
by. A custom `a2a.Store` keeps `TaskRecord.Tenant` and never changes it
on update; one that filters `ListTasks` by `ListQuery.Tenanted` in its
query declares `a2a.TenantFilter` (both built-in stores do), and without
it the server walks the owner's tasks the same way as for scopes. The
SQL store adds the `tenant` column to an older table on open, and its
rows read as made in no tenant, so a caller in a tenant no longer sees
them.

Every request-driven store call runs on the request's context, so a
store that scopes by it (its tenant, its trace) sees the caller:
`GetTask`, `ListTasks`, and the four push-config methods read on it, and
a client that leaves stops the read. `CancelTask` and the push-config
create carry the request's values but not its cancellation, so a cancel
or a create the client abandons halfway still completes. A push config
belongs to its task: every push-config method first reads the task
under the caller's owner, scopes and tenant, and answers `-32001` when
the caller cannot see it. The `ExtendedCard` hook gets the request's
context too.

The SQL task store registers its task and push-config tables with
`App.ExportData` and `App.EraseUserData`, keyed on the owner column, so
erasing a user deletes their tasks and push configs (see
[data export](data-export.md)).

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
With a shared store the cancel may be served by another replica: the
replica running the task polls the store while the handler runs, and on
finding a terminal state it did not write it cuts the handler's context
and sends the state to its own subscribers. A
cancel that lands before the handler has started (a streaming client
knows the task id from the first snapshot) means the handler never
runs at all; the task settles as `TASK_STATE_CANCELED`.

One handler run is bounded by `A2AConfig.TaskTimeout` (default 5
minutes); a run that exceeds it fails with `task timed out`. Past the
deadline every `TaskContext` mutation (`Working`, `Artifact`,
`Complete`, …) returns an error wrapping `context.DeadlineExceeded`, so
a handler that finishes late cannot mark the task completed. A run
whose task another replica settled and retention then removed is
cancelled too; its local subscribers' streams end with no status event.

`App.Shutdown` ends runs as well. A run outlives its request (a
`returnImmediately` send, a dropped stream), so the app calls
`Server.Shutdown(ctx)` first: no run is claimed after it, every live
run's context is cancelled, and it waits until each run has stored and
published its final state (`TASK_STATE_CANCELED` for one cut short) or
`ctx` ends. A send or resume during shutdown gets an
unsupported-operation error; reads still answer. A host driving
`a2a.Server` itself calls `Shutdown` the same way.

Host code that runs on the server's own goroutines cannot take the
process down. A panicking skill handler fails its task. A `Store` or
`RetentionTrimmer` that panics while a run settles ends that run with an
`a2a: run finalize panicked` log line; the task keeps its last stored
state. A `PushOptions.Client` that panics fails that one delivery with
an `a2a: push delivery panicked` line. Each panic value is logged
scrubbed of control bytes.

Terminal rows do not accumulate without end. Retention counts per
**tier**: one owner's tasks made in one tenant under one scope set
(`a2a.RetentionTier`, the task's `Tenant` and `Scope`; a session is the
unscoped tier, each narrowed API token or embed grant its own). Each
tier retains at most 64 settled (completed / failed / canceled /
rejected) tasks — the tier's oldest terminal rows are deleted after
each settle in it, in-flight tasks never. Paused tasks (input or auth
required) are capped at 64 per tier too: past the bound the tier's
oldest are canceled after each pause in it, and terminal retention then
deletes them. One tier's activity never counts, deletes or cancels
another tier's tasks: a narrow token cannot push out the results a
session ran, and work in one tenant cannot push out another's. An owner
therefore retains up to 64 + 64 tasks per tenant and scope set it acts
in. Each task carries at most 8
push-notification configs, the oldest deleted past the bound. A trimmed task's push configs are deleted
with it, and each trim that deletes tasks also deletes the owner's
configs whose task is already gone (one registered while an earlier trim
deleted its task). Every row holds the caller's message
body (up to the 1 MiB cap) and the default store is in-RAM, so cheap
authenticated writes must not grow either table unbounded. The bounds
are `a2a.Config` knobs (`TerminalTaskRetention`, `PausedTaskRetention`,
`MaxPushConfigsPerTask`; 0 = the default, negative = the host owns
retention). The terminal and push-config bounds go through the optional
`a2a.RetentionTrimmer` Store interface both built-in stores implement
(`TrimTerminalTasks(ctx, owner, tier, keep)` trims only rows whose
tenant and scope equal the tier's); the paused bound cancels through
`UpdateTask`, so it holds on any Store.

Skill-handler runs are bounded per owner too:
`Config.MaxConcurrentRunsPerOwner` caps how many runs one owner may
have going at once, `returnImmediately` runs included (0 = 16,
negative = unlimited for deployments that bound runs elsewhere). A new
message past the cap, or a resume of an `INPUT_REQUIRED` task while
every slot is taken, is refused with `-32004`
(`maximum concurrent task runs reached`) before anything is written:
no task row is created and the paused task stays as it was.

### Streaming

`SendStreamingMessage` and `SubscribeToTask` answer
`text/event-stream`. Each event's data is a complete JSON-RPC response
object whose result is a `StreamResponse` — exactly one of `task`,
`message`, `statusUpdate`, `artifactUpdate`. The stream sends the
current task snapshot first, then every event until the task is terminal
or interrupted, then closes. A comment line (`: keep-alive`) paces idle
streams every 15 seconds so proxies do not time the connection out.

Each stream pins a relay goroutine, a bus channel, and tickers for
its whole life, so one owner's concurrent streams are seated:
`Config.MaxStreamSeatsPerOwner` (0 = 16, negative = unlimited for
deployments that bound streams elsewhere) and
`Config.StreamSeatOverflow` select what an owner at the cap does with
their next stream — `stream.SeatOverflowRefuse` (the default)
answers 429 at connect, `SeatOverflowEvictOldest` closes that owner's
oldest stream and seats the new one. A `SendStreamingMessage` refused
at the seat cap is refused before anything is written: no task is
created and no run slot is taken. A stream whose client is gone before
the first event still runs the task it created, as if the client had
left right after the snapshot.

`SubscribeToTask` to a task running in this process sends the snapshot,
then only the events the snapshot does not already reflect. Each event
carries the task version its write produced, and events at or below
the snapshot's version are skipped, so an artifact append
(`append: true`) is never applied twice on top of a snapshot that
already holds it.

With the SQL store, `SubscribeToTask` on a task running on another
replica falls back to polling the store and emitting a snapshot whenever
the stored version changes, and so does a subscribe whose local run ends without settling the task, or whose local run has paused while the store holds a newer version (another replica resumed it) — per-event fidelity across a replica
boundary would need a shared bus, which a SQL store cannot give. Events
between polls coalesce into the next snapshot. Its store reads run under
the request's context, so a hung store read ends when the client leaves
or the stream's seat is evicted.

## Push notifications

A client registers a webhook per task (in `SendMessage`'s
`configuration.taskPushNotificationConfig`, or via
`CreateTaskPushNotificationConfig` after the task exists). The server
POSTs each `StreamResponse` to the configured URL with the
`A2A-Notification-Token` header carrying the config's token, so the
receiver can check the notification came from the agent it registered
with.

Delivery is best effort by design: one attempt, no retry, a non-2xx or
transport error is logged once. Deliveries to one push config are sent
one at a time, in event order, since an appended artifact chunk carries
no sequence number. An event that reaches a config's queue after a newer
one is dropped rather than sent late, so a terminal state is always the
last notification a receiver gets. At most 16 configs have a delivery in flight per
server, and up to 16 further events wait behind them process-wide;
anything past those bounds is dropped with a Warn log line, so a hung
receiver cannot pile up goroutines and payloads and delays only its own
config. Reliable delivery with backoff is
[queue](queue.md)'s job; task progress must never stall on a receiver
that is down. The SSRF posture: push URLs targeting internal hosts
(loopback, RFC1918, CGNAT, link-local) are refused at registration
unless `A2AConfig.AllowPrivatePush` is set, redirects are not followed,
and the client is dial-time guarded. The guarded client never routes
through an `HTTP_PROXY`/`HTTPS_PROXY` proxy, because the dial check
cannot see the connection a proxy makes. A caller-supplied
`PushOptions.Client` whose transport is a plain `*http.Transport` (no
`Proxy`, no custom dialer) is cloned and given the same dial-time
guard, so the address checked is the address dialed. Any other
transport (one with a proxy or a custom dialer, or a `RoundTripper` that
is not an `*http.Transport`) keeps its own routing and is wrapped with a
resolve-then-check guard instead; that guard resolves once to check and
the transport resolves again to dial, so a DNS name that rebinds
between the two passes it.

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
- `app.A2ASkills()` feeding the agent card (its entity skills are the
  ones an anonymous `tools/list` shows, none when `WithMCPGate` refuses
  anonymous callers, since a skill names its entity and fields; every
  skill call runs the gate), and
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
