package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/embed"
)

// Double-submit CSRF pairs a cookie with a header, and no cookie is ever sent
// from inside an embed frame — SameSite is computed against the top-level
// browsing context, which is the customer's site. So an app that installs
// auth.CSRF() would 403 every embed exchange with "missing cookie" and the
// feature would be dead in exactly the configuration this framework recommends.
//
// The exemption is safe because these endpoints have no ambient credential to
// abuse: the exchange consumes a single-use nonce the caller must already
// possess, the refresh a grant they must already possess. Same reasoning as
// SkipBearerAuth.
func TestCSRFExemptsEmbedEndpoints(t *testing.T) {
	reached := false
	h := CSRF(WithCSRFSecret([]byte("csrf-secret-csrf-secret-csrf-32b")))(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			reached = true
			w.WriteHeader(http.StatusOK)
		}))

	for _, path := range []string{embed.ExchangePath, embed.RefreshPath} {
		reached = false
		rec := httptest.NewRecorder()
		// No cookie, no token — exactly what a frame sends.
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		h.ServeHTTP(rec, req)

		if rec.Code == http.StatusForbidden {
			t.Errorf("%s was refused by CSRF (%s) — no cookie is ever sent from inside a frame, so every embed exchange would fail", path, strings.TrimSpace(rec.Body.String()))
		}
		if !reached {
			t.Errorf("%s never reached the handler", path)
		}
	}

	// The exemption must not widen to ordinary routes.
	reached = false
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/posts", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("an ordinary POST with no CSRF token got %d, want 403 — the embed exemption leaked", rec.Code)
	}
	if reached {
		t.Error("an ordinary POST with no CSRF token reached the handler")
	}
}

// The PATH exemption covers only the two handshake endpoints. What covers an
// embed's island RPC to an ordinary app route — which carries the grant header
// and no cookie — is the header branch, and removing it would 403 every one of
// them. It had no test.
func TestCSRFExemptsGrantBearingRequestsOnOrdinaryRoutes(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/posts", nil)
	if embed.CSRFExempt(req) {
		t.Fatal("an ordinary POST with no grant was exempted from CSRF")
	}
	req.Header.Set(embed.GrantHeader, "emg_whatever")
	if !embed.CSRFExempt(req) {
		t.Error("a grant-bearing POST to an ordinary route was not exempted — " +
			"every island RPC from inside a frame would 403, since a frame sends " +
			"no cookie to double-submit")
	}
}
