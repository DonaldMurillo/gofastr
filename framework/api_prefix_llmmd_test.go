package framework

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

// #503: under WithAPIPrefix("/api") the /api/llm.md index linked each
// resource to /<table>/llm.md and listed its base path unprefixed, so an
// agent following the index landed on 404s. The index links must be the
// paths the routes are mounted at, and each link must answer.
func TestLLMIndexLinksUnderAPIPrefix(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := prefixedTicketsApp(t, db, "/api")
		// CRUD off, one relative custom endpoint: it mounts under the
		// prefix (/api/reports/summary), so its index row must too.
		app.Entity("reports", entity.EntityConfig{
			Fields:   []schema.Field{{Name: "title", Type: schema.String}},
			Exposure: &entity.ExposureConfig{CRUD: new(false)},
			Endpoints: []entity.Endpoint{{Method: http.MethodGet, Path: "summary",
				Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {})}},
		})
		// A read-only view mounts under the prefix too, through App.View,
		// which records no CRUD mount of its own.
		app.View(migrate.View{
			Name:      "open_tickets",
			Select:    "SELECT id, title FROM tickets",
			DependsOn: []string{"tickets"},
			Columns: []migrate.Column{
				{Name: "id", Type: schema.String, PrimaryKey: true},
				{Name: "title", Type: schema.String},
			},
		})
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
		if !strings.Contains(md, "[open_tickets](/api/open_tickets/llm.md)") {
			t.Errorf("index should link the view's prefixed page:\n%s", md)
		}
		if !strings.Contains(md, "`/api/reports`") {
			t.Errorf("index should list the CRUD-off entity under the prefix:\n%s", md)
		}
		if !strings.Contains(md, "`/api/tickets`") {
			t.Errorf("index Base Path should carry the prefix:\n%s", md)
		}
		// The per-entity page is auth-gated (WithPublicOpenAPI covers the
		// index only), so the anonymous answer is 401. 404 is the bug.
		if got := h.Get("/api/tickets/llm.md").Status(); got != http.StatusUnauthorized {
			t.Errorf("anonymous GET /api/tickets/llm.md = %d, want 401 (404 means the index links a route that is not mounted)", got)
		}

		// The page itself must name the routes where they live: an agent
		// that reads "GET /tickets" from it gets the same 404 the index
		// used to hand out.
		page := signedInGet(t, app, "/api/tickets/llm.md")
		for _, want := range []string{"Resource: `/api/tickets`", "### GET /api/tickets\n", "### POST /api/tickets/_batch"} {
			if !strings.Contains(page, want) {
				t.Errorf("per-entity llm.md missing %q:\n%s", want, page)
			}
		}
		if strings.Contains(page, "### GET /tickets") {
			t.Errorf("per-entity llm.md lists the unprefixed path:\n%s", page)
		}
		if view := signedInGet(t, app, "/api/open_tickets/llm.md"); !strings.Contains(view, "### GET /api/open_tickets\n") {
			t.Errorf("view llm.md lists the unprefixed path:\n%s", view)
		}
	})
}

func signedInGet(t *testing.T, app *App, path string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, path, nil)
	r = r.WithContext(handler.SetUser(r.Context(), struct{ ID string }{ID: "u1"}))
	app.Router().ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s = %d, want 200:\n%s", path, rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}
