package rtc

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/handler"
)

// A joined socket was authorized once, at the upgrade: a peer whose session
// was revoked afterwards kept signaling and kept receiving the room until it
// disconnected (2026-10-27 bug bash, unrechecked). The socket re-checks on
// every inbound frame and on an idle ticker, and closes on refusal.
func TestRevokedPeerIsClosed(t *testing.T) {
	for _, tc := range []struct {
		name  string
		every time.Duration
		frame bool
		via   string // "authorize"/"moved": ServeHTTP re-runs Authorize; "principal": Serve re-checks the principal
	}{
		{"authorize idle", 20 * time.Millisecond, false, "authorize"},
		{"authorize on frame", time.Hour, true, "authorize"},
		{"authorize moves room", 20 * time.Millisecond, false, "moved"},
		{"principal idle", 20 * time.Millisecond, false, "principal"},
		{"principal on frame", time.Hour, true, "principal"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var revoked atomic.Bool
			s := newTestSignaler(t, Config{Authorize: func(*http.Request) (Join, error) {
				if revoked.Load() {
					switch tc.via {
					case "authorize":
						return Join{}, errors.New("session revoked")
					case "moved":
						return Join{Room: "room2", User: "u1"}, nil
					}
				}
				return Join{Room: "room1", User: "u1"}, nil
			}})
			s.recheckEvery = tc.every
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.via != "principal" {
					s.ServeHTTP(w, r)
					return
				}
				r = r.WithContext(handler.WithPrincipalCheck(r.Context(), func(c context.Context) (context.Context, bool) {
					return c, !revoked.Load()
				}))
				s.Serve(w, r, Join{Room: "room1", User: "u1"})
			}))
			t.Cleanup(srv.Close)
			t.Cleanup(s.Close)

			c := wsDial(t, srv.URL+"/ws/room1")
			c.expectEnv(t, "snapshot")
			revoked.Store(true)
			if tc.frame {
				c.send(map[string]any{"kind": "status", "data": map[string]any{"ok": true}})
			}
			deadline := time.Now().Add(3 * time.Second)
			for {
				if _, err := c.recv(time.Until(deadline)); err != nil {
					if !errors.Is(err, errClosed) {
						t.Fatalf("SECURITY: the revoked peer's socket stayed open: %v", err)
					}
					return
				}
			}
		})
	}
}
