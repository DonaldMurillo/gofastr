package framework

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

// App.View mounts a view's entity read-only (List, Get, _events), and
// llm.md says so (#358), but /openapi.json asked only whether CRUD was
// mounted, so it documented create, update, delete and the _batch family
// on every view. An SDK built from it shipped six calls that answer 405.
//
// The property: every operation the spec lists for the view is a route
// the server serves (no 404/405), and the read routes are still listed.
func TestSpecListsOnlyAViewsReadRoutes(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := prefixedTicketsApp(t, db, "")
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

		rec := httptest.NewRecorder()
		app.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/openapi.json", nil))
		var doc struct {
			Paths map[string]map[string]any `json:"paths"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
			t.Fatal(err)
		}
		listed := map[string]bool{}
		for path, ops := range doc.Paths {
			if !strings.HasPrefix(path, "/open_tickets") {
				continue
			}
			for method := range ops {
				op := strings.ToUpper(method) + " " + path
				listed[op] = true
				url := strings.ReplaceAll(path, "{id}", "x1")
				var body *strings.Reader
				if method == "get" {
					body = strings.NewReader("")
				} else {
					body = strings.NewReader(`{"title":"t","items":[{"id":"x1","title":"t"}],"ids":["x1"]}`)
				}
				if strings.HasSuffix(path, "/_events") {
					continue // a stream; its mount is checked by the read set below
				}
				req := httptest.NewRequest(strings.ToUpper(method), url, body)
				req.Header.Set("Content-Type", "application/json")
				w := httptest.NewRecorder()
				req = req.WithContext(handler.SetUser(req.Context(), struct{ ID string }{ID: "u1"}))
				app.router.ServeHTTP(w, req)
				if w.Code == http.StatusMethodNotAllowed || (w.Code == http.StatusNotFound && !strings.Contains(path, "{id}")) {
					t.Errorf("spec lists %s, the server answers %d", op, w.Code)
				}
			}
		}
		for _, want := range []string{"GET /open_tickets", "GET /open_tickets/{id}", "GET /open_tickets/_events"} {
			if !listed[want] {
				t.Errorf("spec dropped the view's read route %s (listed %v)", want, listed)
			}
		}
		for _, banned := range []string{"POST /open_tickets", "PUT /open_tickets/{id}", "PATCH /open_tickets/{id}", "DELETE /open_tickets/{id}", "POST /open_tickets/_batch"} {
			if listed[banned] {
				t.Errorf("spec lists %s on a read-only view", banned)
			}
		}
	})
}
