package webbotauth

// helpers_test.go: shared TLS directory-server scaffolding. Tests dial
// loopback servers through a transport that maps any hostname onto the
// server and trusts its test certificate - environment substitution for
// DNS + PKI, mirroring how a real verifier reaches the agent's origin.
// The SSRF posture itself is what directory_test.go exercises.

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// testTLSServer is a mutable HTTPS test server.
type testTLSServer struct {
	srv  *httptest.Server
	pool *x509.CertPool

	mu      sync.Mutex
	handler http.HandlerFunc
}

func (s *testTLSServer) setHandler(h http.HandlerFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handler = h
}

func (s *testTLSServer) serve(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	h := s.handler
	s.mu.Unlock()
	if h == nil {
		http.NotFound(w, r)
		return
	}
	h(w, r)
}

// startTLSServer starts an HTTPS server whose certificate covers the
// single-label *.test names used in tests plus signature-agent.test.
func startTLSServer(t *testing.T, h http.HandlerFunc) *testTLSServer {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "*.test"},
		DNSNames:     []string{"*.test", "test", "signature-agent.test", "localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, pub, priv)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(cert)

	s := &testTLSServer{pool: pool, handler: h}
	s.srv = httptest.NewUnstartedServer(http.HandlerFunc(s.serve))
	s.srv.TLS = &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: priv}}}
	s.srv.StartTLS()
	t.Cleanup(s.srv.Close)
	return s
}

// resolverForServer builds a resolver whose fetches all land on srv.
func resolverForServer(t *testing.T, srv *testTLSServer) *directoryResolver {
	t.Helper()
	tr := guardedTransport(true) // tests dial loopback by design
	tr.TLSClientConfig = &tls.Config{RootCAs: srv.pool}
	addr := srv.srv.Listener.Addr().String()
	tr.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, network, addr)
	}
	res := newDirectoryResolver(nil)
	res.client = &http.Client{
		Transport: tr,
		// draft section 5.5: never follow redirects.
		CheckRedirect: func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse },
	}
	return res
}
