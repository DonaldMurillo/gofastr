package framework

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// A wrong method on an API route ran the UI host's 405: a themed HTML
// page, and a guest session minted on the way, while every other API
// error is JSON. Under the API namespace the 405 is now a problem
// document carrying the router's Allow; outside it the previous handler
// still answers.
func TestAPIWrongMethodIsProblemJSON(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware(), WithConfig(AppConfig{Name: "api405"}))
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {})
	app.Router().Get("/api/things", ok)
	app.Router().Group("/api/grp").Get("/x", ok)
	app.Router().Get("/page", ok)
	app.Router().MethodNotAllowed(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "gofastr-session", Value: "minted"})
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("X-Previous-405", "yes")
		w.WriteHeader(http.StatusMethodNotAllowed)
	}))
	a, cleanup := startApp(t, app)
	defer cleanup()

	for _, path := range []string{"/api/things", "/api/grp/x"} {
		rec := httptest.NewRecorder()
		a.router.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, path, nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("PUT %s = %d, want 405", path, rec.Code)
			continue
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/problem+json") {
			t.Errorf("PUT %s: Content-Type %q, want application/problem+json", path, ct)
		}
		if rec.Header().Get("X-Previous-405") != "" || rec.Header().Get("Set-Cookie") != "" {
			t.Errorf("PUT %s reached the previous 405 (Set-Cookie %q)", path, rec.Header().Get("Set-Cookie"))
		}
		if got := rec.Header().Get("Allow"); got != "GET, HEAD" {
			t.Errorf("PUT %s: Allow %q, want GET, HEAD", path, got)
		}
		var doc map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
			t.Errorf("PUT %s: body is not JSON: %v (%s)", path, err, rec.Body.String())
			continue
		}
		if status, _ := doc["status"].(float64); int(status) != http.StatusMethodNotAllowed || doc["reason"] != "api_method_not_allowed" {
			t.Errorf("PUT %s: problem = %v", path, doc)
		}
	}
	rec := httptest.NewRecorder()
	a.router.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/page", nil))
	if rec.Header().Get("X-Previous-405") != "yes" {
		t.Errorf("PUT /page did not reach the previous 405 (code %d)", rec.Code)
	}
}

// The same through a real UI host, whose 405 page minted the session.
func TestAPIWrongMethodMintsNoSession(t *testing.T) {
	fwApp := NewUIHostApp(uihost.New(app.NewApp("api405host")), WithConfig(AppConfig{Name: "api405host"}))
	fwApp.Router().Get("/api/things", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	a, cleanup := startApp(t, fwApp)
	defer cleanup()
	rec := httptest.NewRecorder()
	a.router.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/things", nil))
	if rec.Code != http.StatusMethodNotAllowed || !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/problem+json") {
		t.Errorf("PUT /api/things = %d %q, want 405 problem+json", rec.Code, rec.Header().Get("Content-Type"))
	}
	if c := rec.Header().Get("Set-Cookie"); c != "" {
		t.Errorf("PUT /api/things minted a session: %q", c)
	}
}
