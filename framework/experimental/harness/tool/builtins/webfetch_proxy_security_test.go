package builtins

import "testing"

// Through an environment proxy the dial-time SSRF check sees only the
// proxy's address; the proxy then reaches whatever the model asked for.
func TestWebFetchIgnoresEnvProxy(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://proxy.example:3128")
	t.Setenv("HTTPS_PROXY", "http://proxy.example:3128")
	if webFetchTransport().Proxy != nil {
		t.Fatal("WebFetch's guarded transport routes through the environment proxy, past its dial check")
	}
}
