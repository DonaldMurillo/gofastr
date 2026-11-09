package uihost

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// #669: resolveBaseURL took the scheme of the agent card's URLs and the
// page Link headers (sitemap, llms.txt, card, MCP endpoint) from an
// X-Forwarded-Proto header any client could send, so a client could make
// the host advertise http:// URLs or poison a cached document. The header
// now counts only when the TCP peer is listed in WithTrustedProxies, the
// rule framework/wellknown.go's resolveWellKnownBase applies (bb01c896a).
func TestForwardedProtoNeedsTrustedPeer(t *testing.T) {
	opts := func(extra ...Option) []Option {
		return append([]Option{
			WithAgentCard(AgentCardConfig{Name: "t", MCPEndpoint: "/mcp"}),
			WithAgentLinkHeaders(),
			WithSitemap(SitemapConfig{}),
		}, extra...)
	}
	get := func(ds *UIHost, path, proto, remote string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Host = "app.example.com"
		req.Header.Set("X-Forwarded-Proto", proto)
		req.RemoteAddr = remote
		rec := httptest.NewRecorder()
		ds.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status %d, want 200", path, rec.Code)
		}
		return rec
	}

	cases := []struct {
		name    string
		trusted []string
		remote  string
		want    string
	}{
		{"untrusted peer", []string{"10.0.0.0/8"}, "203.0.113.9:4444", "http://app.example.com"},
		{"no proxies configured", nil, "10.1.2.3:4444", "http://app.example.com"},
		{"trusted proxy", []string{"10.0.0.0/8"}, "10.1.2.3:4444", "https://app.example.com"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var extra []Option
			if tc.trusted != nil {
				extra = append(extra, WithTrustedProxies(tc.trusted...))
			}
			ds := newISOHost(opts(extra...)...)

			card := get(ds, "/.well-known/agent-card.json", "https", tc.remote).Body.String()
			if !strings.Contains(card, `"`+tc.want+`/mcp"`) {
				t.Errorf("SECURITY: [uihost-forwarded-proto] card service URL: want origin %s, card:\n%s", tc.want, card)
			}
			link := get(ds, "/", "https", tc.remote).Header().Get("Link")
			for _, rel := range []string{"/sitemap.xml>", "/.well-known/agent-card.json>", "/mcp>"} {
				if !strings.Contains(link, "<"+tc.want+rel) {
					t.Errorf("SECURITY: [uihost-forwarded-proto] Link %s: want origin %s, Link: %q", rel, tc.want, link)
				}
			}
		})
	}
}
