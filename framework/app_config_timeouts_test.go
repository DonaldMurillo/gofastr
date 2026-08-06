package framework

import (
	"context"
	"testing"
	"time"
)

// TestMCPAppConfigReflectsRunningServerTimeouts pins the second half of
// effectiveServerTimeoutsMs: once App.Start has built the http.Server, the
// app_config introspection tool must report the deadlines the LIVE server
// actually uses — defaults plus HTTPServerTimeouts overrides and
// DisableRequestTimeout zeroing — not the pre-start default constants the
// pre-Start path falls back to. An operator asking "why did my handler get
// cut?" via MCP gets the resolved values, not guesses.
func TestMCPAppConfigReflectsRunningServerTimeouts(t *testing.T) {
	app := NewApp(
		WithMCPIntrospection(),
		WithHTTPServerTimeouts(HTTPServerTimeoutsConfig{
			ReadHeaderTimeout: new(4 * time.Second),
			ReadTimeout:       new(7 * time.Second),
			WriteTimeout:      new(8 * time.Second),
			IdleTimeout:       new(9 * time.Second),
		}),
	)
	_, stop := startOnRandomPort(t, app)
	defer stop()

	res, err := app.MCP.CallTool(context.Background(), "app_config", map[string]any{})
	if err != nil {
		t.Fatalf("CallTool app_config: %v", err)
	}
	m, ok := res.(map[string]any)
	if !ok {
		t.Fatalf("app_config result = %T, want map[string]any", res)
	}
	to, ok := m["http_server_timeouts_ms"].(map[string]int64)
	if !ok {
		t.Fatalf("http_server_timeouts_ms = %T, want map[string]int64", m["http_server_timeouts_ms"])
	}
	// Every value must match the override, proving the tool read the running
	// http.Server rather than reporting the pre-start defaults.
	want := map[string]int64{
		"read_header_ms": (4 * time.Second).Milliseconds(),
		"read_ms":        (7 * time.Second).Milliseconds(),
		"write_ms":       (8 * time.Second).Milliseconds(),
		"idle_ms":        (9 * time.Second).Milliseconds(),
	}
	for k, w := range want {
		if to[k] != w {
			t.Errorf("%s = %d, want %d (resolved live-server deadline)", k, to[k], w)
		}
	}
}
