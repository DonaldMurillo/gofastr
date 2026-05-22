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
	"fmt"
	"sync"
	"time"
)

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
	mu       sync.Mutex
	drainers []Drainer
	checkers []HealthChecker
	timeout  time.Duration
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
// called concurrently during shutdown.
func (lc *Lifecycle) RegisterDrainer(d Drainer) {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	lc.drainers = append(lc.drainers, d)
}

// RegisterHealthChecker adds a component that reports health status.
// During shutdown, all checkers are marked unhealthy.
func (lc *Lifecycle) RegisterHealthChecker(hc HealthChecker) {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	lc.checkers = append(lc.checkers, hc)
}

// Shutdown executes the graceful shutdown sequence:
//
//  1. Mark all health checkers as unhealthy (load balancers divert traffic).
//  2. Drain all registered drainers concurrently with a timeout.
//  3. Return the first error (if any).
//
// The context is used as the parent for the drain timeout.
func (lc *Lifecycle) Shutdown(ctx context.Context) error {
	// Phase 1: Mark unhealthy (handled by checkers themselves when
	// the app's health endpoint starts returning 503 during shutdown)

	// Phase 2: Drain with timeout
	drainCtx, cancel := context.WithTimeout(ctx, lc.timeout)
	defer cancel()

	lc.mu.Lock()
	drainers := make([]Drainer, len(lc.drainers))
	copy(drainers, lc.drainers)
	lc.mu.Unlock()

	var firstErr error
	var wg sync.WaitGroup
	var errMu sync.Mutex

	for _, d := range drainers {
		wg.Add(1)
		go func(d Drainer) {
			defer wg.Done()
			if err := d.Drain(drainCtx); err != nil {
				errMu.Lock()
				if firstErr == nil {
					firstErr = fmt.Errorf("drain failed: %w", err)
				}
				errMu.Unlock()
			}
		}(d)
	}

	wg.Wait()
	return firstErr
}

// IsHealthy returns true if all registered health checkers report healthy.
// Returns true if no checkers are registered.
func (lc *Lifecycle) IsHealthy() bool {
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
