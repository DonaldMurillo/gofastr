package framework

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// NewApp must auto-register /__livereload + /__livereload.js when GOFASTR_DEV=1
// and the opt-out env var is not set. Production env always wins (kept off
// even if GOFASTR_DEV slips through).
func TestNewAppAutoWiresLiveReloadWhenDev(t *testing.T) {
	t.Setenv("GOFASTR_DEV", "1")
	t.Setenv("GOFASTR_DEV_LIVERELOAD", "")
	t.Setenv("GOFASTR_ENV", "")

	app := NewApp(WithoutDefaultMiddleware())

	req := httptest.NewRequest(http.MethodGet, "/__livereload.js", nil)
	rr := httptest.NewRecorder()
	app.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("/__livereload.js status=%d, want 200", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "EventSource") {
		t.Fatalf("script body missing EventSource:\n%s", rr.Body.String())
	}
}

// livereloadNotServed boots an app under env and pins that the
// livereload script is not served, for the reason named by why.
func livereloadNotServed(t *testing.T, why string, env map[string]string) {
	t.Helper()
	for k, v := range env {
		t.Setenv(k, v)
	}
	rr := httptest.NewRecorder()
	NewApp(WithoutDefaultMiddleware()).Router().ServeHTTP(rr,
		httptest.NewRequest(http.MethodGet, "/__livereload.js", nil))
	if rr.Code == http.StatusOK {
		t.Fatalf("/__livereload.js served %s (status=%d)", why, rr.Code)
	}
}

func TestNewAppSkipsLiveReloadInProduction(t *testing.T) {
	livereloadNotServed(t, "in production env — must be 404", map[string]string{
		"GOFASTR_DEV": "1",
		"GOFASTR_ENV": "production",
	})
}

func TestNewAppLiveReloadOptOut(t *testing.T) {
	livereloadNotServed(t, "despite opt-out", map[string]string{
		"GOFASTR_DEV":            "1",
		"GOFASTR_DEV_LIVERELOAD": "0",
		"GOFASTR_ENV":            "",
	})
}

func TestNewAppLiveReloadOffWithoutDev(t *testing.T) {
	livereloadNotServed(t, "without GOFASTR_DEV", map[string]string{
		"GOFASTR_DEV":            "",
		"GOFASTR_ENV":            "",
		"GOFASTR_DEV_LIVERELOAD": "",
	})
}

func TestNewAppWiresLayoutLayerWhenDev(t *testing.T) {
	t.Setenv("GOFASTR_DEV", "1")
	t.Setenv("GOFASTR_DEV_LAYOUT", "")
	t.Setenv("GOFASTR_ENV", "")

	rr := httptest.NewRecorder()
	NewApp(WithoutDefaultMiddleware()).Router().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/__devlayout.js", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("/__devlayout.js status=%d, want 200", rr.Code)
	}

	t.Setenv("GOFASTR_ENV", "production")
	rr = httptest.NewRecorder()
	NewApp(WithoutDefaultMiddleware()).Router().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/__devlayout.js", nil))
	if rr.Code == http.StatusOK {
		t.Fatalf("/__devlayout.js served in production")
	}
}
