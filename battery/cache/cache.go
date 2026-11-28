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
	"github.com/DonaldMurillo/gofastr/internal/jsonexact"
)

// ErrCacheMiss is returned when a key is not found in the cache.
var ErrCacheMiss = errors.New("cache: key not found")

// Cache defines the interface for cache implementations.
type Cache interface {
	// Get retrieves a value from the cache and deserializes it into dest.
	Get(ctx context.Context, key string, dest any) error

	// Set stores a value in the cache with the given TTL.
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

var flights = struct {
	sync.Mutex
	m map[string]*flight
}{m: map[string]*flight{}}

type flight struct {
	done    chan struct{}
	err     error
	waiters int
	cancel  context.CancelFunc
	val     any
	loaded  bool
}

type fillResult struct {
	val    any
	loaded bool
}

func joinFill(ctx context.Context, key string, fill func(context.Context) (fillResult, error)) (fillResult, error) {
	flights.Lock()
	f := flights.m[key]
	if f == nil {
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
			if flights.m[key] == f {
				delete(flights.m, key)
			}
			f.cancel()
		}
		flights.Unlock()
		return fillResult{}, ctx.Err()
	}
}

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

// GetOrSet returns the cached value for key, deserialized into dest.
func GetOrSet(ctx context.Context, c Cache, key string, ttl time.Duration, dest any, loader Loader) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	target := reflect.ValueOf(dest)
	if !target.IsValid() || target.Kind() != reflect.Pointer || target.IsNil() {
		return &json.InvalidUnmarshalError{Type: reflect.TypeOf(dest)}
	}
	targetType := target.Type().Elem()

	// Fast path: already cached.
	if err := c.Get(ctx, key, dest); err == nil {
		return nil
	} else if !errors.Is(err, ErrCacheMiss) {
		return err
	}

	fill := func(flightCtx context.Context) (fillResult, error) {
		// Re-check: another writer may have populated the cache while we
		// queued for the flight. It decodes into a fresh value of dest's
		// type, never dest itself: the fill can outlive the caller whose
		// dest it would write, and every caller reads its own dest back
		// below. Not into an any either: that decodes a number as float64
		// and refuses 1e400, which dest's own type may hold. A dest that
		// points at an interface may hold its decoding target there, which
		// a zero value of the interface loses; any JSON fits a RawMessage.
		var probe any
		if targetType.Kind() == reflect.Interface {
			probe = new(json.RawMessage)
		} else {
			probe = reflect.New(targetType).Interface()
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
		res, err = joinFill(ctx, "alone:"+strconv.FormatUint(aloneFills.Add(1), 10), fill)
	}
	if err != nil {
		return err
	}

	// Read back so every waiter (including the leader) fills dest from the
	// canonical cached representation.
	if err := ctx.Err(); err != nil {
		return err
	}
	err = c.Get(ctx, key, dest)
	if errors.Is(err, ErrCacheMiss) && res.loaded && ttl >= 0 {
		data, merr := json.Marshal(res.val)
		if merr != nil {
			return merr
		}
		return jsonexact.Unmarshal(data, dest)
	}
	return err
}

var aloneFills atomic.Uint64

func cacheFlightKey(c Cache, key string) (string, bool) {
	v := reflect.ValueOf(c)
	if !v.IsValid() || v.Kind() != reflect.Pointer || v.IsNil() {
		return "", false
	}
	return fmt.Sprintf("%T:%x:%s", c, v.Pointer(), key), true
}

type config struct {
	defaultTTL      time.Duration
	prefix          string
	cleanupInterval time.Duration
	maxEntries      int
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

// WithCleanupInterval sets the interval for the background cleanup
// goroutine in MemoryCache.
func WithCleanupInterval(d time.Duration) Option {
	return func(c *config) {
		c.cleanupInterval = d
	}
}

// WithMaxEntries bounds a MemoryCache to at most n live entries.
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
		defaultTTL:      0,
		cleanupInterval: time.Minute,
	}
	for _, o := range opts {
		o(&cfg)
	}
	return cfg
}
