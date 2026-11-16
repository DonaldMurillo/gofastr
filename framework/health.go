package framework

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"github.com/DonaldMurillo/gofastr/framework/dev"
	"github.com/DonaldMurillo/gofastr/framework/localrun"
)

// ReadinessCheck is a named probe GET /readyz runs. Check must be fast,
// idempotent and safe to call concurrently; an error answers 503.
type ReadinessCheck struct {
	Name  string
	Check func(ctx context.Context) error
}

// ReadinessRegistrar is implemented by a plugin or battery that adds
// readiness checks; InitPlugins calls it.
type ReadinessRegistrar interface {
	RegisterReadinessChecks(app *App)
}

type healthState struct {
	mu      sync.RWMutex
	checks  []ReadinessCheck
	timeout time.Duration
	verbose bool
	drain   *time.Duration
}

const productionReadinessDrain = 5 * time.Second

// WithReadinessDrain sets how long Shutdown keeps serving after /readyz
// starts answering 503, before the listener closes; /healthz stays 200.
// The default is 5s, and 0 under test, `gofastr dev`, a battery/service
// run, a loopback bind or an in-process listener. The grace takes at most
// half of what Shutdown's context has left. A negative d is zero.
func WithReadinessDrain(d time.Duration) AppOption {
	d = max(d, 0)
	return func(a *App) {
		if a.health == nil {
			a.health = &healthState{}
		}
		a.health.drain = &d
	}
}

func (a *App) readinessDrain() time.Duration {
	if a.health != nil && a.health.drain != nil {
		return *a.health.drain
	}
	return defaultReadinessDrain(localrun.UnderTest(), dev.Enabled(), a.servesLocalOnly())
}

func defaultReadinessDrain(underTest, devMode, local bool) time.Duration {
	if underTest || devMode || local {
		return 0
	}
	return productionReadinessDrain
}

func (a *App) servesLocalOnly() bool {
	a.serverMu.Lock()
	host, inProcess := a.boundHost, a.boundInProcess
	a.serverMu.Unlock()
	return inProcess || a.isLocalRun(host)
}

func capReadinessDrain(ctx context.Context, grace time.Duration) time.Duration {
	dl, ok := ctx.Deadline()
	if !ok {
		return grace
	}
	return max(min(grace, time.Until(dl)/2), 0)
}

func (a *App) drainReadiness(ctx context.Context) {
	a.draining.Store(true)
	a.serverMu.Lock()
	serving := a.server != nil
	a.serverMu.Unlock()
	configured := a.readinessDrain()
	grace := capReadinessDrain(ctx, configured)
	if !serving || grace <= 0 {
		return
	}
	a.Logger().Info("readiness draining: /readyz answers 503; serving until the grace ends",
		"grace", grace.String(), "configured", configured.String())
	t := time.NewTimer(grace)
	defer t.Stop()
	select {
	case <-t.C:
	case <-ctx.Done():
	}
}

func (a *App) signalShutdownTimeout() time.Duration {
	timeout := a.Config.ShutdownTimeout
	if timeout <= 0 {
		timeout = defaultShutdownTimeout
	}
	return timeout + a.readinessDrain()
}

const defaultReadinessTimeout = 5 * time.Second

// WithReadinessTimeout sets the /readyz deadline, 5s by default. A check
// still running at it reports status "timeout".
func WithReadinessTimeout(d time.Duration) AppOption {
	return func(a *App) {
		if a.health == nil {
			a.health = &healthState{}
		}
		a.health.timeout = d
	}
}

// WithVerboseReadiness includes each failed check's error text in the
// /readyz response, which is otherwise "check failed". Use it only where
// /readyz is not public.
func WithVerboseReadiness() AppOption {
	return func(a *App) {
		if a.health == nil {
			a.health = &healthState{}
		}
		a.health.verbose = true
	}
}

// RegisterReadiness adds a readiness check. Names need not be unique.
func (a *App) RegisterReadiness(name string, check func(ctx context.Context) error) *App {
	if a.health == nil {
		a.health = &healthState{}
	}
	a.health.mu.Lock()
	a.health.checks = append(a.health.checks, ReadinessCheck{Name: name, Check: check})
	a.health.mu.Unlock()
	return a
}

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

func (a *App) readinessTimeout() time.Duration {
	if a.health == nil || a.health.timeout <= 0 {
		return defaultReadinessTimeout
	}
	return a.health.timeout
}

func (a *App) readinessVerbose() bool {
	return a.health != nil && a.health.verbose
}

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

func (a *App) healthHandlers() (liveness, readiness http.HandlerFunc) {
	liveness = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}
	readiness = func(w http.ResponseWriter, r *http.Request) {
		if a.draining.Load() {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(ReadinessResponse{Status: "draining", Checks: []ReadinessResult{}})
			return
		}
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
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(results)
	}
	return liveness, readiness
}

func (a *App) registerHealthEndpoints() {
	liveness, readiness := a.healthHandlers()
	a.router.Get("/healthz", liveness)
	a.router.Get("/readyz", readiness)
}

// ReadinessResponse is the JSON shape returned by /readyz.
type ReadinessResponse struct {
	Status string            `json:"status"`
	Checks []ReadinessResult `json:"checks"`
}

// ReadinessResult is one row in the /readyz response.
type ReadinessResult struct {
	Name   string `json:"name"`
	Status string `json:"status"` // ok, error or timeout
	Error  string `json:"error,omitempty"`
	DurMS  int64  `json:"durationMs"`
}

func runReadinessChecks(ctx context.Context, checks []ReadinessCheck, verbose bool) ReadinessResponse {
	if len(checks) == 0 {
		return ReadinessResponse{Status: "ready", Checks: []ReadinessResult{}}
	}
	type indexed struct {
		i   int
		res ReadinessResult
	}
	out := make([]ReadinessResult, len(checks))
	done := make([]bool, len(checks))
	resultCh := make(chan indexed, len(checks))
	for i, c := range checks {
		go func(i int, c ReadinessCheck) {
			res := ReadinessResult{Name: c.Name, DurMS: 0}
			defer func() {
				if r := recover(); r != nil {
					res = ReadinessResult{
						Name:   c.Name,
						Status: "error",
						Error:  redactError(fmt.Sprintf("panic: %v", textsafe.Recovered(r)), verbose),
					}
				}
				resultCh <- indexed{i: i, res: res}
			}()
			start := time.Now()
			if c.Check == nil {
				res.Status = "error"
				res.Error = redactError("nil check function", verbose)
				return
			}
			err := c.Check(ctx)
			res.DurMS = time.Since(start).Milliseconds()
			if err != nil {
				res.Status = "error"
				res.Error = redactError(err.Error(), verbose)
			} else {
				res.Status = "ok"
			}
		}(i, c)
	}
	for completed := 0; completed < len(checks); completed++ {
		select {
		case r := <-resultCh:
			out[r.i] = r.res
			done[r.i] = true
		case <-ctx.Done():
			for i, c := range checks {
				if !done[i] {
					out[i] = ReadinessResult{
						Name:   c.Name,
						Status: "timeout",
						Error:  redactError("deadline exceeded", verbose),
					}
				}
			}
			return ReadinessResponse{Checks: out}
		}
	}
	return ReadinessResponse{Checks: out}
}

// RunReadinessChecks runs the checks /readyz runs and returns its body.
func (a *App) RunReadinessChecks(ctx context.Context) ReadinessResponse {
	return runReadinessChecks(ctx, a.readinessChecks(), a.readinessVerbose())
}

func redactError(msg string, verbose bool) string {
	if verbose {
		return msg
	}
	return "check failed"
}

func dbReadinessCheck(a *App) ReadinessCheck {
	return ReadinessCheck{
		Name: "db",
		Check: func(ctx context.Context) error {
			return a.DB.PingContext(ctx)
		},
	}
}
