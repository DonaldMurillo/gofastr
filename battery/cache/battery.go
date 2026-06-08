package cache

import (
	"context"
	"io"

	"github.com/DonaldMurillo/gofastr/framework"
)

// Battery is the framework.Battery + framework.BatteryLifecycle adapter for
// the cache battery.  It ties the lifecycle of the underlying Cache (and
// especially MemoryCache's background cleanup goroutine) into the App's
// structured start/stop sequence.
//
// Construct via [NewBattery] and register:
//
//	c := cache.NewMemoryCache(cache.WithTTL(5 * time.Minute))
//	app.Batteries.Register(cache.NewBattery(c))
//
// The battery's Init is a no-op — the cache is fully configured before
// registration.  OnStop closes the underlying cache if it implements
// io.Closer (MemoryCache does; RedisCache does not).
type Battery struct {
	c Cache
}

// NewBattery wraps c in a framework lifecycle adapter.
func NewBattery(c Cache) *Battery {
	return &Battery{c: c}
}

// Cache returns the underlying Cache so callers can inject it where needed
// without reaching through the battery wrapper.
func (b *Battery) Cache() Cache { return b.c }

// Name implements framework.Battery.
func (b *Battery) Name() string { return "cache" }

// Init implements framework.Battery.  The cache is fully constructed by the
// caller before registration so Init is a no-op here.
func (b *Battery) Init(_ *framework.App) error { return nil }

// OnStart implements framework.BatteryLifecycle.  No startup work needed.
func (b *Battery) OnStart(_ context.Context) error { return nil }

// OnStop implements framework.BatteryLifecycle.  Stops the background
// cleanup goroutine on MemoryCache (or any cache that implements io.Closer).
func (b *Battery) OnStop(_ context.Context) error {
	if cl, ok := b.c.(io.Closer); ok {
		return cl.Close()
	}
	return nil
}

// Compile-time interface checks.
var (
	_ framework.Battery          = (*Battery)(nil)
	_ framework.BatteryLifecycle = (*Battery)(nil)
)
