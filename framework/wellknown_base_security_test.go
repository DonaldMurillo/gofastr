package framework

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// #669 (was #657): the well-known documents built their absolute URLs
// from r.Host and an X-Forwarded-Proto header any client could set, so a
// spoofed Host painted an attacker-named origin into cacheable
// discovery documents. The base now comes from the configured
// AppConfig.BaseURL when set; without one, X-Forwarded-Proto is honored
// only from a configured trusted proxy, never from an arbitrary client.
func TestWellKnownBaseNotSpoofable(t *testing.T) {
	newApp := func(t *testing.T, cfg AppConfig) *App {
		t.Helper()
		fwApp := NewApp(WithMCP(), WithConfig(cfg))
		fwApp.Entity("posts", entity.EntityConfig{
			Table:  "posts",
			Fields: []schema.Field{{Name: "title", Type: schema.String}},
		}.WithTimestamps(false))
		a, cleanup := startApp(t, fwApp)
		t.Cleanup(cleanup)
		return a
	}
	cardURL := func(t *testing.T, app *App, host, proto, remote string) string {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/.well-known/mcp/server-card.json", nil)
		req.Host = host
		if proto != "" {
			req.Header.Set("X-Forwarded-Proto", proto)
		}
		if remote != "" {
			req.RemoteAddr = remote
		}
		w := httptest.NewRecorder()
		app.router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("server card: %d %s", w.Code, w.Body.String())
		}
		var card struct {
			Remotes []struct {
				URL string `json:"url"`
			} `json:"remotes"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &card); err != nil {
			t.Fatalf("decode card: %v", err)
		}
		if len(card.Remotes) == 0 {
			t.Fatal("card has no remotes")
		}
		return card.Remotes[0].URL
	}

	t.Run("configured base wins over Host and proto", func(t *testing.T) {
		app := newApp(t, AppConfig{BaseURL: "https://app.example.com"})
		got := cardURL(t, app, "evil.example", "http", "203.0.113.9:4444")
		if !strings.HasPrefix(got, "https://app.example.com/") {
			t.Errorf("SECURITY: [wellknown-cache-poison] spoofed Host/proto reached the card: url %q, want the configured base", got)
		}
	})

	t.Run("forwarded proto ignored from an untrusted peer", func(t *testing.T) {
		app := newApp(t, AppConfig{TrustedProxies: []string{"10.0.0.0/8"}})
		got := cardURL(t, app, "app.example.com", "https", "203.0.113.9:4444")
		if strings.HasPrefix(got, "https://") {
			t.Errorf("SECURITY: [wellknown-cache-poison] X-Forwarded-Proto from an untrusted client changed the origin: url %q", got)
		}
	})

	t.Run("forwarded proto honored from a trusted proxy", func(t *testing.T) {
		app := newApp(t, AppConfig{TrustedProxies: []string{"10.0.0.0/8"}})
		got := cardURL(t, app, "app.example.com", "https", "10.1.2.3:4444")
		if !strings.HasPrefix(got, "https://") {
			t.Errorf("X-Forwarded-Proto from the configured trusted proxy was ignored: url %q", got)
		}
	})
}
