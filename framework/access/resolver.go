package access

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/core/handler"
)

const defaultResolverTTL = 30 * time.Second

// maxResolverEntries bounds the cache: user ids are attacker-mintable.
const maxResolverEntries = 100_000

// CachedResolver caches resolved roles per user id and tenant. A context with
// no user exposing GetID() is resolved uncached.
type CachedResolver struct {
	resolve   func(context.Context) []string
	ttl       time.Duration
	lastSweep time.Time

	mu      sync.Mutex
	entries map[resolverKey]*cachedRoleEntry
}

type resolverKey struct {
	user       string
	tenant     string
	authTenant string
}

type cachedRoleEntry struct {
	roles     []string
	expiresAt time.Time
	ready     chan struct{}
	shared    bool
}

// CachedResolverOption configures a CachedResolver.
type CachedResolverOption func(*CachedResolver)

// WithTTL sets how long resolved roles stay cached (default 30s). Zero or
// negative keeps single-flight but caches nothing.
func WithTTL(ttl time.Duration) CachedResolverOption {
	return func(resolver *CachedResolver) {
		//gofastr:allow(optzero) a zero or negative TTL is the documented "single-flight, no caching" setting
		resolver.ttl = ttl
	}
}

// NewCachedResolver wraps resolve with per-user TTL caching. A nil resolve panics.
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

// Resolve returns a copy of the current user's roles. Concurrent misses for one
// key share a single resolve call.
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
				r.mu.Unlock()
				<-entry.ready
				if entry.shared {
					return cloneRoles(entry.roles)
				}
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
		flight.roles, flight.shared = roles, true
	}
	close(flight.ready)
	r.mu.Unlock()
	return roles
}

func (r *CachedResolver) sweep() {
	now := time.Now()
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.entries) < maxResolverEntries && now.Sub(r.lastSweep) < r.ttl {
		return
	}
	r.lastSweep = now
	for key, entry := range r.entries {
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

// Invalidate drops userID's cached roles in every tenant, on this replica only;
// other replicas keep theirs until the TTL.
func (r *CachedResolver) Invalidate(userID string) {
	r.mu.Lock()
	for key := range r.entries {
		if key.user == userID {
			delete(r.entries, key)
		}
	}
	r.mu.Unlock()
}

// InvalidateAll drops every cached entry on this replica.
func (r *CachedResolver) InvalidateAll() {
	r.mu.Lock()
	r.entries = make(map[resolverKey]*cachedRoleEntry)
	r.mu.Unlock()
}

func resolverUserID(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	id := UserID(ctx)
	return id, id != ""
}

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
