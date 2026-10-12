package netguard

import (
	"net/http"
	"testing"
)

// The dial-time check sees the address the transport connects to. Through
// an environment proxy that is the proxy, and the proxy then reaches the
// target, internal or not. A guarded transport must never route through
// one, whatever the environment says.
func TestGuardedTransportIgnoresEnvProxy(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://proxy.example:3128")
	t.Setenv("HTTPS_PROXY", "http://proxy.example:3128")
	tr := GuardedTransport(false, "netguard-test")
	if tr.Proxy != nil {
		req, _ := http.NewRequest(http.MethodGet, "http://metadata.example/latest", nil)
		u, err := tr.Proxy(req)
		t.Fatalf("guarded transport keeps a proxy func (resolves to %v, err %v); "+
			"the dial guard would only see the proxy's address", u, err)
	}
	// The opt-out keeps the environment proxy: nothing is guarded there.
	if GuardedTransport(true, "netguard-test").Proxy == nil {
		t.Error("allowPrivate transport dropped the environment proxy it has no reason to refuse")
	}
}
