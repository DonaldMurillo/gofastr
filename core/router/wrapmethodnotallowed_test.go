package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// WrapMethodNotAllowed is WrapNotFound's twin for the 405 fall-through:
// the wrapper claims what it claims, delegates the rest to the handler
// set before it, sees the Allow header the router computed, and runs
// inside the chain of the group that owns the path.
func TestWrapMethodNotAllowedDelegates(t *testing.T) {
	r := New()
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {})
	r.Get("/claimed/x", ok)
	r.Get("/other", ok)
	api := r.Group("/grp")
	api.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			w.Header().Set("X-Group-Chain", "yes")
			next.ServeHTTP(w, req)
		})
	})
	api.Get("/x", ok)
	r.MethodNotAllowed(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Previous-405", "yes")
		w.WriteHeader(http.StatusMethodNotAllowed)
	}))
	r.WrapMethodNotAllowed(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if strings.HasPrefix(req.URL.Path, "/claimed/") || strings.HasPrefix(req.URL.Path, "/grp/") {
				w.Header().Set("X-Seen-Allow", w.Header().Get("Allow"))
				w.WriteHeader(http.StatusTeapot)
				return
			}
			next.ServeHTTP(w, req)
		})
	})

	for _, tc := range []struct {
		path      string
		code      int
		previous  string
		groupSeen string
	}{
		{"/claimed/x", http.StatusTeapot, "", ""},
		{"/grp/x", http.StatusTeapot, "", "yes"},
		{"/other", http.StatusMethodNotAllowed, "yes", ""},
	} {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, tc.path, nil))
		if rec.Code != tc.code || rec.Header().Get("X-Previous-405") != tc.previous || rec.Header().Get("X-Group-Chain") != tc.groupSeen {
			t.Errorf("PUT %s = %d previous=%q group=%q, want %d %q %q", tc.path, rec.Code,
				rec.Header().Get("X-Previous-405"), rec.Header().Get("X-Group-Chain"), tc.code, tc.previous, tc.groupSeen)
		}
		if tc.code == http.StatusTeapot && rec.Header().Get("X-Seen-Allow") != "GET, HEAD" {
			t.Errorf("PUT %s: the wrapper saw Allow %q, want GET, HEAD", tc.path, rec.Header().Get("X-Seen-Allow"))
		}
	}

	// No previous handler: next is the plain 405.
	bare := New()
	bare.Get("/x", ok)
	bare.WrapMethodNotAllowed(func(next http.Handler) http.Handler { return next })
	rec := httptest.NewRecorder()
	bare.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/x", nil))
	if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != "GET, HEAD" {
		t.Errorf("bare 405 = %d Allow %q", rec.Code, rec.Header().Get("Allow"))
	}
}
