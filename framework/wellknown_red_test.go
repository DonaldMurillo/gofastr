//go:build red

// RED TEST — open finding, 2026-09-03 round-8 adversarial pass
// (tests-only; no fix applied).
// Property: a response whose body embeds request-derived absolute URLs
// declares those request inputs via Vary — the contract varyWellKnown /
// writeWellKnownJSON (framework/wellknown.go:53-68) already implements
// for every other well-known document built on resolveWellKnownBase.
// Surfaces: framework/wellknown.go:handleMCPServerCard (:102-115) →
// buildMCPServerCard (:133-146) → resolveWellKnownBase (:42-51), which
// splices r.Host and the RAW X-Forwarded-Proto value into
// remotes[].url (no http/https validation, unlike every cleared XFP
// consumer elsewhere in the repo).
// Finding: handleMCPServerCard writes Content-Type and Cache-Control:
// no-cache directly and never calls varyWellKnown, so the card response
// carries NO Vary while its body varies on Host and X-Forwarded-Proto.
// A shared cache that keys only on the URL can store one caller's
// remotes[].url origin (forged X-Forwarded-Proto or foreign Host) and
// serve it to every later visitor — agents then discover an
// attacker-influenced MCP endpoint.
// Severity: LOW. no-cache asks caches to revalidate, so poisoning
// requires a cache that both skips revalidation and ignores the missing
// Vary; a cache that ignores Vary would misbehave even with Vary set.
// This is primarily a consistency pin with the three sibling well-known
// responses (api-catalog, mcp catalog, oauth-AS) that all declare Vary
// for the exact same dependency.
// Fix direction: call varyWellKnown(w) in handleMCPServerCard (or route
// the card through writeWellKnownJSON with the card media type), so
// well-formed caches key the card on the inputs its body varies on.
package framework

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestWellKnownCardRedVariesOnInputs: the MCP server card at both
// mounted paths must declare Vary on Host and X-Forwarded-Proto — the
// request inputs resolveWellKnownBase splices into remotes[].url.
func TestWellKnownCardRedVariesOnInputs(t *testing.T) {
	app, cleanup := startApp(t, NewApp(WithMCP()))
	defer cleanup()

	for _, path := range []string{"/.well-known/mcp/server-card.json", "/mcp/server-card"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Host = "cards.example"
		req.Header.Set("X-Forwarded-Proto", "https")
		rec := httptest.NewRecorder()
		app.router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status %d, want 200", path, rec.Code)
		}

		// Collect Vary field names across header lines and
		// comma-joined lists, case-insensitively (field names are
		// case-insensitive per RFC 9110).
		have := map[string]bool{}
		for _, v := range rec.Header().Values("Vary") {
			for _, f := range strings.Split(v, ",") {
				have[strings.ToLower(strings.TrimSpace(f))] = true
			}
		}
		for _, want := range []string{"host", "x-forwarded-proto"} {
			if !have[want] {
				t.Errorf("SECURITY: [wellknown] %s: Vary missing %q (Vary headers: %q) — "+
					"the card body embeds remotes[].url built from r.Host and the raw "+
					"X-Forwarded-Proto (resolveWellKnownBase), but unlike every sibling "+
					"well-known response it declares no Vary, so a shared cache keyed on "+
					"the URL alone can serve one caller's origin to everyone (LOW: needs a "+
					"cache that also ignores the no-cache revalidation request)",
					path, want, rec.Header().Values("Vary"))
			}
		}
	}
}
