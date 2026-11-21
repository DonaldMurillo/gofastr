# UI capability map

Start here when your question is about a UI shape rather than a Go package—"Can I build a live dashboard?", "Where should optimistic state live?", or "Do I need a client framework?" This map pairs each product job with its components, state ownership, delivery tier, runnable example, and reference docs.

## The state boundary

Default architecture across GoFastr UI:

```text
database/server  = durable business truth
signals/store    = immediate client UI projection
RPC              = mutation and reconciliation
polling          = passive freshness without a held connection
SSE              = push delivery when the connection itself matters
HTML/data result = authoritative response
```

A local signal makes a control feel instant, never durable. An optimistic update moves first, and the RPC response commits or rolls it back. SSE notifies connected views about background or other-user changes—it never confirms the user's own click.

## What kind of UI are you building?

Run `go run ./examples/site` to interact with every linked gallery route live.

| Job to be done | Compose | State, delivery, and scaling | Runnable proof | Primary docs |
|---|---|---|---|---|
| CRUD/admin or form-heavy screens | `ui.DataTable`, `ui.Form`, `ui.FormField`, entity CRUD, `battery/admin` | Server owns truth, validation, filtering, and pagination. Stateless and replica-safe. | [`/components/datatable`](../../../examples/site/components.go), [`/components/form`](../../../examples/site/components.go), [`examples/meridian`](../../../examples/meridian/main.go) | [Forms](form-module.md), [Admin](admin.md), [Interactive patterns](interactive-patterns.md) |
| Optimistic mutations | `ui.OptimisticAction`, `ui.ToggleAction`, versioned `sortablelist` | Immediate client projection; RPC validates and commits, rolling back on error or refetching on `409`. | [`/components/optimisticaction`](../../../examples/site/components.go), [`/components/toggleaction`](../../../examples/site/components.go) | [Optimistic UI](optimistic-ui.md), [Interactive patterns](interactive-patterns.md), [Runtime contract](runtime-contract.md) |
| Live dashboards and streamed status | SSR charts/metrics, `store.Slice`, island signals, `data-cui-poll`, SSE | SSR renders first paint. Prefer polling (`data-cui-poll`) for passive freshness (stateless, any replica). Use SSE (`WithFanout` across replicas) for sub-second push; queue/outbox for lossless work. | [`/examples/live-dashboard`](../../../examples/site/screen_livedash.go), [`/components/rpc-signal`](../../../examples/site/components.go), [`/components/linechart`](../../../examples/site/components.go), [`/components/recordsummary`](../../../examples/site/components.go) | [Live dashboards](live-dashboards.md), [Reactivity model](reactivity.md), [Signal store](signal-store.md), [Events and SSE](events.md), [Scaling](scaling.md) |
| Master/detail workspaces | `ui.PaneHost`, server-rendered detail fragments, durable routes | URL identifies the selected record; `PaneHost` manages in-page pane behavior while RPC swaps fragments. | [`/examples/workspace`](../../../examples/site/screen_workspace.go), [`/components/panehost`](../../../examples/site/components.go) | [Pane host](pane-host.md), [UI composition recipes](ui-composition-recipes.md) |
| Sortable lists and Kanban | `framework/ui.SortableList` with stable keys and version | Browser previews drag reorder; RPC persists durable order and restores/refetches on error or `409`. | [`/components/sortablelist`](../../../examples/site/components.go) | [Interactive patterns](interactive-patterns.md), [Runtime contract](runtime-contract.md) |
| Notifications, progress, and activity feeds | `ui.NotificationBell`, toast presets, `progress`, `ui.Timeline`, signals/SSE | Toasts are ephemeral; durable read state and lossless feeds live in the DB/outbox, not the lossy SSE buffer. | [`/components/notificationbell`](../../../examples/site/components.go), [`/components/progress`](../../../examples/site/components.go), [`/components/timeline`](../../../examples/site/components.go) | [Widgets](widgets.md), [Events and SSE](events.md), [Notifications](notifications.md) |
| Server-authoritative reactive SaaS | SSR screen + typed store + RPC fragment swaps + SSE invalidation | SSR seeds the typed store; user writes go through RPC; background updates arrive via SSE (`WithFanout` for multi-replica). | [`/components/signal-store`](../../../examples/site/components.go), [`/components/rpc-form-signal`](../../../examples/site/components.go) | [Signal store](signal-store.md), [Interactive patterns](interactive-patterns.md), [Events and SSE](events.md) |
| Presence and collaborative awareness | `island.Manager` presence topics + `ui.AvatarGroup` | Server-derived identity; live, self-healing rosters merge across replicas when `WithFanout` is enabled. | [`/examples/presence`](../../../examples/site/screen_presence.go), [`/components/avatargroup`](../../../examples/site/components.go) | [Presence](presence.md), [Scaling](scaling.md) |
| Static/exportable UI | SSR screens + `App.ExportStatic` + client-only signals | Build-time HTML/assets with no Go server; server RPC/SSE actions disable cleanly. | none in the repo | [Static export](static-export.md), [Runtime contract](runtime-contract.md) |
| SPA integration by deliberate choice | GoFastr HTTP/OpenAPI/MCP backend + React, Vue, or Svelte client | Client framework owns rendering; GoFastr owns data, auth, validation, and APIs. Keep the two renderers separate. | none in the repo | [Entity declarations](entity-declarations.md), [Security](security.md), [SDKs](sdk.md) |

## Decide where state lives

| Decision | Prefer the first when | Prefer the second when |
|---|---|---|
| Local signal vs typed store | One small producer/consumer scope that can reset with the page. | Shared across components, needs SSR seeding, or app-global lifetime (`core-ui/store`). |
| Computed signal vs server recomputation | Cheap, synchronous, presentation-only derivation with a registered reducer. | Depends on auth, DB data, pagination/filtering, or business rules. |
| RPC signal vs fragment swap | Response is one scalar/attribute shared by bound nodes. | Response is structured UI—let Go render HTML and swap the island. |
| Optimistic update vs pending-only action | Reversible, low-risk, with clean rollback/reconciliation. | Destructive, expensive, or security-sensitive—wait for the server response. |
| SSE push vs durable queue/outbox | A newer update heals a missed one (counts, status, presence, dashboard refresh). | Every event must run (billing, email, workflows, audit)—use `battery/queue` or outbox. |
| GoFastr UI vs React/Vue/Svelte | SSR-first screens, server-owned rules, and tiny JS fit the product. | The product centers on a client-owned render graph or heavy JS ecosystem widget. |

## Stateless and affinity-bound islands

- **Reconstructable islands (preferred):** each RPC reads route, query, session, and DB state to render the next fragment. Any replica can serve any request—no sticky routing needed.
- **Affinity-bound islands:** mutable widget objects kept in one process's RAM. `WithFanout` bridges SSE pushes, not in-memory objects. Redesign around reconstructable state before adding a second replica (see [Horizontal scaling](scaling.md)).
- **Client-only signals:** pure browser UI projections, reset on reload unless declared app-global and reseeded.

## Delivery guarantees

| Lane | Guarantee | Use it for | Do not use it for |
|---|---|---|---|
| RPC response | One request → one authoritative status + HTML/data body | Mutations, validation, fragment swaps, reconciliation | Broadcasting to other sessions |
| Polling | Interval GET; any replica answers from DB | Passive freshness (dashboards, counters, statuses) | Sub-second push or connection-presence |
| SSE / event bus | Best-effort; slow consumers drop old frames | Live status, invalidation, presence, realtime updates | Billing, audit, lossless workflows |
| Fanout | Best-effort cross-replica broadcast | Delivering SSE/island push to whichever replica holds the socket | Single-execution side effects |
| Queue / outbox | Durable, at-least-once delivery (idempotent consumers) | Workflows, email, webhooks, durable side effects | Immediate browser confirmation |

**Explicit non-goals:** GoFastr does not specialize in canvas/media editors, timeline/video authoring, offline-first CRDT workspaces, or a client-owned virtual DOM. Use a dedicated client library via the API or plugin boundary when those are the core of your product.

## Search vocabulary

Find this map offline with `gofastr docs --grep`:

```text
reactive state · client state · optimistic UI · rollback · reconciliation
live updates · realtime · event stream · live dashboard · mutation
derived state · cache invalidation · server-driven UI
```

## Common mistakes

- **Starting from a component name.** Decide truth ownership, mutation path, and delivery tier first; then pick components.
- **Treating a signal as durable state.** Signals are UI projections; persist real data on the server.
- **Using SSE to confirm the user's own click.** The initiating RPC response owns confirmation and rollback.
- **Expecting `WithFanout` to make process-local state stateless.** Fanout bridges push frames, not in-memory structs.
- **Reaching for React/Vue by habit.** Use a client SPA when the product genuinely needs a client-owned render graph, not for forms, tables, or island swaps the runtime already handles.
