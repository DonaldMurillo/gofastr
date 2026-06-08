package framework

import "context"

// WithSeed registers a seed function to run during App.Start AFTER
// auto-migration completes (so every entity table exists) and BEFORE the
// HTTP server accepts traffic. This is the documented fix for the common
// "no such table" footgun: calling db.Exec("INSERT …") from main() before
// Start() runs migrations fails, because the table isn't there yet.
//
// Seed funcs run in registration order. The first to return a non-nil
// error aborts Start (the partial-startup teardown drains anything an
// earlier phase spawned). The context is the app's lifecycle context, so
// a long-running seed respects shutdown.
//
// WithSeed composes with the per-entity EntityConfig.Seed callback: entity
// seeds (idempotent, ledger-tracked via _gofastr_seeded) run first as part
// of auto-migration's RunSeeds phase; WithSeed funcs run immediately after,
// in the order registered. Use WithSeed for cross-entity or app-level seed
// logic that doesn't belong to a single entity.
//
//	site := framework.NewApp(framework.WithDB(db))
//	site.Entity("foods", foodsConfig)
//	site.WithSeed(seedFoods) // runs after the foods table is migrated
//
// Returns the App for fluent chaining.
func (a *App) WithSeed(fn func(ctx context.Context) error) *App {
	if fn == nil {
		return a
	}
	a.seedHooks = append(a.seedHooks, fn)
	return a
}

// runSeedHooks fires every WithSeed func in registration order using the
// app's lifecycle context. Called by Start after auto-migration + the
// per-entity RunSeeds phase, before plugins/batteries init. Returns the
// first error so Start aborts before binding the port.
func (a *App) runSeedHooks() error {
	a.ensureLifecycleContext()
	for _, fn := range a.seedHooks {
		if err := fn(a.appCtx); err != nil {
			return err
		}
	}
	return nil
}
