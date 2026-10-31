package uihost

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/island"
)

// WithTrustedProxies reaches the island manager: behind a listed proxy,
// two anonymous visitors count as two clients for the stream cap; with
// no list they are the proxy's one address (2026-10-27 R26).
func TestTrustedProxiesReachTheStreamCap(t *testing.T) {
	req := func(xff string) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/__gofastr/sse?session=s", nil)
		r.RemoteAddr = "10.0.0.1:4711"
		r.Header.Set("X-Forwarded-For", xff)
		return r
	}
	anon := island.PresenceIdentity{}
	proxied := New(app.NewApp("p"), WithTrustedProxies("10.0.0.0/8")).Islands
	if proxied.ClientKey(req("198.51.100.1"), anon) == proxied.ClientKey(req("198.51.100.2"), anon) {
		t.Fatal("visitors behind a listed proxy share one stream budget")
	}
	plain := New(app.NewApp("p")).Islands
	if plain.ClientKey(req("198.51.100.1"), anon) != plain.ClientKey(req("198.51.100.2"), anon) {
		t.Fatal("X-Forwarded-For was read with no trusted proxy")
	}
}
