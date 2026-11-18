package queue

import (
	"database/sql"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/pgtest"
	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// The DB-backed scenarios run against both queue databases: an in-memory
// SQLite, and a real Postgres when one is configured (internal/pgtest
// skips the suite when it is not).

// forBackendDBs runs assert once per backend, as one subtest per backend.
func forBackendDBs(t *testing.T, assert func(t *testing.T, db *sql.DB)) {
	t.Helper()
	t.Run("sqlite", func(t *testing.T) {
		db, err := sql.Open("sqlite3", ":memory:")
		if err != nil {
			t.Fatal(err)
		}
		db.SetMaxOpenConns(1)
		t.Cleanup(func() { db.Close() })
		assert(t, db)
	})
	t.Run("postgres", func(t *testing.T) {
		assert(t, pgtest.DB(t))
	})
}

// forBackendQueues is forBackendDBs with a DBQueue built from opts on each
// backend's database.
func forBackendQueues(t *testing.T, opts []DBQueueOption, assert func(t *testing.T, db *sql.DB, q *DBQueue)) {
	t.Helper()
	forBackendDBs(t, func(t *testing.T, db *sql.DB) {
		q, err := NewDBQueue(db, opts...)
		if err != nil {
			t.Fatal(err)
		}
		assert(t, db, q)
	})
}
