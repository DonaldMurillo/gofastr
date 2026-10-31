package netguard

import (
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// A transport that speaks HTTP/2 on its own still speaks it once the
// guarded dialer is installed. Clone() of a zero-value transport offers h2
// in its TLS handshake, and a custom DialContext switches off the
// automatic HTTP/2 setup, so the clone wrote HTTP/1.1 onto a connection
// the server had agreed would carry HTTP/2.
func TestGuardPlainKeepsHTTP2(t *testing.T) {
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, r.Proto)
	}))
	srv.EnableHTTP2 = true
	srv.StartTLS()
	defer srv.Close()

	guarded, ok := withDialer(&http.Transport{}, (&net.Dialer{}).DialContext)
	if !ok {
		t.Fatal("plain transport refused")
	}
	guarded.TLSClientConfig.InsecureSkipVerify = true // the test server's own certificate
	resp, err := (&http.Client{Transport: guarded}).Get(srv.URL)
	if err != nil {
		t.Fatalf("GET over the guarded clone: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if string(body) != "HTTP/2.0" {
		t.Errorf("server saw %q, want HTTP/2.0", body)
	}
}

// A transport that routes on its own (a proxy, or any dial hook,
// deprecated ones included) is left alone: swapping its dialer would
// change where it connects. The caller wraps it instead.
func TestGuardPlainRefusesCustomDialers(t *testing.T) {
	dial := func(context.Context, string, string) (net.Conn, error) { return nil, nil }
	plainDial := func(string, string) (net.Conn, error) { return nil, nil }
	for name, tr := range map[string]*http.Transport{
		"proxy":          {Proxy: http.ProxyURL(&url.URL{Scheme: "http", Host: "proxy.internal:3128"})},
		"DialContext":    {DialContext: dial},
		"DialTLSContext": {DialTLSContext: dial},
		"Dial":           {Dial: plainDial},
		"DialTLS":        {DialTLS: plainDial},
	} {
		if guarded, ok := GuardPlainTransport(tr, "test"); ok || guarded != nil {
			t.Errorf("%s: transport guarded in place, want it refused", name)
		}
	}
}

// http.DefaultTransport.Clone() is the usual way to tune a client, and
// it carries ProxyFromEnvironment and a stock net.Dialer: it is plain,
// and its clone dials through the guard. It used to fall back to the
// resolve-then-dial check, which DNS rebinding passes (2026-10-27 B64,
// P54).
func TestGuardPlainTakesDefaultClone(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.MaxIdleConns = 7
	guarded, ok := GuardPlainTransport(tr, "test")
	if !ok {
		t.Fatal("DefaultTransport.Clone() refused as a custom dialer")
	}
	if guarded.Proxy != nil || guarded.MaxIdleConns != 7 {
		t.Errorf("clone: Proxy set %v, MaxIdleConns %d; want no proxy and the tuning kept", guarded.Proxy != nil, guarded.MaxIdleConns)
	}
	resp, err := (&http.Client{Transport: guarded}).Get(srv.URL)
	if err == nil {
		_ = resp.Body.Close()
		t.Fatal("the guarded clone reached a loopback server")
	}
}

// A net.Dialer the caller built is a custom dialer even though its
// DialContext shares the stock method's code: swapping it for the guard
// would drop its Resolver, Control and LocalAddr (split-horizon DNS, a
// pinned egress interface). Only the DefaultTransport's own dialer is
// stock (2026-10-28 review of the B64 fix).
func TestGuardPlainRefusesOwnNetDialer(t *testing.T) {
	for name, d := range map[string]*net.Dialer{
		"resolver":  {Resolver: &net.Resolver{PreferGo: true}},
		"localaddr": {LocalAddr: &net.TCPAddr{IP: net.IPv4(10, 0, 0, 5)}},
		"plain":     {},
	} {
		if _, ok := GuardPlainTransport(&http.Transport{DialContext: d.DialContext}, "test"); ok {
			t.Errorf("%s: a caller's own net.Dialer was swapped for the guard", name)
		}
	}
}

// The clone of a plain transport dials through the guard: a loopback
// address is refused at connect time.
func TestGuardPlainBlocksLoopback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()
	guarded, ok := GuardPlainTransport(&http.Transport{}, "test")
	if !ok {
		t.Fatal("plain transport refused")
	}
	resp, err := (&http.Client{Transport: guarded}).Get(srv.URL)
	if err == nil {
		_ = resp.Body.Close()
		t.Fatal("the guarded clone reached a loopback server")
	}
}

// A caller's TLS config with no ALPN list makes an HTTP/1.1 transport,
// and the clone stays one.
func TestGuardPlainKeepsHTTP1Config(t *testing.T) {
	tr := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}}
	guarded, ok := withDialer(tr, (&net.Dialer{}).DialContext)
	if !ok {
		t.Fatal("plain transport refused")
	}
	if guarded.ForceAttemptHTTP2 {
		t.Error("an HTTP/1.1 transport was switched to HTTP/2")
	}
}
