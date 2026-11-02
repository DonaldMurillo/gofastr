package query

// WHERE fragments run against a real SQLite database (the repo's
// modernc-backed "sqlite3" driver), so each test checks what the engine
// does with the built SQL, not only its spelling.

import (
	"database/sql"
	"reflect"
	"slices"
	"testing"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// postsDB opens an in-memory SQLite database holding three posts across
// two tenants.
func postsDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`CREATE TABLE posts (id TEXT, tenant_id TEXT, status TEXT, name TEXT);
INSERT INTO posts VALUES
  ('a1','A','draft','x'),
  ('b1','B','draft','y'),
  ('b2','B','pub','a%b');`); err != nil {
		t.Fatal(err)
	}
	return db
}

// queryIDs runs q and returns the first column of every row, sorted.
func queryIDs(t *testing.T, db *sql.DB, q string, args []any) []string {
	t.Helper()
	rows, err := db.Query(q, args...)
	if err != nil {
		t.Fatalf("query failed\n  sql:  %q\n  args: %v\n  err:  %v", q, args, err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	slices.Sort(out)
	return out
}

// An E that ends a keyword (ESCAPE'\') is not a Postgres E'…' prefix.
// copyStringLiteral read it as one, took \' for an escaped quote, stayed
// in the literal to the end of the condition and left the second $1
// unrenumbered, so it bound the first argument instead of the second.
func TestKeywordEndingInEDoesNotOpenAnEscapeString(t *testing.T) {
	db := postsDB(t)
	q, args := Select("id").From("posts").
		Where(`name LIKE $1 ESCAPE'\' AND tenant_id = $1`, `a\%b`, "B").
		Build()
	if got, want := queryIDs(t, db, q, args), []string{"b2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v\n  sql: %s", got, want, q)
	}

	for _, tc := range []struct{ in, want string }{
		// An E that ends a word: a plain literal, the later $1 renumbers.
		{`n LIKE'x\' AND id = $1 AND k = $1`, `n LIKE'x\' AND id = $1 AND k = $2`},
		{`CASE WHEN a THEN 1 ELSE'\' END = $1 AND b = $1`, `CASE WHEN a THEN 1 ELSE'\' END = $1 AND b = $2`},
		// An E that starts a token is still the escape-string prefix.
		{`E'it\'s $5' AND id = $1`, `E'it\'s $5' AND id = $1`},
		{`(e'it\'s $5') AND id = $1`, `(e'it\'s $5') AND id = $1`},
	} {
		if got := renumberPlaceholders(tc.in, 1); got != tc.want {
			t.Errorf("renumber %q\n  got  %s\n  want %s", tc.in, got, tc.want)
		}
	}
}

// A condition that ends in a -- line comment used to swallow the
// closing paren Build writes after it, and every clause after that, so
// the statement no longer parsed. Each builder that writes a WHERE must
// end the comment before the paren.
func TestTrailingLineCommentEndsBeforeTheCloseParen(t *testing.T) {
	db := postsDB(t)
	const comment = "status = $1 -- drafts only"

	q, args := Select("id").From("posts").
		Where(comment, "draft").Where("tenant_id = $1", "B").Build()
	if got, want := queryIDs(t, db, q, args), []string{"b1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("select got %v, want %v\n  sql: %q", got, want, q)
	}

	q, args = Count("posts").Where(comment, "draft").Where("tenant_id = $1", "B").Build()
	var n int
	if err := db.QueryRow(q, args...).Scan(&n); err != nil || n != 1 {
		t.Fatalf("count = %d, %v; want 1\n  sql: %q", n, err, q)
	}

	q, args = Update("posts").Set("name", "z").Where(comment, "draft").Where("tenant_id = $1", "B").Build()
	if res, err := db.Exec(q, args...); err != nil {
		t.Fatalf("update: %v\n  sql: %q", err, q)
	} else if rows, _ := res.RowsAffected(); rows != 1 {
		t.Fatalf("update touched %d rows, want 1\n  sql: %q", rows, q)
	}

	q, args = Delete("posts").Where(comment, "draft").Where("tenant_id = $1", "B").Build()
	if res, err := db.Exec(q, args...); err != nil {
		t.Fatalf("delete: %v\n  sql: %q", err, q)
	} else if rows, _ := res.RowsAffected(); rows != 1 {
		t.Fatalf("delete removed %d rows, want 1\n  sql: %q", rows, q)
	}

	// A condition with no open line comment gets no newline.
	q, _ = Select("id").From("posts").Where("a = $1 -- c\n AND b = $1", 1, 2).Build()
	if want := "SELECT id FROM posts WHERE (a = $1 -- c\n AND b = $2)"; q != want {
		t.Errorf("closed comment:\n  got  %q\n  want %q", q, want)
	}
}

// SQLite closes a block comment at the first */, while the renumberer
// nests them the Postgres way. A comment holding a "/*" (a glob like
// /api/*) never closed under the nesting scan, so the $1 after it kept
// its digit and bound the first argument when SQLite ran it.
func TestUnclosedNestedBlockCommentClosesAtFirstEnd(t *testing.T) {
	db := postsDB(t)
	q, args := Select("id").From("posts").
		Where("status = $1 /* see /api/* docs */ AND tenant_id = $1", "draft", "B").
		Build()
	if got, want := queryIDs(t, db, q, args), []string{"b1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v\n  sql: %s", got, want, q)
	}

	for _, tc := range []struct{ in, want string }{
		// Balanced nesting, which Postgres accepts, keeps the nesting
		// reading: the $9 after the inner */ is still comment.
		{`a = $1 /* x /* $9 */ $9 */ AND b = $1`, `a = $1 /* x /* $9 */ $9 */ AND b = $2`},
		// No */ at all: the comment runs to the end, as before.
		{`a = $1 /* never closed $1`, `a = $1 /* never closed $1`},
	} {
		if got := renumberPlaceholders(tc.in, 1); got != tc.want {
			t.Errorf("renumber %q\n  got  %s\n  want %s", tc.in, got, tc.want)
		}
	}
}
