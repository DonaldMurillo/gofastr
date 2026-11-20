# A2A: Agent-to-Agent protocol

`framework.WithA2A` adds an Agent-to-Agent (A2A) JSON-RPC endpoint (default `/a2a`) and Agent Card discovery (`/.well-known/agent-card.json`) so external AI agents can discover your app's capabilities, submit long-running tasks, stream progress, and fetch artifacts over a standard protocol.

## Quickstart

```go
app := framework.NewApp(
	framework.WithDB(db),
	framework.WithA2A(framework.A2AConfig{
		Name:        "Support Triage Agent",
		Description: "Classifies and routes customer support tickets.",
		URL:         "https://app.example.com/a2a",
	}),
)
```

In a role-split deployment (`GOFASTR_ROLE=agent`), GoFastr serves `/a2a` alongside `/mcp`, `/healthz`, and `/readyz` without exposing browser pages or entity CRUD routes (see [Agent host](agent-host.md) and [Horizontal scaling](scaling.md)).

## Tasks and lifecycle

A2A organizes asynchronous agent work around tasks (`tasks/send`, `tasks/sendSubscribe`, `tasks/get`, `tasks/cancel`):
- **States:** `submitted` → `working` → `input-required` / `completed` / `failed` / `canceled`.
- **Streaming:** `tasks/sendSubscribe` streams status updates and partial artifacts over SSE.
- **Graceful shutdown:** `App.Shutdown` cancels in-flight A2A task contexts and waits for them to drain before closing batteries and the database.

## Auth and owner scoping

`/a2a` forwards through your app's router middleware chain, so `Cookie`, `Authorization`, and `X-API-Key` authentication plus owner scoping and tenant scoping apply automatically:
- **Fail-closed task isolation:** every A2A task is bound to the caller's owner ID (and tenant ID when multi-tenant). One caller cannot read (`tasks/get`), subscribe to, or cancel another caller's task ID — foreign task IDs return not-found.
- **Require authentication:** never expose `/a2a` anonymously on a public listener unless every skill and underlying entity is intentionally public.
- **Scoped tokens:** restrict service-to-service agent callers with scoped API tokens rather than admin credentials.

## Common mistakes

- **Exposing `/a2a` without authentication middleware:** lets unauthenticated callers invoke skills and consume compute/LLM quota.
- **Ignoring `ctx.Done()` inside custom A2A skill handlers:** prevents `tasks/cancel` and `App.Shutdown` from stopping runaway work cleanly.
- **Treating task IDs as capability secrets without owner scoping:** always rely on GoFastr's owner and tenant scoping so callers can only access their own tasks.
- **Forgetting to set `URL` to your canonical public origin in `A2AConfig`:** the Agent Card advertises `URL` to discovering agents; a localhost URL breaks remote clients.
