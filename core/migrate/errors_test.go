package migrate

import (
	"context"
	"strings"
	"testing"
)

// TestChecksumMismatchError_Message pins the human-facing drift message and
// exercises the short() hash truncation.
func TestChecksumMismatchError_Message(t *testing.T) {
	e := &ChecksumMismatchError{
		Version:  7,
		Name:     "add_index",
		Recorded: "abcdef0123456789abcdef",
		Current:  "0011223344556677889900",
	}
	msg := e.Error()
	for _, want := range []string{"7", "add_index", "abcdef012345", "001122334455", "modified after"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error message missing %q: %s", want, msg)
		}
	}
	// short() must not truncate a value already ≤12 chars.
	if got := short("abc"); got != "abc" {
		t.Errorf("short(abc) = %q, want abc", got)
	}
}

// TestParseMigration_BadVersion covers the version-parse error branch.
func TestParseMigration_BadVersion(t *testing.T) {
	m, _ := newSQLiteMigrator(t)
	content := `-- +migrate Version notanumber
-- +migrate Up
SELECT 1;`
	if err := m.RegisterFromReader(strings.NewReader(content)); err == nil {
		t.Fatal("expected an error parsing a non-numeric version")
	}
}

// TestUp_BadUpSQLErrors covers the runMigrationUp failure path: a migration
// whose Up is invalid must surface an error and not record the version.
func TestUp_BadUpSQLErrors(t *testing.T) {
	m, db := newSQLiteMigrator(t)
	ctx := context.Background()
	m.Register(Migration{Version: 1, Name: "bad", Up: "THIS IS NOT SQL", Down: ""})
	if err := m.Up(ctx); err == nil {
		t.Fatal("expected Up to fail on invalid SQL")
	}
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM _migrations").Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Fatalf("failed migration recorded a row (%d); the tx should have rolled back", n)
	}
}

// TestDown_BadDownSQLErrors covers the runMigrationDown failure path.
func TestDown_BadDownSQLErrors(t *testing.T) {
	m, _ := newSQLiteMigrator(t)
	ctx := context.Background()
	m.Register(Migration{Version: 1, Name: "ok", Up: "CREATE TABLE d1 (id INTEGER)", Down: "NOT VALID SQL"})
	if err := m.Up(ctx); err != nil {
		t.Fatalf("Up: %v", err)
	}
	if err := m.Down(ctx, 1); err == nil {
		t.Fatal("expected Down to fail on invalid Down SQL")
	}
	// The version must still be recorded as applied (rollback undid the delete).
	st, err := m.Status(ctx)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(st.Applied) != 1 {
		t.Fatalf("expected version still applied after failed Down, got %+v", st.Applied)
	}
}

// TestInvalidTableName covers the SafeIdent failure branches across the runner
// entry points.
func TestInvalidTableName(t *testing.T) {
	_, db := newSQLiteMigrator(t)
	ctx := context.Background()
	m := New(db, WithDialect(DialectSQLite), WithTableName("bad name; DROP TABLE x"))

	if err := m.CreateMigrationsTable(ctx); err == nil {
		t.Error("expected CreateMigrationsTable to reject an invalid table name")
	}
	if err := m.Up(ctx); err == nil {
		t.Error("expected Up to reject an invalid table name")
	}
	if err := m.Force(ctx, 1, true); err == nil {
		t.Error("expected Force to reject an invalid table name")
	}
}
