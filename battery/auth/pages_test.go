package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/router"
)

// recordPages is a Pages that records what the battery handed it.
type recordPages struct {
	confirm *ConfirmPageData
	hop     *HopPageData
	locale  string
	headers http.Header
}

func (p *recordPages) MagicLinkConfirm(w http.ResponseWriter, r *http.Request, d ConfirmPageData) {
	p.confirm, p.locale, p.headers = &d, i18n.FromContext(r.Context()).Tag, w.Header().Clone()
	w.WriteHeader(http.StatusOK)
}

func (p *recordPages) Hop(w http.ResponseWriter, r *http.Request, d HopPageData) {
	p.hop, p.headers = &d, w.Header().Clone()
	w.WriteHeader(http.StatusOK)
}

// wantCredentialHeaders checks the headers the battery sets before it
// hands a credential-bearing page to Pages.
func wantCredentialHeaders(t *testing.T, h http.Header) {
	t.Helper()
	if got := h.Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control before Pages = %q, want no-store", got)
	}
	if got := h.Get("Referrer-Policy"); got != "no-referrer" {
		t.Errorf("Referrer-Policy before Pages = %q, want no-referrer", got)
	}
}

// AuthConfig.Pages renders the confirmation screen: it gets the token,
// the form action, the request's locale and the resolved words, with
// the address emphasised and escaped.
func TestPagesRenderMagicLinkConfirm(t *testing.T) {
	pages := &recordPages{}
	mgr := New(AuthConfig{
		SessionTTL: time.Hour, SessionCookie: "session_id",
		UserStore: newMemoryUserStore(), DevMode: true, Pages: pages,
	})
	plugin := NewMagicLinkPlugin(MagicLinkConfig{BaseURL: "http://localhost", TokenTTL: time.Hour, DevMode: true})
	mgr.Use(plugin)
	if err := mgr.Init(nil); err != nil {
		t.Fatal(err)
	}
	r := router.New()
	mgr.RegisterRoutes(r)
	token, err := createPurposeToken(context.Background(), plugin.tokenStore, purposeMagicLink, "a<b>@example.com", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/auth/magic-link/verify?token="+token, nil)
	req = req.WithContext(i18n.WithContext(req.Context(), i18n.Locale{Tag: "fr"}))
	r.ServeHTTP(httptest.NewRecorder(), req)

	d := pages.confirm
	if d == nil {
		t.Fatal("Pages.MagicLinkConfirm was not called")
	}
	if d.Token != token || d.Action != "/auth/magic-link/verify" {
		t.Errorf("data = token %q action %q, want the link's token and /auth/magic-link/verify", d.Token, d.Action)
	}
	if pages.locale != "fr" {
		t.Errorf("Pages saw locale %q, want fr", pages.locale)
	}
	if d.Words.Title != "Confirm sign-in" || d.Words.Submit != "Sign in" || d.Words.Warning == "" {
		t.Errorf("words = %+v, want the confirmation phrases", d.Words)
	}
	if !strings.Contains(string(d.Words.Ask), "<strong>a&lt;b&gt;@example.com</strong>") {
		t.Errorf("SECURITY: Ask = %q, want the escaped address inside <strong>", d.Words.Ask)
	}
	wantCredentialHeaders(t, pages.headers)
}

// AuthConfig.Pages renders the hop a webmail click on a verification
// link lands on, with this request's own path as the target.
func TestPagesRenderHop(t *testing.T) {
	c := newClaimRig(t)
	pages := &recordPages{}
	c.mgr.config.Pages = pages
	const email, pw = "owner@example.com", "owner-chosen-pw-1"
	c.json("/auth/register", map[string]string{"email": email, "password": pw}, "")
	sess := c.login(email, pw)
	if w := c.json("/auth/send-verification", map[string]string{}, sess); w.Code != http.StatusOK {
		t.Fatalf("send-verification: %d %s", w.Code, w.Body.String())
	}
	_, body := c.verifies.snapshot()
	path := "/auth/verify-email?token=" + url.QueryEscape(extractTokenFromBody(body))
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	c.r.ServeHTTP(httptest.NewRecorder(), req)

	d := pages.hop
	if d == nil {
		t.Fatal("Pages.Hop was not called")
	}
	if d.Target != path || d.Title != "Verifying your email" || d.Continue != "Continue" {
		t.Errorf("hop data = %+v, want target %q and the verify phrases", *d, path)
	}
	if got := pages.headers.Get("Refresh"); got != "0;url="+path {
		t.Errorf("Refresh before Pages = %q, want 0;url=%s", got, path)
	}
	wantCredentialHeaders(t, pages.headers)
}
