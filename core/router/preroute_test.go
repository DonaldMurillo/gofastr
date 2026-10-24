package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// stripES routes "/es/x" as "/x" and redirects "/en/x" to "/x".
func stripES(req *http.Request) (*http.Request, string) {
	if rest, ok := strings.CutPrefix(req.URL.Path, "/en/"); ok {
		return nil, "/" + rest
	}
	rest, ok := strings.CutPrefix(req.URL.Path, "/es/")
	if !ok {
		return nil, ""
	}
	r2 := req.Clone(req.Context())
	r2.URL.Path = "/" + rest
	return r2, ""
}

func TestPreRouteRewritesBeforeMatch(t *testing.T) {
	r := New()
	r.Get("/pricing", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Write([]byte("pricing " + req.URL.Path))
	}))
	r.PreRoute(stripES)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/es/pricing", nil))
	if rec.Code != 200 || rec.Body.String() != "pricing /pricing" {
		t.Fatalf("got %d %q", rec.Code, rec.Body.String())
	}
}

func TestPreRouteRedirectRunsChain(t *testing.T) {
	r := New()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			w.Header().Set("X-Chain", "1")
			next.ServeHTTP(w, req)
		})
	})
	r.Get("/pricing", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	r.PreRoute(stripES)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/en/pricing", nil))
	if rec.Code != http.StatusPermanentRedirect || rec.Header().Get("Location") != "/pricing" {
		t.Fatalf("got %d Location %q", rec.Code, rec.Header().Get("Location"))
	}
	if rec.Header().Get("X-Chain") != "1" {
		t.Fatal("the redirect skipped the middleware chain")
	}
}

func TestPreRouteNilRemoves(t *testing.T) {
	r := New()
	r.Get("/es/pricing", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("raw"))
	}))
	r.PreRoute(stripES)
	r.PreRoute(nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/es/pricing", nil))
	if rec.Body.String() != "raw" {
		t.Fatalf("got %q", rec.Body.String())
	}
}
