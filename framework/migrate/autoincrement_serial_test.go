package migrate

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

// TestSQLType_AutoIncrementSerialOnPostgres pins the dialect-specific rendering
// for an auto-incrementing integer column. On Postgres the column MUST render
// as BIGSERIAL so a real sequence backs it, 64-bit like every Int. A plain
// "BIGINT PRIMARY KEY" has no sequence on Postgres and never auto-increments.
// On SQLite plain INTEGER is correct: "INTEGER PRIMARY KEY" aliases the rowid
// and auto-increments when the column is omitted from INSERT.
func TestSQLType_AutoIncrementSerialOnPostgres(t *testing.T) {
	f := schema.Field{Name: "id", Type: schema.Int, AutoGenerate: schema.AutoIncrement}
	if got := SQLType(f, DialectPostgres); got != "BIGSERIAL" {
		t.Errorf("postgres AutoIncrement Int: SQLType = %q, want %q", got, "BIGSERIAL")
	}
	if got := SQLType(schema.Field{Name: "n", Type: schema.Int}, DialectPostgres); got != "BIGINT" {
		t.Errorf("postgres Int: SQLType = %q, want BIGINT", got)
	}
	if got := SQLType(f, DialectSQLite); got != "INTEGER" {
		t.Errorf("sqlite AutoIncrement Int: SQLType = %q, want %q (rowid alias)", got, "INTEGER")
	}
}
