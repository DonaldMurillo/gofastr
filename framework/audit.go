package framework

import (
	"context"
	cryptorand "crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/framework/hook"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
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
	// Use this to keep CRUD audit coverage on PHI-bearing entities
	// without leaking content into the audit log — e.g. for a search-
	// history table return `map[string]any{"id": row["id"]}` so the
	// audit still records who searched when, but not what.
	//
	// Redact is invoked for AfterCreate, AfterUpdate, AND AfterDelete.
	// On delete the input is `map[string]any{"id": <record_id>}`; the
	// returned map's "id" value (if any) becomes the audit row's
	// record_id, letting hosts pseudonymise the natural key on delete
	// too.
	//
	// Returning nil is equivalent to returning an empty map. The
	// callback must not panic. Failures inside Redact don't bubble up
	// — write something safe if a key is missing.
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
		created_at  %s NOT NULL,
		diff        TEXT
	)`, query.QuoteIdent(safeTable), tsType)
	_, err = db.Exec(stmt)
	return err
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
			diff, _ := json.Marshal(map[string]any{"new": cfg.applyRedact(ent.GetName(), row)})
			return writeAuditRow(ctx, a.DB, table, ent.GetName(), auditOpCreate, id, cfg.actor(ctx), diff)
		})
		hr.RegisterHook(hook.AfterUpdate, func(ctx context.Context, data any) error {
			row, _ := data.(map[string]any)
			id := stringifyPK(row, pk)
			diff, _ := json.Marshal(map[string]any{"new": cfg.applyRedact(ent.GetName(), row)})
			return writeAuditRow(ctx, a.DB, table, ent.GetName(), auditOpUpdate, id, cfg.actor(ctx), diff)
		})
		hr.RegisterHook(hook.AfterDelete, func(ctx context.Context, data any) error {
			id, _ := data.(string)
			// Let Redact substitute the natural-key record_id for
			// PHI-bearing tables. Input shape mirrors what the doc
			// describes: a one-key map the host can transform.
			if cfg.Redact != nil {
				redacted := cfg.applyRedact(ent.GetName(), map[string]any{"id": id})
				if v, ok := redacted["id"]; ok {
					id = fmt.Sprintf("%v", v)
				} else {
					id = ""
				}
			}
			return writeAuditRow(ctx, a.DB, table, ent.GetName(), auditOpDelete, id, cfg.actor(ctx), nil)
		})
	}
	return a
}

func (c AuditConfig) actor(ctx context.Context) string {
	if c.Actor == nil {
		return ""
	}
	return c.Actor(ctx)
}

// applyRedact runs c.Redact if set. Passes the row through untouched
// when Redact is nil so existing audit-coverage behavior is unaffected.
//
// Defensive-copies the row before handing it to Redact so a host
// callback that mutates its input can't corrupt the live response
// payload — the docstring warns against mutation but the runtime
// enforces it. Cheap (one map allocation per audit row) and removes
// a foot-shaped landmine from the API.
//
// When Redact returns nil, substitutes an empty map so the audit diff
// JSON is `{"new":{}}` rather than `{"new":null}` — matching the
// "nil equivalent to empty map" contract on AuditConfig.Redact.
func (c AuditConfig) applyRedact(entity string, row map[string]any) map[string]any {
	if c.Redact == nil {
		return row
	}
	copied := make(map[string]any, len(row))
	for k, v := range row {
		copied[k] = v
	}
	out := c.Redact(entity, copied)
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
	_, err = exec.ExecContext(ctx,
		fmt.Sprintf("INSERT INTO %s (id, entity, op, record_id, actor_id, created_at, diff) VALUES ($1, $2, $3, $4, $5, $6, $7)", query.QuoteIdent(safeTable)),
		id, ent, string(op), recordID, nullIfEmpty(actor), time.Now().UTC(), diffArg,
	)
	return err
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
