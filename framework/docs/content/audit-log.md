# Audit log

`App.WithAuditLog` records an audit row in `audit_log` for every entity create, update, delete, restore, purge, state transition, and state override — written **inside the same SQL transaction** as the mutation so rollbacks never leave phantom audit entries.

## Quickstart

Register entities **first**, then enable the audit log (otherwise hooks have no entities to attach to):

```go
app := framework.NewApp(framework.WithDB(db))
app.Entity("posts", framework.EntityConfig{ /* ... */ })

app.WithAuditLog(framework.AuditConfig{
	Table: "audit_log", // default
	Actor: func(ctx context.Context) string {
		return currentUserID(ctx) // nil falls back to handler.GetUser(ctx)
	},
	Entities: []string{"posts"}, // empty = audit all registered entities
})
```

## Schema and row format

`EnsureAuditTable` creates `audit_log` (`id`, `entity`, `op`, `record_id`, `actor_id`, `tenant_id`, `created_at`, `diff`, `reason`):
- `id` is a non-enumerable random ID (`aud_<32 hex>`).
- `op` is `create`, `update`, `delete`, `restore`, `purge`, `transition:<key>`, or `state_override` (carrying `reason`).
- `diff` stores `{"new": ...}` on create, `{"old": ..., "new": ...}` on update/transition, and `{"old": ...}` on delete.
- `tenant_id` captures `tenant.GetTenantID(ctx)` automatically (`battery/admin`'s `/admin/audit` scopes queries strictly to `WHERE tenant_id = $1` when a tenant is in context).
- Sensitive record IDs (keys named `api_key`, `token`, `secret`, etc.) are stored as `sha256:<hex>` by default (`framework.AuditDigestID` detects hashed IDs); custom `Redact` callbacks fail safe (storing `"[redaction failed]"` rather than leaking raw values if they panic).

## Auth security events

`battery/auth` writes security events (login, failed login, 2FA enrollment/challenge/lockout, password reset, OAuth link/conflict, magic links, email codes, passkeys, session revocation, account claims) into the **same `audit_log` table** (`entity = "auth"`) via `auth.NewSQLAuditSink(db, "")`:

```go
sink, err := auth.NewSQLAuditSink(db, "")
mgr := auth.New(auth.AuthConfig{
	AuditSink: sink,
	UserStore: myUserStore,
})
```

Security posture of auth audit rows:
- Never records credentials (passwords, hashes, tokens, TOTP secrets, backup codes).
- Rows without a known user (`record_id = "-"`, `actor_id = NULL`) store a 12-char SHA-256 `email_hash` instead of the raw email so user erasure cannot leave raw addresses behind.
- Emits under a detached 5-second timeout after the state change commits, and recovers sink panics so a broken audit sink never blocks login.

## Custom non-CRUD audit events

Record domain actions outside CRUD (like "suspend user" or "export report") with `framework.AppendAuditEvent`:

```go
framework.AppendAuditEvent(ctx, db, "", "billing", "export.run", userID, userID,
	map[string]any{"format": "csv", "rows": 1280})
```

## Common mistakes

- **Calling `WithAuditLog` before `app.Entity(...)`:** hooks bind only to entities registered at call time.
- **Passing SQL table names instead of entity names in `AuditConfig.Entities`:** pass the key given to `app.Entity`.
- **Querying `audit_log` in a multi-tenant app without `WHERE tenant_id = $1`:** mixes audit trails across tenants.
- **Relying on `UpsertOne` for exact create-vs-update audit classification under concurrent writes:** use explicit create/update calls when exact lifecycle semantics matter.
