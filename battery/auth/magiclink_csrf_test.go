package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/router"
)

func magicLinkCSRFRouter(t *testing.T) (*router.Router, *MagicLinkPlugin) {
	t.Helper()
	mgr := New(AuthConfig{
		SessionTTL:    time.Hour,
		SessionCookie: "session_id",
		UserStore:     newMemoryUserStore(),
		DevMode:       true,
	})
	plugin := NewMagicLinkPlugin(MagicLinkConfig{
		BaseURL:  "http://localhost",
		TokenTTL: time.Hour,
		DevMode:  true,
	})
	mgr.Use(plugin)
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	r := router.New()
	mgr.RegisterRoutes(r)
	return r, plugin
}

// mintedSession returns the session cookie value the response set, or "".
func mintedSession(rec *httptest.ResponseRecorder) string {
	for _, c := range rec.Result().Cookies() {
		if c.Name == "session_id" {
			return c.Value
		}
	}
	return ""
}

// The confirmation step exists so an attacker who mails their OWN magic
// link cannot silently land a victim's browser in the attacker's account.
// Its only defence against the attacker auto-submitting the form from
// their page is rejectCrossSiteForm.
//
// That guard used to run only for x-www-form-urlencoded and multipart.
// text/plain is the third enctype an HTML <form> can use and is a
// CORS-simple request (no preflight), so the guard never ran, and since
// verifyHandler falls back to the query-string token, the attacker needed
// no body at all:
//
//	<form method="POST" enctype="text/plain"
//	      action="https://app/auth/magic-link/verify?token=ATTACKER_TOKEN">
func TestVerifyRejectsCrossSiteTextPlain(t *testing.T) {
	r, plugin := magicLinkCSRFRouter(t)

	token, err := createPurposeToken(context.Background(), plugin.tokenStore, purposeMagicLink, "attacker@evil.example", time.Hour)
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost,
		"/auth/magic-link/verify?token="+token, strings.NewReader("")) // not-a-secret: minted by this test's in-memory store
	// The headers a real browser sends for a cross-site text/plain form.
	req.Header.Set("Content-Type", "text/plain;charset=UTF-8")
	req.Header.Set("Origin", "https://evil.example")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	req.Header.Set("Sec-Fetch-Mode", "navigate")

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if got := mintedSession(rec); got != "" {
		t.Fatalf("cross-site text/plain POST minted a session (status %d, session_id=%q)",
			rec.Code, got)
	}
}

// A bodyless cross-site fetch() sends no Content-Type at all, which is
// equally preflight-free. The guard must treat an absent Content-Type as
// forgeable for the same reason it treats text/plain as forgeable.
func TestVerifyRejectsCrossSiteNoBody(t *testing.T) {
	r, plugin := magicLinkCSRFRouter(t)

	token, err := createPurposeToken(context.Background(), plugin.tokenStore, purposeMagicLink, "attacker@evil.example", time.Hour)
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/auth/magic-link/verify?token="+token, nil)
	req.Header.Set("Origin", "https://evil.example")
	req.Header.Set("Sec-Fetch-Site", "cross-site")

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if got := mintedSession(rec); got != "" {
		t.Fatalf("cross-site bodyless POST minted a session (status %d, session_id=%q)",
			rec.Code, got)
	}
}

// A cross-origin JSON caller is NOT forgeable, it needs a preflight the
// framework never answers, and legitimate cross-origin SPAs depend on
// reaching these routes through a configured CORS middleware. Widening
// the guard must not have swept JSON in.
func TestVerifyAllowsCrossOriginJSON(t *testing.T) {
	r, plugin := magicLinkCSRFRouter(t)

	token, err := createPurposeToken(context.Background(), plugin.tokenStore, purposeMagicLink, "alice@example.com", time.Hour)
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/auth/magic-link/verify?token="+token, nil)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://spa.example")
	req.Header.Set("Sec-Fetch-Site", "cross-site")

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code == http.StatusForbidden {
		t.Fatalf("cross-origin JSON was refused as a forged form: %d %q",
			rec.Code, strings.TrimSpace(rec.Body.String()))
	}
}

var csrfInputRe = regexp.MustCompile(`name="_csrf"\s+value="([^"]*)"`)

// battery/auth.CSRF is what WithBFFPosture installs app-wide and what the
// docs recommend for any cookie-authenticated app. It requires a "_csrf"
// field on every unsafe method.
//
// The default confirmation page used to render one hidden field ("token")
// and nothing else, so following a magic link produced a screen whose only
// button 403'd, passwordless sign-in was unreachable in the documented
// wiring. ConfirmPageData carries no request and no context, so a custom
// page could not reach the token either; hence ConfirmPageData.CSRFField.
//
// This drives the whole flow the way a browser does: GET the page, submit
// every input the document actually contains, carrying the GET's cookies.
func TestConfirmPageFormWorksBehindCSRF(t *testing.T) {
	r, plugin := magicLinkCSRFRouter(t)
	guarded := CSRF()(r)

	token, err := createPurposeToken(context.Background(), plugin.tokenStore, purposeMagicLink, "alice@example.com", time.Hour)
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}

	get := httptest.NewRequest(http.MethodGet, "/auth/magic-link/verify?token="+token, nil)
	getRec := httptest.NewRecorder()
	guarded.ServeHTTP(getRec, get)
	if getRec.Code != http.StatusOK {
		t.Fatalf("GET confirm page = %d, want 200", getRec.Code)
	}

	m := csrfInputRe.FindStringSubmatch(getRec.Body.String())
	if m == nil {
		t.Fatal("confirmation page rendered no _csrf field, so its own form cannot pass CSRF")
	}

	form := "token=" + token + "&_csrf=" + m[1] // not-a-secret: both minted by this test
	post := httptest.NewRequest(http.MethodPost, "/auth/magic-link/verify", strings.NewReader(form))
	post.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	post.Header.Set("Sec-Fetch-Site", "same-origin")
	for _, c := range getRec.Result().Cookies() {
		post.AddCookie(c)
	}
	postRec := httptest.NewRecorder()
	guarded.ServeHTTP(postRec, post)

	if postRec.Code == http.StatusForbidden {
		t.Fatalf("submitting the page the plugin itself rendered returned %d %q",
			postRec.Code, strings.TrimSpace(postRec.Body.String()))
	}
	if mintedSession(postRec) == "" {
		t.Fatalf("confirmed sign-in minted no session (status %d)", postRec.Code)
	}
}
