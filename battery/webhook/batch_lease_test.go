package webhook

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"
)

// slowReceiverRT answers 200 to every POST after advancing the shared test
// clock by step, the stand-in for a slow receiver. On the POST numbered
// hookAt it runs hook once (a second worker's tick, in the tests below).
type slowReceiverRT struct {
	mu     sync.Mutex
	clock  *time.Time
	step   time.Duration
	hookAt int
	hook   func()
	n      int
	byID   map[string]int
}

func (rt *slowReceiverRT) now() time.Time {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return *rt.clock
}

func (rt *slowReceiverRT) RoundTrip(req *http.Request) (*http.Response, error) {
	rt.mu.Lock()
	rt.n++
	rt.byID[req.Header.Get("X-GoFastr-Delivery-ID")]++
	*rt.clock = rt.clock.Add(rt.step)
	fire := rt.n == rt.hookAt && rt.hook != nil
	rt.mu.Unlock()
	if fire {
		rt.hook()
	}
	return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Request: req}, nil
}

// The bar for starting a claimed delivery is one client timeout of lease
// left, capped at half the lease: a client timeout as long as the lease
// must not stop a fresh claim from starting.
func TestTickStartBarFollowsClientTimeout(t *testing.T) {
	cases := []struct {
		name    string
		timeout time.Duration
		want    int // POSTs in the first tick
	}{
		{"10s timeout, 30s lease", 10 * time.Second, 3}, // starts at 30s, 22s, 14s of lease left
		{"60s timeout, 30s lease", time.Minute, 2},      // capped at 15s: 30s, 22s
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			clock := time.Now().UTC().Truncate(time.Second)
			rt := &slowReceiverRT{clock: &clock, step: 8 * time.Second, byID: map[string]int{}}
			mgr := New(NewMemoryStore(), Options{
				LeasePeriod:          30 * time.Second,
				PollInterval:         time.Hour,
				AllowPrivateNetworks: true,
				HTTPClient:           &http.Client{Timeout: tc.timeout, Transport: rt},
			})
			mgr.nowFn = rt.now
			if _, err := mgr.Subscribe(ctx, Subscriber{
				ID: "sub1", URL: "https://receiver.example/hook", Secret: "s", Events: []string{"*"},
			}); err != nil {
				t.Fatalf("subscribe: %v", err)
			}
			for range 5 {
				if _, err := mgr.Publish(ctx, "evt", []byte(`{}`)); err != nil {
					t.Fatalf("publish: %v", err)
				}
			}
			mgr.tick(ctx)
			if rt.n != tc.want {
				t.Fatalf("first tick POSTed %d deliveries, want %d", rt.n, tc.want)
			}
		})
	}
}

// A release that comes after the lease expired and another worker
// re-claimed the delivery must leave that claim's lease and attempt alone.
func TestStaleReleaseKeepsNewClaim(t *testing.T) {
	eachLeasedStore(t, func(t *testing.T, s LeasedStore) {
		ctx := context.Background()
		now := time.Now().UTC().Truncate(time.Second)
		if err := s.AddDelivery(ctx, Delivery{ID: "d1", SubscriberID: "s", Event: "e", Payload: []byte("p"),
			Status: StatusPending, NextAttemptAt: now, CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatal(err)
		}
		a := claimOne(t, s, now)
		b := claimOne(t, s, now.Add(2*time.Minute))
		if err := s.(claimReleaser).releaseClaim(ctx, a, now.Add(2*time.Minute)); err != nil {
			t.Fatalf("stale release: %v", err)
		}
		got, err := listDelivery(s, "d1")
		if err != nil {
			t.Fatal(err)
		}
		if got.Attempts != 2 || !got.NextAttemptAt.Equal(b.NextAttemptAt) {
			t.Fatalf("after a stale release: attempts %d, next attempt %v; want 2 and the new lease %v",
				got.Attempts, got.NextAttemptAt, b.NextAttemptAt)
		}
	})
}

// A tick claims up to 32 deliveries under one lease and POSTs them one
// after another. With a slow receiver the later deliveries' lease ran out
// before their turn, a second worker re-claimed and sent them, and the
// first worker then sent them again. The tick now releases the claims it
// has no time left to run (refunding their attempt) instead of starting
// them on a lease that runs out mid-POST.
func TestTickReleasesClaimsItCannotRun(t *testing.T) {
	eachLeasedStore(t, func(t *testing.T, store LeasedStore) {
		ctx := context.Background()
		clock := time.Now().UTC().Truncate(time.Second)
		rt := &slowReceiverRT{clock: &clock, step: 8 * time.Second, byID: map[string]int{}}
		opts := Options{
			MaxAttempts:          5,
			LeasePeriod:          30 * time.Second,
			PollInterval:         time.Hour,
			AllowPrivateNetworks: true, // the custom client is used as-is
			HTTPClient:           &http.Client{Timeout: 10 * time.Second, Transport: rt},
		}
		first := New(store, opts)
		second := New(store, opts)
		first.nowFn, second.nowFn = rt.now, rt.now
		// The fourth POST runs past the batch's lease (claimed at T, lease
		// T+30s, each POST 8s): a second worker ticks then.
		rt.hookAt, rt.hook = 4, func() { second.tick(ctx) }

		if _, err := first.Subscribe(ctx, Subscriber{
			ID: "sub1", URL: "https://receiver.example/hook", Secret: "s", Events: []string{"*"},
		}); err != nil {
			t.Fatalf("subscribe: %v", err)
		}
		const n = 5
		for range n {
			if _, err := first.Publish(ctx, "evt", []byte(`{}`)); err != nil {
				t.Fatalf("publish: %v", err)
			}
		}
		for range 4 {
			first.tick(ctx)
			rt.mu.Lock()
			clock = clock.Add(time.Second)
			rt.mu.Unlock()
		}

		all, err := store.ListDeliveries(ctx, "", 10)
		if err != nil || len(all) != n {
			t.Fatalf("list: %v (n=%d)", err, len(all))
		}
		for _, d := range all {
			if got := rt.byID[d.ID]; got != 1 {
				t.Errorf("delivery %s POSTed %d times, want 1", d.ID, got)
			}
			if d.Status != StatusSuccess || d.Attempts != 1 {
				t.Errorf("delivery %s: status %q attempts %d, want success after 1 attempt",
					d.ID, d.Status, d.Attempts)
			}
		}
	})
}

// cancelOnPostRT answers 200 and cancels the tick's context on its first
// POST: a shutdown landing while the batch is mid-way.
type cancelOnPostRT struct {
	cancel context.CancelFunc
	byID   map[string]int
}

func (rt *cancelOnPostRT) RoundTrip(req *http.Request) (*http.Response, error) {
	rt.byID[req.Header.Get("X-GoFastr-Delivery-ID")]++
	rt.cancel()
	return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Request: req}, nil
}

// A shutdown mid-batch returned with the rest of the batch claimed and
// its attempt spent, so with MaxAttempts 1 the next claim dead-lettered
// deliveries that were never sent. The tick now hands them back with the
// attempt refunded, and the next tick sends each once.
func TestShutdownTickRefundsUnstarted(t *testing.T) {
	eachLeasedStore(t, func(t *testing.T, store LeasedStore) {
		tickCtx, cancel := context.WithCancel(context.Background())
		defer cancel()
		rt := &cancelOnPostRT{cancel: cancel, byID: map[string]int{}}
		mgr := New(store, Options{
			MaxAttempts:          1,
			LeasePeriod:          time.Minute,
			PollInterval:         time.Hour,
			AllowPrivateNetworks: true,
			HTTPClient:           &http.Client{Timeout: 10 * time.Second, Transport: rt},
		})
		ctx := context.Background()
		if _, err := mgr.Subscribe(ctx, Subscriber{
			ID: "sub1", URL: "https://receiver.example/hook", Secret: "s", Events: []string{"*"},
		}); err != nil {
			t.Fatalf("subscribe: %v", err)
		}
		const n = 4
		for range n {
			if _, err := mgr.Publish(ctx, "evt", []byte(`{}`)); err != nil {
				t.Fatalf("publish: %v", err)
			}
		}
		mgr.tick(tickCtx)
		if len(rt.byID) != 1 {
			t.Fatalf("the cancelled tick POSTed %d deliveries, want 1", len(rt.byID))
		}
		mgr.tick(ctx)
		all, err := store.ListDeliveries(ctx, "", 10)
		if err != nil || len(all) != n {
			t.Fatalf("list: %v (n=%d)", err, len(all))
		}
		for _, d := range all {
			if rt.byID[d.ID] != 1 || d.Status != StatusSuccess || d.Attempts != 1 {
				t.Errorf("delivery %s: POSTed %d times, status %q, attempts %d; want one send, success, 1 attempt",
					d.ID, rt.byID[d.ID], d.Status, d.Attempts)
			}
		}
	})
}
