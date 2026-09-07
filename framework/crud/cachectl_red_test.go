//go:build red

package crud

// RED TEST — open finding, 2026-09-06/07 adversarial round 5, phase 2
// (family enumeration; tier T1).
// Property: a GET response whose rows are scoped to the caller (OwnerField/
// tenant/read-scope) carries Cache-Control: no-store — the admin battery's
// pinned posture for exactly this data shape (fragmentcache_security_test.go:
// "an intermediary or the back/forward cache must never be able to retain one
// admin's row fragment"). RFC 9111's storage restriction covers
// Authorization-header requests, NOT cookie-authenticated ones — the
// framework's default session auth — the CDN/cookie asymmetry the embed
// CHANGELOG names ("two different subjects looked byte-identical to a CDN").
// Surfaces: crud.go::List (~:781), crud.go::Get, crud_cursor.go::
// serveCursorList (:178), crud_stream.go streaming list (:133) — every arm
// sets only Content-Type (recon probe: owner-scoped GET as alice → 200,
// Cache-Control "", Vary "", body = alice's rows only). crud's own llm.md
// arms already set no-store for these very routes. List/Get/cursor arms are
// driven below; the streaming arm shares the identical header shape.
// Finding: a shared cache that stores no-freshness 200s (cache-everything
// CDN config, transparent proxy) or a shared machine's bfcache replays user
// A's rows at user B's URL.
// Fix direction: stamp Cache-Control: no-store (admin-gate idiom) before the
// scope checks so 401/403 arms carry it too.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

func TestCrudRedScopedRowsNoStore(t *testing.T) {
	ddl := `CREATE TABLE notes (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, title TEXT);`
	cfg := makeEntityConfig("notes", "notes", "user_id", []schema.Field{
		{Name: "user_id", Type: schema.String, Required: true},
		{Name: "title", Type: schema.String},
	})
	ch, db := setupSecurityTestHandler(t, cfg, ddl)
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`INSERT INTO notes (id, user_id, title) VALUES ('a1','alice','alice-note'), ('b1','bob','bob-note')`); err != nil {
		t.Fatalf("setup broken: seed: %v", err)
	}

	get := func(path, id string) *httptest.ResponseRecorder {
		req := withTestUser(httptest.NewRequest(http.MethodGet, path, nil), "alice")
		if id != "" {
			req.SetPathValue("id", id)
		}
		rr := httptest.NewRecorder()
		if id == "" {
			ch.List()(rr, req)
		} else {
			ch.Get()(rr, req)
		}
		return rr
	}

	// Premise guard: the List body is caller-scoped (alice sees only her
	// row) — otherwise the cache assertion would be vacuous.
	list := get("/notes", "")
	if list.Code != http.StatusOK {
		t.Fatalf("setup broken: list status %d: %s", list.Code, list.Body.String())
	}
	if !strings.Contains(list.Body.String(), "alice-note") || strings.Contains(list.Body.String(), "bob-note") {
		t.Fatalf("setup broken: list is not owner-scoped: %s", list.Body.String())
	}
	if cc := list.Header().Get("Cache-Control"); !strings.Contains(cc, "no-store") {
		t.Errorf("SECURITY: [crud-rest-cachectl] owner-scoped GET /notes carries Cache-Control %q — every arm sets only Content-Type, so a shared cache that stores no-freshness 200s (or a shared machine's bfcache) replays alice's rows at bob's URL; the admin battery pins no-store for exactly this data shape and crud's own llm.md arms already suppress these routes", cc)
	}

	one := get("/notes/a1", "a1")
	if one.Code != http.StatusOK {
		t.Fatalf("setup broken: get status %d", one.Code)
	}
	if cc := one.Header().Get("Cache-Control"); !strings.Contains(cc, "no-store") {
		t.Errorf("SECURITY: [crud-rest-cachectl] owner-scoped GET /notes/a1 carries Cache-Control %q — same surface, same leak", cc)
	}

	// Cursor arm: follow the Link header's next-page URL when the list
	// emits one; the cursor endpoint is serveCursorList's surface.
	if link := list.Header().Get("Link"); strings.Contains(link, "cursor=") {
		if i := strings.IndexByte(link, '<'); i >= 0 {
			if j := strings.IndexByte(link, '>'); j > i {
				path := link[i+1 : j]
				if k := strings.IndexByte(path, '?'); k >= 0 {
					path = path[k:]
					cur := get("/notes"+path, "")
					if cc := cur.Header().Get("Cache-Control"); !strings.Contains(cc, "no-store") {
						t.Errorf("SECURITY: [crud-rest-cachectl] cursor list %s carries Cache-Control %q — serveCursorList shares the bare-Content-Type shape", path, cc)
					}
				}
			}
		}
	}
}
