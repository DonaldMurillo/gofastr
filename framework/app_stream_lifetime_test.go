package framework

import (
	"testing"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// DisableRequestTimeout documents itself as the opt-out for SSE and long
// uploads; the server-level read/write timeouts must follow it, or the
// opt-out is a lie past 60 seconds.
func TestDisableTimeoutRelaxesServer(t *testing.T) {
	app := NewApp(WithConfig(AppConfig{DisableRequestTimeout: true}))

	_, stop := startOnRandomPort(t, app)
	defer stop()

	app.serverMu.Lock()
	srv := app.server
	app.serverMu.Unlock()
	if srv.ReadTimeout != 0 || srv.WriteTimeout != 0 {
		t.Fatalf("read/write timeouts = %v/%v, want 0/0 when request timeout is disabled",
			srv.ReadTimeout, srv.WriteTimeout)
	}
	if srv.ReadHeaderTimeout == 0 || srv.IdleTimeout == 0 {
		t.Fatal("header/idle timeouts must stay set — they bound the connection, not the stream")
	}
}
