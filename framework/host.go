package framework

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"time"
)

// Host serves a booted App: the built-in HTTP server behind Start and
// Serve, ServicesHost, or an external library's native, terminal or
// canvas shell. A host owns its transport and how long it runs. The
// lifecycle engine behind App.Run owns every boot and shutdown phase, so
// a host never migrates, seeds, initializes plugins or runs hooks.
//
// A native host coordinates the lifecycle its operating system owns
// rather than running the platform's UI loop: it calls App.Run from the
// OS launch callback on a goroutine of its own, and calls Shutdown, or
// cancels Run's context, when the OS ends the app.
type Host interface {
	// Run serves the App and blocks until it stops serving. The engine
	// calls it at most once, after ServicesReady has closed, with the
	// App's lifecycle context, which Shutdown cancels. (The built-in HTTP
	// host is the one exception: it serves the interactive setup wizard
	// before the deferred phases close ServicesReady. Every other host
	// refuses interactive setup.) Return nil when ctx is done or the host
	// ended on its own; return an error to roll the App back.
	Run(ctx context.Context, s *Session) error
	// Stop releases the host's transport. Shutdown calls it once per
	// admitted run, after cancelling Run's context and before batteries
	// and OnStop hooks stop: while Run serves, after Run has returned, or
	// when a stop during boot means Run is never called. It must release
	// whatever the host already holds in each case. ctx carries the
	// teardown's deadline, and Stop must return once ctx is done: Shutdown
	// and the rest of the teardown wait for Stop, so a Stop that ignores
	// ctx hangs them.
	Stop(ctx context.Context) error
}

// Session is one run of an App under a Host. It is opaque: the built-in
// hosts read the boot's results through it, and external hosts receive
// it so Host can grow without breaking their implementations.
type Session struct {
	app              *App
	setupInteractive bool // the setup wizard serves until its swap starts the deferred phases
	hasAPI           bool // the banner advertises /openapi.json and /api/docs/
	hasLLMMD         bool // the banner advertises /api/llm.md
}

// ErrAlreadyStarted is returned by Run, Start and Serve when the App has
// already been started. An App runs once; build a new App to run again.
var ErrAlreadyStarted = errors.New("framework: App already started")

// ServicesHost returns the built-in services-only Host. It opens no
// transport: no http.Server, listener or HTTP client is constructed. The
// App's entities, services, workers and data run in-process, and a
// presentation calls them directly (MustCrudHandler, the App's DB). Its
// Run returns when the App stops; its Stop has nothing to release. Role
// selection (WithRole) is unchanged by the host.
func ServicesHost() Host { return servicesHost{} }

type servicesHost struct{}

func (servicesHost) Run(ctx context.Context, _ *Session) error {
	<-ctx.Done()
	return nil
}

func (servicesHost) Stop(context.Context) error { return nil }

// ServicesReady returns a channel closed once every boot phase has
// succeeded (migrations, seeds, plugin and battery Init, setup, battery
// and OnStart hooks, the outbox relay, route finalization), just before
// the host starts its transport. With interactive setup it closes once
// the wizard completes and the deferred OnStart hooks succeed. It never
// closes when boot fails or Shutdown begins first. OnReady, which needs
// a bound HTTP listener, still reports transport readiness.
func (a *App) ServicesReady() <-chan struct{} {
	a.serverMu.Lock()
	defer a.serverMu.Unlock()
	if a.servicesReady == nil {
		a.servicesReady = make(chan struct{})
	}
	return a.servicesReady
}

// markServicesReadyLocked closes ServicesReady once. Callers hold
// serverMu and have checked that no Shutdown has begun.
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

// Run boots the App and hands it to host, which owns the transport and
// the running lifetime. Start and Serve are Run with the built-in HTTP
// host; ServicesHost runs the App with no transport at all.
//
// The lifecycle engine owns the boot order: auto-migrate and entity
// seeds, WithSeed hooks, plugin and battery Init (and a second migrate
// for schema they declared), first-run setup, battery and OnStart hooks,
// the outbox relay, then route finalization and BootValidator checks.
// ServicesReady closes after the last of them, then host.Run is called.
// A failure in any phase rolls back through Shutdown, so whatever an
// earlier phase started is stopped, and Run returns the error.
//
// ctx bounds the run: once it is done, Run shuts the App down, with the
// deadline the SIGTERM handler uses (AppConfig.ShutdownTimeout plus the
// readiness grace) and ctx's values, and returns Shutdown's error. Boot
// phases, OnStart hooks and host.Run receive the App's lifecycle context
// instead: it carries ctx's values, and only Shutdown cancels it, so the
// stop order (readiness drain, host, batteries, OnStop hooks) holds
// however the stop begins. Run installs no signal handler; Start does.
//
// An App runs once. Run, Start or Serve during or after an admitted run
// returns ErrAlreadyStarted; one that begins after Shutdown returns nil
// and starts nothing. A host refused either way, or by a configuration
// error reported before boot, is never called.
//
// Shutdown during boot cancels the lifecycle context, lets the running
// phase return, starts no later phase and tears down: host.Run is never
// called, host.Stop is. The teardown runs on the context of the stop
// that began it, so it keeps that Shutdown's deadline and values (for
// Run's own ctx, the SIGTERM deadline above). A Shutdown whose context
// ends before the phase returns stops waiting and returns ctx's error;
// the teardown then still runs once the phase returns, on a fresh
// five-second budget that keeps that context's values. A lifecycle
// callback that calls Shutdown synchronously waits for a teardown that
// cannot start until the callback returns, so it returns at its own
// deadline, and with no deadline (context.Background()) it never
// returns and the run hangs. Return an error instead, or call Shutdown
// on another goroutine.
//
// A stop during boot is not a failure: Run returns nil when the running
// phase returns the stop's own cancellation, context.Canceled, or the
// two forms a database driver reports it in (database/sql's ErrTxDone
// for a transaction it rolled back when the context ended, and SQLite's
// SQLITE_INTERRUPT). A phase that fails for any other reason fails Run
// even while a stop is pending.
//
// When host.Run returns an error, Run rolls back and returns it. When it
// returns nil and no Shutdown is under way, Run shuts the App down and
// returns Shutdown's error; otherwise Run returns nil without waiting
// for the teardown, as Start always has.
//
// A teardown's error is reported once, by whoever began it. A Shutdown
// call returns the error of the teardown it began, during boot
// included; Run joins the error of a teardown it began itself (a
// rollback, a host that ended on its own, ctx ending) to its own. A
// rollback with no Shutdown under way runs on a five-second budget.
//
// OnStart, OnReady, seed and battery hooks keep their recover-to-error
// isolation. A panic the engine does not contain (a BootValidator's, by
// contract, or the host's) tears the App down, then continues with the
// same value.
//
// A host with no HTTP listener cannot honor OnReady hooks, which take the
// listener's address: Run refuses an App that has one, before boot, or as
// a rollback when a plugin or start hook registers one. It also refuses
// interactive first-run setup, whose wizard is served over HTTP; headless
// setup runs. Routes, WrapServer, TLS and the server timeouts stay
// configured and unused.
func (a *App) Run(ctx context.Context, host Host) error {
	if ctx == nil {
		return errors.New("framework: Run: nil context")
	}
	if host == nil {
		return errors.New("framework: Run: nil Host")
	}
	return a.run(ctx, host)
}

// run is the lifecycle engine behind Run, Start and Serve.
func (a *App) run(ctx context.Context, host Host) (err error) {
	if a.oauthResource != nil {
		if err := a.oauthResource.validate(); err != nil {
			return err
		}
	}
	// Opt-in via GOFASTR_SEMANTIC_COVERAGE=1, so an integration test that
	// drives the real binary over HTTP still records what it exercised.
	// No-op in every other process.
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

	// From here every return has torn the App down, or left the teardown
	// to a Shutdown already under way.
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
	// App-level seed hooks (App.WithSeed). Run after auto-migration + the
	// per-entity RunSeeds phase so every table exists, and before plugins
	// init so a plugin that reads seed data sees it. Serialize across
	// replicas behind the SAME advisory lock as RunSeeds ( DISTINCT from
	// the migration lock) so two booting replicas don't race a hook.
	// WithSeed hooks have no ledger. They serialize-per-boot but still
	// run on every replica, so keep them idempotent. SQLite is unwrapped
	// (single-process; the lock pin would fight the pool the hooks use).
	// With no DB the call is unchanged (no coordination to do).
	if err := a.runSeedHooksSerialized(); err != nil {
		return a.abortRun(fmt.Errorf("seed hooks: %w", err))
	}
	if a.stopRequestedNow() {
		return a.abortRun(nil)
	}

	// Initialize plugins and batteries (routes, middleware, tools, hooks).
	// Must happen after auto-migrate (so entity tables exist) but before
	// start hooks (so batteries can reference their routes).
	schemaBefore := a.schemaSize()
	if err := a.InitPlugins(); err != nil {
		return a.abortRun(fmt.Errorf("init plugins: %w", err))
	}
	// An entity, view or routine declared from a battery's or module's
	// Init (the pattern modules.md documents) missed the pass above, so
	// migrate and seed again. Both are idempotent: what the first pass
	// covered diffs to nothing and its seeds are in the ledger.
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

	// `gofastr export` runs the app with GOFASTR_EXPORT set: export
	// every page now (migrations and seeds have run, so data-backed
	// pages render their rows) and return instead of serving.
	if dir := os.Getenv(envExport); dir != "" {
		res, err := a.exportStatic(a.appCtx, dir, os.Getenv(envExportBase))
		if err != nil {
			return a.abortRun(fmt.Errorf("export: %w", err))
		}
		// The receipt goes last: it says the export above is complete.
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
	// Run OnStart hooks (cron/queue workers, custom setup) and the outbox
	// relay. Failure here aborts before the host starts, better than a
	// half-up server. Deferred when interactive setup is active: the
	// hooks may start consumers that touch tables setup owns, so the
	// HTTP host's setup swap starts them once setup completes.
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

	// Boot is over. The check, the end of boot and ServicesReady share
	// one critical section with Shutdown's stop request, so a Shutdown
	// either lands first (and this run tears down without calling the
	// host) or finds boot over and stops the host itself.
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
	// The host ended on its own: stop the App with the deadline a
	// SIGTERM would get.
	sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), a.signalShutdownTimeout())
	defer cancel()
	return a.Shutdown(sctx)
}

// rollbackTimeout bounds a teardown Run begins itself after a failure,
// and the cleanup of a stop during boot whose Shutdown gave up waiting
// before the running phase returned.
const rollbackTimeout = 5 * time.Second

// admit claims the App for one run under serverMu, the lock Shutdown's
// stop request takes, and creates the lifecycle context from ctx's
// values (an earlier caller may already have created it).
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

// stopRequestedLocked reports whether a Shutdown has begun. Callers hold
// serverMu. The second clause is the record a Shutdown leaves of itself
// on the lifecycle context (appCancel cleared beside a live appCtx),
// which a caller that set the context up by hand also produces.
func (a *App) stopRequestedLocked() bool {
	return a.stopRequested || (a.appCtx != nil && (a.appCancel == nil || a.appCtx.Err() != nil))
}

func (a *App) stopRequestedNow() bool {
	a.serverMu.Lock()
	defer a.serverMu.Unlock()
	return a.stopRequestedLocked()
}

// endBoot marks boot over. It reports whether a Shutdown that arrived
// during boot left the teardown to this run, and that Shutdown's
// context, which the teardown runs on.
func (a *App) endBoot() (handoff bool, ctx context.Context) {
	a.serverMu.Lock()
	defer a.serverMu.Unlock()
	a.booting = false
	handoff, a.teardownToEngine = a.teardownToEngine, false
	ctx, a.handoffCtx = a.handoffCtx, nil
	return handoff, ctx
}

// abortRun ends a run that will not (or no longer) serve: it tears the
// App down, or finishes the teardown a Shutdown during boot left to it,
// and returns cause joined with the error of a teardown it began itself.
// The error of a teardown a Shutdown began is that Shutdown's to return.
//
// A stop is not a failure. Run returns nil for a shutdown at any later
// point, and a SIGTERM mid-migration deserves the same answer, so cause
// is dropped when a Shutdown had begun and cause is nothing but the
// stop's cancellation (stopShaped). An independent failure that
// coincides with the stop is still returned.
func (a *App) abortRun(cause error) error {
	handoff, hctx := a.endBoot()
	// Read the stop state BEFORE tearing down: the teardown below records
	// a stop of its own, so checking afterwards could not tell an
	// external Shutdown from this one.
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
		// A Shutdown already under way reports its own teardown; this
		// call only waited for it.
		err = nil
	}
	return joinErrors(cause, err)
}

// finishHandoff runs the teardown a Shutdown during boot claimed, on that
// Shutdown's context, and hands it the result. A context that ended while
// the boot phase still ran (the caller gave up waiting) cannot bound the
// cleanup it was waiting for: the teardown gets rollbackTimeout instead,
// and keeps the context's values.
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

// shutdownWhenDone shuts the App down once ctx is done, with the SIGTERM
// deadline and ctx's values. The returned join waits for that Shutdown,
// if it started, and returns its error.
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
// modernc.org/sqlite, the driver sqlite/stdlib registers, interrupts a
// statement when its context ends and reports a BEGIN it interrupted
// with this code (as an error whose Code method returns it) rather than
// as context.Canceled.
const sqliteInterrupt = 9

// stopShaped reports whether err is nothing but a stop's cancellation
// surfacing from the boot phase it interrupted: every error in its tree
// is context.Canceled (or reports itself as it, as net's dial error
// does), database/sql's ErrTxDone, which a transaction's Commit returns
// once database/sql rolled the transaction back because its context
// ended, or SQLite's interrupt code. A join or multi-%w wrap holding any
// other error is not stop-shaped: an independent failure that coincides
// with the stop is still a failure. Callers check that a stop is under
// way; on its own none of these shapes is graceful.
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

// joinErrors joins two errors without wrapping either one alone, so a
// single error keeps its identity.
func joinErrors(first, second error) error {
	switch {
	case second == nil:
		return first
	case first == nil:
		return second
	}
	return errors.Join(first, second)
}

// stopHost calls the admitted host's Stop under recover-to-error
// isolation: the teardown that follows (batteries, OnStop hooks) must
// still run when a host panics.
func (a *App) stopHost(ctx context.Context, host Host) (err error) {
	defer func() {
		if v := recover(); v != nil {
			err = fmt.Errorf("stopping host: %w", recoveredPanic(a.Logger(), "host stop", fmt.Sprintf("%T", v)))
		}
	}()
	if err := host.Stop(ctx); err != nil {
		if _, web := host.(*httpHost); web {
			return err // already "draining http server: ..."
		}
		return fmt.Errorf("stopping host: %w", err)
	}
	return nil
}

// refuseReadyHooks fails a run whose host binds no HTTP listener while
// OnReady hooks are registered: they take the listener's address and
// would never run, and skipping a registered hook silently is what this
// refuses. ServicesReady is the transport-free readiness signal.
func (a *App) refuseReadyHooks() error {
	if n := len(a.readyHooks); n > 0 {
		return fmt.Errorf("framework: Run: %d OnReady hook(s) need an HTTP listener and this host has none; wait on App.ServicesReady instead", n)
	}
	return nil
}

// startOutboxRelay starts the outbox relay (WithOutbox). Runs on the app
// lifecycle context so Shutdown cancels it; the returned stop func is
// also registered as an OnStop drainer so shutdown blocks until the loop
// has fully exited: no half-delivered batch outlives the process.
// Worker-scoped: a serve-only process stages rows (StageEvent runs in
// the write transaction regardless of role) but never claims them.
// Delivery belongs to the worker/all processes.
func (a *App) startOutboxRelay() {
	if a.outbox == nil || !a.runsWorkers() {
		return
	}
	stopRelay := a.outbox.StartRelay(a.appCtx)
	a.OnStop(func() error {
		stopRelay()
		return nil
	})
}

// startDeferred runs the boot phases interactive setup postponed, the
// OnStart hooks and the outbox relay, then closes ServicesReady. The HTTP
// host's setup swap calls it once, after the wizard completes. A failure
// gets the same fail-loud semantics as the identical failure at normal
// boot (which aborts Start): log and shut down. The process exits, and
// the NEXT boot finds setup complete and runs the hooks on the normal
// path, failing Start properly. Silently running without consumers would
// be a half-up server.
func (a *App) startDeferred() {
	if err := a.runStartHooks(); err != nil {
		if a.stopRequestedNow() && stopShaped(err) {
			return // the hooks stopped because the App is stopping
		}
		a.Logger().Error("setup: deferred start hooks failed; shutting down (restart will boot normally and surface this error at Start)", "error", err)
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_ = a.Shutdown(ctx)
		}()
		return
	}
	a.startOutboxRelay()
	a.serverMu.Lock()
	if !a.stopRequestedLocked() {
		a.markServicesReadyLocked()
	}
	a.serverMu.Unlock()
}
