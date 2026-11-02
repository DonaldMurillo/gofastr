# Audit log

`WithAuditLog` writes an audit row for every Create/Update/Delete on
the registered entities. The row is written **inside the same
transaction** as the change it describes, so a rollback drops the
audit alongside the change, never partially.

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
    op          TEXT       NOT NULL,   -- 'create' | 'update' | 'delete' | 'restore' | 'purge'
                                        -- | 'state_override' | 'transition:<key>'
    record_id   TEXT       NOT NULL,
    actor_id    TEXT,                  -- nullable
    tenant_id   TEXT,                  -- nullable; current tenant at write time
    created_at  TIMESTAMPTZ NOT NULL,  -- DATETIME on SQLite
    diff        TEXT,                  -- JSON
    reason      TEXT                   -- a state override's reason (states.md)
);
```

The `id` is minted from `crypto/rand` (16 bytes, `aud_`-prefixed hex): it
is opaque and non-enumerable — there is no timestamp component to walk —
and a rand failure refuses the audit row rather than falling back to a
weaker id.

`EnsureAuditTable(db, table)` creates this table; `WithAuditLog` calls
it for you and panics on failure (this is initialisation-time work, and
loud failure is preferable to silent log loss).

`tenant_id` is populated from `tenant.GetTenantID(ctx)` at write time, so
multi-tenant apps can scope the audit trail per tenant instead of mixing
every tenant's rows in one table. It is `NULL` for writes with no tenant
in context (single-tenant apps, system/async writes). The `tenant_id` and
`reason` columns are added idempotently: an `audit_log` table created by
an older binary gets each missing nullable column on the next
`EnsureAuditTable`, with existing rows left untouched. Replicas booting
together on one old table all succeed: Postgres adds with `ADD COLUMN IF
NOT EXISTS`, and on SQLite a failed add passes once the catalog shows the
column. A database role that may not `ALTER` the table fails at boot; add
`reason TEXT` with a migration role first. See
[multi-tenant](multi-tenant.md) for the tenant-scoped query pattern.

`restore` and `purge` come from the soft-delete operations
(`crud.RestoreOne`, `crud.PurgeOne`): they run the ordinary update and
delete hook chains, and the operation they carry on the context — keyed
to the one entity and record being restored or purged — reaches that
record's audit row, so the trail says what actually happened rather than
"update"/"delete". A hook that writes another entity's row mid-restore
still gets that row's own operation: only the CRUD handler sets an
override, and app code cannot write an arbitrary operation name into the
trail.
A state move writes `transition:<key>` and a state override
`state_override` the same way (see [states](states.md)).

## Configuration

| Field      | Effect                                                                  |
|------------|--------------------------------------------------------------------------|
| `Table`    | Destination table. Defaults to `audit_log`.                              |
| `Actor`    | Resolves the actor ID (typically user ID) from `context.Context`. Empty string = system write. Nil records the request user's `GetID()` (the user `handler.GetUser` returns), or no actor when the request has none. |
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

`record_id` is the record's id, the same string its URL carries, with
control bytes stripped. The CRUD handler reads it from its own key
columns and puts it on the AfterCreate/AfterUpdate hook context; a hook
of your own reads it with `crud.AuditRecordIDFor(ctx, entityName)`.
That holds where the row cannot say which key is the key: two API
versions may give the key and another column each other's wire names.
Two cases change it:

- With no `Redact`, a key column whose name or wire name the default
  scrub treats as sensitive (`api_key`, `token`, `secret`, ...) is stored
  as `sha256:<hex digest>` of the id on every row. The Activity tab
  hashes the id the same way when it looks the record's rows up. A
  digest names no live record, so a reader that resolves `record_id` to
  a record checks `framework.AuditDigestID(recordID)` first; the admin
  battery's audit page does, and shows such rows without a record link.
- A `Redact` callback receives `{"id": <record id>}` on delete, and the
  `"id"` it returns becomes the delete row's `record_id`. A result with
  no `"id"` keeps the original. Create and update rows keep the raw id,
  so the delete row of a pseudonymised record does not show in its
  Activity tab.

## Transactional behaviour

Audit hooks resolve the active CRUD transaction via
`TxFromContext(ctx)`. The row is inserted through the same
transaction, so:

- A failed `After*` hook later in the chain rolls back the audit row
  with the parent write.
- Batch operations write all audit rows in the same transaction; a
  per-item failure rolls back every audit row in the batch.
- Audit writes outside a transaction (rare: async hooks) fall back
  to the plain connection pool.

## What gets audited

- `AfterCreate` → `op = 'create'`, `diff = {"new": <record>}`
- `AfterUpdate` → `op = 'update'`, `diff = {"old": <record>, "new": <record>}`
- `AfterDelete` → `op = 'delete'`, `diff = {"old": <record>}`
- a state move (`RunTransition`) → `op = 'transition:<key>'`, both images
  in `diff`, inside the move's transaction
- an update under `crud.WithStateOverride`, or an `UpsertOne` under it
  that lands on an existing row → `op = 'state_override'`, with the
  override's reason in the `reason` column

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

The admin battery's own audit surfaces apply this rule themselves: when
the admin context carries a tenant id, `GET <prefix>/audit` and the
overview's audit-entries tile both filter with `WHERE tenant_id = $1`.
Rows with a `NULL` tenant (system writes, single-tenant apps) are NOT
visible to a tenant-scoped admin — the predicate is strict equality, so
one tenant can never infer another's (or the platform's) write activity.
An admin context with no tenant id sees every row, the platform-operator
posture.

## Auth security events

The CRUD hooks cover entity writes, but security-sensitive auth activity
(login, 2FA, password reset, OAuth, magic-link) is not a CRUD row and
would never reach the audit table on its own. `battery/auth` fills that
gap with an `AuditSink`: the auth manager emits a fixed-vocabulary
security event at each decision point, and the built-in SQL sink writes
it into the **same `audit_log` table** as the CRUD hooks (entity
`"auth"`), so an operator has one trail for "who did what".

### Wiring

```go
sink, err := auth.NewSQLAuditSink(db, "") // "" → "audit_log", same table as WithAuditLog
mgr := auth.New(auth.AuthConfig{
    AuditSink: sink,   // nil disables it: emit calls are no-ops
    UserStore: myUserStore,
    …
})
```

`NewSQLAuditSink` calls `EnsureAuditTable` once at construction (the
table is shared with `WithAuditLog`), so the two never drift apart. The
sink is called on the request path and writes synchronously, so pair it
with a fast DB or wrap it in a buffering implementation for high-volume
deployments.

### Event taxonomy

| Kind | When it fires |
|---|---|
| `login.succeeded` | A password or passkey (`via=passkey`) login completes and the session is fully privileged (no pending 2FA). |
| `login.pending_2fa` | Login succeeds at the password or passkey step but the user has 2FA, so a `PendingTwoFactor` session is minted. |
| `login.failed` | Bad credentials (unknown email OR wrong password: same event, `reason=bad_credentials`) or the fail-closed 2FA rejection (`reason=twofa_failclosed`, with `via=passkey` on a passkey sign-in). |
| `register.succeeded` | A new user is created. |
| `session.revoked` | Logout (`reason=logout`) or a password reset purging existing sessions (`reason=password_reset`, with `count`). |
| `2fa.enrolled` | 2FA enrollment verified (secret enabled + backup codes issued). |
| `2fa.challenge_succeeded` | 2FA challenge passed (`method=totp` or `method=backup_code`). |
| `2fa.challenge_failed` | 2FA challenge rejected. |
| `2fa.challenge_locked` | A pending session spent its per-session challenge budget and was deleted; the user signs in again. |
| `2fa.disabled` | 2FA turned off. |
| `2fa.backup_codes_regenerated` | Backup codes refreshed. |
| `password.reset_requested` | Forgot-password requested. Fires for **known and unknown** emails (empty `UserID` for unknown), so account probing is visible. `known=true/false`. |
| `password.reset_completed` | Password successfully reset. |
| `oauth.linked` | A NEW `(provider, providerID)` binding was persisted. |
| `oauth.login` | An already-linked OAuth identity logged in. |
| `oauth.refused` | OAuth callback refused on email collision (`reason=link_conflict`), the account-takeover defence. |
| `magiclink.requested` | A magic link was sent. |
| `magiclink.consumed` | A magic link token was redeemed for a session. |
| `magiclink_tokens.purged` | A password change (`reason=password_change`) or a completed reset (`reason=password_reset`) spent the user's outstanding magic links (`count`). A token store without `MagicLinkTokenPurger` keeps them and logs a warning. |
| `passkey.registered` | A passkey was added (`alg`, `backup_eligible`). |
| `passkey.deleted` | A passkey was removed by its owner, or a completed reset removed all of them (`reason=password_reset`, `count`). |
| `passkey.refused` | A passkey ceremony failed a check (`ceremony=login` or `register`). `reason` is one of `client_data`, `challenge`, `attestation`, `credential_mismatch`, `unknown_credential`, `user_handle`, `signature`, `backup_eligibility`, `account_gone`. |
| `passkey.clone_suspected` | A sign-in presented a signature counter that did not move past the stored one (`stored`, `presented`). |
| `account.claimed` | A magic link (`via=magic_link`) or a completed reset (`via=password_reset`) proved the mailbox of an account whose address was unverified. The account's OAuth links, API tokens and sessions were removed (counts in `oauth_links_removed`, `tokens_revoked`, `sessions_revoked`), its password too when `password_cleared=true`, its passkeys (`passkeys_removed`), and it is now verified. |

Each row carries `record_id`/`actor_id` = the resolved user id (or `"-"`
when unknown, since the column is NOT NULL), and a `diff` JSON of
`{email, remote, …meta}`. `remote` is the host part of `r.RemoteAddr`;
`X-Forwarded-For` is never trusted (see the auth threat model). A row
with no user id (an unknown address, a magic-link request, a refused
OAuth sign-in) carries `email_hash`, a 12-hex-char sha256 prefix of the
canonical address, instead of `email`, because erasure scrubs the rows
a user acted from and cannot reach a row with no actor: a raw address
there outlived the user. The address is canonicalized with
`auth.CanonicalEmail`, the default, even on a host that set
`AuthConfig.CanonicalizeEmail` (the sink has no manager); look a row up
by `hashedIdentifier`'s rule over that form: the first 12 hex chars of
its sha256. The digest is a pseudonym, not anonymous data: anyone holding
a candidate address can confirm a match.

Every sink, the built-in one and a host's own (`AuthConfig.AuditSink`
and the token middleware's `WithTokenAudit`), gets the request's
context minus its cancellation, with a 5-second deadline. Every event is
emitted after the change it records, so a client that hangs up after a
2FA disable or a role change no longer drops the row.

### Redaction posture

Auth events NEVER carry credentials: no passwords, hashes, session tokens,
JWTs, TOTP secrets/codes, backup codes, magic-link/reset tokens, or OAuth
access/refresh tokens. The `Meta` map holds only fixed-vocabulary strings
(a `reason` code, a `method`, a `count`). The ONLY user-controlled string
in any event field is `Email`. A misbehaving sink that panics is
recovered: the event is lost, but the login/reset/2FA flow it was auditing
proceeds (a broken audit sink must never break auth).

### Custom sinks and non-auth events

`AuditSink` is an interface. Implement it to route events to syslog,
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
transaction when one is on `ctx`). Unlike the CRUD hooks it is NOT
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
