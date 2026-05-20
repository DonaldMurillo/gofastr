package webhook

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestValidateSubscriberURL_RejectsLoopbackAndPrivate(t *testing.T) {
	cases := []string{
		"http://127.0.0.1/x",
		"http://localhost/x",
		"http://[::1]/x",
		"http://10.0.0.5/x",
		"http://192.168.1.1/x",
		"http://172.16.5.5/x",
		"http://169.254.169.254/latest/meta-data/iam/security-credentials/",
		"http://metadata.google.internal/computeMetadata/v1/instance/",
		"http://0.0.0.0/x",
	}
	for _, raw := range cases {
		if err := validateSubscriberURL(raw, false); err == nil {
			t.Errorf("expected SSRF rejection for %q", raw)
		}
	}
}

func TestValidateSubscriberURL_RejectsNonHTTPSchemes(t *testing.T) {
	cases := []string{
		"file:///etc/passwd",
		"gopher://example.com/x",
		"ftp://example.com/x",
		"javascript:alert(1)",
		"",
	}
	for _, raw := range cases {
		if err := validateSubscriberURL(raw, false); err == nil {
			t.Errorf("expected non-http scheme rejection for %q", raw)
		}
	}
}

func TestValidateSubscriberURL_AllowsPublicHTTPandHTTPS(t *testing.T) {
	cases := []string{
		"https://api.example.com/hooks",
		"http://203.0.113.10/hook", // documentation block — treated as public
		"https://example.com:8443/x",
	}
	for _, raw := range cases {
		if err := validateSubscriberURL(raw, false); err != nil {
			t.Errorf("public URL rejected: %q: %v", raw, err)
		}
	}
}

func TestValidateSubscriberURL_AllowPrivateLetsLoopbackThrough(t *testing.T) {
	// When AllowPrivateNetworks is opted-in (dev/test), loopback is fine
	// but the scheme guard still rejects non-http(s).
	if err := validateSubscriberURL("http://127.0.0.1:9999/x", true); err != nil {
		t.Fatalf("private allowed: should accept loopback, got %v", err)
	}
	if err := validateSubscriberURL("file:///etc/passwd", true); err == nil {
		t.Fatalf("private allowed must still reject file:// scheme")
	}
}

func TestSubscribe_RejectsSSRFURL(t *testing.T) {
	mgr := New(NewMemoryStore(), Options{
		MaxAttempts:  1,
		Backoff:      []time.Duration{0},
		PollInterval: 5 * time.Millisecond,
	})
	_, err := mgr.Subscribe(context.Background(), Subscriber{
		URL:    "http://169.254.169.254/x",
		Secret: "shh",
	})
	if err == nil {
		t.Fatalf("expected SSRF rejection on subscribe, got nil")
	}
	if !strings.Contains(err.Error(), "169.254") && !strings.Contains(err.Error(), "metadata") {
		t.Fatalf("expected SSRF rejection mentioning the bad host, got %v", err)
	}
}
