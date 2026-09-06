package webhook

import (
	"context"
	"strings"
	"testing"
	"time"
)

// Property: a NEGATIVE caller-supplied duration must never fold onto the
// default arm of a `<= 0` check. A negative leasePeriod (sign error, unit
// mistake, or a computed `deadline - now` that went past) silently became
// the 30s default — the strongest lease the store grants — instead of
// being refused, so a caller asking for a shorter-than-default lease got
// a longer one with no signal.
// Surfaces: MemoryStore.ClaimDueDeliveries, SQLStore.ClaimDueDeliveries
// (the two bundled LeasedStore implementations).
// Pins both bundled stores folding leasePeriod <= 0 onto the 30s
// default, found by the 2026-09-04 red-probe round; fixed in
// ClaimDueDeliveries rejecting leasePeriod < 0 with an error while 0
// keeps the default.

func seedDueDelivery(t *testing.T, add func(context.Context, Delivery) error) {
	t.Helper()
	d := Delivery{
		ID:            "d-negdur",
		SubscriberID:  "sub-negdur",
		Event:         "evt.negdur",
		Payload:       []byte(`{}`),
		Status:        StatusPending,
		NextAttemptAt: time.Now().UTC().Add(-time.Minute),
	}
	if err := add(context.Background(), d); err != nil {
		t.Fatalf("seed delivery: %v", err)
	}
}

func TestClaimLeaseNegativeRejected(t *testing.T) {
	now := time.Now().UTC()
	ctx := context.Background()

	mem := NewMemoryStore()
	seedDueDelivery(t, mem.AddDelivery)
	if _, err := mem.ClaimDueDeliveries(ctx, now, 1, -time.Second); err == nil {
		t.Fatal("MemoryStore.ClaimDueDeliveries: negative leasePeriod silently folded onto the 30s default")
	} else if !strings.Contains(err.Error(), "leasePeriod") {
		t.Fatalf("error must name leasePeriod, got: %v", err)
	}

	_, sqls := openSQLStore(t)
	seedDueDelivery(t, sqls.AddDelivery)
	if _, err := sqls.ClaimDueDeliveries(ctx, now, 1, -time.Second); err == nil {
		t.Fatal("SQLStore.ClaimDueDeliveries: negative leasePeriod silently folded onto the 30s default")
	} else if !strings.Contains(err.Error(), "leasePeriod") {
		t.Fatalf("error must name leasePeriod, got: %v", err)
	}
}

func TestClaimLeaseZeroKeepsDefault(t *testing.T) {
	now := time.Now().UTC()
	ctx := context.Background()
	want := now.Add(30 * time.Second)

	mem := NewMemoryStore()
	seedDueDelivery(t, mem.AddDelivery)
	got, err := mem.ClaimDueDeliveries(ctx, now, 1, 0)
	if err != nil {
		t.Fatalf("memory claim with zero leasePeriod: %v", err)
	}
	if len(got) != 1 || !got[0].NextAttemptAt.Equal(want) {
		t.Fatalf("zero leasePeriod must keep the 30s default: got NextAttemptAt %v, want %v", got, want)
	}

	_, sqls := openSQLStore(t)
	seedDueDelivery(t, sqls.AddDelivery)
	got2, err := sqls.ClaimDueDeliveries(ctx, now, 1, 0)
	if err != nil {
		t.Fatalf("sql claim with zero leasePeriod: %v", err)
	}
	if len(got2) != 1 || !got2[0].NextAttemptAt.Equal(want) {
		t.Fatalf("zero leasePeriod must keep the 30s default: got NextAttemptAt %v, want %v", got2, want)
	}
}
