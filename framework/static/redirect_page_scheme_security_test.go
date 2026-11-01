package static

import (
	"context"
	"strings"
	"testing"
)

// TestRedirectPageRefusesUnsafeScheme pins that an exported redirect
// page never makes a script or data URL live: the target lands in a
// meta refresh, a canonical link and an anchor, and HTML escaping alone
// leaves javascript: intact.
func TestRedirectPageRefusesUnsafeScheme(t *testing.T) {
	for _, href := range []string{"javascript:alert(1)", "JavaScript:alert(1)", "data:text/html,x", "vbscript:x"} {
		page := strings.ToLower(redirectPage(context.Background(), href, ""))
		for _, bad := range []string{"javascript:", "data:", "vbscript:"} {
			if strings.Contains(page, bad) {
				t.Fatalf("redirect page for %q carries %q:\n%s", href, bad, page)
			}
		}
	}
	page := redirectPage(context.Background(), "/new/path", "")
	if !strings.Contains(page, `href="/new/path"`) || !strings.Contains(page, `url=/new/path"`) {
		t.Fatalf("a safe target lost its href:\n%s", page)
	}
}
