package harnesscmd

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/ids"
)

func TestChatPageRedNoStore(t *testing.T) {
	sess := ids.NewSessionID()
	const token = "red-harness-token-canary" // not-a-secret: synthetic red-test fixture asserting the page embeds a token (any string works)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	chatPage(rec, req, sess, token)

	if rec.Code != http.StatusOK {
		t.Fatalf("setup broken: status %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), token) {
		t.Fatalf("setup broken: page does not embed the token — harness shape changed")
	}
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "no-store") {
		t.Errorf("SECURITY: [harness-chatpage-nostore] the harness chat page embeds a live 24h bearer token in a GET body with Cache-Control %q — the handler's own comment calls it a credential-bearing response, and a shared machine's disk cache or bfcache retains the page and its token; theme_edit_page.go:55 in this package already sets no-store for a page with no token", cc)
	}
}
