package access

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/handler"
)

type secResolverUser string

func (u secResolverUser) GetID() string { return string(u) }

func secResolverCtx(id string) context.Context {
	return handler.SetUser(context.Background(), secResolverUser(id))
}

// Pins the unbounded resolver cache, found by the 2026-09-04 red-probe
// round; fixed in CachedResolver by an opportunistic TTL sweep plus a
// max-entries cap (the flashStore.put / ratelimit maxKeys posture).
// Property: a per-user cache keyed on identity must reclaim entries whose
// TTL has lapsed - an expired entry for a user who never comes back may not
// be retained indefinitely just because that key is never resolved again.
// Surfaces: framework/access/resolver.go CachedResolver.Resolve (sweeps at
// most once per TTL, unconditionally once the cap is hit) and
// CachedResolver.sweep (expired-first reclaim, low-water shed over
// maxResolverEntries).
func TestResolverReclaimsExpiredEntries(t *testing.T) {
	resolver := NewCachedResolver(
		func(context.Context) []string { return []string{"member"} },
		WithTTL(time.Millisecond),
	)

	// 1000 distinct identities resolve once and never return.
	for i := range 1000 {
		_ = resolver.Resolve(secResolverCtx(fmt.Sprintf("u-%04d", i)))
	}
	time.Sleep(10 * time.Millisecond) // every entry is now past its TTL

	// Housekeeping point: any later resolve (here: a brand-new user) is the
	// moment an implementation may sweep, exactly like flashStore.put.
	_ = resolver.Resolve(secResolverCtx("fresh-user"))

	resolver.mu.Lock()
	retained := len(resolver.entries)
	resolver.mu.Unlock()

	// The fresh entry (and at most one racing flight) may remain; the 1000
	// expired ones may not.
	if retained > 2 {
		t.Fatalf("SECURITY: [resolver-cache] %d expired role-cache entries retained past their TTL with no revisit - the entries map only grows with distinct user ids (attacker-mintable via registration), never shrinks", retained)
	}
}

// Pins the resolver's distinct-id flood bound, found by the 2026-09-04
// red-probe round; fixed in CachedResolver.sweep by shedding to a
// low-water mark once maxResolverEntries is reached (soonest-expiring
// first), mirroring ratelimit's evictLocked.
// Property: the cache map must stay bounded even when every entry is live
// (a flood that outpaces the TTL sweep), because the keys are
// attacker-mintable distinct user ids, not live users.
// Surfaces: framework/access/resolver.go CachedResolver.sweep (cap shed).
func TestResolverShedsPastEntryCap(t *testing.T) {
	resolver := NewCachedResolver(func(context.Context) []string { return nil })
	resolver.mu.Lock()
	for i := range maxResolverEntries + 500 {
		resolver.entries[fmt.Sprintf("u-%06d", i)] = &cachedRoleEntry{
			roles:     []string{"member"},
			expiresAt: time.Now().Add(time.Hour), // all live: only the cap forces the shed
		}
	}
	resolver.mu.Unlock()

	// The cap is hit on the next housekeeping point. Building the state
	// directly keeps the test off the 100k-resolve flood path, the same
	// shortcut ratelimit's TestElapsedBlockIsReclaimed takes.
	resolver.sweep()

	resolver.mu.Lock()
	retained := len(resolver.entries)
	resolver.mu.Unlock()
	lowWater := maxResolverEntries * 9 / 10
	if retained > lowWater {
		t.Fatalf("SECURITY: [resolver-cache] %d live entries retained past the %d cap - a distinct-id flood grows the map without bound", retained, maxResolverEntries)
	}
}

// Pins the canceled-context cache poisoning, found by the 2026-09-04
// red-probe round; fixed in resolveFlight by treating a ctx whose Err() is
// non-nil at completion as uncacheable (the flight is dropped, not
// installed), mirroring how Invalidate leaves in-flight results uncached.
// Property: a role resolution that observed a canceled request context must
// not be cached for (or single-flight-shared with) later callers whose
// contexts are alive: the resolver seam has no error return, so a canceled
// DB lookup reads as "no roles" and one aborted request strips a user's
// permissions for a full TTL.
// Surfaces: framework/access/resolver.go CachedResolver.Resolve (passes
// the first caller's ctx into resolve) and resolveFlight (ctx.Err() gate
// before the entry install).
func TestResolverSkipsCacheOnCanceledCtx(t *testing.T) {
	resolver := NewCachedResolver(func(ctx context.Context) []string {
		if ctx.Err() != nil {
			return nil // the normal DB-backed-resolver shape: canceled lookup, no roles
		}
		return []string{"admin"}
	}, WithTTL(time.Minute))

	// One aborted request whose context dies before the resolver runs.
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if roles := resolver.Resolve(handler.SetUser(canceled, secResolverUser("u-1"))); roles != nil {
		t.Fatalf("sanity: canceled resolve returned %v, want nil", roles)
	}

	// A healthy request for the same user must not inherit the poisoned entry.
	got := resolver.Resolve(secResolverCtx("u-1"))
	if len(got) == 0 || got[0] != "admin" {
		t.Fatalf("SECURITY: [resolver-cache] one canceled request's nil role resolution was cached and served to a healthy request: user u-1 resolved to %v for the full TTL (want [admin])", got)
	}
}
