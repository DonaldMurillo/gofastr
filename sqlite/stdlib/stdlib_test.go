package stdlib

import (
	"database/sql"
	"database/sql/driver"
	"testing"

	modernsqlite "modernc.org/sqlite"
)

func TestSQLite3AliasUsesModerncRegisteredFunctions(t *testing.T) {
	const name = "gofastr_stdlib_alias_test_fn"
	if err := modernsqlite.RegisterScalarFunction(name, 0,
		func(_ *modernsqlite.FunctionContext, _ []driver.Value) (driver.Value, error) {
			return int64(42), nil
		}); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var got int64
	if err := db.QueryRow("SELECT " + name + "()").Scan(&got); err != nil {
		t.Fatalf("registered function unavailable through sqlite3 alias: %v", err)
	}
	if got != 42 {
		t.Fatalf("registered function returned %d, want 42", got)
	}
}
