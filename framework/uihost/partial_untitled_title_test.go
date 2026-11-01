package uihost

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/render"
)

type untitledScreen struct{}

func (untitledScreen) Render() render.HTML { return "untitled body" }

// TestPartialUntitledScreenSendsAppName: a screen with no title is
// titled with the app name on a full load; its partial omitted
// X-Gofastr-Title, so a soft navigation kept the PREVIOUS page's
// document.title and route announcement. Both answers now carry the
// same effective title.
func TestPartialUntitledScreenSendsAppName(t *testing.T) {
	a := app.NewApp("Luna Help")
	a.Register("/plain", untitledScreen{}, nil)
	host := New(a)

	full := httptest.NewRecorder()
	host.ServeHTTP(full, httptest.NewRequest("GET", "/plain", nil))
	if !strings.Contains(full.Body.String(), "<title>Luna Help</title>") {
		t.Fatalf("full load title is not the app name; body head: %.300s", full.Body.String())
	}

	req := httptest.NewRequest("GET", "/plain", nil)
	req.Header.Set("X-Gofastr-Navigate", "1")
	rec := httptest.NewRecorder()
	host.ServeHTTP(rec, req)
	if rec.Header().Get("X-Gofastr-Partial") != "true" {
		t.Fatalf("not a partial answer: %v", rec.Header())
	}
	raw := rec.Header().Get("X-Gofastr-Title")
	got, err := url.PathUnescape(raw)
	if err != nil || got != "Luna Help" {
		t.Fatalf("partial X-Gofastr-Title = %q (%v), want the app name the full document uses", raw, err)
	}
}
