package framework

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/localrun"
)

// testCert writes a self-signed loopback certificate with the given
// serial to certFile and keyFile and returns it for a client's pool.
func testCert(t *testing.T, serial int64, certFile, keyFile string) *x509.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(serial),
		Subject:               pkix.Name{CommonName: "localhost"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:              []string{"localhost"},
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IsCA:                  true,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return cert
}

// serveOn starts app on a loopback port and returns the bound address
// and the banner it printed.
func serveOn(t *testing.T, app *App) (string, *bytes.Buffer) {
	t.Helper()
	var banner bytes.Buffer
	app.startupOutput = &banner
	app.Config.DisableSignalHandling = true
	ready := make(chan string, 1)
	app.OnReady(func(addr string) { ready <- addr })
	done := make(chan error, 1)
	go func() { done <- app.Start("127.0.0.1:0") }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = app.Shutdown(ctx)
		<-done
	})
	select {
	case addr := <-ready:
		return addr, &banner
	case err := <-done:
		t.Fatalf("Start: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("server did not start")
	}
	return "", nil
}

// pinnedClient trusts only roots and speaks HTTP/2 when the server offers it.
func pinnedClient(roots ...*x509.Certificate) *http.Client {
	pool := x509.NewCertPool()
	for _, c := range roots {
		pool.AddCert(c)
	}
	tr := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool}, ForceAttemptHTTP2: true, DisableKeepAlives: true}
	return &http.Client{Transport: tr, Timeout: 5 * time.Second}
}

func TestTLSServesHTTPS(t *testing.T) {
	dir := t.TempDir()
	certFile, keyFile := filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	cert := testCert(t, 1, certFile, keyFile)
	addr, banner := serveOn(t, NewApp(WithTLS(TLSConfig{CertFile: certFile, KeyFile: keyFile})))

	resp, err := pinnedClient(cert).Get("https://" + addr + "/healthz")
	if err != nil {
		t.Fatalf("GET over TLS: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d", resp.StatusCode)
	}
	if resp.ProtoMajor != 2 {
		t.Errorf("proto = %s, want HTTP/2 over TLS", resp.Proto)
	}
	if resp.TLS == nil || resp.TLS.Version < tls.VersionTLS12 {
		t.Errorf("TLS state = %+v", resp.TLS)
	}
	if !strings.Contains(banner.String(), "https://"+addr) {
		t.Errorf("banner does not advertise https://%s:\n%s", addr, banner.String())
	}
	if strings.Contains(banner.String(), "http://"+addr) {
		t.Errorf("banner still advertises http://%s", addr)
	}

	plain, err := (&http.Client{Timeout: 5 * time.Second}).Get("http://" + addr + "/healthz")
	if err == nil {
		body, _ := io.ReadAll(plain.Body)
		_ = plain.Body.Close()
		if plain.StatusCode == http.StatusOK {
			t.Errorf("plain HTTP served the app: %s", body)
		}
	}
}

func TestTLSRefusesOldVersions(t *testing.T) {
	dir := t.TempDir()
	certFile, keyFile := filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	cert := testCert(t, 1, certFile, keyFile)
	addr, _ := serveOn(t, NewApp(WithTLS(TLSConfig{CertFile: certFile, KeyFile: keyFile})))

	client := pinnedClient(cert)
	tr := client.Transport.(*http.Transport)
	tr.TLSClientConfig.MaxVersion = tls.VersionTLS11
	tr.TLSClientConfig.MinVersion = tls.VersionTLS10
	if resp, err := client.Get("https://" + addr + "/healthz"); err == nil {
		_ = resp.Body.Close()
		t.Fatal("a TLS 1.1 client completed a handshake")
	}
}

// A Config whose GetConfigForClient hands back a TLS 1.0 config still
// refuses an old client: the callback's config replaces the outer one
// for the handshake, so the minimum has to hold there too.
func TestTLSCallbackConfigKeepsMinimum(t *testing.T) {
	dir := t.TempDir()
	certFile, keyFile := filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	cert := testCert(t, 1, certFile, keyFile)
	pair, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		t.Fatal(err)
	}
	weak := &tls.Config{MinVersion: tls.VersionTLS10, Certificates: []tls.Certificate{pair}}
	addr, _ := serveOn(t, NewApp(WithTLS(TLSConfig{Config: &tls.Config{
		GetConfigForClient: func(*tls.ClientHelloInfo) (*tls.Config, error) { return weak, nil },
	}})))

	client := pinnedClient(cert)
	tr := client.Transport.(*http.Transport)
	tr.TLSClientConfig.MaxVersion = tls.VersionTLS11
	tr.TLSClientConfig.MinVersion = tls.VersionTLS10
	if resp, err := client.Get("https://" + addr + "/healthz"); err == nil {
		_ = resp.Body.Close()
		t.Fatal("a TLS 1.1 client completed a handshake through GetConfigForClient")
	}
	if resp, err := pinnedClient(cert).Get("https://" + addr + "/healthz"); err != nil {
		t.Fatalf("a TLS 1.2+ client was refused: %v", err)
	} else {
		_ = resp.Body.Close()
	}
}

// Every URL the app prints or records for itself names https under TLS:
// a setup link that said http would send its token in the clear.
func TestTLSAppURLsAreHTTPS(t *testing.T) {
	t.Setenv("GOFASTR_SETUP", "")
	t.Setenv("GOFASTR_ROLE", "")
	t.Setenv(localrun.EnvDir, t.TempDir())
	dir := t.TempDir()
	certFile, keyFile := filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	testCert(t, 1, certFile, keyFile)
	app := NewApp(
		WithTLS(TLSConfig{CertFile: certFile, KeyFile: keyFile}),
		WithSetup(&mockSetupRunnerSwappable{incomplete: true}),
	)
	addr, banner := serveOn(t, app)

	if !strings.Contains(banner.String(), "https://"+addr+"/setup") {
		t.Errorf("banner has no https setup link:\n%s", banner)
	}
	recs, err := localrun.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 || !strings.HasPrefix(recs[0].URL, "https://") {
		t.Errorf("local run records = %+v, want one https URL", recs)
	}
}

func TestTLSReloadsRenewedCert(t *testing.T) {
	dir := t.TempDir()
	certFile, keyFile := filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	newCert, newKey := filepath.Join(dir, "new-cert.pem"), filepath.Join(dir, "new-key.pem")
	client := pinnedClient(testCert(t, 1, certFile, keyFile), testCert(t, 2, newCert, newKey))
	addr, _ := serveOn(t, NewApp(WithTLS(TLSConfig{CertFile: certFile, KeyFile: keyFile})))

	serialOf := func() int64 {
		t.Helper()
		resp, err := client.Get("https://" + addr + "/healthz")
		if err != nil {
			t.Fatalf("GET: %v", err)
		}
		_ = resp.Body.Close()
		return resp.TLS.PeerCertificates[0].SerialNumber.Int64()
	}
	if got := serialOf(); got != 1 {
		t.Fatalf("first serial = %d, want 1", got)
	}
	// Renew the way certbot does: new bytes at the same paths.
	later := time.Now().Add(2 * time.Second)
	for from, to := range map[string]string{newCert: certFile, newKey: keyFile} {
		b, err := os.ReadFile(from)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(to, b, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(to, later, later); err != nil {
			t.Fatal(err)
		}
	}
	if got := serialOf(); got != 2 {
		t.Errorf("serial after renewal = %d, want 2", got)
	}
}

func TestTLSConfigRefused(t *testing.T) {
	dir := t.TempDir()
	certFile, keyFile := filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	testCert(t, 1, certFile, keyFile)
	pair, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		t.Fatal(err)
	}
	certs := []tls.Certificate{pair}
	// Each case names the refusal it expects, so a case cannot pass on a
	// different check that happens to run first.
	for name, tc := range map[string]struct {
		cfg  TLSConfig
		want string
	}{
		"nothing":               {TLSConfig{}, "exactly one"},
		"cert without key":      {TLSConfig{CertFile: certFile}, "both CertFile and KeyFile"},
		"files and autocert":    {TLSConfig{CertFile: certFile, KeyFile: keyFile, AutoCert: &AutoCertConfig{Domains: []string{"example.com"}, CacheDir: dir}}, "exactly one"},
		"files and config":      {TLSConfig{CertFile: certFile, KeyFile: keyFile, Config: &tls.Config{}}, "exactly one"},
		"autocert no domain":    {TLSConfig{AutoCert: &AutoCertConfig{CacheDir: dir}}, "needs Domains"},
		"autocert empty domain": {TLSConfig{AutoCert: &AutoCertConfig{Domains: []string{"example.com", ""}, CacheDir: dir}}, "empty domain"},
		"autocert no cache":     {TLSConfig{AutoCert: &AutoCertConfig{Domains: []string{"example.com"}}}, "needs CacheDir"},
		"config tls 1.0":        {TLSConfig{Config: &tls.Config{MinVersion: tls.VersionTLS10, Certificates: certs}}, "below TLS 1.2"},
		"config no cert":        {TLSConfig{Config: &tls.Config{}}, "no certificate"},
		"missing cert file":     {TLSConfig{CertFile: filepath.Join(dir, "nope.pem"), KeyFile: keyFile}, "TLS certificate"},
	} {
		t.Run(name, func(t *testing.T) {
			app := NewApp(WithTLS(tc.cfg))
			app.startupOutput = io.Discard
			app.Config.DisableSignalHandling = true
			done := make(chan error, 1)
			go func() { done <- app.Start("127.0.0.1:0") }()
			select {
			case err := <-done:
				if err == nil || !strings.Contains(err.Error(), tc.want) {
					t.Errorf("Start = %v, want an error containing %q", err, tc.want)
				}
			case <-time.After(5 * time.Second):
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				_ = app.Shutdown(ctx)
				t.Error("Start served with a bad TLS config")
			}
		})
	}
}

func TestHTTPProtocolsServesH2C(t *testing.T) {
	var protos http.Protocols
	protos.SetHTTP1(true)
	protos.SetUnencryptedHTTP2(true)
	addr, _ := serveOn(t, NewApp(WithHTTPProtocols(protos)))

	var client http.Protocols
	client.SetUnencryptedHTTP2(true)
	c := &http.Client{Transport: &http.Transport{Protocols: &client}, Timeout: 5 * time.Second}
	resp, err := c.Get("http://" + addr + "/healthz")
	if err != nil {
		t.Fatalf("h2c GET: %v", err)
	}
	_ = resp.Body.Close()
	if resp.ProtoMajor != 2 {
		t.Errorf("proto = %s, want HTTP/2 with prior knowledge", resp.Proto)
	}
}

func TestDefaultServerIsHTTP1Only(t *testing.T) {
	addr, _ := serveOn(t, NewApp())
	var client http.Protocols
	client.SetUnencryptedHTTP2(true)
	c := &http.Client{Transport: &http.Transport{Protocols: &client}, Timeout: 5 * time.Second}
	if resp, err := c.Get("http://" + addr + "/healthz"); err == nil {
		_ = resp.Body.Close()
		if resp.ProtoMajor == 2 {
			t.Error("the default server spoke h2c")
		}
	}
}
