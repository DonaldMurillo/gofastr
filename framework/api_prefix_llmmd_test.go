package framework

import (
	"database/sql"
	"net/http"
	"strings"
	"testing"
)

// #503: under WithAPIPrefix("/api") the /api/llm.md index linked each
// resource to /<table>/llm.md and listed its base path unprefixed, so an
// agent following the index landed on 404s. The index links must be the
// paths the routes are mounted at, and each link must answer.
func TestLLMIndexLinksUnderAPIPrefix(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := prefixedTicketsApp(t, db, "/api")
		stop := covStartAndStop(t, app)
		defer stop()

		h := TestHarness(t, app)
		index := h.Get("/api/llm.md")
		if index.Status() != http.StatusOK {
			t.Fatalf("GET /api/llm.md = %d, want 200", index.Status())
		}
		md := index.Body()
		if !strings.Contains(md, "[tickets](/api/tickets/llm.md)") {
			t.Errorf("index should link the prefixed per-entity page:\n%s", md)
		}
		if !strings.Contains(md, "`/api/tickets`") {
			t.Errorf("index Base Path should carry the prefix:\n%s", md)
		}
		// The per-entity page is auth-gated (WithPublicOpenAPI covers the
		// index only), so the anonymous answer is 401. 404 is the bug.
		if got := h.Get("/api/tickets/llm.md").Status(); got == http.StatusNotFound {
			t.Errorf("GET /api/tickets/llm.md = 404: the index links a route that is not mounted")
		}
	})
}
