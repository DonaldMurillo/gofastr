package webhook

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ============================================================================
// Pins attempts never being consumed at claim and settled absolutely from
// stale snapshots, found by the 2026-09-04 red-probe round; fixed by making
// every claim consume one attempt store-side (SQLStore claimPostgres /
// claimSqlite `attempts = attempts + 1`, MemoryStore.ClaimDueDeliveries,
// tick's non-leased fallback, MarkEnvelopeProcessing for inbound envelopes)
// and removing the Go-side settle counter writes (UpdateDelivery /
// UpdateEnvelope no longer persist attempts).
//
// Property 1: every CLAIM of a delivery consumes one attempt, so a
// crash-before-settle loop terminates at MaxAttempts instead of
// redelivering forever. The queue batteries state and pin this contract at
// claim time (battery/queue redis.go "Attempts are bumped at claim ... a
// worker that crashes before Ack/Nack has still consumed a delivery, so a
// poison message cannot redeliver forever", pinned by
// TestRedisClaimCrashLoopRespectsMaxAttempts; DBQueue's claim UPDATE does
// `attempts = attempts + 1`).
// Surfaces: SQLStore.claimPostgres / claimSqlite and
// MemoryStore.ClaimDueDeliveries, plus tick's plain-DueDeliveries fallback.
// ============================================================================

// TestClaimConsumesAttemptCrashLoop: three claim/crash cycles of the same
// delivery must have consumed three attempts in the stored row.
func TestClaimConsumesAttemptCrashLoop(t *testing.T) {
	_, sqlStore := openSQLStore(t)
	memStore := NewMemoryStore()
	for _, tc := range []struct {
		name  string
		store LeasedStore
	}{
		{"sqlstore", sqlStore},
		{"memorystore", memStore},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			now := time.Now().UTC()
			d := Delivery{
				ID:            "d-crashloop",
				SubscriberID:  "s1",
				Event:         "evt",
				Payload:       []byte(`{}`),
				Attempts:      0,
				Status:        StatusPending,
				NextAttemptAt: now,
				CreatedAt:     now,
			}
			if err := addDelivery(tc.store, d); err != nil {
				t.Fatalf("seed delivery: %v", err)
			}
			const claims = 3
			lease := 30 * time.Second
			for i := range claims {
				// Each cycle: claim, then the worker dies before any settle
				// (simulated by simply not settling); the lease expires and
				// the next claim re-delivers the row.
				due, err := tc.store.ClaimDueDeliveries(ctx, now.Add(time.Duration(i)*(lease+time.Second)), 10, lease)
				if err != nil {
					t.Fatalf("claim %d: %v", i+1, err)
				}
				if len(due) != 1 || due[0].ID != d.ID {
					t.Fatalf("claim %d: got %d deliveries, want the seeded row", i+1, len(due))
				}
			}
			got, err := listDelivery(tc.store, d.ID)
			if err != nil {
				t.Fatalf("read back: %v", err)
			}
			if got.Attempts != claims {
				t.Fatalf("SECURITY: [webhook] delivery claimed %d times with no settle, but the stored attempts = %d: "+
					"a worker that dies between claim and settle consumes no attempt, so MaxAttempts never trips and the "+
					"delivery is re-delivered every lease period forever (redis/DB queues bump attempts at claim for exactly this)",
					claims, got.Attempts)
			}
		})
	}
}

// ============================================================================
// Pins the settle writing its counter absolutely from a pre-settle
// snapshot, found by the 2026-09-04 red-probe round; fixed by dropping the
// Go-side counter write (Manager.attempt) and the attempts column from both
// stores' settle UPDATE, so the claim-consumed count is never overwritten.
//
// Property 2: the attempts recorded on the row must equal the delivery
// attempts that actually ran — a settle writing its counter absolutely from
// a pre-settle snapshot means two overlapping runners (a worker whose lease
// expired mid-POST, the designed crash-recovery re-claim) each write
// snapshot+1 and the second overwrites the first: two POSTs are recorded as
// one attempt, and the MaxAttempts budget silently grows to 2x under
// lease-overrun.
// Surfaces: Manager.attempt's settle against both stores' UpdateDelivery
// (the terminal fence only blocks non-terminal settles over terminal rows;
// a pending-over-pending overwrite was unfenced, so the fix removes the
// counter from the write entirely).
// ============================================================================

// TestSettleAttemptsCountEveryPost: two overlapping runners each POST the
// subscriber and fail; the stored attempts must equal the POST count.
func TestSettleAttemptsCountEveryPost(t *testing.T) {
	var posts atomic.Int32
	firstArrived := make(chan struct{})
	releaseFirst := make(chan struct{})
	var once sync.Once
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if posts.Add(1) == 1 {
			once.Do(func() { close(firstArrived) })
			<-releaseFirst // W1's POST hangs (slow receiver) while its lease expires
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	store := NewMemoryStore()
	mgr := New(store, Options{
		MaxAttempts:          6,
		Backoff:              []time.Duration{time.Hour},
		AllowPrivateNetworks: true, // httptest is loopback
	})
	if _, err := mgr.Subscribe(context.Background(), Subscriber{
		ID: "sub1", URL: srv.URL, Secret: "s", Events: []string{"*"},
	}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	ctx := context.Background()
	if _, err := mgr.Publish(ctx, "evt", []byte(`{}`)); err != nil {
		t.Fatalf("publish: %v", err)
	}

	// W1 claims and starts its attempt; its POST is now hanging.
	due1, err := store.ClaimDueDeliveries(ctx, time.Now().UTC(), 10, 30*time.Second)
	if err != nil || len(due1) != 1 {
		t.Fatalf("W1 claim: %v (len %d)", err, len(due1))
	}
	w1Done := make(chan struct{})
	go func() {
		defer close(w1Done)
		mgr.attempt(ctx, due1[0])
	}()
	select {
	case <-firstArrived:
	case <-time.After(5 * time.Second):
		t.Fatal("setup: W1 POST never arrived")
	}

	// W1's lease expires; W2 re-claims the still-pending row (attempts
	// snapshot unchanged: no settle has landed) and completes its attempt.
	due2, err := store.ClaimDueDeliveries(ctx, time.Now().UTC().Add(31*time.Second), 10, 30*time.Second)
	if err != nil || len(due2) != 1 {
		t.Fatalf("W2 re-claim: %v (len %d)", err, len(due2))
	}
	mgr.attempt(ctx, due2[0])

	// W1's hung POST finally fails; its late settle lands on the pending row.
	close(releaseFirst)
	select {
	case <-w1Done:
	case <-time.After(5 * time.Second):
		t.Fatal("setup: W1 attempt never finished")
	}

	if got := posts.Load(); got != 2 {
		t.Fatalf("setup: subscriber saw %d POSTs, want 2", got)
	}
	got, err := listDelivery(store, due1[0].ID)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if got.Attempts != 2 {
		t.Fatalf("SECURITY: [webhook] %d delivery attempts ran (POSTs observed at the subscriber), but the row records attempts=%d: "+
			"each runner settles the absolute value of its own stale claim-time snapshot, so overlapping runners under-write the "+
			"attempt count and MaxAttempts silently grants extra deliveries", posts.Load(), got.Attempts)
	}
}

// ============================================================================
// Pins the inbound twin of the same shape (added when the absattempts
// contract swept the tree): SQLInboundStore.update wrote e.Attempts
// absolutely, so two overlapping ProcessInbound passes (the queue's lease
// expiry re-running an envelope's job while a slow handler still runs)
// both wrote the same snapshot+1 and counted two invocations as one.
// Fixed by the store-side processing claim: MarkEnvelopeProcessing does
// `attempts = attempts + 1` and the settle never touches the counter.
// Property: the attempts column counts processing transitions, monotonically.
// Surfaces: SQLInboundStore.MarkEnvelopeProcessing / UpdateEnvelope,
// MemoryInboundStore twins, ProcessInbound's claimer probe.
// ============================================================================

// TestInboundAttemptsCountEveryPass: two overlapping processing passes of
// the same envelope must both be counted; the settle must not rewind them.
func TestInboundAttemptsCountEveryPass(t *testing.T) {
	_, sqlStore := openInboundSQLStore(t)
	memStore := NewMemoryInboundStore()
	for _, tc := range []struct {
		name  string
		store InboundStore
	}{
		{"sqlstore", sqlStore},
		{"memorystore", memStore},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			env := InboundEnvelope{
				ID: "env-attempts", Source: "github", DedupeKey: "del-1",
				Payload: []byte(`{}`), Status: InboundStatusReceived,
				ReceivedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
			}
			if err := tc.store.AddEnvelope(ctx, env); err != nil {
				t.Fatalf("seed: %v", err)
			}

			// Two runners both loaded the envelope, then both transition
			// it to processing (the queue-lease overrun shape).
			for range 2 {
				if err := tc.store.(envelopeProcessingClaimer).MarkEnvelopeProcessing(ctx, env.ID); err != nil {
					t.Fatalf("mark processing: %v", err)
				}
			}

			// A late settle carries the pre-transition snapshot; it must
			// not rewind the consumed attempts.
			stale, err := tc.store.GetEnvelope(ctx, env.ID)
			if err != nil || stale == nil {
				t.Fatalf("reload: %v %v", stale, err)
			}
			stale.Status = InboundStatusFailed
			stale.Attempts = 0
			stale.LastError = "late runner failure"
			if err := tc.store.UpdateEnvelope(ctx, *stale); err != nil {
				t.Fatalf("settle: %v", err)
			}

			got, err := tc.store.GetEnvelope(ctx, env.ID)
			if err != nil || got == nil {
				t.Fatalf("read back: %v %v", got, err)
			}
			if got.Attempts != 2 || got.Status != InboundStatusFailed || got.LastError != "late runner failure" {
				t.Fatalf("SECURITY: [webhook] two processing passes plus a stale settle left envelope "+
					"attempts=%d status=%q err=%q; want attempts=2 (monotonic count), failed, error kept",
					got.Attempts, got.Status, got.LastError)
			}
		})
	}
}

// addDelivery adapts the two shipped stores behind one seeding shape for
// the loops above.
func addDelivery(s LeasedStore, d Delivery) error {
	type adder interface {
		AddDelivery(ctx context.Context, d Delivery) error
	}
	return s.(adder).AddDelivery(context.Background(), d)
}

func listDelivery(s LeasedStore, id string) (Delivery, error) {
	all, err := s.(interface {
		ListDeliveries(ctx context.Context, subscriberID string, limit int) ([]Delivery, error)
	}).ListDeliveries(context.Background(), "", 100)
	if err != nil {
		return Delivery{}, err
	}
	for _, d := range all {
		if d.ID == id {
			return d, nil
		}
	}
	return Delivery{}, nil
}
