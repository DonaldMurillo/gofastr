package admin

import (
	"context"
	"database/sql"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/internal/pgtest"
)

// framework.EnsureAuditTable and AppendAuditEvent quote the configured
// audit table, so Postgres keeps a mixed-case "AuditLog" as written. The
// audit page's reads must name that same table, not the folded auditlog
// an unquoted name reaches.
func TestMixedCaseAuditTable(t *testing.T) {
	run := func(t *testing.T, db *sql.DB) {
		ctx := context.Background()
		if err := framework.EnsureAuditTable(db, "AuditLog"); err != nil {
			t.Fatalf("EnsureAuditTable: %v", err)
		}
		if err := framework.AppendAuditEvent(ctx, db, "AuditLog", "notes", "create", "n1", "u1", nil); err != nil {
			t.Fatalf("AppendAuditEvent: %v", err)
		}
		b := New(Config{AuditTable: "AuditLog"})
		b.db = db
		if n, err := b.countAuditWhere(ctx, auditFilter{}); err != nil || n != 1 {
			t.Fatalf("countAuditWhere = %d, %v; want 1", n, err)
		}
		if rows, err := b.queryAudit(ctx, 10); err != nil || len(rows) != 1 {
			t.Fatalf("queryAudit = %d rows, %v; want 1", len(rows), err)
		}
	}
	t.Run("sqlite", func(t *testing.T) { run(t, newDB(t)) })
	t.Run("postgres", func(t *testing.T) { run(t, pgtest.DB(t)) })
}
