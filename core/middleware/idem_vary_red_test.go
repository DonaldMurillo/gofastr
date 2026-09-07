//go:build red

package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// RED TEST — open finding, 2026-09-06 adversarial pass (round 5;
// tests-only; no fix applied).
// Property: middleware that varies its response on a request header APPENDS
// to Vary, never Set — the convention TestCORS_AppendsVaryInsteadOfClobbering
// pins (cors.go: "Add, not Set: an upstream middleware may already have
// written Vary") applies to every Vary writer in a chain.
// Surfaces: core/middleware/idempotency.go body-too-large bypass arm —
// w.Header().Set("Vary", "Idempotency-Key"), the only Set("Vary") in core/.
// Finding: chained CORS(...)(Idempotency(IdempotencyConfig{MaxBodyBytes:
// small, ...})), POST over-cap body with Origin + Idempotency-Key headers →
// response carries ACAO (CORS ran) but Vary lists ONLY Idempotency-Key — the
// Set clobbered Vary: Origin; a shared cache can pin one origin's CORS
// variant and serve it to another origin.
// Fix direction: Add, not Set, mirroring cors.go's append rule.

func TestIdempotencyRedVaryAppendNotSet(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := CORS(CORSConfig{AllowedOrigins: []string{"https://ok.example"}})(
		Idempotency(IdempotencyConfig{
			MaxBodyBytes: 16,
			Principal:    testPrincipal,
		})(inner),
	)

	// 64-byte body against a 16-byte cap: the request must take the
	// body-too-large bypass arm, which is the arm that writes Vary.
	req := httptest.NewRequest(http.MethodPost, "/orders", strings.NewReader(strings.Repeat("x", 64)))
	req.Header.Set("Origin", "https://ok.example")
	req.Header.Set(IdempotencyKeyHeader, "vary-clobber-1")
	req.Header.Set("X-Caller", "alice") // testPrincipal reads X-Caller
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if acao := rec.Header().Get("Access-Control-Allow-Origin"); acao != "https://ok.example" {
		t.Fatalf("setup broken: CORS arm did not run (ACAO=%q, status=%d)", acao, rec.Code)
	}
	if rec.Header().Get("Idempotent-Bypass") != "body-too-large" {
		t.Fatalf("setup broken: request did not take the body-too-large bypass arm (headers=%v, status=%d)",
			rec.Header(), rec.Code)
	}

	// Vary-collection helper mirrored from TestCORS_AppendsVaryInsteadOfClobbering.
	vary := rec.Header().Values("Vary")
	var sawOrigin, sawIdemKey bool
	for _, v := range vary {
		low := strings.ToLower(v)
		if strings.Contains(low, "origin") {
			sawOrigin = true
		}
		if strings.Contains(low, "idempotency-key") {
			sawIdemKey = true
		}
	}
	if !sawOrigin || !sawIdemKey {
		t.Errorf("SECURITY: [idem-vary-clobber] Idempotency's bypass arm Set() Vary and clobbered "+
			"CORS's Vary: Origin; got %v. Attack: a shared cache keys variants on Vary; without "+
			"Origin listed it can pin one origin's CORS response variant and serve it to another origin.",
			vary)
	}
}
