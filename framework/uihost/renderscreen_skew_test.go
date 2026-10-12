package uihost

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/runtime"
)

// TestRenderScreenPartialHonoursMarkupSkew: RenderScreen's partial arm
// is a navigation response like handlePartialPage's, so a stale
// runtime (a tab opened before the deploy, no markup header, a script
// fetch) must get the 409 reload body, and a current runtime must get
// the partial marked X-Gofastr-Partial so nav.js never treats a bare
// screen body as a full document.
func TestRenderScreenPartialHonoursMarkupSkew(t *testing.T) {
	ds := New(newRecoverApp())
	get := func(markup, mode string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/session/dead", nil)
		req.Header.Set("X-Gofastr-Navigate", "1")
		if markup != "" {
			req.Header.Set(runtime.MarkupHeader, markup)
		}
		if mode != "" {
			req.Header.Set("Sec-Fetch-Mode", mode)
		}
		rec := httptest.NewRecorder()
		ds.RenderScreen(rec, req, recoverComp{}, ScreenResponse{Status: http.StatusGone})
		return rec
	}

	stale := get("", "cors")
	if stale.Code != http.StatusConflict {
		t.Fatalf("stale client: status %d, want 409", stale.Code)
	}
	if !strings.Contains(stale.Body.String(), `http-equiv="refresh"`) {
		t.Errorf("stale client body lacks the reload: %s", stale.Body.String())
	}
	if strings.Contains(stale.Body.String(), "Session over") {
		t.Errorf("stale client got the screen body its kernel cannot place: %s", stale.Body.String())
	}

	for _, tc := range []struct{ name, markup, mode string }{
		{"current generation", runtime.MarkupVersion, "cors"},
		{"no fetch metadata", "", ""},
	} {
		rec := get(tc.markup, tc.mode)
		if rec.Code != http.StatusGone {
			t.Errorf("%s: status %d, want 410", tc.name, rec.Code)
		}
		if rec.Header().Get("X-Gofastr-Partial") != "true" {
			t.Errorf("%s: X-Gofastr-Partial = %q, want true", tc.name, rec.Header().Get("X-Gofastr-Partial"))
		}
		if !strings.Contains(rec.Body.String(), "Session over") {
			t.Errorf("%s: partial body missing the screen: %s", tc.name, rec.Body.String())
		}
	}
}
