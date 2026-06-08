package framework

import (
	"context"
	cryptorand "crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/hook"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
	"github.com/DonaldMurillo/gofastr/framework/tenant"
)

// AuditConfig configures the audit log helper.
//
// Audit rows are written via lifecycle hooks: AfterCreate, AfterUpdate, and
// AfterDelete. The hook fires inside the same transaction as the operation
// it audits, so partial writes are impossible — a rollback drops the audit
// row along with the change.
type AuditConfig struct {
	// Table is the destination table for audit rows. Defaults to "audit_log".
	Table string

	// Actor resolves the actor id (typically a user id) from the request
	// context. Return "" when no actor is attached (e.g. system writes).
	Actor func(context.Context) string

	// Entities restricts auditing to the named entities. Empty means audit
	// every registered entity.
	Entities []string

	// Redact, when non-nil, is called with the entity name and a
	// defensive copy of the row about to be serialised into the
	// `diff` column. Return either the modified input (safe to mutate
	// — the framework already copied it) or a fresh map. Either works.
	//
	// When nil, the framework applies a default sensitive-field scrub
	// (see defaultSensitiveSuffixes) — fields whose names look like
	// passwords, tokens, secrets, or keys are dropped from the diff so
	// a host that forgot to configure Redact doesn't accidentally
	// stream credentials into the audit log.
	//
	// Redact is invoked for AfterCreate, AfterUpdate, AND AfterDelete.
	// On delete the input is `map[string]any{"id": <record_id>}`; the
	// returned map's "id" value (if any) becomes the audit row's
	// record_id, letting hosts pseudonymise the natural key on delete
	// too. If the callback returns a map with no `id` key the framework
	// falls back to the original — silently erasing the record_id is
	// an audit-forensics erasure primitive we don't want to expose.
	//
	// Returning nil is equivalent to returning an empty map. A panic
	// inside Redact is recovered: the audit row is still written using
	// the original pre-redact value so audit coverage isn't lost to a
	// misbehaving callback.
	Redact func(entity string, row map[string]any) map[string]any
}

// auditOp is the op-code stored in the "op" column.
type auditOp string

const (
	auditOpCreate auditOp = "create"
	auditOpUpdate auditOp = "update"
	auditOpDelete auditOp = "delete"
)

// EnsureAuditTable creates the audit_log table if it does not exist. Idempotent.
// Dialect-aware via the existing migrate.DetectDialect helper.
func EnsureAuditTable(db *sql.DB, table string) error {
	if table == "" {
		table = "audit_log"
	}
	dialect := migrate.DetectDialect(db)

	tsType := "DATETIME"
	if dialect == migrate.DialectPostgres {
		tsType = "TIMESTAMPTZ"
	}
	safeTable, err := query.SafeIdent(table)
	if err != nil {
		return fmt.Errorf("audit: invalid table name %q: %w", table, err)
	}
	stmt := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
		id          TEXT PRIMARY KEY,
		entity      TEXT NOT NULL,
		op          TEXT NOT NULL,
		record_id   TEXT NOT NULL,
		actor_id    TEXT,
		tenant_id   TEXT,
		created_at  %s NOT NULL,
		diff        TEXT
	)`, query.QuoteIdent(safeTable), tsType)
	if _, err = db.Exec(stmt); err != nil {
		return err
	}
	// Backward-compat: a table created by an older binary has no tenant_id
	// column. Add it (nullable) so multi-tenant stamping works against an
	// existing audit table without a manual migration. ADD COLUMN IF NOT
	// EXISTS keeps this idempotent on both dialects (Postgres + SQLite 3.35+).
	alter := fmt.Sprintf("ALTER TABLE %s ADD COLUMN IF NOT EXISTS tenant_id TEXT", query.QuoteIdent(safeTable))
	if _, err = db.Exec(alter); err != nil {
		// Fall back to a probe-then-add for dialects without IF NOT EXISTS
		// on ADD COLUMN. A failure here means the column already exists
		// (the common case) or the dialect rejects the syntax; either way
		// we only surface an error if the column is genuinely missing.
		if !auditColumnExists(db, safeTable, "tenant_id") {
			plain := fmt.Sprintf("ALTER TABLE %s ADD COLUMN tenant_id TEXT", query.QuoteIdent(safeTable))
			if _, addErr := db.Exec(plain); addErr != nil {
				return fmt.Errorf("audit: add tenant_id column: %w", addErr)
			}
		}
	}
	return nil
}

// auditColumnExists reports whether the named column is present on table.
// Used as a dialect-agnostic fallback when ADD COLUMN IF NOT EXISTS isn't
// supported: a plain "SELECT col FROM table WHERE 1=0" succeeds only when
// the column exists.
func auditColumnExists(db *sql.DB, table, col string) bool {
	safeCol, err := query.SafeIdent(col)
	if err != nil {
		return false
	}
	q := fmt.Sprintf("SELECT %s FROM %s WHERE 1=0", query.QuoteIdent(safeCol), query.QuoteIdent(table))
	rows, err := db.Query(q)
	if err != nil {
		return false
	}
	_ = rows.Close()
	return true
}

// WithAuditLog enables audit logging on every entity registered on the app
// (or the subset named in cfg.Entities). Call AFTER Entity registrations.
//
// Returns the app for fluent chaining. Panics if the audit table cannot be
// created — this is initialization-time work so loud failure is preferable
// to silent log loss.
func (a *App) WithAuditLog(cfg AuditConfig) *App {
	if a.DB == nil {
		panic("framework: WithAuditLog requires a database (use WithDB)")
	}
	table := cfg.Table
	if table == "" {
		table = "audit_log"
	}
	if err := EnsureAuditTable(a.DB, table); err != nil {
		panic(fmt.Sprintf("framework: EnsureAuditTable: %v", err))
	}

	want := map[string]bool{}
	for _, name := range cfg.Entities {
		want[name] = true
	}

	for name, ent := range a.Registry.All() {
		if len(want) > 0 && !want[name] {
			continue
		}
		ent := ent
		pk := "id"
		hr := a.HookRegistry(name)

		hr.RegisterHook(hook.AfterCreate, func(ctx context.Context, data any) error {
			row, _ := data.(map[string]any)
			id := stringifyPK(row, pk)
			redacted := cfg.applyRedact(ent.GetName(), row)
			diff := buildAuditCreateDiff(redacted, row, auditMeta(ctx))
			return writeAuditRow(ctx, a.DB, table, ent.GetName(), auditOpCreate, id, cfg.actor(ctx), diff)
		})
		hr.RegisterHook(hook.AfterUpdate, func(ctx context.Context, data any) error {
			row, _ := data.(map[string]any)
			id := stringifyPK(row, pk)
			redactedNew := cfg.applyRedact(ent.GetName(), row)
			var redactedOld map[string]any
			var originalOld map[string]any
			if pre := crud.AuditPreImageFromContext(ctx); pre != nil {
				originalOld = pre
				redactedOld = cfg.applyRedact(ent.GetName(), pre)
			}
			diff := buildAuditUpdateDiff(redactedOld, redactedNew, originalOld, row, auditMeta(ctx))
			return writeAuditRow(ctx, a.DB, table, ent.GetName(), auditOpUpdate, id, cfg.actor(ctx), diff)
		})
		hr.RegisterHook(hook.AfterDelete, func(ctx context.Context, data any) error {
			originalID, _ := data.(string)
			recordID := originalID
			// Let Redact substitute the natural-key record_id for
			// PHI-bearing tables. Input shape mirrors what the doc
			// describes: a one-key map the host can transform.
			//
			// If the callback omits the "id" key entirely, fall back
			// to the original — silently erasing the natural key
			// gives a malformed redact callback an audit-forensics
			// erasure primitive (see
			// TestAudit_DeleteRedactOmittedIDKeepsOriginalRecordID).
			if cfg.Redact != nil {
				redacted := cfg.applyRedact(ent.GetName(), map[string]any{"id": originalID})
				if v, ok := redacted["id"]; ok {
					recordID = fmt.Sprintf("%v", v)
				}
				// else: keep recordID = originalID
			}
			// Snapshot the deleted row's pre-image (if doDelete
			// captured one) so the audit row records WHAT was
			// deleted, not just WHO did it. The pre-image goes
			// through the same redact pipeline as create/update.
			var diff []byte
			pre := crud.AuditPreImageFromContext(ctx)
			meta := auditMeta(ctx)
			if pre != nil {
				redactedOld := cfg.applyRedact(ent.GetName(), pre)
				diff = buildAuditDeleteDiff(redactedOld, pre, meta)
			} else if meta != nil {
				diff = buildAuditDeleteDiff(nil, nil, meta)
			}
			return writeAuditRow(ctx, a.DB, table, ent.GetName(), auditOpDelete, sanitizeAuditField(recordID), cfg.actor(ctx), diff)
		})
	}
	return a
}

// actor returns the resolved actor id, recovering from any panic in the
// host-provided callback (audit must never crash the request it's
// auditing — losing the actor is preferable to aborting the user write
// or to losing audit coverage entirely). The result is passed through
// sanitizeAuditField to defuse log-injection via control characters.
func (c AuditConfig) actor(ctx context.Context) string {
	if c.Actor == nil {
		return ""
	}
	var out string
	func() {
		defer func() {
			if r := recover(); r != nil {
				out = ""
			}
		}()
		out = c.Actor(ctx)
	}()
	return sanitizeAuditField(out)
}

// sanitizeAuditField strips control bytes (including newlines and NUL)
// from fields that land in the audit row's TEXT columns. Those columns
// are queried and emitted verbatim by forensics tools, so an injected
// `\n` or `\x00` would let a malicious actor or redact callback forge
// log lines / split records. Drop everything below 0x20 plus DEL.
func sanitizeAuditField(s string) string {
	if s == "" {
		return s
	}
	if !strings.ContainsFunc(s, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// defaultSensitiveSuffixes lists field-name suffixes the framework
// scrubs from the audit `diff` column when the host did NOT set a
// custom Redact callback. Match is case-insensitive: `password`,
// `Password`, `password_hash`, `api_key`, `oauth_token`, and
// `recovery_secret` all qualify. The list is intentionally narrow —
// any host with broader scrubbing needs should provide a Redact
// callback.
var defaultSensitiveSuffixes = []string{
	"password",
	"password_hash",
	"passwordhash",
	"secret",
	"token",
	"api_key",
	"apikey",
	"auth_key",
	"authkey",
	"private_key",
	"privatekey",
	"access_token",
	"accesstoken",
	"refresh_token",
	"refreshtoken",
	"session_key",
	"sessionkey",
}

// defaultRedact removes fields whose names match a known-sensitive
// suffix from a defensive copy of the row. Called when
// AuditConfig.Redact is nil so production callers don't accidentally
// leak secrets through the audit log just by forgetting to configure
// a Redact callback.
func defaultRedact(row map[string]any) map[string]any {
	if len(row) == 0 {
		return row
	}
	out := make(map[string]any, len(row))
	for k, v := range row {
		if isDefaultSensitiveField(k) {
			continue
		}
		out[k] = v
	}
	return out
}

func isDefaultSensitiveField(name string) bool {
	lower := strings.ToLower(name)
	for _, suf := range defaultSensitiveSuffixes {
		if lower == suf || strings.HasSuffix(lower, "_"+suf) {
			return true
		}
	}
	return false
}

// auditMeta extracts client IP and User-Agent from the live request
// (if CRUD attached one to ctx) so audit rows record where a write
// came from. Returns nil when there's no request — async /
// system-driven hooks omit the meta block rather than emitting empty
// fields.
func auditMeta(ctx context.Context) map[string]any {
	r := crud.AuditRequestFromContext(ctx)
	if r == nil {
		return nil
	}
	meta := map[string]any{}
	if ip := clientIP(r); ip != "" {
		meta["client_ip"] = ip
	}
	if ua := r.UserAgent(); ua != "" {
		meta["user_agent"] = ua
	}
	if len(meta) == 0 {
		return nil
	}
	return meta
}

// clientIP returns the remote address with the trailing port stripped.
// Audit forensics needs the source IP literal; the port is
// per-connection and useless after the fact.
func clientIP(r *http.Request) string {
	addr := r.RemoteAddr
	if addr == "" {
		return ""
	}
	if i := strings.LastIndex(addr, ":"); i >= 0 {
		return addr[:i]
	}
	return addr
}

// buildAuditCreateDiff renders `{"new": redacted, "meta": {...}}`.
// Falls back to the unredacted row if the redacted form contains
// something json.Marshal can't represent (functions, channels,
// circular references) — audit evidence must not vanish when a host
// returns a malformed redact output, because that turns the redact
// callback into an audit-erasure primitive.
func buildAuditCreateDiff(redacted, original map[string]any, meta map[string]any) []byte {
	payload := map[string]any{"new": redacted}
	if meta != nil {
		payload["meta"] = meta
	}
	if diff, err := json.Marshal(payload); err == nil {
		return diff
	}
	payload["new"] = original
	if diff, err := json.Marshal(payload); err == nil {
		return diff
	}
	return []byte(`{"new":{}}`)
}

// buildAuditUpdateDiff renders `{"old": redactedOld, "new":
// redactedNew, "meta": {...}}`. Same evidence-preserving fallback
// chain as buildAuditCreateDiff. The "old" block is omitted (rather
// than set to null) when no pre-image was captured — keeps the
// column shape predictable for older callers that grep for `"old"`.
func buildAuditUpdateDiff(redactedOld, redactedNew, originalOld, originalNew map[string]any, meta map[string]any) []byte {
	payload := map[string]any{"new": redactedNew}
	if redactedOld != nil {
		payload["old"] = redactedOld
	}
	if meta != nil {
		payload["meta"] = meta
	}
	if diff, err := json.Marshal(payload); err == nil {
		return diff
	}
	// First fallback: keep meta + old, swap new for the unredacted
	// pre-marshal value. If only the new side was poisoned, this
	// recovers everything else.
	payload["new"] = originalNew
	if diff, err := json.Marshal(payload); err == nil {
		return diff
	}
	// Second fallback: also swap old for the unredacted pre-image.
	if originalOld != nil {
		payload["old"] = originalOld
		if diff, err := json.Marshal(payload); err == nil {
			return diff
		}
	}
	return []byte(`{"new":{}}`)
}

// buildAuditDeleteDiff renders `{"old": redactedOld, "meta": {...}}`
// for the deleted record snapshot. Returns nil when neither a
// pre-image nor any meta was captured — preserves the legacy
// behaviour where async delete hooks emit a NULL diff column.
func buildAuditDeleteDiff(redactedOld, originalOld map[string]any, meta map[string]any) []byte {
	if redactedOld == nil && meta == nil {
		return nil
	}
	payload := map[string]any{}
	if redactedOld != nil {
		payload["old"] = redactedOld
	}
	if meta != nil {
		payload["meta"] = meta
	}
	if diff, err := json.Marshal(payload); err == nil {
		return diff
	}
	if originalOld != nil {
		payload["old"] = originalOld
		if diff, err := json.Marshal(payload); err == nil {
			return diff
		}
	}
	return []byte(`{"old":{}}`)
}

// applyRedact runs c.Redact if set, otherwise applies a default
// sensitive-field scrub (see defaultSensitiveSuffixes). Always
// defensive-copies the row before handing it to a host callback so
// a callback that mutates its input can't corrupt the live response
// payload — the docstring warns against mutation but the runtime
// enforces it.
//
// When Redact returns nil, substitutes an empty map so the audit
// diff JSON is `{"new":{}}` rather than `{"new":null}` — matching
// the "nil equivalent to empty map" contract on
// AuditConfig.Redact.
//
// A panicking Redact must not abort the user's transaction. The
// docstring already declares panics out-of-contract; recovering
// turns the contract violation into a soft fallback (the original
// pre-redact row) instead of a hard process crash.
func (c AuditConfig) applyRedact(entity string, row map[string]any) map[string]any {
	if c.Redact == nil {
		return defaultRedact(row)
	}
	copied := make(map[string]any, len(row))
	for k, v := range row {
		copied[k] = v
	}
	var out map[string]any
	panicked := false
	func() {
		defer func() {
			if r := recover(); r != nil {
				panicked = true
			}
		}()
		out = c.Redact(entity, copied)
	}()
	if panicked {
		return row
	}
	if out == nil {
		return map[string]any{}
	}
	return out
}

func stringifyPK(row map[string]any, pk string) string {
	if row == nil {
		return ""
	}
	if v, ok := row[pk]; ok {
		return fmt.Sprintf("%v", v)
	}
	// Case-insensitive fallback: handlers may return camelCase keys.
	for k, v := range row {
		if k == pk || k == "id" || k == "ID" {
			return fmt.Sprintf("%v", v)
		}
	}
	return ""
}

func writeAuditRow(ctx context.Context, db *sql.DB, table, ent string, op auditOp, recordID, actor string, diff []byte) error {
	// 8 bytes of crypto/rand suffix to defeat the "two concurrent
	// hooks land in the same nanosecond" PK collision that took the
	// session-ID code down before it was fixed in core-ui/uihost.
	// A collision here rolls back the user's transaction along with
	// the audit row.
	var rb [8]byte
	_, _ = cryptorand.Read(rb[:])
	id := "aud_" + strconv.FormatInt(time.Now().UnixNano(), 10) + "_" + hex.EncodeToString(rb[:])
	var diffArg any
	if diff != nil {
		diffArg = string(diff)
	}
	// Prefer the active CRUD transaction when present — keeps the audit row
	// atomic with the change it describes. Outside a tx (e.g. async hooks)
	// we fall back to the plain pool.
	var exec interface {
		ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	} = db
	if tx, ok := TxFromContext(ctx); ok {
		exec = tx
	}
	safeTable, err := query.SafeIdent(table)
	if err != nil {
		return fmt.Errorf("audit: invalid table name %q: %w", table, err)
	}
	// Stamp the current tenant (if any) so multi-tenant apps can scope the
	// audit trail per tenant. Sanitised like the other TEXT columns to
	// defuse log-injection via control characters.
	tenantID := sanitizeAuditField(tenant.GetTenantID(ctx))
	_, err = exec.ExecContext(ctx,
		fmt.Sprintf("INSERT INTO %s (id, entity, op, record_id, actor_id, tenant_id, created_at, diff) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)", query.QuoteIdent(safeTable)),
		id, ent, string(op), recordID, nullIfEmpty(actor), nullIfEmpty(tenantID), time.Now().UTC(), diffArg,
	)
	return err
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
