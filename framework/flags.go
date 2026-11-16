package framework

import (
	"context"
	"net/http"

	"github.com/DonaldMurillo/gofastr/core/featureflag"
	"github.com/DonaldMurillo/gofastr/core/router"
)

// Flags returns the app's feature-flag evaluator, creating an in-memory one
// on first call and installing it as featureflag.Default.
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

// SetFlagStore backs the evaluator with s. Panics once Flags, IsEnabled or an
// earlier SetFlagStore has run.
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

// IsEnabled is Flags().Bool(ctx, key).
func (a *App) IsEnabled(ctx context.Context, key string) bool {
	return a.Flags().Bool(ctx, key)
}

// FlagContextMiddleware puts the App's flag evaluator on each request context,
// so featureflag.Bool reads this App's flags. It is in DefaultMiddleware.
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
