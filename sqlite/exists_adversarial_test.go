package sqlite

import "testing"

func TestNotExistsJoinParameterFailsClosed(t *testing.T) {
	db, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	for _, statement := range []string{
		"CREATE TABLE parents (id INTEGER PRIMARY KEY)",
		"CREATE TABLE links (parent_id INTEGER, scope_id INTEGER)",
		"CREATE TABLE scopes (id INTEGER PRIMARY KEY)",
		"INSERT INTO parents VALUES (1)",
		"INSERT INTO links VALUES (1, 7)",
		"INSERT INTO scopes VALUES (7)",
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`DELETE FROM parents AS p
		WHERE NOT EXISTS (
			SELECT 1 FROM links AS l
			JOIN scopes AS s ON s.id=$1 AND s.id=l.scope_id
			WHERE l.parent_id=p.id
		)`, 7); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM parents").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("correlated join false-negative deleted protected row; count=%d", count)
	}
}
