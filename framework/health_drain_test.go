package framework

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/lifecycle"
)

// startDrainApp starts app on a loopback port and returns its base URL.
func startDrainApp(t *testing.T, app *App) string {
	t.Helper()
	ready := make(chan string, 1)
	app.OnReady(func(addr string) { ready <- addr })
	done := make(chan error, 1)
	go func() { done <- app.Start("127.0.0.1:0") }()
	select {
	case addr := <-ready:
		return "http://" + addr
	case err := <-done:
		t.Fatalf("Start returned before ready: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("server never became ready")
	}
	return ""
}

// freshGet sends a GET on a new connection, so it proves the listener
// still accepts, not that a kept-alive connection still answers.
func freshGet(t *testing.T, url string) (int, []byte) {
	t.Helper()
	c := &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{DisableKeepAlives: true}}
	resp, err := c.Get(url)
	if err != nil {
		return 0, nil
	}
	defer resp.Body.Close()
	var buf [4096]byte
	n, _ := resp.Body.Read(buf[:])
	return resp.StatusCode, buf[:n]
}

// TestShutdownDrainsReadinessBeforeClosing pins the drain order a load
// balancer needs: on Shutdown, /readyz answers 503 at once, while the
// server keeps accepting and serving for the grace, so the balancer has
// time to notice and stop routing before the listener closes. Liveness
// stays 200 the whole time: a liveness failure gets the pod restarted,
// not drained. Before this, /readyz kept answering 200 until the
// listener closed, and requests the balancer still routed in that
// window were refused.
func TestShutdownDrainsReadinessBeforeClosing(t *testing.T) {
	const grace = 600 * time.Millisecond
	app := NewApp(WithoutDefaultMiddleware(), WithReadinessDrain(grace))
	app.Router().Get("/work", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	base := startDrainApp(t, app)

	if code, _ := freshGet(t, base+"/readyz"); code != http.StatusOK {
		t.Fatalf("/readyz before Shutdown = %d, want 200", code)
	}

	start := time.Now()
	shutdownDone := make(chan error, 1)
	go func() { shutdownDone <- app.Shutdown(context.Background()) }()

	// /readyz flips at once.
	var code int
	var body []byte
	for deadline := time.Now().Add(grace / 2); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if code, body = freshGet(t, base+"/readyz"); code == http.StatusServiceUnavailable {
			break
		}
	}
	if code != http.StatusServiceUnavailable {
		t.Fatalf("/readyz during the drain grace = %d, want 503", code)
	}
	var rr ReadinessResponse
	if err := json.Unmarshal(body, &rr); err != nil || rr.Status != "draining" {
		t.Errorf("/readyz drain body = %s (%v), want status draining", body, err)
	}
	// Still accepting and serving: liveness and real traffic.
	if code, _ := freshGet(t, base+"/healthz"); code != http.StatusOK {
		t.Errorf("/healthz during the drain grace = %d, want 200", code)
	}
	if code, _ := freshGet(t, base+"/work"); code != http.StatusOK {
		t.Errorf("a request during the drain grace = %d, want 200 (the server must still accept)", code)
	}

	select {
	case err := <-shutdownDone:
		if err != nil {
			t.Fatalf("Shutdown: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Shutdown never returned")
	}
	if took := time.Since(start); took < grace {
		t.Errorf("Shutdown took %v, want at least the %v grace", took, grace)
	}
	if code, _ := freshGet(t, base+"/healthz"); code != 0 {
		t.Errorf("/healthz after Shutdown = %d, want the listener closed", code)
	}
}

// TestShutdownDrainGraceZeroSkipsAndCtxBoundsIt: WithReadinessDrain(0)
// skips the wait, and a grace longer than Shutdown's context ends with
// the context, never past it.
func TestShutdownDrainGraceZeroSkipsAndCtxBoundsIt(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware(), WithReadinessDrain(0))
	startDrainApp(t, app)
	start := time.Now()
	if err := app.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if took := time.Since(start); took > time.Second {
		t.Errorf("Shutdown with a zero grace took %v", took)
	}

	app = NewApp(WithoutDefaultMiddleware(), WithReadinessDrain(time.Minute))
	startDrainApp(t, app)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start = time.Now()
	_ = app.Shutdown(ctx)
	if took := time.Since(start); took > 5*time.Second {
		t.Errorf("Shutdown with a one-minute grace and a 200ms context took %v", took)
	}
}

// TestReadinessDrainDefault: five seconds in production; zero in a Go
// test binary (and the processes it starts) and under `gofastr dev`, so
// every test that stops an App and every dev reload does not wait out a
// balancer that is not there. An explicit option always wins, and a
// negative one is zero. The signal handler's deadline grows by the
// grace, so the drain after it keeps its whole ShutdownTimeout.
func TestReadinessDrainDefault(t *testing.T) {
	if got := defaultReadinessDrain(false, false, false); got != 5*time.Second {
		t.Errorf("production default = %v, want 5s", got)
	}
	if got := defaultReadinessDrain(true, false, false); got != 0 {
		t.Errorf("default under test = %v, want 0", got)
	}
	if got := defaultReadinessDrain(false, true, false); got != 0 {
		t.Errorf("default under gofastr dev = %v, want 0", got)
	}
	if got := defaultReadinessDrain(false, false, true); got != 0 {
		t.Errorf("default for a local-only run = %v, want 0", got)
	}
	if got := NewApp(WithReadinessDrain(2 * time.Second)).readinessDrain(); got != 2*time.Second {
		t.Errorf("explicit grace = %v, want 2s", got)
	}
	if got := NewApp(WithReadinessDrain(-time.Second)).readinessDrain(); got != 0 {
		t.Errorf("negative grace = %v, want 0", got)
	}
	app := NewApp(WithReadinessDrain(3 * time.Second))
	if got := app.signalShutdownTimeout(); got != defaultShutdownTimeout+3*time.Second {
		t.Errorf("signal shutdown deadline = %v, want ShutdownTimeout + grace", got)
	}
}

// TestShutdownDrainLeavesTheTeardownItsShare is the reviewer's scenario:
// a 5s readiness grace and a caller's 5s Shutdown deadline. The grace
// used to wait out the whole deadline, so the HTTP drain, the batteries
// and the OnStop drainers ran on an expired context: Shutdown took 5s,
// returned "draining http server: context deadline exceeded" and cut a
// request still in flight. The grace now takes at most half of what is
// left of the context, so a request accepted during it completes and the
// drainers run with time to spare.
func TestShutdownDrainLeavesTheTeardownItsShare(t *testing.T) {
	const grace = 5 * time.Second
	app := NewApp(WithoutDefaultMiddleware(), WithReadinessDrain(grace))
	app.Router().Get("/slow", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	var (
		mu       sync.Mutex
		stopLeft time.Duration
		stopErr  error
	)
	app.lc.PrependDrainer(lifecycle.DrainFunc(func(ctx context.Context) error {
		mu.Lock()
		defer mu.Unlock()
		stopErr = ctx.Err()
		if dl, ok := ctx.Deadline(); ok {
			stopLeft = time.Until(dl)
		}
		return nil
	}))
	base := startDrainApp(t, app)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	start := time.Now()
	shutdownDone := make(chan error, 1)
	go func() { shutdownDone <- app.Shutdown(ctx) }()

	// Inside the (capped) grace: the listener still accepts. The request
	// is in flight when the grace ends and must still complete.
	time.Sleep(1500 * time.Millisecond)
	slow := make(chan int, 1)
	go func() {
		c := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{DisableKeepAlives: true}}
		resp, err := c.Get(base + "/slow")
		if err != nil {
			slow <- 0
			return
		}
		resp.Body.Close()
		slow <- resp.StatusCode
	}()

	var err error
	select {
	case err = <-shutdownDone:
	case <-time.After(15 * time.Second):
		t.Fatal("Shutdown never returned")
	}
	took := time.Since(start)
	if err != nil {
		t.Errorf("Shutdown(5s ctx) = %v, want nil: the grace starved the drain", err)
	}
	if code := <-slow; code != http.StatusOK {
		t.Errorf("request in flight when the grace ended = %d, want 200", code)
	}
	if took > 4500*time.Millisecond {
		t.Errorf("Shutdown took %v: the grace spent the caller's whole deadline", took)
	}
	mu.Lock()
	defer mu.Unlock()
	if stopErr != nil || stopLeft < 500*time.Millisecond {
		t.Errorf("stop drainer ran with ctx err %v and %v left, want a live context with time to spare", stopErr, stopLeft)
	}
}

// TestReadinessDrainCappedByDeadline: the grace never takes more than
// half of the time Shutdown's context has left; with no deadline it runs
// in full.
func TestReadinessDrainCappedByDeadline(t *testing.T) {
	if got := capReadinessDrain(context.Background(), 5*time.Second); got != 5*time.Second {
		t.Errorf("no deadline: grace = %v, want 5s", got)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	if got := capReadinessDrain(ctx, 5*time.Second); got > 2*time.Second || got < 1500*time.Millisecond {
		t.Errorf("4s deadline, 5s grace: grace = %v, want about 2s", got)
	}
	if got := capReadinessDrain(ctx, time.Second); got != time.Second {
		t.Errorf("4s deadline, 1s grace: grace = %v, want 1s (under the cap)", got)
	}
	done, cancelDone := context.WithTimeout(context.Background(), -time.Second)
	defer cancelDone()
	if got := capReadinessDrain(done, 5*time.Second); got != 0 {
		t.Errorf("expired deadline: grace = %v, want 0", got)
	}
	// The signal handler's deadline is ShutdownTimeout plus the grace, so
	// a grace up to ShutdownTimeout keeps its full length under the cap.
	app := NewApp(WithReadinessDrain(5 * time.Second))
	sctx, scancel := context.WithTimeout(context.Background(), app.signalShutdownTimeout())
	defer scancel()
	if got := capReadinessDrain(sctx, 5*time.Second); got < 4900*time.Millisecond {
		t.Errorf("signal handler deadline: grace = %v, want the full 5s", got)
	}
}

// TestReadinessDrainLocalRunDefaultsToZero: with no WithReadinessDrain,
// an app nothing balances skips the grace. A listener bound to loopback
// only (the desktop battery refuses any other bind) and a battery/service
// run (marked through SetLocalRunInfo, whatever it binds) are local; a
// wildcard bind is not.
func TestReadinessDrainLocalRunDefaultsToZero(t *testing.T) {
	loop := NewApp(WithoutDefaultMiddleware())
	startDrainApp(t, loop)
	t.Cleanup(func() { _ = loop.Shutdown(context.Background()) })
	if !loop.servesLocalOnly() {
		t.Error("an app bound to 127.0.0.1 is not local-only")
	}

	wide := NewApp(WithoutDefaultMiddleware())
	ready := make(chan string, 1)
	wide.OnReady(func(addr string) { ready <- addr })
	go func() { _ = wide.Start("0.0.0.0:0") }()
	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("wildcard app never became ready")
	}
	t.Cleanup(func() { _ = wide.Shutdown(context.Background()) })
	if wide.servesLocalOnly() {
		t.Error("an app bound to 0.0.0.0 is local-only; a balancer can reach it")
	}

	svc := NewApp(WithoutDefaultMiddleware())
	svc.SetLocalRunInfo(LocalRunInfo{Service: "acme"})
	if !svc.servesLocalOnly() {
		t.Error("a battery/service run is not local-only")
	}
}
