package crud

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// url.ParseQuery drops a pair that does not decode (%ZZ, a bare %, a
// ';' separator), so ?cursor=%ZZ restarted a cursor walk at page 1 with
// a 200, ?q=%ZZ read as no search and ?name=%ZZ as no filter: the
// client believed the list was narrowed and got every row. The list
// refuses such a query outright, on the offset and the cursor page.
func TestListRefusesUndecodableQuery(t *testing.T) {
	for _, cursorMode := range []bool{false, true} {
		ch, _ := covItems(t, func(c *entity.EntityConfig) {
			if !cursorMode {
				return
			}
			if c.Pagination == nil {
				c.Pagination = &entity.PaginationConfig{}
			}
			c.Pagination.CursorField = "seq"
		}, 3)
		get := func(query string) *httptest.ResponseRecorder {
			rec := httptest.NewRecorder()
			ch.List()(rec, withTestUser(httptest.NewRequest(http.MethodGet, "/items"+query, nil), "u1"))
			return rec
		}
		for _, q := range []string{
			"?cursor=%ZZ", "?cursor=abc%", "?cursor=abc&cursor=%ZZ",
			"?q=%ZZ", "?name=%ZZ", "?name=n%ZZ", "?name_like=x%",
			`?where={"field":"name","value":"x%ZZ"}`, "?name=n;x",
			"?na%ZZme=x", "?cursor=&direction=%ZZ", "?cursor=&direction=forward;x",
			"?stream=true&name=%ZZ",
		} {
			rec := get(q)
			if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "query_undecodable") {
				t.Errorf("cursor=%v GET %s = %d %s, want 400 query_undecodable", cursorMode, q, rec.Code, rec.Body)
			}
		}
		for _, q := range []string{"", "?name=n", "?name=%6E", "?cursor=", "?name=a%3Bb"} {
			if rec := get(q); rec.Code != http.StatusOK {
				t.Errorf("cursor=%v GET %s = %d %s, want 200", cursorMode, q, rec.Code, rec.Body)
			}
		}
	}
}

// ServeStreamingList is an exported entry an in-process caller reaches
// without List(): it reads its query the same strict way.
func TestStreamListRefusesUndecodable(t *testing.T) {
	ch, _ := covItems(t, nil, 3)
	rec := httptest.NewRecorder()
	req := withTestUser(httptest.NewRequest(http.MethodGet, "/items?trashed=%ZZ", nil), "u1")
	ch.ServeStreamingList(req.Context(), rec, req, []string{"id", "name"}, nil, nil, nil, 1, 10, nil)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "query_undecodable") {
		t.Errorf("ServeStreamingList ?trashed=%%ZZ = %d %s, want 400 query_undecodable", rec.Code, rec.Body)
	}
}

// Read-one read its query through URL.Query, which drops a pair that does
// not decode: ?fields=name%ZZ answered 200 with the full row, ?include=
// x%ZZ loaded nothing, and ?trashed=true%ZZ hid a trashed row the caller
// asked for (404). It parses strictly, as the list does.
func TestGetRefusesUndecodableQuery(t *testing.T) {
	db := setupDB(t, `CREATE TABLE items (id TEXT PRIMARY KEY, name TEXT, deleted_at TEXT)`)
	ent := entity.Define("items", entity.EntityConfig{
		Name: "items", Table: "items",
		Fields: []schema.Field{{Name: "name", Type: schema.String}},
		Scope:  &entity.ScopeConfig{SoftDelete: true},
	}.WithTimestamps(false))
	ent.SetDB(db)
	seedRows(t, db, "items", []map[string]any{{"id": "a", "name": "n", "deleted_at": "2024-01-01T00:00:00Z"}})
	ch := NewCrudHandler(ent, db)
	get := func(query string) *httptest.ResponseRecorder {
		req := withTestUser(httptest.NewRequest(http.MethodGet, "/items/a"+query, nil), "u1")
		req.SetPathValue("id", "a")
		rec := httptest.NewRecorder()
		ch.Get()(rec, req)
		return rec
	}
	for _, q := range []string{"?fields=name%ZZ", "?include=x%ZZ", "?trashed=true%ZZ", "?trashed=true&x=%ZZ", "?trashed=true;x"} {
		if rec := get(q); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "query_undecodable") {
			t.Errorf("GET %s = %d %s, want 400 query_undecodable", q, rec.Code, rec.Body)
		}
	}
	if rec := get("?trashed=true&fields=name"); rec.Code != http.StatusOK {
		t.Errorf("GET ?trashed=true&fields=name = %d %s, want 200", rec.Code, rec.Body)
	}
}
