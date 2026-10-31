package stream

import (
	"context"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/handler"
)

// A broker stream checked its caller once, at connect: a session revoked
// afterwards kept receiving every publish until the client disconnected
// (2026-10-27 bug bash, unrechecked). The heartbeat and every delivery
// re-check the principal and close on refusal.
func TestBrokerClosesRevokedStream(t *testing.T) {
	for _, tc := range []struct {
		name      string
		heartbeat time.Duration
		publish   bool
	}{
		{"on delivery", time.Hour, true},
		{"on heartbeat", 20 * time.Millisecond, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := NewSSEBroker(SSEBrokerConfig{Topic: "t", HeartbeatInterval: tc.heartbeat})
			defer b.Close()
			var revoked atomic.Bool
			ctx := handler.WithPrincipalCheck(t.Context(), func(c context.Context) (context.Context, bool) {
				return c, !revoked.Load()
			})
			req := httptest.NewRequest("GET", "/events", nil).WithContext(ctx)
			done := make(chan struct{})
			go func() { b.Subscribe(httptest.NewRecorder(), req); close(done) }()
			waitForSubscribers(t, b, 1)

			revoked.Store(true)
			if tc.publish {
				b.Publish("note", "secret")
			}
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("SECURITY: the stream stayed open after its principal was revoked")
			}
		})
	}
}
