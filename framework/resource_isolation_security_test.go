package framework

import (
	"net/http"
	"net/http/httptest"
	"testing"

	fembed "github.com/DonaldMurillo/gofastr/framework/embed"
)

func TestWithResourceIsolationRefusesCrossOriginSubresources(t *testing.T) {
	app := NewApp(WithResourceIsolation(func(r *http.Request) bool { return r.URL.Path == "/callback" }))
	for _, p := range []string{"/app.js", "/callback", fembed.LoaderPath} {
		app.Router().HandleFunc("GET", p, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	}
	app, cleanup := startApp(t, app)
	defer cleanup()

	for _, tc := range []struct {
		name, path, site string
		want             int
	}{
		{"cross-site script refused", "/app.js", "cross-site", http.StatusForbidden},
		{"same-site sibling refused", "/app.js", "same-site", http.StatusForbidden},
		{"same-origin passes", "/app.js", "same-origin", http.StatusNoContent},
		{"allow hook admits", "/callback", "cross-site", http.StatusNoContent},
		{"embed loader admitted", fembed.LoaderPath, "cross-site", http.StatusNoContent},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, tc.path, nil)
			r.Header.Set("Sec-Fetch-Site", tc.site)
			r.Header.Set("Sec-Fetch-Mode", "no-cors")
			r.Header.Set("Sec-Fetch-Dest", "script")
			w := httptest.NewRecorder()
			app.router.ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatalf("status %d, want %d", w.Code, tc.want)
			}
			if w.Header().Get("Content-Security-Policy") == "" {
				t.Fatal("the answer carries no security headers: isolation must run inside SecurityHeaders")
			}
		})
	}
}

func TestResourceIsolationIsOffByDefault(t *testing.T) {
	app := NewApp()
	app.Router().HandleFunc("GET", "/app.js", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	app, cleanup := startApp(t, app)
	defer cleanup()
	r := httptest.NewRequest(http.MethodGet, "/app.js", nil)
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	r.Header.Set("Sec-Fetch-Mode", "no-cors")
	w := httptest.NewRecorder()
	app.router.ServeHTTP(w, r)
	if w.Code != http.StatusNoContent {
		t.Fatalf("status %d, want 204 without WithResourceIsolation", w.Code)
	}
}

func TestWithResourceIsolationPanicsWithoutDefaults(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("WithResourceIsolation with WithoutDefaultMiddleware did not panic")
		}
	}()
	app := NewApp(WithoutDefaultMiddleware(), WithResourceIsolation(nil))
	_, cleanup := startApp(t, app)
	cleanup()
}
