package netguard

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestSweepCheckOutboundURLRefusals(t *testing.T) {
	refused := []string{
		"",
		"ftp://example.com/x",
		"http://",
		"https://user:pw@example.com/x",
		"http://localhost/x",
		"http://LocalHost:8080/x",
		"http://app.localhost/x",
		"http://metadata.google.internal/x",
		"http://svc.cluster.INTERNAL/x",
		"http://127.0.0.1/x",
		"http://[::1]/x",
		"http://169.254.169.254/latest",
		"http://10.0.0.1/x",
		"http://[64:ff9b::a9fe:a9fe]/x",
		"http://:8080/x",
	}
	for _, raw := range refused {
		err := CheckOutboundURL(raw, "probe", false)
		if err == nil {
			t.Errorf("CheckOutboundURL(%q) accepted, want refused", raw)
			continue
		}
		if !strings.HasPrefix(err.Error(), "probe: ") {
			t.Errorf("CheckOutboundURL(%q) error %q does not start with the prefix", raw, err)
		}
	}
	for _, raw := range []string{"https://93.184.216.34/hook", "http://8.8.8.8:8080/x"} {
		if err := CheckOutboundURL(raw, "probe", false); err != nil {
			t.Errorf("CheckOutboundURL(%q) refused a public literal: %v", raw, err)
		}
	}
	for _, raw := range []string{"http://localhost/x", "http://127.0.0.1/x", "http://a.internal/x", "http://10.0.0.5:8080/x", "http://[::1]:9000/x"} {
		if err := CheckOutboundURL(raw, "probe", true); err != nil {
			t.Errorf("allowPrivate refused %q: %v", raw, err)
		}
	}
	for _, raw := range []string{"ftp://localhost/x", "http://u:p@localhost/x", "http://", "http://:8080/x"} {
		if err := CheckOutboundURL(raw, "probe", true); err == nil {
			t.Errorf("allowPrivate accepted %q, want the scheme, userinfo and host checks kept", raw)
		}
	}
}

func hangingResolver(t *testing.T) {
	t.Helper()
	prev := net.DefaultResolver
	net.DefaultResolver = &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	t.Cleanup(func() { net.DefaultResolver = prev })
}

func TestSweepCheckOutboundURLLookupBounded(t *testing.T) {
	hangingResolver(t)
	start := time.Now()
	if err := CheckOutboundURL("https://hangs.example.test/x", "probe", false); err != nil {
		t.Fatalf("a lookup that never answers must pass to the dial-time guard, got %v", err)
	}
	if d := time.Since(start); d > lookupTimeout+time.Second {
		t.Fatalf("CheckOutboundURL waited %s on DNS, want at most %s", d, lookupTimeout)
	}
}

type countRT struct{ calls int }

func (c *countRT) RoundTrip(*http.Request) (*http.Response, error) {
	c.calls++
	return nil, context.Canceled
}

func TestSweepGuardRoundTripperShapes(t *testing.T) {
	if tr, ok := GuardRoundTripper(nil, "probe").(*http.Transport); !ok || tr.Proxy != nil || tr.DialContext == nil {
		t.Fatalf("nil must become the guarded transport, got %T", GuardRoundTripper(nil, "probe"))
	}
	plain := &http.Transport{MaxIdleConns: 7}
	got, ok := GuardRoundTripper(plain, "probe").(*http.Transport)
	if !ok || got == plain || got.MaxIdleConns != 7 || plain.DialContext != nil {
		t.Fatalf("a plain transport must be cloned with its settings, got %T", got)
	}
	proxied := &http.Transport{Proxy: http.ProxyURL(&url.URL{Scheme: "http", Host: "proxy.test:3128"})}
	if g, ok := GuardRoundTripper(proxied, "probe").(*ResolveGuard); !ok || g.Inner != http.RoundTripper(proxied) {
		t.Fatal("a proxied transport must be wrapped, not replaced")
	}
	inner := &countRT{}
	rt := GuardRoundTripper(inner, "probe")
	for _, target := range []string{"http://127.0.0.1:1/x", "http://[::ffff:169.254.169.254]/x"} {
		req, _ := http.NewRequest(http.MethodGet, target, nil)
		_, err := rt.RoundTrip(req)
		if err == nil || !strings.Contains(err.Error(), "not allowed") {
			t.Fatalf("ResolveGuard let %s through: %v", target, err)
		}
	}
	if inner.calls != 0 {
		t.Fatalf("the inner transport ran %d times for refused targets", inner.calls)
	}
	req, _ := http.NewRequest(http.MethodGet, "http://8.8.8.8/x", nil)
	_, _ = rt.RoundTrip(req)
	if inner.calls != 1 {
		t.Fatal("a public literal must reach the inner transport")
	}
}

func TestSweepDialCheckRefusesUnresolvedAndInternal(t *testing.T) {
	check := dialCheck("probe")
	for _, addr := range []string{"example.com:443", "not-an-ip", "127.0.0.1:80", "[::1]:443"} {
		if err := check("tcp", addr, nil); err == nil {
			t.Errorf("dialCheck accepted %q", addr)
		}
	}
	if err := check("tcp", "8.8.8.8:443", nil); err != nil {
		t.Errorf("dialCheck refused a public address: %v", err)
	}
}

func TestSweepLoopbackAuthorityBracketsWithoutPort(t *testing.T) {
	for _, a := range []string{"[::1]", "[LOCALHOST]"} {
		if !IsLoopbackAuthority(a) {
			t.Errorf("IsLoopbackAuthority(%q) = false", a)
		}
	}
}

func TestSweepOriginWithoutHostRefusedOnHostlessRequest(t *testing.T) {
	r, _ := http.NewRequest(http.MethodPost, "/x", nil)
	r.Host = ""
	for _, o := range []string{"null", "file://", "https://"} {
		r.Header.Set("Origin", o)
		if OriginAllowed(r, nil) {
			t.Errorf("Origin %q admitted on a request with no Host", o)
		}
	}
}
