# UI capability map

Start here when the question is about a product shape rather than a Go
package: "Can I build a live dashboard?", "Where should optimistic state
live?", or "Does this need a client framework?" The component catalog answers
what symbols exist. This guide maps jobs to primitives, state ownership,
delivery and scaling semantics, runnable proof, and the detailed API docs.

## The state boundary

Use this as the default architecture:

```text
database/server = durable business truth
signals/store    = immediate UI projection
RPC              = mutation and reconciliation
SSE              = update/invalidation delivery
HTML/data result = authoritative response
```

That boundary is more important than the individual component choice. A local
signal can make a control feel immediate, but it does not become business
truth. An optimistic projection can move first, but the RPC response commits
or rolls it back. SSE tells connected views that server state changed; it does
not confirm the user's own mutation.

## What kind of UI are you building?

Every row distinguishes a capability from a packaged convenience. "Proof"
names a route in the runnable `examples/site` gallery (start it with
`go run ./examples/site`) or a complete example program. "Primary docs" points
to the public contract rather than repeating it here.

| Job to be done | Compose | State, delivery, and scaling semantics | Runnable proof | Primary docs |
|---|---|---|---|---|
| CRUD/admin or form-heavy screens | `ui.DataTable`, `ui.Form`, `ui.FormField`, entity CRUD, `battery/admin` | The database is truth. Filters, validation, pagination, and writes run on the server. RPC or normal form responses return authoritative HTML/data. CRUD requests are stateless and replica-safe; sessions and live invalidation still need the shared backends described in scaling. | [`/components/datatable`](../../../examples/site/components.go), [`/components/form`](../../../examples/site/components.go), and [`examples/backoffice`](../../../examples/backoffice/main.go) | [Forms](form-module.md), [Admin](admin.md), [Interactive patterns](interactive-patterns.md) |
| Optimistic mutations | `ui.OptimisticAction`, `ui.ToggleAction`, versioned `sortablelist` | A signal/DOM change is a temporary projection. The RPC owns validation and commit. Non-2xx rolls back; a versioned 409 refetches authoritative server HTML. Do not wait for SSE to confirm the initiating action. | [`/components/optimisticaction`](../../../examples/site/components.go), [`/components/toggleaction`](../../../examples/site/components.go) | [Interactive patterns](interactive-patterns.md), [Runtime contract](runtime-contract.md) |
| Live dashboards and streamed status | SSR charts/status components, `store.Slice`, island signals, SSE | SSR supplies the first complete view. The server owns metrics; signals project the latest value. SSE is best-effort update/invalidation delivery and may drop old frames for slow consumers. Add `WithFanout` across replicas; use an outbox/queue for lossless work. | [`/examples/live-dashboard`](../../../examples/site/screen_livedash.go), [`/components/rpc-signal`](../../../examples/site/components.go), [`/components/linechart`](../../../examples/site/components.go), [`/components/recordsummary`](../../../examples/site/components.go) | [Live dashboards](live-dashboards.md), [Signal store](signal-store.md), [Events and SSE](events.md), [Scaling](scaling.md) |
| Master/detail workspaces | `ui.PaneHost`, server-rendered detail fragments, routes for durable/deep-linkable identity | The route identifies the selected durable record. Opening/swapping a pane is in-page projection; RPC can return its detail HTML. `PaneHost` owns pane behavior, not data fetching. Reconstructable handlers are stateless; process-held pane/widget objects require affinity. | [`/examples/workspace`](../../../examples/site/screen_workspace.go), [`/components/panehost`](../../../examples/site/components.go) | [Pane host](pane-host.md), [UI composition recipes](ui-composition-recipes.md) |
| Sortable lists and Kanban | `core-ui/patterns/sortablelist`, stable keys, optional group/container/version | The browser previews a reorder. RPC persists it. Non-2xx restores the prior DOM; versioned 409 responses refetch server-rendered column state for reconciliation. Durable order belongs in the database. | [`/components/sortablelist`](../../../examples/site/components.go) | [Interactive patterns](interactive-patterns.md), [Runtime contract](runtime-contract.md) |
| Notifications, progress, and activity feeds | `ui.NotificationBell`, toast presets, `progress`, `ui.Timeline`, signals/SSE | A toast is ephemeral projection; durable notification/read state belongs in the database. Progress can be an RPC result for user work or an SSE update for background work. An activity feed that must not lose entries comes from durable rows/outbox, not the lossy SSE buffer. | [`/components/notificationbell`](../../../examples/site/components.go), [`/components/progress`](../../../examples/site/components.go), [`/components/timeline`](../../../examples/site/components.go) | [Widgets](widgets.md), [Events and SSE](events.md), [Notifications](notifications.md) |
| Server-authoritative reactive SaaS | SSR screen + typed store + RPC signal/fragment swaps + SSE invalidation | The store is a typed client projection seeded from SSR. User mutations use RPC and receive authoritative HTML/data. Background/other-user changes arrive through SSE and trigger a signal update or refetch. Shared fanout makes the push lane replica-aware; durable side effects stay on outbox/queue consumers. | [`/components/signal-store`](../../../examples/site/components.go), [`/components/rpc-form-signal`](../../../examples/site/components.go) | [Signal store](signal-store.md), [Interactive patterns](interactive-patterns.md), [Events and SSE](events.md) |
| Presence and collaborative awareness | `island.Manager` presence topics + `ui.AvatarGroup` | Identity is derived by the server. Rosters are live, lossy, self-healing state, not an audit record. With fanout they merge across replicas; without it they are local to one process. | [`/examples/presence?presence=presence-demo`](../../../examples/site/screen_presence.go), [`/components/avatargroup`](../../../examples/site/components.go) | [Presence](presence.md), [Scaling](scaling.md) |
| Static/exportable UI | SSR screens + `App.ExportStatic`; client-only signals/theme/copy where useful | The export is build-time truth: HTML and assets run without a Go server. Server RPC, SSE, and server-backed widgets are deliberately disabled and must not be presented as live. | [`examples/static-site`](../../../examples/static-site/README.md) | [Static export](static-export.md), [Runtime contract](runtime-contract.md) |
| SPA integration by deliberate choice | GoFastr HTTP/OpenAPI/MCP backend + Vue, React, Svelte, or another client | The client framework owns browser state and rendering. GoFastr still owns durable data, authorization, validation, and API responses. This is a separate architecture, not a way to mix a second renderer into a GoFastr-managed screen. | [`examples/spa`](../../../examples/spa/README.md) | [Entity declarations](entity-declarations.md), [Security](security.md), [SDKs](sdk.md) |

## Decide where state lives

| Decision | Prefer the first option when | Prefer the second option when |
|---|---|---|
| Local signal vs typed store | The value has one small producer/consumer scope and can reset with the page. | Many consumers share it, it needs SSR seeding, or app-global lifetime must be explicit. Use `core-ui/store`. |
| Computed signal vs server recomputation | The derivation is cheap, synchronous, presentation-only, and can be expressed by a CSP-safe registered reducer. | The result depends on authorization, durable data, pagination/filter rules, expensive work, or business invariants. Return it from the server. |
| RPC signal vs fragment swap | The authoritative response is one text/value/attribute shared by bound consumers. | The authoritative response is structured UI. Let Go render HTML and replace the island region. |
| Optimistic update vs pending-only action | The action is reversible, the prior projection is known, and failure/409 reconciliation is designed. | The effect is destructive, expensive, security-sensitive, or cannot be undone honestly. Show pending state, then commit from the response. |
| SSE latest-state/drop vs durable queue/outbox | A future update heals a missed one: badge counts, status, invalidation, presence, dashboard refresh. | Every event/side effect matters: billing, email, workflow transitions, audit, or ordered processing. Use `battery/queue` or the transactional outbox. |
| Process-local island vs reconstructable state | You deploy one process or deliberately configure affinity, and losing ephemeral state on restart is acceptable. | You need stateless load balancing or restart survival. Reconstruct from URL/session/database on every RPC and use fanout only for push delivery. |
| GoFastr UI vs Vue/React/Svelte | SSR-first screens, server-owned rules, small generic JS, and HTML fragment reconciliation fit the product. | The product fundamentally needs a client-owned render graph or an ecosystem-specific client library. Keep GoFastr as the API/backend boundary. |

## Stateless and affinity-bound islands

These terms describe server ownership, not visual appearance.

- A **reconstructable island** reads route/query/session/database state for each
  request and renders the next authoritative fragment. Any replica can handle
  its RPC. This is the preferred shape.
- An **affinity-bound island** holds mutable widget/island objects in one
  process. `WithFanout` can deliver an update to the replica holding an SSE
  connection, but it cannot recreate that object on another replica. Use
  sticky sessions or redesign the handler around reconstructable state.
- A client-only signal is neither durable nor affinity-bound server state. It
  is a projection and may reset unless declared app-global and reseeded.

Read [Horizontal scaling](scaling.md) before adding the second replica.

## Delivery guarantees

| Lane | Guarantee | Use it for | Do not use it for |
|---|---|---|---|
| RPC response | One request receives one status and authoritative HTML/data response | Mutations, validation, reconciliation, fragment swaps | Broadcast to other sessions |
| SSE/event bus | Best effort; default slow-client mode drops the oldest queued frames | Latest status, invalidation, presence, live dashboard updates | Billing, audit, exactly-once workflows |
| Fanout | Best-effort broadcast between replicas; handlers run on every replica | Making SSE/island push visible wherever the browser connected | Single-execution side effects |
| Queue/outbox | Durable, at-least-once processing; consumers must be idempotent | Workflows, email, webhooks, audit-adjacent side effects | Immediate browser confirmation |

Performance claims are bounded by what is measured. [Benchmarks](benchmarks.md)
includes island RPC, SSE delivery/drop metrics, and full UI-host SSR. It does
not measure browser hydration or promise that lossy delivery became durable.

## Capability, convenience, and non-goals

- **Capability** means the public primitives compose the architecture and a
  runnable example proves the path.
- **Packaged convenience** means GoFastr provides the product-shaped component
  or battery directly, such as `DataTable`, `PaneHost`, `OptimisticAction`, or
  `battery/admin`. A capability may exist without a one-call convenience.
- **Proven performance** means a named benchmark exercises that exact lane.
  Treat unmeasured browser/network behavior as unmeasured.
- **Explicit non-goals:** GoFastr does not specialize in canvas/media editors,
  timeline/video authoring, offline-first CRDT workspaces, or a client-owned
  virtual DOM. Use a purpose-built client library and integrate it through the
  API/plugin boundary when those are the center of the product.

## Search vocabulary

`gofastr docs --grep` is substring search over the same embedded corpus exposed
by the framework docs MCP tools. Useful ecosystem terms for this guide include:

```text
reactive state
client state
optimistic UI
rollback
reconciliation
live updates
realtime
event stream
live dashboard
mutation
derived state
cache invalidation
server-driven UI
```

Examples:

```bash
gofastr docs ui-capability-map
gofastr docs --grep "live dashboard"
gofastr docs --grep reconciliation
gofastr docs --grep "reactive state"
```

## See also

- [UI composition recipes](ui-composition-recipes.md) for product-shaped page
  grammar after the architecture is chosen.
- [UI components index](ui-new-components.md) for constructors and exact live
  gallery routes.
- [Interactive patterns](interactive-patterns.md) for RPC, optimistic, signal,
  rollback, and reconciliation wiring.
- [Signal store](signal-store.md) for typed client projection and derived state.
- [Runtime contract](runtime-contract.md) for the SSR/hydration/island/SSE
  boundary.
- [Events and SSE](events.md), [Presence](presence.md), and
  [Scaling](scaling.md) for delivery and replica semantics.

## Common mistakes

- **Starting from a component name.** State the job, truth owner, mutation
  path, and delivery guarantee first; then choose components.
- **Calling a signal durable state.** It is a UI projection. Persist business
  truth on the server.
- **Using SSE as mutation acknowledgment.** The initiating RPC response is the
  acknowledgment and reconciliation channel.
- **Assuming fanout makes process-held widget state stateless.** It only bridges
  the push lane; reconstruct state or configure affinity.
- **Promising durable realtime from the default event stream.** Default SSE is
  latest-state delivery and can drop frames. Use queue/outbox for durable work.
- **Choosing a client framework by reflex.** Use one when the product needs a
  client-owned renderer, not to recreate an island or fragment swap already
  covered by the runtime.
