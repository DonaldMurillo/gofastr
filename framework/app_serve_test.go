package framework

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"
)

// pipeListener is an in-process net.Listener: Dial hands the server one
// end of a net.Pipe. It is the shape a mobile shell uses, where the
// WebView's requests reach the app with no socket at all.
type pipeListener struct {
	conns  chan net.Conn
	once   sync.Once
	closed chan struct{}
}

func newPipeListener() *pipeListener {
	return &pipeListener{conns: make(chan net.Conn), closed: make(chan struct{})}
}

func (l *pipeListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.conns:
		return c, nil
	case <-l.closed:
		return nil, net.ErrClosed
	}
}

func (l *pipeListener) Close() error {
	l.once.Do(func() { close(l.closed) })
	return nil
}

func (l *pipeListener) isClosed() bool {
	select {
	case <-l.closed:
		return true
	default:
		return false
	}
}

func (l *pipeListener) Addr() net.Addr { return pipeAddr{} }

func (l *pipeListener) dial(ctx context.Context, _, _ string) (net.Conn, error) {
	client, server := net.Pipe()
	select {
	case l.conns <- server:
		return client, nil
	case <-l.closed:
		return nil, net.ErrClosed
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

type pipeAddr struct{}

func (pipeAddr) Network() string { return "pipe" }
func (pipeAddr) String() string  { return "pipe" }

func quietServeApp() *App {
	app := NewApp(WithoutDefaultMiddleware(), WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))))
	app.startupOutput = &bytes.Buffer{}
	app.Config.DisableSignalHandling = true
	return app
}

// Serve runs the whole Start lifecycle on a caller-supplied listener: a
// route answers over an in-process pipe, OnReady sees the listener's own
// address, and Shutdown ends Serve with nil.
func TestServeOnCallerListener(t *testing.T) {
	app := quietServeApp()
	app.Router().Get("/ping", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "pong")
	}))
	ready := make(chan string, 1)
	app.OnReady(func(addr string) { ready <- addr })

	ln := newPipeListener()
	done := make(chan error, 1)
	go func() { done <- app.Serve(ln) }()

	select {
	case addr := <-ready:
		if addr != "pipe" {
			t.Fatalf("OnReady got %q, want the listener's own address %q", addr, "pipe")
		}
	case err := <-done:
		t.Fatalf("Serve returned before OnReady: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("OnReady never fired")
	}

	client := &http.Client{Transport: &http.Transport{DialContext: ln.dial}, Timeout: 5 * time.Second}
	resp, err := client.Get("http://app.local/ping")
	if err != nil {
		t.Fatalf("GET over the pipe: %v", err)
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<10))
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(body) != "pong" {
		t.Fatalf("GET /ping = %d %q, want 200 %q", resp.StatusCode, body, "pong")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := app.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve after Shutdown = %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not return after Shutdown")
	}
	if !ln.isClosed() {
		t.Fatal("Serve returned without closing the listener")
	}
}

// A start phase that fails before the server exists still closes the
// caller's listener: Serve's contract is that every return closes it.
func TestServeClosesListenerOnEarlyAbort(t *testing.T) {
	app := quietServeApp()
	boom := errors.New("boom")
	app.OnStart(func(context.Context) error { return boom })
	ln := newPipeListener()
	if err := app.Serve(ln); !errors.Is(err, boom) {
		t.Fatalf("Serve = %v, want the start hook's error", err)
	}
	if !ln.isClosed() {
		t.Fatal("an aborted Serve left the caller's listener open")
	}
}

func TestServeNilListener(t *testing.T) {
	if err := quietServeApp().Serve(nil); err == nil {
		t.Fatal("Serve(nil) = nil, want an error")
	}
}

// countingListener counts Close calls on the listener it wraps.
type countingListener struct {
	*pipeListener
	mu     sync.Mutex
	closes int
}

func (l *countingListener) Close() error {
	l.mu.Lock()
	l.closes++
	l.mu.Unlock()
	return l.pipeListener.Close()
}

// Serve closes the caller's listener exactly once: a listener whose
// Close is not idempotent (one that closes a channel) must not panic
// on Shutdown, an early abort, or the two together.
func TestServeClosesTheListenerOnce(t *testing.T) {
	for name, abort := range map[string]bool{"shutdown": false, "early abort": true} {
		t.Run(name, func(t *testing.T) {
			app := quietServeApp()
			if abort {
				app.OnStart(func(context.Context) error { return errors.New("boom") })
			}
			ready := make(chan struct{})
			app.OnReady(func(string) { close(ready) })
			ln := &countingListener{pipeListener: newPipeListener()}
			done := make(chan error, 1)
			go func() { done <- app.Serve(ln) }()
			if !abort {
				select {
				case <-ready:
				case <-time.After(5 * time.Second):
					t.Fatal("OnReady never fired")
				}
				if err := app.Shutdown(context.Background()); err != nil {
					t.Fatal(err)
				}
			}
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("Serve did not return")
			}
			ln.mu.Lock()
			defer ln.mu.Unlock()
			if ln.closes != 1 {
				t.Fatalf("Close called %d times, want 1", ln.closes)
			}
		})
	}
}

// An app served on an in-process listener is local-only: no balancer
// can probe it, so Shutdown skips the production readiness drain (a
// phone shell going to the background has seconds, not five of them
// to spare). A unix socket is not: a proxy on the machine can front it.
func TestServeInProcessSkipsReadinessDrain(t *testing.T) {
	app := quietServeApp()
	ready := make(chan struct{})
	app.OnReady(func(string) { close(ready) })
	ln := newPipeListener()
	go func() { _ = app.Serve(ln) }()
	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("OnReady never fired")
	}
	t.Cleanup(func() { _ = app.Shutdown(context.Background()) })
	if !app.servesLocalOnly() {
		t.Fatal("an app on an in-process listener is not local-only")
	}
	if got := defaultReadinessDrain(false, false, app.servesLocalOnly()); got != 0 {
		t.Fatalf("readiness drain = %v, want 0", got)
	}
	for network, want := range map[string]bool{"pipe": true, "memory": true, "tcp": false, "tcp6": false, "unix": false} {
		if got := inProcessNetwork(network); got != want {
			t.Errorf("inProcessNetwork(%q) = %v, want %v", network, got, want)
		}
	}
}
