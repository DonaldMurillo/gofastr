package outbox

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// ============================================================================
// Claim-crash loop budget — pinned 2026-09-05 (adversarial pass round 4;
// promoted from that round's red probe once the claim consumed attempts).
// Family: F20 Transaction and side-effect ordering (delivery semantics
// under crash/retry).
// Property: claiming a delivery consumes one attempt IN SQL, so a relay
// that crashes between claim and settle (kill -9, OOM, mid-handler deploy)
// terminates at MaxAttempts instead of re-delivering forever.
// Surfaces: delivery.go claimDeliveriesSQLite / claimDeliveriesPostgres
// (the claim UPDATEs), the claim-time budget guard, and
// deadLetterExpiredExhaustedClaims (the expired-lease exhaustion sweep),
// both reached from relay.go pump for every claimed delivery on every
// replica.
// ============================================================================

// TestClaimCrashLoopConsumesAttempts drives maxAttempts claim→crash→lease-expire
// cycles (no settle ever lands, the relay-died-mid-handler shape) and asserts
// the budget was consumed and the row is no longer claimable.
func TestClaimCrashLoopConsumesAttempts(t *testing.T) {
	db, o := openOutbox(t, WithMaxAttempts(2))
	ctx := context.Background()

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	id, err := o.Append(ctx, tx, "t.crash", map[string]any{"k": 1})
	if err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	insertDelivery(t, db, o, id, "svc", "pending", 0, "")

	handsOut := func() bool {
		got, err := o.claimDeliveries(ctx)
		if err != nil {
			t.Fatalf("claim: %v", err)
		}
		for _, d := range got {
			if d.RowID == id && d.Consumer == "svc" {
				return true
			}
		}
		return false
	}
	expireLease := func() {
		if _, err := db.Exec(fmt.Sprintf(
			`UPDATE %s SET claimed_until = ? WHERE row_id = ? AND consumer = ?`, o.qd()),
			time.Now().UTC().Add(-time.Minute), id, "svc"); err != nil {
			t.Fatalf("expire lease: %v", err)
		}
	}

	// MaxAttempts claim/crash cycles: each claim hands the row out, the "relay" then
	// dies without settling, and the lease expires so the next pump re-claims.
	for i := 1; i <= o.maxAttempts; i++ {
		if !handsOut() {
			t.Fatalf("cycle %d: delivery not claimable before the attempt budget was spent", i)
		}
		expireLease()
	}

	// The budget is exhausted: every claim consumed an attempt, so the row must not
	// be handed out again.
	if handsOut() {
		d := findDelivery(t, mustDeliveries(t, o, id), "svc")
		t.Fatalf("SECURITY: [outbox] delivery claimed %d times with no settle is still claimable (status=%q attempts=%d max=%d): "+
			"claims consume no attempt, so a relay that dies between claim and settle re-delivers the row every lease "+
			"period forever and MaxAttempts never trips — battery/queue and battery/webhook bump attempts at claim for exactly this",
			o.maxAttempts+1, d.Status, d.Attempts, o.maxAttempts)
	}
	d := findDelivery(t, mustDeliveries(t, o, id), "svc")
	if d.Attempts < o.maxAttempts {
		t.Fatalf("SECURITY: [outbox] %d claim/crash cycles left attempts=%d (max=%d): a crash between claim and "+
			"settle must still consume the attempt, else the crash loop is unbounded",
			o.maxAttempts, d.Attempts, o.maxAttempts)
	}
}
