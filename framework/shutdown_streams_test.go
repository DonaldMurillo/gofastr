package framework

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/stream"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// startStreamApp starts a on a loopback port and returns its address and
// the channel Start's result arrives on.
func startStreamApp(t *testing.T, a *App) (string, chan error) {
	t.Helper()
	a.Config.DisableSignalHandling = true
	ready := make(chan string, 1)
	a.OnReady(func(addr string) { ready <- addr })
	done := make(chan error, 1)
	go func() { done <- a.Start("127.0.0.1:0") }()
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

// shutdownFast shuts a down and fails when the drain took long enough to
// mean it waited on an open stream.
func shutdownFast(t *testing.T, a *App, done chan error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	started := time.Now()
	if err := a.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if took := time.Since(started); took > 3*time.Second {
		t.Fatalf("Shutdown took %v; the drain waited on an open stream", took)
	}
	<-done
}

// wsHandshake dials addr and completes a WebSocket handshake on path.
func wsHandshake(t *testing.T, addr, path string) (net.Conn, *bufio.Reader) {
	t.Helper()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	var key [16]byte
	_, _ = rand.Read(key[:])
	req := "GET " + path + " HTTP/1.1\r\nHost: " + addr + "\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n" +
		"Sec-WebSocket-Key: " + base64.StdEncoding.EncodeToString(key[:]) + "\r\nSec-WebSocket-Version: 13\r\n\r\n"
	if _, err := conn.Write([]byte(req)); err != nil {
		t.Fatal(err)
	}
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, nil)
	if err != nil || resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("handshake: %v %v", resp, err)
	}
	return conn, br
}

// A WebSocket is a hijacked connection: Server.Shutdown neither waits
// for it nor closes it, so it outlived Shutdown and kept a handler
// working against stopped batteries. Shutdown closes a core/stream
// connection with 1001, going away, even after the handler that opened
// it returned.
func TestShutdownClosesWebSocket(t *testing.T) {
	a := NewApp(WithoutDefaultMiddleware())
	returned := make(chan struct{})
	a.Router().Get("/ws", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(returned)
		c, err := stream.Upgrade(w, r, stream.WSConfig{CheckOrigin: func(*http.Request) bool { return true }})
		if err != nil {
			return
		}
		go func() {
			for {
				if _, err := c.Read(); err != nil {
					return
				}
			}
		}()
	}))
	addr, done := startStreamApp(t, a)
	conn, br := wsHandshake(t, addr, "/ws")
	<-returned
	settleAfterHandler()

	got := make(chan uint16, 1)
	go func() {
		_ = conn.SetReadDeadline(time.Now().Add(4 * time.Second))
		var hdr [2]byte
		if _, err := io.ReadFull(br, hdr[:]); err != nil || hdr[0]&0x0f != 0x8 {
			got <- 0
			return
		}
		payload := make([]byte, hdr[1]&0x7f)
		if _, err := io.ReadFull(br, payload); err != nil || len(payload) < 2 {
			got <- 0
			return
		}
		got <- binary.BigEndian.Uint16(payload)
	}()
	shutdownFast(t, a, done)
	if code := <-got; code != shutdownCloseCode {
		t.Fatalf("close code = %d, want %d (going away)", code, shutdownCloseCode)
	}
}

// A handler that hijacks by hand gets no close frame, but its connection
// is closed: nothing keeps serving past Shutdown.
func TestShutdownClosesHijackedConn(t *testing.T) {
	a := NewApp(WithoutDefaultMiddleware())
	a.Router().Get("/raw", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, rw, err := http.NewResponseController(w).Hijack()
		if err != nil {
			return
		}
		_, _ = rw.WriteString("HTTP/1.1 101 Switching Protocols\r\nUpgrade: x-raw\r\nConnection: Upgrade\r\n\r\n")
		_ = rw.Flush()
		go func() { _, _ = io.Copy(io.Discard, conn) }()
	}))
	addr, done := startStreamApp(t, a)
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte("GET /raw HTTP/1.1\r\nHost: test\r\nUpgrade: x-raw\r\nConnection: Upgrade\r\n\r\n")); err != nil {
		t.Fatal(err)
	}
	br := bufio.NewReader(conn)
	if resp, err := http.ReadResponse(br, nil); err != nil || resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("upgrade: %v %v", resp, err)
	}
	shutdownFast(t, a, done)
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := br.ReadByte(); err != io.EOF {
		t.Fatalf("read after Shutdown = %v, want EOF: the hijacked connection is still open", err)
	}
}

// The island bus (/__gofastr/sse) is an event stream like any other:
// Shutdown does not wait out its deadline on an open tab.
func TestShutdownEndsIslandBus(t *testing.T) {
	a := NewApp(WithoutDefaultMiddleware())
	site := app.NewApp("bus")
	host := uihost.New(site)
	a.Mount(host)
	sess := host.CreateSession()
	addr, done := startStreamApp(t, a)

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	req := "GET /__gofastr/sse?session=" + sess.ID + " HTTP/1.1\r\nHost: 127.0.0.1\r\nAccept: text/event-stream\r\n" +
		"Cookie: gofastr-session=" + sess.Token + "\r\n\r\n"
	if _, err := conn.Write([]byte(req)); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil || !strings.Contains(line, "200") {
		t.Fatalf("bus status line %q (%v)", line, err)
	}
	shutdownFast(t, a, done)
}

// A hijack once the drain has ended the others is refused: a connection
// taken over then would outlive Shutdown.
func TestUpgradeDuringDrainRefused(t *testing.T) {
	a := &App{}
	a.streams.endAll(context.Background())
	var hijackErr error
	h := a.endStreamsOnShutdown(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _, hijackErr = http.NewResponseController(w).Hijack()
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/ws", nil))
	if !errors.Is(hijackErr, errDraining) {
		t.Fatalf("hijack during the drain: %v, want errDraining", hijackErr)
	}
}

// A stream is a stream because the handler answers with one, not because
// the client asked: curl -N sends Accept: */* and still gets an event
// stream, which must not hold the drain.
func TestStreamWithoutAcceptEnds(t *testing.T) {
	a := NewApp(WithoutDefaultMiddleware())
	a.Router().Get("/events", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_ = http.NewResponseController(w).Flush()
		<-r.Context().Done()
	}))
	addr, done := startStreamApp(t, a)
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	if _, err := conn.Write([]byte("GET /events HTTP/1.1\r\nHost: test\r\nAccept: */*\r\n\r\n")); err != nil {
		t.Fatal(err)
	}
	if line, err := bufio.NewReader(conn).ReadString('\n'); err != nil || !strings.Contains(line, "200") {
		t.Fatalf("status line %q (%v)", line, err)
	}
	shutdownFast(t, a, done)
}

// A JSON GET whose Accept lists text/event-stream (here at q=0) is an
// ordinary request: Shutdown drains it, it does not cancel it.
func TestJSONGetDrainsNormally(t *testing.T) {
	a := &App{}
	var ctxErr error
	h := a.endStreamsOnShutdown(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a.streams.endAll(context.Background())
		ctxErr = r.Context().Err()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	req := httptest.NewRequest(http.MethodGet, "/api/items", nil)
	req.Header.Set("Accept", "application/json, text/event-stream;q=0")
	h.ServeHTTP(httptest.NewRecorder(), req)
	if ctxErr != nil {
		t.Fatalf("a JSON GET was cancelled by the drain: %v", ctxErr)
	}
}

// A WebSocket leaves the open set when its connection closes, not when
// its handler returns, and not never: a long-running server must not
// keep every connection it ever served.
func TestClosedWebSocketLeavesSet(t *testing.T) {
	a := NewApp(WithoutDefaultMiddleware())
	opened := make(chan *stream.WebSocketConn, 1)
	returned := make(chan struct{})
	a.Router().Get("/ws", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(returned)
		c, err := stream.Upgrade(w, r, stream.WSConfig{CheckOrigin: func(*http.Request) bool { return true }, CloseTimeout: 50 * time.Millisecond})
		if err != nil {
			return
		}
		opened <- c
	}))
	addr, done := startStreamApp(t, a)
	t.Cleanup(func() { shutdownFast(t, a, done) })
	wsHandshake(t, addr, "/ws")
	c := <-opened
	<-returned
	settleAfterHandler()
	if n := openCount(a); n != 1 {
		t.Fatalf("open set holds %d after the handler returned, want 1", n)
	}
	_ = c.Close()
	deadline := time.Now().Add(2 * time.Second)
	for openCount(a) != 0 {
		if time.Now().After(deadline) {
			t.Fatalf("open set still holds %d after the connection closed", openCount(a))
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// settleAfterHandler gives the middleware around a handler that just
// returned time to finish its own deferred work, so a test sees the
// state a long-lived connection is in once its handler is long gone.
func settleAfterHandler() { time.Sleep(100 * time.Millisecond) }

func openCount(a *App) int {
	a.streams.mu.Lock()
	defer a.streams.mu.Unlock()
	return len(a.streams.open)
}
