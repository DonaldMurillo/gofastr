package webhook

import (
	"testing"
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
