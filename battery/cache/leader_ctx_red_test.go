//go:build red

package cache

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// RED TEST — open finding, 2026-09-06 adversarial pass (round 5; tests-only; no fix applied).
// CONTRACT-QUESTION red: GetOrSet's doc promises loader-exactly-once /
// error-propagation / never-cached but is silent on context semantics —
// this asserts a request-scoped failure never crosses request boundaries;
// if leader-ctx propagation is deliberate, document it and delete this.
// Property: one caller's abandoned request must not fail other callers'
// cache fill — the singleflight loader runs under the LEADER's context, so
// one attacker canceling mid-load (trivial client disconnect) hands
// context.Canceled to every concurrent waiter with live contexts;
// per-expiry repeat = targeted availability DoS on hot keys.
// Surfaces: battery/cache/cache.go::GetOrSet :70-84 — the getOrSetGroup.Do
// closure calls loader(leaderCtx) and the single (nil, err) result is
// shared with all waiters.
// Finding: verified today waiterErr == context.Canceled for a waiter whose
// own context is Background and live.
// Fix direction: run the loader under a context detached from any single
// caller (or re-check each waiter's own ctx before propagating a shared
// Canceled), so a request-scoped abort never crosses request boundaries.

// redCtxCountingCache counts Get calls so the scene can observe when the
// waiter has passed its fast-path miss and is about to park in the flight.
type redCtxCountingCache struct {
	Cache
	gets atomic.Int64
}

func (c *redCtxCountingCache) Get(ctx context.Context, key string, dest any) error {
	c.gets.Add(1)
	return c.Cache.Get(ctx, key, dest)
}

// redCtxScene runs one leader+waiter GetOrSet race on a fresh cache.
// cancelLeader controls whether the leader's context dies while the loader
// is in flight. The loader holds the flight open until the test releases
// it, so the join point is deterministic. Returns both GetOrSet errors,
// the waiter's decoded value, and the loader invocation count.
func redCtxScene(t *testing.T, cancelLeader bool) (leaderErr, waiterErr error, waiterVal string, runs int32) {
	t.Helper()
	mc := NewMemoryCache(WithMaxEntries(16))
	defer mc.Close()
	c := &redCtxCountingCache{Cache: mc}

	started := make(chan struct{})
	var startOnce sync.Once
	release := make(chan struct{})
	// canceledOkay gates the loader's ctx.Done branch: cancellation alone
	// must not complete the flight before the waiter has parked, otherwise
	// the waiter would lead a fresh (successful) flight and the assert
	// would prove nothing.
	canceledOkay := make(chan struct{})
	var loaderRuns atomic.Int32
	loader := func(ctx context.Context) (any, error) {
		loaderRuns.Add(1)
		startOnce.Do(func() { close(started) })
		select {
		case <-release:
			return "loaded", nil
		case <-ctx.Done():
			<-canceledOkay
			return nil, ctx.Err()
		}
	}

	leaderCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	leaderDone := make(chan error, 1)
	go func() {
		var v string
		leaderDone <- GetOrSet(leaderCtx, c, "hot", time.Minute, &v, loader)
	}()

	<-started // the leader is inside the loader; the flight is open.

	if cancelLeader {
		cancel() // the attacker disconnects; the flight stays open.
	}

	waiterDone := make(chan error, 1)
	go func() {
		waiterDone <- GetOrSet(context.Background(), c, "hot", time.Minute, &waiterVal, loader)
	}()

	// Bounded poll: the waiter's fast-path miss is the 3rd Get on the cache
	// (leader fast-path, leader in-flight re-check, waiter fast-path).
	deadline := time.Now().Add(5 * time.Second)
	for c.gets.Load() < 3 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if c.gets.Load() < 3 {
		t.Fatalf("setup broken: waiter never reached its fast-path miss (gets=%d)", c.gets.Load())
	}
	// The join is a handful of instructions past that miss; give it a
	// generous bound. runs==1 is asserted by the caller, so a missed join
	// surfaces as a setup failure, never a false pass.
	time.Sleep(100 * time.Millisecond)

	close(canceledOkay)
	close(release)

	select {
	case waiterErr = <-waiterDone:
	case <-time.After(5 * time.Second):
		t.Fatalf("setup broken: waiter GetOrSet did not return")
	}
	select {
	case leaderErr = <-leaderDone:
	case <-time.After(5 * time.Second):
		t.Fatalf("setup broken: leader GetOrSet did not return")
	}
	return leaderErr, waiterErr, waiterVal, loaderRuns.Load()
}

// TestGetOrSetRedWaiterKeepsOwnCtx: the leader cancels mid-load; a waiter
// with its own live Background context must not inherit that cancellation.
func TestGetOrSetRedWaiterKeepsOwnCtx(t *testing.T) {
	leaderErr, waiterErr, waiterVal, runs := redCtxScene(t, true)
	if runs != 1 {
		t.Fatalf("setup broken: loader ran %d times, want 1 — the waiter missed the open flight and re-loaded; widen the join window", runs)
	}
	if errors.Is(waiterErr, context.Canceled) {
		t.Errorf("SECURITY: [cache-leader-ctx] waiter's GetOrSet failed with context.Canceled although its own context is live: the singleflight loader runs under the LEADER's context (cache.go:76) and the shared error crosses request boundaries — one attacker disconnect fails every joined waiter's fill, and per-expiry repeat is a targeted availability DoS on hot keys")
	}
	if waiterErr == nil && waiterVal != "loaded" {
		t.Errorf("SECURITY: [cache-leader-ctx] waiter succeeded but decoded %q, want the loader's value", waiterVal)
	}
	// The leader itself failing is in contract: it canceled its own request.
	_ = leaderErr

	// Positive control: identical scene without the cancel — both callers
	// succeed via one loader run, proving the harness joins a real flight.
	cLeader, cWaiter, cVal, cRuns := redCtxScene(t, false)
	if cRuns != 1 || cLeader != nil || cWaiter != nil || cVal != "loaded" {
		t.Fatalf("setup broken: no-cancel control: leader=%v waiter=%v val=%q runs=%d, want nil/nil/loaded/1", cLeader, cWaiter, cVal, cRuns)
	}
}
