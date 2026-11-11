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

// encoding/json turns a malformed byte into U+FFFD, so a JSON body with
// raw invalid UTF-8 was stored as different text than the client sent
// (201), while the same NUL escaped as \u0000 was refused. JSON text is
// UTF-8 (RFC 8259): the body is refused before any decode, on create,
// update and batch alike.
func TestJSONBodyRefusesBadUTF8(t *testing.T) {
	database := setupDB(t, `CREATE TABLE utf_items (id TEXT PRIMARY KEY, title TEXT)`)
	definition := entity.Define("utf_items", entity.EntityConfig{
		Name: "utf_items", Table: "utf_items",
		Fields: []schema.Field{{Name: "title", Type: schema.String}},
	}.WithTimestamps(false))
	definition.SetDB(database)
	ch := NewCrudHandler(definition, database)
	routes := router.New()
	RegisterCrudRoutes(routes, ch, "/utf_items")
	row, err := ch.CreateOne(withTestUserCtx("alice"), map[string]any{"title": "kept"})
	if err != nil {
		t.Fatal(err)
	}
	id := row["id"].(string)
	for _, tc := range []struct{ method, path, body string }{
		{http.MethodPost, "/utf_items", "{\"title\":\"a\xff\xfeb\"}"},
		{http.MethodPatch, "/utf_items/" + id, "{\"title\":\"short\xe2\x82\"}"},
		{http.MethodPut, "/utf_items/" + id, "{\"title\":\"bad\xc0\xaf\"}"},
		{http.MethodPost, "/utf_items/_batch", "{\"items\":[{\"title\":\"a\xffb\"}]}"},
	} {
		req := withTestUser(httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body)), "alice")
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		routes.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "json_not_utf8") {
			t.Errorf("%s %s with invalid UTF-8 = %d %s, want 400 json_not_utf8", tc.method, tc.path, rec.Code, rec.Body)
		}
	}
	var titles []string
	rows, err := database.Query(`SELECT title FROM utf_items`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		titles = append(titles, s)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(titles) != 1 || titles[0] != "kept" {
		t.Fatalf("rows after refused writes = %q, want only the kept one", titles)
	}
}
