package migrate

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestWithAdvisoryLock_SQLiteRunsFn(t *testing.T) {
	db, _, _ := sqlmock.New()
	ran := false
	err := WithAdvisoryLock(context.Background(), db, DialectSQLite, func(_ *sql.Conn) error {
		ran = true
		return nil
	})
	if err != nil {
		t.Fatalf("WithAdvisoryLock: %v", err)
	}
	if !ran {
		t.Fatal("fn did not run on the SQLite no-op path")
	}
}

func TestWithAdvisoryLock_NilDBStillRunsFn(t *testing.T) {
	ran := false
	err := WithAdvisoryLock(context.Background(), nil, DialectPostgres, func(_ *sql.Conn) error {
		ran = true
		return nil
	})
	if err != nil {
		t.Fatalf("WithAdvisoryLock(nil db): %v", err)
	}
	if !ran {
		t.Fatal("fn did not run with a nil db")
	}
}

func TestWithAdvisoryLock_PropagatesFnError(t *testing.T) {
	db, _, _ := sqlmock.New()
	sentinel := errors.New("boom")
	err := WithAdvisoryLock(context.Background(), db, DialectSQLite, func(_ *sql.Conn) error {
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel error, got %v", err)
	}
}

// TestWithAdvisoryLock_PostgresLockUnlockOrder asserts the Postgres path takes
// pg_advisory_lock before fn and releases it after, on the same pinned
// connection.
func TestWithAdvisoryLock_PostgresLockUnlockOrder(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(false))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectQuery(regexp.QuoteMeta("SELECT pg_try_advisory_lock($1)")).
		WithArgs(AdvisoryLockKey).
		WillReturnRows(sqlmock.NewRows([]string{"pg_try_advisory_lock"}).AddRow(true))
	mock.ExpectExec(regexp.QuoteMeta("SELECT pg_advisory_unlock($1)")).
		WithArgs(AdvisoryLockKey).WillReturnResult(sqlmock.NewResult(0, 0))

	ran := false
	err = WithAdvisoryLock(context.Background(), db, DialectPostgres, func(_ *sql.Conn) error {
		ran = true
		return nil
	})
	if err != nil {
		t.Fatalf("WithAdvisoryLock(postgres): %v", err)
	}
	if !ran {
		t.Fatal("fn did not run between lock and unlock")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

// TestWithAdvisoryLock_PostgresRetriesUntilAcquired asserts the poll loop
// keeps trying while the lock is held elsewhere (pg_try_advisory_lock returns
// false), then proceeds once it wins.
func TestWithAdvisoryLock_PostgresRetriesUntilAcquired(t *testing.T) {
	prev := lockPollInterval
	lockPollInterval = time.Millisecond
	defer func() { lockPollInterval = prev }()

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	falseRow := func() *sqlmock.Rows {
		return sqlmock.NewRows([]string{"pg_try_advisory_lock"}).AddRow(false)
	}
	trueRow := func() *sqlmock.Rows {
		return sqlmock.NewRows([]string{"pg_try_advisory_lock"}).AddRow(true)
	}
	q := regexp.QuoteMeta("SELECT pg_try_advisory_lock($1)")
	mock.ExpectQuery(q).WillReturnRows(falseRow())
	mock.ExpectQuery(q).WillReturnRows(falseRow())
	mock.ExpectQuery(q).WillReturnRows(trueRow())
	mock.ExpectExec(regexp.QuoteMeta("SELECT pg_advisory_unlock($1)")).
		WillReturnResult(sqlmock.NewResult(0, 0))

	ran := false
	if err := WithAdvisoryLock(context.Background(), db, DialectPostgres, func(_ *sql.Conn) error {
		ran = true
		return nil
	}); err != nil {
		t.Fatalf("WithAdvisoryLock: %v", err)
	}
	if !ran {
		t.Fatal("fn did not run after the lock was finally acquired")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

// TestWithAdvisoryLock_PostgresCtxCancelWhileWaiting asserts that a contended
// lock returns promptly when ctx is cancelled, and fn never runs — the
// deadlock-avoidance guarantee.
func TestWithAdvisoryLock_PostgresCtxCancelWhileWaiting(t *testing.T) {
	prev := lockPollInterval
	lockPollInterval = 5 * time.Millisecond
	defer func() { lockPollInterval = prev }()

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	// Always "held by someone else".
	mock.MatchExpectationsInOrder(false)
	q := regexp.QuoteMeta("SELECT pg_try_advisory_lock($1)")
	for i := 0; i < 50; i++ {
		mock.ExpectQuery(q).WillReturnRows(
			sqlmock.NewRows([]string{"pg_try_advisory_lock"}).AddRow(false))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	ran := false
	err = WithAdvisoryLock(ctx, db, DialectPostgres, func(_ *sql.Conn) error {
		ran = true
		return nil
	})
	if err == nil {
		t.Fatal("expected a context error while waiting for the lock")
	}
	if ran {
		t.Fatal("fn must not run when the lock was never acquired")
	}
}

func TestWithAdvisoryLock_PostgresLockAcquireError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectQuery(regexp.QuoteMeta("SELECT pg_try_advisory_lock($1)")).
		WillReturnError(errors.New("conn reset"))

	ran := false
	err = WithAdvisoryLock(context.Background(), db, DialectPostgres, func(_ *sql.Conn) error {
		ran = true
		return nil
	})
	if err == nil {
		t.Fatal("expected an error when the lock acquire fails")
	}
	if ran {
		t.Fatal("fn must not run when the lock cannot be acquired")
	}
}
