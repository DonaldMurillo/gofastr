//go:build red

package uihost

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// RED TEST — open finding, 2026-09-06 adversarial pass (round 5; tests-only; no fix applied).
// Property: a page whose body is per-user carries Cache-Control: no-store —
// every sibling arm already does (handlePartialPage sets no-store before ANY
// branch, pinned by partial_security_test.go; the handlePage re-mint arm sets
// it with the comment "SSR HTML is per-user anyway; make it explicit";
// RenderScreen serves private screens no-store by default; the embed content
// route is no-store).
// Surfaces: framework/uihost/uihost.go::handlePage live-session arm (~1401-1431):
// verifySessionToken succeeds → the whole no-store block at 1402-1411 is
// skipped, and the 200 that ships at 1431 carries neither Cache-Control nor
// Vary. That page embeds per-user data: SSR HTML rendered under the caller's
// policy/user context, chrome /__gofastr/sse?session=<id> (uihost.go:1792),
// and RequireAuthenticated widget chrome SSR-inlined with request context
// (the control pinned by widget_gate_security_test.go step 3).
// Finding: a request whose session cookie VERIFIES gets the full page with no
// cache suppression at all, so any shared intermediary configured to store
// no-freshness responses and keying on URL alone serves visitor B user A's
// page — the exact threat model partial_security_test.go states for the
// partial shape, one branch over.
// Fix direction: set Cache-Control: no-store (with Vary: Cookie) before the
// live/dead session split, exactly like handlePartialPage does.

func TestPageLiveSessionRedNoStore(t *testing.T) {
	ds := actionsHost()
	sess := ds.CreateSession()
	req := httptest.NewRequest("GET", "/plain", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieSecureName, Value: sess.Token})
	req.AddCookie(&http.Cookie{Name: sessionCookieDevName, Value: sess.Token})
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("setup broken: live-session page status %d, want 200", w.Code)
	}
	if !strings.Contains(w.Body.String(), sess.ID) {
		t.Fatalf("setup broken: page body does not embed the session id — premise of the per-user payload does not hold")
	}
	if cc := w.Header().Get("Cache-Control"); !strings.Contains(cc, "no-store") {
		t.Errorf("SECURITY: [uihost-pagecache] live-session page 200 Cache-Control = %q, want no-store — "+
			"the verified-cookie arm of handlePage skips the re-mint block's no-store entirely and ships a "+
			"per-user page (caller-context SSR HTML, /__gofastr/sse?session=<id> chrome, gated widget chrome) "+
			"with no cache suppression, so a shared cache that stores no-freshness responses keyed on URL alone "+
			"can serve visitor B this session's page", cc)
	}
}
