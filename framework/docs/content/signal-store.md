# Signal store (shared client state)

`core-ui/store` adds Go typing, namespacing, SSR seeding, and CSP-safe computed values on top of the runtime's signal bus. One producer (a screen `Load` or island RPC) updates a slice; every bound consumer on the page updates client-side with zero extra server round-trips.

## Producer → signal → consumers

```go
var Org = store.New("org")
var CompanyName = Org.String("companyName", "Acme Corp").Global()

// Producer: seed per-request value in Load(ctx) and publish updates from RPCs.
func (s *SettingsScreen) Load(ctx context.Context) error {
    CompanyName.Seed(ctx, s.tenant.Name)
    return nil
}
editBtn := interactive.OnClick(ui.Button(ui.ButtonConfig{Label: "Rename"}),
    CompanyName.Publish(interactive.Post("/island/org/rename")))

// Consumers: bind inside RenderCtx(ctx) so SSR stamps the seeded value.
header := CompanyName.Bind(ctx, "span", map[string]string{"class": "site-name"})
title  := CompanyName.BindAttr(ctx, "a", "title", map[string]string{"href": "/"})
```

## Declaring slices & scope

```go
s := store.New("cart")
count := s.Int("count", 0)                   // cart.count
label := s.String("label", "")               // cart.label
open  := s.Bool("open", false)               // cart.open
items := store.JSON[[]Item](s, "items", nil) // generic free function

count.Global() // preserve across SPA navigations
```

- Names allow letters, digits, `.`, `_`, `-`. Re-declaring a name with a different default panics; identical re-declaration is idempotent.
- **Page-scoped (default):** Seeded only on pages that reference the slice; resets on navigation and restores on Back/Forward.
- **App-global (`.Global()`):** Seeded on every page and preserved across client-side navigations (for cart counts, org name, UI state).

The host emits seeds in an inert `<script type="application/json" id="gofastr-signals">` block (and `#gofastr-signals-partial` on SPA swaps) before hydration so SSR DOM and client signals never drift.

## Using slices with components & attributes

`ui.Counter`, `ui.Tabs`, and `ui.SignalToggle` accept a `Slice` field alongside `Ctx` so SSR renders the seeded value:

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

Read the resolved per-request value in custom components with `slice.Value(ctx)` (not `Default()`).

For `BindAttr`: boolean HTML attributes (`disabled`, `hidden`, `checked`, `open`, `selected`) are omitted/removed for falsy values (`false`, `null`, `""`, `0`, `"false"`) and set with live DOM property sync for `checked`, `selected`, and `value`. URL-bearing attributes enforce `javascript:`/`data:` scheme blocking.

## Computed (derived) values

Derive client-side values from other slices with `store.Computed` (cycles panic at declaration time):

```go
var Greeting = store.Computed[string](Org, "greeting", "greet", "org.companyName")
greetEl := Greeting.Bind(ctx, "h1", nil)
```

Register the reducer as a real JS function (**no `eval`, CSP-safe**) via `WithExtraScripts` so it loads **after** `runtime.js`:

```js
(window.__gofastr._reducers = window.__gofastr._reducers || {}).greet =
    (v) => 'Hello ' + v['org.companyName'];
```

## XSS safety

- Seed JSON is double-escaped inside inert `application/json` script tags.
- `Bind` (text mode) HTML-escapes values automatically.
- `BindHTML` writes `innerHTML`—use **only** for trusted server-rendered markup, never user input.

## Common mistakes

- **Re-declaring a slice name with a different default.** Declare shared slices once in a common package and import them.
- **Expecting page-scoped slices to survive navigation.** Call `.Global()` for state that persists across routes.
- **Loading computed reducers before `runtime.js`.** `runtime.js` initializes `window.__gofastr` on boot and overwrites earlier `_reducers` maps; ship reducers via `WithExtraScripts`.
- **Calling `Bind(ctx, ...)` outside `RenderCtx(ctx)`.** Passing `context.Background()` stamps the default instead of the seeded request value.
- **Using `BindHTML` with user-controlled strings.** Always use `Bind` (text mode) unless you control every byte of the HTML.
