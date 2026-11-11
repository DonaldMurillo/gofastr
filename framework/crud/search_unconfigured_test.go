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
		CREATE TABLE q_items (id TEXT PRIMARY KEY, q TEXT)`)
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
	if rec := serve("q_items", []schema.Field{{Name: "q", Type: schema.String}}, "?q=x"); rec.Code != http.StatusOK {
		t.Errorf("?q= on a column named q = %d %s, want the filter", rec.Code, rec.Body)
	}
}
