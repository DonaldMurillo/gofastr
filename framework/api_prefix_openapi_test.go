package framework

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// servedOpenAPI boots app through the real Start path, /openapi.json is
// mounted there, not by Entity(), and returns the decoded document: the
// sorted path keys, the per-path documented methods, and the server URLs.
func servedOpenAPI(t *testing.T, app *App) (paths []string, methods map[string][]string, servers []string) {
	t.Helper()
	stop := covStartAndStop(t, app)
	defer stop()

	resp := TestHarness(t, app).Get("/openapi.json")
	if resp.Status() != http.StatusOK {
		t.Fatalf("GET /openapi.json = %d, want 200", resp.Status())
	}
	var doc struct {
		Servers []struct {
			URL string `json:"url"`
		} `json:"servers"`
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal([]byte(resp.Body()), &doc); err != nil {
		t.Fatalf("decode /openapi.json: %v", err)
	}
	methods = make(map[string][]string, len(doc.Paths))
	for p, ops := range doc.Paths {
		paths = append(paths, p)
		for m := range ops {
			methods[p] = append(methods[p], strings.ToUpper(m))
		}
		sort.Strings(methods[p])
	}
	sort.Strings(paths)
	for _, s := range doc.Servers {
		servers = append(servers, s.URL)
	}
	return paths, methods, servers
}

// prefixedTicketsApp is the shape the 2026-07-26 backend eval exercised: an
// entity mounted under WithAPIPrefix, with the spec served publicly.
func prefixedTicketsApp(t *testing.T, db *sql.DB, prefix string) *App {
	t.Helper()
	opts := []AppOption{WithDB(db), WithPublicOpenAPI()}
	if prefix != "" {
		opts = append(opts, WithAPIPrefix(prefix))
	}
	app := NewApp(opts...)
	app.Entity("tickets", entity.EntityConfig{
		Table:  "tickets",
		Fields: []schema.Field{{Name: "title", Type: schema.String, Required: true}},
	}.WithTimestamps(false))
	if err := AutoMigrate(db, app.Registry); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	return app
}

// TestAPIPrefix_OpenAPIPathsMatchLiveRoutes is the regression net for the
// defect the 2026-07-26 Codex backend eval reproduced twice and ranked as its
// highest-value next move: "/openapi.json returned 200, but the document
// did not describe the live /api/tickets path".
//
// The spec used to key its paths relative to a servers[0].url of "/api", which
// is legal OpenAPI: servers + paths compose to the right URL. It is also the
// one form that misleads a consumer reading `paths` literally, and that
// consumer is the audience this framework is built for: the eval's agent AND
// its deterministic grader both concluded the document was wrong, and both
// were reading the document the way agents read documents.
//
// So the contract is now the blunt one: a path key IS the path you request.
// servers stays "/" so a servers-aware client still resolves the same URL.
func TestAPIPrefix_OpenAPIPathsMatchLiveRoutes(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := prefixedTicketsApp(t, db, "/api")
		paths, _, servers := servedOpenAPI(t, app)

		// Every documented path must be the path a client actually calls.
		want := map[string]bool{
			"/api/tickets":         true,
			"/api/tickets/{id}":    true,
			"/api/tickets/_batch":  true,
			"/api/tickets/_events": true,
		}
		for _, p := range paths {
			if !want[p] {
				t.Errorf("openapi documents path %q, which is not where the routes mount — under WithAPIPrefix(\"/api\") every path key must carry the prefix", p)
			}
			delete(want, p)
		}
		for p := range want {
			t.Errorf("openapi never documents %q, but the route is mounted there", p)
		}

		// A server entry that repeats the prefix would double it for any
		// client that composes servers + paths. Assert the count too: a
		// range over an empty slice passes vacuously, which would leave the
		// servers half of this contract untested.
		if len(servers) != 1 || servers[0] != "/" {
			t.Errorf("servers = %q, want exactly [\"/\"] — the prefix now lives in the path keys, so repeating it here doubles it to /api/api/tickets, and emitting none leaves clients without a base", servers)
		}
	})
}

// The documented path is not merely well-formed. It is reachable. This walks
// the spec and requests each key, so a path that is correct-looking but wrong
// (a missed segment, a stale plural) still fails.
func TestAPIPrefix_EveryDocumentedPathIsRoutable(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := prefixedTicketsApp(t, db, "/api")
		paths, methods, _ := servedOpenAPI(t, app)
		if len(paths) == 0 {
			t.Fatal("spec documented no paths — the assertions below would be vacuous")
		}

		for _, p := range paths {
			// {id} is a template, not a URL. Substitute a concrete value so
			// the router can match it.
			req := strings.ReplaceAll(p, "{id}", "1")
			for _, method := range methods[p] {
				rec := httptest.NewRecorder()
				r := httptest.NewRequest(method, req, strings.NewReader("{}"))
				r.Header.Set("Content-Type", "application/json")
				// _events is SSE: a matched route holds the connection open
				// until the client goes away, so an unbounded ServeHTTP here
				// hangs the suite rather than failing it. A deadline turns
				// "still streaming" into "routed", which is exactly the claim
				// under test: an unrouted path 404s at once and never
				// reaches the deadline.
				ctx, cancel := context.WithTimeout(handler.SetUser(r.Context(), struct{ ID string }{ID: "u1"}), 250*time.Millisecond)
				app.Router().ServeHTTP(rec, r.WithContext(ctx))
				cancel()
				// Only 404 is disqualifying. A 400/422 from an empty body, or
				// a 404 for "no row with id 1", both mean the route matched,
				// but "no such row" and "no such route" share a status, so a
				// templated path cannot distinguish them and is exempt.
				if rec.Code == http.StatusNotFound && !strings.Contains(p, "{id}") {
					t.Errorf("openapi documents %s %q but the router answers 404 — the spec and the router disagree", method, req)
				}
			}
		}
	})
}

// Default (no prefix) is unchanged: bare paths, server "/". This is the guard
// that the fix above did not simply move the problem.
func TestAPIPrefix_DefaultOpenAPIPathsStayBare(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := prefixedTicketsApp(t, db, "")
		paths, _, servers := servedOpenAPI(t, app)

		for _, p := range paths {
			if got := p[:len("/tickets")]; got != "/tickets" {
				t.Errorf("unprefixed app documents %q, want a bare /tickets… path", p)
			}
		}
		if len(servers) != 1 || servers[0] != "/" {
			t.Errorf("servers = %q, want exactly [\"/\"] with no prefix configured", servers)
		}
	})
}
