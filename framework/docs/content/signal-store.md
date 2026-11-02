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
That holds for an element id inside a seeded value too: HTML a producer renders
during the page (a section whose id the page derives, `features-2`) is seeded
with the id the page's markup shows, never the placeholder the id stood in as
while the page rendered (`render.SettleIDs`).

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

Beside the seed, the host emits `<script type="application/json"
id="gofastr-signals-page">` listing the page's page-scoped signal names
(omitted when there are none). A navigation that swaps in a whole
document, and Back or Forward to the first page loaded, reset those
names to that document's seed and leave globals alone, so going back to
page A shows A's values rather than the ones page B left behind. Back or
Forward to any cached page, on a layout with outlets too, restores the
values that page arrived with.

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

Computed slices may read other computed slices, but not in a circle.
`Computed` panics when a declaration closes a cycle (`a` reads `b`, `b`
reads `a`, or a slice listing itself) and names the path:
`store: computed "c.b" has a dependency cycle: c.b -> c.a -> c.b`. The
runtime also never re-runs a node inside its own recompute, so a cycle in
hand-written `data-cui-computed-deps` stops when it comes back round to a
node still recomputing, instead of recursing until the stack overflows.

## Retrofitted components

`ui.Counter`, `ui.Tabs`, and `ui.SignalToggle` accept a typed `Slice` (their
`Slice` field) in addition to the legacy `SignalName` string. With a slice they
derive the signal name and stamp the slice's value for the request on their
`Ctx`: what a producer passed to `Seed`, else the declared default. That is
the value the page's seed carries, so a counter seeded to 7 renders 7 on the
server (and without script) and its first + makes it 8. Pass the request
context, or the component stamps the default:

<!-- gofastr:compile
import "context"
import "github.com/DonaldMurillo/gofastr/framework/ui"
import "github.com/DonaldMurillo/gofastr/core-ui/store"
var ctx context.Context
-->
```go
var cartCount = store.New("cart").Int("count", 0)

cartCount.Seed(ctx, 7) // in the screen's Load(ctx)
ui.Counter(ui.CounterConfig{Slice: cartCount, Ctx: ctx}) // renders 7
```

A component of your own that renders its own `data-cui-signal` binding
reads the same value with `slice.Value(ctx)`. `Default()` is the
declaration and never sees a seed, so stamping it shows the wrong value
on any request that seeded another.

## Attribute bindings

`BindAttr` writes the slice value into the named attribute, at the server
render and on every update. The boolean attributes `disabled`, `hidden`,
`checked`, `open` and `selected` are on whenever present, so a false value
(`false`, `null`, `""`, `0`, `"false"`) removes the attribute and any other
value sets it. The server render omits a false one and writes a true one
with an empty value (`disabled=""`), so the page never arrives with
`disabled="false"`. On update, `checked` and `selected` also write the live
property (`disabled`, `hidden` and `open` reflect the attribute), so a
checkbox the reader already clicked still follows the signal. `value`
writes the live `el.value` as well as the attribute. Other attributes,
`aria-*` included, get the value as text.

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
- [Runtime contract](runtime-contract.md) defines seeding and SPA-navigation rules.

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
