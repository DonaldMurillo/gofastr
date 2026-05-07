# core-ui — Framework Design Document

> A Go-native UI framework that thinks like mobile, renders to the web, and is built for AI to author.
> Part of GoFastr. Inspired by SwiftUI/Flutter's composability, Angular's incremental hydration, and SolidJS's fine-grained reactivity.

---

## Core Philosophy

1. **AI-authorable** — AI can generate, read, modify, and visually reason about UI code without seeing the screen.
2. **Mobile-inspired** — Screens, widgets, layouts. Not pages and divs. Semantic hierarchy, not free-form HTML.
3. **Accessible by default** — ARIA landmarks, roles, keyboard nav baked into primitives. You can't build inaccessible UI.
4. **Progressive performance** — Ship CSS and JS only when needed. Preload based on user journey. Accumulate, never unload.
5. **Go-idiomatic** — Valid Go. No new syntax. `.ui.go` convention like `_test.go`. Linter enforces the restricted subset.

---

## Architecture Overview

```
┌──────────────────────────────────────────────────────────┐
│  App                                                      │
│  ├── DI Container (global providers, theme, config)       │
│  ├── Router (code-based route → screen mapping)           │
│  └── Layouts (shared chrome: nav, header, footer)         │
│       ├── Screen [/users]         ← full page view        │
│       ├── Screen [/users/:id]     ← full page view        │
│       ├── Drawer [filters]        ← side panel            │
│       ├── Sheet [cart]            ← bottom panel          │
│       └── Dialog [confirm]        ← modal overlay         │
│            └── Widget             ← self-contained unit   │
│                 └── Component     ← reusable piece        │
│                      └── Element  ← atomic (Button, etc)  │
└──────────────────────────────────────────────────────────┘
```

### Hierarchy Roles

| Level | Responsibility | ADA Role | Hydrates? |
|---|---|---|---|
| **App** | DI container, routing, theme | `<html lang>` | No |
| **Layout** | Shared chrome (nav, header, footer) | Landmarks (banner, navigation, contentinfo) | No |
| **Screen** | Top-level view. Renders as page, drawer, sheet, or dialog depending on context | `<main>` / `role="dialog"` / `role="region"` | On activation |
| **Widget** | Self-contained interactive unit with own signals | `role` per widget type | On first interaction (incremental hydration) |
| **Component** | Reusable composable piece | Semantic element | Inherits from parent |
| **Element** | Atomic primitive — Button, Input, Text, Heading | Correct ARIA per element type | N/A (handled by parent widget) |

---

## Component Model

### Three-part components: State, Render, Actions

Every interactive unit has three things:

```go
// counter.ui.go — valid Go, linter enforces restricted subset

// STATE — struct fields are reactive state
type Counter struct {
    Count int
    Label string
}

// RENDER — describes the HTML. Semantic primitives, ADA by default.
func (c Counter) Render() HTML {
    return Article(nil,
        Heading(3, c.Label),
        Group(RoleStatus, Attrs{"aria-live": "polite"},
            Text(fmt.Sprintf("%d", c.Count)),
        ),
        ButtonGroup(
            Button("Decrement", OnClick("decrement"), Secondary),
            Button("Increment", OnClick("increment"), Primary),
        ),
    )
}

// ACTIONS — event handlers. Framework infers client vs server.
func (c *Counter) Actions() {
    On("increment", func() {
        c.Count++     // pure local mutation → compiles to JS, runs in browser
    })
    On("decrement", func() {
        c.Count--     // pure local mutation → compiles to JS, runs in browser
    })
}
```

### Client vs Server Inference

The framework analyzes action handlers at build time:

| Handler behavior | Inference | Runtime |
|---|---|---|
| Only reads/writes local struct fields | **Client** | Compiles to JS, runs in browser |
| Calls `Server()` | **Hybrid** | Client action triggers server, response streams via SSE |
| Accesses DB, network, filesystem, or injected services | **Server** | Runs as goroutine, streams updates via SSE |
| Creates goroutines, channels, or uses `reflect` | **Compile error** | `.ui.go` linter rejects it |

```go
// Example: hybrid — local state + server offload
func (c *ProductCard) Actions() {
    On("add-to-cart", func() {
        c.InCart = true                         // local state → JS
        c.Count = c.Count + 1                   // local state → JS
        Server("add-to-cart", c.Product.ID)     // offload to server → SSE island
    })
}
```

### The `Server()` escape hatch

When a handler needs real backend work, `Server()` marks it:

```go
Server(event string, args ...any)
```

This tells the framework:
1. Send this event + args to the server
2. Server runs the handler in a goroutine
3. Server streams updated HTML back via SSE
4. The component becomes a live island from this point forward

---

## Reactive Signals

Fine-grained reactivity inspired by Angular signals / SolidJS.

```go
// Signal primitives
type Signal[T any] struct { /* internal */ }

func NewSignal[T any](initial T) *Signal[T]
func (s *Signal[T]) Get() T
func (s *Signal[T]) Set(v T)
func (s *Signal[T]) Update(fn func(T) T)  // compute new from old

func Computed[T any](fn func() T) *Signal[T]  // auto-tracks dependencies
func Effect(fn func())                          // runs when tracked signals change
```

### Signal usage in components

```go
type ProductFilters struct {
    Search  Signal[string]
    Category Signal[string]
    Results  Computed[[]Product]  // derived from Search + Category
}

func (f *ProductFilters) Render() HTML {
    return Form(nil,
        Input(Text, "search",
            Placeholder("Search products..."),
            Bind(&f.Search),  // two-way binding
        ),
        Select("category",
            Options("All", "Electronics", "Books", "Clothing"),
            Bind(&f.Category),
        ),
    )
}

func (f *ProductFilters) Actions() {
    // Results auto-updates when Search or Category changes
    // This runs on server because it needs to query data
    Effect(func() {
        f.Results = Compute(func() []Product {
            return db.SearchProducts(f.Search.Get(), f.Category.Get())
        })
    })
}
```

### Dependency injection

Global services, provided at the App level, injectable into any component:

```go
// main.go
app := NewApp(
    Provide(NewCartService),       // singleton
    Provide(NewAuthService),       // singleton
    Provide(NewAnalyticsService),  // singleton
    Theme(DefaultTheme),
)

// In any component — inject via struct embedding or fields
type ProductCard struct {
    Product  Product
    Cart     *CartService    `inject:""`  // auto-injected
    Analytics *AnalyticsService `inject:""`
}
```

---

## App & Routing

Code-based, compositional. The app is declared as a tree of intentions:

```go
app := NewApp(
    Provide(NewCartService),
    Provide(NewAuthService),
    Theme(DefaultTheme),
)

// Layouts — shared chrome
admin := Layout("admin",
    Sidebar(AdminNav),
    Header(UserMenu),
)

public := Layout("public",
    Header(PublicNav),
    Footer(PublicFooter),
)

// Screens — top-level views, rendered inside a layout
app.Screen("/users", admin, &UserListScreen{})
app.Screen("/users/new", admin, &UserFormScreen{})
app.Screen("/users/:id", admin, &UserDetailScreen{})

// Drawers — side panels
app.Drawer("filters", admin, &FilterDrawer{})

// Sheets — bottom panels
app.Sheet("cart", public, &CartSheet{})

// Dialogs — modal overlays
app.Dialog("confirm-delete", &ConfirmDialog{})
```

### Screen types

| Type | Rendering | ARIA | Hydration |
|---|---|---|---|
| **Screen** | Full page or route-based | `<main>` landmark | On page load |
| **Drawer** | Side panel, slides in | `role="dialog"` aria-modal | On activation |
| **Sheet** | Bottom panel, slides up | `role="dialog"` aria-modal | On activation |
| **Dialog** | Modal overlay | `role="dialog"`, focus trap, escape close | On activation |

Same component definition can be mounted as any screen type. The layout decides the presentation.

---

## The `.ui.go` Convention

### Why `.ui.go`?

- Valid Go — `gofmt`, `go vet`, IDE highlighting, autocomplete all work
- Like `_test.go` — same language, special treatment by tooling
- Clear compilation boundary — the build system knows these files compile to JS
- No new syntax to learn, no new parser to write

### Linter rules for `.ui.go`

A custom linter (`core-ui check`) enforces the restricted subset:

**Allowed:**
- Struct field read/write
- Basic types (string, int, bool, float, slices, maps)
- Control flow (if, for, switch, range)
- String formatting (`fmt.Sprintf`)
- Framework primitives (`Signal`, `Computed`, `Effect`, `On`, `Server`, `Render`, `Bind`)
- Range over slices/maps
- Function literals (closures for event handlers)
- Calling other `.ui.go` component Render methods
- Calling framework Element constructors (Button, Heading, etc.)

**Forbidden:**
- `go` keyword (goroutines)
- `chan` (channels)
- `interface{}` type assertions
- `reflect` package
- `net/http`, `database/*`, `os/*`, any I/O
- `time.Sleep`
- Method calls on non-framework types (except local struct methods)
- Direct pointer arithmetic
- Importing non-framework packages (except `fmt`, `strings`)

### Build pipeline

```
counter.ui.go
    │
    ├── go vet, gofmt ──→ ✅ valid Go
    │
    ├── core-ui check ──→ ✅ passes linter (restricted subset)
    │
    ├── go build ───────→ counter.o (server-side rendering, SSR/SSG)
    │
    └── core-ui compile ─→ counter.behavior.js (client-side hydration)
                          counter.styles.css (extracted styles)
```

The same `.ui.go` file produces:
1. **Go object code** — used by the server for SSR/SSG rendering
2. **JS behavior module** — the compiled client-side hydration code
3. **CSS chunk** — extracted styles for this component

---

## Accessibility (ADA) by Default

Every primitive has correct semantics baked in. The AI can't build inaccessible UI because the primitives won't let it.

### Elements produce correct markup

```go
// What you write
Button("Save", OnClick("save"), Primary)

// What renders (framework handles this)
<button type="button" class="btn btn-primary" onclick="..." aria-label="Save">Save</button>

// What you write
Heading(2, "User Details")

// What renders
<h2 id="heading-user-details">User Details</h2>
// ID auto-generated for aria-labelledby references
```

### Layout landmarks are automatic

```go
Layout("admin",
    Sidebar(AdminNav),     // → <nav aria-label="Admin navigation">
    Header(UserMenu),      // → <header role="banner">
    Footer(Acknowledgments), // → <footer role="contentinfo">
)
// Screen content → <main id="main-content" role="main">
// Skip-nav link auto-injected: <a href="#main-content" class="skip-link">Skip to main content</a>
```

### Widget-level accessibility

```go
// Dialog — framework handles focus trap, escape close, aria-modal
Dialog("confirm-delete", &ConfirmDialog{})
// → <dialog open aria-modal="true" aria-labelledby="heading-confirm-delete">
// → Focus trapped inside, Escape closes, Tab cycles within

// Live regions — framework marks dynamic content
Group(RoleStatus, Attrs{"aria-live": "polite"},
    Text(cartCount),
)
// → <div role="status" aria-live="polite">3 items</div>
```

---

## Styling System

### Token-based theming + utility classes

```go
// theme.config.go
var DefaultTheme = Theme{
    Colors: Colors{
        "primary":    "#4F46E5",
        "secondary":  "#6B7280",
        "danger":     "#EF4444",
        "success":    "#10B981",
        "surface":    "#FFFFFF",
        "background": "#F9FAFB",
        "text":       "#1F2937",
        "text-muted": "#6B7280",
    },
    Spacing: Spacing{
        "xs": 2, "sm": 4, "md": 8, "lg": 16, "xl": 24, "2xl": 32,
    },
    Radii: Radii{
        "sm": 4, "md": 8, "lg": 12, "full": 9999,
    },
    Fonts: Fonts{
        "body":    "'Inter', system-ui, sans-serif",
        "heading": "'Inter', system-ui, sans-serif",
        "mono":    "'JetBrains Mono', monospace",
    },
    Breakpoints: Breakpoints{
        "sm": 640, "md": 768, "lg": 1024, "xl": 1280,
    },
}
```

### Utility classes in Go

```go
// Semantic style references — maps to token values
func (c ProductCard) Render() HTML {
    return Card(
        Use("card"),  // applies card styles from theme
        Image(c.Product.Image, c.Product.Name),
        Heading(4, c.Product.Name),
        Text(c.Product.Price),
        Button("Add to Cart", OnClick("add"), Primary),
    )
}

// Or inline utilities (Tailwind-like, but generated from tokens)
Div(
    Classes{
        "flex":     true,
        "gap-{md}": true,    // resolves to gap-8 from token
        "p-{lg}":   true,    // resolves to p-16 from token
    },
    children...,
)
```

### Progressive CSS Loading

CSS is loaded progressively based on the user journey. Never unloaded.

```
Build time:
  1. Scan all components → map component → required CSS classes
  2. Scan all screens → map screen → components used
  3. Analyze routes → build screen transition graph (user journey)
  4. Generate CSS chunks per screen + preload hints for adjacent screens

Runtime:
  Initial load:    Ship CSS for current screen's components (critical path)
  Navigation:      Load CSS for next screen (preload started on previous screen)
  Hydration:       Load CSS for newly hydrated widget (if not already loaded)
  Caching:         Browser cache. Once loaded, stays loaded. No unload.
```

```
User visits /products:
  Ship: base.css + nav.css + grid.css + card.css + filters.css = 6KB
  Preload: gallery.css + tabs.css (next likely screen: /products/:id)

User navigates to /products/:id:
  Ship: gallery.css + tabs.css (already preloaded = instant)
  Preload: cart.css + form.css (next likely: /cart)

User navigates to /cart:
  Ship: cart.css (already preloaded = instant)
  No new preloads (checkout reuses form.css already loaded)
```

---

## Progressive JS Hydration

### Incremental hydration (Angular-inspired)

The HTML is pre-rendered (SSG or SSR). JS interactivity is activated on demand:

1. **Page load** — HTML is visible immediately. No JS blocking render.
2. **First interaction** — User clicks a widget. Framework hydrates JUST that widget.
3. **After hydration** — Widget runs locally (client-compiled actions) or as island (server goroutine).
4. **Pre-hydration** — Framework can preload JS for widgets likely to be interacted with next.

### JS runtime budget

| Component | Size | Loads when |
|---|---|---|
| Core runtime | ~3KB | First page load |
| Component behavior (avg) | ~0.5KB each | First interaction with widget |
| Preloaded behaviors | ~0.3KB each | <link rel="preload"> for likely interactions |

### Hydration flow

```
1. Server renders full HTML page (SSG at build time, or SSR per request)
   → <div data-widget="product-card" data-id="42">
        <img src="..." alt="Product Name" />
        <h4>Product Name</h4>
        <button data-action="add-to-cart">Add to Cart</button>
      </div>

2. Page loads. HTML is visible. No JS executed yet.

3. User clicks "Add to Cart"
   → Core runtime intercepts click (event delegation)
   → Fetches product-card.behavior.js (if not already loaded)
   → Hydrates widget: attaches event listeners, initializes local state
   → Executes action handler locally OR sends to server via Server()

4. From now on, this widget is interactive.
   → Client actions run in browser (no round-trip)
   → Server actions stream updates via SSE
```

---

## SSG / SSR / Live Modes

The same component definitions work in all modes. The framework decides delivery.

### SSG — Static Site Generation

```go
// Build time: renders HTML, outputs to /dist/
app.Static("/", &HomeScreen{})
app.Static("/about", &AboutScreen{})
app.Static("/products", &ProductListScreen{})  // static list page
// Product detail pages generated from data
app.Static("/products/:id", &ProductDetailScreen{}, FromData(loadProducts))
```

### SSR — Server-Side Rendering

```go
// Per request: renders HTML, sends response
app.Screen("/users/:id", admin, &UserDetailScreen{})
// → GET /users/42 → server renders UserDetailScreen with user data → HTML response
```

### Live — Server-Driven Island

```go
// Any component with Server() calls becomes a live island when hydrated
// Server spawns a goroutine per connected component
func (c *LiveCounter) Actions() {
    On("increment", func() {
        c.Count++
        analytics.Track("increment", c.Count)  // external call → server action
        Server("sync-count", c.Count)           // → SSE stream to client
    })
}
```

---

## Build Tools

### `core-ui check` — Linter

Validates `.ui.go` files against the restricted subset. Integrates with `go vet`.

```bash
core-ui check ./...
# output:
# components/cart.ui.go:42: goroutines not allowed in .ui.go files
# components/cart.ui.go:55: import "database/sql" not allowed in .ui.go files
```

### `core-ui compile` — Go → JS compiler

Compiles `.ui.go` action handlers to JavaScript. Only handles the restricted subset.

```bash
core-ui compile ./...
# For each .ui.go file with Actions():
#   → dist/js/product-card.behavior.js
#   → dist/css/product-card.styles.css
```

### `core-ui build` — Full build

SSG + compile + CSS extraction in one step.

```bash
core-ui build
# 1. Renders SSG pages → dist/
# 2. Compiles .ui.go → dist/js/*.behavior.js
# 3. Extracts CSS → dist/css/*.css
# 4. Generates preload manifests based on route graph
# 5. Generates service worker for offline caching (optional)
```

---

## Summary: What Makes This Different

| Dimension | React/Angular/Svelte | core-ui |
|---|---|---|
| **Language** | JS/TS | Go (valid Go, `.ui.go` subset) |
| **Mental model** | Web pages with components | Mobile app with screens, widgets |
| **Accessibility** | Opt-in (eslint-plugin-jsx-a11y) | Impossible to opt out (primitives enforce it) |
| **Hydration** | Full page or manual `lazy()` | Incremental, interaction-triggered, inferred |
| **Client/Server split** | Developer decides (Next.js RSC) | Framework infers from handler purity |
| **CSS loading** | All upfront (Tailwind) or per-route | Journey-aware progressive loading |
| **State** | External library (Redux, NgRx) | Built-in signals + DI |
| **AI authoring** | AI generates broken JSX/CSS | Restricted subset + semantic primitives = correct by construction |
| **SSG/SSR/Live** | Different frameworks for each | Same code, different delivery mode |

---

## Open Questions & Future Exploration

1. **Go→JS compiler scope** — Exactly which Go expressions to support in the restricted subset? Need a formal spec.
2. **Signal thread safety** — How do concurrent goroutines update signals safely? Mutex per signal? Lock-free?
3. **Widget hydration boundary** — Is the widget always the hydration unit? Or can sub-components hydrate independently?
4. **Testing** — How to test `.ui.go` components? Snapshot testing of rendered HTML? Visual regression?
5. **Error boundaries** — What happens when a server-side widget crashes? Error UI per widget?
6. **Animation** — Transition between screens, drawer open/close, sheet slide-up. CSS animations or framework-managed?
7. **Form handling** — Built-in form primitives with validation? Or keep it minimal and let widgets compose forms from elements?
8. **Code splitting** — How granular should JS chunks be? Per widget? Per screen? Per behavior?
9. **Offline / PWA** — Can SSG pages work offline? Service worker generation from route graph?
10. **Editor tooling** — VSCode extension for `.ui.go`? Syntax highlighting is free (it's Go), but linter integration, autocomplete for framework primitives?
