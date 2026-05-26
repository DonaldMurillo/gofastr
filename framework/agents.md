# framework (root + sub-helpers)

App-level helpers worth surfacing to AI agents alongside the batteries.

## `framework.WithAuditLog(cfg)` — automatic CRUD audit

**Use this when** the prompt mentions: audit trail, who did what, track
changes, compliance log, history of modifications, admin accountability.

```go
app := framework.NewApp(
    framework.WithDB(db),
    framework.WithAuditLog(framework.AuditConfig{
        Actor:    func(ctx context.Context) string { return currentUserID(ctx) },
        Entities: []string{"users", "orders"},   // restrict to non-PHI entities
        Redact:   nil,                            // optional func(entityName, row) row to redact JSON diff
    }),
)
```

Writes one `audit_log` row per AfterCreate / AfterUpdate / AfterDelete
inside the same transaction as the change, so audit and write commit
or roll back together. Failed audit insert → write fails.

**PHI caveat:** the audit row contains a full JSON `diff` of the
changed row. For entities holding user-content (search history, symptom
logs, etc.) either exclude them via `Entities` or set `Redact` to drop
the sensitive keys before serialisation. Don't fold full content into
audit; that defeats the point of audit hygiene.

**Don't reinvent** a hand-rolled `audit_log` entity + `recordAuditLog`
helper for entity CRUD. DO keep custom audit writes for domain actions
(suspend, delete, password.reset) — the framework only covers CRUD,
not arbitrary domain events.

---

## `framework/dev` — auto-wired livereload

`gofastr dev` rebuilds the binary; `framework.NewApp` + `uihost.New`
auto-wire `/__livereload` + `/__livereload.js` so every open tab
refreshes when the new binary boots. No host code needed.

**Disable:** `GOFASTR_DEV_LIVERELOAD=0` (keeps rebuild loop). Production
is hard-killed by `GOFASTR_ENV=production`.

**See:** [`framework/docs/content/dev-livereload.md`](docs/content/dev-livereload.md).

---

## `core/dotenv` — automatic `.env` loading

`framework.NewApp` reads `.env.local`, `.env.<APP_ENV>`, `.env` in
order (earlier wins) before option callbacks run. Existing `os.Environ`
always beats file values.

**Disable:** `GOFASTR_DOTENV=off` in the real process env.

**Don't** add a separate `godotenv.Load()` call before `NewApp` — it's
already done.

---

## `framework.WithMCPIntrospection()` — live-app agent debug

Adds `framework_docs_list/get/search`, `app_routes`, `app_plugins`,
`app_batteries`, `app_config`, `app_readiness` to the app's MCP
endpoint so a connected agent can answer "what routes exist" /
"is the app ready" without leaving the session.

**Use this when** the prompt asks about live introspection, debugging
a running server, or "is this app healthy". See the
`gofastr-mcp-debug` Claude Code skill.

---

## Don't reinvent

- A custom audit log entity — `WithAuditLog` for CRUD; domain actions stay custom.
- A livereload SSE handler — framework auto-wires it under `gofastr dev`.
- A dotenv loader — `NewApp` does it.
- Per-test PG isolation — see `framework/testkit` (`NewIsolatedDB`).
