package framework

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	uiapp "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// AppConfig.TrustedProxies reaches a mounted uihost that sets no
// uihost.WithTrustedProxies: the agent card and the page Link header take
// https from the X-Forwarded-Proto a listed proxy sends, and http when any
// other peer sends it.
func TestAppProxiesReachUIHost(t *testing.T) {
	on := true
	site := uiapp.NewApp("proxied")
	site.RegisterScreen(uiapp.NewScreen("/", &isAgenticScreen{}).WithTitle("Home"), nil)
	host := uihost.New(site, uihost.WithAgentReady(uihost.AgentReadyConfig{
		Title: "Proxied", AgentCard: &uihost.AgentCardConfig{Name: "Proxied", MCPEndpoint: "/mcp"}, LinkHeaders: &on,
	}))
	app := NewUIHostApp(host,
		WithConfig(AppConfig{Name: "proxied", TrustedProxies: []string{"10.0.0.0/8"}}),
		WithSecret("proxied-test-secret-with-32-bytes!!"))
	started, cleanup := startApp(t, app)
	defer cleanup()
	for _, c := range []struct{ remote, want string }{
		{"10.1.2.3:4444", "https://app.example.com"},
		{"203.0.113.9:4444", "http://app.example.com"},
	} {
		get := func(path string) *httptest.ResponseRecorder {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.Host = "app.example.com"
			req.Header.Set("X-Forwarded-Proto", "https")
			req.RemoteAddr = c.remote
			rec := httptest.NewRecorder()
			started.router.ServeHTTP(rec, req)
			return rec
		}
		if card := get("/.well-known/agent-card.json").Body.String(); !strings.Contains(card, `"`+c.want+`/mcp"`) {
			t.Errorf("peer %s: card service URL wants origin %s, card:\n%s", c.remote, c.want, card)
		}
		if link := get("/").Header().Get("Link"); !strings.Contains(link, "<"+c.want+"/.well-known/agent-card.json>") {
			t.Errorf("peer %s: Link wants origin %s, Link: %q", c.remote, c.want, link)
		}
	}
}
