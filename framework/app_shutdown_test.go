package framework

import (
	"bufio"
	"context"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"testing"
	"time"
)

// docker stop / kubectl rollout send SIGTERM. Start must install signal
// handling by default and drain (server, batteries, OnStop hooks) before
// the process exits — deploy.md promises exactly this.
func TestSIGTERMDrainsApp(t *testing.T) {
	// Keep the test binary alive if Start does NOT handle the signal:
	// any Notify channel suppresses Go's default terminate action.
	guard := make(chan os.Signal, 1)
	signal.Notify(guard, syscall.SIGTERM)
	defer signal.Stop(guard)

	app := NewApp(WithoutDefaultMiddleware())
	stopped := make(chan struct{})
	app.OnStop(func() error { close(stopped); return nil })
	ready := make(chan string, 1)
	app.OnReady(func(addr string) { ready <- addr })

	done := make(chan error, 1)
	go func() { done <- app.Start("127.0.0.1:0") }()

	select {
	case <-ready:
	case err := <-done:
		t.Fatalf("Start returned before ready: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("server never became ready")
	}

	if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatalf("send SIGTERM: %v", err)
	}

	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("SIGTERM did not drain the app: OnStop hook never ran")
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Start should return nil after graceful drain, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Start did not return after SIGTERM drain")
	}
}

// An open SSE stream never goes idle, so http.Server.Shutdown alone waits
// out the full deadline and leaves the connection open. Shutdown must
// force-close whatever remains once the drain deadline expires — the
// drain is bounded, not best-effort-forever.
func TestShutdownForceClosesHangingConns(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware())
	app.Router().Get("/hang", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		<-r.Context().Done()
	}))
	ready := make(chan string, 1)
	app.OnReady(func(addr string) { ready <- addr })
	done := make(chan error, 1)
	go func() { done <- app.Start("127.0.0.1:0") }()

	var addr string
	select {
	case addr = <-ready:
	case err := <-done:
		t.Fatalf("Start returned before ready: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("server never became ready")
	}

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte("GET /hang HTTP/1.1\r\nHost: test\r\n\r\n")); err != nil {
		t.Fatalf("write request: %v", err)
	}
	// Wait for the response headers so the request is in-flight.
	br := bufio.NewReader(conn)
	line, err := br.ReadString('\n')
	if err != nil || !strings.Contains(line, "200") {
		t.Fatalf("expected 200 status line, got %q (%v)", line, err)
	}

	shutdownStart := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	_ = app.Shutdown(ctx) // deadline error expected: the stream can't drain
	if elapsed := time.Since(shutdownStart); elapsed > 3*time.Second {
		t.Fatalf("Shutdown not bounded by its context: took %v", elapsed)
	}

	// The hanging connection must now be force-closed: a read should
	// error out promptly instead of blocking until the deadline.
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	buf := make([]byte, 1024)
	for {
		if _, err := br.Read(buf); err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				t.Fatal("connection still open after Shutdown deadline — drain is not bounded")
			}
			break // EOF / reset: force-closed as required
		}
	}
	<-done
}
