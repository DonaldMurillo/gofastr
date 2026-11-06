# Application hosts: who serves the App

An App boots the same way whatever serves it. `App.Run(ctx, host)` runs
the lifecycle engine (migrations, seeds, plugin and battery `Init`,
setup, `OnStart` hooks, route finalization), then hands the booted App
to a `framework.Host`. The host owns the transport and decides how long
the App runs. `Start` and `Serve` are `Run` with the built-in HTTP
host, so all three share one boot order, one rollback and one
`Shutdown`.

| Call | Host | Transport |
|---|---|---|
| `app.Start(addr)` | built-in HTTP | binds `addr`, installs the SIGINT/SIGTERM handler, prints the banner, fires `OnReady` |
| `app.Serve(ln)` | built-in HTTP | serves a listener the caller made, such as an in-process listener for a WebView; closes `ln` on every return |
| `app.Run(ctx, framework.ServicesHost())` | services only | none: no `http.Server`, listener or HTTP client exists |
| `app.Run(ctx, yourHost)` | yours | whatever your host owns: a native window, a terminal, a canvas |

An App runs once, under one host. There is no host registry and no way
to attach a second host to a running App.

## Run the App with no transport

`ServicesHost` is for a presentation that calls the App in-process: a
native UI, a terminal shell, a test. The entities, services, workers
and database run exactly as they do under `Start`. Nothing listens.

The example extends the one-person setup in [Local apps](local-apps.md):
the data directory supplies the database, secret and owner identity, and
`registerNotes` stands for the app's own entity registrations (here an
owner-scoped `notes` entity, `Scope.OwnerField` set).

<!-- gofastr:compile
var dataDir string
func registerNotes(*framework.App) {}
-->
```go
import (
	"context"
	"log"
	"os"
	"os/signal"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/localapp"
)

data, err := localapp.Open(dataDir)
if err != nil {
	log.Fatal(err)
}
// The host owns identity wiring, as an HTTP host does. Without the
// extractor every owner-scoped call fails with "owner context required".
if localapp.Install() {
	log.Fatal("battery/auth owns identity in this binary; do not hand out the local owner")
}
app := framework.NewApp(
	framework.WithDB(data.DB),
	framework.WithSecret(data.Secret),
)
app.OnStop(data.Close)
registerNotes(app)

ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
defer stop()

done := make(chan error, 1)
go func() { done <- app.Run(ctx, framework.ServicesHost()) }()

select {
case <-app.ServicesReady():
case err := <-done:
	if err != nil {
		log.Fatal(err) // boot failed and was rolled back
	}
	return // stopped during boot, before the App was ready
}

notes := app.MustCrudHandler("notes")
owner := handler.SetUser(context.Background(), data.User)
if _, err := notes.CreateOne(owner, map[string]any{"title": "first"}); err != nil {
	log.Print(err)
}

if err := <-done; err != nil { // returns after ctx ends and Shutdown runs
	log.Print(err)
}
```

Owner scoping, RBAC and hooks apply to in-process calls the same way
they do to HTTP requests, so a services-only app still installs an
owner extractor and puts a user in the context. A call with no user in
it is refused on an owner-scoped entity, and another identity sees none
of the owner's rows.

To call through the REST handlers instead, as a generated SDK's Local
repositories do, pass `app.InProcessHandler()`. It runs a request
through the router and its middleware with no listener, takes identity
from the request's context, and refuses a request carrying cookies or
other credentials ([SDK](sdk.md#typed-repositories)). Like
`App.Router()`, it skips the setup wizard, `WrapServer` and locale
routing. It is a way to call the App, not a transport to serve.

`WithRole` still picks what runs: `RoleServe` skips the workers and
`RoleWorker` runs them. Under `ServicesHost` no role serves HTTP, the
worker role's health endpoints included.

## Readiness: ServicesReady and OnReady

`App.ServicesReady()` returns a channel that closes once every boot
phase has succeeded, just before the host starts its transport. It
never closes when boot fails or `Shutdown` begins first. With
interactive first-run setup it closes after the wizard completes and
the deferred `OnStart` hooks succeed.

`OnReady` keeps its meaning: the HTTP listener is bound, and the hook
receives its address. A host with no HTTP listener cannot honor that,
so `Run` refuses an App that registered an `OnReady` hook. The refusal
comes before boot when the hook is already registered, and as a
rollback when a plugin or an `OnStart` hook registers one. Wait on
`ServicesReady` instead.

The same refusal covers interactive first-run setup, whose wizard is
served over HTTP. Headless setup (every required value supplied) runs.
Routes, `WrapServer`, TLS and the server timeouts stay configured and
unused; they are not refused.

## The lifecycle rules

These hold for every host, the built-in ones included.

- **Context.** `ctx` bounds the run. When it is done, `Run` shuts the
  App down with the deadline the signal handler uses
  (`ShutdownTimeout` plus the readiness grace) and `ctx`'s values, and
  returns `Shutdown`'s error. Boot phases, `OnStart` hooks and `host.Run` get
  the App's lifecycle context instead: it carries `ctx`'s values and
  only `Shutdown` cancels it, so the stop order is the same however the
  stop begins. `Run` installs no signal handler; `Start` does.
- **One run.** A second `Run`, `Start` or `Serve`, during the run or
  after it, returns `framework.ErrAlreadyStarted`. A call that begins
  after `Shutdown` returns nil and starts nothing. A host refused this
  way, or by a configuration error found before boot, is never called.
- **Shutdown during boot.** It cancels the lifecycle context, lets the
  running phase return, starts no later phase and tears down.
  `host.Run` is never called and `host.Stop` is. The teardown runs on
  the context of the stop that began it, so `host.Stop`, the batteries
  and the `OnStop` hooks get that `Shutdown`'s deadline and values, or
  the signal deadline when `Run`'s `ctx` began it. `Shutdown` waits for
  the teardown and returns its error. If its `ctx` ends before the
  running phase returns, `Shutdown` returns `ctx`'s error, and the
  teardown still runs once the phase returns, on a fresh five-second
  budget that keeps `ctx`'s values.
- **What a stop during boot returns.** `Run` returns nil when the
  running phase returns the stop's own cancellation: `context.Canceled`,
  `sql.ErrTxDone` for a transaction rolled back when the context ended,
  or SQLite's `SQLITE_INTERRUPT`. A phase that fails for any other
  reason, such as a plugin's `Init` error or an `OnStart` hook that
  ignores its context and returns its own error, fails `Run` even while
  a stop is pending.
- **Shutdown from a boot callback.** A seed, `OnStart` hook or
  `BootValidator` that calls `Shutdown` synchronously waits for a
  teardown that cannot start until the callback returns. It returns at
  its `ctx`'s deadline. With no deadline, as with
  `Shutdown(context.Background())`, it never returns and the run hangs.
  Return an error instead, or call `Shutdown` on another goroutine.
- **Host error or early return.** An error from `host.Run` rolls the App
  back, and `Run` returns that error. A nil return with no `Shutdown`
  under way (the user closed the window) shuts the App down, and `Run`
  returns `Shutdown`'s error. A nil return after `Shutdown` began makes
  `Run` return nil without waiting, as `Start` always has.
- **Teardown errors.** Whoever began a teardown reports its error, once.
  A `Shutdown` call returns the error of the teardown it began, during
  boot included. `Run` joins the error of a teardown it began itself (a
  rollback, a host that returned on its own, `ctx` ending) to its own,
  so a boot error and a failing `host.Stop` both reach the caller
  (`errors.Is` matches each). A rollback with no `Shutdown` under way
  runs on a five-second budget. When a `Shutdown` during boot stops
  waiting because its `ctx` ended, the teardown it began has no caller
  left to return to, so a failure of that teardown is logged. The
  `OnStart` hooks interactive setup defers follow the same split as a
  stop during boot: one the stop cancelled is silent, and any other
  failure is logged and shuts the App down. A stop already pending does
  not silence it.
- **Panics.** `OnStart`, `OnReady`, seed and battery hooks keep their
  recover-to-error isolation. A panic the engine does not contain (the
  host's, or a `BootValidator`'s) tears the App down, then continues
  with the same value.
- **Shutdown.** The teardown runs once. A later or concurrent call
  waits for it, then returns nil, or `ctx`'s error if its own `ctx`
  ends first. The stop order is the readiness drain, A2A runs,
  `host.Stop`, process modules, batteries, then `OnStop` hooks, all
  bounded by `ctx`.

## Write a host

A host implements two methods:

```go
type Host interface {
	Run(ctx context.Context, s *framework.Session) error
	Stop(ctx context.Context) error
}
```

`Run` serves the booted App and blocks until it stops serving. It is
called at most once, after `ServicesReady` has closed, with the
lifecycle context. (The built-in HTTP host serves the interactive setup
wizard before `ServicesReady` closes; every other host refuses
interactive setup, so for yours the order always holds.) `Stop` releases
the transport. `Shutdown` calls it once per admitted run: while `Run`
serves, after `Run` returned, or when a stop during boot means `Run`
never ran. `Stop` must return once its `ctx` is done: `Shutdown` and the
rest of the teardown wait for it, so a `Stop` that ignores `ctx` can
hang `Shutdown` past its deadline. An error or a panic from `Stop` does
not skip the teardown: the batteries and `OnStop` hooks still run, and
`Shutdown` returns the failure as `stopping host: ...`. `Session` is
opaque; the built-in hosts read the boot's results through it, and it
lets `Host` grow without breaking yours.

A host that wants GoFastr's HTTP surface uses `Serve` with a listener it
made, such as an in-process listener a WebView reaches. The handler
`Serve` runs stacks the setup wizard, the `WrapServer` wrappers, locale
routing and the stream end that `Shutdown` triggers on top of the
router; a host that serves `App.Router()` itself gets none of them, and
`Session` does not hand out the composed handler.

A native host coordinates the lifecycle the operating system owns. It
does not run the platform's UI loop inside `Run`. It calls `App.Run`
from the OS launch callback on a goroutine of its own, and calls
`Shutdown`, or cancels `Run`'s context, when the OS ends the app.

`Stop` can race the start of `Run`: a `Shutdown` that lands after boot
finds the host before it has opened anything. Record the stop and the
resource under one lock, and have `Run` refuse to open once the stop
is recorded:

<!-- gofastr:compile
import "context"
import "sync"
import "github.com/DonaldMurillo/gofastr/framework"
type nativeWindow struct{ closed chan struct{} }
func (w *nativeWindow) Closed() <-chan struct{} { return w.closed }
func (*nativeWindow) Close(context.Context) error { return nil }
func openWindow() (*nativeWindow, error) { return &nativeWindow{closed: make(chan struct{})}, nil }
var _ framework.Host = (*windowHost)(nil)
-->
```go
type windowHost struct {
	mu      sync.Mutex
	win     *nativeWindow
	stopped bool
}

func (h *windowHost) Run(ctx context.Context, _ *framework.Session) error {
	h.mu.Lock()
	if h.stopped {
		h.mu.Unlock()
		return nil // Shutdown got here first
	}
	win, err := openWindow()
	if err != nil {
		h.mu.Unlock()
		return err // the App rolls back and Run returns err
	}
	h.win = win
	h.mu.Unlock()

	select {
	case <-ctx.Done(): // Shutdown began
	case <-win.Closed(): // the user closed the window: Run shuts the App down
	}
	return nil
}

func (h *windowHost) Stop(ctx context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.stopped = true
	if h.win == nil {
		return nil // stopped before Run opened anything
	}
	return h.win.Close(ctx)
}
```

The lock is held across `openWindow` so a `Stop` cannot land between
the check and the open. Keep toolkit event handlers and user code
outside it. `Stop` also runs after the user closed the window, so the
window's `Close` must tolerate a second call.

## Common mistakes

- **Registering `OnReady` under a host with no listener.** `Run` refuses
  the App. Wait on `ServicesReady`, or register the hook only on the
  path that calls `Start` or `Serve`.
- **Running the platform UI loop inside `Host.Run`.** The OS owns that
  loop and often the main thread. Call `App.Run` on a goroutine from the
  launch callback and let `Run` block on `ctx` and the host's own
  close event.
- **A `Stop` that assumes `Run` ran.** `Stop` is called for a stop
  during boot too. Release only what the host holds, and make a later
  `Run` open nothing.
- **A `Stop` that ignores `ctx`.** `Shutdown` waits for `Stop`, so one
  that blocks on a toolkit call with no timeout holds the whole teardown.
  Pass `ctx` on, or give up when it is done.
- **Serving `App.Router()` or `App.InProcessHandler()` from your own
  HTTP host.** Both skip the setup wizard, `WrapServer` and locale
  routing, and `InProcessHandler` is for trusted server-side calls,
  never a network listener. Use `Serve` with your listener.
- **Calling `Shutdown` synchronously from an `OnStart` hook to abort
  boot.** It waits out its deadline, and with `context.Background()` it
  never returns. Return the error instead.
- **Calling `log.Fatal` on whatever `<-done` returns before
  `ServicesReady`.** A stop during boot ends `Run` with nil. Check for
  nil first.
- **Running one App twice.** The second call returns
  `ErrAlreadyStarted`, or nil after `Shutdown`. Build a new App, over
  the same data directory if the data should survive.
