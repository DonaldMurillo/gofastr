package embed

import "testing"

func TestNormalizeOriginCanonicalizes(t *testing.T) {
	// Every input on the left is the SAME origin written differently. If any
	// of these stopped normalizing to one string, a customer's trailing slash
	// or capitalized host would silently never match its own allowlist entry.
	cases := map[string]string{
		"https://acme.com":      "https://acme.com",
		"https://acme.com/":     "https://acme.com",
		"https://ACME.com":      "https://acme.com",
		"HTTPS://Acme.Com/":     "https://acme.com",
		"https://acme.com:443":  "https://acme.com",
		"http://acme.com:80":    "http://acme.com",
		"https://acme.com:8443": "https://acme.com:8443",
		"http://localhost:3000": "http://localhost:3000",
		"https://[::1]:8443":    "https://[::1]:8443",
		"https://[::1]":         "https://[::1]",
	}
	for in, want := range cases {
		got, err := NormalizeOrigin(in)
		if err != nil {
			t.Errorf("NormalizeOrigin(%q): unexpected error %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("NormalizeOrigin(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeOriginRejectsNonOrigins(t *testing.T) {
	bad := []string{
		"",
		"   ",
		"acme.com",                   // no scheme
		"//acme.com",                 // scheme-relative
		"ftp://acme.com",             // wrong scheme
		"https://",                   // no host
		"https://*.acme.com",         // wildcards are not a spelling we accept
		"*",                          //
		"https://user@acme.com",      // userinfo
		"https://user:pw@acme.com",   //
		"https://acme.com/dashboard", // path
		"https://acme.com/?a=1",      // query
		"https://acme.com#frag",      // fragment
		"https:acme.com",             // opaque
	}
	for _, in := range bad {
		if got, err := NormalizeOrigin(in); err == nil {
			t.Errorf("NormalizeOrigin(%q) = %q, want an error", in, got)
		}
	}
}

func TestOriginSetRequiresAtLeastOne(t *testing.T) {
	if _, err := newOriginSet(nil); err == nil {
		t.Fatal("an empty allowlist must be an error, never allow-everything")
	}
}

func TestOriginSetMatchesNormalized(t *testing.T) {
	s, err := newOriginSet([]string{"https://acme.com/", "https://ACME.com:443", "http://localhost:3000"})
	if err != nil {
		t.Fatalf("newOriginSet: %v", err)
	}
	if got := s.List(); len(got) != 2 {
		t.Fatalf("duplicate spellings must collapse: got %v", got)
	}
	for _, ok := range []string{"https://acme.com", "https://acme.com/", "https://ACME.COM", "http://localhost:3000/"} {
		if !s.Has(ok) {
			t.Errorf("Has(%q) = false, want true", ok)
		}
	}
	for _, no := range []string{"http://acme.com", "https://evil.com", "https://acme.com.evil.com", "https://sub.acme.com", "garbage"} {
		if s.Has(no) {
			t.Errorf("Has(%q) = true, want false", no)
		}
	}
}

// An allowlist entry has to match what the BROWSER sends, and the browser
// serializes origins canonically. An entry the browser can never produce is not
// a security hole — nothing matches it — but it is a config that silently never
// completes a handshake, which is worse to debug than an error at boot.
func TestNormalizeOriginMatchesBrowserSerialization(t *testing.T) {
	// Verified against Chromium's new URL(x).origin.
	same := map[string]string{
		"https://[0:0:0:0:0:0:0:1]":      "https://[::1]",
		"https://[::1]":                  "https://[::1]",
		"https://[0:0:0:0:0:0:0:1]:8443": "https://[::1]:8443",
		"https://acme.com:0443":          "https://acme.com",
		"https://acme.com:00080":         "https://acme.com:80",
		"http://acme.com:0080":           "http://acme.com",
	}
	for in, want := range same {
		got, err := NormalizeOrigin(in)
		if err != nil {
			t.Errorf("NormalizeOrigin(%q): %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("NormalizeOrigin(%q) = %q, want %q (what the browser sends)", in, got, want)
		}
	}

	// Numeric hosts the browser reads as IPv4 but Go reads as DNS labels. We
	// cannot reproduce the browser's serialization for them, so refuse rather
	// than accept an entry that can never match.
	for _, in := range []string{
		"https://127.1",
		"https://2130706433",
		"https://0177.0.0.1",
		"https://acme.com:0",
		"https://acme.com:65536",
		"https://acme.com:99999",
	} {
		if got, err := NormalizeOrigin(in); err == nil {
			t.Errorf("NormalizeOrigin(%q) = %q, want an error — the browser would send something else", in, got)
		}
	}

	// A real dotted-quad still works.
	if got, err := NormalizeOrigin("http://127.0.0.1:8088"); err != nil || got != "http://127.0.0.1:8088" {
		t.Errorf("NormalizeOrigin(dotted quad) = %q, %v", got, err)
	}
}
