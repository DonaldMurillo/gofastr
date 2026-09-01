package auth

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/handler"
)

// Property: a response body containing a live credential must be marked
// uncacheable (Cache-Control: no-store). The codebase's own convention is
// magiclink.go's credential page, which sets no-store; the JSON credential
// surfaces must not be cacheable either. The GET backup-codes surface is
// the sharpest: GET responses are heuristically cacheable by browsers and
// proxies, and the back/forward cache retains POST bodies' responses too.
//
// Surfaces (one property, every surface):
//   - POST /auth/2fa/enroll        (TOTP secret + otpauth URL)
//   - POST /auth/2fa/verify        (plaintext backup codes)
//   - POST /auth/2fa/backup-codes (plaintext backup codes)
//   - POST /auth/tokens            (gfsk_ plaintext, "shown exactly ONCE;
//     never retrievable again")
//   - POST /auth/login             (live JWT in the 200 JSON body)
//   - GET  /auth/me                (session-scoped account data)
func TestCredentialResponsesSetNoStore(t *testing.T) {
	mgr, _, _, cookie := newTwoFATestEnv(t)
	r := mountRoutes(mgr)

	check := func(surface string, w *httptest.ResponseRecorder) {
		t.Helper()
		if cc := w.Header().Get("Cache-Control"); !strings.Contains(cc, "no-store") {
			t.Errorf("SECURITY: [cred-no-store] %s returned a live credential with Cache-Control %q: an intermediary or back/forward cache can retain a body the API treats as shown-once", surface, cc)
		}
	}

	// Surface: login (JWT). Runs BEFORE any 2FA enrolment below: a
	// pending-2FA login deliberately withholds the JWT (core.go), and
	// this surface pins the credential-bearing shape of the plain
	// path — a cached login response is a cached bearer credential.
	lreq := httptest.NewRequest("POST", "/auth/login", strings.NewReader(`{"email":"alice@test.com","password":"testpass"}`))
	lreq.Header.Set("Content-Type", "application/json")
	lw := httptest.NewRecorder()
	r.ServeHTTP(lw, lreq)
	if lw.Code != http.StatusOK {
		t.Fatalf("login: %d %s", lw.Code, lw.Body.String())
	}
	check("POST /auth/login", lw)

	// Surface: /auth/me (session-scoped account data: who is signed in
	// under this cookie — replayable from a shared machine's cache).
	mreq := httptest.NewRequest("GET", "/auth/me", nil)
	mreq.AddCookie(&http.Cookie{Name: "session_id", Value: cookie})
	mw := httptest.NewRecorder()
	r.ServeHTTP(mw, mreq)
	if mw.Code != http.StatusOK {
		t.Fatalf("me: %d %s", mw.Code, mw.Body.String())
	}
	check("GET /auth/me", mw)

	// Surface: enroll (TOTP secret).
	req := httptest.NewRequest("POST", "/auth/2fa/enroll", nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: cookie})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("enroll: %d %s", w.Code, w.Body.String())
	}
	check("POST /auth/2fa/enroll", w)
	var enrollResp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&enrollResp); err != nil {
		t.Fatal(err)
	}

	// Surface: verify (plaintext backup codes).
	code := GenerateTOTP(enrollResp["secret"].(string), uint64(time.Now().Unix())/30)
	req = httptest.NewRequest("POST", "/auth/2fa/verify", strings.NewReader(fmt.Sprintf(`{"code":"%s"}`, code)))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session_id", Value: cookie})
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("verify: %d %s", w.Code, w.Body.String())
	}
	check("POST /auth/2fa/verify", w)

	// Surface: backup-codes (plaintext backup codes on a GET).
	req = httptest.NewRequest("POST", "/auth/2fa/backup-codes", nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: cookie})
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("backup-codes: %d %s", w.Code, w.Body.String())
	}
	check("POST /auth/2fa/backup-codes", w)

	// Surface: token create (gfsk_ plaintext). Driven handler-direct like
	// TestAPIToken_CreateIgnoresBodyOwner: the route only needs the session
	// user in ctx.
	_, ts, _ := newTokenTestDB(t)
	mgr2 := New(AuthConfig{JWTSecret: "x", DevMode: true})
	if err := mgr2.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	plugin := NewTokensPlugin(ts)
	if err := plugin.Init(mgr2); err != nil {
		t.Fatalf("plugin Init: %v", err)
	}
	req = bearerRequestWithJSON("POST", "/auth/tokens", `{"name":"n"}`)
	req = req.WithContext(handler.SetUser(req.Context(), &BasicUser{ID: "alice"}))
	w = httptest.NewRecorder()
	plugin.createTokenHandler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("token create: %d %s", w.Code, w.Body.String())
	}
	check("POST /auth/tokens", w)
}
