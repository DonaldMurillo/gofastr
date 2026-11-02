package framework

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"
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
	if got := defaultReadinessDrain(false, false); got != 5*time.Second {
		t.Errorf("production default = %v, want 5s", got)
	}
	if got := defaultReadinessDrain(true, false); got != 0 {
		t.Errorf("default under test = %v, want 0", got)
	}
	if got := defaultReadinessDrain(false, true); got != 0 {
		t.Errorf("default under gofastr dev = %v, want 0", got)
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
