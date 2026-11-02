package harnesscmd

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/ids"
)

// TestChatPageRefusesFraming pins that the harness chat page cannot be
// framed. The page holds a 24 h human-class token and the permission
// modal whose "Allow for session" button grants every tool for the
// rest of the session. The Host guard stops a DNS-rebound page from
// READING it, but a page on any site can still load it in an iframe
// (a navigation carries the loopback Host and no Origin) and overlay
// its own content to steer the operator's click onto that button
// while a prompt is open. Both the legacy X-Frame-Options and CSP
// frame-ancestors refuse that.
func TestChatPageRefusesFraming(t *testing.T) {
	for _, path := range []string{"/", "/endpoints"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		chatPage(rec, req, ids.NewSessionID(), "frame-test-token") // not-a-secret: synthetic fixture

		if rec.Code != http.StatusOK {
			t.Fatalf("%s: setup broken: status %d", path, rec.Code)
		}
		if got := rec.Header().Get("X-Frame-Options"); got != "DENY" {
			t.Errorf("%s: X-Frame-Options = %q, want DENY", path, got)
		}
		if csp := rec.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "frame-ancestors 'none'") {
			t.Errorf("%s: Content-Security-Policy = %q, want frame-ancestors 'none'", path, csp)
		}
	}
}
