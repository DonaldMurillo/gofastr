# Signal store (shared client state)

`core-ui/store` is a typed, server-declared shared-state primitive. It layers
typing, namespacing, and **SSR seeding** on top of the runtime's signal bus so
state composes across components without re-rendering dependents on the server.

It exists to close four gaps in the raw signal bus: no SSR seeding (the store
started empty, so `getSignal` returned `undefined` until the first
interaction), stringly-typed global names, no bridge between server islands and
client consumers, and no client-side computed values.

## The model: producer → signal → consumers

One **producer** (an island/widget or a screen loader) owns a value. Many
**consumers** (pure presentational components) bind to it read-only. When the
producer updates the value, the change goes out through a single signal to every
consumer **client-side**; no server round-trip per consumer.

```go
var Org = store.New("org")
var CompanyName = Org.String("companyName", "Acme Corp").Global()

// PRODUCER: resolve the per-request value in Load(ctx), publish edits.
func (s *SettingsScreen) Load(ctx context.Context) error {
    CompanyName.Seed(ctx, s.tenant.Name)
    return nil
}
editBtn := interactive.OnClick(ui.Button("Rename"),
    CompanyName.Publish(interactive.Post("/island/org/rename")))

// CONSUMERS: pure presentational, anywhere; attr + initial value from one source.
header := CompanyName.Bind(ctx, "span", map[string]string{"class": "site-name"})
title  := CompanyName.BindAttr(ctx, "a", "title", map[string]string{"href": "/"})
```

`Bind` requires `ctx` and is called from a component's `RenderCtx(ctx)` (the
ctx-aware render interface) so it stamps the **resolved** value: the per-request
value if a producer seeded one, else the declared default. The same resolved
value goes into the SSR seed, so the DOM and the client store can never drift.

## Declaring slices

```go
s := store.New("cart")            // namespace; "" for no prefix
count := s.Int("count", 0)        // cart.count
name  := s.String("label", "")    // cart.label
open  := s.Bool("open", false)
items := store.JSON[[]Item](s, "items", nil) // generic; free function

count.Global()                    // app-global: seeds every page, survives SPA nav
```

Names allow letters, digits, `.`, `_`, `-`. Re-declaring the same name with a
different default panics; declaring it identically is idempotent.

## Scope and seeding

- **Page-scoped** (default): seeded only on pages whose HTML references the
  slice. Reset to the page's value on every navigation.
- **App-global** (`.Global()`): seeded on every page and **preserved across
  client-side navigation**; a value the user mutated (cart count) survives.

The host emits one inert `<script type="application/json" id="gofastr-signals">`
island; the runtime seeds `_signals` before hydration. SPA-nav partials carry a
scope-split `#gofastr-signals-partial` island that merges without clobbering
mutated globals. Static export (SSG) emits the same block.

## Computed (derived) values

```go
var Greeting = store.Computed[string](Org, "greeting", "greet", "org.companyName")
greetEl := Greeting.Bind(ctx, "h1", nil)
```

`Computed` recomputes client-side when any dependency signal changes, by running
the JS reducer registered under its name. Register reducers as real functions,
**no `eval`, CSP-safe**:

```js
// shipped via WithExtraScripts, loaded AFTER runtime.js
(window.__gofastr._reducers = window.__gofastr._reducers || {}).greet =
    (v) => 'Hello ' + v['org.companyName'];
```

> Reducers must load **after** `runtime.js`: the runtime assigns the
> `window.__gofastr` namespace wholesale on boot, which would wipe a
> `_reducers` map set before it.

## Browser-persisted slices

A signal is a UI projection and the server is where truth lives. Some
state has no server to live on: a draft, a filter set, a scratch list a
signed-out visitor builds in one browser. `Persist` lets the browser
remember such a slice without an app leaving the framework to write a
document script.

<!-- gofastr:compile
import "context"
import "github.com/DonaldMurillo/gofastr/core-ui/store"

type Team struct{ Name string }
-->
```go
TB := store.New("teambuilder")

// 64 KiB cap (PersistDefaultMaxBytes), or name your own.
Teams := store.JSON[[]Team](TB, "teams", nil).Persist()
Filter := TB.String("filter", "").PersistMax(2048)

// Read and write them like any other slice: Bind, Publish,
// __gofastr.setSignal / getSignal.
ctx := context.Background()
_ = Teams.Bind(ctx, "ol", map[string]string{"class": "teams"})
_ = Filter.Bind(ctx, "span", nil)
```

Every binding a persisted slice renders carries
`data-fui-signal-persist="<cap>"`, and that marker is what loads the
runtime half, so a persisted slice is restored on the screens that
bind it, exactly as a page-scoped slice is seeded on the screens that
reference it.

### The contract, in four promises and no more

- **Best-effort.** Private mode, a blocked origin, a full quota and a
  cleared store are all normal. A read that fails leaves the server's
  value in place; a write that fails raises `gofastr:persist-overflow`
  on `window` (`{name, reason, size, max}`, `reason` one of `size`,
  `quota`, `encode`, `unavailable`, `untrusted`) and changes nothing
  else. `untrusted` means the signal held a value the runtime marked as
  not page-authored (a `?query` seed, for instance), which the store
  never keeps. `size` is counted in UTF-8 bytes of the JSON text, the
  unit `PersistMax` is declared in. **Never persist something whose loss
  is a bug.**
- **Namespaced.** Storage is the runtime's `local` primitive
  (IndexedDB, with a tiny-value `localStorage` fallback), keyed
  `gofastr.state.` + the component-encoded slice name, and the slice
  name carries its `Store` namespace, so `Teams` above is
  `gofastr.state.teambuilder.teams`. An app cannot choose the raw key;
  `core-ui/check`'s storage-key lint refuses one.
- **Size-bounded.** A value over the slice's cap is not written and the
  page is told instead, so an app can say "you have run out of room"
  rather than lose the write silently. `PersistMax` accepts up to 1 MiB:
  a signal is re-serialised on every change, so large data
  wants `__gofastr.local` directly, or the server.
- **Invisible to the server.** Nothing is posted, no cookie is set, no
  header is added, and the restore lands AFTER hydration, so first paint
  always shows the server's seed. When a Go render must know a
  browser-held value at first paint, use the cookie mirror `ui.Banner`
  uses (`framework/ui/banner.go` reads it with
  `app.RequestFromContext`); that channel is sized for a bit, not a
  blob.

`Persist` implies `.Global()`: the browser's value has to survive a
client-side navigation, and the app-global merge rule ("seed a global
only the first time it is seen") is what keeps a partial render from
clobbering it. Two open tabs of the same origin converge: the
primitive mirrors one tab's write into the other.

It does **not** make GoFastr offline-first (an explicit
[non-goal](ui-capability-map.md)): there is no conflict resolution, no
queue of pending mutations and no sync. It is a browser remembering a
projection.

One slice stops being enough at named collections, a schema version
with migrations, a key field, a filtered list, or a value the server
reads at render or action time or pushes back. The next step is then
[`framework/local`](local-state.md), which builds on the same primitive
and adds the explicit bridges. Its `SeedSignal` is the collection-backed
cousin of `Persist`: a slice cannot be both.

## Retrofitted components

`ui.Counter`, `ui.Tabs`, and `ui.SignalToggle` accept a typed `Slice` (their
`Slice` field) in addition to the legacy `SignalName` string. With a slice they
derive the signal name and stamp the slice's declared default, one source of
truth instead of a hardcoded initial value:

<!-- gofastr:compile
import "github.com/DonaldMurillo/gofastr/framework/ui"
import "github.com/DonaldMurillo/gofastr/core-ui/store"
-->
```go
ui.Counter(ui.CounterConfig{Slice: store.New("cart").Int("count", 0)})
```

## XSS notes

- The seed island is inert `application/json` parsed via `JSON.parse`; values
  are double-escaped (`json.Marshal` HTML-escaping + `</`→`<\/`).
- `Bind` (text mode) HTML-escapes the value. `BindHTML` writes to `innerHTML`:
  **trusted values only**.
- URL-bearing attributes bound via `BindAttr` keep the runtime's
  `javascript:`/`data:` scheme guard.

## See also

- [UI capability map](ui-capability-map.md) shows when a local signal, typed store, server recomputation, or durable database state is the right boundary.
- [Interactive patterns](interactive-patterns.md) covers RPC producers that publish authoritative values and fragments.
- [Runtime contract](runtime-contract.md) defines seeding and SPA-navigation rules, and documents the `local` browser-store primitive `Persist` is built on.
- [Local state](local-state.md) is the declared, collection-shaped layer above the same primitive, with the bridges to Go screens.

## Common mistakes

- **Re-declaring a slice name with a different default.** Panics at
  declaration time: two producers must not claim one name with
  different values. Declare the slice once in a shared package and
  import it from both sides; identical re-declaration is idempotent
  and fine.
- **Expecting a page-scoped slice to survive navigation.** Page-scoped
  is the default and resets to the page's value on every nav. State
  the user mutates and carries across pages (cart count, theme) needs
  `.Global()`.
- **Loading computed reducers before `runtime.js`.** The runtime
  assigns the whole `window.__gofastr` namespace on boot, wiping any
  `_reducers` map registered earlier. Ship reducers via
  `WithExtraScripts` so they load after the runtime.
- **Calling `Bind` outside a ctx-aware render.** `Bind(ctx, …)` stamps
  the *resolved* per-request value: the one a producer seeded in
  `Load(ctx)`. Render from `RenderCtx(ctx)` with the request context;
  a background/stub context stamps only the declared default and the
  SSR output diverges from what the producer intended.
- **Using `BindHTML` for user-influenced values.** It writes to
  `innerHTML`: trusted values only. `Bind` (text mode) escapes;
  reach for it unless you control every byte of the value.
- **Treating a persisted slice as durable.** `Persist` is best-effort
  by contract: a browser may refuse to store anything at all, and a
  user may clear it between two page loads. Business truth still
  belongs on the server.
- **Expecting a persisted value at first paint.** The restore is
  asynchronous and runs after hydration, so SSR always renders the
  server's seed. A screen that must not flash needs the value on the
  request, the cookie mirror `ui.Banner` uses, not in the browser
  store.
- **Persisting a slice the page never binds.** The marker rides on the
  bindings, exactly as the seed rides on references: a persisted slice
  no element on the page binds is never restored. Bind it, even to a
  hidden anchor, on the screens that need it.
