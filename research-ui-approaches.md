# GoFastr UI — Architecture Research

> How should we build `ui-core/` + `ui-framework/` for server-rendered HTML?
> Same philosophy as `core/` + `framework/`: powerful primitives at the bottom, AI-fast prototyping on top.
> **Constraint**: Built from scratch. No htmx. No existing Go template engines. No JS framework dependencies.

---

## Your Proposal: Typed HTML Builder with Declarative Pages

### Core idea

Extend what `core/render` already started. `HTML` type + `Tag()`/`Text()`/`Join()` are the primitives. Build `ui-core/` as a richer primitive layer (forms, tables, navigation, attribute builders). Then `ui-framework/` auto-generates pages from entity definitions.

### Architecture

```
ui-core/          ← Rich HTML primitive library
  html/             HTML builder (Tag, VoidTag, Attr, Attrs, Join, Raw, Text, If, Map)
  form/             Form primitives (Input, Select, Textarea, Checkbox, Radio, Fieldset, Label, ValidationErrors)
  table/            Table primitives (Table, Th, Td, Thead, Tbody, Pagination, SortHeaders)
  nav/              Navigation (Navbar, Sidebar, Breadcrumbs, Tabs, Link, ActiveLink)
  page/             Page structure (Document, Head, Meta, Script, Stylesheet, Body, Hero, Section, Grid)
  components/       Reusable component system (Component[T], Slot, ForEach, Conditional)
  css/              CSS utility builder (Class, Style, Responsive, DarkMode tokens)
  js/               Minimal JS generation (Toggle, Submit, Navigate, Confirm, Validate)
  partial/          Partial rendering (render just a component, not the full page)

ui-framework/     ← Entity-aware auto-UI
  admin/            Auto-generated admin panel from entity definitions
  crud-pages/       List/Create/Edit/Detail/Delete pages per entity
  forms/            Auto-generated forms from entity field schemas
  tables/           Auto-generated data tables with sort/filter/paginate
  dashboard/        Auto-generated dashboard from entity stats
  layout/           Default admin layout (sidebar nav, breadcrumbs, user menu)
```

### What a page looks like

```go
// ui-core level — hand-crafting with rich primitives
func UserListPage(users []User) render.HTML {
    return page.Document("Users",
        nav.Breadcrumb("Home", "Users"),
        page.Hero("User Management",
            html.A(html.Attrs{"href": "/users/new", "class": "btn btn-primary"}, html.Text("New User")),
        ),
        table.DataTable(users,
            table.Column("Name", func(u User) render.HTML { return html.Text(u.Name) }),
            table.Column("Email", func(u User) render.HTML { return html.Text(u.Email) }),
            table.Column("Role", func(u User) render.HTML { return badge.RoleBadge(u.Role) }),
            table.Column("Actions", func(u User) render.HTML {
                return html.Join(
                    html.A(html.Attrs{"href": "/users/"+u.ID+"/edit"}, html.Text("Edit")),
                    form.DeleteForm("/users/"+u.ID, "Delete"),
                )
            }),
        ),
    )
}

// ui-framework level — auto-generated from entity
app.Entity("users", framework.EntityConfig{
    Fields: []schema.Field{
        {Name: "name", Type: schema.String, Required: true},
        {Name: "email", Type: schema.String, Required: true, Unique: true},
        {Name: "role", Type: schema.Enum, Values: []string{"admin", "author", "reader"}},
    },
    UI: uiframework.AutoUI, // ← flips on auto admin pages
})
// → GET /admin/users        (list table with sort/filter/paginate)
// → GET /admin/users/new    (create form)
// → GET /admin/users/:id    (detail view)
// → GET /admin/users/:id/edit (edit form)
// → POST /admin/users       (handle create)
// → PUT /admin/users/:id    (handle update)
// → DELETE /admin/users/:id (handle delete)
```

### Strengths
- ✅ Extends what you already have (`core/render`)
- ✅ Go-idiomatic — everything is typed Go functions
- ✅ Compile-time safe — wrong HTML structure won't compile
- ✅ AI-friendly — easy to generate Go component functions
- ✅ Entity system already has field definitions → forms/tables map naturally

### Weaknesses
- ⚠️ Verbose — every HTML element is a Go function call
- ⚠️ No interactivity beyond forms (full page reloads for everything)
- ⚠️ CSS styling needs a strategy (inline? class-based? utility-gen?)
- ⚠️ JS interactions need to be hand-generated or minimal

### Interactivity model
Forms submit → full page reload. Links navigate → full page reload. That's it.
Can add `js.Toggle()` for show/hide, but fundamentally server-rendered multi-page app.

---

## Alternative 1: Virtual DOM Diff + Server-Sent Patches

### Core idea
Don't send full HTML pages. Send a *virtual DOM description* from Go, diff it on the client with a tiny runtime (~2KB JS), and apply only the patches. Think "server-as-react" but the server holds the state and computes the DOM.

### Architecture

```
ui-core/
  vdom/           Virtual DOM types (VNode, VElement, VText, VFragment, VAttribute)
  diff/           Diff algorithm (patch list generation)
  patch/          Patch types (Insert, Remove, Update, Reorder, SetAttr, RemoveAttr)
  serialize/      Wire format for VDOM (compact binary or JSON)
  mount/          Client mount point (where VDOM renders into real DOM)
  events/         Event delegation (click, input, submit → serialized → sent to server)

ui-framework/
  stateful/       Server-side component state management (per-session VDOM tree)
  binding/        Two-way data binding (form field changes → server state update → VDOM patch)
  entity-ui/      Auto VDOM generation from entity definitions
```

### What it looks like

```go
// Server holds state, computes VDOM, sends patches
func CounterPage(ctx *uicore.RenderContext) vdom.VNode {
    count := ctx.State("count", 0) // server-side per-session state
    
    return vdom.Div(nil,
        vdom.H1(nil, vdom.Text(fmt.Sprintf("Count: %d", count))),
        vdom.Button(vdom.On("click", func(ctx *uicore.EventContext) {
            ctx.SetState("count", count+1) // server updates state
            ctx.Rerender() // recompute VDOM, diff, send patches
        }), vdom.Text("+1")),
    )
}

// Entity auto-UI: VDOM generated from schema
app.Entity("users", framework.EntityConfig{
    Fields: []schema.Field{...},
    UI: uiframework.LiveUI, // live-updating VDOM admin
})
```

### Client runtime (tiny, ~2KB)
```javascript
// This is the ONLY JS we ship. ~2KB minified.
class GoFastrRuntime {
    connect() { /* WebSocket or SSE */ }
    applyPatch(patch) { /* DOM manipulation */ }
    delegateEvent(event) { /* serialize & send to server */ }
}
```

### Strengths
- ✅ True interactivity without writing JS
- ✅ Server is the source of truth (all state in Go)
- ✅ Tiny client runtime (~2KB vs React's 40KB)
- ✅ Only sends diffs → efficient after initial load
- ✅ Feels like a SPA but server-rendered

### Weaknesses
- ⚠️ Complex to build (diff algorithm, patch serialization, event delegation)
- ⚠️ Latency-sensitive — every interaction round-trips to server
- ⚠️ Server memory per session (VDOM tree per connected user)
- ⚠️ Harder to debug (diff bugs show as weird DOM states)
- ⚠️ Offline impossible — every interaction needs server

---

## Alternative 2: Islands Architecture with Progressive Hydration

### Core idea
Server renders full HTML pages. Sprinkle tiny "islands" of interactivity that the server can update independently via SSE/WebSocket. The page is mostly static HTML. Islands are dynamic zones identified by IDs.

### Architecture

```
ui-core/
  island/         Island primitive (named interactive zone within static HTML)
  stream/         SSE stream per island (server can push updates to specific islands)
 hydrate/         Client hydration for islands (attach event listeners, bind to stream)
  static/         Static HTML generation (the 90% of the page that doesn't change)
  slot/           Slot system (holes in static HTML where islands plug in)

ui-framework/
  entity-islands/  Auto-generate islands for entity tables/forms
  live-table/      Table island that auto-updates when data changes
  live-form/       Form island with validation feedback
  dashboard/       Dashboard composed of multiple islands
```

### What it looks like

```go
// Most of the page is static HTML, rendered once
func UserPage(user User) render.HTML {
    return page.Document(user.Name,
        nav.Sidebar("admin"),
        page.Section("User Details",
            // Static content — rendered once, never updates
            html.P(nil, html.Text("Email: "+user.Email)),
            
            // Island — this zone is live, server can push updates
            island.Live("user-status", user.ID, 
                html.Span(html.Attrs{"class": "badge"}, html.Text(user.Role)),
            ),
            
            // Another island — form with live validation
            island.Form("edit-user", "/users/"+user.ID,
                form.Input("name", user.Name, form.Validate.Required()),
                form.Input("email", user.Email, form.Validate.Email()),
                form.Submit("Save"),
            ),
        ),
    )
}

// Server pushes updates to specific islands
app.Subscribe("user.updated", func(ctx context.Context, user User) {
    island.Push("user-status", user.ID, func() render.HTML {
        return html.Span(html.Attrs{"class": "badge"}, html.Text(user.Role))
    })
})
```

### Client runtime (~1KB)
```javascript
// Connect to SSE stream, listen for island updates
class GoFastrIslands {
    connect() { new EventSource("/.islands/stream") }
    onIslandUpdate(id, html) { document.getElementById(id).innerHTML = html }
}
```

### Strengths
- ✅ Best of both worlds — static HTML for most of the page, live updates where needed
- ✅ Tiny client runtime (~1KB)
- ✅ Works great with existing `core/render` and `core/stream` (SSE)
- ✅ Progressive enhancement — works without JS (degrades to static)
- ✅ Memory efficient — only track state for active islands
- ✅ Feels natural with Go's concurrency (goroutine per island stream)

### Weaknesses
- ⚠️ Island boundaries need careful design
- ⚠️ Cross-island communication is awkward
- ⚠️ Initial page is full HTML (can be large for big tables)
- ⚠️ SSE reconnection logic needed for reliability

---

## Alternative 3: Reactive Signal Graph with Template Literals

### Core idea
Define a reactive signal graph on the server. Signals are typed values that automatically recompute dependents when changed. Templates are Go string-interpolated HTML with signal references. When a signal changes, the server recomputes the affected template and sends only the changed fragment.

### Architecture

```
ui-core/
  signal/         Reactive signal system (Signal[T], Computed[T], Effect)
  template/       String-interpolated templates with signal bindings
  bind/           Two-way binding helpers (form input ↔ signal)
  fragment/       Named HTML fragments (identifiable chunks server can replace)
  reactivity/     Dependency tracking (which signals affect which fragments)

ui-framework/
  entity-signals/  Auto-create signals from entity queries (live data)
  reactive-table/  Table that auto-updates when underlying data changes
  reactive-form/   Form that validates as you type
  query-signals/   Wrap DB queries in signals (auto-refresh on interval or event)
```

### What it looks like

```go
func CounterPage() render.HTML {
    // Declare signals
    count := signal.New(0)
    double := signal.Computed(func() int { return count.Get() * 2 })
    
    // Template with signal bindings — auto-updates when signals change
    return template.HTML(`
        <div>
            <h1>Count: {{.count}}</h1>
            <p>Double: {{.double}}</p>
            <button data-action="increment">+1</button>
        </div>
    `, template.Bind{
        "count":  count,
        "double": double,
    }, template.Actions{
        "increment": func() { count.Set(count.Get() + 1) },
    })
}

// Entity-level: wrap queries in signals
posts := querysignal.Live("SELECT * FROM posts WHERE status = 'published'", 5*time.Second)
// → auto-refreshes every 5s, pushes HTML fragments to connected clients
```

### Strengths
- ✅ Elegant mental model — reactive data flow
- ✅ Granular updates (only re-render affected fragments)
- ✅ Powerful for dashboards and live data views
- ✅ Entity queries as signals is a killer feature for admin panels

### Weaknesses
- ⚠️ Signal graph complexity — cycles, memory leaks, stale computations
- ⚠️ String-interpolated templates lose compile-time safety
- ⚠️ "Magnetic" — once you start, everything becomes reactive (good or bad?)
- ⚠️ Hard to debug (which signal caused this update?)
- ⚠️ Overkill for simple CRUD pages

---

## Alternative 4: Declarative UI Description Language (Go DSL → HTML)

### Core idea
Don't write HTML at all. Write Go DSL that describes UI intent, not markup. The DSL compiles to optimized HTML, CSS, and minimal JS. Think "SwiftUI for the web in Go." You say *what* the UI should look like, the framework decides the HTML.

### Architecture

```
ui-core/
  dsl/            Declarative DSL types (VStack, HStack, Text, Button, List, Form, Field, Nav)
  style/          Style system (tokens, themes, responsive rules, dark mode)
  layout/         Layout engine (flexbox/grid generation from DSL intent)
  render/         DSL → HTML compiler
  action/         Action system (Navigate, Submit, Confirm, Toggle, Validate)
  theme/          Theme engine (design tokens → CSS custom properties)

ui-framework/
  entity-dsl/     Auto-generate DSL from entity definitions
  scaffold/       Scaffold full pages from DSL descriptions
  preview/        Design-time preview (render DSL to HTML for dev)
```

### What it looks like

```go
func UserListPage(users []User) render.HTML {
    return dsl.Page(
        dsl.Title("Users"),
        dsl.Navbar(
            dsl.Brand("GoFastr Admin"),
            dsl.NavItem("Dashboard", "/admin"),
            dsl.NavItem("Users", "/admin/users", dsl.Active()),
        ),
        dsl.Content(
            dsl.Header("User Management",
                dsl.Action("New User", dsl.Navigate("/admin/users/new"), dsl.Primary()),
            ),
            dsl.Table(users).
                Column("Name", dsl.Text(func(u User) string { return u.Name })).
                Column("Email", dsl.Text(func(u User) string { return u.Email })).
                Column("Role", dsl.Badge(func(u User) (string, string) { return u.Role, roleColor(u.Role) })).
                Column("", dsl.Actions(
                    dsl.Action("Edit", dsl.Navigate("/admin/users/{{.ID}}/edit")),
                    dsl.Action("Delete", dsl.Submit("DELETE", "/admin/users/{{.ID}}"), dsl.Danger()),
                )),
        ),
    )
}

// Entity auto-UI — DSL is generated from entity config
app.Entity("users", framework.EntityConfig{
    Fields: []schema.Field{...},
    UI: uiframework.ScaffoldUI(uiframework.ScaffoldConfig{
        ListColumns: []string{"name", "email", "role"},
        SearchFields: []string{"name", "email"},
        Filters: []string{"role"},
    }),
})
```

### Strengths
- ✅ Highest abstraction — you think in UI intent, not HTML tags
- ✅ Can generate radically different outputs (accessible HTML, mobile, CLI) from same DSL
- ✅ Theme system is natural — same DSL, different render output
- ✅ AI-friendly — very regular structure, easy to generate
- ✅ Consistency enforced — all buttons look the same because there's one Button DSL

### Weaknesses
- ⚠️ Learning curve — new mental model, not "just HTML in Go"
- ⚠️ Abstraction leak — eventually you need `<div class="something-specific">` and fight the DSL
- ⚠️ Massive scope — building a layout engine, style system, and renderer from scratch
- ⚠️ Debugging — inspecting DOM doesn't map cleanly back to DSL
- ⚠️ Risk of becoming "yet another framework that fights you"

---

## Alternative 5: Compiler-Based Typed Templates (Codegen Approach)

### Core idea
Write template files in a custom syntax (`.gfui` files). A compiler (`gofastr generate`) parses them and produces **type-safe Go code** — actual Go functions with proper types. Think Templ but purpose-built for GoFastr's entity system. Templates have first-class awareness of entities, fields, and relationships.

### Architecture

```
ui-core/              ← What the generated code calls
  html/                 Runtime HTML helpers (escape, attr, join)
  css/                  CSS utilities (class builder, responsive, tokens)
  js/                   JS snippet generation (progressive enhancement)
  form/                 Form runtime (validation display, CSRF)
  component/            Component registry (for cross-template components)

ui-framework/         ← Codegen + entity integration
  parser/              .gfui template parser
  codegen/             .gfui → Go code generator
  entity-pages/        Auto-generate .gfui templates from entity definitions
  scaffold/            Full page scaffolding
  watch/               Hot-reload: .gfui change → recompile → inject

templates/            ← User writes these
  layouts/
    admin.gfu
  pages/
    user-list.gfu
    user-form.gfu
  components/
    badge.gfu
    data-table.gfu
```

### Template syntax example (`.gfui` file)

```
// user-list.gfui
package pages

import "github.com/gofastr/gofastr/framework"

template UserList(users []User, page pagination.Page) html {
    <layout:admin title="Users">
        <nav:breadcrumb>
            <nav:item href="/admin">Home</nav:item>
            <nav:item active>Users</nav:item>
        </nav:breadcrumb>

        <page:header>
            <h1>Users</h1>
            <a href="/admin/users/new" class="btn primary">New User</a>
        </page:header>

        <ui:data-table data="{users}" page="{page}">
            <ui:column label="Name">
                {.Name}
            </ui:column>
            <ui:column label="Email">
                {.Email}
            </ui:column>
            <ui:column label="Role">
                <ui:badge variant="{roleColor(.Role)}">{.Role}</ui:badge>
            </ui:column>
            <ui:column label="Actions">
                <a href="/admin/users/{.ID}/edit">Edit</a>
                <form method="POST" action="/admin/users/{.ID}" data-confirm="Delete user?">
                    <input type="hidden" name="_method" value="DELETE" />
                    <button type="submit" class="btn danger">Delete</button>
                </form>
            </ui:column>
        </ui:data-table>
    </layout:admin>
}
```

### Generated Go code (what `gofastr generate` produces)

```go
// user-list.gen.go — AUTO-GENERATED
func UserList(users []User, page pagination.Page) render.HTML {
    var b strings.Builder
    b.WriteString(AdminLayout("Users", render.Join(
        Breadcrumb(
            BreadcrumbItem("/admin", "Home", false),
            BreadcrumbItem("", "Users", true),
        ),
        // ... rest compiled to direct strings.Builder calls
    )))
    return render.Raw(b.String())
}
```

### Entity auto-generation

```go
app.Entity("users", framework.EntityConfig{
    Fields: []schema.Field{
        {Name: "name", Type: schema.String, Required: true},
        {Name: "email", Type: schema.String, Required: true},
        {Name: "role", Type: schema.Enum, Values: []string{"admin", "author", "reader"}},
    },
    UI: uiframework.GenerateTemplates, // generates .gfui files → Go code
})
// → generates templates/pages/users/list.gfu
// → generates templates/pages/users/form.gfu
// → generates templates/pages/users/detail.gfu
// → user edits the .gfu files to customize
// → gofastr generate compiles them to Go
```

### Strengths
- ✅ Best DX for writing UI — familiar HTML-like syntax with Go type safety
- ✅ Compile-time guaranteed — typo a field name? Compiler error.
- ✅ Entity-aware — templates can reference entity fields with autocomplete
- ✅ AI-friendly — AI writes .gfui templates (simpler than raw Go HTML builders)
- ✅ Customizable — auto-generated templates are starting points, user edits them
- ✅ Performance — generates `strings.Builder` code, zero reflection

### Weaknesses
- ⚠️ Building a parser + codegen is a significant project
- ⚠️ Custom syntax = custom tooling (no editor support, no syntax highlighting initially)
- ⚠️ Build step required — can't just run `go build` (need `gofastr generate` first)
- ⚠️ Error messages can be confusing (template syntax error → which line?)

---

## Comparison Matrix

| Dimension | Your Proposal (Typed Builder) | Alt 1 (VDOM Diff) | Alt 2 (Islands) | Alt 3 (Reactive Signals) | Alt 4 (Declarative DSL) | Alt 5 (Codegen Templates) |
|---|---|---|---|---|---|---|
| **Build effort** | 🟢 Low (extends existing) | 🔴 Very high | 🟡 Medium | 🔴 High | 🔴 Very high | 🟡 Medium |
| **Type safety** | 🟢 Full | 🟢 Full | 🟢 Full | 🟡 Partial (strings) | 🟢 Full | 🟢 Full |
| **DX / ergonomics** | 🟡 Verbose | 🟡 Verbose | 🟢 Good | 🟢 Elegant | 🟢 High-level | 🟢 Familiar syntax |
| **Interactivity** | 🔴 Full reloads | 🟢 Live updates | 🟢 Live islands | 🟢 Reactive | 🟡 Actions only | 🟡 Actions only |
| **Performance** | 🟢 Fast (static HTML) | 🟡 Diff overhead | 🟢 Mostly static | 🟡 Reactivity overhead | 🟢 Static output | 🟢 Compiled output |
| **AI-friendliness** | 🟢 Easy to generate | 🟡 Complex to generate | 🟢 Easy to generate | 🟡 Signal graph hard for AI | 🟢 Very regular | 🟢 Template syntax easy |
| **Debuggability** | 🟢 Read the Go code | 🔴 Diff bugs | 🟢 Inspect islands | 🔴 Signal tracing | 🟡 DSL → DOM gap | 🟢 Read generated code |
| **Fits existing core/** | 🟢 Extends render | 🟡 Needs new stream | 🟢 Uses render + stream | 🟡 New paradigm | 🔴 New paradigm | 🟢 Generates render calls |
| **Offline-capable** | 🟢 Static HTML works | 🔴 Needs server | 🟢 Degrades gracefully | 🔴 Needs server | 🟢 Static HTML works | 🟢 Static HTML works |
| **Customizability** | 🟢 Full control | 🟡 VDOM constraints | 🟢 Mix static + live | 🟡 Signal constraints | 🔴 DSL constraints | 🟢 Edit the templates |

---

## My Recommendation: Hybrid — Your Proposal + Islands + Codegen

The best approach combines three ideas rather than picking one:

1. **`ui-core/`** = Your typed HTML builder (extend `core/render`) + **islands** for interactivity
2. **`ui-framework/`** = Entity auto-UI (your proposal) + **optional codegen** for customization (Alt 5)

### Why this combo works

- **Your builder is the foundation** — `Tag()`, `Component[T]`, `Layout` already exist. Extend them.
- **Islands solve the interactivity gap** — most pages are static HTML. Only tables and forms need live updates. Use `core/stream` (SSE) to push island updates. Tiny ~1KB client runtime.
- **Codegen for escape hatches** — auto-generated pages work 90% of the time. When they don't, `gofastr generate ui` scaffolds Go template files you can edit freely. No custom syntax needed — it generates Go code using `ui-core` primitives.

### Phased build

| Phase | What | Scope |
|---|---|---|
| **Phase 1** | Extend `core/render` into `ui-core/` (forms, tables, nav, page, components, CSS utilities) | Foundation |
| **Phase 2** | `ui-framework/` auto-generates CRUD pages from entity definitions using `ui-core/` | Fast prototyping |
| **Phase 3** | Add islands — live tables, form validation, dashboard widgets | Interactivity |
| **Phase 4** | Codegen — scaffold customizable page templates from entities | Escape hatch |

This gives you a working admin panel in Phase 2, interactivity in Phase 3, and full customizability in Phase 4. Each phase ships independently.

---

## Open Questions

1. **CSS strategy**: Utility classes (Tailwind-like generation)? Token-based theming? Inline styles? BEM? Mix?
2. **JS boundary**: How much JS is acceptable? Islands need ~1KB. Is that the ceiling? Or do we allow progressive enhancement scripts?
3. **Form handling**: Server-side validation + full reload? Or client-side validation + island updates?
4. **Admin panel scope**: How much should `ui-framework/` auto-generate vs. leave to the user?
5. **Mobile**: Responsive by default? Or mobile-first? How do layouts adapt?
6. **Dark mode**: Token-based from day one? Or later?
7. **Accessibility**: WCAG AA compliance for auto-generated components? First-class ARIA attributes in primitives?
8. **Testing**: How do we test UI? Snapshot testing of rendered HTML? Visual regression?
