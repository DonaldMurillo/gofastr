package ui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
)

// A DismissID banner must not render at all when the request carries the
// dismissal cookie the runtime writes — server-side skip is what makes
// dismissal flash-free (localStorage alone hides only after JS runs).
func TestBannerSkipsRenderOnDismissCookie(t *testing.T) {
	cfg := BannerConfig{Title: "Notice", Dismissible: true, DismissID: "test-note"}

	// Without the cookie (or without a request in ctx) the banner renders.
	if h := string(Banner(cfg)); !strings.Contains(h, "Notice") {
		t.Fatalf("banner should render without the dismiss cookie:\n%s", h)
	}
	rNo := httptest.NewRequest("GET", "/", nil)
	cfg.Ctx = app.WithRequest(rNo.Context(), rNo)
	if h := string(Banner(cfg)); !strings.Contains(h, "Notice") {
		t.Fatalf("banner should render when request lacks the cookie:\n%s", h)
	}

	// With the runtime's mirrored cookie, the banner is skipped entirely.
	r := httptest.NewRequest("GET", "/", nil)
	r.AddCookie(&http.Cookie{Name: "gofastr.banner-dismiss.test-note", Value: "1"})
	cfg.Ctx = app.WithRequest(r.Context(), r)
	if h := string(Banner(cfg)); h != "" {
		t.Errorf("dismissed banner must render nothing, got:\n%s", h)
	}

	// A different banner's cookie must not suppress this one.
	r2 := httptest.NewRequest("GET", "/", nil)
	r2.AddCookie(&http.Cookie{Name: "gofastr.banner-dismiss.other", Value: "1"})
	cfg.Ctx = app.WithRequest(r2.Context(), r2)
	if h := string(Banner(cfg)); !strings.Contains(h, "Notice") {
		t.Errorf("unrelated dismiss cookie must not suppress the banner:\n%s", h)
	}
}
