package uihost

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
)

// Every partial-shaped response carries Cache-Control: no-store, including
// the branches that return before the success path's unconditional Set:
// handlePartialPage's own threat model (per-user rendered HTML, re-mint
// Set-Cookie pairs) applies to the prefetch 204 and the policy-redirect
// 303 exactly as it does to the 200 body, and a shared cache configured to
// store no-freshness responses would otherwise key either on the URL.

// TestPartialEarlyExitsCarryNoStore pins both early-exit shapes: the
// dead-session prefetch 204 and the live-session policy-redirect 303.
func TestPartialEarlyExitsCarryNoStore(t *testing.T) {
	// (a) Dead-session prefetch: the 204 used to return before any
	// no-store was set, shipping an anonymous, header-gated empty
	// response with no cache suppression.
	{
		ds := actionsHost()
		req := httptest.NewRequest("GET", "/act", nil)
		req.Header.Set("X-Gofastr-Navigate", "1")
		req.Header.Set("X-Gofastr-Prefetch", "1")
		w := httptest.NewRecorder()
		ds.ServeHTTP(w, req)
		if w.Code != http.StatusNoContent {
			t.Fatalf("prefetch with dead session: status %d, want 204", w.Code)
		}
		if cc := w.Header().Get("Cache-Control"); !strings.Contains(cc, "no-store") {
			t.Errorf("SECURITY: [uihost] prefetch 204 Cache-Control = %q, want no-store — "+
				"the 204 returns ahead of the success path's unconditional no-store, so an "+
				"anonymous, header-gated empty response ships with no cache suppression and a "+
				"shared cache configured to store no-freshness responses can blank any app route "+
				"for later visitors", cc)
		}
	}

	// (b) Live session + policy redirect with an unsafe URL: the re-mint
	// block is skipped (no second no-store) and the hard 303 arm returns
	// before the unconditional one; a cached 303 replays its Location
	// onto later visitors' navigations of that URL.
	{
		pol := app.PolicyFunc(func(ctx context.Context) app.Decision {
			return app.Decision{Kind: app.DecisionRedirect, URL: "https://evil.example/login"}
		})
		application := app.NewApp("t")
		application.RegisterScreen(
			app.NewScreen("/dash", &testHomeComp{}).WithPolicy(pol),
			nil,
		)
		ds := New(application)

		sess := ds.CreateSession()
		req := httptest.NewRequest("GET", "/dash", nil)
		req.Header.Set("X-Gofastr-Navigate", "1")
		req.AddCookie(&http.Cookie{Name: sessionCookieSecureName, Value: sess.Token})
		req.AddCookie(&http.Cookie{Name: sessionCookieDevName, Value: sess.Token})
		w := httptest.NewRecorder()
		ds.ServeHTTP(w, req)
		if w.Code != http.StatusSeeOther {
			t.Fatalf("policy redirect partial: status %d, want 303 (unsafe redirect URL falls back to a hard redirect)", w.Code)
		}
		if cc := w.Header().Get("Cache-Control"); !strings.Contains(cc, "no-store") {
			t.Errorf("SECURITY: [uihost] partial 303 Cache-Control = %q, want no-store — "+
				"with a live session the re-mint no-store never runs and the 303 arm returns "+
				"before the unconditional one, so the redirect ships with no cache suppression "+
				"and a shared cache can replay its Location onto later visitors' navigations", cc)
		}
	}
}
