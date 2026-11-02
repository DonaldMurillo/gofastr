package cache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

// ErrCacheMiss is returned when a key is not found in the cache.
var ErrCacheMiss = errors.New("cache: key not found")

// Cache defines the interface for cache implementations.
type Cache interface {
	// Get retrieves a value from the cache and deserializes it into dest.
	// Returns ErrCacheMiss if the key does not exist or has expired.
	Get(ctx context.Context, key string, dest any) error

	// Set stores a value in the cache with the given TTL.
	// A TTL of 0 means the entry uses the default TTL. A negative TTL
	// is "already expired": nothing is stored and any existing entry
	// under the key is dropped (or refused, on backends that cannot
	// express it) — never widened into retention.
	Set(ctx context.Context, key string, value any, ttl time.Duration) error

	// Delete removes a key from the cache.
	Delete(ctx context.Context, key string) error

	// Exists checks whether a key exists in the cache and has not expired.
	Exists(ctx context.Context, key string) (bool, error)

	// Clear removes all entries from the cache.
	Clear(ctx context.Context) error
}

// Item represents a cached entry.
type Item struct {
	Key       string
	Value     any
	ExpiresAt time.Time
}

// Loader produces a value to populate the cache on a miss.
type Loader func(ctx context.Context) (any, error)

// flights collapses concurrent GetOrSet misses for the same pointer-backed
// cache instance and key. Value-backed Cache implementations have no stable
// identity available through the interface, so they fill on their own
// rather than risk joining a different instance's fill.
var flights = struct {
	sync.Mutex
	m map[string]*flight
}{m: map[string]*flight{}}

// flight is one shared fill. waiters counts the callers still waiting on
// it; the last one to leave before done cancels the fill.
type flight struct {
	done    chan struct{}
	err     error
	waiters int
	cancel  context.CancelFunc
	// val is the loader's value when this fill ran the loader and
	// stored it (loaded true). A waiter whose read-back misses decodes
	// it instead of reporting a miss.
	val    any
	loaded bool
}

// fillResult is what a fill hands back to its waiters: the loader's
// value, when the fill ran the loader.
type fillResult struct {
	val    any
	loaded bool
}

// joinFill runs fill once per key across concurrent callers and waits for
// it under the caller's own context. A caller whose context ends leaves
// with ctx.Err() at once; the fill keeps running for the callers still
// waiting, and is cancelled when none is left, so a stalled loader never
// holds a request past its deadline and never runs on for nobody.
func joinFill(ctx context.Context, key string, fill func(context.Context) (fillResult, error)) (fillResult, error) {
	flights.Lock()
	f := flights.m[key]
	if f == nil {
		// Detached from any single caller (see GetOrSet), cancelled only
		// by the last waiter leaving.
		fctx, cancel := context.WithCancel(context.WithoutCancel(ctx))
		f = &flight{done: make(chan struct{}), cancel: cancel}
		flights.m[key] = f
		go runFlight(fctx, key, f, fill)
	}
	f.waiters++
	flights.Unlock()

	select {
	case <-f.done:
		return fillResult{val: f.val, loaded: f.loaded}, f.err
	case <-ctx.Done():
		flights.Lock()
		f.waiters--
		if f.waiters == 0 {
			// Unhook it first: a caller arriving now starts a fresh fill
			// instead of joining one that is being cancelled.
			if flights.m[key] == f {
				delete(flights.m, key)
			}
			f.cancel()
		}
		flights.Unlock()
		return fillResult{}, ctx.Err()
	}
}

// runFlight runs fill on its own goroutine. A loader panic there would end
// the process rather than reach a caller's recover, so it comes back as
// every waiter's error.
func runFlight(ctx context.Context, key string, f *flight, fill func(context.Context) (fillResult, error)) {
	defer func() {
		if rec := recover(); rec != nil {
			f.err = fmt.Errorf("cache: loader panicked: %s", textsafe.Recovered(rec))
		}
		flights.Lock()
		if flights.m[key] == f {
			delete(flights.m, key)
		}
		flights.Unlock()
		f.cancel()
		close(f.done)
	}()
	res, err := fill(ctx)
	f.val, f.loaded, f.err = res.val, res.loaded, err
}

// GetOrSet returns the cached value for key, deserialized into dest. On a miss
// it invokes loader exactly once, even when many goroutines miss the same key
// concurrently, stores the result with the given TTL, and shares it with all
// waiters. A loader error is propagated and never cached.
//
// dest must be a non-nil pointer. The loader's returned value is round-tripped
// through the cache (JSON) so the value written into dest is the cached form.
// When the stored entry is already gone at read-back (evicted by a bounded
// cache or Redis maxmemory, or expired under a short TTL), dest gets the
// loaded value through the same JSON round trip instead of a miss.
// A negative ttl follows the Cache.Set contract: the loader still runs, but
// nothing is stored and GetOrSet surfaces the miss (ErrCacheMiss).
//
// The loader (and the shared fill's cache writes) runs under a context
// detached from any single caller: one requester aborting mid-load never
// fails another waiter's fill. Each caller's own context still governs its
// fast-path read, its wait (a caller whose context ends returns ctx.Err()
// while the fill runs on for the others) and its final read-back. The
// fill's context is cancelled when the last waiting caller leaves, and an
// abandoned fill stores nothing. A value-backed Cache shares no fill:
// each caller fills alone, and still leaves at its deadline. A loader
// panic is returned as an error.
func GetOrSet(ctx context.Context, c Cache, key string, ttl time.Duration, dest any, loader Loader) error {
	// Fast path: already cached.
	if err := c.Get(ctx, key, dest); err == nil {
		return nil
	} else if !errors.Is(err, ErrCacheMiss) {
		return err
	}

	// The flight is detached from any single caller: whichever goroutine
	// wins the race, its request context must not own the shared fill.
	// Under the leader's raw context, one caller disconnecting mid-load
	// handed context.Canceled to every joined waiter holding a live
	// context of its own — a request-scoped abort crossing request
	// boundaries. flightCtx keeps the context's values (trace, auth) and
	// drops its cancellation and deadline; joinFill cancels it once no
	// caller waits.
	fill := func(flightCtx context.Context) (fillResult, error) {
		// Re-check: another writer may have populated the cache while we
		// queued for the flight. It decodes into a fresh value of dest's
		// type, never dest itself: the fill can outlive the caller whose
		// dest it would write, and every caller reads its own dest back
		// below. Not into an any either: that decodes a number as float64
		// and refuses 1e400, which dest's own type may hold. A dest that
		// points at an interface may hold its decoding target there, which
		// a zero value of the interface loses; any JSON fits a RawMessage.
		probe := dest
		if t := reflect.TypeOf(dest); t != nil && t.Kind() == reflect.Pointer {
			if t.Elem().Kind() == reflect.Interface {
				probe = new(json.RawMessage)
			} else {
				probe = reflect.New(t.Elem()).Interface()
			}
		}
		if err := c.Get(flightCtx, key, probe); err == nil {
			return fillResult{}, nil
		} else if !errors.Is(err, ErrCacheMiss) {
			return fillResult{}, err
		}
		val, lerr := loader(flightCtx)
		if lerr != nil {
			return fillResult{}, lerr
		}
		// A fill abandoned by its last caller must not store: a later
		// caller may already have filled the key afresh, and this value
		// would overwrite it. A Set already under way when the last caller
		// leaves still lands; fencing that needs a compare-and-set the
		// Cache interface does not have.
		if err := flightCtx.Err(); err != nil {
			return fillResult{}, err
		}
		if err := c.Set(flightCtx, key, val, ttl); err != nil {
			return fillResult{}, err
		}
		return fillResult{val: val, loaded: true}, nil
	}
	var res fillResult
	var err error
	if flightKey, ok := cacheFlightKey(c, key); ok {
		res, err = joinFill(ctx, flightKey, fill)
	} else {
		// A value implementation may be a map- or slice-backed cache, so
		// formatting its interface value cannot produce reliable instance
		// identity. Fill alone instead of letting separate instances
		// with the same type/key share a result: a flight under a key no
		// other call uses, so the caller still leaves at its deadline.
		res, err = joinFill(ctx, "alone:"+strconv.FormatUint(aloneFills.Add(1), 10), fill)
	}
	if err != nil {
		return err
	}

	// Read back so every waiter (including the leader) fills dest from the
	// canonical cached representation.
	err = c.Get(ctx, key, dest)
	if errors.Is(err, ErrCacheMiss) && res.loaded && ttl >= 0 {
		// The value was loaded and stored, and is gone already: evicted
		// by a bounded cache or Redis maxmemory, or expired under a
		// short TTL. The load succeeded, so hand back its value through
		// the same JSON round trip the cache applies, rather than a miss
		// the caller would read as a failure. A negative ttl stored
		// nothing on purpose and keeps surfacing the miss.
		data, merr := json.Marshal(res.val)
		if merr != nil {
			return merr
		}
		return json.Unmarshal(data, dest)
	}
	return err
}

// aloneFills numbers the flights of value-backed caches. A shared key
// starts with a %T, which is never the bare word "alone".
var aloneFills atomic.Uint64

func cacheFlightKey(c Cache, key string) (string, bool) {
	v := reflect.ValueOf(c)
	if !v.IsValid() || v.Kind() != reflect.Pointer || v.IsNil() {
		return "", false
	}
	return fmt.Sprintf("%T:%x:%s", c, v.Pointer(), key), true
}

// config holds cache configuration set via options.
type config struct {
	defaultTTL      time.Duration
	prefix          string
	cleanupInterval time.Duration
	maxEntries      int // 0 means unbounded
}

// Option configures a cache instance.
type Option func(*config)

// WithTTL sets the default TTL for cache entries.
func WithTTL(d time.Duration) Option {
	return func(c *config) {
		c.defaultTTL = d
	}
}

// WithPrefix sets a key prefix for all cache operations.
func WithPrefix(p string) Option {
	return func(c *config) {
		c.prefix = p
	}
}

// WithCleanupInterval sets the interval for the background cleanup goroutine
// in MemoryCache. Defaults to 1 minute if not set.
func WithCleanupInterval(d time.Duration) Option {
	return func(c *config) {
		c.cleanupInterval = d
	}
}

// WithMaxEntries bounds a MemoryCache to at most n live entries. Once the cap
// is reached, inserting a new key evicts the least-recently-used entry (LRU).
//
// The zero-config default is UNBOUNDED (n <= 0), preserving historical
// behavior. Set this whenever cache keys are influenced by untrusted input to
// avoid unbounded memory growth (OOM/DoS). It has no effect on RedisCache,
// where eviction is governed by the Redis server's own maxmemory policy.
func WithMaxEntries(n int) Option {
	return func(c *config) {
		if n < 0 {
			n = 0
		}
		c.maxEntries = n
	}
}

func applyOptions(opts ...Option) config {
	cfg := config{
		defaultTTL:      0, // no expiry by default
		cleanupInterval: time.Minute,
	}
	for _, o := range opts {
		o(&cfg)
	}
	return cfg
}
