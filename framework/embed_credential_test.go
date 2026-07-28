package framework

import (
	"net/http"
	"net/http/httptest"
	"testing"

	fembed "github.com/DonaldMurillo/gofastr/framework/embed"
)

// Cookie arrives as SEVERAL header fields in ordinary deployments: a proxy
// prepends its own and forwards the browser's as a second. Header.Get returns
// only the first, so two users authenticating from their distinct later fields
// shared one idempotency namespace — and the second caller received the first's
// stored response while its own handler never ran.
func TestCredentialFingerprintReadsEveryCookieField(t *testing.T) {
	alice := httptest.NewRequest(http.MethodPost, "/orders", nil)
	alice.Header.Add("Cookie", "edge=blue")
	alice.Header.Add("Cookie", "session_id=alice")

	bob := httptest.NewRequest(http.MethodPost, "/orders", nil)
	bob.Header.Add("Cookie", "edge=blue")
	bob.Header.Add("Cookie", "session_id=bob")

	if a, b := credentialFingerprint(alice), credentialFingerprint(bob); a == b {
		t.Fatalf("two users behind one proxy share the namespace %q — "+
			"only the shared first Cookie field was hashed", a)
	}
}

// The same request must always land in the same namespace, or idempotency
// stops working at all.
func TestCredentialFingerprintIsStable(t *testing.T) {
	mk := func() *http.Request {
		r := httptest.NewRequest(http.MethodPost, "/orders", nil)
		r.Header.Add("Cookie", "edge=blue")
		r.Header.Add("Cookie", "session_id=alice")
		r.Header.Set("Authorization", "Bearer t")
		return r
	}
	if a, b := credentialFingerprint(mk()), credentialFingerprint(mk()); a != b {
		t.Fatalf("fingerprint is not stable: %q vs %q", a, b)
	}
}

// Distinct credential kinds are distinct namespaces, and no credential at all
// is distinguishable from every real one.
func TestCredentialFingerprintSeparatesCredentialKinds(t *testing.T) {
	seen := map[string]string{}
	for name, set := range map[string]func(*http.Request){
		"none":   func(*http.Request) {},
		"cookie": func(r *http.Request) { r.Header.Set("Cookie", "session_id=a") },
		"bearer": func(r *http.Request) { r.Header.Set("Authorization", "Bearer a") },
		"apikey": func(r *http.Request) { r.Header.Set("X-API-Key", "a") },
		"grant":  func(r *http.Request) { r.Header.Set(fembed.GrantHeader, "a") },
	} {
		r := httptest.NewRequest(http.MethodPost, "/orders", nil)
		set(r)
		got := credentialFingerprint(r)
		if prev, dup := seen[got]; dup {
			t.Fatalf("%q and %q share namespace %q", name, prev, got)
		}
		seen[got] = name
	}
}
