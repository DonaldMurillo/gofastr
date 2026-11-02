package webhook

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// LeasePeriod and the HTTP client's Timeout are each valid alone. With the
// lease no longer than the timeout, a receiver that answers slowly but
// inside the timeout outlives the delivery's claim: a second Manager on
// the same store re-claims the delivery mid-POST and sends it again. New
// now raises such a lease to twice the client timeout and logs that it
// did, the outbox's handler-budget rule (e5616f008).
func TestLeaseOutlivesClientTimeout(t *testing.T) {
	eachLeasedStore(t, func(t *testing.T, store LeasedStore) {
		ctx := context.Background()
		clock := time.Now().UTC().Truncate(time.Second)
		// Each POST takes 20s: inside the 30s client timeout, past the
		// 10s lease.
		rt := &slowReceiverRT{clock: &clock, step: 20 * time.Second, byID: map[string]int{}}
		var logMu sync.Mutex
		var logs []string
		opts := Options{
			LeasePeriod:          10 * time.Second,
			PollInterval:         time.Hour,
			AllowPrivateNetworks: true, // the custom client is used as-is
			HTTPClient:           &http.Client{Timeout: 30 * time.Second, Transport: rt},
			Logger: func(format string, args ...any) {
				logMu.Lock()
				defer logMu.Unlock()
				logs = append(logs, fmt.Sprintf(format, args...))
			},
		}
		first := New(store, opts)
		second := New(store, opts)
		first.nowFn, second.nowFn = rt.now, rt.now
		// The second worker ticks while the first one's POST is in flight.
		rt.hookAt, rt.hook = 1, func() { second.tick(ctx) }

		if _, err := first.Subscribe(ctx, Subscriber{
			ID: "sub1", URL: "https://receiver.example/hook", Secret: "s", Events: []string{"*"},
		}); err != nil {
			t.Fatalf("subscribe: %v", err)
		}
		if _, err := first.Publish(ctx, "evt", []byte(`{}`)); err != nil {
			t.Fatalf("publish: %v", err)
		}
		first.tick(ctx)

		if rt.n != 1 {
			t.Errorf("one delivery was POSTed %d times: the second worker re-claimed it while the first worker's POST was still inside its client timeout", rt.n)
		}
		if got, want := first.opts.LeasePeriod, time.Minute; got != want {
			t.Errorf("LeasePeriod = %s, want %s (twice the 30s client timeout)", got, want)
		}
		logMu.Lock()
		defer logMu.Unlock()
		if len(logs) == 0 || !strings.Contains(logs[0], "LeasePeriod") {
			t.Errorf("the raised lease was not logged: %q", logs)
		}
	})
}

// A lease already longer than the client timeout, and a client with no
// timeout, are left as configured.
func TestLeaseLongerThanClientTimeoutKept(t *testing.T) {
	for _, tc := range []struct {
		name    string
		lease   time.Duration
		timeout time.Duration
	}{
		{"lease past the timeout", 15 * time.Second, 10 * time.Second},
		{"no client timeout", 5 * time.Second, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var logged bool
			m := New(NewMemoryStore(), Options{
				LeasePeriod:          tc.lease,
				AllowPrivateNetworks: true,
				HTTPClient:           &http.Client{Timeout: tc.timeout},
				Logger:               func(string, ...any) { logged = true },
			})
			if m.opts.LeasePeriod != tc.lease || logged {
				t.Errorf("LeasePeriod = %s (logged %v), want %s untouched", m.opts.LeasePeriod, logged, tc.lease)
			}
		})
	}
	// The default client's 10s timeout counts too, and a lease equal to
	// the timeout is no longer than it.
	for _, lease := range []time.Duration{5 * time.Second, 10 * time.Second} {
		m := New(NewMemoryStore(), Options{LeasePeriod: lease, Logger: func(string, ...any) {}})
		if m.opts.LeasePeriod != 20*time.Second {
			t.Errorf("a %s lease under the default 10s client timeout: LeasePeriod = %s, want 20s", lease, m.opts.LeasePeriod)
		}
	}
}
