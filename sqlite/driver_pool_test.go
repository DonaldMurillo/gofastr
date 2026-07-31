package sqlite

import (
	"context"
	"database/sql"
	"path/filepath"
	"sync"
	"testing"
)

// Concurrent writers through one *sql.DB must not lose rows. The
// registered driver used to build a fresh engine per physical
// connection, so two connections on one file had independent pagers and
// page caches with no locking between them — they overwrote each other's
// pages and silently dropped committed rows.
func TestConcurrentWritersLoseNoRows(t *testing.T) {
	const writers, perWriter = 2, 25
	path := filepath.Join(t.TempDir(), "w.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `CREATE TABLE t (id INTEGER)`); err != nil {
		t.Fatalf("create: %v", err)
	}

	var wg sync.WaitGroup
	errs := make([]error, writers)
	for w := range writers {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := range perWriter {
				if _, err := db.ExecContext(ctx,
					`INSERT INTO t (id) VALUES (?)`, w*1000+i); err != nil {
					errs[w] = err
					return
				}
			}
		}(w)
	}
	wg.Wait()
	for w, err := range errs {
		if err != nil {
			t.Fatalf("writer %d: %v", w, err)
		}
	}

	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM t`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != writers*perWriter {
		t.Fatalf("%d rows survived %d concurrent inserts — connections are on separate engines and clobbered each other's pages",
			n, writers*perWriter)
	}
}

// Every connection in one *sql.DB pool must address the SAME database.
func TestFileDSNSharesEngineAcrossConns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pool.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	// Hold conn1 open so conn2 is forced to be a second physical conn.
	conn1, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("conn1: %v", err)
	}
	defer conn1.Close()
	if _, err := conn1.ExecContext(ctx, `CREATE TABLE t (id INTEGER)`); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := conn1.ExecContext(ctx, `INSERT INTO t (id) VALUES (1)`); err != nil {
		t.Fatalf("insert: %v", err)
	}

	conn2, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("conn2: %v", err)
	}
	defer conn2.Close()
	var n int
	if err := conn2.QueryRowContext(ctx, `SELECT COUNT(*) FROM t`).Scan(&n); err != nil {
		t.Fatalf("second connection cannot see the first's committed write: %v", err)
	}
	if n != 1 {
		t.Fatalf("second connection sees %d rows, want 1 — connections are on separate engines", n)
	}
}

// Two independent in-memory DBs must stay independent: ":memory:" is not
// a shared name, so sharing an engine by DSN must not merge them.
func TestMemoryDSNsStayIndependent(t *testing.T) {
	db1, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db1: %v", err)
	}
	defer db1.Close()
	db2, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db2: %v", err)
	}
	defer db2.Close()

	if _, err := db1.Exec(`CREATE TABLE only_in_one (id INTEGER)`); err != nil {
		t.Fatalf("create in db1: %v", err)
	}
	if _, err := db2.Exec(`SELECT * FROM only_in_one`); err == nil {
		t.Fatal("a separate :memory: database must not see db1's table")
	}
}
