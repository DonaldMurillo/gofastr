package framework

import (
	"context"
	"database/sql"
	"net/http"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/hook"
)

// EnsureAuditTable defaults the table name when empty, and rejects an
// invalid identifier via SafeIdent before issuing DDL.
func TestCovEnsureAuditTable(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		// Empty table → defaults to "audit_log".
		if err := EnsureAuditTable(db, ""); err != nil {
			t.Fatalf("EnsureAuditTable default: %v", err)
		}
		// Invalid identifier → SafeIdent error (line 87).
		if err := EnsureAuditTable(db, "bad-name; DROP TABLE x"); err == nil {
			t.Fatal("expected invalid table-name error")
		}
	})
}

// writeAuditRow re-validates the table name and errors on a bad identifier
// (defensive guard at line 492) without issuing any SQL.
func TestCovWriteAuditRowBadTable(t *testing.T) {
	err := writeAuditRow(context.Background(), nil, "bad-name; DROP", "posts", auditOpCreate, "1", "", nil)
	if err == nil {
		t.Fatal("expected invalid table-name error from writeAuditRow")
	}
}

// WithAuditLog panics without a DB.
func TestCovWithAuditLogNoDB(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware())
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic: WithAuditLog without DB")
		}
	}()
	app.WithAuditLog(AuditConfig{})
}

// WithAuditLog panics when EnsureAuditTable fails (invalid table name).
func TestCovWithAuditLogBadTable(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := NewApp(WithDB(db), WithoutDefaultMiddleware())
		defer func() {
			if recover() == nil {
				t.Fatal("expected panic from EnsureAuditTable failure")
			}
		}()
		app.WithAuditLog(AuditConfig{Table: "bad-name; DROP"})
	})
}

// An audited delete with a request in context but NO pre-image captured
// writes a meta-only delete diff (audit.go line 183 branch).
func TestCovAuditDeleteMetaOnly(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		if _, err := db.Exec(`CREATE TABLE posts (id TEXT PRIMARY KEY, title TEXT NOT NULL)`); err != nil {
			t.Fatalf("create table: %v", err)
		}
		app := NewApp(WithDB(db), WithoutDefaultMiddleware())
		app.Entity("posts", entity.EntityConfig{
			Table:  "posts",
			Fields: []schema.Field{{Name: "title", Type: schema.String, Required: true}},
		}.WithTimestamps(false))
		app.WithAuditLog(AuditConfig{})

		// Fire the registered AfterDelete hook directly with a request in
		// context (auditMeta non-nil) but NO pre-image — hits the
		// meta-only delete-diff branch (audit.go line 183-185), which the
		// CRUD path never reaches because doDelete always captures a
		// pre-image first.
		r := &http.Request{Header: http.Header{}, RemoteAddr: "1.2.3.4:9000"}
		ctx := crud.WithAuditRequest(context.Background(), r)
		if err := app.HookRegistry("posts").ExecuteHooks(ctx, hook.AfterDelete, "rec-123"); err != nil {
			t.Fatalf("AfterDelete hook: %v", err)
		}

		// The delete audit row should carry a meta-only diff (no "old").
		var diff sql.NullString
		err := db.QueryRow(`SELECT diff FROM audit_log WHERE op = 'delete' AND record_id = 'rec-123'`).Scan(&diff)
		if err != nil {
			t.Fatalf("query delete audit row: %v", err)
		}
		if !diff.Valid || diff.String == "" {
			t.Fatalf("expected a meta-bearing delete diff, got %q", diff.String)
		}
	})
}
