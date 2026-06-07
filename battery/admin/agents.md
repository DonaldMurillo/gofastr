# battery/admin

An admin back-office battery with two halves:

1. **Entity CRUD** — generated list / create / edit / delete screens for your
   entities, rendered through the app's UI host so they hydrate with
   `runtime.js` (DataTable island, `data-fui-confirm` delete, SSR forms). No
   bespoke JS. Requires a mounted UI host.
2. **Ops dashboards** — read-only queue + audit pages on data the framework
   already collects (`battery/queue`, `framework.WithAuditLog`). Self-contained
   HTML, no host needed.

**Use this when** the prompt mentions: admin page, back office, CRUD UI, manage
entities, edit records, ops dashboard, view queue, browse audit log, "/admin".

**Import:** `github.com/DonaldMurillo/gofastr/battery/admin`

**Shape:**
```go
// Needs a UI host for the entity screens:
site := appui.NewApp("My App")
app := framework.NewUIHostApp(uihost.New(site), framework.WithDB(db))
app.Use(auth.SessionMiddleware(mgr)) // sets the request user

app.Entity("products", productsConfig)
app.RegisterBattery(admin.New(admin.Config{
    Title:     "Back office",
    // Entities: nil  → auto-expose every CRUD-enabled entity (CRUD=false skipped)
    // Entities: []string{"products"} → only these
    Authorize: func(ctx context.Context) bool { // optional; default = any non-nil user
        u := auth.GetCurrentUser(ctx)
        return u != nil && u.HasRole("admin")
    },
    Queue: myQueue, // optional ops dashboard
    DB:    db,      // optional ops dashboard (audit)
}))
```

**Routes mounted:**
- Entity screens (host): `GET {prefix}/e/{table}`, `/e/{table}/new`, `/e/{table}/edit/:id`
- Entity RPC/form (router): `POST .../_create`, `POST .../_update/{id}`, `DELETE .../_delete/{id}`, `GET .../_rows`
- Ops: `GET {prefix}` (overview), `GET {prefix}/queue`, `GET {prefix}/audit`

**Gated by default:** any authenticated non-nil user. Anonymous → 401 (screens
via host policy chain, routes via middleware). The non-nil check matters —
`auth.SessionMiddleware` seeds a nil user, so "user present?" alone is insufficient.

**Writes go through each entity's own `CrudHandler`** with the request context
forwarded → validation, `OwnerField`/tenant scope, hooks, events all apply.
Never re-implement CRUD/pagination/filter logic here.

**Don't reinvent** the read-only audit/queue browser. The entity CRUD covers
generic record management; write your own domain-admin for bespoke workflows.

**Example:** `examples/backoffice`.
