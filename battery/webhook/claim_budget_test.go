package webhook

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/pgtest"
)

// ============================================================================
// Recovery honours the attempt budget. Every claim consumes one attempt,
// but no claim had a ceiling and the only MaxAttempts check ran in
// schedule, after a failed send. With MaxAttempts 1, a worker that claimed
// and crashed before settling left the row pending; once the lease expired
// the next tick claimed it again (attempts 2) and POSTed a second time.
// A claimed delivery past the budget is now dead-lettered without a send.
// ============================================================================

// TestExpiredClaimPastBudgetIsDead: MaxAttempts 1, claim without settling,
// let the lease expire, tick: no request, delivery dead.
func TestExpiredClaimPastBudgetIsDead(t *testing.T) {
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
		t.Run(tc.name, func(t *testing.T) {
			var posts atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				posts.Add(1)
				w.WriteHeader(http.StatusOK)
			}))
			defer srv.Close()

			store := tc.open(t)
			const lease = 30 * time.Second
			mgr := New(store, Options{
				MaxAttempts:          1,
				LeasePeriod:          lease,
				AllowPrivateNetworks: true, // httptest is loopback
			})
			clock := time.Now().UTC().Truncate(time.Second)
			mgr.nowFn = func() time.Time { return clock }
			ctx := context.Background()
			if _, err := mgr.Subscribe(ctx, Subscriber{
				ID: "sub1", URL: srv.URL, Secret: "s", Events: []string{"*"},
			}); err != nil {
				t.Fatalf("subscribe: %v", err)
			}
			if _, err := mgr.Publish(ctx, "evt", []byte(`{}`)); err != nil {
				t.Fatalf("publish: %v", err)
			}

			// A worker claims the delivery (its one attempt) and dies
			// before sending or settling.
			claimed, err := store.ClaimDueDeliveries(ctx, clock, 10, lease)
			if err != nil || len(claimed) != 1 {
				t.Fatalf("crashed worker's claim: %v (len %d), want one delivery", err, len(claimed))
			}
			id := claimed[0].ID

			clock = clock.Add(lease + time.Second) // the lease expires
			mgr.tick(ctx)

			if n := posts.Load(); n != 0 {
				t.Fatalf("recovery tick sent %d request(s) for a delivery whose only attempt the "+
					"crashed claim already consumed (MaxAttempts 1)", n)
			}
			got, err := listDelivery(store, id)
			if err != nil {
				t.Fatalf("read back: %v", err)
			}
			if got.Status != StatusDead {
				t.Fatalf("delivery status = %q (attempts %d), want dead", got.Status, got.Attempts)
			}
		})
	}
}
