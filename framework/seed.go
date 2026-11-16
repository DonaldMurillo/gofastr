package framework

import (
	"context"
	"fmt"
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
		if err := a.runSeedHookSafe(fn); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) runSeedHookSafe(fn func(ctx context.Context) error) (err error) {
	defer func() {
		if v := recover(); v != nil {
			err = recoveredPanic(a.Logger(), "seed hook", fmt.Sprintf("%T", v))
		}
	}()
	return fn(a.appCtx)
}
