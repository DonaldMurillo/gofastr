# battery/admin

The back office: a dashboard, the entity screens (`framework/entityui`
list, record and create), and the operations pages (jobs, audit log,
roles, user roles, process modules), in one shell drawn through the
app's UI host. Ships no CSS and no JS.

**Use this when** the prompt mentions: admin page, back office, manage
entities, edit records, ops dashboard, view queue, browse audit log,
"/admin".

**Import:** `github.com/DonaldMurillo/gofastr/battery/admin`

**Shape:**
```go
site := appui.NewApp("My App")
app := framework.NewUIHostApp(uihost.New(site), framework.WithDB(db))
app.Use(auth.SessionMiddleware(mgr)) // sets the request user

app.Entity("products", productsConfig)
app.RegisterBattery(admin.New(admin.Config{
    Title:    "Back office",
    UI:       app.EntityUI(entityui.Extensions{}), // required with entities
    Entities: []string{"products"},                // or AllEntities: true
    Queue:    myQueue,                             // optional Jobs page
    Pages:    []admin.Page{...},                   // optional app pages
    Cards:    []admin.Card{...},                   // optional dashboard cards
}))
```

**Pages:** `{prefix}` dashboard, `/search`, `/entities/<name>`,
`/entities/<name>/create`, `/entities/<name>/:id`, `/queue`, `/audit`,
`/rbac/roles`, `/rbac/users`, `/modules`. Writes go to
`{prefix}/api/<name>` through the app's own CRUD handler (hooks, audit,
scope apply).

**Gate:** default deny. An authenticated user holding `AdminRole`
("admin"), or `Authorize`. Anonymous 401 (or `LoginPath` redirect),
no role 403. Embed grants and a Decider deny refuse the whole admin.

**Elevation:** admin routes and screens lift only the entity's
`Exposure.Access` check (`crud.WithElevation`); tenant and owner scope
still apply. App `Page`/`Card` builds run unelevated.

**Don't:** add CSS, hand-roll markup, or re-implement CRUD here; a
missing piece goes into `framework/ui` or `framework/entityui`.

**Docs:** `gofastr docs admin`. **Example:** `examples/backoffice`.
