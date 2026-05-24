package auth

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// TestSuccessRedirect_RejectsBackslashBypass pins the open-redirect fix.
// `/\evil.example/x` survived the `//` prefix check (HasPrefix("//") is
// false) but browsers normalise the backslash to forward slash at
// navigation time, landing on evil.example. successRedirect MUST treat
// backslash as scheme-like and fall back to the supplied default.
func TestSuccessRedirect_RejectsBackslashBypass(t *testing.T) {
	cases := []struct {
		name string
		next string
	}{
		{"backslash double-slash", `/\evil.example/path`},
		{"backslash single", `/\evil.example`},
		{"mixed slash backslash", `/\/evil.example/`},
		{"protocol-relative double slash", `//evil.example/`},
		{"absolute http URL", `http://evil.example/`},
		{"absolute https URL", `https://evil.example/`},
		{"javascript scheme", `javascript:alert(1)`},
		{"data URL", `data:text/html,<script>`},
		{"control character", "/normal\x00/evil"},
		{"CRLF injection", "/normal\r\nLocation: https://evil"},
		// URL-encoded variants — browsers decode percent-escapes before
		// resolving the URL, so encoded backslash is exactly as
		// dangerous as literal backslash.
		{"url-encoded backslash", "/%5Cevil.example/path"},
		{"url-encoded uppercase backslash", "/%5cevil.example/path"},
		{"url-encoded double-percent", "/%5C%5Cevil.example/"},
		{"url-encoded CRLF", "/normal%0d%0aLocation:%20evil"},
		{"url-encoded NUL", "/normal%00/evil"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/auth/login?next="+url.QueryEscape(tc.next),
				strings.NewReader(""))
			got := successRedirect(req, "/safe")
			if got != "/safe" {
				t.Errorf("attacker-controlled next=%q produced redirect=%q (want /safe)", tc.next, got)
			}
		})
	}
}

// TestSuccessRedirect_AllowsLegitimatePath confirms the happy path
// still works.
func TestSuccessRedirect_AllowsLegitimatePath(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/auth/login?next=/dashboard", nil)
	got := successRedirect(req, "/")
	if got != "/dashboard" {
		t.Errorf("legit next was rejected: got %q want /dashboard", got)
	}
}

// TestWriteFormAuthError_RejectsCrossOriginReferer pins the phishing
// relay fix. An attacker hosts a form on evil.example that posts to
// the real auth endpoint. On error, writeFormAuthError must NOT 303
// the user back to evil.example. Same-origin Referer is OK; anything
// else falls back to the configured login-error path (or JSON when
// nothing's configured).
func TestWriteFormAuthError_RejectsCrossOriginReferer(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/auth/login", nil)
	req.Header.Set("Host", "real.example")
	req.Host = "real.example"
	req.Header.Set("Referer", "https://evil.example/phish.html")
	rec := httptest.NewRecorder()
	writeFormAuthError(rec, req, http.StatusUnauthorized, "invalid_credentials")

	if loc := rec.Header().Get("Location"); strings.Contains(loc, "evil.example") {
		t.Errorf("redirected to attacker Referer: %q", loc)
	}
}

// TestWriteFormAuthError_StripsRefererQuery pins the state-confusion
// fix: attacker-controlled query params on the Referer URL must NOT
// propagate to the redirect Location. The error code from msg is the
// only query value the redirect should carry.
func TestWriteFormAuthError_StripsRefererQuery(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/auth/login", nil)
	req.Host = "real.example"
	req.Header.Set("Referer", "https://real.example/login?return=//evil.attack&token=hijack")
	rec := httptest.NewRecorder()
	writeFormAuthError(rec, req, http.StatusUnauthorized, "invalid_credentials")

	loc := rec.Header().Get("Location")
	if strings.Contains(loc, "return=") || strings.Contains(loc, "token=hijack") {
		t.Errorf("Location propagated Referer query: %q", loc)
	}
	if !strings.Contains(loc, "error=invalid_credentials") {
		t.Errorf("Location missing error code: %q", loc)
	}
}

// TestWriteFormAuthError_AcceptsSameOriginReferer confirms the happy
// path: a legit form on the same host, user hits Submit, password is
// wrong → 303 back to the same page with ?error=.
func TestWriteFormAuthError_AcceptsSameOriginReferer(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/auth/login", nil)
	req.Host = "real.example"
	req.Header.Set("Referer", "https://real.example/login")
	rec := httptest.NewRecorder()
	writeFormAuthError(rec, req, http.StatusUnauthorized, "invalid_credentials")

	loc := rec.Header().Get("Location")
	if !strings.Contains(loc, "/login") {
		t.Errorf("same-origin Referer rejected: Location=%q", loc)
	}
	if !strings.Contains(loc, "error=invalid_credentials") {
		t.Errorf("error code not appended: Location=%q", loc)
	}
}
