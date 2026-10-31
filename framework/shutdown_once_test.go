package framework

import (
	"bufio"
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// stopCounter is a battery that counts its OnStop calls.
type stopCounter struct{ stops atomic.Int32 }

func (*stopCounter) Name() string                   { return "stop-counter" }
func (*stopCounter) Init(*App) error                { return nil }
func (*stopCounter) OnStart(context.Context) error  { return nil }
func (s *stopCounter) OnStop(context.Context) error { s.stops.Add(1); return nil }

// Shutdown promises "safe to call multiple times; subsequent calls are
// no-ops", but a second call found the server already detached and went
// straight to stopping batteries: sequentially every OnStop ran twice,
// and concurrently (a signal handler plus a deferred Shutdown) the second
// caller closed batteries under requests the first was still draining
// (2026-10-27 S3).
func TestShutdownRunsOnce(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware())
	bat := &stopCounter{}
	app.RegisterBattery(bat)
	release := make(chan struct{})
	inFlight := make(chan struct{})
	app.Router().Get("/slow", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(inFlight)
		<-release
		w.WriteHeader(http.StatusOK)
	}))
	addr, done := startForTest(t, app)

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte("GET /slow HTTP/1.1\r\nHost: test\r\n\r\n")); err != nil {
		t.Fatal(err)
	}
	<-inFlight

	first := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		first <- app.Shutdown(ctx)
	}()
	waitDetached(t, app)

	second := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		second <- app.Shutdown(ctx)
	}()
	select {
	case <-second:
		t.Fatal("a concurrent Shutdown returned while the first was still draining a request")
	case <-time.After(300 * time.Millisecond):
	}
	if n := bat.stops.Load(); n != 0 {
		t.Fatalf("batteries stopped %d time(s) while a request was still in flight", n)
	}

	close(release)
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil || !strings.Contains(line, "200") {
		t.Fatalf("the drained request answered %q (%v), want 200", line, err)
	}
	for _, ch := range []chan error{first, second} {
		select {
		case err := <-ch:
			if err != nil {
				t.Fatalf("Shutdown: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("Shutdown never returned")
		}
	}
	<-done

	if err := app.Shutdown(context.Background()); err != nil {
		t.Fatalf("a later Shutdown: %v", err)
	}
	if n := bat.stops.Load(); n != 1 {
		t.Fatalf("OnStop ran %d times, want 1", n)
	}
}

// A Shutdown called from inside the teardown (here an OnStop hook) waits
// on its own context, so it returns at its deadline instead of waiting
// forever on the teardown it is part of.
func TestShutdownFromStopHookIsBounded(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware())
	inner := make(chan error, 1)
	app.OnStop(func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		inner <- app.Shutdown(ctx)
		return nil
	})
	_, done := startForTest(t, app)

	outer := make(chan error, 1)
	go func() { outer <- app.Shutdown(context.Background()) }()
	select {
	case <-outer:
	case <-time.After(5 * time.Second):
		t.Fatal("Shutdown deadlocked on a Shutdown called from its own stop hook")
	}
	if err := <-inner; !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("the nested Shutdown returned %v, want its deadline", err)
	}
	<-done
}

func startForTest(t *testing.T, app *App) (string, chan error) {
	t.Helper()
	ready := make(chan string, 1)
	app.OnReady(func(addr string) { ready <- addr })
	done := make(chan error, 1)
	go func() { done <- app.Start("127.0.0.1:0") }()
	select {
	case addr := <-ready:
		return addr, done
	case err := <-done:
		t.Fatalf("Start returned before ready: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("server never became ready")
	}
	return "", nil
}

// waitDetached waits until a Shutdown has taken the server, so the
// drain is under way.
func waitDetached(t *testing.T, app *App) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		app.serverMu.Lock()
		srv := app.server
		app.serverMu.Unlock()
		if srv == nil {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("Shutdown never took the server")
}
