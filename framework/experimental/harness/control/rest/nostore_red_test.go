//go:build red

package rest

// RED TEST — open finding, 2026-09-06/07 adversarial round 5, phase 2
// (family enumeration; tier T3 CONTRACT-QUESTION).
// CONTRACT-QUESTION red: operator-local dev tool and bearer-header auth is
// RFC-protected in conforming shared caches — but the mcpserver twin in the
// same control tree pins no-store for its session-bound JSON (http.go:93-95
// "session-bound JSON-RPC / SSE responses, never cacheable by
// intermediaries"); delete if maintainer keeps the REST arm cacheable.
// Property: the harness REST control API's per-owner JSON GETs carry
// Cache-Control: no-store.
// Surfaces: framework/experimental/harness/control/rest/rest.go::
// writeJSON :418-421 — backs GET /v1/sessions, /v1/sessions/, /v1/profiles,
// /v1/providers, /v1/tools, /v1/skills, /v1/slash-commands (auth:
// X-Harness-Token/Bearer). Only the SSE arm at :354 sets any Cache-Control.
// Finding: bfcache and non-conforming proxies retain one owner's session
// metadata/histories.
// Fix direction: no-store at the handle() wrapper, mcpserver's shape.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control/auth"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/ids"
)

func TestRestJSONRedNoStore(t *testing.T) {
	s := newServer(t)
	sess := ids.NewSessionID()
	tok, err := s.Encoder.Encode(auth.Claims{
		Ver:      auth.VerCurrent,
		JTI:      ids.NewJTI(),
		Sessions: []ids.SessionID{sess},
	})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/sessions", nil)
	req.Header.Set("X-Harness-Token", tok)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("setup broken: /v1/sessions status %d: %.200s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("setup broken: content type %q", rec.Header().Get("Content-Type"))
	}
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "no-store") {
		t.Errorf("SECURITY: [harness-rest-nostore] GET /v1/sessions (per-owner session inventory) answers with Cache-Control %q — writeJSON sets only Content-Type while the mcpserver twin in the same control tree pins no-store for session-bound JSON", cc)
	}
}
