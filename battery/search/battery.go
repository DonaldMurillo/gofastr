package search

import (
	"context"
	"io"

	"github.com/DonaldMurillo/gofastr/framework"
)

// Battery is the framework.Battery adapter for the search battery.
type Battery struct {
	b Backend
}

// NewBattery wraps b in a framework lifecycle adapter.
func NewBattery(b Backend) *Battery {
	return &Battery{b: b}
}

// Backend returns the underlying Backend so callers can inject it where
// needed without reaching through the battery wrapper.
func (bat *Battery) Backend() Backend { return bat.b }

// Name implements framework.Battery.
func (bat *Battery) Name() string { return "search" }

// Init implements framework.Battery.
func (bat *Battery) Init(_ *framework.App) error { return nil }

// OnStart implements framework.BatteryLifecycle.
func (bat *Battery) OnStart(_ context.Context) error { return nil }

// OnStop implements framework.BatteryLifecycle: it closes the backend
// when the backend implements io.Closer.
func (bat *Battery) OnStop(_ context.Context) error {
	if c, ok := bat.b.(io.Closer); ok {
		return c.Close()
	}
	return nil
}

var _ framework.BatteryLifecycle = (*Battery)(nil)
