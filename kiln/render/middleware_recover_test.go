package render

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestCatalogRecoverCutsStartedResponse: the catalog's recover wrote
// http.Error unconditionally, so a panic after a 200 and part of the
// body appended "internal error" to it. It is core/middleware's Recovery
// now: a started response is cut, an unstarted one answers 500.
func TestCatalogRecoverCutsStartedResponse(t *testing.T) {
	mw, err := buildRecover(nil)
	if err != nil {
		t.Fatal(err)
	}
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "partial")
		panic("mid-body bug")
	}))
	rec := httptest.NewRecorder()
	var v any
	func() {
		defer func() { v = recover() }()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))
	}()
	if v != http.ErrAbortHandler {
		t.Fatalf("recovered %v, want http.ErrAbortHandler", v)
	}
	if rec.Body.String() != "partial" {
		t.Fatalf("body %q: error text spliced onto the started response", rec.Body.String())
	}

	before := mw(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("early bug") }))
	rec = httptest.NewRecorder()
	before.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("unstarted panic: status %d, want 500", rec.Code)
	}
}
