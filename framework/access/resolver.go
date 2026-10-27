package access

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/core/handler"
)

const defaultResolverTTL = 30 * time.Second

// maxResolverEntries caps the number of distinct user ids the cache tracks
// at once. The key is attacker-mintable (an account-registration flood
// mints one id per account), so without a cap the map grows monotonically
// with the number of distinct ids ever seen, not with live users. Mirrors
// the ratelimit maxKeys precedent; far above any legitimate concurrent
// user count.
const maxResolverEntries = 100_000

// CachedResolver caches resolved roles by the authenticated user's ID and the
// request's tenant. Contexts without a user exposing GetID() are resolved
// without caching so anonymous callers can never share an entry.
type CachedResolver struct {
	resolve   func(context.Context) []string
	ttl       time.Duration
	lastSweep time.Time

	mu      sync.Mutex
	entries map[resolverKey]*cachedRoleEntry
}

// resolverKey partitions the cache. A user's roles may depend on the tenant
// the request runs in (admin in one, viewer in another), so the tenant is
// part of the key: keyed by user alone, the first tenant's answer was served
// to the user's requests in every other tenant for a TTL. Both tenant slots
// count: the resolved tenant ID (handler.SetTenantID, which
// framework/tenant.SetTenantID writes) and the auth-seeded handler.SetTenant
// value when it is a string. A struct key, not a joined string, so no
// separator can make two different tuples collide.
type resolverKey struct {
	user       string
	tenant     string
	authTenant string
}

type cachedRoleEntry struct {
	roles     []string
	expiresAt time.Time
	ready     chan struct{}
}

// CachedResolverOption configures a CachedResolver.
type CachedResolverOption func(*CachedResolver)

// WithTTL sets the duration resolved roles remain cached. A zero or negative
// duration retains single-flight behavior but expires the result immediately.
func WithTTL(ttl time.Duration) CachedResolverOption {
	return func(resolver *CachedResolver) {
		resolver.ttl = ttl
	}
}

// NewCachedResolver wraps a role resolver with per-user TTL caching. It derives
// the cache key from the authenticated user installed in core/handler context,
// the same identity seam used by battery/auth and access.Middleware resolvers.
func NewCachedResolver(resolve func(context.Context) []string, opts ...CachedResolverOption) *CachedResolver {
	if resolve == nil {
		panic("access: NewCachedResolver requires a resolver")
	}
	resolver := &CachedResolver{
		resolve: resolve,
		ttl:     defaultResolverTTL,
		entries: make(map[resolverKey]*cachedRoleEntry),
	}
	for _, opt := range opts {
		opt(resolver)
	}
	return resolver
}

// Resolve returns the current user's roles. Concurrent misses for the same
// user share one resolver call. Returned roles are defensive copies.
func (r *CachedResolver) Resolve(ctx context.Context) []string {
	userID, ok := resolverUserID(ctx)
	if !ok {
		return cloneRoles(r.resolve(ctx))
	}
	key := resolverKey{user: userID, tenant: handler.GetTenantID(ctx), authTenant: resolverAuthTenant(ctx)}

	r.sweep()

	for {
		r.mu.Lock()
		entry := r.entries[key]
		if entry != nil {
			if entry.ready != nil {
				ready := entry.ready
				r.mu.Unlock()
				<-ready
				continue
			}
			if time.Now().Before(entry.expiresAt) {
				roles := cloneRoles(entry.roles)
				r.mu.Unlock()
				return roles
			}
		}

		flight := &cachedRoleEntry{ready: make(chan struct{})}
		r.entries[key] = flight
		r.mu.Unlock()

		roles := r.resolveFlight(ctx, key, flight)
		return cloneRoles(roles)
	}
}

func (r *CachedResolver) resolveFlight(ctx context.Context, key resolverKey, flight *cachedRoleEntry) (roles []string) {
	defer func() {
		if recovered := recover(); recovered != nil {
			r.mu.Lock()
			if r.entries[key] == flight {
				delete(r.entries, key)
			}
			close(flight.ready)
			r.mu.Unlock()
			panic(recovered)
		}
	}()

	roles = cloneRoles(r.resolve(ctx))
	r.mu.Lock()
	// A resolution that observed a dead context is not truth: the resolver
	// seam has no error return, so a canceled DB lookup reads as "no roles".
	// Cache nothing and share nothing — drop the flight so waiters resolve
	// again under their own contexts, the same way Invalidate leaves an
	// in-flight result uncached. One aborted request must not strip a
	// user's permissions for a TTL window.
	if ctx.Err() != nil {
		if r.entries[key] == flight {
			delete(r.entries, key)
		}
		close(flight.ready)
		r.mu.Unlock()
		return roles
	}
	if r.entries[key] == flight {
		r.entries[key] = &cachedRoleEntry{
			roles:     roles,
			expiresAt: time.Now().Add(r.ttl),
		}
	}
	close(flight.ready)
	r.mu.Unlock()
	return roles
}

// sweep reclaims cache entries opportunistically so an expired entry for a
// user who never returns is reclaimed without that key being resolved
// again: at most once per TTL (the flashStore.put / ratelimit.AllowContext
// posture) and unconditionally once the entry cap is hit. If the map is
// still at/over the cap after shedding expired entries (a distinct-id flood
// that outpaces the TTL sweep), entries are dropped to a low-water mark,
// soonest-expiring first, so the map stays bounded. Dropping a settled
// entry is always safe: re-creating it lazily just resolves again.
func (r *CachedResolver) sweep() {
	now := time.Now()
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.entries) < maxResolverEntries && now.Sub(r.lastSweep) < r.ttl {
		return
	}
	r.lastSweep = now
	for key, entry := range r.entries {
		// In-flight entries are settled in microseconds; leave them to
		// their own install path rather than unparking waiters early.
		if entry.ready == nil && !now.Before(entry.expiresAt) {
			delete(r.entries, key)
		}
	}
	if len(r.entries) < maxResolverEntries {
		return
	}
	lowWater := maxResolverEntries * 9 / 10
	type expiring struct {
		key     resolverKey
		expires time.Time
	}
	pending := make([]expiring, 0, len(r.entries))
	for key, entry := range r.entries {
		pending = append(pending, expiring{key: key, expires: entry.expiresAt})
	}
	sort.Slice(pending, func(i, j int) bool {
		if !pending[i].expires.Equal(pending[j].expires) {
			return pending[i].expires.Before(pending[j].expires)
		}
		// Same-clock ties (a flood resolves in one tick): fall back to key
		// order so the shed is deterministic despite randomized iteration.
		a, b := pending[i].key, pending[j].key
		if a.user != b.user {
			return a.user < b.user
		}
		if a.tenant != b.tenant {
			return a.tenant < b.tenant
		}
		return a.authTenant < b.authTenant
	})
	for i := 0; i < len(pending) && len(r.entries) > lowWater; i++ {
		delete(r.entries, pending[i].key)
	}
}

// Invalidate removes one user's cached roles in every tenant. An in-flight
// result is not cached; calls waiting on it observe the invalidation and
// resolve again.
func (r *CachedResolver) Invalidate(userID string) {
	r.mu.Lock()
	for key := range r.entries {
		if key.user == userID {
			delete(r.entries, key)
		}
	}
	r.mu.Unlock()
}

// InvalidateAll removes every cached role resolution. In-flight results are
// not cached, and calls waiting on them resolve again.
func (r *CachedResolver) InvalidateAll() {
	r.mu.Lock()
	r.entries = make(map[resolverKey]*cachedRoleEntry)
	r.mu.Unlock()
}

func resolverUserID(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	user, ok := handler.GetUser(ctx)
	if !ok || user == nil {
		return "", false
	}
	identified, ok := user.(interface{ GetID() string })
	if !ok {
		return "", false
	}
	userID := identified.GetID()
	return userID, userID != ""
}

// resolverAuthTenant is the auth-seeded tenant (handler.SetTenant) when it is
// a string. Other types are left out of the key rather than formatted: a
// pointer formats to its address and would split the cache per request.
func resolverAuthTenant(ctx context.Context) string {
	v, _ := handler.GetTenant(ctx)
	s, _ := v.(string)
	return s
}

func cloneRoles(roles []string) []string {
	if len(roles) == 0 {
		return nil
	}
	cloned := make([]string, len(roles))
	copy(cloned, roles)
	return cloned
}
