package stream

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Property: evicting a subscriber ends its handler. With no server
// WriteTimeout (the usual setting for SSE), a subscriber that stops
// reading parks its handler inside a socket write once the kernel
// buffers fill. Replacement and SeatOverflowEvictOldest used to close
// done and release the seat, but the parked write never looks at done:
// the handler stayed resident and the seat cap no longer bounded the
// goroutines and connections one principal holds.
func TestEvictionEndsBlockedHandler(t *testing.T) {
	for name, tc := range map[string]struct {
		cfg    SSEBrokerConfig
		nextID string
	}{
		"evict oldest": {
			cfg:    SSEBrokerConfig{MaxSeatsPerPrincipal: 1, SeatOverflow: SeatOverflowEvictOldest},
			nextID: "b",
		},
		"replace same id": {nextID: "a"},
	} {
		t.Run(name, func(t *testing.T) {
			cfg := tc.cfg
			cfg.Topic = "t"
			cfg.HeartbeatInterval = time.Hour
			cfg.Principal = func(r *http.Request) string { return r.Header.Get("X-User") }
			b := NewSSEBroker(cfg)
			defer b.Close()

			var resident atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				resident.Add(1)
				defer resident.Add(-1)
				b.Subscribe(w, r)
			}))
			defer srv.Close()
			defer srv.CloseClientConnections()

			// Subscriber "a" sends its request and never reads.
			stalled, err := net.Dial("tcp", srv.Listener.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			defer stalled.Close()
			if _, err := io.WriteString(stalled, "GET /?subscriber_id=a&buffer=1 HTTP/1.1\r\nHost: x\r\nX-User: u\r\n\r\n"); err != nil {
				t.Fatal(err)
			}
			waitForSubscribers(t, b, 1)
			var sub *subscriber
			b.mu.RLock()
			for _, s := range b.subscribers {
				sub = s
			}
			b.mu.RUnlock()

			// Publish far more than loopback socket buffers hold (32 MiB),
			// then confirm a's one-slot buffer stays full: its handler is
			// parked in a write.
			chunk := strings.Repeat("x", 64<<10)
			for range 512 {
				b.Publish("big", chunk)
			}
			deadline := time.Now().Add(10 * time.Second)
			for !blockedFor(sub, 200*time.Millisecond) {
				if time.Now().After(deadline) {
					t.Fatal("stalled subscriber's handler never blocked in a write")
				}
				b.Publish("big", chunk)
			}

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/?subscriber_id="+tc.nextID, nil)
			req.Header.Set("X-User", "u")
			go func() {
				resp, err := http.DefaultClient.Do(req)
				if err == nil {
					_, _ = io.Copy(io.Discard, resp.Body)
					resp.Body.Close()
				}
			}()

			// Wait for the newcomer to take the seat, then for the
			// evicted handler to return.
			waitUntil(t, func() bool {
				b.mu.RLock()
				defer b.mu.RUnlock()
				_, aSeated := b.subscribers[sub.regID]
				return len(b.subscribers) == 1 && (!aSeated || b.subscribers[sub.regID] != sub)
			})
			deadline = time.Now().Add(3 * time.Second)
			for resident.Load() != 1 {
				if time.Now().After(deadline) {
					t.Fatalf("%d handlers resident after the eviction, want 1: the evicted one is still parked in its write", resident.Load())
				}
				time.Sleep(5 * time.Millisecond)
			}
		})
	}
}

// Property: an eviction that reaches a handler after it returned leaves
// the connection alone. Between the unlock and the cut, a handler that
// saw done can return and net/http can start the next keep-alive request
// on the same socket; a past write deadline there would fail a stranger's
// response.
func TestLateCutSkipsReturnedHandler(t *testing.T) {
	cuts := 0
	sub := &subscriber{cut: func() { cuts++ }}
	sub.interruptWrite()
	if cuts != 1 {
		t.Fatalf("live handler: %d cuts, want 1", cuts)
	}
	sub.returned = true
	sub.interruptWrite()
	if cuts != 1 {
		t.Fatalf("returned handler was cut (%d cuts): the next request on its connection would fail", cuts)
	}
}

// blockedFor reports whether sub's buffer stays full for d: a handler
// still draining would empty the one slot.
func blockedFor(sub *subscriber, d time.Duration) bool {
	end := time.Now().Add(d)
	for time.Now().Before(end) {
		if len(sub.ch) < cap(sub.ch) {
			return false
		}
		time.Sleep(5 * time.Millisecond)
	}
	return true
}
