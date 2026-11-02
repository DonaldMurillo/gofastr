package framework

import (
	"context"
	"net/http"

	"github.com/DonaldMurillo/gofastr/core/featureflag"
	"github.com/DonaldMurillo/gofastr/core/router"
)

// Flags returns the app's feature-flag evaluator, creating one on
// first call. The default backing store is in-memory; for clustered
// deployments call SetFlagStore with a Redis- or DB-backed
// implementation BEFORE any caller invokes Flags, once the lazy
// default fires, subsequent SetFlagStore calls panic to avoid the
// silent race where some goroutines still hold a reference to the
// previous evaluator.
//
// The default middleware puts the evaluator on every request context
// (FlagContextMiddleware), so featureflag.Bool(r.Context(), "...") reads
// this App's flags even with several Apps in one process. It is also
// installed as featureflag.Default() for contexts that never passed
// through a request (a job, a cron run); there the last App wired wins.
func (a *App) Flags() *featureflag.Evaluator {
	a.flagMu.Lock()
	defer a.flagMu.Unlock()
	if a.flagEval == nil {
		a.flagEval = featureflag.NewEvaluator(featureflag.NewMemoryStore())
		featureflag.SetDefault(a.flagEval)
	}
	a.flagAccessed = true
	return a.flagEval
}

// SetFlagStore swaps the underlying store. Must be called before any
// caller has triggered the lazy default; a second SetFlagStore call
// (or any call after Flags() / IsEnabled has already been used)
// panics to avoid the silent race where stale references to the
// previous evaluator persist in handler closures.
//
// Useful for tests that want a preconfigured store, or for production
// wiring that uses a persistent backend, wire it during NewApp setup,
// before any request runs.
func (a *App) SetFlagStore(s featureflag.Store) *App {
	a.flagMu.Lock()
	defer a.flagMu.Unlock()
	if a.flagAccessed {
		panic("framework: SetFlagStore called after the flag evaluator was already used: " +
			"wire your store before any handler / startup hook calls Flags() or IsEnabled, " +
			"otherwise stale evaluator references will linger in goroutines")
	}
	a.flagEval = featureflag.NewEvaluator(s)
	featureflag.SetDefault(a.flagEval)
	a.flagAccessed = true
	return a
}

// IsEnabled is a convenience wrapper that calls Flags().Bool with the
// supplied context. Use it from handler code that doesn't otherwise
// need a direct reference to the evaluator.
//
// The context is expected to already carry an EvalContext (via
// featureflag.WithContext) when user/tenant gating matters; without
// one, the call still works but only the kill-switch (Flag.Enabled)
// and the Tenants list and rollout for the request's resolved tenant
// (handler.GetTenantID), if any, are consulted.
func (a *App) IsEnabled(ctx context.Context, key string) bool {
	return a.Flags().Bool(ctx, key)
}

// FlagContextMiddleware returns middleware that puts the App's flag
// evaluator on every request context (featureflag.WithEvaluator), so
// featureflag.Bool(r.Context(), ...) reads this App's store rather than
// the process-wide default another App may have installed. An App with
// no evaluator yet attaches nil, which reads every flag as off, as its
// empty lazy store would.
//
// The App installs this as part of the default middleware chain. It's
// exposed for apps that opt out (WithoutDefaultMiddleware).
func (a *App) FlagContextMiddleware() router.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			a.flagMu.Lock()
			e := a.flagEval
			a.flagMu.Unlock()
			next.ServeHTTP(w, r.WithContext(featureflag.WithEvaluator(r.Context(), e)))
		})
	}
}
