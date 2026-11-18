// Package lifecycle is the graceful-shutdown engine: drainers run in order under one timeout, health checkers go unhealthy first.
package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// ErrShuttingDown is returned by a registration made after Shutdown has begun.
var ErrShuttingDown = errors.New("lifecycle: shutdown already started")

// Drainer flushes pending work at shutdown, blocking until done or ctx ends.
type Drainer interface {
	Drain(ctx context.Context) error
}

// DrainFunc is a function adapter for Drainer.
type DrainFunc func(ctx context.Context) error

// Drain calls the underlying function.
func (f DrainFunc) Drain(ctx context.Context) error { return f(ctx) }

// HealthChecker reports readiness.
type HealthChecker interface {
	IsHealthy() bool
}

// Lifecycle manages the graceful shutdown sequence.
type Lifecycle struct {
	mu           sync.Mutex
	drainers     []Drainer
	checkers     []HealthChecker
	timeout      time.Duration
	shuttingDown atomic.Bool
}

// New creates a new Lifecycle manager.
func New() *Lifecycle {
	return &Lifecycle{
		timeout: 30 * time.Second,
	}
}

// WithTimeout sets the drain timeout (default 30s); d <= 0 keeps the current one.
func WithTimeout(d time.Duration) func(*Lifecycle) {
	return func(lc *Lifecycle) {
		if d > 0 {
			lc.timeout = d
		}
	}
}

// RegisterDrainer appends d to the drain order, or returns ErrShuttingDown once Shutdown has begun.
func (lc *Lifecycle) RegisterDrainer(d Drainer) error {
	return lc.AppendDrainer(d)
}

// AppendDrainer is RegisterDrainer.
func (lc *Lifecycle) AppendDrainer(d Drainer) error {
	return lc.whileLive(func() { lc.drainers = append(lc.drainers, d) })
}

func (lc *Lifecycle) whileLive(add func()) error {
	if lc.shuttingDown.Load() {
		return ErrShuttingDown
	}
	lc.mu.Lock()
	defer lc.mu.Unlock()
	if lc.shuttingDown.Load() {
		return ErrShuttingDown
	}
	add()
	return nil
}

// PrependDrainer puts d at the front of the drain order, or returns ErrShuttingDown.
func (lc *Lifecycle) PrependDrainer(d Drainer) error {
	return lc.whileLive(func() { lc.drainers = append([]Drainer{d}, lc.drainers...) })
}

// RegisterHealthChecker adds hc to IsHealthy, or returns ErrShuttingDown.
func (lc *Lifecycle) RegisterHealthChecker(hc HealthChecker) error {
	return lc.whileLive(func() { lc.checkers = append(lc.checkers, hc) })
}

// Shutdown executes once: IsHealthy turns false, then drainers run in registration order under one shared timeout; returns the first error.
func (lc *Lifecycle) Shutdown(ctx context.Context) error {
	lc.shuttingDown.Store(true)

	lc.mu.Lock()
	timeout := lc.timeout
	drainers := make([]Drainer, len(lc.drainers))
	copy(drainers, lc.drainers)
	lc.drainers = nil
	lc.mu.Unlock()

	drainCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var firstErr error
	recordErr := func(err error) {
		if firstErr == nil {
			firstErr = err
		}
	}

	for _, d := range drainers {
		func(d Drainer) {
			defer func() {
				if r := recover(); r != nil {
					recordErr(fmt.Errorf("drain panicked: %v", r))
				}
			}()
			if err := d.Drain(drainCtx); err != nil {
				recordErr(fmt.Errorf("drain failed: %w", err))
			}
		}(d)
	}
	return firstErr
}

// RunWithSignals waits for SIGINT or SIGTERM and then runs Shutdown; it returns nil if ctx ends first.
func (lc *Lifecycle) RunWithSignals(ctx context.Context) error {
	return lc.RunWithSignalsUsing(ctx, lc.Shutdown)
}

// RunWithSignalsUsing is RunWithSignals calling shutdown instead of Shutdown.
func (lc *Lifecycle) RunWithSignalsUsing(ctx context.Context, shutdown func(context.Context) error) error {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	select {
	case <-sigCh:
		return shutdown(ctx)
	case <-ctx.Done():
		return nil
	}
}

// IsHealthy reports whether Shutdown has not begun and every checker is healthy.
func (lc *Lifecycle) IsHealthy() bool {
	if lc.shuttingDown.Load() {
		return false
	}
	lc.mu.Lock()
	checkers := make([]HealthChecker, len(lc.checkers))
	copy(checkers, lc.checkers)
	lc.mu.Unlock()

	for _, hc := range checkers {
		if !hc.IsHealthy() {
			return false
		}
	}
	return true
}

// SetShutdownTimeout sets the drain timeout; d <= 0 leaves it unchanged.
func (lc *Lifecycle) SetShutdownTimeout(d time.Duration) {
	if d <= 0 {
		return
	}
	lc.mu.Lock()
	defer lc.mu.Unlock()
	lc.timeout = d
}
