package crud

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// ?q= on an entity with no SearchFields was dropped, so the list
// answered every row to a client that believed it was searching, while
// ListAll refuses the same Search. It is now a 400; a blank q is still
// no search, and an entity whose own column is named q still filters it.
func TestSearchWithoutFieldsRefused(t *testing.T) {
	database := setupDB(t, `CREATE TABLE plain_items (id TEXT PRIMARY KEY, title TEXT);
		CREATE TABLE hostq_items (id TEXT PRIMARY KEY, title TEXT);
		CREATE TABLE q_items (id TEXT PRIMARY KEY, q TEXT);
		INSERT INTO q_items VALUES ('1', 'x'), ('2', 'y');
		CREATE TABLE hidq_items (id TEXT PRIMARY KEY, q TEXT);
		CREATE TABLE wireq_items (id TEXT PRIMARY KEY, tag TEXT);
		INSERT INTO wireq_items VALUES ('1', 'x'), ('2', 'y')`)
	serve := func(table string, fields []schema.Field, query string) *httptest.ResponseRecorder {
		def := entity.Define(table, entity.EntityConfig{Name: table, Table: table, Fields: fields}.WithTimestamps(false))
		def.SetDB(database)
		routes := router.New()
		RegisterCrudRoutes(routes, NewCrudHandler(def, database), "/"+table)
		rec := httptest.NewRecorder()
		routes.ServeHTTP(rec, withTestUser(httptest.NewRequest(http.MethodGet, "/"+table+query, nil), "alice"))
		return rec
	}
	plain := []schema.Field{{Name: "title", Type: schema.String}}
	for _, query := range []string{"?q=zzz", "?q=zzz&cursor=", "?q=zzz&limit=5"} {
		rec := serve("plain_items", plain, query)
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "search_not_configured") {
			t.Errorf("GET %s = %d %s, want 400 search_not_configured", query, rec.Code, rec.Body)
		}
	}
	for _, query := range []string{"", "?q=", "?q=%20%20"} {
		if rec := serve("plain_items", plain, query); rec.Code != http.StatusOK {
			t.Errorf("GET %q = %d %s, want 200", query, rec.Code, rec.Body)
		}
	}
	// A column named q, by name or by WireName, filters: only the
	// matching row comes back.
	for _, tc := range []struct {
		table string
		field schema.Field
	}{
		{"q_items", schema.Field{Name: "q", Type: schema.String}},
		{"wireq_items", schema.Field{Name: "tag", WireName: "q", Type: schema.String}},
	} {
		rec := serve(tc.table, []schema.Field{tc.field}, "?q=x")
		if rec.Code != http.StatusOK {
			t.Errorf("%s ?q=x = %d %s, want the filter", tc.table, rec.Code, rec.Body)
			continue
		}
		if body := rec.Body.String(); !strings.Contains(body, `"id":"1"`) || strings.Contains(body, `"id":"2"`) {
			t.Errorf("%s ?q=x = %s, want only row 1", tc.table, body)
		}
		// The gate refuses a repeated column q before the filter parser,
		// and on read-one too, where no filter parser runs.
		for _, path := range []string{"?q=x&q=y", "/1?q=x&q=y"} {
			repeated := serve(tc.table, []schema.Field{tc.field}, path)
			if repeated.Code != http.StatusBadRequest || !strings.Contains(repeated.Body.String(), "query_key_repeated") {
				t.Errorf("%s %s repeated column q = %d %s, want 400 query_key_repeated", tc.table, path, repeated.Code, repeated.Body)
			}
		}

	}
	// A host that owns q through AllowedFilterParams reads it itself, and
	// an allowed param may repeat: the one-value refusal belongs to the
	// framework's own search.
	hostQ := entity.Define("hostq_items", entity.EntityConfig{
		Name: "hostq_items", Table: "hostq_items", Fields: plain,
		AllowedFilterParams: []string{"q"},
	}.WithTimestamps(false))
	hostQ.SetDB(database)
	hostRoutes := router.New()
	RegisterCrudRoutes(hostRoutes, NewCrudHandler(hostQ, database), "/hostq_items")
	hostRec := httptest.NewRecorder()
	hostRoutes.ServeHTTP(hostRec, withTestUser(httptest.NewRequest(http.MethodGet, "/hostq_items?q=red&q=blue", nil), "alice"))
	if hostRec.Code != http.StatusOK {
		t.Errorf("host-owned ?q=red&q=blue = %d %s, want 200", hostRec.Code, hostRec.Body)
	}
	// A Hidden column named q is no filter a client can name, so ?q= is
	// a search the entity does not offer.
	rec := serve("hidq_items", []schema.Field{{Name: "q", Type: schema.String, Hidden: true}}, "?q=x")
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "search_not_configured") {
		t.Errorf("?q= on a Hidden column named q = %d %s, want 400 search_not_configured", rec.Code, rec.Body)
	}
}
