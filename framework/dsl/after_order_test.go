package dsl

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// TestDSLAfterEmitsOneOrderBy pins after()'s SQL to the shape crud's
// cursor path emits: the keyset predicate plus ONE ORDER BY on the
// cursor column. query.Cursor appended its own ORDER BY after order()'s,
// so the documented example order(id).after(…) ordered by id twice.
func TestDSLAfterEmitsOneOrderBy(t *testing.T) {
	reg := postsRegistry(t, nil)
	for _, q := range []string{
		`posts.where(status="published").order(id).after("p1024").limit(20)`,
		`posts.where(status="published").after("p1024").limit(20)`,
		`posts.order(id ASC).order(id).after("p1024")`,
	} {
		qb, err := BuildDSLQuery(reg, q)
		if err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		sqlStr, _ := qb.Build()
		if strings.Count(sqlStr, "ORDER BY") != 1 || strings.Count(sqlStr, "id ASC") != 1 || !strings.Contains(sqlStr, "id > $") {
			t.Fatalf("%s: sql = %s, want one ORDER BY id ASC and the keyset predicate", q, sqlStr)
		}
	}
}

// TestDSLAfterRefusesForeignOrder pins the refusal of an order() that
// sorts the rows by anything but the cursor column ascending. after()
// keysets on the cursor column, so order(seq).after(<id>) sorted by seq
// while cutting on id, and walking it skipped and repeated rows (the
// walk below served some rows twice and others never).
func TestDSLAfterRefusesForeignOrder(t *testing.T) {
	reg := postsRegistry(t, nil)
	for _, q := range []string{
		`posts.order(seq ASC).after("c").limit(2)`,
		`posts.order(id DESC).after("c").limit(2)`,
		`posts.order(id).order(seq).after("c")`,
	} {
		if _, err := BuildDSLQuery(reg, q); err == nil {
			t.Errorf("%s: accepted; after() pages on id ascending", q)
		}
	}

	// Walking after() the documented way serves every row exactly once.
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver not available")
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE posts (id TEXT PRIMARY KEY, status TEXT, seq INTEGER NOT NULL, created_at TEXT, updated_at TEXT)`); err != nil {
		t.Fatal(err)
	}
	for i, id := range []string{"a", "b", "c", "d", "e", "f"} {
		if _, err := db.Exec(`INSERT INTO posts (id, status, seq) VALUES (?, 'x', ?)`, id, 6-i); err != nil {
			t.Fatal(err)
		}
	}
	seen := map[string]int{}
	after := ""
	for page := 0; page < 10; page++ {
		q := `posts.order(id).limit(2)`
		if after != "" {
			q = `posts.order(id).after("` + after + `").limit(2)`
		}
		qb, err := BuildDSLQuery(reg, q)
		if err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		sqlStr, args := qb.Build()
		rows, err := db.QueryContext(context.Background(), "SELECT id FROM ("+sqlStr+")", args...)
		if err != nil {
			t.Fatalf("query %s: %v", sqlStr, err)
		}
		n := 0
		for rows.Next() {
			if err := rows.Scan(&after); err != nil {
				t.Fatal(err)
			}
			seen[after]++
			n++
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		rows.Close()
		if n < 2 {
			break
		}
	}
	for _, id := range []string{"a", "b", "c", "d", "e", "f"} {
		if seen[id] != 1 {
			t.Errorf("row %q served %d times walking order(id).after(id); want once (seen=%v)", id, seen[id], seen)
		}
	}
}
