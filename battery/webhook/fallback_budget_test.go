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

// storeOnly hides every optional capability of the store it wraps: its
// method set is Store's and nothing else, the shape of a host wrapper
// (metrics, tracing) around a shipped store that forwards only Store. Such
// a store takes tick's plain DueDeliveries fallback.
type storeOnly struct{ Store }

// The plain DueDeliveries fallback has no claim, so tick consumes the
// attempt on its snapshot. Both shipped stores' settle never wrote the
// counter (the claim owns it), so the bump was lost: the row stayed at
// attempts 0, every tick POSTed again, and MaxAttempts never dead-lettered
// the delivery.
func TestFallbackTickReachesMaxAttempts(t *testing.T) {
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
				w.WriteHeader(http.StatusInternalServerError)
			}))
			defer srv.Close()

			inner := tc.open(t)
			const maxAttempts = 3
			mgr := New(storeOnly{inner}, Options{
				MaxAttempts:          maxAttempts,
				Backoff:              []time.Duration{0},
				PollInterval:         time.Hour,
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
			ids, err := mgr.Publish(ctx, "evt", []byte(`{}`))
			if err != nil || ids != 1 {
				t.Fatalf("publish: %v (queued %d)", err, ids)
			}
			for range 2 * maxAttempts {
				clock = clock.Add(time.Second)
				mgr.tick(ctx)
			}
			if n := posts.Load(); n != maxAttempts {
				t.Errorf("receiver saw %d POSTs, want %d (MaxAttempts)", n, maxAttempts)
			}
			all, err := inner.ListDeliveries(ctx, "", 10)
			if err != nil || len(all) != 1 {
				t.Fatalf("list: %v (n=%d)", err, len(all))
			}
			if all[0].Status != StatusDead || all[0].Attempts != maxAttempts {
				t.Fatalf("delivery status %q attempts %d, want dead at %d",
					all[0].Status, all[0].Attempts, maxAttempts)
			}
		})
	}
}
