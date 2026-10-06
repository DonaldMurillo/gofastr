package log

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// With TrustForwardedFor on behind an appending proxy, the leftmost
// X-Forwarded-For entry is whatever the client sent. The access log's
// `remote` must record the hop the proxy wrote, not the forged prefix.
func TestRemoteIgnoresClientXFFPrefix(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "10.0.0.1:5555"
	r.Header.Set("X-Forwarded-For", "198.51.100.1, 203.0.113.7")
	if got := remoteAddr(r, true); got != "203.0.113.7" {
		t.Fatalf("remote = %q, want the proxy-written hop 203.0.113.7", got)
	}
}
