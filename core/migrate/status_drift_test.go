package migrate

import (
	"context"
	"testing"
)

// Status is the read an operator runs to see why Up or Down refuses. It
// reported "Applied: 3, Pending: 0" while Up refused an edited applied
// migration and Down refused an applied one with no registered file, so
// the diagnostic said all was well. It lists both now.
func TestStatusReportsModifiedAndUnregistered(t *testing.T) {
	m1, db := newSQLiteMigrator(t)
	ctx := context.Background()
	m1.Register(Migration{Version: 1, Name: "a", Up: "CREATE TABLE a (id INTEGER)", Down: "DROP TABLE a"})
	m1.Register(Migration{Version: 2, Name: "b", Up: "CREATE TABLE b (id INTEGER)", Down: "DROP TABLE b"})
	m1.Register(Migration{Version: 3, Name: "c", Up: "CREATE TABLE c (id INTEGER)", Down: "DROP TABLE c"})
	m1.Register(Migration{Version: 1, Name: "k", Group: "knowledge", Up: "CREATE TABLE k (id INTEGER)", Down: "DROP TABLE k"})
	if err := m1.Up(ctx); err != nil {
		t.Fatalf("Up: %v", err)
	}

	// Version 1 edited after apply, version 3's file deleted, and the
	// knowledge module disabled (its rows are its own, not drift).
	m2 := New(db, WithDialect(DialectSQLite))
	m2.Register(Migration{Version: 1, Name: "a", Up: "CREATE TABLE a (id INTEGER, extra INTEGER)", Down: "DROP TABLE a"})
	m2.Register(Migration{Version: 2, Name: "b", Up: "CREATE TABLE b (id INTEGER)", Down: "DROP TABLE b"})
	st, err := m2.Status(ctx)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(st.Modified) != 1 || st.Modified[0].Version != 1 {
		t.Errorf("Modified = %+v, want version 1", st.Modified)
	}
	if len(st.Unregistered) != 1 || st.Unregistered[0].Version != 3 {
		t.Errorf("Unregistered = %+v, want version 3 (the disabled knowledge group is not drift)", st.Unregistered)
	}
	if len(st.Applied) != 4 {
		t.Errorf("Applied = %d rows, want all 4", len(st.Applied))
	}

	// A clean run reports neither.
	st, err = m1.Status(ctx)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(st.Modified) != 0 || len(st.Unregistered) != 0 {
		t.Errorf("clean Status reported drift: modified %+v, unregistered %+v", st.Modified, st.Unregistered)
	}
}
