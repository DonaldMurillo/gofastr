package netguard

import (
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
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
