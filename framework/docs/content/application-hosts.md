# Application hosts

Every `framework.App` shares one boot engine, rollback sequence, and `Shutdown` order regardless of transport. `App.Run(ctx, host)` runs migrations, seeds, plugin/battery `Init`, setup, `OnStart`, and route finalization, then hands the booted app to a `framework.Host`.

| Call | Host | Transport |
| --- | --- | --- |
| `app.Start(addr)` | Built-in HTTP | Binds `addr`, handles `SIGINT`/`SIGTERM`, prints banner, fires `OnReady` |
| `app.Serve(ln)` | Built-in HTTP | Serves a caller-owned `net.Listener` (e.g. in-process WebView listener); closes `ln` on return |
| `app.Run(ctx, framework.ServicesHost())` | Services only | No listener or `http.Server`; entities, hooks, cron, queues, and DB run in-process |
| `app.Run(ctx, customHost)` | Custom | Any custom lifecycle (native desktop/mobile shell, terminal UI) |

An `App` runs once (`framework.ErrAlreadyStarted` on a second start).

## Running with no HTTP listener (`ServicesHost`)

`framework.ServicesHost()` is for native UIs, CLIs, and tests that call the app directly in Go:

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
		log.Fatal(err)
	}
	return
}

notes := app.MustCrudHandler("notes")
owner := handler.SetUser(context.Background(), data.User)
if _, err := notes.CreateOne(owner, map[string]any{"title": "first"}); err != nil {
	log.Print(err)
}

if err := <-done; err != nil {
	log.Print(err)
}
```

Owner scoping, RBAC, and hooks apply identically to in-process calls. To call through the HTTP router and middleware without opening a socket (as generated local SDK repositories do), use `app.InProcessHandler()` (see [SDK](sdk.md#typed-repositories)).

## Readiness and lifecycle rules

- **`ServicesReady()` vs. `OnReady`:** `App.ServicesReady()` closes as soon as all boot phases succeed on *any* host. `OnReady(func(addr string))` requires an HTTP listener, so `App.Run` refuses apps with `OnReady` hooks (or interactive HTTP first-run setup) when running under a non-HTTP host.
- **Stop order:** `Shutdown` marks `/readyz` `503` immediately, drains readiness, cancels the lifecycle context and A2A runs, calls `host.Stop`, waits for the erasure-retention sweep, stops process modules, stops batteries, and runs drainers and `OnStop` hooks, all bounded by the shutdown context. If the context ends before the sweep join is confirmed, shutdown proceeds with teardown and reports the context error unless an earlier step already failed. The outbox drainer waits for the relay before running hooks registered before the relay started. The queue, outbox relay, access grant-store fanout and event bridge drainers each start their stop and wait for it only until the drain context ends. Then they stop waiting and report `<stop> abandoned after the drain deadline` wrapping the context error, so the remaining hooks can run. An event bridge publish still in flight at that point keeps running in the background and may or may not reach other replicas (see [Events](events.md)). A drain context that has already ended gets that error at once. A panic in one of these stops is returned as an error instead of crashing the process. Custom `OnStop` hooks do not receive a context.
- **Stop during boot:** cancels the lifecycle context, lets the active phase return, skips remaining phases, and runs teardown (`host.Stop` still runs even when `host.Run` never started).

## Writing a custom `framework.Host`

Implement two methods:

```go
type Host interface {
	Run(ctx context.Context, s *framework.Session) error
	Stop(ctx context.Context) error
}
```

Coordinate `Run` and `Stop` under a mutex so a `Shutdown` that arrives before `Run` opens its resource prevents `Run` from opening it afterward, and always honor `ctx` inside `Stop`:

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
		return nil
	}
	win, err := openWindow()
	if err != nil {
		h.mu.Unlock()
		return err
	}
	h.win = win
	h.mu.Unlock()

	select {
	case <-ctx.Done():
	case <-win.Closed():
	}
	return nil
}

func (h *windowHost) Stop(ctx context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.stopped = true
	if h.win == nil {
		return nil
	}
	return h.win.Close(ctx)
}
```

## Common mistakes

- **Registering `OnReady` on a `ServicesHost` app:** `Run` refuses it. Wait on `App.ServicesReady()` instead.
- **Running the OS UI event loop inside `Host.Run`:** the OS owns the main thread; start `App.Run` on a goroutine and block `Host.Run` on `ctx` and your close channel.
- **Assuming `Host.Run` already ran inside `Host.Stop`:** `Stop` is also called on shutdown during boot.
- **Ignoring `ctx` in `Host.Stop`:** hangs graceful shutdown past its deadline.
- **Calling `Shutdown` synchronously from an `OnStart` hook:** deadlocks waiting for the hook to return; return an error instead.
- **Serving `App.Router()` or `App.InProcessHandler()` on a network socket:** skips first-run setup, `WrapServer`, and locale routing; use `App.Serve(ln)` instead.
