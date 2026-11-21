# A2A: Agent-to-Agent protocol

`framework.WithA2A` mounts an Agent2Agent (A2A) v1.0 JSON-RPC task exchange (default `/a2a`), paired with `uihost.WithAgentCard` (or `uihost.WithAgentReady`) to publish the Agent Card at `/.well-known/agent-card.json`.

## Quickstart

Configure discovery metadata on the UI host (`uihost.WithAgentCard`), and configure the task exchange and skills on `framework.WithA2A`:

```go
host := uihost.New(site,
	uihost.WithAgentCard(uihost.AgentCardConfig{
		Name:        "Support Triage Agent",
		Description: "Classifies and routes customer support tickets.",
		A2AEndpoint: "/a2a",
	}),
)

app := framework.NewUIHostApp(host,
	framework.WithDB(db),
	framework.WithA2A(framework.A2AConfig{
		Path:   "/a2a",
		Skills: []a2a.Skill{triageSkill}, // or enable entity MCP tools for derived entity skills
	}),
)
```

`App.Start` fails if neither hand-written `Skills` nor entity-derived skills are present. In a role-split deployment (`GOFASTR_ROLE=agent`), GoFastr serves `/a2a` alongside `/mcp`, `/healthz`, and `/readyz` without exposing browser pages or entity CRUD routes (see [Agent host](agent-host.md) and [Horizontal scaling](scaling.md)).

## Tasks and lifecycle

A2A v1.0 organizes asynchronous agent work around JSON-RPC methods `SendMessage`, `SendStreamingMessage`, `GetTask`, and `CancelTask` (plus `GetExtendedAgentCard` when `ExtendedCard` is configured):
- **States:** `submitted` → `working` → `input-required` / `completed` / `failed` / `canceled`.
- **Streaming:** `SendStreamingMessage` streams status updates and partial artifacts over SSE.
- **Persistence:** `A2AConfig.Store` defaults to an SQL store on `App.DB` (or an in-memory store when no DB is configured).
- **Graceful shutdown:** `App.Shutdown` cancels in-flight A2A task contexts and waits for them to drain before closing batteries and the database.

## Auth and owner scoping

`/a2a` runs through your app's router middleware chain (`Cookie`, `Authorization`, `X-API-Key`) with automatic owner scoping and tenant scoping, and **rejects anonymous callers before dispatch** — even for skills backed by public entities:
- **Fail-closed task isolation:** every A2A task is bound by owner scoping to the caller's owner ID (and tenant ID when multi-tenant). One caller cannot read (`GetTask`), subscribe to, or cancel (`CancelTask`) another caller's task ID — foreign task IDs return not-found.
- **Scoped tokens:** restrict service-to-service agent callers with scoped API tokens rather than admin credentials.

## Common mistakes

- **Configuring `Name`/`Description`/`URL` on `framework.A2AConfig`:** Agent Card metadata belongs on `uihost.WithAgentCard` (or `uihost.WithAgentReady`), not `A2AConfig`.
- **Starting `WithA2A` with zero skills:** supply at least one `a2a.Skill` in `A2AConfig.Skills` or register an entity with `Exposure.MCP: true` (unless `DisableEntitySkills: true`).
- **Ignoring `ctx.Done()` inside custom A2A skill handlers:** prevents `CancelTask` and `App.Shutdown` from stopping runaway work cleanly.
- **Treating task IDs as capability secrets without owner scoping:** rely on GoFastr's owner scoping and authentication so callers can only access their own tasks (anonymous requests are rejected before skill dispatch).
- **Forgetting to set `BaseURL` on `uihost.WithAgentReady` / `WithSitemap`:** the Agent Card advertises your canonical origin to discovering agents; a localhost origin breaks remote clients.
