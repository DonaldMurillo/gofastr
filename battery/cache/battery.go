package cache

import (
	"context"
	"io"

	"github.com/DonaldMurillo/gofastr/framework"
)

// Battery is the framework.Battery + framework.BatteryLifecycle adapter
// for the cache battery.
type Battery struct {
	c Cache
}

// NewBattery wraps c in a framework lifecycle adapter.
func NewBattery(c Cache) *Battery {
	return &Battery{c: c}
}

// Cache returns the underlying Cache so callers can inject it where
// needed without reaching through the battery wrapper.
func (b *Battery) Cache() Cache { return b.c }

// Name implements framework.Battery.
func (b *Battery) Name() string { return "cache" }

// Init implements framework.Battery.
func (b *Battery) Init(_ *framework.App) error { return nil }

// OnStart implements framework.BatteryLifecycle.
func (b *Battery) OnStart(_ context.Context) error { return nil }

// OnStop implements framework.BatteryLifecycle.
func (b *Battery) OnStop(_ context.Context) error {
	switch cl := b.c.(type) {
	case io.Closer:
		return cl.Close()
	case interface{ Close() }:
		cl.Close()
	}
	return nil
}

var (
	_ framework.Battery          = (*Battery)(nil)
	_ framework.BatteryLifecycle = (*Battery)(nil)
)
