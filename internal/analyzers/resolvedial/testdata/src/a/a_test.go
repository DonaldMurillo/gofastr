package a

import (
	"net"
	"net/http"
)

// A test double that resolves and delegates is not a shipped guard.
type testRT struct{ inner http.RoundTripper }

func (g testRT) RoundTrip(r *http.Request) (*http.Response, error) {
	_, _ = net.LookupIP(r.URL.Hostname())
	return g.inner.RoundTrip(r)
}
