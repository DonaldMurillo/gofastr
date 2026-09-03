//go:build red

// RED TEST — open finding, 2026-09-03 adversarial pass round 4 (tests-only;
// no fix applied).
//
// Property: every browser-facing response kiln serves carries
// X-Content-Type-Options: nosniff — the repo's own always-on discipline
// (docs/content/security.md; app_middleware_test.go pins the default chain;
// the harness web client and the relay set it by hand).
//
// Surfaces: the chat panel widget mounted on the Live aux router by
// chat.MountPanel (panel.go:47, cmd/kiln main.go:211) — chrome HTML fragment
// at /core-ui/widget/kiln-panel/chrome, JSON state at
// /core-ui/widget/kiln-panel/state, stylesheet at
// /core-ui/widget/kiln-panel/style.css. The aux router is a bare
// core/router with no default middleware chain, so none of them carry
// nosniff. Distinct surface from kiln/live's fallback page (the other half
// of the same parity gap, pinned separately in kiln/live).
//
// Finding: the panel chrome is HTML rendered from live session/world state
// and injected into the operator's page; the state endpoint is JSON over
// which the panel re-renders the chat log. In a production app these same
// widget endpoints ride the framework chain and get nosniff; on kiln's aux
// router they get nothing. Content-Type confusion on a same-origin JSON
// endpoint that feeds innerHTML-style re-renders is exactly where the
// nosniff belt is supposed to sit.
//
// Fix direction: wrap the aux router (or the widget/chat mounts) in the
// same security-header middleware the default chain applies — one wrapper
// at the kiln server covers panel, widget assets, and RPC surfaces at once.
//
// Severity: low, labeled honestly — loopback dev tool, same-origin surfaces
// behind originGuard. Pinned as parity with the repo's documented
// "nosniff always" contract, not as a standalone exploit.

package chat_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/kiln/chat"
)

func TestChatRedPanelNoSniff(t *testing.T) {
	l, tools := setup(t) // from chat_test.go: live + chat server + fallback
	chat.MountPanel(l.Aux(), l, tools, func() any { return nil })

	for _, path := range []string{
		"/core-ui/widget/kiln-panel/chrome", // HTML fragment from live session state
		"/core-ui/widget/kiln-panel/state",  // JSON the panel re-renders from
		"/core-ui/widget/kiln-panel/style.css",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Accept", "text/html,*/*")
		rec := httptest.NewRecorder()
		l.ServeHTTP(rec, req)
		res := rec.Result()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("%s: status %d — surface moved, revisit this pin", path, res.StatusCode)
		}
		if ct := res.Header.Get("Content-Type"); ct == "" {
			t.Fatalf("%s: no Content-Type at all — wrong surface", path)
		}
		if got := res.Header.Get("X-Content-Type-Options"); got != "nosniff" {
			t.Errorf("SECURITY: %s (Content-Type %q) served without X-Content-Type-Options (got %q). "+
				"The panel surfaces render live session/world state into the operator's page; in a production app "+
				"the same widget endpoints ride the framework chain and carry nosniff — on kiln's bare aux router "+
				"they carry nothing. Fix: wrap the aux router in the default chain's security-header middleware.",
				path, res.Header.Get("Content-Type"), got)
		}
	}
}
