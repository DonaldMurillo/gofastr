package apiversions_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/framework/experimental/apiversions"
)

// Replacement URLs flow into the Link response header as clickable
// successor hints. A `javascript:`/`data:`/`mailto:` there is a phishing
// primitive once the docs viewer or SDK generator renders the link.
// Encoded CR/LF is a header-smuggling primitive on consumers that
// decode the URL.
var unsafeReplacements = []string{
	"javascript:alert(1)",
	"data:text/html,<svg/onload=1>",
	"file:///etc/passwd",
	"mailto:attacker@example.com",
	"//evil.example/upgrade",
	"https://example.com/%0d%0aX-Injected:1",
}

func TestDeprecationHeaders_RejectsUnsafeReplacement(t *testing.T) {
	sunset := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	for _, payload := range unsafeReplacements {
		t.Run(payload, func(t *testing.T) {
			rec := httptest.NewRecorder()
			apiversions.DeprecationHeaders(rec, sunset, payload)
			if got := rec.Header().Get("Link"); got != "" {
				t.Fatalf("DeprecationHeaders kept unsafe replacement %q in Link: %q", payload, got)
			}
		})
	}
}

func TestDeprecationMiddleware_RejectsUnsafeReplacement(t *testing.T) {
	sunset := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	for _, payload := range unsafeReplacements {
		t.Run(payload, func(t *testing.T) {
			r := router.New()
			v1 := apiversions.Version(r, "v1", apiversions.WithDeprecation(sunset, payload))
			v1.Use(v1.DeprecationMiddleware())
			v1.Router().Get("/test", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/test", nil))
			if got := rec.Header().Get("Link"); got != "" {
				t.Fatalf("DeprecationMiddleware kept unsafe replacement %q in Link: %q", payload, got)
			}
		})
	}
}

// Sanity: a safe URL (http/https or relative) still flows through.
func TestDeprecation_SafeReplacementSurvives(t *testing.T) {
	for _, ok := range []string{"https://api.example.com/v2", "/v2", "./v2"} {
		t.Run(ok, func(t *testing.T) {
			rec := httptest.NewRecorder()
			apiversions.DeprecationHeaders(rec, time.Time{}, ok)
			link := rec.Header().Get("Link")
			if !strings.Contains(link, ok) {
				t.Fatalf("DeprecationHeaders dropped safe replacement %q (link=%q)", ok, link)
			}
		})
	}
}
