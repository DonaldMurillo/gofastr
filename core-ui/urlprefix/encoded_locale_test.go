package urlprefix

import "testing"

// The router decodes the path before it splits the locale off, so
// /%65s/about and /es%2Fpricing are the Spanish page. The rewrite
// compared the raw segment and gave it a second prefix, /es/%65s/about;
// an escape that decodes to the separator needs the same re-split.
func TestEncodedLocaleNotPrefixedTwice(t *testing.T) {
	rule := Rule{Prefix: "/es", Locales: []string{"en", "es"}}
	for _, u := range []string{
		"/%65s/about", "/%65n/about", "/%65s", "/__%67ofastr/runtime.js",
		"/es%2Fpricing", "/es%2Fpricing?q=1",
	} {
		if got := Path(u, rule); got != u {
			t.Errorf("Path(%q) = %q, want it unchanged", u, got)
		}
	}
	if got := Path("/about%2Fx", rule); got != "/es/about%2Fx" {
		t.Errorf("Path(/about%%2Fx) = %q, want /es/about%%2Fx", got)
	}
}
