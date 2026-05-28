package uihost

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
)

func TestUIHost_PageLLMIndexDisabledByDefault(t *testing.T) {
	ds := newTestUIHost()
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/llm-pages.md", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("SECURITY: [uihost-llm] /llm-pages.md returned %d and exposed %q. Attack: route inventory is public by default.", rec.Code, rec.Body.String())
	}
}

func TestUIHost_PageLLMScreenDocDisabledByDefault(t *testing.T) {
	ds := newTestUIHostWithMultipleRoutes()
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/about/llm.md", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("SECURITY: [uihost-llm] per-screen llm.md returned %d and exposed %q. Attack: page-specific docs are public by default.", rec.Code, rec.Body.String())
	}
}

func TestUIHost_CreateSessionGETRejected(t *testing.T) {
	ds := newTestUIHost()
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/__gofastr/session", nil))

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("SECURITY: [uihost-session] GET /__gofastr/session returned %d. Attack: session minting is exposed to GET/CSRF/caching semantics.", rec.Code)
	}
}

func TestUIHost_NotFoundCarriesFrameDenyHeader(t *testing.T) {
	ds := newTestUIHost()
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/missing-page", nil))

	if rec.Header().Get("X-Frame-Options") != "DENY" {
		t.Fatalf("SECURITY: [uihost-404] not-found response missing X-Frame-Options DENY: %#v", rec.Header())
	}
}

func TestUIHost_NotFoundCarriesContentSecurityPolicy(t *testing.T) {
	ds := newTestUIHost()
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/missing-page", nil))

	if rec.Header().Get("Content-Security-Policy") == "" {
		t.Fatalf("SECURITY: [uihost-404] not-found response missing Content-Security-Policy header: %#v", rec.Header())
	}
}

func TestUIHost_NotFoundCarriesNoSniffHeader(t *testing.T) {
	ds := newTestUIHost()
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/missing-page", nil))

	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("SECURITY: [uihost-404] not-found response missing X-Content-Type-Options nosniff: %#v", rec.Header())
	}
}

func TestUIHost_PageResponsesCarryNoSniffHeader(t *testing.T) {
	ds := newTestUIHost()
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("SECURITY: [uihost-headers] page response missing X-Content-Type-Options nosniff: %#v", rec.Header())
	}
}

func TestUIHost_PageResponsesCarryReferrerPolicy(t *testing.T) {
	ds := newTestUIHost()
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Header().Get("Referrer-Policy") == "" {
		t.Fatalf("SECURITY: [uihost-headers] page response missing Referrer-Policy header: %#v", rec.Header())
	}
}

func TestUIHost_RuntimeJSCarriesNoSniffHeader(t *testing.T) {
	ds := newTestUIHost()
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/__gofastr/runtime.js", nil))

	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("SECURITY: [uihost-runtime] runtime.js missing X-Content-Type-Options nosniff: %#v", rec.Header())
	}
}

func TestUIHost_ColorSchemeJSCarriesNoSniffHeader(t *testing.T) {
	ds := newTestUIHost()
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/__gofastr/color-scheme.js", nil))

	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("SECURITY: [uihost-runtime] color-scheme.js missing X-Content-Type-Options nosniff: %#v", rec.Header())
	}
}

func TestUIHost_AppCSSCarriesNoSniffHeader(t *testing.T) {
	ds := newTestUIHost()
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/__gofastr/app.css", nil))

	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("SECURITY: [uihost-runtime] app.css missing X-Content-Type-Options nosniff: %#v", rec.Header())
	}
}

func TestUIHost_ActionsJSCarriesNoSniffHeader(t *testing.T) {
	application := app.NewApp("actions-nosniff")
	application.RegisterScreen(app.NewScreen("/", &testHomeComp{}).WithTitle("Home"), nil)
	ds := New(application)

	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/__gofastr/actions.js", nil))

	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("SECURITY: [uihost-runtime] actions.js missing X-Content-Type-Options nosniff: %#v", rec.Header())
	}
}

func TestUIHost_WidgetCatalogRequiresAuth(t *testing.T) {
	ds := newTestUIHost()
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/__gofastr/widgets", nil))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("SECURITY: [uihost-widgets] widget catalog returned %d and exposed %q. Attack: infrastructure/widget inventory is public by default.", rec.Code, rec.Body.String())
	}
}

func TestUIHost_RuntimeModuleCarriesNoSniffHeader(t *testing.T) {
	ds := newTestUIHost()
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/__gofastr/runtime/widgets.js", nil))

	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("SECURITY: [uihost-runtime] split runtime module missing X-Content-Type-Options nosniff: %#v", rec.Header())
	}
}
