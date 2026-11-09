package entityui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/internal/clientip"
)

// trustedPeer is the TCP peer the copy-link tests list as a trusted
// proxy when they test the scheme enum rather than the trust rule.
const trustedPeer = "10.1.2.3:4444"

// copyLinkPage renders invoice inv-1's record page for a request from
// remote, under the trusted-proxy list trusted (what the App stamps on
// each request context), that carries the given X-Forwarded-Proto.
func copyLinkPage(t *testing.T, x *testUI, trusted []string, remote, proto string) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/rec/invoices/inv-1", nil)
	req.RemoteAddr = remote
	req.Header.Set("X-Forwarded-Proto", proto)
	ctx := clientip.WithTrusted(context.Background(), clientip.ParseProxies(trusted))
	req = req.WithContext(ctx)
	ctx = handler.SetUser(app.WithRequest(ctx, req), &testUser{id: "u1"})
	return string(x.ui.Record("invoices", "inv-1").Base("/rec/invoices").RenderCtx(ctx))
}

// The copy link is built from the request, and X-Forwarded-Proto is
// request input: even from a trusted proxy only an exact http or https
// may pick the scheme, so a forged value cannot put another origin in
// the link the reader copies.
func TestCopyLinkRefusesForgedScheme(t *testing.T) {
	x := newInvoiceUI(t)
	trusted := []string{"10.0.0.0/8"}
	for _, proto := range []string{"https://evil.example/p", "https,http", "javascript"} {
		body := copyLinkPage(t, x, trusted, trustedPeer, proto)
		if strings.Contains(body, "evil.example") || strings.Contains(body, proto+"://") {
			t.Fatalf("X-Forwarded-Proto %q reached the copy link:\n%s", proto, body)
		}
		if !strings.Contains(body, ">http://example.com/rec/invoices/inv-1<") {
			t.Fatalf("X-Forwarded-Proto %q: want the request's own http origin:\n%s", proto, body)
		}
	}
}

// #669: the record's copy link and Copy API URL took their scheme from
// an X-Forwarded-Proto any client could send. The header now counts only
// from a peer listed in AppConfig.TrustedProxies, the rule the
// well-known documents, uihost and pluginhost follow.
func TestCopyLinkForwardedProtoTrust(t *testing.T) {
	x := newInvoiceUI(t)
	cases := []struct {
		name    string
		trusted []string
		remote  string
		scheme  string
	}{
		{"untrusted peer", []string{"10.0.0.0/8"}, "203.0.113.9:4444", "http"},
		{"no proxies configured", nil, trustedPeer, "http"},
		{"trusted proxy", []string{"10.0.0.0/8"}, trustedPeer, "https"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := copyLinkPage(t, x, tc.trusted, tc.remote, "https")
			for _, want := range []string{
				">" + tc.scheme + "://example.com/rec/invoices/inv-1<",
				">" + tc.scheme + "://example.com/api/invoices/inv-1<",
			} {
				if !strings.Contains(body, want) {
					t.Errorf("SECURITY: [entityui-forwarded-proto] want %q in the record page:\n%s", want, body)
				}
			}
		})
	}
}
