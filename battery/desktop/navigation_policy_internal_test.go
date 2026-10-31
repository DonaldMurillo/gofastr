package desktop

import "testing"

// OnStop clears the bound address while a window can still be up, and
// a URL with an empty host ("http:///x") must not match the cleared
// address.
func TestStoppedBatteryRefusesEmptyHost(t *testing.T) {
	b, _ := newTestBattery(t)
	for _, raw := range []string{"http:///x", "http:/x", "http://"} {
		if b.allowNavigation(raw) {
			t.Errorf("allowNavigation(%q) with no bound address = true", raw)
		}
	}
}

// The refusal log names the host only: the query can carry an OAuth
// code. url.Parse refuses ASCII controls in a host, but not a C1
// control like U+0085, which the scrub encodes.
func TestNavigationHostDropsPathAndQuery(t *testing.T) {
	for raw, want := range map[string]string{
		"https://evil.example/cb?code=s3cret#x": "evil.example",
		"http://127.0.0.1:9/notes":              "127.0.0.1:9",
		"http://a\u0085b.example/":              "a%c2%85b.example",
		"http://a%0Ab.example/":                 "",
		"data:text/html,<p>x</p>":               "",
		"http://[::1":                           "",
	} {
		if got := NavigationHost(raw); got != want {
			t.Errorf("NavigationHost(%q) = %q, want %q", raw, got, want)
		}
	}
}
