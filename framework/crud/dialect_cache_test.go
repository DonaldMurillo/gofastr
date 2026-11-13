package crud

import (
	"context"
	"database/sql"
	"testing"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

type countingQueryer struct {
	db *sql.DB
	n  int
}

func (c *countingQueryer) QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row {
	c.n++
	return c.db.QueryRowContext(ctx, q, args...)
}

// The dialect probe fails on SQLite by design, so it must run once per
// database, not once per request: unremembered, every request that
// reached it ran a failing query. An answer that is not certain (here a
// cancelled context) is not remembered.
func TestDialectProbeRunsOncePerDatabase(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	q := &countingQueryer{db: db}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if isPostgres(cancelled, db, q) {
		t.Fatal("a cancelled probe read as Postgres")
	}
	if isPostgres(context.Background(), db, q) || q.n != 2 {
		t.Fatalf("after a cancelled probe the database was not asked again: %d probes", q.n)
	}
	for range 3 {
		if isPostgres(context.Background(), db, q) {
			t.Fatal("SQLite read as Postgres")
		}
	}
	if q.n != 2 {
		t.Fatalf("%d probes for one database, want the cancelled one and one that answered", q.n)
	}

	// With no stable handle, nothing is remembered.
	if isPostgres(context.Background(), nil, q); q.n != 3 {
		t.Fatalf("a probe with no handle did not run: %d probes", q.n)
	}
}
