package fanout

import (
	"database/sql"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/pgtest"
	_ "github.com/lib/pq"
)

// Property: Close ends a fallback-row fetch the dispatcher is blocked in.
// The SELECT for a "t:<id>" pointer ran with no context, so a slow or
// locked table kept the dispatcher goroutine and a pool connection alive
// past Close, for as long as the database cared to wait.
func TestCloseCancelsFallbackFetch(t *testing.T) {
	dsn := pgtest.FreshDatabaseDSN(t)
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	f, err := NewPostgres(dsn, db)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	// A second pool holds the table lock and sends the pointer, so the
	// fanout's pool counts only the dispatcher's fetch.
	lockDB, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer lockDB.Close()
	tx, err := lockDB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`LOCK TABLE ` + f.qt() + ` IN ACCESS EXCLUSIVE MODE`); err != nil {
		t.Fatal(err)
	}
	if _, err := lockDB.Exec(`SELECT pg_notify($1, 't:1')`, f.channel); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for db.Stats().InUse == 0 {
		if time.Now().After(deadline) {
			t.Fatal("dispatcher never started the fallback fetch")
		}
		time.Sleep(5 * time.Millisecond)
	}

	closed := make(chan struct{})
	go func() { _ = f.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not return while the fallback fetch was blocked")
	}
	// Close waits for the dispatcher, which releases its connection before
	// it returns: no polling, the pool is idle the moment Close is done.
	if n := db.Stats().InUse; n != 0 {
		t.Fatalf("%d pool connection(s) still busy after Close: the dispatcher's fetch outlived it", n)
	}
}
