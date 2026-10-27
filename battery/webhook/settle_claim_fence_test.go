package webhook

import (
	"context"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/pgtest"
)

// eachLeasedStore runs body against the memory, sqlite and postgres
// delivery stores.
func eachLeasedStore(t *testing.T, body func(t *testing.T, s LeasedStore)) {
	t.Helper()
	stores := []struct {
		name string
		open func(t *testing.T) LeasedStore
	}{
		{"memory", func(*testing.T) LeasedStore { return NewMemoryStore() }},
		{"sqlite", func(t *testing.T) LeasedStore { _, s := openSQLStore(t); return s }},
		{"postgres", func(t *testing.T) LeasedStore {
			s, err := NewSQLStore(pgtest.DB(t), WithSQLAllowPlaintext())
			if err != nil {
				t.Fatalf("NewSQLStore: %v", err)
			}
			return s
		}},
	}
	for _, tc := range stores {
		t.Run(tc.name, func(t *testing.T) { body(t, tc.open(t)) })
	}
}

func claimOne(t *testing.T, s LeasedStore, now time.Time) Delivery {
	t.Helper()
	got, err := s.ClaimDueDeliveries(context.Background(), now, 10, time.Minute)
	if err != nil || len(got) != 1 {
		t.Fatalf("claim at %v = %v, %v; want one delivery", now, got, err)
	}
	return got[0]
}

// A's lease expires and B re-claims. A's late failure schedules a retry
// whose backoff ends before B's lease does: that write must not replace
// B's lease, or a third worker claims the delivery while B still sends it.
func TestStaleRetryKeepsLiveLease(t *testing.T) {
	eachLeasedStore(t, func(t *testing.T, s LeasedStore) {
		ctx := context.Background()
		now := time.Now().UTC().Truncate(time.Second)
		if err := s.AddDelivery(ctx, Delivery{ID: "d1", SubscriberID: "s", Event: "e", Payload: []byte("p"),
			Status: StatusPending, NextAttemptAt: now, CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatal(err)
		}
		a := claimOne(t, s, now)
		b := claimOne(t, s, now.Add(2*time.Minute)) // lease until now+3m

		a.Status = StatusPending
		a.LastError = "http 500"
		a.NextAttemptAt = now.Add(2*time.Minute + 10*time.Second)
		a.UpdatedAt = now.Add(2 * time.Minute)
		if err := s.UpdateDelivery(ctx, a); err != nil {
			t.Fatal(err)
		}

		got, err := s.ClaimDueDeliveries(ctx, now.Add(2*time.Minute+20*time.Second), 10, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 0 {
			t.Fatalf("a third worker claimed %s (attempts %d) inside B's lease: A's stale retry replaced the lease B holds", got[0].ID, got[0].Attempts)
		}

		// Control: the live claimant's own retry still lands.
		b.Status = StatusPending
		b.LastError = "http 502"
		b.NextAttemptAt = now.Add(4 * time.Minute)
		if err := s.UpdateDelivery(ctx, b); err != nil {
			t.Fatal(err)
		}
		list, err := s.ListDeliveries(ctx, "s", 10)
		if err != nil || len(list) != 1 {
			t.Fatalf("list = %v, %v", list, err)
		}
		if list[0].LastError != "http 502" || !list[0].NextAttemptAt.Equal(now.Add(4*time.Minute)) {
			t.Fatalf("live claimant's retry = %q at %v; want http 502 at %v: the fence refused the claim it should admit",
				list[0].LastError, list[0].NextAttemptAt, now.Add(4*time.Minute))
		}
	})
}

// A stalls on its last attempt, B re-claims past the budget and
// dead-letters, and an operator replays the delivery. A's late give-up
// must not dead-letter the replay before it ran once.
func TestStaleDeadSettleKeepsReplay(t *testing.T) {
	eachLeasedStore(t, func(t *testing.T, s LeasedStore) {
		ctx := context.Background()
		now := time.Now().UTC().Truncate(time.Second).Add(-time.Hour)
		if err := s.AddDelivery(ctx, Delivery{ID: "d1", SubscriberID: "s", Event: "e", Payload: []byte("p"),
			Status: StatusPending, NextAttemptAt: now, CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatal(err)
		}
		a := claimOne(t, s, now)
		b := claimOne(t, s, now.Add(2*time.Minute))

		// The live claimant's own dead settle lands.
		b.Status, b.LastError = StatusDead, "attempt budget exhausted"
		if err := s.UpdateDelivery(ctx, b); err != nil {
			t.Fatal(err)
		}
		if list, _ := s.ListDeliveries(ctx, "s", 10); len(list) != 1 || list[0].Status != StatusDead {
			t.Fatalf("after B's dead settle = %+v; want dead: the fence refused the live claimant", list)
		}
		if err := s.(ReplayableStore).ResetDelivery(ctx, "d1"); err != nil {
			t.Fatal(err)
		}

		a.Status, a.LastError = StatusDead, "http 500"
		if err := s.UpdateDelivery(ctx, a); err != nil {
			t.Fatal(err)
		}
		list, err := s.ListDeliveries(ctx, "s", 10)
		if err != nil || len(list) != 1 {
			t.Fatalf("list = %v, %v", list, err)
		}
		if got := list[0]; got.Status != StatusPending || got.LastError != "" || got.Attempts != 0 {
			t.Fatalf("replayed delivery = %s attempts=%d err=%q; want pending, 0, \"\": a stale dead settle cancelled the replay",
				got.Status, got.Attempts, got.LastError)
		}
	})
}

// A's POST succeeds after B re-claimed past the budget and dead-lettered.
// The receiver has the payload, so A's success must land over the dead
// row; left dead, an operator's replay would deliver it twice.
func TestStaleSuccessLandsOverDeadLetter(t *testing.T) {
	eachLeasedStore(t, func(t *testing.T, s LeasedStore) {
		ctx := context.Background()
		now := time.Now().UTC().Truncate(time.Second)
		if err := s.AddDelivery(ctx, Delivery{ID: "d1", SubscriberID: "s", Event: "e", Payload: []byte("p"),
			Status: StatusPending, NextAttemptAt: now, CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatal(err)
		}
		a := claimOne(t, s, now)
		b := claimOne(t, s, now.Add(2*time.Minute))
		b.Status, b.LastError = StatusDead, "attempt budget exhausted"
		if err := s.UpdateDelivery(ctx, b); err != nil {
			t.Fatal(err)
		}
		a.Status, a.LastError, a.NextAttemptAt = StatusSuccess, "", time.Time{}
		if err := s.UpdateDelivery(ctx, a); err != nil {
			t.Fatal(err)
		}
		if list, err := s.ListDeliveries(ctx, "s", 10); err != nil || len(list) != 1 || list[0].Status != StatusSuccess {
			t.Fatalf("after A's late success = %+v, %v; want success: a delivered row was left dead", list, err)
		}
	})
}

// A stalls, another runner delivers, and retention reaps the success row.
// A's late settle must not put the delivery back: completed work would
// become deliverable again.
func TestStaleSettleRecreatesNoReapedRow(t *testing.T) {
	eachLeasedStore(t, func(t *testing.T, s LeasedStore) {
		ctx := context.Background()
		now := time.Now().UTC().Truncate(time.Second)
		if err := s.AddDelivery(ctx, Delivery{ID: "d1", SubscriberID: "s", Event: "e", Payload: []byte("p"),
			Status: StatusPending, NextAttemptAt: now, CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatal(err)
		}
		a := claimOne(t, s, now)

		done := a
		done.Status, done.NextAttemptAt = StatusSuccess, time.Time{}
		if err := s.UpdateDelivery(ctx, done); err != nil {
			t.Fatal(err)
		}
		if n, err := s.(RetentionSweeper).ReapTerminalBefore(ctx, now.Add(time.Hour)); err != nil || n != 1 {
			t.Fatalf("reap = %d, %v; want the success row gone", n, err)
		}

		// A retry and a give-up: the dead settle carries no lease, so only
		// the missing row stops it landing in the dead-letter list.
		for _, st := range []DeliveryStatus{StatusPending, StatusDead} {
			stale := a
			stale.Status = st
			stale.LastError = "http 500"
			stale.NextAttemptAt = now.Add(time.Minute)
			if err := s.UpdateDelivery(ctx, stale); err != nil {
				t.Fatal(err)
			}
			if list, err := s.ListDeliveries(ctx, "", 10); err != nil || len(list) != 0 {
				t.Fatalf("deliveries after a stale %s settle = %v, %v; want none: the settle recreated a reaped row", st, list, err)
			}
		}
	})
}
