// Package lifecycle provides a documented, cooperative graceful-shutdown
// contract for GoFastr applications. It formalises the drain-in-flight /
// stop-accepting / flush-queues sequence and exposes plugin hooks so
// batteries can cooperate during shutdown.
//
// The lifecycle phases are:
//
//  1. Drain — stop accepting new requests, let in-flight requests finish.
//     Signaled via context cancellation. HTTP server Shutdown handles this.
//
//  2. Flush — batteries flush their queues, buffers, and pending writes.
//     Each battery that implements Drainer participates.
//
//  3. Stop — final cleanup: close connections, release resources.
//     OnStop hooks fire in reverse registration order.
//
// Usage (typically done by App.Start automatically):
//
//	lc := lifecycle.New(app)
//	lc.RegisterDrainer(queueBattery)
//	lc.RegisterDrainer(cacheBattery)
//	app.OnStop(lc.Shutdown)
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

// ErrShuttingDown is returned by RegisterDrainer / RegisterHealthChecker
// when called after Shutdown has begun.
var ErrShuttingDown = errors.New("lifecycle: shutdown already started")

// Drainer is implemented by batteries and plugins that need to flush
// pending work during graceful shutdown. Drain should block until all
// in-flight work is complete or the context is cancelled.
//
// Example: a queue battery drains its worker goroutines; a cache battery
// writes dirty entries to disk.
type Drainer interface {
	Drain(ctx context.Context) error
}

// DrainFunc is a function adapter for Drainer.
type DrainFunc func(ctx context.Context) error

// Drain calls the underlying function.
func (f DrainFunc) Drain(ctx context.Context) error { return f(ctx) }

// HealthChecker is implemented by components that can report readiness.
// During shutdown, components report unhealthy so load balancers stop
// sending traffic before the drain begins.
type HealthChecker interface {
	IsHealthy() bool
}

// Lifecycle manages the graceful shutdown sequence.
type Lifecycle struct {
	mu            sync.Mutex
	drainers      []Drainer
	checkers      []HealthChecker
	timeout       time.Duration
	shuttingDown  atomic.Bool
}

// New creates a new Lifecycle manager.
func New() *Lifecycle {
	return &Lifecycle{
		timeout: 30 * time.Second,
	}
}

// WithTimeout sets the maximum duration for the drain phase.
// Default is 30 seconds.
func WithTimeout(d time.Duration) func(*Lifecycle) {
	return func(lc *Lifecycle) { lc.timeout = d }
}

// RegisterDrainer adds a component to the drain phase. Drainers are
// called sequentially during shutdown, in registration order. Returns
// ErrShuttingDown if Shutdown has already started — late registrations
// are dropped to keep the shutdown snapshot deterministic.
func (lc *Lifecycle) RegisterDrainer(d Drainer) error {
	return lc.AppendDrainer(d)
}

// AppendDrainer adds a drainer to the END of the drain order. Equivalent
// to RegisterDrainer; named explicitly when callers want to be explicit
// about ordering relative to PrependDrainer.
func (lc *Lifecycle) AppendDrainer(d Drainer) error {
	if lc.shuttingDown.Load() {
		return ErrShuttingDown
	}
	lc.mu.Lock()
	defer lc.mu.Unlock()
	if lc.shuttingDown.Load() {
		return ErrShuttingDown
	}
	lc.drainers = append(lc.drainers, d)
	return nil
}

// PrependDrainer adds a drainer to the FRONT of the drain order — so
// it runs BEFORE every previously-registered drainer. Used to encode
// LIFO semantics for app-level stop hooks (last registered = first
// drained).
func (lc *Lifecycle) PrependDrainer(d Drainer) error {
	if lc.shuttingDown.Load() {
		return ErrShuttingDown
	}
	lc.mu.Lock()
	defer lc.mu.Unlock()
	if lc.shuttingDown.Load() {
		return ErrShuttingDown
	}
	lc.drainers = append([]Drainer{d}, lc.drainers...)
	return nil
}

// RegisterHealthChecker adds a component that reports health status.
// During shutdown, all checkers are marked unhealthy. Returns
// ErrShuttingDown if Shutdown has already started.
func (lc *Lifecycle) RegisterHealthChecker(hc HealthChecker) error {
	if lc.shuttingDown.Load() {
		return ErrShuttingDown
	}
	lc.mu.Lock()
	defer lc.mu.Unlock()
	if lc.shuttingDown.Load() {
		return ErrShuttingDown
	}
	lc.checkers = append(lc.checkers, hc)
	return nil
}

// Shutdown executes the graceful shutdown sequence:
//
//  1. Mark all health checkers as unhealthy (load balancers divert traffic).
//  2. Drain all registered drainers concurrently with a timeout.
//  3. Return the first error (if any).
//
// The context is used as the parent for the drain timeout.
func (lc *Lifecycle) Shutdown(ctx context.Context) error {
	// Phase 1: Mark unhealthy. Load balancers polling IsHealthy now see
	// false and divert traffic before in-flight requests finish draining.
	lc.shuttingDown.Store(true)

	// Phase 2: Drain with timeout. Snapshot the timeout and the drainers
	// under the same lock that SetShutdownTimeout writes to, so a
	// concurrent SetShutdownTimeout during Shutdown is not an
	// unsynchronised read/write.
	lc.mu.Lock()
	timeout := lc.timeout
	drainers := make([]Drainer, len(lc.drainers))
	copy(drainers, lc.drainers)
	// Snapshot consumed — clear so a second Shutdown() call is a safe
	// no-op even if callers re-enter (idempotent shutdown contract).
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

	// Sequential, in-order drain. Concurrent draining loses the LIFO
	// ordering app-level stop hooks rely on (last-registered runs
	// first, so a battery's close runs AFTER any user code that
	// depends on it). Callers that want parallel drain can launch
	// goroutines from inside their Drain implementation.
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

// RunWithSignals blocks until SIGINT or SIGTERM is received, then runs
// Shutdown using ctx as the parent context. Returns Shutdown's error,
// or nil if ctx is cancelled before a signal arrives.
func (lc *Lifecycle) RunWithSignals(ctx context.Context) error {
	return lc.RunWithSignalsUsing(ctx, lc.Shutdown)
}

// RunWithSignalsUsing is like RunWithSignals but invokes the supplied
// shutdown function instead of lc.Shutdown directly. Used by App so
// SIGTERM walks the App.Shutdown path (HTTP server drain + battery
// stop + lifecycle.Shutdown), not just lifecycle.Shutdown alone.
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

// IsHealthy returns true if all registered health checkers report healthy
// AND Shutdown has not been called. Returns true if no checkers are
// registered and the lifecycle is not shutting down.
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

// SetShutdownTimeout configures the drain timeout.
func (lc *Lifecycle) SetShutdownTimeout(d time.Duration) {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	lc.timeout = d
}
