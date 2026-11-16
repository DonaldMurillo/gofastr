//go:build unix

package framework

import (
	"context"
	"io"
	"net"
	"testing"
	"time"
)

// A unix socket whose path reads like a loopback host:port does not make
// the app's bound host loopback.
func TestSweepServeUnixSocketPathIsNotABoundHost(t *testing.T) {
	t.Chdir(t.TempDir())
	ln, err := net.Listen("unix", "127.0.0.1:8080")
	if err != nil {
		t.Fatal(err)
	}
	app := NewApp(WithoutDefaultMiddleware())
	app.startupOutput = io.Discard
	app.Config.DisableSignalHandling = true
	ready := make(chan struct{})
	app.OnReady(func(string) { close(ready) })
	done := make(chan error, 1)
	go func() { done <- app.Serve(ln) }()
	select {
	case <-ready:
	case err := <-done:
		t.Fatalf("Serve returned before ready: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("Serve never became ready")
	}
	app.serverMu.Lock()
	host := app.boundHost
	app.serverMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := app.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	<-done
	if host != "" {
		t.Fatalf("boundHost = %q for a unix socket, want empty", host)
	}
}
