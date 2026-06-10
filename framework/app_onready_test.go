package framework

import (
	"context"
	"net"
	"strconv"
	"testing"
	"time"
)

// OnReady must fire only after the listener actually bound — with a real
// port resolved from :0 — so generated apps can print their startup banner
// without lying when an earlier phase (auto-migrate, hooks) fails.
func TestOnReadyFiresAfterBind(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware())
	ready := make(chan string, 1)
	app.OnReady(func(addr string) { ready <- addr })

	done := make(chan error, 1)
	go func() { done <- app.Start("127.0.0.1:0") }()

	select {
	case addr := <-ready:
		_, port, err := net.SplitHostPort(addr)
		if err != nil {
			t.Fatalf("OnReady addr %q: %v", addr, err)
		}
		if n, err := strconv.Atoi(port); err != nil || n == 0 {
			t.Fatalf("OnReady should receive the bound port, got %q", addr)
		}
	case err := <-done:
		t.Fatalf("Start returned before OnReady fired: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("OnReady never fired")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = app.Shutdown(ctx)
	<-done
}

// A failing OnStart hook aborts Start before the port binds — OnReady must
// not fire (that is the whole point of the hook).
func TestOnReadySkippedOnStartError(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware())
	fired := false
	app.OnReady(func(string) { fired = true })
	app.OnStart(func(context.Context) error { return errStored })
	if err := app.Start("127.0.0.1:0"); err == nil {
		t.Fatal("expected Start to fail from the OnStart hook")
	}
	if fired {
		t.Fatal("OnReady fired even though Start aborted before binding")
	}
}
