package cron

import (
	"context"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/pgtest"
)

// TestPostgresAdvisoryLease_MutualExclusion proves two acquires of the same
// advisory-lock key on the same Postgres cannot both be held at once, and that
// release re-enables acquire. Skips when Postgres is unreachable (see
// internal/pgtest).
func TestPostgresAdvisoryLease_MutualExclusion(t *testing.T) {
	db := pgtest.DB(t)
	// pgtest pins MaxOpenConns(1) for advisory-lock correctness; mutual
	// exclusion needs TWO distinct sessions (one per conn) so the second
	// pg_try_advisory_lock runs against a lock the first session holds.
	db.SetMaxOpenConns(2)
	const key = 9876543210
	lease := NewPostgresAdvisoryLease(db, key)

	held1, rel1, err := lease.Acquire(context.Background())
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	if !held1 {
		t.Fatal("first acquire should succeed on a free lock")
	}

	// While the first is held, a second acquire on the same key must fail.
	held2, _, err := lease.Acquire(context.Background())
	if err != nil {
		t.Fatalf("second acquire: %v", err)
	}
	if held2 {
		t.Error("SECURITY/HA: second acquire succeeded while the first held the lock — no mutual exclusion")
	}

	rel1()

	// After release the lock is free again.
	held3, rel3, err := lease.Acquire(context.Background())
	if err != nil {
		t.Fatalf("acquire after release: %v", err)
	}
	if !held3 {
		t.Error("acquire after release should succeed")
	}
	rel3()
}
