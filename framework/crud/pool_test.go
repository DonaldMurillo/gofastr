package crud

import (
	"database/sql"
	"testing"
)

func TestBorrowReturnRowSlice(t *testing.T) {
	s := borrowRowSlice()
	*s = append(*s, map[string]any{"a": 1})
	*s = append(*s, map[string]any{"b": 2})
	if len(*s) != 2 {
		t.Fatalf("len = %d, want 2", len(*s))
	}
	returnRowSlice(s)
	// After return, the slice should be reusable
	s2 := borrowRowSlice()
	if len(*s2) != 0 {
		t.Fatalf("len after reuse = %d, want 0", len(*s2))
	}
	returnRowSlice(s2)
}

func TestBorrowReturnPtrSlice(t *testing.T) {
	p := borrowPtrSlice(5)
	if len(*p) != 5 {
		t.Fatalf("len = %d, want 5", len(*p))
	}
	returnPtrSlice(p)
	p2 := borrowPtrSlice(3)
	if len(*p2) != 3 {
		t.Fatalf("len after reuse = %d, want 3", len(*p2))
	}
	returnPtrSlice(p2)
}

func TestScanRowsPooledEmpty(t *testing.T) {
	// Use an in-memory SQLite to test scanRowsPooled end-to-end.
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver not available")
	}
	defer db.Close()

	_, err = db.Exec("CREATE TABLE t (id INTEGER, name TEXT)")
	if err != nil {
		t.Skipf("sqlite3 not available: %v", err)
	}

	rows, err := db.Query("SELECT id, name FROM t")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	result, err := scanRowsPooled(rows, []string{"id", "name"}, func(s string) string { return s })
	if err != nil {
		t.Fatal(err)
	}
	if len(*result) != 0 {
		t.Fatalf("expected 0 rows, got %d", len(*result))
	}
	returnRowSlice(result)
}

func TestScanRowsPooledWithData(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver not available")
	}
	defer db.Close()

	_, err = db.Exec("CREATE TABLE t (id INTEGER, name TEXT)")
	if err != nil {
		t.Skipf("sqlite3 not available: %v", err)
	}
	_, err = db.Exec("INSERT INTO t (id, name) VALUES (1, 'alice'), (2, 'bob')")
	if err != nil {
		t.Fatal(err)
	}

	rows, err := db.Query("SELECT id, name FROM t")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	result, err := scanRowsPooled(rows, []string{"id", "name"}, func(s string) string { return s })
	if err != nil {
		t.Fatal(err)
	}
	if len(*result) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(*result))
	}
	row0 := (*result)[0]
	if row0["name"] != "alice" {
		t.Errorf("row0 name = %v, want alice", row0["name"])
	}
	row1 := (*result)[1]
	if row1["name"] != "bob" {
		t.Errorf("row1 name = %v, want bob", row1["name"])
	}
	returnRowSlice(result)
}

func TestScanRowsPooledKeyFunc(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver not available")
	}
	defer db.Close()

	_, err = db.Exec("CREATE TABLE t (my_col INTEGER)")
	if err != nil {
		t.Skipf("sqlite3 not available: %v", err)
	}
	_, err = db.Exec("INSERT INTO t (my_col) VALUES (42)")
	if err != nil {
		t.Fatal(err)
	}

	rows, err := db.Query("SELECT my_col FROM t")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	// keyFunc converts column names to uppercase
	result, err := scanRowsPooled(rows, []string{"my_col"}, func(s string) string {
		return "MY_COL"
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(*result) != 1 {
		t.Fatalf("expected 1 row, got %d", len(*result))
	}
	v, ok := (*result)[0]["MY_COL"]
	if !ok {
		t.Error("key MY_COL not found in row")
	} else if v == nil {
		t.Error("MY_COL value is nil")
	}
	returnRowSlice(result)
}
