package uihost

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestActionCacheCommentsMatchPolicy(t *testing.T) {
	source, err := os.ReadFile("uihost.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, stale := range []string{"addressed and served immutable", "served private+immutable"} {
		if strings.Contains(string(source), stale) {
			t.Errorf("action-script comment contradicts private, no-cache policy: %q", stale)
		}
	}
	for _, version := range []string{"", "?v=current", "?v=previous"} {
		for _, conditional := range []bool{false, true} {
			request := httptest.NewRequest(http.MethodGet, "/__gofastr/widget/card.js"+version, nil)
			if conditional {
				request.Header.Set("If-None-Match", `"current"`)
			}
			recorder := httptest.NewRecorder()
			serveVersionedText(recorder, request, "application/javascript", "compiled", "current", true)
			if got := recorder.Header().Get("Cache-Control"); got != "private, no-cache" {
				t.Errorf("version %q, conditional %v: cache policy = %q", version, conditional, got)
			}
			wantStatus := http.StatusOK
			if conditional {
				wantStatus = http.StatusNotModified
			}
			if recorder.Code != wantStatus {
				t.Errorf("version %q, conditional %v: status = %d, want %d", version, conditional, recorder.Code, wantStatus)
			}
		}
	}
}
