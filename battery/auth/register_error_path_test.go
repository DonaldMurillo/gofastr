package auth

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// Every form-path auth error fell back to the one login error path: the
// app's pages send Referrer-Policy: no-referrer, so a browser never sends
// the Referer writeFormAuthError prefers, and a failed signup (weak
// password, missing field, a cross-site or throttled post) landed on
// /login. A register form now falls back to its own error path; login
// keeps the login path, and with no register path set it is still used.
func TestRegisterErrorsLandOnRegisterPath(t *testing.T) {
	SetDefaultLoginErrorPath("/login")
	SetDefaultRegisterErrorPath("/signup")
	t.Cleanup(func() {
		SetDefaultLoginErrorPath("")
		SetDefaultRegisterErrorPath("")
	})
	r := setupLoginCSRF(t)
	post := func(path string, vals url.Values, hdr map[string]string) string {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(vals.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Host = "app.example"
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusSeeOther {
			t.Fatalf("POST %s = %d %s, want 303", path, w.Code, w.Body)
		}
		return w.Header().Get("Location")
	}
	weak := url.Values{"email": {"new@example.com"}, "password": {"x"}}
	cases := []struct {
		name, path string
		vals       url.Values
		hdr        map[string]string
		want       string
	}{
		{"weak password", "/auth/register", weak, nil, "/signup?error=weak_password"},
		{"missing field", "/auth/register", url.Values{"email": {"new@example.com"}}, nil, "/signup?error="},
		{"cross-site", "/auth/register", weak, map[string]string{"Origin": "https://evil.example"}, "/signup?error="},
		{"login", "/auth/login", url.Values{"email": {"alice@example.com"}, "password": {"wrong-password"}}, nil, "/login?error="},
	}
	for _, tc := range cases {
		if loc := post(tc.path, tc.vals, tc.hdr); !strings.HasPrefix(loc, tc.want) {
			t.Errorf("%s: Location = %q, want %s…", tc.name, loc, tc.want)
		}
	}

	SetDefaultRegisterErrorPath("")
	if loc := post("/auth/register", weak, nil); !strings.HasPrefix(loc, "/login?error=") {
		t.Errorf("no register path: Location = %q, want the login path", loc)
	}
}
