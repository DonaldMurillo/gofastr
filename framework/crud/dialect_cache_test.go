package crud

import (
	"context"
	"database/sql"
	"runtime"
	"testing"
	"time"

	fdb "github.com/DonaldMurillo/gofastr/framework/db"
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

// A write runs inside a transaction, where the handler's DB is that
// transaction: the answer is remembered under the database it was begun
// on, or every write would probe again. And what is remembered does not
// keep a closed database alive.
func TestDialectIsRememberedThroughATransaction(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ch := &CrudHandler{DB: db}
	if got := ch.dialectHandle(); got != db {
		t.Fatalf("a handler on a database remembers under %v", got)
	}
	// Both ways an operation gets its transaction: one of its own, and
	// a savepoint in the caller's.
	q := &countingQueryer{db: db}
	probe := func(ctx context.Context, txCh *CrudHandler) error {
		if _, isTx := txCh.DB.(*sql.Tx); !isTx {
			t.Error("setup: the operation is not in a transaction")
		}
		if got := txCh.dialectHandle(); got != db {
			t.Errorf("a handler in a transaction remembers under %v, want the database it began on", got)
		}
		if isPostgres(ctx, txCh.dialectHandle(), q) {
			t.Error("SQLite read as Postgres")
		}
		return nil
	}
	for range 2 {
		if err := ch.inTx(context.Background(), probe); err != nil {
			t.Fatal(err)
		}
	}
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := ch.inTx(fdb.WithTx(context.Background(), tx), probe); err != nil {
		t.Fatal(err)
	}
	if q.n != 1 {
		t.Errorf("%d probes across three writes, want 1", q.n)
	}
	// A handler bound to a caller's transaction from the start has no
	// database to remember under, and asks each time.
	bound := &CrudHandler{DB: tx}
	if bound.dialectHandle() != nil {
		t.Error("a handler that only ever saw a transaction claims a database")
	}
}

func TestDialectMemoDoesNotKeepADatabaseAlive(t *testing.T) {
	count := func() int {
		n := 0
		knownDialect.Range(func(any, any) bool { n++; return true })
		return n
	}
	before := count()
	func() {
		for range 20 {
			db, err := sql.Open("sqlite3", ":memory:")
			if err != nil {
				t.Fatal(err)
			}
			isPostgres(context.Background(), db, db)
			_ = db.Close()
		}
	}()
	if count() < before+20 {
		t.Fatalf("setup: the twenty databases were not remembered (%d entries, %d before)", count(), before)
	}
	for range 50 {
		runtime.GC()
		if count() <= before {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Errorf("%d entries left after the databases were collected, %d before: closed databases are held for good", count(), before)
}
