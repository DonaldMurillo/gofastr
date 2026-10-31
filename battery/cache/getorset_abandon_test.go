package cache

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

// gateCache holds the fill's re-check (the second read of the key) until
// gate closes.
type gateCache struct {
	*MemoryCache
	reads atomic.Int32
	gate  chan struct{}
}

func (g *gateCache) wait() {
	if g.reads.Add(1) == 2 {
		<-g.gate
	}
}

func (g *gateCache) Get(ctx context.Context, key string, dest any) error {
	g.wait()
	return g.MemoryCache.Get(ctx, key, dest)
}

func (g *gateCache) Exists(ctx context.Context, key string) (bool, error) {
	g.wait()
	return g.MemoryCache.Exists(ctx, key)
}

// The fill's re-check decoded into the leader's dest, so a fill that
// outlived its leader's deadline wrote the caller's variable after
// GetOrSet had returned.
func TestGetOrSetLeaderDestUntouched(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		mc := NewMemoryCache(WithMaxEntries(16))
		defer mc.Close()
		gc := &gateCache{MemoryCache: mc, gate: make(chan struct{})}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		var v string
		err := GetOrSet(ctx, gc, "k", time.Minute, &v, func(context.Context) (any, error) {
			return "loaded", nil
		})
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("err = %v, want DeadlineExceeded", err)
		}
		if err := mc.Set(context.Background(), "k", "other", time.Minute); err != nil {
			t.Fatal(err)
		}
		close(gc.gate)
		synctest.Wait()
		if v != "" {
			t.Fatalf("the fill wrote %q into a dest whose caller had left", v)
		}
	})
}

// A loader that ignored its cancellation stored its value after its last
// caller left, over the value a later caller had filled.
func TestGetOrSetAbandonedFillStoresNothing(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		mc := NewMemoryCache(WithMaxEntries(16))
		defer mc.Close()
		release := make(chan struct{})
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		var v string
		err := GetOrSet(ctx, mc, "k", time.Minute, &v, func(context.Context) (any, error) {
			<-release
			return "old", nil
		})
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("err = %v, want DeadlineExceeded", err)
		}
		if err := mc.Set(context.Background(), "k", "new", time.Minute); err != nil {
			t.Fatal(err)
		}
		close(release)
		synctest.Wait()
		var got string
		if err := mc.Get(context.Background(), "k", &got); err != nil || got != "new" {
			t.Fatalf("after the abandoned fill: %q, %v; want the later fill's \"new\"", got, err)
		}
	})
}

// A value-backed cache shares no fill, yet its loader ran detached from
// the caller, so a loader waiting on its context held the caller forever.
func TestGetOrSetValueCacheDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		var v string
		err := GetOrSet(ctx, newValueCache(), "k", time.Minute, &v, func(ctx context.Context) (any, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		})
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("err = %v, want DeadlineExceeded", err)
		}
	})
}

// The re-check decoded into an any, which reads a number as float64 and
// refused a stored 1e400 that the caller's own type holds.
func TestGetOrSetRecheckUsesDestType(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		mc := NewMemoryCache(WithMaxEntries(16))
		defer mc.Close()
		gc := &gateCache{MemoryCache: mc, gate: make(chan struct{})}
		var raw json.RawMessage
		done := make(chan error, 1)
		go func() {
			done <- GetOrSet(context.Background(), gc, "k", time.Minute, &raw, func(context.Context) (any, error) {
				return "loaded", nil
			})
		}()
		synctest.Wait()
		if err := mc.Set(context.Background(), "k", json.RawMessage("1e400"), time.Minute); err != nil {
			t.Fatal(err)
		}
		close(gc.gate)
		if err := <-done; err != nil || string(raw) != "1e400" {
			t.Fatalf("GetOrSet = %q, %v; want the stored 1e400", raw, err)
		}
	})
}

// A value-backed cache's loader that never looks at its context held the
// caller past its deadline, and stored after the caller had left.
func TestGetOrSetValueCacheStalledLoader(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		vc := newValueCache()
		release := make(chan struct{})
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		var v string
		err := GetOrSet(ctx, vc, "k", time.Minute, &v, func(context.Context) (any, error) {
			<-release
			return "late", nil
		})
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("err = %v, want DeadlineExceeded", err)
		}
		close(release)
		synctest.Wait()
		var got string
		if err := vc.Get(context.Background(), "k", &got); !errors.Is(err, ErrCacheMiss) {
			t.Fatalf("the abandoned fill stored %q (err %v)", got, err)
		}
	})
}

// A dest that points at an interface holding the decoding target lost
// that target in a zero-valued probe, and the re-check refused a value
// dest decodes.
func TestGetOrSetRecheckInterfaceDest(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		mc := NewMemoryCache(WithMaxEntries(16))
		defer mc.Close()
		gc := &gateCache{MemoryCache: mc, gate: make(chan struct{})}
		var raw json.RawMessage
		var dst json.Unmarshaler = &raw
		done := make(chan error, 1)
		go func() {
			done <- GetOrSet(context.Background(), gc, "k", time.Minute, &dst, func(context.Context) (any, error) {
				return "loaded", nil
			})
		}()
		synctest.Wait()
		if err := mc.Set(context.Background(), "k", json.RawMessage(`{"n":1}`), time.Minute); err != nil {
			t.Fatal(err)
		}
		close(gc.gate)
		if err := <-done; err != nil || string(raw) != `{"n":1}` {
			t.Fatalf("GetOrSet = %q, %v; want the stored object", raw, err)
		}
	})
}
