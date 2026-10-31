package crud

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/pagination"
)

// ?fields= holds on a keyset page as on an offset page. The cursor
// branch returned before the projection ran, so ?cursor=&fields=title
// answered every visible column and an unknown field answered 200. A
// cursor column left out of the projection is read for the next cursor
// and kept off the rows.
func TestCursorPageHonorsFields(t *testing.T) {
	ch, db := setupSecurityTestHandler(t, makeEntityConfig("items", "items", "", []schema.Field{
		{Name: "title", Type: schema.String},
		{Name: "body", Type: schema.String},
		{Name: "rank", Type: schema.Int, Required: true},
	}, func(c *entity.EntityConfig) {
		c.Pagination = &entity.PaginationConfig{CursorFields: []string{"rank"}}
	}), `CREATE TABLE items (id TEXT PRIMARY KEY, title TEXT, body TEXT, rank INTEGER NOT NULL)`)
	seedRows(t, db, "items", []map[string]any{
		{"id": "i-1", "title": "c", "body": "x", "rank": 3},
		{"id": "i-2", "title": "a", "body": "y", "rank": 1},
		{"id": "i-3", "title": "b", "body": "z", "rank": 2},
	})
	get := func(q string) *httptest.ResponseRecorder {
		t.Helper()
		req := makeRequest(t, RequestOpts{Method: http.MethodGet, Path: "/items?" + q, UserID: "alice"})
		rr := httptest.NewRecorder()
		ch.List()(rr, req)
		return rr
	}

	if rr := get("cursor=&fields=no_such_col"); rr.Code != http.StatusBadRequest {
		t.Fatalf("?cursor=&fields=no_such_col = %d, want 400 as without cursor (body=%s)", rr.Code, rr.Body.String())
	}

	var titles []string
	cursor := ""
	for range 4 {
		rr := get("cursor=" + url.QueryEscape(cursor) + "&limit=2&fields=title")
		if rr.Code != http.StatusOK {
			t.Fatalf("keyset page = %d (body=%s)", rr.Code, rr.Body.String())
		}
		var page pagination.CursorPage
		if err := json.Unmarshal(rr.Body.Bytes(), &page); err != nil {
			t.Fatal(err)
		}
		for _, row := range page.Data {
			keys := make([]string, 0, len(row))
			for k := range row {
				keys = append(keys, k)
			}
			slices.Sort(keys)
			if !slices.Equal(keys, []string{"id", "title"}) {
				t.Fatalf("?fields=title row carried %v, want [id title]", keys)
			}
			titles = append(titles, row["title"].(string))
		}
		if !page.HasMore {
			break
		}
		cursor = page.Cursor
	}
	if !slices.Equal(titles, []string{"a", "b", "c"}) {
		t.Fatalf("paged titles = %v, want [a b c] in rank order", titles)
	}
}
