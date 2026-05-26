# battery/admin

Read-only admin pages plugged into data the framework already collects:
queue jobs (`battery/queue`) and the audit log (`framework.WithAuditLog`).

**Use this when** the prompt mentions: admin page, back office, ops
dashboard, view queue, browse audit log, "show recent jobs", "/admin".

**Import:** `github.com/DonaldMurillo/gofastr/battery/admin`

**Shape:**
```go
app.RegisterBattery(admin.New(admin.Config{
    PathPrefix: "/admin/system",  // optional; defaults to "/admin"
    Title:      "My App · System",
    DB:         db,                // for /admin/audit
    AuditTable: "audit_log",       // matches framework.WithAuditLog
    Queue:      myQueue,           // optional; for /admin/queue
}))
```

**Routes mounted:**
- `GET {PathPrefix}` — index with per-section summary cards
- `GET {PathPrefix}/queue` — jobs list with `?status=` filter
- `GET {PathPrefix}/audit` — audit log paged newest-first

**Don't reinvent** the read-only audit/queue browser. DO write your own
domain-admin (per-user pages, moderation tooling) — the battery is
deliberately limited to data the framework already owns.

**Auth:** the battery does not gate access. Wire your own middleware
(e.g. `auth.RequireRole("admin")`) in front of `{PathPrefix}`.
