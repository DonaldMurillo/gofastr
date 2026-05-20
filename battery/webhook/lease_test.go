package webhook

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

// ----- LeasedStore contract --------------------------------------------------

func TestSQLStore_ClaimDueDeliveriesIsExclusive(t *testing.T) {
	_, store := openSQLStore(t)
	ctx := context.Background()

	// Seed a single due delivery.
	now := time.Now()
	d := Delivery{
		ID:            "d1",
		SubscriberID:  "s1",
		Event:         "x",
		Payload:       []byte("{}"),
		Status:        StatusPending,
		NextAttemptAt: now.Add(-time.Second),
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := store.AddDelivery(ctx, d); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Simulate two workers — only ONE should win the claim.
	a, err := store.ClaimDueDeliveries(ctx, now, 10, 30*time.Second)
	if err != nil {
		t.Fatalf("claim A: %v", err)
	}
	b, err := store.ClaimDueDeliveries(ctx, now, 10, 30*time.Second)
	if err != nil {
		t.Fatalf("claim B: %v", err)
	}
	if len(a)+len(b) != 1 {
		t.Fatalf("exactly one worker must own the row; got A=%d B=%d", len(a), len(b))
	}
}

func TestSQLStore_LeasedRowBecomesClaimableAgainAfterLeaseExpires(t *testing.T) {
	_, store := openSQLStore(t)
	ctx := context.Background()

	now := time.Now()
	d := Delivery{
		ID:            "d2",
		SubscriberID:  "s2",
		Event:         "x",
		Payload:       []byte("{}"),
		Status:        StatusPending,
		NextAttemptAt: now.Add(-time.Second),
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := store.AddDelivery(ctx, d); err != nil {
		t.Fatal(err)
	}

	got, err := store.ClaimDueDeliveries(ctx, now, 10, 100*time.Millisecond)
	if err != nil || len(got) != 1 {
		t.Fatalf("first claim: %v %d", err, len(got))
	}
	// Within lease window, the same row is not claimable.
	got2, _ := store.ClaimDueDeliveries(ctx, now, 10, 100*time.Millisecond)
	if len(got2) != 0 {
		t.Fatalf("row should be leased; second claim should return nothing, got %d", len(got2))
	}
	// After the lease expires (simulate by advancing the "now" we pass).
	got3, err := store.ClaimDueDeliveries(ctx, now.Add(500*time.Millisecond), 10, 30*time.Second)
	if err != nil || len(got3) != 1 {
		t.Fatalf("post-expiry claim: %v %d (expected 1)", err, len(got3))
	}
}

func TestMemoryStore_ClaimDueDeliveriesIsExclusive(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	now := time.Now()
	if err := store.AddDelivery(ctx, Delivery{
		ID: "d", SubscriberID: "s", Event: "x", Payload: []byte("{}"),
		Status: StatusPending, NextAttemptAt: now.Add(-time.Second),
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	a, _ := store.ClaimDueDeliveries(ctx, now, 10, 30*time.Second)
	b, _ := store.ClaimDueDeliveries(ctx, now, 10, 30*time.Second)
	if len(a)+len(b) != 1 {
		t.Fatalf("exclusive claim broken: A=%d B=%d", len(a), len(b))
	}
}

// Compile-time assertions that both bundled stores satisfy LeasedStore.
var (
	_ LeasedStore = (*SQLStore)(nil)
	_ LeasedStore = (*MemoryStore)(nil)
)

// Helper used by the SQL test variants — must match the DB the existing
// openSQLStore uses (sqlite).
var _ = sql.ErrNoRows
