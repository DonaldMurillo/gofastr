package framework

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestWellKnownCardVariesOnInputs: the MCP server card at both mounted
// paths declares Vary on Host and X-Forwarded-Proto — the request inputs
// resolveWellKnownBase splices into remotes[].url — like every sibling
// well-known response, so a well-formed shared cache keys the card on the
// inputs its body varies on.
func TestWellKnownCardVariesOnInputs(t *testing.T) {
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
					"X-Forwarded-Proto (resolveWellKnownBase), so a shared cache keyed on "+
					"the URL alone can serve one caller's origin to everyone",
					path, want, rec.Header().Values("Vary"))
			}
		}
	}
}
