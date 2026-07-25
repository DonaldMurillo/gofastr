package chromepdf

import (
	"strings"
	"testing"
)

// TestPDFRendererRefusesNetworkByDefault pins the network half of the
// print SSRF gate at the configuration layer, so it holds whether or not
// a Chromium binary is present (the browser-driven proof is the
// chromium-tagged test below).
//
// Attack: the renderer navigated headless Chrome to a
// `data:text/html;base64,…` document with no resolver rules, no proxy
// and no request interception. First-party components put caller strings
// into URL attributes, so a PDF rendering a user-supplied avatar URL
// fetched it from inside the trust boundary — and the response bytes are
// rendered into the downloaded PDF, making it a readable exfil channel
// for 169.254.169.254, loopback admin panels and RFC1918.
func TestPDFRendererRefusesNetworkByDefault(t *testing.T) {
	rules := hostResolverRules(nil)
	if !strings.HasPrefix(rules, "MAP * ~NOTFOUND") {
		t.Fatalf("SECURITY: [ssrf] the default renderer does not block name resolution; rules = %q", rules)
	}
	if strings.Contains(rules, "EXCLUDE") {
		t.Errorf("SECURITY: [ssrf] the default renderer excludes a host from the block: %q", rules)
	}
}

// An explicit allow-list is the documented escape hatch — it must permit
// exactly the named hosts and nothing more.
func TestPDFRendererAllowListIsExact(t *testing.T) {
	rules := hostResolverRules([]string{"cdn.example.com", "  ", "assets.example.com"})
	if !strings.HasPrefix(rules, "MAP * ~NOTFOUND") {
		t.Fatalf("allow-list dropped the catch-all block: %q", rules)
	}
	for _, want := range []string{"EXCLUDE cdn.example.com", "EXCLUDE assets.example.com"} {
		if !strings.Contains(rules, want) {
			t.Errorf("allow-listed host missing from resolver rules: %q not in %q", want, rules)
		}
	}
	if strings.Count(rules, "EXCLUDE") != 2 {
		t.Errorf("blank entries must not become rules: %q", rules)
	}
}
