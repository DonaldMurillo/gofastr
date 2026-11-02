package cron

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/pgtest"
	"github.com/lib/pq"
)

// The lease's release unlocked on the pinned connection, dropped the
// error, and returned the connection to the pool, which does not end the
// session. An unlock that failed (a cancelled statement, a statement
// timeout) left the lock held by a pooled connection, so no other replica
// could lead until this process exited (2026-11-01 bug bash review).

// failUnlockConnector opens real lib/pq connections whose
// pg_advisory_unlock fails once fail is set, standing in for a cancelled
// or timed-out unlock on a session that stays alive.
type failUnlockConnector struct {
	dsn  string
	fail *atomic.Bool
}

func (c failUnlockConnector) Connect(context.Context) (driver.Conn, error) {
	conn, err := pq.Driver{}.Open(c.dsn)
	if err != nil {
		return nil, err
	}
	return &failUnlockConn{Conn: conn, fail: c.fail}, nil
}

func (c failUnlockConnector) Driver() driver.Driver { return pq.Driver{} }

type failUnlockConn struct {
	driver.Conn
	fail *atomic.Bool
}

var errUnlockCancelled = errors.New("pq: canceling statement due to user request")

func (c *failUnlockConn) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	if c.fail.Load() && strings.Contains(q, "pg_advisory_unlock") {
		return nil, errUnlockCancelled
	}
	return c.Conn.(driver.QueryerContext).QueryContext(ctx, q, args)
}

func (c *failUnlockConn) ExecContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	if c.fail.Load() && strings.Contains(q, "pg_advisory_unlock") {
		return nil, errUnlockCancelled
	}
	return c.Conn.(driver.ExecerContext).ExecContext(ctx, q, args)
}

func TestPostgresAdvisoryLeaseReleasesWhenUnlockFails(t *testing.T) {
	dsn := pgtest.BaseDSN(t)
	var fail atomic.Bool
	db := sql.OpenDB(failUnlockConnector{dsn: dsn, fail: &fail})
	t.Cleanup(func() { db.Close() })
	db.SetMaxOpenConns(2)
	const key = 9876543211
	lease := NewPostgresAdvisoryLease(db, key)

	held, release, err := lease.Acquire(context.Background())
	if err != nil || !held {
		t.Fatalf("acquire: %v %v", held, err)
	}
	fail.Store(true)
	release()
	fail.Store(false)

	// Another replica: its own pool, its own session.
	other, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { other.Close() })
	otherLease := NewPostgresAdvisoryLease(other, key)
	got, rel, err := otherLease.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !got {
		t.Fatal("after a failed unlock the lock stayed held by a pooled connection: another replica cannot lead")
	}
	rel()
}
