package uihost

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// A /__gofastr asset revalidates against an If-None-Match list, "*" and a
// weak tag, as RFC 9110 has caches send them. The header was compared
// with == against the one ETag, so each of these got a full 200.
func TestAssetRevalidatesETagList(t *testing.T) {
	h := ScriptHandler([]byte("console.log('x')"))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x.js", nil))
	etag := rec.Header().Get("ETag")
	if etag == "" {
		t.Fatal("no ETag")
	}
	for _, inm := range []string{etag, `"other", ` + etag, "*", "W/" + etag} {
		req := httptest.NewRequest(http.MethodGet, "/x.js", nil)
		req.Header.Set("If-None-Match", inm)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotModified {
			t.Errorf("If-None-Match %s = %d, want 304", inm, rec.Code)
		}
	}
	req := httptest.NewRequest(http.MethodGet, "/x.js", nil)
	req.Header.Set("If-None-Match", `"other"`)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("a non-matching tag = %d, want 200", rec.Code)
	}
}
