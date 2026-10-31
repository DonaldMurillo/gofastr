package webbotauth

import "testing"

// @authority drops a default port (RFC 9421 §2.2.3), and an IPv6
// literal keeps its brackets with or without one: "[2001:db8::1]:443"
// came out bare, so a signer's "[2001:db8::1]" never verified.
func TestAuthorityKeepsIPv6Brackets(t *testing.T) {
	for _, c := range []struct{ host, scheme, want string }{
		{"[2001:db8::1]:443", "https", "[2001:db8::1]"},
		{"[2001:DB8::1]:80", "http", "[2001:db8::1]"},
		{"[2001:db8::1]:8443", "https", "[2001:db8::1]:8443"},
		{"[2001:db8::1]", "https", "[2001:db8::1]"},
		{"Example.COM:443", "https", "example.com"},
		{"example.com:8080", "http", "example.com:8080"},
	} {
		if got := normalizeAuthority(c.host, c.scheme); got != c.want {
			t.Errorf("normalizeAuthority(%q, %q) = %q, want %q", c.host, c.scheme, got, c.want)
		}
	}
}
