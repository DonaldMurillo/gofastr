# framework/localentity — records kept in the visitor's browser

Use this when a page saves per-visitor data that has no business on
the server (a game team, a wishlist, a draft, a checklist) and should
survive reloads and stay in step across tabs. Do not reach for it for
data other users, other devices, or server code need: that is a server
entity (`framework.EntityConfig`).

Shape:

```go
var Box = localdb.New("team-builder")                 // core-ui/localdb
var Members = localentity.Define(Box, "members", []schema.Field{...},
    localentity.Indexed("level"), localentity.MaxRecords(6))

Members.Form("team-form", ui.Form(...))               // saves into IndexedDB
l := Members.List(localentity.ListConfig{OrderBy: "level", Desc: true})
ui.Grid(ui.GridConfig{}, l.Render(l.Row(func(r localentity.Row) render.HTML {
    return ui.Card(ui.CardConfig{HeadingContent: r.Text("nickname")}, ...)
}), l.Empty(ui.EmptyState(...))))
Members.Count()                                        // live record count
```

Don't reinvent: no app JavaScript, no localStorage, no hand-rolled
list markup. Rows are server-rendered templates the `localentity`
behaviour clones and fills with text only. For queries a form and a
list do not cover, page scripts use `__gofastr.localdb` directly
(`framework/docs/content/localdb.md`). Built-in fields: `id` (UUIDv7),
`created_at`, `updated_at`. Docs: `framework/docs/content/local-entities.md`.
