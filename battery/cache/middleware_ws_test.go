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

// A hijack the client writer refuses (an outer middleware's writer that
// cannot hijack, http.TimeoutHandler's for one) leaves the response with
// the handler, which answers the failed upgrade with an error status.
// The recorder marked itself streamed before it tried, so that status
// and its headers were dropped and the client got the error text as a
// 200.
func TestCacheKeepsStatusWhenHijackFails(t *testing.T) {
	h := CacheMiddleware(NewMemoryCache(WithTTL(time.Minute)), time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := stream.Upgrade(w, r, stream.WSConfig{}); err == nil {
			t.Error("Upgrade succeeded on a writer that cannot hijack")
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		http.Error(w, "upgrade failed", http.StatusInternalServerError)
	}))
	for i := range 2 {
		req := httptest.NewRequest(http.MethodGet, "/ws", nil)
		req.Header.Set("Upgrade", "websocket")
		req.Header.Set("Connection", "Upgrade")
		req.Header.Set("Sec-WebSocket-Version", "13")
		req.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
		rec := httptest.NewRecorder() // no http.Hijacker
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("request %d: status %d, want the handler's 500", i, rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
			t.Errorf("request %d: Content-Type %q, want the handler's text/plain", i, ct)
		}
		if got := rec.Header().Get("X-Cache"); got != "MISS" {
			t.Errorf("request %d: X-Cache %q, want MISS (an error is never stored)", i, got)
		}
	}
}
