package framework

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core/featureflag"
)

// Flags returns the app's feature-flag evaluator, creating one on
// first call. The default backing store is in-memory; for clustered
// deployments call SetFlagStore with a Redis- or DB-backed
// implementation BEFORE any caller invokes Flags — once the lazy
// default fires, subsequent SetFlagStore calls panic to avoid the
// silent race where some goroutines still hold a reference to the
// previous evaluator.
//
// The evaluator is also installed as featureflag.Default() so package-
// level featureflag.Bool(ctx, "...") calls work from anywhere in the app.
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
// wiring that uses a persistent backend — wire it during NewApp setup,
// before any request runs.
func (a *App) SetFlagStore(s featureflag.Store) *App {
	a.flagMu.Lock()
	defer a.flagMu.Unlock()
	if a.flagAccessed {
		panic("framework: SetFlagStore called after the flag evaluator was already used — " +
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
// and uniform rollout are consulted.
func (a *App) IsEnabled(ctx context.Context, key string) bool {
	return a.Flags().Bool(ctx, key)
}
