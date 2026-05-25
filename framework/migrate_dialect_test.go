package framework

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

// TestSqlType_DialectMatrix locks in the SQL types emitted per (FieldType,
// Dialect). The Postgres-specific changes (TIMESTAMPTZ, DOUBLE PRECISION,
// JSONB) are the load-bearing ones; SQLite-side rows pin behaviour we don't
// want to drift.
func TestSqlType_DialectMatrix(t *testing.T) {
	cases := []struct {
		field        schema.Field
		wantSQLite   string
		wantPostgres string
	}{
		{schema.Field{Type: schema.String}, "TEXT", "TEXT"},
		{schema.Field{Type: schema.Text}, "TEXT", "TEXT"},
		{schema.Field{Type: schema.Int}, "INTEGER", "INTEGER"},
		{schema.Field{Type: schema.Float}, "REAL", "DOUBLE PRECISION"},
		{schema.Field{Type: schema.Bool}, "BOOLEAN", "BOOLEAN"},
		{schema.Field{Type: schema.Decimal}, "DECIMAL(19,4)", "DECIMAL(19,4)"},
		{schema.Field{Type: schema.Enum}, "TEXT", "TEXT"},
		{schema.Field{Type: schema.UUID}, "TEXT", "TEXT"},
		{schema.Field{Type: schema.Timestamp}, "DATETIME", "TIMESTAMPTZ"},
		{schema.Field{Type: schema.Date}, "DATE", "DATE"},
		{schema.Field{Type: schema.JSON}, "TEXT", "JSONB"},
		{schema.Field{Type: schema.Relation}, "TEXT", "TEXT"},
		{schema.Field{Type: schema.Image}, "TEXT", "TEXT"},
		{schema.Field{Type: schema.File}, "TEXT", "TEXT"},
	}
	for _, c := range cases {
		if got := migrate.SQLType(c.field, DialectSQLite); got != c.wantSQLite {
			t.Errorf("migrate.SQLType(%v, sqlite) = %q, want %q", c.field.Type, got, c.wantSQLite)
		}
		if got := migrate.SQLType(c.field, DialectPostgres); got != c.wantPostgres {
			t.Errorf("migrate.SQLType(%v, postgres) = %q, want %q", c.field.Type, got, c.wantPostgres)
		}
	}
}

// TestColumnDefaultClause_AutoUUID pins the V3 #8 fix: AutoUUID columns
// on Postgres get DEFAULT gen_random_uuid() so raw-SQL INSERTs that
// omit the id column succeed instead of crashing with a NOT NULL
// constraint violation. SQLite has no built-in UUID generator —
// the column stays app-managed there.
func TestColumnDefaultClause_AutoUUID(t *testing.T) {
	f := schema.Field{Name: "id", Type: schema.String, AutoGenerate: schema.AutoUUID}
	if got := migrate.ColumnDefaultClause(f, DialectPostgres); got != " DEFAULT gen_random_uuid()" {
		t.Errorf("postgres AutoUUID default: got %q, want %q", got, " DEFAULT gen_random_uuid()")
	}
	if got := migrate.ColumnDefaultClause(f, DialectSQLite); got != "" {
		t.Errorf("sqlite AutoUUID default: got %q, want empty (no built-in UUID generator)", got)
	}
}

// TestColumnDefaultClause_LiteralDefaultsStillWork guards the regression
// path: explicit f.Default values must continue to render via SQLDefault
// — the AutoUUID branch must not shadow them.
func TestColumnDefaultClause_LiteralDefaultsStillWork(t *testing.T) {
	cases := []struct {
		f       schema.Field
		dialect Dialect
		want    string
	}{
		{schema.Field{Default: "hello"}, DialectPostgres, " DEFAULT 'hello'"},
		{schema.Field{Default: 42}, DialectSQLite, " DEFAULT 42"},
		{schema.Field{Default: true}, DialectPostgres, " DEFAULT TRUE"},
		{schema.Field{Default: true}, DialectSQLite, " DEFAULT 1"},
		// AutoUUID + explicit Default → Default wins (operator intent).
		{schema.Field{AutoGenerate: schema.AutoUUID, Default: "00000000-0000-0000-0000-000000000000"}, DialectPostgres,
			" DEFAULT '00000000-0000-0000-0000-000000000000'"},
		// No default and no AutoUUID → empty.
		{schema.Field{Type: schema.String}, DialectPostgres, ""},
		{schema.Field{Type: schema.String}, DialectSQLite, ""},
		// AutoTimestamp does NOT auto-emit a DEFAULT — framework hooks set
		// created_at/updated_at app-side. Don't broaden the auto-default
		// surface beyond what V3 #8 asked for.
		{schema.Field{Type: schema.Timestamp, AutoGenerate: schema.AutoTimestamp}, DialectPostgres, ""},
	}
	for i, c := range cases {
		if got := migrate.ColumnDefaultClause(c.f, c.dialect); got != c.want {
			t.Errorf("case %d (%v/%v): got %q, want %q", i, c.f, c.dialect, got, c.want)
		}
	}
}

// TestSqlDefault_BoolDialectIdiom pins the bool rendering: SQLite keeps the
// historic 1/0 form, Postgres emits TRUE/FALSE so pg_dump round-trips
// idiomatically.
func TestSqlDefault_BoolDialectIdiom(t *testing.T) {
	cases := []struct {
		v            bool
		wantSQLite   string
		wantPostgres string
	}{
		{true, "1", "TRUE"},
		{false, "0", "FALSE"},
	}
	for _, c := range cases {
		f := schema.Field{Default: c.v}
		if got := migrate.SQLDefault(f, DialectSQLite); got != c.wantSQLite {
			t.Errorf("migrate.SQLDefault(%v, sqlite) = %q, want %q", c.v, got, c.wantSQLite)
		}
		if got := migrate.SQLDefault(f, DialectPostgres); got != c.wantPostgres {
			t.Errorf("migrate.SQLDefault(%v, postgres) = %q, want %q", c.v, got, c.wantPostgres)
		}
	}
}
