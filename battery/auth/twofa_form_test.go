package auth

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// A form sign-in of a 2FA account goes to the code page, and that page's
// form (ui.OneTimeCode posting "code") finishes it: a wrong code 303s
// back to the page with ?error=, the right one 303s to next with a
// session past the second factor.
func TestTwoFAChallengeHTMLForm(t *testing.T) {
	mgr, _, _, cookie := newTwoFATestEnv(t)
	r := mountRoutes(mgr)
	jsonPost := func(path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: "session_id", Value: cookie})
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	var enrolled map[string]any
	_ = json.NewDecoder(jsonPost("/auth/2fa/enroll", "").Body).Decode(&enrolled)
	secret, _ := enrolled["secret"].(string)
	step := uint64(time.Now().Unix()) / 30
	if w := jsonPost("/auth/2fa/verify", fmt.Sprintf(`{"code":%q}`, GenerateTOTP(secret, step))); w.Code != http.StatusOK {
		t.Fatalf("verify: %d %s", w.Code, w.Body.String())
	}

	form := func(path string, v url.Values, session string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(v.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Referer", "http://example.com/login/2fa")
		if session != "" {
			req.AddCookie(&http.Cookie{Name: "session_id", Value: session})
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	login := form("/auth/login", url.Values{"email": {"alice@test.com"}, "password": {"testpass"}}, "")
	if login.Code != http.StatusSeeOther || login.Header().Get("Location") != "/login/2fa" {
		t.Fatalf("form login of a 2FA account: %d Location=%q, want 303 to /login/2fa", login.Code, login.Header().Get("Location"))
	}
	var pending string
	for _, c := range login.Result().Cookies() {
		if c.Name == "session_id" {
			pending = c.Value
		}
	}

	wrong := form("/auth/2fa/challenge", url.Values{"code": {"000000"}}, pending)
	if wrong.Code != http.StatusSeeOther || !strings.HasPrefix(wrong.Header().Get("Location"), "/login/2fa?error=") {
		t.Errorf("a wrong code: %d Location=%q, want 303 back to /login/2fa?error=…", wrong.Code, wrong.Header().Get("Location"))
	}

	right := form("/auth/2fa/challenge", url.Values{"code": {GenerateTOTP(secret, step+1)}, "next": {"/app"}}, pending)
	if right.Code != http.StatusSeeOther || right.Header().Get("Location") != "/app" {
		t.Fatalf("the right code: %d Location=%q body=%s, want 303 to /app", right.Code, right.Header().Get("Location"), right.Body.String())
	}
	var full string
	for _, c := range right.Result().Cookies() {
		if c.Name == "session_id" {
			full = c.Value
		}
	}
	me := httptest.NewRequest(http.MethodGet, "/auth/me", nil)
	me.AddCookie(&http.Cookie{Name: "session_id", Value: full})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, me)
	if w.Code != http.StatusOK {
		t.Errorf("/auth/me with the session the form challenge set: %d, want 200", w.Code)
	}
}
