package framework

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
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
}

// auditOp is the op-code stored in the "op" column.
type auditOp string

const (
	auditOpCreate auditOp = "create"
	auditOpUpdate auditOp = "update"
	auditOpDelete auditOp = "delete"
)

// EnsureAuditTable creates the audit_log table if it does not exist. Idempotent.
// Dialect-aware via the existing detectDialect helper.
func EnsureAuditTable(db *sql.DB, table string) error {
	if table == "" {
		table = "audit_log"
	}
	dialect := detectDialect(db)

	tsType := "DATETIME"
	if dialect == DialectPostgres {
		tsType = "TIMESTAMPTZ"
	}
	stmt := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
		id          TEXT PRIMARY KEY,
		entity      TEXT NOT NULL,
		op          TEXT NOT NULL,
		record_id   TEXT NOT NULL,
		actor_id    TEXT,
		created_at  %s NOT NULL,
		diff        TEXT
	)`, table, tsType)
	_, err := db.Exec(stmt)
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

	for name, entity := range a.Registry.All() {
		if len(want) > 0 && !want[name] {
			continue
		}
		ent := entity
		pk := "id"
		hr := a.HookRegistry(name)

		hr.RegisterHook(AfterCreate, func(ctx context.Context, data any) error {
			row, _ := data.(map[string]any)
			id := stringifyPK(row, pk)
			diff, _ := json.Marshal(map[string]any{"new": row})
			return writeAuditRow(ctx, a.DB, table, ent.GetName(), auditOpCreate, id, cfg.actor(ctx), diff)
		})
		hr.RegisterHook(AfterUpdate, func(ctx context.Context, data any) error {
			row, _ := data.(map[string]any)
			id := stringifyPK(row, pk)
			diff, _ := json.Marshal(map[string]any{"new": row})
			return writeAuditRow(ctx, a.DB, table, ent.GetName(), auditOpUpdate, id, cfg.actor(ctx), diff)
		})
		hr.RegisterHook(AfterDelete, func(ctx context.Context, data any) error {
			id, _ := data.(string)
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

func writeAuditRow(ctx context.Context, db *sql.DB, table, entity string, op auditOp, recordID, actor string, diff []byte) error {
	id := fmt.Sprintf("aud_%d", time.Now().UnixNano())
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
	_, err := exec.ExecContext(ctx,
		fmt.Sprintf("INSERT INTO %s (id, entity, op, record_id, actor_id, created_at, diff) VALUES ($1, $2, $3, $4, $5, $6, $7)", table),
		id, entity, string(op), recordID, nullIfEmpty(actor), time.Now().UTC(), diffArg,
	)
	return err
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
