package framework

import (
	"context"
)

// WithSeed registers fn to run at Start after auto-migration and entity
// seeds, before plugins init, in registration order, on the lifecycle
// context. The first error, or panic, aborts Start. A nil fn is ignored.
func (a *App) WithSeed(fn func(ctx context.Context) error) *App {
	if fn == nil {
		return a
	}
	a.seedHooks = append(a.seedHooks, fn)
	return a
}

func (a *App) runSeedHooks() error {
	a.ensureLifecycleContext()
	for _, fn := range a.seedHooks {
		if err := a.runHookSafe("seed hook", fn); err != nil {
			return err
		}
	}
	return nil
}
