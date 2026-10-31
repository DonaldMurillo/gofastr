package cache

import (
	"bufio"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/stream"
)

// A websocket upgrade behind the cache: core/stream asserts
// http.Hijacker, and once the handler holds the connection the
// middleware must neither write a response nor store one (2026-10-27
// B16). Without the recorder's own Hijack, a hijack through Unwrap
// leaves the empty 200 the recorder still holds to be cached and served
// to the next upgrade.
func TestCacheLetsWebSocketsUpgrade(t *testing.T) {
	var runs atomic.Int64
	h := CacheMiddleware(NewMemoryCache(WithTTL(time.Minute)), time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		runs.Add(1)
		c, err := stream.Upgrade(w, r, stream.WSConfig{})
		if err != nil {
			t.Errorf("Upgrade behind the cache: %v", err)
			return
		}
		_ = c.Close()
	}))
	// The next upgrade waits until the middleware has returned from the
	// last one, so anything it stored is there to be served.
	done := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.ServeHTTP(w, r)
		done <- struct{}{}
	}))
	defer srv.Close()

	for i := range 2 {
		conn, err := net.Dial("tcp", srv.Listener.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		_, _ = conn.Write([]byte("GET /ws HTTP/1.1\r\nHost: " + srv.Listener.Addr().String() +
			"\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Version: 13\r\n" +
			"Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n\r\n"))
		status, err := bufio.NewReader(conn).ReadString('\n')
		_ = conn.Close()
		if err != nil {
			t.Fatalf("upgrade %d: %v", i, err)
		}
		if !strings.HasPrefix(status, "HTTP/1.1 101") {
			t.Fatalf("upgrade %d answered %q, want 101", i, strings.TrimSpace(status))
		}
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatalf("upgrade %d: the handler never returned", i)
		}
	}
	if runs.Load() != 2 {
		t.Fatalf("handler ran %d times, want 2: an upgrade was served from the cache", runs.Load())
	}
}
