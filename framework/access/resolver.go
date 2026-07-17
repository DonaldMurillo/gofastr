package access

import (
	"context"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/core/handler"
)

const defaultResolverTTL = 30 * time.Second

// CachedResolver caches resolved roles by the authenticated user's ID.
// Contexts without a user exposing GetID() are resolved without caching so
// anonymous callers can never share an entry.
type CachedResolver struct {
	resolve func(context.Context) []string
	ttl     time.Duration

	mu      sync.Mutex
	entries map[string]*cachedRoleEntry
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
		entries: make(map[string]*cachedRoleEntry),
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

	for {
		r.mu.Lock()
		entry := r.entries[userID]
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
		r.entries[userID] = flight
		r.mu.Unlock()

		roles := r.resolveFlight(ctx, userID, flight)
		return cloneRoles(roles)
	}
}

func (r *CachedResolver) resolveFlight(ctx context.Context, userID string, flight *cachedRoleEntry) (roles []string) {
	defer func() {
		if recovered := recover(); recovered != nil {
			r.mu.Lock()
			if r.entries[userID] == flight {
				delete(r.entries, userID)
			}
			close(flight.ready)
			r.mu.Unlock()
			panic(recovered)
		}
	}()

	roles = cloneRoles(r.resolve(ctx))
	r.mu.Lock()
	if r.entries[userID] == flight {
		r.entries[userID] = &cachedRoleEntry{
			roles:     roles,
			expiresAt: time.Now().Add(r.ttl),
		}
	}
	close(flight.ready)
	r.mu.Unlock()
	return roles
}

// Invalidate removes one user's cached roles. An in-flight result is not
// cached; calls waiting on it observe the invalidation and resolve again.
func (r *CachedResolver) Invalidate(userID string) {
	r.mu.Lock()
	delete(r.entries, userID)
	r.mu.Unlock()
}

// InvalidateAll removes every cached role resolution. In-flight results are
// not cached, and calls waiting on them resolve again.
func (r *CachedResolver) InvalidateAll() {
	r.mu.Lock()
	r.entries = make(map[string]*cachedRoleEntry)
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

func cloneRoles(roles []string) []string {
	if len(roles) == 0 {
		return nil
	}
	cloned := make([]string, len(roles))
	copy(cloned, roles)
	return cloned
}
