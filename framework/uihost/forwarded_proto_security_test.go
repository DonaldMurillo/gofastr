package uihost

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/clientip"
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

// #669 review: the host read only WithTrustedProxies, so an App whose
// AppConfig.TrustedProxies listed its proxy still advertised http:// URLs
// behind it. With no host option the host reads the list the App puts on
// each request's context; a host list replaces it rather than adding to it.
func TestForwardedProtoReadsAppProxies(t *testing.T) {
	appList := clientip.ParseProxies([]string{"10.0.0.0/8"})
	cases := []struct {
		name   string
		host   []string
		remote string
		want   string
	}{
		{"App proxy", nil, "10.1.2.3:4444", "https://app.example.com"},
		{"peer outside the App list", nil, "203.0.113.9:4444", "http://app.example.com"},
		{"host list replaces the App's", []string{"192.168.0.0/16"}, "10.1.2.3:4444", "http://app.example.com"},
		{"host proxy", []string{"192.168.0.0/16"}, "192.168.1.1:4444", "https://app.example.com"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts := []Option{WithAgentCard(AgentCardConfig{Name: "t", MCPEndpoint: "/mcp"}), WithAgentLinkHeaders()}
			if tc.host != nil {
				opts = append(opts, WithTrustedProxies(tc.host...))
			}
			ds := newISOHost(opts...)
			get := func(path string) *httptest.ResponseRecorder {
				req := httptest.NewRequest(http.MethodGet, path, nil)
				req.Host = "app.example.com"
				req.Header.Set("X-Forwarded-Proto", "https")
				req.RemoteAddr = tc.remote
				req = req.WithContext(clientip.WithTrusted(req.Context(), appList))
				rec := httptest.NewRecorder()
				ds.ServeHTTP(rec, req)
				return rec
			}
			if card := get("/.well-known/agent-card.json").Body.String(); !strings.Contains(card, `"`+tc.want+`/mcp"`) {
				t.Errorf("SECURITY: [uihost-app-proxies] card service URL: want origin %s, card:\n%s", tc.want, card)
			}
			if link := get("/").Header().Get("Link"); !strings.Contains(link, "<"+tc.want+"/.well-known/agent-card.json>") {
				t.Errorf("SECURITY: [uihost-app-proxies] Link: want origin %s, Link: %q", tc.want, link)
			}
		})
	}
}
