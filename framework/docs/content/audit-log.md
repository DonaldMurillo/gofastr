# Audit log

`WithAuditLog` writes an audit row for every Create/Update/Delete on
the registered entities. The row is written **inside the same
transaction** as the change it describes, so a rollback drops the
audit alongside the change — never partial.

## Quickstart

```go
app := framework.NewApp(framework.WithDB(db))
app.Entity("posts", framework.EntityConfig{ … })
app.Entity("users", framework.EntityConfig{ … })

// Register entities first, THEN enable audit:
app.WithAuditLog(framework.AuditConfig{
    Table: "audit_log",                // default; can be omitted
    Actor: func(ctx context.Context) string {
        return currentUserID(ctx)
    },
    Entities: []string{"posts", "users"}, // empty = audit everything
})
```

Order matters: `WithAuditLog` walks the registry and attaches hooks,
so it must be called *after* `Entity` registrations. Otherwise the
audit hooks bind to no entities.

## Schema

```sql
CREATE TABLE audit_log (
    id          TEXT       PRIMARY KEY,
    entity      TEXT       NOT NULL,
    op          TEXT       NOT NULL,   -- 'create' | 'update' | 'delete'
    record_id   TEXT       NOT NULL,
    actor_id    TEXT,                  -- nullable
    tenant_id   TEXT,                  -- nullable; current tenant at write time
    created_at  TIMESTAMPTZ NOT NULL,  -- DATETIME on SQLite
    diff        TEXT                   -- JSON
);
```

`EnsureAuditTable(db, table)` creates this table; `WithAuditLog` calls
it for you and panics on failure (this is initialisation-time work —
loud failure is preferable to silent log loss).

`tenant_id` is populated from `tenant.GetTenantID(ctx)` at write time, so
multi-tenant apps can scope the audit trail per tenant instead of mixing
every tenant's rows in one table. It is `NULL` for writes with no tenant
in context (single-tenant apps, system/async writes). The column is added
idempotently — an `audit_log` table created by an older binary gets a
nullable `tenant_id` added on the next `EnsureAuditTable`, with existing
rows left untouched. See [multi-tenant](multi-tenant.md) for the
tenant-scoped query pattern.

## Configuration

| Field      | Effect                                                                  |
|------------|--------------------------------------------------------------------------|
| `Table`    | Destination table. Defaults to `audit_log`.                              |
| `Actor`    | Resolves the actor ID (typically user ID) from `context.Context`. Empty string = system write. |
| `Entities` | Allowlist of entity names to audit. Empty = every registered entity.    |

## Row shape

For `create`, `diff` contains the post-write record:

```json
{"new": {"id": "p1", "title": "First", "status": "published"}}
```

For `update`, CRUD snapshots the row before the write and records both images:

```json
{"old": {"id": "p1", "status": "draft"}, "new": {"id": "p1", "status": "published"}}
```

For `delete`, `record_id` holds the deleted ID and `diff.old` contains the
pre-delete row. These snapshots are captured by the CRUD handler inside the
write transaction; callers that invoke hooks directly without CRUD may have a
null old image.

## Transactional behaviour

Audit hooks resolve the active CRUD transaction via
`TxFromContext(ctx)`. The row is inserted through the same
transaction, so:

- A failed `After*` hook later in the chain rolls back the audit row
  with the parent write.
- Batch operations write all audit rows in the same transaction; a
  per-item failure rolls back every audit row in the batch.
- Audit writes outside a transaction (rare — async hooks) fall back
  to the plain connection pool.

## What gets audited

- `AfterCreate` → `op = 'create'`, `diff = {"new": <record>}`
- `AfterUpdate` → `op = 'update'`, `diff = {"old": <record>, "new": <record>}`
- `AfterDelete` → `op = 'delete'`, `diff = {"old": <record>}`

`Before*` hooks are not audited; the audit only records committed
changes (modulo transactional behaviour above).

## Querying the audit log

There are no built-in HTTP endpoints for audit_log. Either:

- Declare `audit_log` as a read-only entity (`CRUD: false` for write,
  manually register a custom endpoint for reads).
- Query directly from your own admin handler.

```sql
SELECT entity, op, record_id, actor_id, created_at
FROM audit_log
WHERE entity = 'posts'
ORDER BY created_at DESC
LIMIT 50;
```

In a multi-tenant app, scope the read to the caller's tenant so one
tenant can't see another's audit trail:

```sql
SELECT entity, op, record_id, actor_id, created_at
FROM audit_log
WHERE tenant_id = $1
ORDER BY created_at DESC
LIMIT 50;
```

## Auth security events

The CRUD hooks cover entity writes, but security-sensitive auth activity
(login, 2FA, password reset, OAuth, magic-link) is not a CRUD row — it
would never reach the audit table on its own. `battery/auth` fills that
gap with an `AuditSink`: the auth manager emits a fixed-vocabulary
security event at each decision point, and the built-in SQL sink writes
it into the **same `audit_log` table** as the CRUD hooks (entity
`"auth"`), so an operator has one trail for "who did what".

### Wiring

```go
sink, err := auth.NewSQLAuditSink(db, "") // "" → "audit_log", same table as WithAuditLog
mgr := auth.New(auth.AuthConfig{
    AuditSink: sink,   // nil disables — emit calls are no-ops
    UserStore: myUserStore,
    …
})
```

`NewSQLAuditSink` calls `EnsureAuditTable` once at construction (the
table is shared with `WithAuditLog`), so the two never drift apart. The
sink is called on the request path — it writes synchronously, so pair it
with a fast DB or wrap it in a buffering implementation for high-volume
deployments.

### Event taxonomy

| Kind | When it fires |
|---|---|
| `login.succeeded` | Password login completes and the session is fully privileged (no pending 2FA). |
| `login.pending_2fa` | Login succeeds at the password step but the user has 2FA — a `PendingTwoFactor` session is minted. |
| `login.failed` | Bad credentials (unknown email OR wrong password — same event, `reason=bad_credentials`) or the fail-closed 2FA rejection (`reason=twofa_failclosed`). |
| `register.succeeded` | A new user is created. |
| `session.revoked` | Logout (`reason=logout`) or a password reset purging existing sessions (`reason=password_reset`, with `count`). |
| `2fa.enrolled` | 2FA enrollment verified (secret enabled + backup codes issued). |
| `2fa.challenge_succeeded` | 2FA challenge passed (`method=totp` or `method=backup_code`). |
| `2fa.challenge_failed` | 2FA challenge rejected. |
| `2fa.disabled` | 2FA turned off. |
| `2fa.backup_codes_regenerated` | Backup codes refreshed. |
| `password.reset_requested` | Forgot-password requested — fires for **known and unknown** emails (empty `UserID` for unknown), so account probing is visible. `known=true/false`. |
| `password.reset_completed` | Password successfully reset. |
| `oauth.linked` | A NEW `(provider, providerID)` binding was persisted. |
| `oauth.login` | An already-linked OAuth identity logged in. |
| `oauth.refused` | OAuth callback refused on email collision (`reason=link_conflict`) — the account-takeover defence. |
| `magiclink.requested` | A magic link was sent. |
| `magiclink.consumed` | A magic link token was redeemed for a session. |

Each row carries `record_id`/`actor_id` = the resolved user id (or `"-"`
when unknown — the column is NOT NULL), and a `diff` JSON of
`{email, remote, …meta}`. `remote` is the host part of `r.RemoteAddr`;
`X-Forwarded-For` is never trusted (see the auth threat model).

### Redaction posture

Auth events NEVER carry credentials: no passwords, hashes, session tokens,
JWTs, TOTP secrets/codes, backup codes, magic-link/reset tokens, or OAuth
access/refresh tokens. The `Meta` map holds only fixed-vocabulary strings
(a `reason` code, a `method`, a `count`) — the ONLY user-controlled string
in any event field is `Email`. A misbehaving sink that panics is
recovered: the event is lost, but the login/reset/2FA flow it was auditing
proceeds (a broken audit sink must never break auth).

### Custom sinks and non-auth events

`AuditSink` is an interface — implement it to route events to syslog,
a SIEM, or a separate table. For a one-off non-CRUD audit row outside
auth (a domain action like "suspend user" or "export report"), call the
framework helper directly:

```go
framework.AppendAuditEvent(ctx, db, "", "billing", "export.run", userID, userID,
    map[string]any{"format": "csv", "rows": 1280})
```

`AppendAuditEvent` reuses the CRUD row writer and the same control-byte
sanitisation as the hooks, so a custom caller and the lifecycle hooks
cannot drift apart. It writes through the plain pool (or the active
transaction when one is on `ctx`) — unlike the CRUD hooks it is NOT
automatically transactional, because security events are not part of an
entity write. The caller owns atomicity if it needs any.

## Common mistakes

- **Calling `WithAuditLog` before `Entity`.** Hooks register against
  whatever is in the registry at call time. Audit nothing if you call
  it first.
- **Filtering `Entities` by table name.** It expects the entity
  *name* (the key passed to `app.Entity`), not the SQL table name.
- **Expecting `diff` to contain a before/after pair.** It contains
  only `new`. Compute deltas client-side if needed.
- **Relying on the audit row for crash recovery.** Same transaction:
  if the parent write commits, the audit committed. If it didn't, no
  audit either.
