package migrate_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	migrate "github.com/DonaldMurillo/gofastr/core/migrate"
	"github.com/DonaldMurillo/gofastr/internal/pgtest"
)

// The migration lock is a session advisory lock on the pinned connection,
// released by SELECT pg_advisory_unlock on that connection before it goes
// back to the pool. A NoTransaction migration that opened its own
// transaction (BEGIN) and failed inside it left the session in an aborted
// transaction, where the unlock fails too; the error was dropped and the
// connection, still holding the lock, went back to the pool. Every other
// replica then waited on the lock for as long as this process lived, and
// this one's next run failed on the aborted session (2026-11-01 bug bash).
func TestPG_LockReleasedAfterAbortedNoTxMigration(t *testing.T) {
	db := pgtest.DB(t)
	m := pgMigrator(t, db)
	if err := m.Register(migrate.Migration{Version: 1, Name: "aborts", NoTransaction: true,
		Up: "BEGIN; SELECT 1/0; COMMIT", Down: "SELECT 1"}); err != nil {
		t.Fatal(err)
	}
	if err := m.Up(context.Background()); err == nil {
		t.Fatal("the failing migration applied")
	}

	// Another process's view: a second pool must get the lock.
	other := pgtest.DB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err := migrate.WithAdvisoryLock(ctx, other, migrate.DialectPostgres, func(*sql.Conn) error { return nil })
	if err != nil {
		t.Fatalf("another replica could not take the migration lock after the failed run: %v", err)
	}
	// And this process can run again (reporting the dirty migration).
	if err := m.Up(context.Background()); err == nil || !isDirty(err) {
		t.Fatalf("second run = %v, want the dirty refusal", err)
	}
}

func isDirty(err error) bool {
	for e := err; e != nil; {
		if e == migrate.ErrDirty {
			return true
		}
		u, ok := e.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		e = u.Unwrap()
	}
	return false
}
