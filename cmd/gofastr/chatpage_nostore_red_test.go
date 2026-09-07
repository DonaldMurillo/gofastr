//go:build red

package main

// RED TEST — open finding, 2026-09-06/07 adversarial round 5, phase 2
// (family enumeration; tier T2, T1-lean).
// Property: a GET HTML page embedding a live bearer credential is no-store
// — the magiclink credential-page pin's contract ("a cached login response
// is a cached bearer credential").
// Surfaces: cmd/gofastr/harness_http.go::chatPage :241-252 (mux '/' at
// :109-115) → renderChat embeds the 24h harness bearer token + session id
// in meta tags; the code's own comment :105-108 calls it "a
// credential-bearing response even though it takes no token to request";
// render.RespondHTML (core/render/render.go:9-12) sets only Content-Type.
// Finding: browser disk cache / bfcache on a shared machine retains the
// page and its token; the documented --addr=:8090 LAN bind widens exposure.
// Fix direction: Cache-Control: no-store before RespondHTML — exactly what
// theme_edit_page.go:55 in this package already does for a page carrying
// no token at all.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/ids"
)

func TestChatPageRedNoStore(t *testing.T) {
	sess := ids.NewSessionID()
	const token = "red-harness-token-canary" // not-a-secret: synthetic red-test fixture asserting the page embeds a token (any string works)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	chatPage(rec, req, sess, token)

	if rec.Code != http.StatusOK {
		t.Fatalf("setup broken: status %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), token) {
		t.Fatalf("setup broken: page does not embed the token — harness shape changed")
	}
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "no-store") {
		t.Errorf("SECURITY: [harness-chatpage-nostore] the harness chat page embeds a live 24h bearer token in a GET body with Cache-Control %q — the handler's own comment calls it a credential-bearing response, and a shared machine's disk cache or bfcache retains the page and its token; theme_edit_page.go:55 in this package already sets no-store for a page with no token", cc)
	}
}
