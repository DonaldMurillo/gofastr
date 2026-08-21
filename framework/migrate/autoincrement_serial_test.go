package migrate

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

// TestSQLType_AutoIncrementSerialOnPostgres pins the dialect-specific rendering
// for an auto-incrementing integer column. On Postgres the column MUST render
// as SERIAL so a real sequence backs it. A plain "INTEGER PRIMARY KEY" has no
// sequence on Postgres and never auto-increments. On SQLite plain INTEGER is
// correct: "INTEGER PRIMARY KEY" aliases the rowid and auto-increments when the
// column is omitted from INSERT.
func TestSQLType_AutoIncrementSerialOnPostgres(t *testing.T) {
	f := schema.Field{Name: "id", Type: schema.Int, AutoGenerate: schema.AutoIncrement}
	if got := SQLType(f, DialectPostgres); got != "SERIAL" {
		t.Errorf("postgres AutoIncrement Int: SQLType = %q, want %q", got, "SERIAL")
	}
	if got := SQLType(f, DialectSQLite); got != "INTEGER" {
		t.Errorf("sqlite AutoIncrement Int: SQLType = %q, want %q (rowid alias)", got, "INTEGER")
	}
}
