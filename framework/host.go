package framework

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/lifecycle"
)

// Host serves a booted App and owns its transport; App.Run owns every
// boot and shutdown phase.
type Host interface {
	// Run serves the App until ctx, which Shutdown cancels, is done. It is
	// called at most once, after ServicesReady closes. A non-nil error
	// rolls the App back.
	Run(ctx context.Context, s *Session) error
	// Stop releases whatever the host holds, whether or not Run was
	// called, and returns once ctx is done.
	Stop(ctx context.Context) error
}

// Session is one run of an App under a Host. It is opaque.
type Session struct {
	app              *App
	setupInteractive bool
	hasAPI           bool
	hasLLMMD         bool
}

// ErrAlreadyStarted is returned by Run, Start and Serve when the App has
// already been started.
var ErrAlreadyStarted = errors.New("framework: App already started")

// ServicesHost returns a Host that opens no transport: the App's
// entities, workers and data run in-process until the App stops.
func ServicesHost() Host { return servicesHost{} }

type servicesHost struct{}

func (servicesHost) Run(ctx context.Context, _ *Session) error {
	<-ctx.Done()
	return nil
}

func (servicesHost) Stop(context.Context) error { return nil }

// ServicesReady returns a channel closed once every boot phase has
// succeeded, just before the host starts its transport. With interactive
// setup it closes once the wizard's deferred OnStart hooks succeed. It
// never closes when boot fails or Shutdown begins first.
func (a *App) ServicesReady() <-chan struct{} {
	a.serverMu.Lock()
	defer a.serverMu.Unlock()
	if a.servicesReady == nil {
		a.servicesReady = make(chan struct{})
	}
	return a.servicesReady
}

func (a *App) markServicesReadyLocked() {
	if a.servicesReadyClosed {
		return
	}
	if a.servicesReady == nil {
		a.servicesReady = make(chan struct{})
	}
	close(a.servicesReady)
	a.servicesReadyClosed = true
}

// Run boots the App and hands it to host, which serves until the App stops.
// Start and Serve are Run with the built-in HTTP host. A failure in any
// boot phase rolls the App back and Run returns it; a stop during boot
// returns nil. When ctx is done Run shuts the App down and returns
// Shutdown's error. An App runs once: a later Run returns
// ErrAlreadyStarted, or nil after Shutdown. A host with no HTTP
// listener refuses OnReady hooks and interactive setup. A lifecycle
// callback must not call Shutdown synchronously: it waits for a teardown
// that cannot start until the callback returns.
func (a *App) Run(ctx context.Context, host Host) error {
	if ctx == nil {
		return errors.New("framework: Run: nil context")
	}
	if host == nil {
		return errors.New("framework: Run: nil Host")
	}
	return a.run(ctx, host)
}

func (a *App) run(ctx context.Context, host Host) (err error) {
	if a.oauthResource != nil {
		if err := a.oauthResource.validate(); err != nil {
			return err
		}
	}
	a.enableSemanticCoverageFromEnv()

	if err := a.checkQueuedEndpointsClaimed(); err != nil {
		return err
	}
	web, _ := host.(*httpHost)
	if web != nil {
		if err := web.preflight(); err != nil {
			return err
		}
	} else if err := a.refuseReadyHooks(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if admitted, err := a.admit(ctx, host); !admitted {
		return err
	}

	join := a.shutdownWhenDone(ctx)
	defer func() { err = joinErrors(err, join()) }()
	defer func() {
		if v := recover(); v != nil {
			_ = a.abortRun(nil)
			panic(v)
		}
	}()

	a.warnUnresolvableRelations()
	if web != nil {
		web.prepare()
	}

	if err := a.migrateAndSeed(); err != nil {
		return a.abortRun(err)
	}
	if a.stopRequestedNow() {
		return a.abortRun(nil)
	}
	if err := a.runSeedHooksSerialized(); err != nil {
		return a.abortRun(fmt.Errorf("seed hooks: %w", err))
	}
	if a.stopRequestedNow() {
		return a.abortRun(nil)
	}

	schemaBefore := a.schemaSize()
	if err := a.InitPlugins(); err != nil {
		return a.abortRun(fmt.Errorf("init plugins: %w", err))
	}
	if a.schemaSize() != schemaBefore {
		if err := a.migrateAndSeed(); err != nil {
			return a.abortRun(fmt.Errorf("after init plugins: %w", err))
		}
	}
	if web == nil {
		if err := a.refuseReadyHooks(); err != nil {
			return a.abortRun(err)
		}
	}
	if a.stopRequestedNow() {
		return a.abortRun(nil)
	}

	if dir := os.Getenv(envExport); dir != "" {
		res, err := a.exportStatic(a.appCtx, dir, os.Getenv(envExportBase))
		if err != nil {
			return a.abortRun(fmt.Errorf("export: %w", err))
		}
		if err := writeExportReceipt(dir, os.Getenv(envExportRun), len(res.Pages)); err != nil {
			return a.abortRun(fmt.Errorf("export receipt: %w", err))
		}
		a.Logger().Info("static export written", "dir", dir)
		return a.abortRun(nil)
	}

	setupInteractive, err := a.bootSetup(web != nil)
	if err != nil {
		return a.abortRun(err)
	}
	if !setupInteractive {
		if err := a.runStartHooks(); err != nil {
			return a.abortRun(fmt.Errorf("start hooks: %w", err))
		}
		a.startOutboxRelay()
	}
	if a.stopRequestedNow() {
		return a.abortRun(nil)
	}

	hasAPI, hasLLMMD, err := a.finalizeRoutes()
	if err != nil {
		return a.abortRun(err)
	}
	if web == nil {
		if err := a.refuseReadyHooks(); err != nil {
			return a.abortRun(err)
		}
	}

	a.serverMu.Lock()
	if a.stopRequestedLocked() {
		a.serverMu.Unlock()
		return a.abortRun(nil)
	}
	a.booting = false
	if !setupInteractive {
		a.markServicesReadyLocked()
	}
	hostCtx := a.appCtx
	a.serverMu.Unlock()

	s := &Session{app: a, setupInteractive: setupInteractive, hasAPI: hasAPI, hasLLMMD: hasLLMMD}
	if err := host.Run(hostCtx, s); err != nil {
		return a.abortRun(err)
	}
	if a.stopRequestedNow() {
		return nil
	}
	sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), a.signalShutdownTimeout())
	defer cancel()
	return a.Shutdown(sctx)
}

const rollbackTimeout = 5 * time.Second

func (a *App) admit(ctx context.Context, host Host) (bool, error) {
	a.serverMu.Lock()
	defer a.serverMu.Unlock()
	if a.runHost != nil {
		return false, ErrAlreadyStarted
	}
	if a.stopRequestedLocked() {
		return false, nil
	}
	a.runHost = host
	a.booting = true
	if a.appCtx == nil {
		a.appCtx, a.appCancel = context.WithCancel(context.WithoutCancel(ctx))
	}
	return true, nil
}

func (a *App) stopRequestedLocked() bool {
	return a.stopRequested || (a.appCtx != nil && (a.appCancel == nil || a.appCtx.Err() != nil))
}

func (a *App) stopRequestedNow() bool {
	a.serverMu.Lock()
	defer a.serverMu.Unlock()
	return a.stopRequestedLocked()
}

func (a *App) endBoot() (handoff bool, ctx context.Context) {
	a.serverMu.Lock()
	defer a.serverMu.Unlock()
	a.booting = false
	return a.takeHandoffLocked()
}

func (a *App) takeHandoffLocked() (handoff bool, ctx context.Context) {
	if a.booting || a.deferredStarting {
		return false, nil
	}
	handoff, a.teardownToEngine = a.teardownToEngine, false
	ctx, a.handoffCtx = a.handoffCtx, nil
	return handoff, ctx
}

func (a *App) abortRun(cause error) error {
	handoff, hctx := a.endBoot()
	if cause != nil && a.stopRequestedNow() && stopShaped(cause) {
		cause = nil
	}
	if handoff {
		a.finishHandoff(hctx)
		return cause
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(a.appCtx), rollbackTimeout)
	defer cancel()
	began, err := a.stop(ctx)
	if !began {
		err = nil
	}
	return joinErrors(cause, err)
}

func (a *App) finishHandoff(ctx context.Context) {
	a.shutdownMu.Lock()
	done := a.shutdownDone
	a.shutdownMu.Unlock()
	defer close(done)
	callerContext := ctx
	if ctx.Err() != nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.WithoutCancel(ctx), rollbackTimeout)
		defer cancel()
	}
	a.handoffErr = a.shutdown(ctx)
	if a.handoffErr != nil && callerContext.Err() != nil {
		a.Logger().Error("shutdown: teardown failed after the initiating caller's context ended", "error", a.handoffErr)
	}
}

func (a *App) shutdownWhenDone(ctx context.Context) (join func() error) {
	if ctx.Done() == nil {
		return func() error { return nil }
	}
	fired := make(chan struct{})
	var err error
	stop := context.AfterFunc(ctx, func() {
		defer close(fired)
		sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), a.signalShutdownTimeout())
		defer cancel()
		err = a.Shutdown(sctx)
	})
	return func() error {
		if stop() {
			return nil
		}
		<-fired
		return err
	}
}

// sqliteInterrupt is SQLite's SQLITE_INTERRUPT result code.
const sqliteInterrupt = 9

func stopShaped(err error) bool {
	if err == nil {
		return false
	}
	if multi, ok := err.(interface{ Unwrap() []error }); ok {
		errs := multi.Unwrap()
		for _, e := range errs {
			if !stopShaped(e) {
				return false
			}
		}
		return len(errs) > 0
	}
	if err == context.Canceled || err == sql.ErrTxDone {
		return true
	}
	if is, ok := err.(interface{ Is(error) bool }); ok && is.Is(context.Canceled) {
		return true
	}
	if coded, ok := err.(interface{ Code() int }); ok && coded.Code() == sqliteInterrupt {
		return true
	}
	return stopShaped(errors.Unwrap(err))
}

func joinErrors(first, second error) error {
	switch {
	case second == nil:
		return first
	case first == nil:
		return second
	}
	return errors.Join(first, second)
}

func (a *App) stopHost(ctx context.Context, host Host) (err error) {
	defer func() {
		if v := recover(); v != nil {
			err = fmt.Errorf("stopping host: %w", recoveredPanic(a.Logger(), "host stop", fmt.Sprintf("%T", v)))
		}
	}()
	if err := host.Stop(ctx); err != nil {
		if _, web := host.(*httpHost); web {
			return err
		}
		return fmt.Errorf("stopping host: %w", err)
	}
	return nil
}

func (a *App) refuseReadyHooks() error {
	if n := len(a.readyHooks); n > 0 {
		return fmt.Errorf("framework: Run: %d OnReady hook(s) need an HTTP listener and this host has none; wait on App.ServicesReady instead", n)
	}
	return nil
}

func (a *App) startOutboxRelay() {
	if a.outbox == nil || !a.runsWorkers() {
		return
	}
	stopRelay := a.outbox.StartRelay(a.appCtx)
	a.lc.PrependDrainer(lifecycle.DrainFunc(func(ctx context.Context) error {
		return a.drainWithDeadline(ctx, "outbox relay stop", func() error {
			stopRelay()
			return nil
		}, "outbox relay stop abandoned after the drain deadline")
	}))
}

func (a *App) startDeferred() {
	a.serverMu.Lock()
	if a.stopRequestedLocked() {
		a.serverMu.Unlock()
		return
	}
	a.deferredStarting = true
	a.serverMu.Unlock()

	err := errDeferredStartPanicked
	defer func() { a.endDeferred(err) }()
	startErr := a.runStartHooks()
	if startErr == nil {
		a.startOutboxRelay()
	}
	err = startErr
}

var errDeferredStartPanicked = errors.New("deferred start panicked")

func (a *App) endDeferred(err error) {
	a.serverMu.Lock()
	a.deferredStarting = false
	stopping := a.stopRequestedLocked()
	if err == nil && !stopping {
		a.markServicesReadyLocked()
	}
	handoff, hctx := a.takeHandoffLocked()
	a.serverMu.Unlock()

	failed := err != nil && !(stopping && stopShaped(err))
	if failed {
		a.Logger().Error("setup: deferred start hooks failed; shutting down (restart will boot normally and surface this error at Start)", "error", err)
	}
	if handoff {
		go a.finishHandoff(hctx)
		return
	}
	if failed {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_ = a.Shutdown(ctx)
		}()
	}
}
