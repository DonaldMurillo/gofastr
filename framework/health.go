package framework

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// ReadinessCheck is a named probe run by GET /readyz. Implementations
// must be fast (sub-second), idempotent, and safe to invoke concurrently.
// Return a non-nil error to mark the app "not ready" — clients (load
// balancers, k8s, Fly health checks) treat a 503 from /readyz as a
// signal to stop routing traffic.
type ReadinessCheck struct {
	Name  string
	Check func(ctx context.Context) error
}

// ReadinessRegistrar is the optional interface plugins and batteries
// implement to contribute checks. The framework's plugin/battery
// machinery probes for it during InitPlugins and calls
// RegisterReadinessChecks before health endpoints mount.
//
// The method is deliberately named RegisterReadinessChecks (plural) to
// avoid colliding with App.RegisterReadiness(name, fn) — the two are
// different surfaces, and embedding *App into a battery would otherwise
// produce an ambiguous selector.
type ReadinessRegistrar interface {
	RegisterReadinessChecks(app *App)
}

// healthState owns the registered readiness checks and tuning knobs.
type healthState struct {
	mu       sync.RWMutex
	checks   []ReadinessCheck
	timeout  time.Duration
	verbose  bool // when true, /readyz includes error.Error() strings
}

// defaultReadinessTimeout bounds how long /readyz will wait for the
// slowest check before reporting partial results.
const defaultReadinessTimeout = 5 * time.Second

// WithReadinessTimeout overrides the per-request /readyz deadline.
// Default 5 seconds. Checks that don't return within the deadline are
// reported as errors with status "timeout".
func WithReadinessTimeout(d time.Duration) AppOption {
	return func(a *App) {
		if a.health == nil {
			a.health = &healthState{}
		}
		a.health.timeout = d
	}
}

// WithVerboseReadiness opts in to including each check's error.Error()
// string in the /readyz JSON response. Default is to omit error text
// because /readyz is typically reachable without authentication and
// raw error strings frequently leak internal IPs, connection strings,
// or other infrastructure detail.
//
// Turn this on only when the probes are scoped to a trusted listener
// or behind auth.
func WithVerboseReadiness() AppOption {
	return func(a *App) {
		if a.health == nil {
			a.health = &healthState{}
		}
		a.health.verbose = true
	}
}

// RegisterReadiness adds a readiness check. Names are not required to be
// unique — duplicates surface in the /readyz response as repeated rows,
// which is occasionally useful (the same backend probed at two layers).
//
// Pass a timeout on the context if your check could hang; /readyz also
// applies an overall deadline, but per-check timeouts give better
// signal in the response.
func (a *App) RegisterReadiness(name string, check func(ctx context.Context) error) *App {
	if a.health == nil {
		a.health = &healthState{}
	}
	a.health.mu.Lock()
	a.health.checks = append(a.health.checks, ReadinessCheck{Name: name, Check: check})
	a.health.mu.Unlock()
	return a
}

// readinessChecks returns a snapshot of the registered checks. Safe to
// call concurrently with new registrations.
func (a *App) readinessChecks() []ReadinessCheck {
	if a.health == nil {
		return nil
	}
	a.health.mu.RLock()
	defer a.health.mu.RUnlock()
	cp := make([]ReadinessCheck, len(a.health.checks))
	copy(cp, a.health.checks)
	return cp
}

// readinessTimeout returns the configured per-request deadline.
func (a *App) readinessTimeout() time.Duration {
	if a.health == nil || a.health.timeout <= 0 {
		return defaultReadinessTimeout
	}
	return a.health.timeout
}

// readinessVerbose reports whether the verbose error-text opt-in is on.
func (a *App) readinessVerbose() bool {
	return a.health != nil && a.health.verbose
}

// probeReadinessRegistrars invokes RegisterReadinessChecks on every
// plugin and battery that satisfies the optional interface. Called from
// InitPlugins after lightweight plugins and batteries have initialised
// so they can publish their own checks before /readyz mounts.
func (a *App) probeReadinessRegistrars() {
	if a.Plugins != nil {
		for _, p := range a.Plugins.All() {
			if r, ok := p.(ReadinessRegistrar); ok {
				r.RegisterReadinessChecks(a)
			}
		}
	}
	if a.Batteries != nil {
		for _, b := range a.Batteries.All() {
			if r, ok := b.(ReadinessRegistrar); ok {
				r.RegisterReadinessChecks(a)
			}
		}
	}
}

// registerHealthEndpoints mounts /healthz (liveness) and /readyz
// (readiness). Called during App.Start after plugins/batteries have had
// a chance to register their own checks.
func (a *App) registerHealthEndpoints() {
	a.Router.Get("/healthz", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))

	a.Router.Get("/readyz", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), a.readinessTimeout())
		defer cancel()
		results := runReadinessChecks(ctx, a.readinessChecks(), a.readinessVerbose())

		status := http.StatusOK
		for _, c := range results.Checks {
			if c.Status != "ok" {
				status = http.StatusServiceUnavailable
				break
			}
		}
		results.Status = "ready"
		if status != http.StatusOK {
			results.Status = "not_ready"
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(results)
	}))
}

// ReadinessResponse is the JSON shape returned by /readyz.
type ReadinessResponse struct {
	Status string            `json:"status"`
	Checks []ReadinessResult `json:"checks"`
}

// ReadinessResult is one row in the /readyz response.
type ReadinessResult struct {
	Name     string `json:"name"`
	Status   string `json:"status"` // "ok", "error", or "timeout"
	Error    string `json:"error,omitempty"`
	DurMS    int64  `json:"durationMs"`
}

// runReadinessChecks invokes every check in parallel with the supplied
// context. Each goroutine recovers from panics (a bad check should not
// crash the process) and respects ctx cancellation so a check that
// hangs past the deadline is reported as "timeout" instead of blocking
// the response.
func runReadinessChecks(ctx context.Context, checks []ReadinessCheck, verbose bool) ReadinessResponse {
	if len(checks) == 0 {
		return ReadinessResponse{Status: "ready", Checks: []ReadinessResult{}}
	}
	out := make([]ReadinessResult, len(checks))
	resultCh := make(chan struct{}, len(checks))
	for i, c := range checks {
		go func(i int, c ReadinessCheck) {
			defer func() {
				if r := recover(); r != nil {
					out[i] = ReadinessResult{
						Name:   c.Name,
						Status: "error",
						Error:  redactError(fmt.Sprintf("panic: %v", r), verbose),
						DurMS:  0,
					}
				}
				resultCh <- struct{}{}
			}()
			start := time.Now()
			res := ReadinessResult{Name: c.Name, DurMS: 0}
			if c.Check == nil {
				res.Status = "error"
				res.Error = redactError("nil check function", verbose)
			} else {
				err := c.Check(ctx)
				res.DurMS = time.Since(start).Milliseconds()
				if err != nil {
					res.Status = "error"
					res.Error = redactError(err.Error(), verbose)
				} else {
					res.Status = "ok"
				}
			}
			out[i] = res
		}(i, c)
	}
	// Race the wait against the deadline so a check that ignores ctx
	// can't keep /readyz waiting longer than the request timeout.
	completed := 0
	for completed < len(checks) {
		select {
		case <-resultCh:
			completed++
		case <-ctx.Done():
			// Fill in any unreported rows as timeout. Goroutines that
			// haven't returned will overwrite their slot when they
			// finally do — but the response is already on the wire.
			for i, c := range checks {
				if out[i].Name == "" {
					out[i] = ReadinessResult{
						Name:   c.Name,
						Status: "timeout",
						Error:  redactError("deadline exceeded", verbose),
						DurMS:  0,
					}
				}
			}
			return ReadinessResponse{Checks: out}
		}
	}
	return ReadinessResponse{Checks: out}
}

// redactError returns a fixed string when verbose mode is off so
// /readyz cannot leak internal error text (IPs, DSNs, etc.) to an
// unauthenticated probe. When verbose is on, the original message is
// passed through for operator-side debugging.
func redactError(msg string, verbose bool) string {
	if verbose {
		return msg
	}
	return "check failed"
}

// dbReadinessCheck is the default check registered when App has a DB.
// Uses PingContext so a configured connection timeout is honoured.
func dbReadinessCheck(a *App) ReadinessCheck {
	return ReadinessCheck{
		Name: "db",
		Check: func(ctx context.Context) error {
			// Caller already gated on a.DB != nil at registration time;
			// no defensive nil-check here so a stale DB pointer surfaces
			// as a real failure instead of a silent "ok".
			return a.DB.PingContext(ctx)
		},
	}
}
