package sqlite

import "testing"

func TestPreparedStatementAcceptsNumberedPlaceholders(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	if _, err := db.Exec("CREATE TABLE prepared_records (id TEXT PRIMARY KEY, value TEXT)"); err != nil {
		t.Fatal(err)
	}
	stmt, err := db.Prepare("INSERT INTO prepared_records (id, value) VALUES ($2, $1)")
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	defer stmt.Close()
	if _, err := stmt.Exec("prepared-value", "prepared-id"); err != nil {
		t.Fatalf("prepared numbered insert: %v", err)
	}
}
