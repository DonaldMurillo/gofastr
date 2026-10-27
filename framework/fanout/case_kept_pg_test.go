package fanout

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/pgtest"
)

// An older build kept a configured "Msgs" fallback table case-kept. This
// build names msgs, so NewPostgres refuses, with or without
// WithoutEnsureTable, and prints the rename.
func TestPostgres_CaseKeptTableRefused(t *testing.T) {
	dsn := pgtest.FreshDatabaseDSN(t)
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`CREATE TABLE "Msgs" (id TEXT PRIMARY KEY)`); err != nil {
		t.Fatalf("create the older table: %v", err)
	}
	f, err := NewPostgres(dsn, db, WithTableName("Msgs"), WithoutEnsureTable())
	if err == nil {
		_ = f.Close()
	}
	if err == nil || !strings.Contains(err.Error(), `ALTER TABLE "Msgs" RENAME TO "msgs"`) {
		t.Fatalf("NewPostgres over \"Msgs\": err = %v, want the rename advice", err)
	}
}
