package setup

// Pins: same-site and unknown Sec-Fetch-Site values fall through to the Origin-host comparison (round 5, fixed).
// Property: cross-site form refusal treats 'same-site' and unknown Sec-Fetch-Site values as
// unproven and falls through to the Origin-host comparison (repo convention:
// battery/auth/core.go::rejectCrossSiteForm and battery/admin/csrf.go — both pinned; the
// setup copy diverges).
// Surfaces: battery/setup/token.go::rejectCrossSiteForm (L73-87 — early-allows anything that
// is not literally 'cross-site').
// Finding: during first-run setup, a page on a sibling subdomain (evil.example.com →
// app.example.com) POSTs /setup urlencoded with Sec-Fetch-Site: same-site and Origin:
// https://evil.example.com; the operator's gofastr_setup cookie (SameSite=Strict attaches on
// same-site requests) passes the cookie gate, rejectCrossSiteForm returns allow, and
// handleSubmit runs the AdminStep with attacker ADMIN_EMAIL/ADMIN_PASSWORD → an
// attacker-owned admin account.
// Fix direction: mirror battery/auth + battery/admin — only 'same-origin' and 'none' clear
// the Fetch-Metadata check; 'same-site' and unknown values fall through to the Origin-host
// comparison.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestSetupRedSameSiteOriginRefused(t *testing.T) {
	var ran bool
	var gotEmail, gotPassword string
	r := New(Config{
		Complete: func(context.Context) (bool, error) { return false, nil },
		Steps: []Step{{
			Name: "Create Admin",
			Fields: []Field{
				{Name: "ADMIN_EMAIL", Label: "Admin email"},
				{Name: "ADMIN_PASSWORD", Label: "Admin password", Secret: true},
			},
			Run: func(_ context.Context, values map[string]string) error {
				ran = true
				gotEmail = values["ADMIN_EMAIL"]
				gotPassword = values["ADMIN_PASSWORD"]
				return nil
			},
		}},
	})
	h := r.Handler(func() {}, nil, nil)

	// The operator exchanged the one-time URL token in their browser, so a
	// valid gofastr_setup cookie exists. It is SameSite=Strict — which is
	// exactly why it still attaches to the sibling-subdomain POST below.
	exchange := doGet(h, "/setup?token="+r.token)
	if exchange.Code != http.StatusSeeOther {
		t.Fatalf("setup broken: token exchange returned %d, want 303", exchange.Code)
	}
	var cookie *http.Cookie
	for _, c := range exchange.Result().Cookies() {
		if c.Name == setupCookieName {
			cookie = c
		}
	}
	if cookie == nil || cookie.Value == "" {
		t.Fatal("setup broken: token exchange set no usable setup cookie")
	}

	// The attack: a page on evil.example.com auto-submits the admin-creation
	// form against app.example.com. The hosts are sibling subdomains, so the
	// browser sends Sec-Fetch-Site: same-site (true) and the Strict cookie
	// rides along, while Origin names the attacker's host. The Origin-host
	// comparison the sibling gates run uses the request host — app.example.com.
	form := url.Values{
		"ADMIN_EMAIL":    {"attacker@evil.example"},
		"ADMIN_PASSWORD": {"Str0ngPass!x"},
	}
	req := httptest.NewRequest(http.MethodPost, "/setup", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Host = "app.example.com"
	req.Header.Set("Sec-Fetch-Site", "same-site")
	req.Header.Set("Origin", "https://evil.example.com")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden || ran {
		t.Errorf("SECURITY: [setup-samesite-csrf] sibling-subdomain POST /setup (Sec-Fetch-Site: same-site, Origin: https://evil.example.com vs request host app.example.com, valid setup cookie) returned %d and the admin-creation step ran=%v (email %q, password %q) — rejectCrossSiteForm early-allows any Sec-Fetch-Site value that is not literally 'cross-site', so during first-run setup a forged same-site form executes the AdminStep with attacker credentials and mints an attacker-owned admin; battery/auth core.go and battery/admin csrf.go pin the opposite: 'same-site' falls through to the Origin-host comparison",
			w.Code, ran, gotEmail, gotPassword)
	}
}
