package webhook

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestSSRF_LocalhostRejected verifies that localhost URLs are rejected.
// Attack: SSRF via webhook to http://localhost:8080/admin.
func TestSSRF_LocalhostRejected(t *testing.T) {
	for _, url := range []string{
		"http://localhost:8080/admin",
		"http://localhost/secret",
		"http://127.0.0.1:9090/metrics",
		"http://[::1]:8080/debug",
	} {
		err := validateSubscriberURL(url, false)
		if err == nil {
			t.Errorf("SECURITY: [ssrf] validateSubscriberURL accepted %q. Attack: SSRF to localhost.", url)
		}
	}
}

// TestSSRF_PrivateIPsRejected verifies that RFC1918 IPs are rejected.
// Attack: SSRF to internal services at 10.x, 172.16.x, 192.168.x.
func TestSSRF_PrivateIPsRejected(t *testing.T) {
	for _, url := range []string{
		"http://10.0.0.1/admin",
		"http://172.16.0.1/internal",
		"http://192.168.1.1/router",
		"http://10.255.255.255/test",
	} {
		err := validateSubscriberURL(url, false)
		if err == nil {
			t.Errorf("SECURITY: [ssrf] validateSubscriberURL accepted %q. Attack: SSRF to RFC1918 private IP.", url)
		}
	}
}

// TestSSRF_CloudMetadataRejected verifies that cloud metadata endpoints
// are rejected. Attack: SSRF to AWS/GCP metadata at 169.254.169.254.
func TestSSRF_CloudMetadataRejected(t *testing.T) {
	for _, url := range []string{
		"http://169.254.169.254/latest/meta-data/",
		"http://169.254.169.254/computeMetadata/v1/",
		"http://metadata.google.internal/computeMetadata/v1/",
	} {
		err := validateSubscriberURL(url, false)
		if err == nil {
			t.Errorf("SECURITY: [ssrf] validateSubscriberURL accepted %q. Attack: SSRF to cloud metadata endpoint.", url)
		}
	}
}

// TestSSRF_LinkLocalRejected verifies that link-local IPs are rejected.
// Attack: SSRF to link-local IPs for internal network discovery.
func TestSSRF_LinkLocalRejected(t *testing.T) {
	err := validateSubscriberURL("http://169.254.1.1/internal", false)
	if err == nil {
		t.Errorf("SECURITY: [ssrf] validateSubscriberURL accepted link-local IP. Attack: SSRF via link-local network discovery.")
	}
}

// TestSSRF_UnsupportedSchemesRejected verifies that non-HTTP(S) schemes
// are rejected. Attack: SSRF via file://, gopher://, dict:// protocols.
func TestSSRF_UnsupportedSchemesRejected(t *testing.T) {
	for _, url := range []string{
		"file:///etc/passwd",
		"gopher://internal:7070/_DELETE",
		"dict://internal:2628/get:secret",
		"ftp://internal/secret.txt",
		"ldap://internal/dc=example,dc=com",
	} {
		err := validateSubscriberURL(url, false)
		if err == nil {
			t.Errorf("SECURITY: [ssrf] validateSubscriberURL accepted scheme in %q. Attack: SSRF via non-HTTP protocol.", url)
		}
	}
}

// TestSSRF_EmptyURLRejected verifies that empty URLs are rejected.
func TestSSRF_EmptyURLRejected(t *testing.T) {
	err := validateSubscriberURL("", false)
	if err == nil {
		t.Errorf("SECURITY: [ssrf] validateSubscriberURL accepted empty URL.")
	}
}

// TestSSRF_MissingHostRejected verifies that URLs without a host are
// rejected. Attack: malformed URL bypass.
func TestSSRF_MissingHostRejected(t *testing.T) {
	err := validateSubscriberURL("http://", false)
	if err == nil {
		t.Errorf("SECURITY: [ssrf] validateSubscriberURL accepted URL without host.")
	}
}

// TestSSRF_PublicURLAccepted verifies that legitimate public URLs pass
// validation. This is a negative control.
func TestSSRF_PublicURLAccepted(t *testing.T) {
	for _, url := range []string{
		"https://api.stripe.com/webhooks",
		"https://hooks.slack.com/services/xxx",
		"https://example.com/webhook",
	} {
		err := validateSubscriberURL(url, false)
		if err != nil {
			t.Errorf("validateSubscriberURL rejected legitimate public URL %q: %v", url, err)
		}
	}
}

// TestSSRF_AllowPrivateOverrides verifies that allowPrivate=true skips
// the host check. Used by test helpers.
func TestSSRF_AllowPrivateOverrides(t *testing.T) {
	err := validateSubscriberURL("http://localhost:8080/test", true)
	if err != nil {
		t.Errorf("validateSubscriberURL with allowPrivate=true rejected localhost: %v", err)
	}
}

// TestSSRF_AllowPrivateStillEnforcesScheme verifies that allowPrivate=true
// still rejects non-HTTP schemes. Attack: bypassing scheme check via
// allowPrivate flag.
func TestSSRF_AllowPrivateStillEnforcesScheme(t *testing.T) {
	err := validateSubscriberURL("file:///etc/passwd", true)
	if err == nil {
		t.Errorf("SECURITY: [ssrf] validateSubscriberURL with allowPrivate=true accepted file:// scheme. Attack: SSRF via non-HTTP protocol even with private allowed.")
	}
}

// TestSSRF_DialTimeRejectsInternalIP verifies the delivery client refuses
// connections whose RESOLVED address is internal — closing the DNS
// rebinding / TOCTOU window where a host validates public at Subscribe()
// then re-points DNS to 169.254.169.254 / 127.0.0.1 / RFC1918 before the
// worker dials. Registration-time validation alone cannot catch this.
//
// Attack: register evil.example.com (resolves public), then rebind DNS to
// loopback so the worker dials an internal service.
func TestSSRF_DialTimeRejectsInternalIP(t *testing.T) {
	// The default delivery client (no caller-supplied HTTPClient) must
	// carry a Dialer.Control hook that re-checks the resolved address.
	mgr := New(NewMemoryStore(), Options{})
	tr, ok := mgr.opts.HTTPClient.Transport.(*http.Transport)
	if !ok || tr == nil {
		t.Fatalf("SECURITY: [ssrf] delivery client has no *http.Transport (cannot install dial-time SSRF guard): %T", mgr.opts.HTTPClient.Transport)
	}
	if tr.DialContext == nil {
		t.Fatalf("SECURITY: [ssrf] delivery transport has no DialContext — dial-time SSRF guard absent")
	}

	// A real loopback receiver. With the dial-time guard the connection
	// must be refused even though the server is live; the resolved address
	// is 127.0.0.1.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "http://")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, err := tr.DialContext(ctx, "tcp", host)
	if conn != nil {
		_ = conn.Close()
	}
	if err == nil {
		t.Fatalf("SECURITY: [ssrf] dial to internal/loopback %s succeeded; dial-time SSRF guard missing. Attack: DNS rebinding to localhost.", host)
	}
}

// TestSSRF_DialTimeAllowsPublicIP is the negative control: the dial-time
// guard must NOT block legitimate public addresses.
func TestSSRF_DialTimeAllowsPublicIP(t *testing.T) {
	mgr := New(NewMemoryStore(), Options{})
	tr := mgr.opts.HTTPClient.Transport.(*http.Transport)

	// Don't actually open a socket to the internet; assert the Control
	// callback accepts a public resolved address and rejects an internal
	// one. We reach the control via the dialer indirectly: dial a public
	// TEST-NET address that is non-routable so the connection attempt
	// fails at the network layer, NOT at the guard. A guard rejection
	// would mention "not allowed"; a network failure won't.
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	_, err := tr.DialContext(ctx, "tcp", "203.0.113.10:9") // TEST-NET-3, non-routable
	if err != nil && strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("dial-time guard wrongly rejected public address: %v", err)
	}
}

// TestSSRF_DialTimeHonorsAllowPrivate verifies that when
// AllowPrivateNetworks is set the dial-time guard is disabled (dev/test
// posture), so loopback receivers work.
func TestSSRF_DialTimeHonorsAllowPrivate(t *testing.T) {
	mgr := New(NewMemoryStore(), Options{AllowPrivateNetworks: true})
	tr := mgr.opts.HTTPClient.Transport.(*http.Transport)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "http://")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, err := tr.DialContext(ctx, "tcp", host)
	if err != nil {
		t.Fatalf("AllowPrivateNetworks=true should permit loopback dial, got %v", err)
	}
	if conn != nil {
		_ = conn.Close()
	}
}
