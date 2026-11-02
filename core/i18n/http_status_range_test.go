package i18n

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestHTTPErrorClampsStatus: HTTPError wrote whatever status it was
// handed. A zero (decide.Block(0), an unset config) panicked inside
// net/http's WriteHeader on the error path, and a 2xx sent the error as
// success. Anything outside 400-599 is a 500 now, as handler.WriteError
// sends it.
func TestHTTPErrorClampsStatus(t *testing.T) {
	for _, status := range []int{0, 42, http.StatusOK, http.StatusFound, 600} {
		w := httptest.NewRecorder()
		func() {
			defer func() {
				if v := recover(); v != nil {
					t.Errorf("status %d: HTTPError panicked: %v", status, v)
				}
			}()
			HTTPError(w, httptest.NewRequest("GET", "/", nil), status, StatusPhrase(status))
		}()
		if w.Code != http.StatusInternalServerError {
			t.Errorf("status %d: sent %d, want 500", status, w.Code)
		}
	}
	w := httptest.NewRecorder()
	HTTPError(w, httptest.NewRequest("GET", "/", nil), http.StatusTeapot, StatusPhrase(http.StatusTeapot))
	if w.Code != http.StatusTeapot {
		t.Fatalf("a 4xx is kept: got %d", w.Code)
	}
}
