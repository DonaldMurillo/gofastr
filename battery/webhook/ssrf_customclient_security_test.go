package webhook

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Property: supplying a custom Options.HTTPClient (for a proxy, tracing,
// or timeouts) must NOT silently drop the dial-time SSRF guard. With
// AllowPrivateNetworks=false the guard applies to every delivery client,
// caller-supplied or default — otherwise the common "just set a timeout"
// customization reopens 169.254.169.254 / RFC1918 / loopback.

func dialLoopback(t *testing.T, c *http.Client) error {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	resp, err := c.Do(req)
	if resp != nil {
		resp.Body.Close()
	}
	return err
}

func TestSSRF_CustomClientStillGuarded(t *testing.T) {
	// The exact customization the docs suggest: your own timeout.
	mgr := New(NewMemoryStore(), Options{
		HTTPClient: &http.Client{Timeout: 3 * time.Second},
	})
	if err := dialLoopback(t, mgr.opts.HTTPClient); err == nil {
		t.Fatal("SECURITY: [ssrf] custom HTTPClient delivered to loopback — the dial-time guard was dropped. Attack: DNS rebinding to 169.254.169.254 with any host that customizes the client.")
	}
}

func TestSSRF_CustomTransportSettingsSurvive(t *testing.T) {
	// The guard must wrap, not replace: the caller's transport — proxy,
	// custom dialer (egress tunnel, unix socket), pools — is used
	// verbatim underneath the per-request check. Swapping its dialer
	// would break clients that legitimately dial through private
	// infrastructure to reach public targets.
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.MaxIdleConns = 7 // sentinel
	mgr := New(NewMemoryStore(), Options{HTTPClient: &http.Client{Transport: tr}})

	got, ok := mgr.opts.HTTPClient.Transport.(*ssrfGuardedRoundTripper)
	if !ok {
		t.Fatalf("expected the guard wrapper after guarding, got %T", mgr.opts.HTTPClient.Transport)
	}
	if got.inner != http.RoundTripper(tr) {
		t.Fatal("caller transport replaced instead of wrapped — custom dialers/proxies would break")
	}
	if err := dialLoopback(t, mgr.opts.HTTPClient); err == nil {
		t.Fatal("SECURITY: [ssrf] custom *http.Transport client delivered to loopback")
	}
}

func TestSSRF_AllowPrivateSkipsGuardOnCustomClient(t *testing.T) {
	mgr := New(NewMemoryStore(), Options{
		AllowPrivateNetworks: true,
		HTTPClient:           &http.Client{Timeout: 3 * time.Second},
	})
	if err := dialLoopback(t, mgr.opts.HTTPClient); err != nil {
		t.Fatalf("AllowPrivateNetworks should leave the custom client unguarded, got: %v", err)
	}
}

// A fully custom RoundTripper (not *http.Transport) still gets guarded —
// per-request resolved-IP check wraps it.
type flagRT struct{ called bool }

func (f *flagRT) RoundTrip(r *http.Request) (*http.Response, error) {
	f.called = true
	return http.DefaultTransport.RoundTrip(r)
}

func TestSSRF_CustomRoundTripperGuarded(t *testing.T) {
	rt := &flagRT{}
	mgr := New(NewMemoryStore(), Options{HTTPClient: &http.Client{Transport: rt}})
	err := dialLoopback(t, mgr.opts.HTTPClient)
	if err == nil {
		t.Fatal("SECURITY: [ssrf] custom RoundTripper delivered to loopback unguarded")
	}
	if !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("expected the guard to refuse, got: %v", err)
	}
	if rt.called {
		t.Fatal("guard should refuse BEFORE the inner RoundTripper runs")
	}
}
