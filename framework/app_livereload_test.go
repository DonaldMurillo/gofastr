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

func TestNewAppSkipsLiveReloadInProduction(t *testing.T) {
	t.Setenv("GOFASTR_DEV", "1")
	t.Setenv("GOFASTR_ENV", "production")

	app := NewApp(WithoutDefaultMiddleware())

	req := httptest.NewRequest(http.MethodGet, "/__livereload.js", nil)
	rr := httptest.NewRecorder()
	app.Router().ServeHTTP(rr, req)

	if rr.Code == http.StatusOK {
		t.Fatalf("/__livereload.js served in production env (status=%d) — must be 404", rr.Code)
	}
}

func TestNewAppLiveReloadOptOut(t *testing.T) {
	t.Setenv("GOFASTR_DEV", "1")
	t.Setenv("GOFASTR_DEV_LIVERELOAD", "0")
	t.Setenv("GOFASTR_ENV", "")

	app := NewApp(WithoutDefaultMiddleware())

	req := httptest.NewRequest(http.MethodGet, "/__livereload.js", nil)
	rr := httptest.NewRecorder()
	app.Router().ServeHTTP(rr, req)

	if rr.Code == http.StatusOK {
		t.Fatalf("/__livereload.js served despite opt-out (status=%d)", rr.Code)
	}
}

func TestNewAppLiveReloadOffWithoutDev(t *testing.T) {
	t.Setenv("GOFASTR_DEV", "")
	t.Setenv("GOFASTR_ENV", "")
	t.Setenv("GOFASTR_DEV_LIVERELOAD", "")

	app := NewApp(WithoutDefaultMiddleware())

	req := httptest.NewRequest(http.MethodGet, "/__livereload.js", nil)
	rr := httptest.NewRecorder()
	app.Router().ServeHTTP(rr, req)

	if rr.Code == http.StatusOK {
		t.Fatalf("/__livereload.js served without GOFASTR_DEV (status=%d)", rr.Code)
	}
}
