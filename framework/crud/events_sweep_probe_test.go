package crud

import (
	"context"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/pgtest"
)

// The commit probe must not disturb the transaction it watches. Its poll
// waits behind the owner's running statement; if the poll's own context
// ended there, lib/pq would cancel the backend and mark the connection bad,
// failing the owner's next statement and its commit.
func TestAmbientProbeLeavesOwnerTxIntact(t *testing.T) {
	conn := pgtest.DB(t)
	tx, err := conn.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	owner := make(chan error, 1)
	go func() {
		_, err := tx.Exec("SELECT pg_sleep(0.5)")
		owner <- err
	}()
	time.Sleep(50 * time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if waitTxDone(ctx, tx) {
		t.Fatal("waitTxDone reported an open transaction as done")
	}
	if err := <-owner; err != nil {
		t.Fatalf("the owner's statement failed under the probe: %v", err)
	}
	time.Sleep(200 * time.Millisecond)
	if _, err := tx.Exec("SELECT 1"); err != nil {
		t.Fatalf("the owner's next statement failed after the probe gave up: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("the owner's commit failed after the probe gave up: %v", err)
	}
}
