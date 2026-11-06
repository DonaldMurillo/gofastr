package app

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
)

func TestMarkdownHandlersArePrivate(t *testing.T) {
	a, screens := acctGuardApp(t)
	for _, handler := range []http.Handler{
		ScreenLLMMDHandler(a, screens["/pub"]),
		ScreenLLMMDHandler(a, screens["/acct"]),
		AppLLMMDHandler(a),
	} {
		for _, cookie := range []string{"", "session=member"} {
			recorder := httptest.NewRecorder()
			recorder.Header().Set("Vary", "Accept-Encoding")
			request := httptest.NewRequest(http.MethodGet, "/llm.md", nil)
			request.Header.Set("Cookie", cookie)
			handler.ServeHTTP(recorder, request)
			if got := recorder.Header().Get("Cache-Control"); got != "private, no-store" {
				t.Errorf("Cache-Control=%q, want private, no-store", got)
			}
			for _, name := range []string{"Cookie", "Accept-Encoding"} {
				if !slices.Contains(recorder.Header().Values("Vary"), name) {
					t.Errorf("Vary=%v, missing %s", recorder.Header().Values("Vary"), name)
				}
			}
		}
	}
}
