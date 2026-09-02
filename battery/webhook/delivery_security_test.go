package webhook

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// Property: a RETRYABLE lookup failure must never terminally kill a
// delivery. "Dead" is the terminal state for an exhausted attempt budget
// (Options.MaxAttempts docs) or a definitively gone/inactive subscriber
// (attempt's own LastError text). A transient GetSubscriber error — a DB
// blip, a canceled worker context on a ctx-aware SQL store — says nothing
// about the subscriber, so the delivery must stay retryable.
//
// Production defect (observed red): attempt() branches
//
//	if err != nil || sub == nil || !sub.Active { → StatusDead }
//
// conflating "the store errored" (transient infra) with "the subscriber
// is gone" (terminal), so one failed lookup burns the whole retry budget
// at once. Surfaces asserted below: the plain Store tick path, the
// LeasedStore claim path, and the worker-shutdown (canceled context)
// path — every GetSubscriber error surface attempt() has.

// plainFlakyStore delegates to a MemoryStore but fails GetSubscriber the
// first time and, crucially, hides the LeasedStore upgrade so tick takes
// the plain DueDeliveries path.
type plainFlakyStore struct {
	inner Store
	fails atomic.Int32
}

func (s *plainFlakyStore) AddSubscriber(ctx context.Context, b Subscriber) error {
	return s.inner.AddSubscriber(ctx, b)
}
func (s *plainFlakyStore) GetSubscriber(ctx context.Context, id string) (*Subscriber, error) {
	if s.fails.Add(1) == 1 {
		return nil, errors.New("transient lookup failure")
	}
	return s.inner.GetSubscriber(ctx, id)
}
func (s *plainFlakyStore) ListSubscribers(ctx context.Context) ([]Subscriber, error) {
	return s.inner.ListSubscribers(ctx)
}
func (s *plainFlakyStore) DeleteSubscriber(ctx context.Context, id string) error {
	return s.inner.DeleteSubscriber(ctx, id)
}
func (s *plainFlakyStore) AddDelivery(ctx context.Context, d Delivery) error {
	return s.inner.AddDelivery(ctx, d)
}
func (s *plainFlakyStore) UpdateDelivery(ctx context.Context, d Delivery) error {
	return s.inner.UpdateDelivery(ctx, d)
}
func (s *plainFlakyStore) ListDeliveries(ctx context.Context, sub string, limit int) ([]Delivery, error) {
	return s.inner.ListDeliveries(ctx, sub, limit)
}
func (s *plainFlakyStore) DueDeliveries(ctx context.Context, now time.Time, limit int) ([]Delivery, error) {
	return s.inner.DueDeliveries(ctx, now, limit)
}

// leasedFlakyStore keeps the MemoryStore's LeasedStore promotion (tick
// then goes through ClaimDueDeliveries) and fails GetSubscriber once.
type leasedFlakyStore struct {
	*MemoryStore
	fails atomic.Int32
}

func (s *leasedFlakyStore) GetSubscriber(ctx context.Context, id string) (*Subscriber, error) {
	if s.fails.Add(1) == 1 {
		return nil, errors.New("transient lookup failure")
	}
	return s.MemoryStore.GetSubscriber(ctx, id)
}

// cancelRT makes the outbound HTTP attempt fail with context.Canceled
// exactly once — the deterministic stand-in for a worker shutdown landing
// mid-request — then lets later attempts through.
type cancelRT struct{ fails atomic.Int32 }

func (c *cancelRT) RoundTrip(r *http.Request) (*http.Response, error) {
	if c.fails.Add(1) == 1 {
		return nil, context.Canceled
	}
	return http.DefaultTransport.RoundTrip(r)
}

func TestTransientLookupKeepsDeliveryRetryable(t *testing.T) {
	cases := []struct {
		name string
		// store whose GetSubscriber fails exactly once, transiently.
		store Store
		// transport forces the HTTP attempt itself to fail with
		// context.Canceled (worker shutdown mid-request) instead of a
		// lookup error: the contrast proving cancellation is supposed to
		// be a retryable outcome, never a terminal one.
		cancelTransport bool
	}{
		{name: "plain tick path", store: &plainFlakyStore{inner: NewMemoryStore()}},
		{name: "leased claim path", store: &leasedFlakyStore{MemoryStore: NewMemoryStore()}},
		{name: "cancel during HTTP attempt", store: NewMemoryStore(), cancelTransport: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var hits atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				hits.Add(1)
				w.WriteHeader(http.StatusOK)
			}))
			defer srv.Close()

			opts := Options{
				MaxAttempts:          5,
				Backoff:              []time.Duration{0},
				PollInterval:         time.Hour, // never ticks on its own; we drive tick()
				AllowPrivateNetworks: true,      // httptest is loopback
			}
			if tc.cancelTransport {
				opts.HTTPClient = &http.Client{Transport: &cancelRT{}}
			}
			mgr := New(tc.store, opts)
			if _, err := mgr.Subscribe(context.Background(), Subscriber{URL: srv.URL, Secret: "testsecret", Events: []string{"**"}}); err != nil {
				t.Fatalf("subscribe: %v", err)
			}
			if _, err := mgr.Publish(context.Background(), "order.created", []byte(`{"id":1}`)); err != nil {
				t.Fatalf("publish: %v", err)
			}

			// First tick: the lookup fails transiently (or the attempt is
			// canceled mid-request). Neither may be terminal.
			mgr.tick(context.Background())

			due, err := tc.store.ListDeliveries(context.Background(), "", 10)
			if err != nil || len(due) != 1 {
				t.Fatalf("list deliveries: %v (n=%d)", err, len(due))
			}
			if due[0].Status == StatusDead {
				t.Errorf("SECURITY: [webhook] one transient GetSubscriber failure terminally killed the delivery (status=%q, last_error=%q): a retryable lookup error must leave the delivery retryable, dead is reserved for an exhausted attempt budget or a definitively gone subscriber",
					due[0].Status, due[0].LastError)
			}

			// The delivery must still be deliverable once the transient
			// condition clears (worker restart / store recovery).
			mgr.tick(context.Background())
			if got := hits.Load(); got != 1 {
				t.Errorf("SECURITY: [webhook] delivery was never retried after the transient lookup failure (receiver hits=%d, want 1): the row was buried in a terminal state instead of staying due",
					got)
			}
			after, err := tc.store.ListDeliveries(context.Background(), "", 10)
			if err != nil || len(after) != 1 {
				t.Fatalf("list deliveries after retry: %v (n=%d)", err, len(after))
			}
			if after[0].Status != StatusSuccess {
				t.Errorf("delivery status after recovery tick = %q, want %q", after[0].Status, StatusSuccess)
			}
		})
	}
}

// Property: a completion written by a claim that is no longer current
// must never overwrite the state the recovery claimant recorded. The
// LeasedStore lease hides a row for only leasePeriod; a worker that
// overruns its lease has the row re-claimed under it (the designed crash
// recovery), and its LATE settle must not resurrect a delivered row.
// ResetDelivery's own contract says a non-dead reset "can never
// resurrect an in-flight or delivered one"; the settle path must not
// either. This is the store-side sibling of the queue's claim fencing
// (DBQueue Ack/Nack fence on claim_token, RedisQueue on ownsClaim).
// Surfaces: SQLStore.UpdateDelivery and MemoryStore.UpdateDelivery —
// both write by bare row ID with no status or ownership fence, reached
// from Manager.attempt via saveDelivery.
func TestStaleSettleCannotResurrectDelivered(t *testing.T) {
	ctx := context.Background()
	lease := 50 * time.Millisecond

	stores := []struct {
		name  string
		store LeasedStore
	}{
		{name: "SQLStore", store: mustSQLStore(t)},
		{name: "MemoryStore", store: NewMemoryStore()},
	}
	for _, tc := range stores {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Now().UTC()
			seed := Delivery{
				ID:            "d1",
				SubscriberID:  "s1",
				Event:         "x",
				Payload:       []byte("{}"),
				Status:        StatusPending,
				NextAttemptAt: now.Add(-time.Second),
				CreatedAt:     now,
				UpdatedAt:     now,
			}
			if err := tc.store.AddDelivery(ctx, seed); err != nil {
				t.Fatalf("seed: %v", err)
			}

			// Worker A claims, then stalls past the lease.
			a, err := tc.store.ClaimDueDeliveries(ctx, now, 1, lease)
			if err != nil || len(a) != 1 {
				t.Fatalf("claim A: %v (n=%d)", err, len(a))
			}
			// Lease expiry: worker B recovers the same row.
			b, err := tc.store.ClaimDueDeliveries(ctx, now.Add(2*lease), 1, lease)
			if err != nil || len(b) != 1 || b[0].ID != a[0].ID {
				t.Fatalf("recovery claim B: %v (n=%d)", err, len(b))
			}

			// B delivers successfully and records the terminal state.
			delivered := b[0]
			delivered.Attempts++
			delivered.Status = StatusSuccess
			delivered.NextAttemptAt = time.Time{}
			if err := tc.store.UpdateDelivery(ctx, delivered); err != nil {
				t.Fatalf("record B success: %v", err)
			}

			// A finally wakes and settles a FAILURE for the same row.
			stale := a[0]
			stale.Attempts++
			stale.Status = StatusPending
			stale.LastError = "stale worker failure"
			stale.NextAttemptAt = time.Now().Add(time.Minute)
			if err := tc.store.UpdateDelivery(ctx, stale); err != nil {
				t.Fatalf("stale settle: %v", err)
			}

			rows, err := tc.store.ListDeliveries(ctx, "s1", 10)
			if err != nil || len(rows) != 1 {
				t.Fatalf("list: %v (n=%d)", err, len(rows))
			}
			if rows[0].Status != StatusSuccess {
				t.Errorf("SECURITY: [webhook] stale claimant's settle regressed a delivered row to %q (%q): the terminal record must win, a late failure write must not re-queue a delivered delivery (duplicate POST) nor bury a dead-letter someone triaged",
					rows[0].Status, rows[0].LastError)
			}
		})
	}
}

// mustSQLStore opens the shared sqlite-backed SQLStore fixture.
func mustSQLStore(t *testing.T) *SQLStore {
	t.Helper()
	_, s := openSQLStore(t)
	return s
}
